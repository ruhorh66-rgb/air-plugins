package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func TestN118NativeValidatorConsumesPinnedBytesUnderPathReplacement(t *testing.T) {
	product, settings, args, _ := n118NativeGuardFixture(t)
	n118RestoreExistingValidFixture(t, product)
	candidate, err := prepareNativePreflightRecoveredUse(args, product)
	if err != nil || candidate == nil {
		t.Fatalf("no exact source-owned preflight snapshot: %+v %v", candidate, err)
	}
	// Swap BOTH paths to different bytes before the validator runs.
	// Its semantic inputs must be the previously securely opened bytes,
	// never whichever contents happen to be at the path during parsing.
	if err := os.WriteFile(filepath.Join(product, "run-config.json"),
		[]byte("{invalid config"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "PLAN.md"),
		[]byte("# forged incompatible PLAN\n"), 0600); err != nil {
		t.Fatal(err)
	}
	report, code := validateProductUsingPinnedInputs(product, &nativeValidatePinnedBytes{
		Config: candidate.PinnedConfig, Plan: candidate.PinnedPlan,
	})
	if code != 0 || report.Goals != 1 || report.Criteria != 1 {
		t.Fatalf("validator parsed replacement paths instead of exact opened bytes: code=%d goals=%d criteria=%d problems=%v",
			code, report.Goals, report.Criteria, report.Problems)
	}
	if err := recordNativePreflightRecoveredPass(candidate, report, code); err == nil {
		t.Fatal("post-validation SHA mismatch was credited despite changed on-disk paths")
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("tampered input paths credited %d successful learned uses", got)
	}
}

func TestN118NativeValidatorPinnedAndOrdinaryParseParity(t *testing.T) {
	root := t.TempDir()
	n118RestoreExistingValidFixture(t, root)
	config, err := os.ReadFile(filepath.Join(root, "run-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	ordinary, code := validateProduct(root)
	pinned, pinnedCode := validateProductUsingPinnedInputs(root, &nativeValidatePinnedBytes{
		Config: config, Plan: plan,
	})
	if code != 0 || pinnedCode != code || ordinary.Goals != pinned.Goals ||
		ordinary.Criteria != pinned.Criteria || ordinary.Plan != pinned.Plan ||
		len(ordinary.Problems) != len(pinned.Problems) {
		t.Fatalf("pinned plan/config parsing differs from canonical native validate: ordinary=%+v pinned=%+v", ordinary, pinned)
	}
}

func TestN118RollbackLedgerDuringValidationCannotBeCredited(t *testing.T) {
	product, settings, args, rule := n118NativeGuardFixture(t)
	n118RestoreExistingValidFixture(t, product)
	candidate, err := prepareNativePreflightRecoveredUse(args, product)
	if err != nil || candidate == nil {
		t.Fatalf("missing legitimate preflight at prepare: %v", err)
	}
	report, code := validateProductUsingPinnedInputs(product, &nativeValidatePinnedBytes{
		Config: candidate.PinnedConfig, Plan: candidate.PinnedPlan,
	})
	if code != 0 {
		t.Fatalf("known valid native validator failed: %+v", report.Problems)
	}
	// Use the OFFICIAL module rollback to retire the applied ledger.
	// The simulated attacker then restores the old Markdown file bytes
	// without using the module. Content SHA alone will again look valid,
	// so only a fresh ledger/index readback prevents false use.
	ledgerBytes, err := os.ReadFile(filepath.Join(settings.RuntimeRoot, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	// The module owns an append-only JSONL ledger; other fixtures may have
	// additional applied rows. Identify exactly one CURRENT original positive
	// preflight proposal by its target and SHA, not by journal row count.
	var applied struct {
		ProposalID string `json:"proposal_id"`
	}
	appliedRows := 0
	for _, line := range bytes.Split(bytes.TrimSpace(ledgerBytes), []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var entry struct {
			ProposalID string `json:"proposal_id"`
			Kind       string `json:"kind"`
			Target     string `json:"target"`
			PostSHA256 string `json:"post_sha256"`
			Status     string `json:"status"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			t.Fatal(err)
		}
		if entry.Status == "applied" && entry.Kind == "procedure" &&
			entry.Target == rule.Target && strings.EqualFold(entry.PostSHA256, rule.SHA256) {
			applied.ProposalID = entry.ProposalID
			appliedRows++
		}
	}
	if appliedRows != 1 || applied.ProposalID == "" {
		t.Fatalf("expected exactly one matching applied preflight transaction, got %d", appliedRows)
	}
	rollback, err := executeSharedLearning(product, settings, "rollback",
		map[string]string{"proposal_id": applied.ProposalID})
	if err != nil || rollback.Status == "error" {
		t.Fatalf("module-owned rollback failed: %+v %v", rollback, err)
	}
	target := filepath.Join(product, filepath.FromSlash(rule.Target))
	if err := os.WriteFile(target, []byte(n118SafePreflightProcedure), 0600); err != nil {
		t.Fatal(err)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !currentSelfLearningRuleBytes(owner, rule) {
		t.Fatalf("adversarial byte-identical file recreation did not occur: %v", err)
	}
	if err := recordNativePreflightRecoveredPass(candidate, report, code); err == nil {
		t.Fatal("module ledger rollback and fake file recreation credited prior approval")
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("rolled back module provenance credited %d uses", got)
	}
}
