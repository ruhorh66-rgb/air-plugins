package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN040UnsafeHelpRequiresExactRootAndNeverCallsPlanner(t *testing.T) {
	if !n040RequestedUnsafeHelp(n040HelpArgs("R:/fixture"), "R:/fixture") {
		t.Fatal("exact narrow unsupported help was not recognized")
	}
	for _, input := range [][]string{
		{"release", "-help", "extra"}, {"release"}, {"node", "release"},
		{"release", "-dry-run"}, {"release", "--help"}, {"migrate", "-help"},
		{"plan", "-product", "R:/fixture", "-dry-run", "release", "-help"},
		{"plan", "-product", "R:/other", "release", "-help"},
	} {
		if n040RequestedUnsafeHelp(input, "R:/fixture") {
			t.Fatalf("unsafe broad origin matcher accepted %#v", input)
		}
	}
	root := t.TempDir()
	old := dispatchPlannerModel
	modelCalls := 0
	dispatchPlannerModel = func(string, string, runnerSpec, string, sessionScope) int {
		modelCalls++
		return 92
	}
	defer func() { dispatchPlannerModel = old; planSyntaxOnly.Store(false) }()
	code := run([]string{"plan", "-product", root, "release", "-help"})
	if code != 2 || !planSyntaxOnly.Load() || modelCalls != 0 {
		t.Fatalf("unsupported syntax lost fail-closed property: rc=%d syntax=%t model_calls=%d",
			code, planSyntaxOnly.Load(), modelCalls)
	}
	for _, name := range []string{".woody", "PLAN.proposed.md", "n040-help-opportunities"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("wrong-root syntax captured or mutated %s: %v", name, err)
		}
	}
}

func TestN040HMACBindsNativeActorProductAndNoUseClaim(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	actorSID, actorErr := n040CurrentActorSID()
	processInstance, instanceErr := n040CurrentProcessInstance()
	if actorErr != nil || instanceErr != nil {
		t.Fatal("native process identity unavailable")
	}
	fields := map[string]string{
		"schema": n040OpportunitySchema,
		"id":     "N040-HELP-0123456789abcdef0123456789abcdef",
		"action": n040HelpAction,
		"kind":   "native_unsupported_cli_help_attempt",
		"state":  "PARSER_REJECTED_NO_MODEL", "parser_rc": "2",
		"principal":           "air-worker-cli",
		"current_actor_sid":   actorSID,
		"process_instance_id": processInstance,
		"product_id":          "air-worker",
		"product_root":        "R:/factual/product",
		"runtime_root":        "R:/factual/runtime",
		"owner_config_sha256": strings.Repeat("a", 64),
		"argv_hmac":           ownCLIInvocationMAC(key, n040HelpArgs("R:/factual/product")),
		"binary_sha256":       strings.Repeat("b", 64),
		"process_id":          "123",
		"observed_at_utc":     "2026-10-21T00:00:00Z",
		"learning_use":        "NOT_PROVEN", "saved_paid_model_calls": "NOT_PROVEN",
	}
	mac := n040OpportunityMAC(key, fields)
	if len(mac) != 64 || !n040IDValid(fields["id"]) {
		t.Fatal("invalid native proof contract")
	}
	for _, item := range []struct{ key, value string }{
		{"id", "N040-HELP-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		{"action", "air-worker.fake/v1"},
		{"state", "SUCCESS"},
		{"principal", "model-prose"},
		{"current_actor_sid", "forged"},
		{"process_instance_id", strings.Repeat("0", 32)},
		{"product_root", "R:/foreign/product"},
		{"runtime_root", "R:/foreign/runtime"},
		{"owner_config_sha256", strings.Repeat("c", 64)},
		{"argv_hmac", strings.Repeat("0", 64)},
		{"binary_sha256", strings.Repeat("f", 64)},
		{"process_id", "999"},
		{"learning_use", "PASS"},
		{"saved_paid_model_calls", "1"},
	} {
		altered := make(map[string]string, len(fields))
		for k, v := range fields {
			altered[k] = v
		}
		altered[item.key] = item.value
		if mac == n040OpportunityMAC(key, altered) {
			t.Fatalf("signed opportunity ignored tampered %s", item.key)
		}
	}
	if n040IDValid("../../escape") || n040IDValid("N040-HELP-nope") {
		t.Fatal("untrusted filename escaped strict nonce contract")
	}
	if _, err := json.Marshal(fields); err != nil {
		t.Fatal(err)
	}
}

func TestN040IsolatedNativeOpportunityReceiptAndTamper(t *testing.T) {
	root, runtimeRoot := t.TempDir(), t.TempDir()
	owner := selfLearningOwner{
		Selector: airWorkerSelfLearningSelector{
			ProductRoot: root, RuntimeRoot: runtimeRoot, ConfigSHA256: strings.Repeat("a", 64),
		},
		Settings: sharedLearningSettings{ProductID: "air-worker", RuntimeRoot: runtimeRoot},
	}
	// The signed parser result is not a declaration supplied by the caller.
	// Neither an extra flag nor a non-rejected invocation may mint it.
	badArgs := append(n040HelpArgs(root), "-dry-run")
	if _, _, badErr := n040RuntimeOpportunity(owner, badArgs, 2); badErr == nil {
		t.Fatal("extra actual argv was falsely signed as the canonical help attempt")
	}
	if _, _, badErr := n040RuntimeOpportunity(owner, n040HelpArgs(root), 0); badErr == nil {
		t.Fatal("successful parser result was falsely signed as rejected")
	}
	proof, ref, err := n040RuntimeOpportunity(owner, n040HelpArgs(root), 2)
	if err != nil {
		t.Fatalf("native isolated signed receipt: %v", err)
	}
	id := proof["id"]
	path, err := n040OpportunityPath(owner, id)
	if err != nil || !strings.HasPrefix(ref, filepath.ToSlash(path)+"#sha256=") {
		t.Fatalf("opportunity URI identity invalid: %s %v", ref, err)
	}
	if proof["state"] != "PARSER_REJECTED_NO_MODEL" || proof["learning_use"] != "NOT_PROVEN" {
		t.Fatal("opportunity source claimed positive learning effect")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sha := learnSHA(raw)
	got, err := n040VerifyOpportunity(owner, id, sha)
	if err != nil || got["mac"] != proof["mac"] {
		t.Fatalf("verified signed receipt did not read back: %v", err)
	}
	if _, err := n040VerifyOpportunity(owner, id, strings.Repeat("0", 64)); err == nil {
		t.Fatal("stale receipt hash accepted")
	}
	var edit map[string]string
	if err := json.Unmarshal(raw, &edit); err != nil {
		t.Fatal(err)
	}
	edit["principal"] = "forged-actor"
	tampered, _ := json.Marshal(edit)
	tampered = append(tampered, '\n')
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := n040VerifyOpportunity(owner, id, learnSHA(tampered)); err == nil {
		t.Fatal("new digest of a forged native opportunity bypassed HMAC")
	}
}
