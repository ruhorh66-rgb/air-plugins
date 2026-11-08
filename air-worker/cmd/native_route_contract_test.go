package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN129NativeShellGuardBlocksWrongModels(t *testing.T) {
	denied := []string{
		"codex exec --json -m gpt-6-sol",
		"codex app-server --listen stdio://",
		"& \"R:\\-4-\\npm-global\\codex.cmd\" exec --json",
		"'C:\\Tools\\codex.exe' app-server",
		"claude -p 'read this file'",
		"& \"C:\\Users\\u\\.local\\bin\\claude.exe\" --print --model claude-sonnet-5-5",
		"claude auth status",
		"git status; codex exec --json",
	}
	for _, line := range denied {
		denied, reason := classifyBypass(line, nil)
		if !denied || reason == "" {
			t.Errorf("wrong provider route not rejected: %q (reason=%q)", line, reason)
		}
	}
	allowed := []string{
		"air-worker orchestrate -product . -subagents 2",
		"air-worker plan-review -product . -executor codex",
		"air-worker tool -which claude -preflight -json",
		"air-worker executor list -product . -json",
		"air-worker version",
		"codex login status",
		"codex --version",
		"claude --version",
		"git grep -n 'codex exec' -- cmd",
		"Write-Output 'claude auth status'",
		"",
	}
	for _, line := range allowed {
		if yes, reason := classifyBypass(line, nil); yes {
			t.Errorf("safe native or diagnostic route rejected: %q (%s)", line, reason)
		}
	}
}

func TestN129HookRejectsAIRCommanderDirectModelAndAllowsNative(t *testing.T) {
	for _, example := range []struct {
		tool, command string
		denied        bool
	}{
		{"PowerShell", "codex exec --json", true},
		{"Bash", "claude -p prompt", true},
		{"mcp__AIR_Commander_Test__start_process", "& \"C:\\Tools\\codex.cmd\" exec --json", true},
		{"mcp__AIR_Commander__start_process", "claude auth status", true},
		{"mcp__AIR_Commander_Test__start_process", "air-worker orchestrate -subagents 2", false},
		{"Bash", "air-worker plan-review -executor codex", false},
		{"mcp__AIR_Commander_Test__read_file", "codex exec --json", false},
	} {
		raw, err := json.Marshal(map[string]any{"command": example.command, "node": "SRVLM01"})
		if err != nil {
			t.Fatal(err)
		}
		outcome, err := handlePreToolUseBypassGuard(hookInput{ToolName: example.tool, ToolInput: raw})
		if err != nil {
			t.Fatalf("%s: unexpected guard error: %v", example.tool, err)
		}
		if outcome.Block != example.denied {
			t.Fatalf("%s %q: block=%v want %v (%s)", example.tool, example.command, outcome.Block, example.denied, outcome.Reason)
		}
	}
}

func TestN129BinaryPolicyBlocksSequentialAndWrongReviewer(t *testing.T) {
	cfg := executorTestConfig()
	cfg.Orchestration = orchestrationSpec{
		Enabled: true, Subagents: 2,
		RequireNativeSubagents: true, RequireAnthropicReviewer: true,
	}
	if err := validateModelPolicy(cfg); err != nil {
		t.Fatalf("valid product policy rejected: %v", err)
	}
	for _, tc := range []struct {
		orchestrate bool
		count       int
		denied      bool
	}{
		{true, 2, false}, {true, 0, false},
		// Hermes calls native loop without CLI overrides, using canonical
		// enabled=true/subagents=2; this must be accepted.
		{false, 0, false}, {false, 2, false},
		{true, 1, true}, {true, 3, true},
		{false, 1, true},
	} {
		e := requireNativeLoopInvocation(cfg, tc.orchestrate, tc.count)
		if (e != nil) != tc.denied {
			t.Fatalf("orchestrate=%v count=%d err=%v", tc.orchestrate, tc.count, e)
		}
	}
	for _, c := range []struct {
		executor, reviewer runnerSpec
		blocked            bool
	}{
		{runnerSpec{Kind: "codex", Model: "gpt-6-sol"}, runnerSpec{Kind: "claude", Model: "claude-sonnet-5-5", Effort: "low"}, false},
		{runnerSpec{Kind: "codex"}, runnerSpec{Kind: "codex", Model: "gpt-6-sol", Effort: "low"}, true},
		{runnerSpec{Kind: "claude"}, runnerSpec{Kind: "codex", Model: "gpt-6-sol", Effort: "low"}, true},
		{runnerSpec{Kind: "chatgpt"}, runnerSpec{Kind: "claude", Model: "claude-sonnet-5-5", Effort: "low"}, true},
		{runnerSpec{Kind: "codex"}, runnerSpec{Kind: "claude", Model: "gpt-6-sol", Effort: "low"}, true},
	} {
		e := enforceIndependentAnthropicReviewer(cfg, c.executor, c.reviewer)
		if (e != nil) != c.blocked {
			t.Fatalf("reviewer route: executor=%+v reviewer=%+v err=%v", c.executor, c.reviewer, e)
		}
	}
	old := cfg
	old.Orchestration.Subagents = 1
	if validateModelPolicy(old) == nil {
		t.Fatal("one agent cannot satisfy contract")
	}
	other := cfg
	other.Orchestration = orchestrationSpec{}
	if err := requireNativeLoopInvocation(other, false, 1); err != nil {
		t.Fatalf("other products must retain legacy single executor: %v", err)
	}
}

func TestN129ExecutorDispatchIsDeniedBeforeSessionAndProcess(t *testing.T) {
	root := t.TempDir()
	cfg := executorTestConfig()
	cfg.Orchestration = orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	// Invalid session and task arguments would normally return other errors;
	// code=2 proves the policy denied before either could launch a child.
	stderr, code := captureStderr(t, func() int {
		return cmdExecutorDispatch([]string{"-product", root, "-class", "complex", "-principal", "gpt", "-session-key", "unregistered", "-task-file", "missing.md"})
	})
	if code != 2 || !strings.Contains(stderr, "native parallel subagents") {
		t.Fatalf("dispatch not fail-closed before child: code=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".woody")); !os.IsNotExist(err) {
		t.Fatalf("single dispatch created a runtime job: %v", err)
	}
}

func TestN129ActiveHookEndToEndDeniesDirectAIRCommanderCodex(t *testing.T) {
	session := "n129-host-guard"
	hookTestState(t, session)
	event := map[string]any{
		"session_id": session, "hook_event_name": "PreToolUse",
		"tool_name":  "mcp__AIR_Commander_Test__start_process",
		"tool_input": map[string]any{"node": "SRVLM01", "command": "codex exec --json -m gpt-6-sol"},
	}
	raw, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	withStdin(t, string(raw))
	out, code := captureStderr(t, func() int { return cmdHook([]string{"PreToolUse"}) })
	if code != 2 || !strings.Contains(out, "прямой Codex") {
		t.Fatalf("active host did not block bypass: code=%d stderr=%s", code, out)
	}
}

func TestN129InactiveSessionGetsStartupRouteBeforeDeclare(t *testing.T) {
	t.Setenv(hookStateDirEnv, t.TempDir())
	t.Setenv("AIR_WORKER_PLUGIN_ROOT", "")
	t.Setenv("CLAUDE_PLUGIN_ROOT", "")
	withStdin(t, `{"session_id":"n129-new-session","hook_event_name":"SessionStart","principal":"claude"}`)
	out, code := captureStdout(t, func() int { return cmdHook([]string{"SessionStart"}) })
	if code != 0 {
		t.Fatalf("new session startup failed: rc=%d output=%s", code, out)
	}
	if !strings.Contains(out, "AIRWORKER NATIVE START") || !strings.Contains(out, "CODING ROUTE approved") || !strings.Contains(out, "two-Codex subagent orchestration") {
		t.Fatalf("inactive new session did not receive install-owned route guidance: %s", out)
	}
}

func TestN129CanonicalPolicyCannotBeDowngradedViaConfigOverride(t *testing.T) {
	root := t.TempDir()
	cfg := executorTestConfig()
	cfg.Orchestration = orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}
	canonical := filepath.Join(root, "run-config.json")
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, raw, 0600); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(root, "unsafe-config.json")
	alternate := executorTestConfig()
	another, err := json.Marshal(alternate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, another, 0600); err != nil {
		t.Fatal(err)
	}
	if err := enforceCanonicalNativeRouteConfig(root, override); err == nil {
		t.Fatal("alternate config bypassed pinned native route")
	}
	if _, _, err := loadExecutorConfig(root, override); err == nil {
		t.Fatal("executor config accepted route downgrade")
	}
	if err := enforceCanonicalNativeRouteConfig(root, canonical); err != nil {
		t.Fatalf("canonical config rejected: %v", err)
	}
	if _, _, err := loadExecutorConfig(root, canonical); err != nil {
		t.Fatalf("managed canonical policy invalid: %v", err)
	}
	// A separate product with no managed policy keeps its native optional config behavior.
	otherRoot := t.TempDir()
	if err := enforceCanonicalNativeRouteConfig(otherRoot, override); err != nil {
		t.Fatalf("foreign product config overridden: %v", err)
	}
}

// LPR-approved two-Codex orchestrated coding must not be downgraded to a
// sequential/single executor dispatch even if a valid session is supplied.
func TestDevWork30MandatoryTwoCodexPatchAuthorsBlockSingleDispatch(t *testing.T) {
	cfg := loadReleaseModelPolicy(t)
	root := t.TempDir()
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	stderr, code := captureStderr(t, func() int {
		return cmdExecutorDispatch([]string{"-product", root, "-class", "complex",
			"-principal", "gpt", "-session-key", "n135-not-yet-declared",
			"-task-file", "missing.md"})
	})
	if code != 2 || !strings.Contains(stderr, "native parallel subagents") {
		t.Fatalf("sequential dispatch bypassed required two Codex subagents: code=%d stderr=%q", code, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, ".woody")); !os.IsNotExist(err) {
		t.Fatalf("undocumented model job started for rejected test session: %v", err)
	}
}

func TestN135HermesStructuredRunRestoresCanonicalCodingLadder(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "run-config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg runConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Orchestration.Enabled || cfg.Orchestration.Subagents != 2 ||
		!cfg.Orchestration.RequireNativeSubagents || !cfg.Orchestration.RequireAnthropicReviewer {
		t.Fatalf("Dev Work 30 two-Codex patch-author contract not active: %+v", cfg.Orchestration)
	}
	// Hermes register(ctx) -> air_worker(action=run) -> native AirWorker loop.
	// No flags are needed: canonical policy now mandates two Codex source-patch
	// authors and a single Codex writer with independent Anthropic acceptance.
	if err := requireNativeLoopInvocation(cfg, false, 0); err != nil {
		t.Fatalf("Hermes native coding route rejected: %v", err)
	}
	executor := runnerSpec{Kind: "codex", Model: "gpt-6-sol"}
	reviewer, _, err := cfg.SemanticJudge.Resolve(executor, activeProductReleaseVersion(".."))
	if err != nil {
		t.Fatal(err)
	}
	if err := enforceIndependentAnthropicReviewer(cfg, executor, reviewer); err != nil {
		t.Fatalf("Hermes coding route lost independent Claude reviewer: %v", err)
	}
}

func TestN129CodexAndHermesPluginStartupGuidanceUsesNativeKernel(t *testing.T) {
	codexManifest, err := os.ReadFile(filepath.Join("..", ".codex-plugin", "plugin.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Interface struct {
			DefaultPrompt string `json:"defaultPrompt"`
		} `json:"interface"`
	}
	if err := json.Unmarshal(codexManifest, &doc); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"air-worker orchestrate", "-subagents 2", "TWO native Codex code-writing workers", "workspace-write", "Anthropic Sonnet/Opus", "Standalone executor dispatch is forbidden"} {
		if !strings.Contains(doc.Interface.DefaultPrompt, required) {
			t.Fatalf("Codex plugin startup missing native route %q", required)
		}
	}
	hermesSkill, err := os.ReadFile(filepath.Join("..", ".hermes", "plugins", "air-worker", "skills", "operate-air-worker", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"action: run", "air-worker loop", "mandatory two Codex coding subagents", "unified source patch", "independent Claude", "-subagents 2"} {
		if !strings.Contains(string(hermesSkill), required) {
			t.Fatalf("Hermes skill missing native route %q", required)
		}
	}
}
