package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLegacyOperationalRule(t *testing.T, product, proposalID, class, checkSpec string) {
	t.Helper()
	dir := filepath.Join(product, "learn", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rule := learnRuleRecord{
		Schema: learnRuleSchemaVersion, ProposalID: proposalID, Class: class,
		Rule: "Inspect the original evidence before retrying.", Trigger: "ambiguous execution result",
		CheckType: "gate", CheckSpec: checkSpec,
		TestCase: "ambiguous result -> inspect original evidence before retry",
		Status:   learnApplied, ApprovedAt: now, Approval: "grant:test",
		VerifiedAt: now, VerificationReceipt: "fixture:pass",
	}
	b, _ := json.MarshalIndent(rule, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, proposalID+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInitSharedLearningMigratesOperationalRule(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-test-operational", "execution-unknown-no-blind-retry", "operational-procedure-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	report, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "initialized" || len(report.MigratedRules) != 1 || report.Reviewer != "@self" {
		t.Fatalf("unexpected init report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules")); !os.IsNotExist(err) {
		t.Fatalf("active legacy rule directory survived cutover: %v", err)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "legacy-rules", "LP-test-operational.json")); err != nil {
		t.Fatal(err)
	}
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on {
		t.Fatalf("shared settings unavailable: on=%v err=%v", on, err)
	}
	index, err := executeSharedLearning(product, s, "index", nil)
	if err != nil || index.Status != "ok" || !strings.Contains(string(index.Data), "skills/learned/") {
		t.Fatalf("migrated procedure missing from shared index: %+v %v", index, err)
	}
	status, err := executeSharedLearning(product, s, "status", nil)
	if err != nil || status.Status != "ok" {
		t.Fatalf("shared status failed: %+v %v", status, err)
	}
	again, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil || again.Status != "already_initialized" {
		t.Fatalf("init is not idempotent: %+v %v", again, err)
	}
}

func TestInitSharedLearningRejectsUnsupportedLegacyRule(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-test-block", "blocking-rule", "custom-block-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if _, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err == nil {
		t.Fatal("unsupported legacy executable rule was migrated silently")
	}
	if _, err := os.Stat(filepath.Join(product, sharedLearningConfigFile)); !os.IsNotExist(err) {
		t.Fatal("failed init left shared selector behind")
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-test-block.json")); err != nil {
		t.Fatal("failed init lost legacy rule")
	}
}

func TestRunLearningProcessSelfBound(t *testing.T) {
	t.Setenv("AW_LEARNING_ADAPTER_HELPER", "1")
	product := t.TempDir()
	a := &learningProcessAdapter{
		Executable: "@self", SHA256: "@self",
		Args:      []string{"-test.run=^TestLearningProcessHelper$", "--", "review"},
		TimeoutMS: 5000,
	}
	out, err := runLearningProcess(context.Background(), product, a, json.RawMessage(`{"artifact":"x"}`))
	if err != nil || !strings.Contains(string(out), "kind") {
		t.Fatalf("self-bound adapter failed: %s %v", out, err)
	}
}
