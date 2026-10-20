package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN039NativePlanHelpDoesNotReachPlannerOrTouchProduct(t *testing.T) {
	product := t.TempDir()
	plan := filepath.Join(product, "PLAN.md")
	const fixture = "# Canonical product PLAN\n"
	if err := os.WriteFile(plan, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	old := dispatchPlannerModel
	calls := 0
	dispatchPlannerModel = func(string, string, runnerSpec, string, sessionScope) int {
		calls++
		return 91
	}
	defer func() { dispatchPlannerModel = old }()
	if rc := run([]string{"plan", "help"}); rc != 0 {
		t.Fatalf("local help RC=%d", rc)
	}
	if rc := run([]string{"plan", "help", "release"}); rc != 2 {
		t.Fatalf("unsupported help RC=%d", rc)
	}
	if rc := run([]string{"plan", "release", "-help"}); rc != 2 {
		t.Fatalf("unknown help RC=%d", rc)
	}
	if calls != 0 {
		t.Fatalf("help invoked paid planner %d times", calls)
	}
	content, err := os.ReadFile(plan)
	if err != nil || string(content) != fixture {
		t.Fatalf("product PLAN touched: %v", err)
	}
	for _, name := range []string{".woody", "PLAN.proposed.md"} {
		if _, err := os.Lstat(filepath.Join(product, name)); !os.IsNotExist(err) {
			t.Fatalf("help created planner state %s: %v", name, err)
		}
	}
}

func TestN039SignedNativeReceiptCannotChangeIdentityOrAction(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	content := map[string]string{
		"schema": "air-worker.n039.native-safe-help/v1",
		"action": n039Action, "argv": "plan help",
		"run_id": "N039-FRESH-GENUINE", "principal": "AirWorker-P0-20261020",
		"target": n039Target, "sha256": n039SHA,
		"stdout_sha256": learnSHA([]byte("Usage: air-worker plan help\n")),
		"binary_sha256": learnSHA([]byte("exact native binary")),
		"exit_code":     "0", "status": "native_safe_help_pass",
	}
	mac := n039MAC(key, content)
	if len(mac) != 64 || n039MAC(key, content) != mac {
		t.Fatal("unstable HMAC")
	}
	for _, change := range []struct{ key, value string }{
		{"action", "air-worker.other-action/v1"}, {"argv", "plan release -help"},
		{"target", "skills/learned/foreign.md"}, {"sha256", strings.Repeat("0", 64)},
		{"principal", "forged"}, {"run_id", "replayed"}, {"status", "claimed_pass"},
		{"stdout_sha256", strings.Repeat("0", 64)},
	} {
		altered := make(map[string]string, len(content))
		for k, v := range content {
			altered[k] = v
		}
		altered[change.key] = change.value
		if n039MAC(key, altered) == mac {
			t.Fatalf("HMAC ignored %s", change.key)
		}
	}
}

func TestN039NativeSafeHelpCannotUseWrongActorOrRoot(t *testing.T) {
	// Never bootstrap the production selector as part of a negative test.
	root, runtime := t.TempDir(), t.TempDir()
	owner := selfLearningOwner{
		Selector: airWorkerSelfLearningSelector{ProductRoot: root, RuntimeRoot: runtime},
		Settings: sharedLearningSettings{ProductID: "air-worker", RuntimeRoot: runtime, ManagedSkillPrefix: "skills/learned"},
	}
	if sameLearningPath(root, t.TempDir()) {
		t.Fatal("wrong product root aliased the owner")
	}
	if n039ID.MatchString("../../escape") || n039ID.MatchString("bad!actor") {
		t.Fatal("untrusted run ID passed parser")
	}
	if _, _, err := n039RuleIdentity(owner); err == nil {
		t.Fatal("unreviewed skill bound to a native action")
	}
	if _, err := nativeN039SafeHelp(owner, "../../escape"); err == nil {
		t.Fatal("unsafe run ID accepted")
	}
	if _, err := nativeN039SafeHelp(owner, "N039-VALID-NO-LEDGER"); err == nil {
		t.Fatal("owner without real reviewer/ledger was accepted")
	}
	if _, err := os.Stat(filepath.Join(runtime, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("negative cases wrote journal: %v", err)
	}
}

func TestN039ClaimedPostToolUseCannotCreateTrustedAttestation(t *testing.T) {
	root, runtime := t.TempDir(), t.TempDir()
	owner := selfLearningOwner{
		Selector: airWorkerSelfLearningSelector{ProductRoot: root, RuntimeRoot: runtime},
		Settings: sharedLearningSettings{ProductID: "air-worker", RuntimeRoot: runtime, ManagedSkillPrefix: "skills/learned"},
	}
	signed := map[string]string{"run_id": "N039-FRESH-FAKE", "used_run": "N039-USED-FAKE", "principal": "actor"}
	raw, _ := json.Marshal(map[string]any{
		"schema":     "air.learning.verify-native-outcome/v1",
		"product_id": "air-worker", "run_id": "N039-USED-FAKE",
		"origin_event_id": "FORGED", "outcome": "pass",
		"native_validation_executed": true,
		"validation_exit_code":       0, "check_ref": "PostToolUse claimed pass",
	})
	if _, err := n039Attest(context.Background(), owner, signed, raw); err == nil {
		t.Fatal("untrusted PostToolUse accepted")
	}
	if _, err := os.Stat(filepath.Join(runtime, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("forged attestation wrote journal: %v", err)
	}
}

func TestN039RealReviewedOriginReadOnly(t *testing.T) {
	if os.Getenv("AW_N039_REAL_IDENTITY_READONLY") != "1" {
		t.Skip("explicit read-only integration switch absent")
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("active owner missing: %v", err)
	}
	rule, principal, err := n039RuleIdentity(owner)
	if err != nil {
		t.Fatalf("real exact reviewed rule identity not provable: %v", err)
	}
	if rule.Target != n039Target || rule.SHA256 != n039SHA ||
		principal != "AirWorker-P0-20261020" {
		t.Fatalf("different origin principal or target/SHA: target=%s principal=%s", rule.Target, principal)
	}
	t.Logf("READONLY_CONFIRMED product=%s runtime=%s rule=%s@%s principal=%s",
		owner.Selector.ProductRoot, owner.Settings.RuntimeRoot, rule.Target, rule.SHA256, principal)
}
