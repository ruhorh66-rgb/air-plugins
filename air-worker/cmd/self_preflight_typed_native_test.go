package main

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Fixture uses the real Go learning-module transactions but a test-only
// reviewer, not a production Codex reviewer or release acceptance.
func n093NativeTypedFixture(t *testing.T) (string, sharedLearningSettings, []string, selfLearningLoadedRule) {
	t.Helper()
	product, settings, _ := selfLearningFixture(t)
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight-n093")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("selector: %v", err)
	}
	sel.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	args := []string{"validate", "-product", product, "-json"}
	run := "AW-CLI-N093-TYPED-USE-TEST"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	res, err := executeSharedLearning(product, settings, "review", map[string]string{"run_id": run})
	if err != nil || res.Status != "candidate" {
		t.Fatalf("isolated native reviewer did not apply procedure: %+v %v", res, err)
	}
	// Unit fixtures bypass the production Codex adapter and review-batch;
	// explicitly simulate only their two native signed callback boundaries.
	n093AttestFixtureReviewedProcedure(t, product, run, res.ID)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("owner: %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("signed prior failure: %v", err)
	}
	rule, _, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible || rule.SHA256 != learnSHA([]byte(n093RealReviewedPreflight)) {
		t.Fatalf("novel applied procedure not eligible: %+v %v", rule, err)
	}
	return product, settings, args, rule
}

func TestN093ActualNativeTypedUseDeferredOutcomeNoValidationPass(t *testing.T) {
	product, settings, args, rule := n093NativeTypedFixture(t)
	rc, output := captureLoopOutput(t, func() int {
		return cmdValidate(args[1:])
	})
	if rc != 2 || !strings.Contains(output, "deferred_missing_configuration") ||
		!strings.Contains(output, "\"native_validation_executed\": false") {
		t.Fatalf("invalid native defer: rc=%d output=%s", rc, output)
	}
	if !nativePreflightDeferred.Swap(false) {
		t.Fatal("actual deferred validator did not suppress duplicate failure learning")
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("genuine native preflight action not recorded exactly once")
	}
	receipts, err := os.ReadDir(filepath.Join(settings.RuntimeRoot, "native-preflight-uses"))
	if err != nil || len(receipts) != 1 {
		t.Fatalf("typed receipt absent: %v %d", err, len(receipts))
	}
	data, err := os.ReadFile(filepath.Join(settings.RuntimeRoot, "native-preflight-uses", receipts[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var intent nativePreflightUseReceipt
	if err := json.Unmarshal(data, &intent); err != nil {
		t.Fatal(err)
	}
	if intent.Schema != "air-worker.n093.typed-preflight-intent/v1" ||
		intent.Status != "pending_procedure_used" || intent.Outcome != "pending" ||
		intent.Effect != "NOT_PROVEN" || intent.ConfirmedEventID != "" {
		t.Fatalf("pending native intent claimed completion: %+v", intent)
	}
	results, err := os.ReadDir(filepath.Join(settings.RuntimeRoot, "native-preflight-results"))
	if err != nil || len(results) != 1 {
		t.Fatalf("no independently confirmed native result: %v count=%d", err, len(results))
	}
	confirmedBytes, err := os.ReadFile(filepath.Join(settings.RuntimeRoot, "native-preflight-results", results[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var receipt nativePreflightUseReceipt
	if err := json.Unmarshal(confirmedBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.IntentSHA != learnSHA(data) || receipt.ConfirmedEventID == "" {
		t.Fatalf("final result is not bound to a signed intent and module event: %+v", receipt)
	}
	if receipt.Schema != "air-worker.n093.typed-preflight-use/v1" ||
		receipt.Status != "deferred_missing_configuration" ||
		receipt.Outcome != "deferred_missing_configuration" ||
		receipt.Effect != "avoided_repeat_validation_with_missing_config" ||
		receipt.NativeValidationExecuted || receipt.ActionSchema != nativeLearnedActionSchema ||
		receipt.ActionID != supportedNativePreflightAction().ID ||
		receipt.ActionVersion != 1 || receipt.ProcessID <= 0 ||
		receipt.Principal != "air-worker" || receipt.Session != "AW-CLI-N093-TYPED-USE-TEST" ||
		len(receipt.Nonce) != 24 || receipt.ProcedureSHA != rule.SHA256 {
		t.Fatalf("typed action receipt is missing machine evidence: %+v", receipt)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := nativeTypedPreflightReceiptMAC(key, receipt)
	for i := range key {
		key[i] = 0
	}
	if err != nil || !hmac.Equal([]byte(expected), []byte(receipt.ReceiptMAC)) {
		t.Fatal("typed native receipt lacks valid owner HMAC")
	}
	again, err := nativePreflightGuard(args)
	if err != nil || !again.Deferred || again.RuleID != rule.Target+"@"+rule.SHA256 ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("native replay changed original result or use count: %+v %v", again, err)
	}
	if _, err := os.Stat(filepath.Join(product, "run-config.json")); !os.IsNotExist(err) {
		t.Fatal("native preflight improperly created run-config.json")
	}
}

func TestN093TypedReceiptTamperCannotBeReplayedOrCreditedAgain(t *testing.T) {
	_, settings, args, _ := n093NativeTypedFixture(t)
	result, err := nativePreflightGuard(args)
	if err != nil || !result.Deferred {
		t.Fatalf("initial native measured use: %+v %v", result, err)
	}
	b, err := os.ReadFile(result.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved nativePreflightUseReceipt
	if err := json.Unmarshal(b, &saved); err != nil {
		t.Fatal(err)
	}
	saved.Effect = "claimed_native_validation_pass"
	forged, _ := json.Marshal(saved)
	if err := os.WriteFile(result.ReceiptPath, forged, 0600); err != nil {
		t.Fatal(err)
	}
	if retry, err := nativePreflightGuard(args); err == nil || retry.Deferred {
		t.Fatalf("modified signed machine receipt acquired use: %+v %v", retry, err)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("receipt tampering duplicated prior use=%d", got)
	}
}

func TestN093TypedUseCannotRunWithoutMissingConfiguration(t *testing.T) {
	product, settings, args, _ := n093NativeTypedFixture(t)
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte("{\"plan\":\"PLAN.md\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	if res, err := nativePreflightGuard(args); err != nil || res.Deferred {
		t.Fatalf("changed precondition credited native action: %+v %v", res, err)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("changed precondition emitted false use=%d", got)
	}
}

func TestN093ConcurrentGuardAtMostOneProcedureUsed(t *testing.T) {
	_, settings, args, _ := n093NativeTypedFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = nativePreflightGuard(args) }()
	}
	wg.Wait()
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("racing operations emitted %d uses", got)
	}
}

func TestN093ChildNativeValidate(t *testing.T) {
	if os.Getenv("AW_N093_NATIVE_CHILD") != "1" {
		return
	}
	product := os.Getenv("AW_N093_NATIVE_PRODUCT")
	if product == "" {
		os.Exit(12)
	}
	rc := cmdValidate([]string{"-product", product, "-json"})
	// This is a truthfully deferred native validation: action PASS, validator
	// NOT executed. Exit code 2 must remain non-success at the CLI boundary.
	os.Exit(rc)
}

func TestN093FreshProcessNativeTypedUseAndReplay(t *testing.T) {
	_, settings, args, rule := n093NativeTypedFixture(t)
	runChild := func() (string, int, error) {
		child := exec.Command(os.Args[0], "-test.run=^TestN093ChildNativeValidate$")
		child.Env = append(os.Environ(),
			"AW_N093_NATIVE_CHILD=1",
			"AW_N093_NATIVE_PRODUCT="+args[2])
		out, err := child.CombinedOutput()
		if err == nil {
			return string(out), 0, nil
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return string(out), -1, err
		}
		return string(out), exit.ExitCode(), nil
	}
	output, rc, err := runChild()
	if err != nil || rc != 2 || !strings.Contains(output, "deferred_missing_configuration") {
		t.Fatalf("fresh native child did not observe real deferred outcome: rc=%d err=%v out=%s", rc, err, output)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("fresh process emitted %d measured use events", got)
	}
	receiptDir := filepath.Join(settings.RuntimeRoot, "native-preflight-results")
	entries, err := os.ReadDir(receiptDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("missing native subprocess receipt: %v %d", err, len(entries))
	}
	b, err := os.ReadFile(filepath.Join(receiptDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var receipt nativePreflightUseReceipt
	if err := json.Unmarshal(b, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.ProcessID == os.Getpid() || receipt.ProcessID <= 0 ||
		receipt.ProcedureSHA != rule.SHA256 || receipt.Status != "deferred_missing_configuration" ||
		receipt.NativeValidationExecuted {
		t.Fatalf("not an independent native process/outcome: %+v", receipt)
	}
	original := learnSHA(b)
	output, rc, err = runChild()
	if err != nil || rc != 2 || !strings.Contains(output, "deferred_missing_configuration") {
		t.Fatalf("native child replay failed: rc=%d err=%v out=%s", rc, err, output)
	}
	replayed, err := os.ReadFile(filepath.Join(receiptDir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	if original != learnSHA(replayed) || n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("fresh process replay changed original signed receipt or duplicated native use")
	}
}

func TestN093LateConfigChangeAndDifferentInvocationCannotClaimUse(t *testing.T) {
	product, settings, args, _ := n093NativeTypedFixture(t)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("owner changed: %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("signed prior: %v", err)
	}
	rule, eventID, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible {
		t.Fatalf("review/ledger: %v", err)
	}

	wrongArgs := append(append([]string{}, args...), "-wrong-source")
	if r, err := recordNativePreflightPreventedRetry(owner, prior, rule, eventID, wrongArgs); err == nil || r.Deferred {
		t.Fatalf("wrong invocation gained signed native use: %+v %v", r, err)
	}
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte("{\"plan\":\"PLAN.md\"}"), 0600); err != nil {
		t.Fatal(err)
	}
	// This enters directly after an earlier preflight could already have
	// selected the learned action. The recorder must re-read machine facts.
	if r, err := recordNativePreflightPreventedRetry(owner, prior, rule, eventID, args); err == nil || r.Deferred {
		t.Fatalf("changed configuration acquired stale positive use: %+v %v", r, err)
	}
	if count := n104Events(t, settings.RuntimeRoot, "procedure_used"); count != 0 {
		t.Fatalf("TOCTOU precondition emitted %d false use events", count)
	}
}
