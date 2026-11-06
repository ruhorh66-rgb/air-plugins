package main

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		"AirWorker authorization", "CODING ROUTE restored",
		"Two parallel read-only subagents plus leader are OPTIONAL",
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

func TestN134SessionStartToolOutcomeTruthIsDelivered(t *testing.T) {
	before := hookResult{Context: "existing learning procedure"}
	startup := hookResult{Context: "trusted curator instructions"}
	got := withNativeRouteStartupContext(before, startup)
	for _, required := range []string{
		"HOST_DENIED_PRE_EXECUTION",
		"not a server outage or failed Go test",
		"request_id, PID, exit code UNKNOWN",
		"Do not retry the refused operation",
		"Continue only independent authorized actions",
		"RUNNING/UNKNOWN until its terminal receipt",
		"exit 0 alone is not task read or judge PASS",
		"Host hook rules apply only after verified delivery",
	} {
		if !strings.Contains(got.Context, required) {
			t.Errorf("native SessionStart guidance lacks %q", required)
		}
	}
	if !strings.HasPrefix(got.Context, startup.Context) {
		t.Fatal("curator-provided session context was overwritten")
	}
	if !strings.Contains(got.Context, before.Context) || got.Block {
		t.Fatal("tool outcome guidance must preserve previous context without new blocking power")
	}
}

func TestN134WoodySkillIncludesToolOutcomeBoundary(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "skills", "woody", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, required := range []string{
		"HOST_DENIED_PRE_EXECUTION",
		"NOT_CHECKED_BY_THIS_CALL",
		"EXECUTION_UNKNOWN",
		"NOT_RUN",
		"Не повторяй это же отклонённое действие",
		"разрешённые **независимые** шаги",
		"факт доставки хука **не предполагается**",
	} {
		if !strings.Contains(text, required) {
			t.Errorf("packaged Woody skill has no safety-boundary guidance %q", required)
		}
	}
}

func TestToolPreflightCannotRunOnCodex(t *testing.T) {
	if code := cmdTool([]string{"-which", "codex", "-preflight"}); code != 2 {
		t.Fatalf("invalid model-specific preflight returned %d, want 2", code)
	}
}
