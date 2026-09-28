package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func supportedSGTProposal(id string) learnProposal {
	return learnProposal{
		Schema: learnSchemaVersion, ID: id, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status: learnPending, Class: learnRuleVerifyBeforeClaim,
		Rule:      "Before committing, verify HH:MM SGT labels against current SGT.",
		Trigger:   "before git commit in the managed product",
		CheckType: "hook",
		CheckSpec: learnCheckSGTCommitFreshness,
		TestCase:  "19:09 SGT at current 19:20 SGT -> block; 19:10 SGT -> allow",
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func initLearnRuleGitRepo(t *testing.T, product string) {
	t.Helper()
	runGit(t, product, "init")
	runGit(t, product, "config", "user.email", "airworker-test@example.invalid")
	runGit(t, product, "config", "user.name", "AirWorker Test")
	if err := os.WriteFile(filepath.Join(product, "seed.txt"), []byte("seed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, product, "add", "seed.txt")
	runGit(t, product, "commit", "-m", "seed")
}

func TestSGTCommitFreshnessCases(t *testing.T) {
	now := time.Date(2026, 9, 28, 11, 20, 0, 0, time.UTC) // 19:20 SGT
	for _, tc := range []struct {
		line  string
		block bool
	}{
		{"reported 19:15 SGT", false},
		{"reported 19:10 SGT", false},
		{"reported 19:09 SGT", true},
		{"no timestamp here", false},
	} {
		got := len(staleSGTLabels([]string{tc.line}, now)) > 0
		if got != tc.block {
			t.Fatalf("%q block=%v want=%v", tc.line, got, tc.block)
		}
	}
	midnight := time.Date(2026, 9, 28, 16, 5, 0, 0, time.UTC) // 00:05 SGT
	if got := staleSGTLabels([]string{"reported 23:59 SGT"}, midnight); len(got) != 0 {
		t.Fatalf("midnight wrap false positive: %#v", got)
	}
}

func TestSupportedExecutableRuleRequiresGrantThenActivatesAndRollsBack(t *testing.T) {
	product := t.TempDir()
	initLearnRuleGitRepo(t, product)
	paths := learnPaths(product)
	p := supportedSGTProposal("LP-sgt-apply")
	if err := appendLearnJSON(paths.Proposals, p); err != nil {
		t.Fatal(err)
	}

	if code := cmdLearnApply([]string{"-product", product, "-id", p.ID}); code != 3 {
		t.Fatalf("supported executable proposal without grant code=%d want 3", code)
	}
	grantID := grantLearnProposal(t, product, p.ID)
	if code := cmdLearnApply([]string{"-product", product, "-id", p.ID}); code != 0 {
		t.Fatalf("grant-backed executable apply failed: %d", code)
	}

	rulePath := executableRulePath(paths, p.ID)
	raw, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), learnCheckSGTCommitFreshness) || !strings.Contains(string(raw), "cases=4/4") || !strings.Contains(string(raw), "grant:"+grantID) {
		t.Fatalf("verified manifest missing evidence: %s", raw)
	}
	active, err := approvedLearnRules(product)
	if err != nil || !strings.Contains(string(active), p.ID) {
		t.Fatalf("active rule context missing: %q err=%v", active, err)
	}
	proposals, err := readLearnProposals(paths.Proposals)
	if err != nil || len(proposals) != 1 || proposals[0].Status != learnApplied || proposals[0].LedgerID == "" {
		t.Fatalf("proposal not applied: %#v err=%v", proposals, err)
	}
	ledgerID := proposals[0].LedgerID

	if err := os.WriteFile(filepath.Join(product, "status.md"), []byte("reported 19:00 SGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, product, "add", "status.md")
	now := time.Date(2026, 9, 28, 11, 20, 0, 0, time.UTC) // 19:20 SGT
	blocked, reason, err := enforceSGTCommitRule(product, "git commit -m stale", now)
	if err != nil || !blocked || !strings.Contains(reason, "curator-check") {
		t.Fatalf("stale staged label was not blocked: blocked=%v reason=%q err=%v", blocked, reason, err)
	}

	if err := os.WriteFile(filepath.Join(product, "status.md"), []byte("reported 19:15 SGT\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, product, "add", "status.md")
	blocked, reason, err = enforceSGTCommitRule(product, "git commit -m fresh", now)
	if err != nil || blocked {
		t.Fatalf("fresh staged label was blocked: reason=%q err=%v", reason, err)
	}

	if code := cmdLearnRollback([]string{"-product", product, "-id", ledgerID}); code != 0 {
		t.Fatalf("typed rule rollback failed: %d", code)
	}
	if _, err := os.Stat(rulePath); !os.IsNotExist(err) {
		t.Fatalf("typed rule manifest survived rollback: %v", err)
	}
}

func TestExecutableApprovalDigestBindsCheckSpecAndTestCase(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	p := supportedSGTProposal("LP-sgt-digest")
	if err := appendLearnJSON(paths.Proposals, p); err != nil {
		t.Fatal(err)
	}
	grantLearnProposal(t, product, p.ID)

	rows, err := readLearnProposals(paths.Proposals)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].TestCase = "tampered after approval"
	pb, _ := marshalLearnJSONL(rows)
	if err := writeLearnAtomic(paths.Proposals, pb); err != nil {
		t.Fatal(err)
	}
	if code := cmdLearnApply([]string{"-product", product, "-id", p.ID}); code != 3 {
		t.Fatalf("tampered executable proposal reused approval grant; code=%d", code)
	}
}
