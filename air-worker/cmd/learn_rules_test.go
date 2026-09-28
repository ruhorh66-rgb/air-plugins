package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLearnProposeRequiresExecutableSpec(t *testing.T) {
	product := t.TempDir()
	code := cmdLearnPropose([]string{
		"-product", product,
		"-class", "verify-before-claim",
		"-rule", "Include fresh verdict time before reporting distance.",
	})
	if code != 2 {
		t.Fatalf("proposal without executable spec code=%d want 2", code)
	}
	if _, err := os.Stat(learnPaths(product).Proposals); !os.IsNotExist(err) {
		t.Fatalf("invalid proposal mutated registry: err=%v", err)
	}

	code = cmdLearnPropose([]string{
		"-product", product,
		"-class", "verify-before-claim",
		"-rule", "Include fresh verdict time before reporting distance.",
		"-trigger", "before reporting a distance or PASS claim",
		"-check-type", "hook",
		"-check-spec", "block claim when verdict timestamp is absent or stale",
		"-test-case", "claim without verdict time -> block",
		"-source", "LE-1",
	})
	if code != 0 {
		t.Fatalf("proposal with executable spec code=%d", code)
	}
	rows, err := readLearnProposals(learnPaths(product).Proposals)
	if err != nil || len(rows) != 1 {
		t.Fatalf("proposals=%#v err=%v", rows, err)
	}
	p := rows[0]
	if p.Trigger == "" || p.CheckType != "hook" || p.CheckSpec == "" || p.TestCase == "" {
		t.Fatalf("executable spec was not stored: %#v", p)
	}
}

func TestExecutableProposalCannotActivateBeforeVerifiedCheck(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	p := learnProposal{
		Schema: learnSchemaVersion, ID: "LP-spec", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Status: learnPending, Class: "scope", Rule: "Stay inside assigned product.",
		Trigger: "before a write", CheckType: "hook",
		CheckSpec: "block write outside assigned product",
		TestCase:  "write outside product -> block",
	}
	if err := appendLearnJSON(paths.Proposals, p); err != nil {
		t.Fatal(err)
	}
	if code := cmdLearnApply([]string{"-product", product, "-id", p.ID}); code != 2 {
		t.Fatalf("executable proposal activated before L11-4 verification: code=%d", code)
	}
	rows, err := readLearnProposals(paths.Proposals)
	if err != nil || len(rows) != 1 || rows[0].Status != learnPending {
		t.Fatalf("proposal changed despite missing verified check: rows=%#v err=%v", rows, err)
	}
	if _, err := os.Stat(paths.Rules); !os.IsNotExist(err) {
		t.Fatalf("legacy RULES.md was mutated by blocked executable proposal: %v", err)
	}
}

func TestVerifiedRuleManifestIsOnlyTypedActiveRule(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	if err := os.MkdirAll(paths.RulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rule := learnRuleRecord{
		Schema:              learnRuleSchemaVersion,
		ProposalID:          "LP-verified",
		Class:               "scope",
		Rule:                "Stay inside assigned product.",
		Trigger:             "before write",
		CheckType:           "hook",
		CheckSpec:           "block outside product",
		TestCase:            "outside write -> block",
		Status:              learnApplied,
		ApprovedAt:          now,
		Approval:            "grant:test",
		VerifiedAt:          now,
		VerificationReceipt: "receipt:test-pass",
	}
	raw, _ := json.Marshal(rule)
	path := filepath.Join(paths.RulesDir, rule.ProposalID+".json")
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	text, err := approvedLearnRules(product)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(text), rule.ProposalID) || !strings.Contains(string(text), rule.Rule) {
		t.Fatalf("typed active rule missing from context: %s", text)
	}

	rule.VerifiedAt = ""
	raw, _ = json.Marshal(rule)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := approvedLearnRules(product); err == nil {
		t.Fatal("unverified APPLIED rule was accepted")
	}

	rule.VerifiedAt = now
	rule.Status = learnPending
	raw, _ = json.Marshal(rule)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := approvedLearnRules(product); err == nil {
		t.Fatal("PENDING rule in learn/rules was accepted as active")
	}
}
