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
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-test-operational.json")); err != nil {
		t.Fatalf("Git seed rule did not survive cutover: %v", err)
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

func TestInitSharedLearningRehydratesPackagedManagedProcedure(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-packaged-operational", "execution-unknown-no-blind-retry", "operational-procedure-v1")
	rules, sourcePaths, err := loadLegacyOperationalProcedures(product)
	if err != nil || len(rules) != 1 {
		t.Fatalf("legacy seed discovery: rules=%d err=%v", len(rules), err)
	}
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	intent, err := buildSharedLearningBootstrapIntent(product, runtimeRoot, "air-worker-test", rules, sourcePaths)
	if err != nil || len(intent.Rules) != 1 {
		t.Fatalf("bootstrap intent: %+v err=%v", intent, err)
	}
	procedure := legacyRuleProcedure(rules[0])
	target := filepath.Join(product, filepath.FromSlash(intent.Rules[0].Target))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(procedure), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil || report.Status != "initialized" {
		t.Fatalf("prepackaged managed procedure did not rehydrate runtime: %+v %v", report, err)
	}
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on {
		t.Fatalf("shared settings after rehydrate: on=%v err=%v", on, err)
	}
	if err := verifySharedBootstrapMigration(product, s, intent.Rules[0]); err != nil {
		t.Fatalf("rehydrated runtime lacks migration provenance: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != procedure {
		t.Fatalf("packaged managed procedure changed: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-packaged-operational.json")); err != nil {
		t.Fatalf("Git seed rule lost during rehydrate: %v", err)
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
