package main

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func n124TrustedUserPrompt(t *testing.T, f n124RephaseFixture) hookInput {
	t.Helper()
	state := t.TempDir()
	in := declareLearningTestSession(t, state, "gpt", "n124-test-worker", f.root)
	in.HookEventName = "UserPromptSubmit"
	in.HostTrusted = true
	in.Prompt = "да N124 " + f.requestID + " " + f.requestSHA +
		" " + f.authoritySHA + " " + f.request.Generation
	return in
}

func n124StubTrustedIssuer(t *testing.T) {
	t.Helper()
	old := planRephaseHookSource
	planRephaseHookSource = func() error { return nil }
	t.Cleanup(func() { planRephaseHookSource = old })
}

func TestN124TrustedIssuerMissingUserPromptFailsClosed(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	in := n124TrustedUserPrompt(t, f)
	in.HostTrusted = false
	n124StubTrustedIssuer(t)
	matched, err := capturePlanRephaseUserPromptGrant(f.root, in)
	if !matched || err == nil {
		t.Fatalf("model-supplied non-host prompt minted grant: matched=%v err=%v", matched, err)
	}
	if rc := cmdPlanRephaseApply(f.args()); rc != 3 {
		t.Fatalf("untrusted public apply rc=%d want HOLD", rc)
	}
	runtimeRoot, err := planRephaseGrantRuntime(f.root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(planRephaseGrantPath(runtimeRoot, f.requestID)); !os.IsNotExist(err) {
		t.Fatalf("untrusted prompt wrote native plan grant: %v", err)
	}
}

func TestN124TrustedIssuerRequiresHostProvenance(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	in := n124TrustedUserPrompt(t, f)
	old := planRephaseHookSource
	planRephaseHookSource = func() error { return errors.New("untrusted bridge") }
	t.Cleanup(func() { planRephaseHookSource = old })
	matched, err := capturePlanRephaseUserPromptGrant(f.root, in)
	if !matched || err == nil {
		t.Fatalf("synthetic host origin was accepted: %v %v", matched, err)
	}
	if rc := cmdPlanRephaseApply(f.args()); rc != 3 {
		t.Fatalf("untrusted host provenance applied native CAS: rc=%d", rc)
	}
}

func TestN124TrustedUserPromptExactGrantEnablesOneNativeRequestAndReplay(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	in := n124TrustedUserPrompt(t, f)
	n124StubTrustedIssuer(t)
	matched, err := capturePlanRephaseUserPromptGrant(f.root, in)
	if !matched || err != nil {
		t.Fatalf("trusted exact user prompt did not issue grant: %v %v", matched, err)
	}
	runtimeRoot, err := planRephaseGrantRuntime(f.root)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(planRephaseGrantPath(runtimeRoot, f.requestID))
	if err != nil {
		t.Fatal(err)
	}
	var grant planRephaseTrustedGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatal(err)
	}
	if grant.GrantMAC == "" || grant.RequestSHA256 != f.requestSHA ||
		grant.AuthoritySHA256 != f.authoritySHA || grant.Actor != "gpt-window:n124-test-worker" {
		t.Fatalf("exact host grant fields were lost: %+v", grant)
	}
	if rc := cmdPlanRephaseApply(f.args()); rc != 0 {
		t.Fatalf("default installed-candidate native verifier refused a valid fixture: rc=%d", rc)
	}
	if rc := cmdPlanRephaseApply(f.args()); rc != 0 {
		t.Fatalf("idempotent trusted native rephase replay failed rc=%d", rc)
	}
	if _, err := capturePlanRephaseUserPromptGrant(f.root, in); err == nil {
		t.Fatal("same user words minted a second immutable grant")
	}
}

func TestN124TrustedGrantCannotRebindRunIDActorGenerationOrMAC(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	in := n124TrustedUserPrompt(t, f)
	n124StubTrustedIssuer(t)
	if _, err := capturePlanRephaseUserPromptGrant(f.root, in); err != nil {
		t.Fatal(err)
	}
	runtimeRoot, err := planRephaseGrantRuntime(f.root)
	if err != nil {
		t.Fatal(err)
	}
	authBytes, err := os.ReadFile(f.authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	// Verification needs neither a test-only injected authority nor an
	// executable provider. The host-origin grant must match all exact fields.
	if err := defaultPlanRephaseAuthorityVerifier(f.root, f.request,
		planRephaseAuthority{
			Schema: planRephaseAuthoritySchema, Operation: "plan.rephase.apply", Approved: true,
			Repository: f.request.Repository, RemoteName: f.request.RemoteName,
			CanonicalRef: f.request.CanonicalRef, ExpectedHead: f.request.ExpectedHead,
			Generation: f.request.Generation, Actor: "gpt-window:n124-test-worker",
			NodeID: f.child.ID, ReleaseVersion: f.request.Release.Version,
			RequestSHA256: f.requestSHA,
		}, authBytes, "gpt-window:n124-test-worker", f.requestSHA, f.requestID); err != nil {
		t.Fatalf("valid exact grant failed: %v", err)
	}
	for name, mutate := range map[string]func(*planRephaseRequest, *string, *string){
		"generation": func(r *planRephaseRequest, actor, id *string) { r.Generation += "-new" },
		"actor":      func(r *planRephaseRequest, actor, id *string) { *actor = "gpt-window:foreign" },
		"request-id": func(r *planRephaseRequest, actor, id *string) { *id = "other-request-id" },
	} {
		req := f.request
		actor := "gpt-window:n124-test-worker"
		id := f.requestID
		mutate(&req, &actor, &id)
		if err := verifyPlanRephaseTrustedGrant(f.root, req, planRephaseAuthority{}, authBytes, actor, f.requestSHA, id); err == nil {
			t.Fatalf("forged %s acquired plan rephase authority", name)
		}
	}
	// An existing signed grant cannot survive turning off the original
	// declared session, even while its TTL and HMAC remain valid.
	id, err := parseIdentity("gpt", "n124-test-worker")
	if err != nil {
		t.Fatal(err)
	}
	statePath := sessionModePath(hookStateDir(), id)
	var state sessionModeState
	if err := readJSON(statePath, &state); err != nil {
		t.Fatal(err)
	}
	state.Enabled = false
	if err := writeStateJSON(statePath, state); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanRephaseTrustedGrant(f.root, f.request, planRephaseAuthority{},
		authBytes, "gpt-window:n124-test-worker", f.requestSHA, f.requestID); err == nil {
		t.Fatal("disabled original session still authorized N124 CAS")
	}
	state.Enabled = true
	if err := writeStateJSON(statePath, state); err != nil {
		t.Fatal(err)
	}
	path := planRephaseGrantPath(runtimeRoot, f.requestID)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var grant planRephaseTrustedGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatal(err)
	}
	grant.GrantMAC = strings.Repeat("0", 64)
	tampered, _ := json.Marshal(grant)
	if err := os.WriteFile(path, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanRephaseTrustedGrant(f.root, f.request, planRephaseAuthority{}, authBytes, "gpt-window:n124-test-worker", f.requestSHA, f.requestID); err == nil {
		t.Fatal("forged trusted grant MAC was accepted")
	}
}

func TestN124TrustedGrantExpiryAndMalformedPromptFailClosed(t *testing.T) {
	for _, prompt := range []string{"да N124", "да N124 x", "да N124 ../escape " + strings.Repeat("a", 64)} {
		_, _, _, _, matched, err := parsePlanRephaseUserApproval(prompt)
		if !matched || err == nil {
			t.Fatalf("malformed prompt accepted: %q", prompt)
		}
	}
	f := n124RephaseFixtureSetup(t)
	in := n124TrustedUserPrompt(t, f)
	n124StubTrustedIssuer(t)
	if _, err := capturePlanRephaseUserPromptGrant(f.root, in); err != nil {
		t.Fatal(err)
	}
	old := planRephaseNow
	planRephaseNow = func() time.Time { return time.Now().UTC().Add(25 * time.Hour) }
	t.Cleanup(func() { planRephaseNow = old })
	raw, err := os.ReadFile(f.authorityPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanRephaseTrustedGrant(f.root, f.request, planRephaseAuthority{}, raw,
		"gpt-window:n124-test-worker", f.requestSHA, f.requestID); err == nil {
		t.Fatal("expired human authorization revalidated")
	}
}
