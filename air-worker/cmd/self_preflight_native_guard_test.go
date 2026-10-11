package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n118NativeGuardFixture(t *testing.T) (string, sharedLearningSettings, []string, selfLearningLoadedRule) {
	t.Helper()
	product, settings, _ := selfLearningFixture(t)
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("no fixture selector: %v", err)
	}
	sel.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	args := []string{"validate", "-product", product, "-json"}
	run := "AW-CLI-N118-PROTECTED-RETRY-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	review, err := executeSharedLearning(product, settings, "review", map[string]string{"run_id": run})
	if err != nil || review.Status != "candidate" {
		t.Fatalf("native fixture reviewer failed: %+v %v", review, err)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("self owner missing: %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("original signed failure missing: %v %v", found, err)
	}
	rule, _, found, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !found {
		t.Fatalf("no applied reviewed rule: %v %v", found, err)
	}
	return product, settings, args, rule
}

func TestN118ActualGuardCreatesOnlyMeasuredDeferredNativeUse(t *testing.T) {
	_, settings, args, rule := n118NativeGuardFixture(t)
	result, err := nativePreflightGuard(args)
	if err != nil || !result.Deferred || result.RuleID != rule.Target+"@"+rule.SHA256 {
		t.Fatalf("no actual protected native retry: %+v %v", result, err)
	}
	b, err := os.ReadFile(result.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt map[string]any
	if err := json.Unmarshal(b, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt["status"] != "native_preflight_retry_prepared" ||
		receipt["native_validation_executed"] != false ||
		receipt["outcome"] != "pending" ||
		receipt["effect"] != "pending_correlated_native_use_event" {
		t.Fatalf("no observed machine effect: %+v", receipt)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("missing exact native used event: %d", got)
	}
	if again, err := nativePreflightGuard(args); err != nil || !again.Deferred ||
		again.ReceiptPath != result.ReceiptPath {
		t.Fatalf("duplicate native retry not idempotent: %+v %v", again, err)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("duplicate native retry recorded %d use events", got)
	}
}

func TestN118GuardCannotCreditMissingPriorOrStaleProcedure(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	args := []string{"validate", "-product", product, "-json"}
	if result, err := nativePreflightGuard(args); err != nil || result.Deferred {
		t.Fatalf("no previous failure yet native use: %+v %v", result, err)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("false native procedure use count=%d", got)
	}

	_, s, retry, rule := n118NativeGuardFixture(t)
	priorUse := n104Events(t, s.RuntimeRoot, "procedure_used")
	path := filepath.Join(s.RuntimeRoot, "..", "air-worker", filepath.FromSlash(rule.Target))
	// The product path above is reconstructed only for this isolated test:
	// this rule is release-owned, not a host-provided arbitrary target.
	if err := os.WriteFile(path, []byte(n118SafePreflightProcedure+"\nTAMPERED"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := nativePreflightGuard(retry); err == nil || result.Deferred {
		t.Fatalf("stale reviewed skill falsely used: %+v %v", result, err)
	}
	if got := n104Events(t, s.RuntimeRoot, "procedure_used"); got != priorUse {
		t.Fatalf("stale procedure emitted false native use: %d", got)
	}
}

func TestN118RealValidateDefersRatherThanFakingSuccessOrDoubleReview(t *testing.T) {
	product, settings, _, rule := n118NativeGuardFixture(t)
	before := n104Events(t, settings.RuntimeRoot, "run_completed")
	rc, out := captureLoopOutput(t, func() int {
		return cmdValidate([]string{"-product", product, "-json"})
	})
	if rc != 2 || !strings.Contains(out, "deferred_missing_configuration") ||
		!strings.Contains(out, "native_validation_executed") ||
		!strings.Contains(out, rule.Target+"@"+rule.SHA256) {
		t.Fatalf("native validate did not defer with exact proof rc=%d output=%s", rc, out)
	}
	// In the host executable main() sees code 2 after cmdValidate. The
	// release-owned marker must suppress a second generic CLI-failure
	// review only for this already-machine-measured native action.
	if !nativePreflightDeferred.Swap(false) {
		t.Fatal("successful native defer did not mark its duplicate-capture guard")
	}
	if got := n104Events(t, settings.RuntimeRoot, "run_completed"); got != before {
		t.Fatalf("deferred native use fabricated %d extra failures", got-before)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("real native validate did not emit exactly one use: %d", got)
	}
}

func TestN118UnrelatedNativeValidationNeverClaimsProcedureUse(t *testing.T) {
	product, settings, args, _ := n118NativeGuardFixture(t)
	args = append(append([]string{}, args...), "-unsupported-secret=PRIVATE")
	if res, err := nativePreflightGuard(args); err != nil || res.Deferred {
		t.Fatalf("unrelated/unsupported argv acquired prior learned use: %+v %v", res, err)
	}
	if count := n104Events(t, settings.RuntimeRoot, "procedure_used"); count != 0 {
		t.Fatalf("unrelated validation emitted false procedure_used=%d", count)
	}
	if code, _ := captureLoopOutput(t, func() int {
		return cmdValidate([]string{"-product", product, "-unknown-untrusted-option"})
	}); code != 2 {
		t.Fatalf("unknown native option got unexpected code %d", code)
	}
	if nativePreflightDeferred.Swap(false) {
		t.Fatal("non-preflight failure borrowed a successful-use marker")
	}
}
