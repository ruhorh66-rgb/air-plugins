package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeRoutePreflightReadinessIsNotModelSuccess(t *testing.T) {
	ready := claudeRoutePreflight{
		Schema:       "air-worker.claude-route-preflight/v1",
		ToolResolved: true, ToolRule: "PATH",
		ProfileKnown: true, OAuthSource: "user_env",
		HeadroomReady: true, HeadroomVersion: "test",
		ReviewerModel: "claude-sonnet-5-5", ReviewerEffort: "low",
		ModelCall: "NOT_RUN",
	}
	got, rc := evaluateClaudeRouteReadiness(ready)
	if rc != 0 || got.Status != "READY_TO_TRY_NATIVE_REVIEW" {
		t.Fatalf("readiness is not proven despite healthy native inputs: rc=%d status=%s", rc, got.Status)
	}
	if got.ModelCall != "NOT_RUN" {
		t.Fatalf("preflight incorrectly claims completed model call: %q", got.ModelCall)
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "CLAUDE_CODE_OAUTH_TOKEN") || strings.Contains(string(b), "test-oauth-secret") {
		t.Fatalf("preflight JSON leaked an OAuth identifier or secret: %s", b)
	}
	for _, mutate := range []func(*claudeRoutePreflight){
		func(x *claudeRoutePreflight) { x.ToolResolved = false },
		func(x *claudeRoutePreflight) { x.ProfileKnown = false },
		func(x *claudeRoutePreflight) { x.OAuthSource = "none_observed" },
		func(x *claudeRoutePreflight) { x.HeadroomReady = false },
	} {
		sample := ready
		mutate(&sample)
		checked, code := evaluateClaudeRouteReadiness(sample)
		if code != 2 || checked.Status != "HOLD_NOT_PROVEN" {
			t.Fatalf("unproven input accepted: %+v rc=%d", checked, code)
		}
	}
}

func TestNativeSessionStartGuidancePreservesExistingContext(t *testing.T) {
	before := hookResult{Context: "learned skill SHA verified"}
	startup := hookResult{Context: "curator skill loaded"}
	after := withNativeRouteStartupContext(before, startup)
	for _, need := range []string{
		"curator skill loaded", "learned skill SHA verified",
		"AirWorker authorization", "Two parallel Codex subagents",
		"No Codex App Server", "release-slice bypass",
	} {
		if !strings.Contains(after.Context, need) {
			t.Fatalf("startup lost %q: %s", need, after.Context)
		}
	}
	if strings.Index(after.Context, startup.Context) > strings.Index(after.Context, nativeAirWorkerStartupGuidance) {
		t.Fatal("curator startup guidance must precede native route instructions")
	}
	if after.Block {
		t.Fatal("static SessionStart information unexpectedly blocks a session")
	}
}

func TestToolPreflightCannotRunOnCodex(t *testing.T) {
	if code := cmdTool([]string{"-which", "codex", "-preflight"}); code != 2 {
		t.Fatalf("invalid model-specific preflight returned %d, want 2", code)
	}
}
