package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n093TestHook(in hookInput, tool, input, response string) hookInput {
	in.HookEventName = "PostToolUse"
	in.ToolName = tool
	in.ToolInput = json.RawMessage(input)
	in.ToolResponse = json.RawMessage(response)
	return in
}

func TestN093NoUseWhenLoadedSkillChangesBeforeTrigger(t *testing.T) {
	selfRoot, settings, stateDir := selfLearningFixture(t)
	target := filepath.Join(t.TempDir(), "other-product")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "codex", "n093-stale-before", target)
	in.RunID = "n093-stale-before-run"
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	rec, _, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || len(rec.Loaded) != 1 {
		t.Fatalf("loaded context unavailable: %+v, found=%v err=%v", rec, found, err)
	}
	learned := filepath.Join(selfRoot, filepath.FromSlash(rec.Loaded[0].Target))
	if !currentSelfLearningRuleBytes(selfLearningOwner{Selector: airWorkerSelfLearningSelector{ProductRoot: selfRoot}, Settings: settings}, rec.Loaded[0]) {
		t.Fatal("original self learning bytes were not accepted")
	}
	if err := os.WriteFile(learned, []byte(selfLearningProcedure+"\n# altered outside ledger\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	unknown := n093TestHook(in, "mcp__AIR_Commander_Test__start_process",
		"{\"node\":\"SRVLM01\",\"command\":\"release mutation\"}",
		"{\"error\":\"EXECUTION_UNKNOWN\",\"pid\":4242,\"request_id\":\"n093-stale\"}")
	result, err := handlePostToolUseSelfLearning(unknown)
	if err != nil || result.Context != "" {
		t.Fatalf("stale loaded procedure claimed use/trigger: %+v %v", result, err)
	}
	if _, _, exists, err := readSelfLearningTriggerReceipt(in); err != nil || exists {
		t.Fatalf("stale procedure triggered: exists=%v err=%v", exists, err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("stale bytes emitted %d uses", got)
	}
}

func TestN093NoUseWhenLoadedSkillChangesAfterTrigger(t *testing.T) {
	selfRoot, settings, stateDir := selfLearningFixture(t)
	target := filepath.Join(t.TempDir(), "other-product")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "n093-stale-after", target)
	in.RunID = "n093-stale-after-run"
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	unknown := n093TestHook(in, "mcp__AIR_Commander_Test__start_process",
		"{\"node\":\"SRVLM01\",\"command\":\"release mutation\"}",
		"{\"error\":\"EXECUTION_UNKNOWN\",\"pid\":4242}")
	if _, err := handlePostToolUseSelfLearning(unknown); err != nil {
		t.Fatal(err)
	}
	if _, _, found, err := readSelfLearningTriggerReceipt(in); err != nil || !found {
		t.Fatalf("original trigger missing: found=%v err=%v", found, err)
	}
	rec, _, _, err := readSelfLearningContextReceipt(in)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(selfRoot, filepath.FromSlash(rec.Loaded[0].Target))
	if err := os.WriteFile(path, []byte(selfLearningProcedure+"\n# invalidated bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	diagnostic := n093TestHook(in, "mcp__AIR_Commander_Test__read_process_output",
		"{\"node\":\"SRVLM01\",\"pid\":4242}",
		"{\"status\":\"completed\",\"exit_code\":0,\"pid\":4242}")
	result, err := handlePostToolUseSelfLearning(diagnostic)
	if err != nil || strings.Contains(result.Context, "procedure_used") {
		t.Fatalf("stale SHA used by correlated diagnostics: %+v %v", result, err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("stale proof emitted %d uses", got)
	}
}

func TestN093RuleIdentityRejectsUnmanagedAndTamperedPaths(t *testing.T) {
	selfRoot, _, _ := selfLearningFixture(t)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("active fixture owner missing: %v", err)
	}
	owner.Selector.ProductRoot = selfRoot
	rule := selfLearningLoadedRule{Target: "skills/learned/execution-unknown-no-blind-retry-6b72186f.md"}
	bytes, err := os.ReadFile(filepath.Join(selfRoot, filepath.FromSlash(rule.Target)))
	if err != nil {
		t.Fatal(err)
	}
	rule.SHA256 = learnSHA(bytes)
	if !currentSelfLearningRuleBytes(owner, rule) {
		t.Fatal("genuine tracked procedure refused")
	}
	for _, wrong := range []selfLearningLoadedRule{
		{Target: "../skills/learned/escape.md", SHA256: rule.SHA256},
		{Target: "skills/learned/../learned/" + filepath.Base(rule.Target), SHA256: rule.SHA256},
		{Target: "skills/other/" + filepath.Base(rule.Target), SHA256: rule.SHA256},
		{Target: strings.ReplaceAll(rule.Target, "/", "\\"), SHA256: rule.SHA256},
		{Target: rule.Target, SHA256: strings.Repeat("a", 64)},
	} {
		if currentSelfLearningRuleBytes(owner, wrong) {
			t.Fatalf("unsafe/stale identity accepted: %+v", wrong)
		}
	}
}
