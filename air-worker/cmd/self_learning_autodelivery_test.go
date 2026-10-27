package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Product SessionStart is the automatic trigger. A previously reviewed and
// applied safe preflight is necessary, but the operator never supplies a
// manual "learn self load" instruction in this source E2E.
func TestHLP06AutonomousTwoSessionTypedNativePreflight(t *testing.T) {
	product, settings, args, rule := n093NativeTypedFixture(t)
	stateDir := os.Getenv(hookStateDirEnv)
	first := declareLearningTestSession(t, stateDir, "gpt", "hlp06-first", product)
	first.HookEventName = "SessionStart"
	context, err := handleLearningContextWithReviewBatchStarter(first, nil)
	if err != nil || !strings.Contains(context.Context, "VERIFIED AUTOMATIC DELIVERY") ||
		!strings.Contains(context.Context, rule.SHA256) {
		t.Fatalf("first product-driven SessionStart failed to deliver exact skill: %v %s", err, context.Context)
	}
	receiptPath, err := selfLearningContextReceiptPath(first)
	if err != nil {
		t.Fatal(err)
	}
	before, err := n093ReadContextEvidence(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var firstRec selfLearningContextReceipt
	if err := json.Unmarshal(before, &firstRec); err != nil {
		t.Fatal(err)
	}
	if firstRec.DeliveryMAC == "" || len(firstRec.Loaded) != 1 ||
		firstRec.Loaded[0] != rule || firstRec.UsedAt != "" || firstRec.Outcome != "" ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("automatic delivery improperly claimed native use or lost HMAC")
	}
	// Idempotent duplicate lifecycle event: exact receipt bytes and SHA stable.
	repeated, err := handleLearningContextWithReviewBatchStarter(first, nil)
	if err != nil || repeated.Context != context.Context {
		t.Fatalf("same-session delivery is not idempotent: %v", err)
	}
	after, err := n093ReadContextEvidence(receiptPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("same-session delivery rewrote current signed context: %v", err)
	}

	// Genuine NEW process for the second host session; no model stub, no
	// human prompt and no live product/root ACL modifications.
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestHLP06AutonomousSessionChild$", "-test.v")
	child.Env = append(os.Environ(),
		"AW_HLP06_CHILD=1",
		"AW_HLP06_PRODUCT="+product,
		"AW_HLP06_STATE="+stateDir)
	output, err := child.CombinedOutput()
	if err != nil || !bytes.Contains(output, []byte("HLP06_CHILD_SIGNED_USE_PASS")) {
		t.Fatalf("new-session native action failed: %v output=%s", err, output)
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("second fresh process did not produce exactly one authenticated native use")
	}
	results := deliveryNativeReceipts(t, settings.RuntimeRoot)
	actual := 0
	for path, raw := range results {
		if !strings.Contains(path, "native-preflight-results") {
			continue
		}
		var rec nativePreflightUseReceipt
		if err := json.Unmarshal(raw, &rec); err != nil {
			t.Fatal(err)
		}
		if rec.Schema != "air-worker.n093.typed-preflight-use/v1" ||
			rec.DeliverySession != "hlp06-second" ||
			rec.ProcedureSHA != rule.SHA256 ||
			rec.Outcome != "deferred_missing_configuration" ||
			rec.Effect != "avoided_repeat_validation_with_missing_config" ||
			rec.NativeValidationExecuted || rec.ReceiptMAC == "" ||
			rec.ConfirmedEventID == "" {
			t.Fatalf("fresh native receipt does not prove effect: %+v", rec)
		}
		actual++
	}
	if actual != 1 {
		t.Fatalf("fresh sessions generated unexpected number of actual native results: %d", actual)
	}
	_ = args // Original native argv belongs to the reviewed fixture.
}

func TestHLP06AutonomousSessionChild(t *testing.T) {
	if os.Getenv("AW_HLP06_CHILD") != "1" {
		return
	}
	product := os.Getenv("AW_HLP06_PRODUCT")
	state := os.Getenv("AW_HLP06_STATE")
	if product == "" || state == "" {
		t.Fatal("missing child session input")
	}
	t.Setenv(hookStateDirEnv, state)
	id, err := parseIdentity("gpt", "hlp06-second")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{
		Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{
		Path: product, Principal: id.Principal, SessionKey: id.SessionKey,
	}); err != nil {
		t.Fatal(err)
	}
	in := hookInput{Principal: "gpt", SessionID: "hlp06-second",
		RunID: "hlp06-run-2", HookEventName: "SessionStart"}
	response, err := handleLearningContextWithReviewBatchStarter(in, nil)
	if err != nil || !strings.Contains(response.Context, "VERIFIED AUTOMATIC DELIVERY") {
		t.Fatalf("native SessionStart failed: %v %s", err, response.Context)
	}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("owner unavailable: %v", err)
	}
	current, path, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || len(current.Loaded) != 1 {
		t.Fatalf("no current delivery: %v", err)
	}
	raw, err := n093ReadContextEvidence(path)
	if err != nil {
		t.Fatal(err)
	}
	proof := learningDeliveryProof{"gpt", "hlp06-second", "hlp06-run-2", learnSHA(raw)}
	if err := verifyCurrentLearningDelivery(owner, proof, current.Loaded[0]); err != nil {
		t.Fatal(err)
	}
	args := []string{"-product", product, "-json",
		"-learning-principal", proof.Principal,
		"-learning-session-key", proof.Session,
		"-learning-run-id", proof.RunID,
		"-learning-context-sha256", proof.ContextSHA}
	code, stdout := captureLoopOutput(t, func() int { return cmdValidate(args) })
	if code != 2 || !strings.Contains(stdout, "deferred_missing_configuration") ||
		!strings.Contains(stdout, "outcome_receipt_path") {
		t.Fatalf("supported native action was not linked to delivery: rc=%d output=%s", code, stdout)
	}
	if n104Events(t, owner.Settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("expected one actual native procedure_used")
	}
	// Idempotent retry is the SAME signed result, not a new use/provider job.
	code, stdout = captureLoopOutput(t, func() int { return cmdValidate(args) })
	if code != 2 || !strings.Contains(stdout, "outcome_receipt_path") ||
		n104Events(t, owner.Settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("replayed run changed native effect: code=%d output=%s", code, stdout)
	}
	nativePreflightDeferred.Swap(false)
	t.Log("HLP06_CHILD_SIGNED_USE_PASS")
	_ = filepath.Separator
}

func TestHLP06NoReviewedPriorDoesNotInventAutomaticUse(t *testing.T) {
	product, settings, state := selfLearningFixture(t)
	in := declareLearningTestSession(t, state, "gpt", "hlp06-no-prior", product)
	in.HookEventName = "SessionStart"
	res, err := handleLearningContextWithReviewBatchStarter(in, nil)
	if err != nil || strings.Contains(res.Context, "VERIFIED AUTOMATIC DELIVERY") {
		t.Fatalf("no prior signed failure but auto-delivery claimed: %v %s", err, res.Context)
	}
	ctx, _, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || ctx.DeliveryMAC != "" || n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatalf("ordinary context promoted to authenticated delivery or use: %v found=%v", err, found)
	}
}

func TestHLP06UserPromptCannotMintSessionStartDelivery(t *testing.T) {
	product, settings, _, _ := n093NativeTypedFixture(t)
	state := os.Getenv(hookStateDirEnv)
	in := declareLearningTestSession(t, state, "gpt", "hlp06-userprompt", product)
	in.HookEventName = "UserPromptSubmit"
	res, err := handleLearningContextWithReviewBatchStarter(in, nil)
	if err != nil || strings.Contains(res.Context, "VERIFIED AUTOMATIC DELIVERY") {
		t.Fatalf("untrusted non-start lifecycle minted automatic delivery: %v %s", err, res.Context)
	}
	rec, _, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || rec.DeliveryMAC != "" || n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatalf("prompt context minted signed machine action: %v found=%v", err, found)
	}
}
