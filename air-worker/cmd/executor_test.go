package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func executorTestConfig() runConfig {
	return runConfig{
		Ladder: []string{
			"script",
			"gpt6-luna:low", "gpt6-luna:medium", "gpt6-luna:high",
			"gpt6-sol:low", "gpt6-sol:medium", "gpt6-sol:high",
			"gpt6-astra:low", "gpt6-astra:medium", "gpt6-astra:high",
		},
		Runners: map[string]runnerSpec{
			"gpt6-luna":  {Kind: "codex", Model: "gpt-6-luna"},
			"gpt6-sol":   {Kind: "codex", Model: "gpt-6-sol"},
			"gpt6-astra": {Kind: "codex", Model: "gpt-6-astra"},
			"fb1":        {Kind: "codex", Model: "gpt-fallback-1"},
			"fb2":        {Kind: "codex", Model: "gpt-fallback-2"},
		},
		ModelPolicy: modelPolicySpec{
			Schema: modelPolicySchemaV1,
			ExecutorFallbacks: map[string][]string{
				"gpt6-sol": {"fb1", "fb2"},
			},
			Judges: map[string]modelJudgeLane{
				"default":     {Model: "claude-sonnet-5-5", Efforts: []string{"low", "medium", "high"}},
				"complex":     {Model: "claude-opus-5-5", Efforts: []string{"low", "medium", "high"}},
				"arbitration": {Model: "claude-fable-5-1", Efforts: []string{"low", "medium", "high"}},
			},
		},
	}
}

func TestExecutorListUsesApprovedPolicy(t *testing.T) {
	doc, err := executorListForConfig(executorTestConfig())
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Primary) != 9 {
		t.Fatalf("primary=%d want 9", len(doc.Primary))
	}
	for _, tier := range doc.Primary {
		if tier.Effort == "max" || tier.Effort == "ultra" || tier.Effort == "xhigh" {
			t.Fatalf("forbidden effort surfaced: %+v", tier)
		}
	}
	if len(doc.Fallbacks["gpt6-sol"]) != 6 {
		t.Fatalf("fallback variants=%d want 6", len(doc.Fallbacks["gpt6-sol"]))
	}
	if len(doc.Classes) != 4 || doc.Classes[0].StartTier != "gpt6-luna:low" || doc.Classes[3].MaxTier != "gpt6-astra:high" {
		t.Fatalf("classes=%+v", doc.Classes)
	}
}

func TestExecutorScopeRequiresActiveDeclaredProduct(t *testing.T) {
	root := t.TempDir()
	state := t.TempDir()
	id := sessionIdentity{Principal: "chatgpt", SessionKey: "executor-smoke"}
	now := "2026-09-29T00:00:00Z"
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, DeclaredAt: now, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	scope, err := executorScopeForSession(root, id.Principal, id.SessionKey, state)
	if err != nil {
		t.Fatal(err)
	}
	if scope.ID != id {
		t.Fatalf("scope=%+v want=%+v", scope.ID, id)
	}
	other := t.TempDir()
	if _, err := executorScopeForSession(other, id.Principal, id.SessionKey, state); err == nil {
		t.Fatal("cross-product dispatch must be rejected")
	}
}

func TestExecutorDispatchLoadsPonytailAndWalksVendorFallbacks(t *testing.T) {
	oldCommand := runnerCommand
	oldPonytail := loadPonytailSkill
	t.Cleanup(func() {
		runnerCommand = oldCommand
		loadPonytailSkill = oldPonytail
	})
	t.Setenv("AW_EXECUTOR_HELPER", "1")
	loadPonytailSkill = func() (ponytailSkill, error) {
		return ponytailSkill{Version: "4.10.0", Path: "fixture/SKILL.md", Body: "VENDOR_SKILL_BODY"}, nil
	}
	runnerCommand = func(_ string, args ...string) *exec.Cmd {
		helper := []string{"-test.run=TestExecutorHelperProcess", "--"}
		helper = append(helper, args...)
		return exec.Command(os.Args[0], helper...)
	}

	root := t.TempDir()
	task := filepath.Join(root, "TASK.md")
	if err := os.WriteFile(task, []byte("Return EXECUTOR_OK."), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := executorTestConfig()
	scope := newScope(root, sessionIdentity{Principal: "chatgpt", SessionKey: "executor-smoke"})
	doc := runExecutorDispatch(root, cfg, scope, "standard", "gpt6-sol:low", task)
	if !doc.OK || doc.SelectedTier != "fb2:low" || len(doc.Attempts) != 3 {
		t.Fatalf("dispatch=%+v", doc)
	}
	if doc.Attempts[0].Subtype != "vendor_limit" || doc.Attempts[1].Subtype != "vendor_limit" || !doc.Attempts[2].OK {
		t.Fatalf("attempts=%+v", doc.Attempts)
	}
	receipt, err := readJobReceipt(doc.Attempts[2].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Principal != "chatgpt" || receipt.Session != "executor-smoke" || receipt.Role != "executor/leader" {
		t.Fatalf("receipt=%+v", receipt)
	}
}

func TestExecutorHelperProcess(t *testing.T) {
	if os.Getenv("AW_EXECUTOR_HELPER") != "1" {
		return
	}
	model := ""
	prompt := ""
	for i, arg := range os.Args {
		if arg == "-m" && i+1 < len(os.Args) {
			model = os.Args[i+1]
		}
	}
	if len(os.Args) > 0 {
		prompt = os.Args[len(os.Args)-1]
	}
	if strings.HasPrefix(strings.TrimSpace(prompt), "@ponytail") || !strings.Contains(prompt, "ACTIVE MODE: full") ||
		!strings.Contains(prompt, "VENDOR_SKILL_BODY") || !strings.Contains(prompt, "TASK.md") {
		fmt.Println(`{"type":"turn.failed","error":{"message":"ponytail or task context missing"}}`)
		os.Exit(8)
	}
	fmt.Println(`{"type":"thread.started","thread_id":"executor-helper"}`)
	fmt.Println(`{"type":"turn.started"}`)
	if model == "gpt-6-sol" || model == "gpt-fallback-1" {
		fmt.Println(`{"type":"turn.failed","error":{"message":"HTTP 429 rate limit"}}`)
		os.Exit(1)
	}
	if model != "gpt-fallback-2" {
		fmt.Println(`{"type":"turn.failed","error":{"message":"unexpected model"}}`)
		os.Exit(9)
	}
	fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"EXECUTOR_OK"}}`)
	fmt.Println(`{"type":"turn.completed"}`)
	os.Exit(0)
}
