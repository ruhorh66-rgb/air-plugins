package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// All edits here target disposable test fixtures. A model-authored
// PostToolUse payload or an edited events.jsonl cannot refresh a signed result.
func TestN093ChangedModuleEventCannotBeCreditedOnReplay(t *testing.T) {
	_, settings, args, _ := n093NativeTypedFixture(t)
	result, err := nativePreflightGuard(args)
	if err != nil || !result.Deferred {
		t.Fatalf("initial native action: %+v %v", result, err)
	}
	before, err := os.ReadFile(result.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	eventsPath := filepath.Join(settings.RuntimeRoot, "events.jsonl")
	records, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(records), []byte("\n"))
	tampered := false
	for i, raw := range lines {
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			t.Fatal(err)
		}
		if event["kind"] != "procedure_used" {
			continue
		}
		event["outcome"] = "forged_pass"
		lines[i], err = json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		tampered = true
	}
	if !tampered {
		t.Fatal("fixture did not create a native use event")
	}
	if err := os.WriteFile(eventsPath, append(bytes.Join(lines, []byte("\n")), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if again, err := nativePreflightGuard(args); err == nil || again.Deferred {
		t.Fatalf("tampered module event falsely replayed as valid: %+v %v", again, err)
	}
	after, err := os.ReadFile(result.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("tampered journal changed original signed terminal receipt")
	}
}

func TestN093FinalizationRejectsMissingUseEvent(t *testing.T) {
	_, settings, args, _ := n093NativeTypedFixture(t)
	result, err := nativePreflightGuard(args)
	if err != nil || !result.Deferred {
		t.Fatalf("native action not completed: %+v %v", result, err)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("selector: %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("original run missing: %v", err)
	}
	rule, sourceEvent, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible {
		t.Fatalf("reviewed applied rule missing: %v", err)
	}

	journal := filepath.Join(settings.RuntimeRoot, "events.jsonl")
	raw, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	keep := make([][]byte, 0, len(lines))
	removed := 0
	for _, line := range lines {
		var event struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		if event.Kind == "procedure_used" {
			removed++
			continue
		}
		keep = append(keep, line)
	}
	if removed != 1 {
		t.Fatalf("fixture expected one use, got %d", removed)
	}
	if err := os.WriteFile(journal, append(bytes.Join(keep, []byte("\n")), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	resultPath := result.ReceiptPath
	if err := os.Remove(resultPath); err != nil {
		t.Fatal(err)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	intentPath := filepath.Join(settings.RuntimeRoot, "native-preflight-uses", filepath.Base(resultPath))
	path, valid, err := finalizeN093TypedPreflightResult(owner, prior, rule, sourceEvent, intentPath, key, args)
	if err != nil || valid || path != "" {
		t.Fatalf("missing module event confirmed native use: path=%s valid=%v err=%v", path, valid, err)
	}
	if _, err := os.Stat(resultPath); !os.IsNotExist(err) {
		t.Fatal("missing event created a terminal result")
	}
}

func TestN093FinalizationRejectsConfigChangedAfterUse(t *testing.T) {
	product, settings, args, _ := n093NativeTypedFixture(t)
	result, err := nativePreflightGuard(args)
	if err != nil || !result.Deferred {
		t.Fatalf("native action not completed: %+v %v", result, err)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("selector: %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("original run missing: %v", err)
	}
	rule, eventID, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible {
		t.Fatalf("review/ledger missing: %v", err)
	}
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	intent := filepath.Join(settings.RuntimeRoot, "native-preflight-uses", filepath.Base(result.ReceiptPath))
	_, valid, err := finalizeN093TypedPreflightResult(owner, prior, rule, eventID, intent, key, args)
	if err == nil || valid {
		t.Fatalf("stale OS precondition revalidated a prior result: valid=%v err=%v", valid, err)
	}
}

func TestN093PlanFingerprintTracksMissingPresentAndChangedBytes(t *testing.T) {
	root := t.TempDir()
	absent, err := n093NativePlanFingerprint(root)
	if err != nil || absent != "absent" {
		t.Fatalf("absent PLAN not measured: %q %v", absent, err)
	}
	plan := filepath.Join(root, "PLAN.md")
	if err := os.WriteFile(plan, []byte("# original\\n"), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := n093NativePlanFingerprint(root)
	if err != nil || first == absent {
		t.Fatalf("new PLAN not detected: %q %v", first, err)
	}
	if err := os.WriteFile(plan, []byte("# changed\\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second, err := n093NativePlanFingerprint(root)
	if err != nil || second == first {
		t.Fatalf("PLAN byte change not detected: %q %v", second, err)
	}
}

func TestN093FinalizationRejectsPLANChangedAfterSignedIntent(t *testing.T) {
	product, settings, args, _ := n093NativeTypedFixture(t)
	result, err := nativePreflightGuard(args)
	if err != nil || !result.Deferred {
		t.Fatalf("genuine native use not recorded: %+v %v", result, err)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("selector absent: %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("original run unavailable: %v", err)
	}
	rule, sourceEvent, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible {
		t.Fatalf("current reviewed applied origin unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(product, "PLAN.md"), []byte("# modified after use"), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	intent := filepath.Join(settings.RuntimeRoot, "native-preflight-uses", filepath.Base(result.ReceiptPath))
	_, valid, err := finalizeN093TypedPreflightResult(owner, prior, rule, sourceEvent, intent, key, args)
	if err == nil || valid {
		t.Fatalf("changed PLAN accepted by pending signed intent: valid=%v err=%v", valid, err)
	}
}

func TestN093FinalizationChecksHMACSignedTargetNotLearningOwnerConfig(t *testing.T) {
	// Production R04 keeps a valid owner run-config.json, while the one-shot
	// HMAC-bound disposable validation target correctly lacks that file.
	owner := t.TempDir()
	if err := os.WriteFile(filepath.Join(owner, "run-config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "isolated-native-validation")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	key := bytes.Repeat([]byte{0x42}, ownCLIKeyLength)
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	argv := []string{"validate", "-product", target, "-json"}
	prior := ownCLIFailureReceipt{
		Schema: ownCLIFailureSchema, Action: "validate", ExitCode: 2,
		CauseCode:     "validate-run-config-missing",
		InvocationMAC: ownCLIInvocationMAC(key, argv),
	}
	if err := n093RecheckSignedDeferredTarget(key, prior, argv); err != nil {
		t.Fatalf("valid signed scratch target was rejected because owner has config: %v", err)
	}
	// A different empty scratch must NOT inherit the signed original cause.
	other := t.TempDir()
	wrong := []string{"validate", "-product", other, "-json"}
	if err := n093RecheckSignedDeferredTarget(key, prior, wrong); err == nil {
		t.Fatal("different missing-config scratch bypassed original argv HMAC")
	}
	// The original scratch now has a config, so its original cause is stale.
	if err := os.WriteFile(filepath.Join(target, "run-config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := n093RecheckSignedDeferredTarget(key, prior, argv); err == nil {
		t.Fatal("stale missing-config precondition allowed a terminal use claim")
	}
	if err := os.Remove(filepath.Join(target, "run-config.json")); err != nil {
		t.Fatal(err)
	}
	forged := prior
	forged.InvocationMAC = strings.Repeat("0", len(prior.InvocationMAC))
	if err := n093RecheckSignedDeferredTarget(key, forged, argv); err == nil {
		t.Fatal("forged CLI invocation MAC accepted as original native operation")
	}
}
