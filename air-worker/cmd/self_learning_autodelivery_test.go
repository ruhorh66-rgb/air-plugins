package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	if n104Events(t, owner.Settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("SessionStart delivery incorrectly pre-credited actual native use")
	}
	// No special/manual cmdValidate call. The normal, already established
	// UserPromptSubmit lifecycle event automatically invokes the only safe
	// native preflight, then independently authenticates its signed result.
	in.HookEventName = "UserPromptSubmit"
	in.HostTrusted = true // cmdHook checks external host provenance in production
	result, err := handleUserPromptLearning(in)
	if err != nil || !strings.Contains(result.Context, "AIRWORKER AUTOMATIC NATIVE PREFLIGHT") ||
		!strings.Contains(result.Context, "deferred_missing_configuration") ||
		!strings.Contains(result.Context, "signed_receipt=") ||
		!strings.Contains(result.Context, "native_validator_executed=false") {
		t.Fatalf("product lifecycle failed to perform signed native action: %v %s", err, result.Context)
	}
	if n104Events(t, owner.Settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("automatic lifecycle did not record exactly one actual native use")
	}
	// Recovery after a duplicate prompt is the SAME signed result, never a
	// second provider call, second native action or a new attribution.
	repeated, err := handleUserPromptLearning(in)
	if err != nil || repeated.Context != result.Context ||
		n104Events(t, owner.Settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("replayed lifecycle mutated the signed result or use count: %v", err)
	}
	t.Log("HLP06_CHILD_SIGNED_USE_PASS")
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

func TestHLP06TamperedSignedDeliveryCannotUse(t *testing.T) {
	product, settings, _, _ := n093NativeTypedFixture(t)
	state := os.Getenv(hookStateDirEnv)
	in := declareLearningTestSession(t, state, "gpt", "hlp06-tampered", product)
	in.HookEventName = "SessionStart"
	if _, err := handleLearningContextWithReviewBatchStarter(in, nil); err != nil {
		t.Fatal(err)
	}
	context, path, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || context.DeliveryMAC == "" {
		t.Fatalf("signed SessionStart delivery missing: %v", err)
	}
	context.DeliveryMAC = "forged"
	if err := writeSelfLearningContextReceipt(path, context); err != nil {
		t.Fatal(err)
	}
	in.HookEventName = "UserPromptSubmit"
	in.HostTrusted = true
	result, err := handleUserPromptLearning(in)
	if err == nil || strings.Contains(result.Context, "AUTOMATIC NATIVE PREFLIGHT") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatalf("forged delivery acquired native use: %v context=%s", err, result.Context)
	}
}

func TestHLP06StaleProcedureCannotUse(t *testing.T) {
	product, settings, _, rule := n093NativeTypedFixture(t)
	state := os.Getenv(hookStateDirEnv)
	in := declareLearningTestSession(t, state, "gpt", "hlp06-stale", product)
	in.HookEventName = "SessionStart"
	if _, err := handleLearningContextWithReviewBatchStarter(in, nil); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(product, filepath.FromSlash(rule.Target))
	if err := writeFileAtomicDurable(target, []byte("stale changed body\n")); err != nil {
		t.Fatal(err)
	}
	in.HookEventName = "UserPromptSubmit"
	in.HostTrusted = true
	result, err := handleUserPromptLearning(in)
	if err == nil || strings.Contains(result.Context, "AUTOMATIC NATIVE PREFLIGHT") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatalf("stale SHA became native authority: %v context=%s", err, result.Context)
	}
}

func TestHLP06UntrustedExternalHostCannotUse(t *testing.T) {
	product, settings, _, _ := n093NativeTypedFixture(t)
	state := os.Getenv(hookStateDirEnv)
	in := declareLearningTestSession(t, state, "gpt", "hlp06-untrusted", product)
	in.HookEventName = "SessionStart"
	if _, err := handleLearningContextWithReviewBatchStarter(in, nil); err != nil {
		t.Fatal(err)
	}
	in.HookEventName = "UserPromptSubmit"
	in.HostTrusted = false
	if response, handled, err := autoSelfLearningNativePromptPreflight(
		func() selfLearningOwner {
			owner, enabled, readErr := activeAirWorkerSelfLearningOwner()
			if readErr != nil || !enabled {
				t.Fatalf("owner: %v", readErr)
			}
			return owner
		}(), in); err != nil || handled || response != "" ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatalf("untrusted adapter acquired native use: handled=%t err=%v response=%s", handled, err, response)
	}
}

func TestHLP06DifferentSessionCannotRelabelNativeOutcome(t *testing.T) {
	product, settings, _, _ := n093NativeTypedFixture(t)
	state := os.Getenv(hookStateDirEnv)
	one := declareLearningTestSession(t, state, "gpt", "hlp06-owned-one", product)
	one.HookEventName = "SessionStart"
	if context, err := handleLearningContextWithReviewBatchStarter(one, nil); err != nil ||
		!strings.Contains(context.Context, "VERIFIED AUTOMATIC DELIVERY") {
		t.Fatalf("original session delivery failed: %v", err)
	}
	one.HookEventName = "UserPromptSubmit"
	one.HostTrusted = true
	if result, err := handleUserPromptLearning(one); err != nil ||
		!strings.Contains(result.Context, "AUTOMATIC NATIVE PREFLIGHT") {
		t.Fatalf("original native preflight failed: %v", err)
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("original native use count incorrect")
	}
	original := deliveryNativeReceipts(t, settings.RuntimeRoot)

	two := declareLearningTestSession(t, state, "gpt", "hlp06-foreign-two", product)
	two.HookEventName = "SessionStart"
	context, err := handleLearningContextWithReviewBatchStarter(two, nil)
	if err != nil || strings.Contains(context.Context, "VERIFIED AUTOMATIC DELIVERY") {
		t.Fatalf("second session relabeled original native result: err=%v", err)
	}
	two.HostTrusted = true
	two.HookEventName = "UserPromptSubmit"
	result, err := handleUserPromptLearning(two)
	if err != nil || strings.Contains(result.Context, "AUTOMATIC NATIVE PREFLIGHT") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 ||
		!reflect.DeepEqual(original, deliveryNativeReceipts(t, settings.RuntimeRoot)) {
		t.Fatalf("different session rewrote old signed outcome: %v", err)
	}

	// The original delivery can be read back again without minting another
	// result, even when a second session has already inspected the catalog.
	one.HookEventName = "SessionStart"
	again, err := handleLearningContextWithReviewBatchStarter(one, nil)
	if err != nil || !strings.Contains(again.Context, "VERIFIED AUTOMATIC DELIVERY") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("original same-session resume failed: %v", err)
	}
}

func TestHLP06PendingOriginalIntentCannotMoveToNewSession(t *testing.T) {
	product, settings, _, _ := n093NativeTypedFixture(t)
	state := os.Getenv(hookStateDirEnv)
	original := declareLearningTestSession(t, state, "gpt", "hlp06-recovery-owner", product)
	original.HookEventName = "SessionStart"
	if _, err := handleLearningContextWithReviewBatchStarter(original, nil); err != nil {
		t.Fatal(err)
	}
	original.HookEventName = "UserPromptSubmit"
	original.HostTrusted = true
	if result, err := handleUserPromptLearning(original); err != nil ||
		!strings.Contains(result.Context, "AUTOMATIC NATIVE PREFLIGHT") {
		t.Fatalf("native preflight fixture failed: %v", err)
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("missing source event")
	}
	// Deliberately simulate an interruption before the final receipt is
	// durable, by removing ONLY this disposable test fixture's result file.
	results := filepath.Join(settings.RuntimeRoot, "native-preflight-results")
	children, err := os.ReadDir(results)
	if err != nil || len(children) != 1 {
		t.Fatalf("expected exactly one disposable result: %v", err)
	}
	if err := os.Remove(filepath.Join(results, children[0].Name())); err != nil {
		t.Fatal(err)
	}

	other := declareLearningTestSession(t, state, "gpt", "hlp06-not-owner", product)
	other.HookEventName = "SessionStart"
	context, err := handleLearningContextWithReviewBatchStarter(other, nil)
	if err == nil || strings.Contains(context.Context, "VERIFIED AUTOMATIC DELIVERY") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("unfinished original action was reassigned to another session: %v", err)
	}
	// The original authenticated delivery safely finishes its existing
	// intent/event and restores the missing final result without new use.
	result, err := handleUserPromptLearning(original)
	if err != nil || !strings.Contains(result.Context, "AUTOMATIC NATIVE PREFLIGHT") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("original authenticated retry did not reconcile intent: %v", err)
	}
	if entries, err := os.ReadDir(results); err != nil || len(entries) != 1 {
		t.Fatalf("original result was not read back/recovered: %v", err)
	}
}

func TestHLP06LegacySignedNativeUseDoesNotMintGPTDelivery(t *testing.T) {
	product, settings, args, _ := n093NativeTypedFixture(t)
	rc, output := captureLoopOutput(t, func() int { return cmdValidate(args[1:]) })
	if rc != 2 || !strings.Contains(output, "deferred_missing_configuration") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("baseline native typed use failed: rc=%d", rc)
	}
	// Historical typed native use was already signed WITHOUT any GPT
	// session context. Fresh SessionStart must still deliver ordinary
	// learning guidance, but can neither steal that use nor forge its effect.
	state := os.Getenv(hookStateDirEnv)
	in := declareLearningTestSession(t, state, "gpt", "hlp06-legacy-signed-native", product)
	in.HookEventName = "SessionStart"
	got, err := handleLearningContextWithReviewBatchStarter(in, nil)
	if err != nil || strings.Contains(got.Context, "VERIFIED AUTOMATIC DELIVERY") ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("legacy signed native outcome was relabeled or blocked normal context: %v", err)
	}
	nativePreflightDeferred.Swap(false)
}
