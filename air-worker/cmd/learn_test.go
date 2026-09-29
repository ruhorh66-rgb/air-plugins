package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedLearnProposal(t *testing.T, product, id, class, rule string) {
	t.Helper()
	p := learnPaths(product)
	row := learnProposal{
		Schema: learnSchemaVersion, ID: id, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status: learnPending, Class: class, Rule: rule,
	}
	if err := appendLearnJSON(p.Proposals, row); err != nil {
		t.Fatal(err)
	}
}

func grantLearnProposal(t *testing.T, product, id string) string {
	t.Helper()
	grantID, err := captureLearnApprovalGrant(product, "claude", "test-session", "да "+id)
	if err != nil {
		t.Fatal(err)
	}
	if grantID == "" {
		t.Fatal("approval prompt did not create a grant")
	}
	return grantID
}

func TestLearnApplyRequiresUserPromptApprovalGrant(t *testing.T) {
	product := t.TempDir()
	id := "LP-test-approval"
	seedLearnProposal(t, product, id, "plan-first", "Read the canonical plan before acting.")

	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 3 {
		t.Fatalf("apply without a captured user-prompt grant must return gate code 3, got %d", code)
	}
	if _, err := os.Stat(learnPaths(product).Rules); !os.IsNotExist(err) {
		t.Fatalf("RULES.md must not be created before approval, err=%v", err)
	}

	grantID := grantLearnProposal(t, product, id)
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 0 {
		t.Fatalf("approved apply failed: code=%d", code)
	}
	b, err := os.ReadFile(learnPaths(product).Rules)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), id) || !strings.Contains(string(b), "Read the canonical plan before acting.") {
		t.Fatalf("active rules do not contain applied proposal: %s", b)
	}
	proposals, err := readLearnProposals(learnPaths(product).Proposals)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 1 || proposals[0].Status != learnApplied || proposals[0].Approval != "grant:"+grantID || proposals[0].LedgerID == "" {
		t.Fatalf("proposal was not closed by the grant-backed transaction: %#v", proposals)
	}
}

func TestLearnRollbackRestoresExactPreviousBytes(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	if err := os.MkdirAll(paths.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	before := []byte("# existing rules\n\n- keep-this-byte-for-byte\n")
	if err := os.WriteFile(paths.Rules, before, 0o644); err != nil {
		t.Fatal(err)
	}
	id := "LP-test-rollback"
	seedLearnProposal(t, product, id, "delivery", "Verify delivery before reporting success.")
	grantLearnProposal(t, product, id)
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 0 {
		t.Fatalf("apply failed: %d", code)
	}
	proposals, _ := readLearnProposals(paths.Proposals)
	ledgerID := proposals[0].LedgerID
	if ledgerID == "" {
		t.Fatal("apply did not publish a ledger id")
	}
	if code := cmdLearnRollback([]string{"-product", product, "-id", ledgerID}); code != 0 {
		t.Fatalf("rollback failed: %d", code)
	}
	after, err := os.ReadFile(paths.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("rollback did not restore exact previous bytes\nwant=%q\n got=%q", before, after)
	}
	proposals, _ = readLearnProposals(paths.Proposals)
	if proposals[0].Status != learnRevoked {
		t.Fatalf("proposal status after rollback=%q", proposals[0].Status)
	}
	journal, err := readLearnJournal(paths.Journal)
	if err != nil {
		t.Fatal(err)
	}
	var lifecycle []learnJournalRecord
	for _, event := range journal {
		if event.Kind == "lifecycle" && event.Reference == id {
			lifecycle = append(lifecycle, event)
		}
	}
	if len(lifecycle) != 2 {
		t.Fatalf("apply+rollback must emit two canonical lifecycle events, got %#v", lifecycle)
	}
	if !strings.Contains(lifecycle[0].Evidence, "action=apply") || !strings.Contains(lifecycle[0].Evidence, "ledger=") {
		t.Fatalf("apply lifecycle evidence incomplete: %#v", lifecycle[0])
	}
	if !strings.Contains(lifecycle[1].Evidence, "action=rollback") || !strings.Contains(lifecycle[1].Evidence, "target_ledger="+ledgerID) {
		t.Fatalf("rollback lifecycle evidence incomplete: %#v", lifecycle[1])
	}
}

func TestLearnRollbackRefusesToClobberNewerRules(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	id := "LP-test-newer"
	seedLearnProposal(t, product, id, "scope", "Keep work inside the assigned product.")
	grantLearnProposal(t, product, id)
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 0 {
		t.Fatalf("apply failed: %d", code)
	}
	proposals, _ := readLearnProposals(paths.Proposals)
	ledgerID := proposals[0].LedgerID
	f, err := os.OpenFile(paths.Rules, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("- newer independent rule\n")
	_ = f.Close()
	if code := cmdLearnRollback([]string{"-product", product, "-id", ledgerID}); code != 3 {
		t.Fatalf("rollback over newer state must fail closed with code 3, got %d", code)
	}
	b, _ := os.ReadFile(paths.Rules)
	if !strings.Contains(string(b), "newer independent rule") {
		t.Fatal("failed rollback clobbered newer state")
	}
}

func TestLearnEffectCountsOnlyRepeatsAfterApply(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	id := "LP-test-effect"
	old := learnJournalRecord{
		Schema: learnSchemaVersion, ID: "LR-old", CreatedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		Class: "plan-first", Observed: "old repeat", Kind: "lesson", Source: "worker",
	}
	if err := appendLearnJSON(paths.Journal, old); err != nil {
		t.Fatal(err)
	}
	seedLearnProposal(t, product, id, "plan-first", "Read PLAN before taking the first action.")
	grantLearnProposal(t, product, id)
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 0 {
		t.Fatalf("apply failed: %d", code)
	}
	proposals, _ := readLearnProposals(paths.Proposals)
	appliedAt, err := time.Parse(time.RFC3339Nano, proposals[0].AppliedAt)
	if err != nil {
		t.Fatal(err)
	}
	journal, _ := readLearnJournal(paths.Journal)
	repeats := 0
	for _, row := range journal {
		ts, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
		if row.Class == "plan-first" && ts.After(appliedAt) {
			repeats++
		}
	}
	if repeats != 0 {
		t.Fatalf("pre-apply record counted as repeat: %d", repeats)
	}
	if code := cmdLearnEffect([]string{"-product", product, "-id", id, "-json"}); code != 0 {
		t.Fatalf("effect judge failed: %d", code)
	}

	later := learnJournalRecord{
		Schema: learnSchemaVersion, ID: "LR-new", CreatedAt: appliedAt.Add(time.Second).UTC().Format(time.RFC3339Nano),
		Class: "plan-first", Observed: "same class repeated", Kind: "lesson", Source: "worker",
	}
	if err := appendLearnJSON(paths.Journal, later); err != nil {
		t.Fatal(err)
	}
	journal, _ = readLearnJournal(paths.Journal)
	repeats = 0
	for _, row := range journal {
		ts, _ := time.Parse(time.RFC3339Nano, row.CreatedAt)
		if row.Class == "plan-first" && ts.After(appliedAt) {
			repeats++
		}
	}
	if repeats != 1 {
		t.Fatalf("expected exactly one post-apply repeat, got %d", repeats)
	}
}

func TestLearnContextIsEmptyBeforeFirstApprovedRule(t *testing.T) {
	product := t.TempDir()
	if code := cmdLearnContext([]string{"-product", filepath.Clean(product)}); code != 0 {
		t.Fatalf("empty context failed: %d", code)
	}
}
