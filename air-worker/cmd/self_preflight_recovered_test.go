package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Restoration here is a synthetic test fixture drawn from established
// AirWorker validation contracts, NOT an actual repair of any live product.
func n118RestoreExistingValidFixture(t *testing.T, product string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte(minimalRunConfig), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "PLAN.md"), []byte(minimalGoodPlan), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestN118ActualNativeValidationPassAfterRestoredConfigClaimsOnlyMeasuredUse(t *testing.T) {
	product, settings, args, learned := n118NativeGuardFixture(t)
	n118RestoreExistingValidFixture(t, product)
	_, baseRC := validateProduct(product)
	if baseRC != 0 {
		t.Fatal("canonical fixture did not yield actual native validate PASS")
	}
	out, rc := captureStdout(t, func() int { return cmdValidate(args[1:]) })
	if rc != 0 {
		t.Fatalf("actual native validate did not PASS: rc=%d %s", rc, out)
	}
	dir := filepath.Join(settings.RuntimeRoot, "native-preflight-recovered")
	receipts, err := os.ReadDir(dir)
	if err != nil || len(receipts) != 1 {
		t.Fatalf("missing native successful-validation receipt: %v count=%d", err, len(receipts))
	}
	raw, err := os.ReadFile(filepath.Join(dir, receipts[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["status"] != "validated_pass" ||
		receipt["native_validation_executed"] != true ||
		receipt["validation_exit_code"] != float64(0) ||
		receipt["procedure_sha256"] != learned.SHA256 ||
		receipt["effect"] != "restored_configuration_followed_by_actual_native_validate_pass" {
		t.Fatalf("false or missing successful native observation: %+v", receipt)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("restored native validation must generate one measured use, got %d", got)
	}
	_, rc = captureStdout(t, func() int { return cmdValidate(args[1:]) })
	if rc != 0 || n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("successful validation replay was not idempotent")
	}
}

func TestN118NoSuccessfulUseForInvalidRestorationOrNoPrior(t *testing.T) {
	product, settings, args, _ := n118NativeGuardFixture(t)
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte("{\"plan\":\"PLAN.md\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	_, rc := captureStdout(t, func() int { return cmdValidate(args[1:]) })
	if rc == 0 {
		t.Fatal("invalid restoration unexpectedly validated")
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("failed validate emitted a fabricated successful procedure use")
	}
	if _, err := os.Stat(filepath.Join(settings.RuntimeRoot, "native-preflight-recovered")); !os.IsNotExist(err) {
		t.Fatal("failed native validate published a success receipt")
	}

	product2, settings2, _ := selfLearningFixture(t)
	n118RestoreExistingValidFixture(t, product2)
	_, rc = captureStdout(t, func() int { return cmdValidate([]string{"-product", product2, "-json"}) })
	if rc != 0 {
		t.Fatal("ordinary valid product was incorrectly rejected")
	}
	if n104Events(t, settings2.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("unrelated successful validation was credited as learned use")
	}
}

func TestN118TamperedRuleNotCreditedAfterValidationPass(t *testing.T) {
	product, settings, args, learned := n118NativeGuardFixture(t)
	n118RestoreExistingValidFixture(t, product)
	path := filepath.Join(product, filepath.FromSlash(learned.Target))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, []byte("\nUNTRUSTED-INSTRUCTION")...), 0600); err != nil {
		t.Fatal(err)
	}
	_, rc := captureStdout(t, func() int { return cmdValidate(args[1:]) })
	if rc != 0 {
		t.Fatalf("tampered learned rule interrupted true native validation: rc=%d", rc)
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("stale/tampered learned procedure credited successful native use")
	}
}

func TestN118RecoveredValidationRejectsConfigOrPlanTOCTOU(t *testing.T) {
	product, settings, args, _ := n118NativeGuardFixture(t)
	n118RestoreExistingValidFixture(t, product)
	candidate, err := prepareNativePreflightRecoveredUse(args, product)
	if err != nil || candidate == nil {
		t.Fatalf("no SHA-snapshot for actual restored validator: %+v %v", candidate, err)
	}
	report, code := validateProduct(product)
	if code != 0 {
		t.Fatalf("known valid fixture did not validate: %+v", report.Problems)
	}
	planPath := filepath.Join(product, "PLAN.md")
	current, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, append(current, []byte("\n# changed after validation")...), 0600); err != nil {
		t.Fatal(err)
	}
	if err := recordNativePreflightRecoveredPass(candidate, report, code); err == nil {
		t.Fatal("post-validation PLAN byte change was falsely credited as procedure use")
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("TOCTOU created %d false procedure_used events", got)
	}
}

func TestN118OriginalSignedFailureCannotBeReplacedBeforeRecoveredUse(t *testing.T) {
	product, settings, args, _ := n118NativeGuardFixture(t)
	n118RestoreExistingValidFixture(t, product)
	candidate, err := prepareNativePreflightRecoveredUse(args, product)
	if err != nil || candidate == nil {
		t.Fatalf("no exact prior signed failure selected: %v", err)
	}
	report, code := validateProduct(product)
	if code != 0 {
		t.Fatalf("canonical product fixture not valid: %+v", report.Problems)
	}
	path := filepath.Join(settings.RuntimeRoot, "cli-failures", candidate.Prior.RunID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec ownCLIFailureReceipt
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Observed = "untrusted replacement"
	forged, _ := json.Marshal(rec)
	if err := os.WriteFile(path, forged, 0600); err != nil {
		t.Fatal(err)
	}
	if err := recordNativePreflightRecoveredPass(candidate, report, code); err == nil {
		t.Fatal("changed original signed failure was credited as positive learned use")
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("forged native evidence produced procedure_used")
	}
}
