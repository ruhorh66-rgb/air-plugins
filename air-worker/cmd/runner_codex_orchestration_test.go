package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCodexNativeOrchestrationUsesExactModelRoster(t *testing.T) {
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	t.Setenv("AW_CODEX_ORCH_HELPER", "1")

	var mu sync.Mutex
	var sandboxes []string
	runnerCommand = func(_ string, args ...string) *exec.Cmd {
		sandbox := ""
		for i, arg := range args {
			if arg == "-s" && i+1 < len(args) {
				sandbox = args[i+1]
			}
		}
		mu.Lock()
		sandboxes = append(sandboxes, sandbox)
		mu.Unlock()
		return exec.Command(os.Args[0], "-test.run=TestCodexOrchestrationHelperProcess")
	}

	ctx := loopCtx{Root: t.TempDir(), Subagents: 2}
	runner := runnerSpec{Kind: "codex", Model: "gpt-5.6-luna", Effort: "medium"}
	res := ctx.invokeCodexOrchestrated("codex", "review task", runner, "88")
	if !res.Ok || res.AgentRequested != 2 || res.AgentStarted != 2 || res.AgentCompleted != 2 {
		t.Fatalf("native orchestration not proven: %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	counts := map[string]int{}
	for _, sandbox := range sandboxes {
		counts[sandbox]++
	}
	if counts["read-only"] != 2 || counts["workspace-write"] != 1 {
		t.Fatalf("unexpected process roster: %#v", counts)
	}

	receipts, err := filepath.Glob(filepath.Join(ctx.Root, ".woody", "jobs", "*.receipt.json"))
	if err != nil || len(receipts) != 3 {
		t.Fatalf("expected three durable orchestration receipts, got %d (%v): %v", len(receipts), err, receipts)
	}
	for _, path := range receipts {
		receipt, err := readJobReceipt(path)
		if err != nil {
			t.Fatalf("read orchestration receipt %s: %v", path, err)
		}
		if receipt.Status != jobStatusDone || receipt.Runner != "codex" || receipt.Provider != "openai" ||
			receipt.Model != "gpt-5.6-luna" || receipt.Effort != "medium" {
			t.Fatalf("receipt must prove exact AirWorker roster, got %+v", receipt)
		}
		wantRole, wantSandbox := "executor/leader", "workspace-write"
		if strings.Contains(receipt.Operation, "subagent") {
			wantRole, wantSandbox = "orchestration-subagent", "read-only"
		}
		if receipt.Role != wantRole || receipt.Sandbox != wantSandbox {
			t.Fatalf("receipt does not distinguish reviewer from leader: %+v", receipt)
		}
	}
}

func TestCodexOrchestrationDoesNotAskLeaderForNestedAgents(t *testing.T) {
	root := t.TempDir()
	ctx := loopCtx{Root: root, Orchestrate: true, Subagents: 2, WhatIf: true, MaxTurns: 1}
	ctx.runModelStep(workStep{Index: 1, Num: "1", Title: "1. Review"}, "luna:medium", "", runnerSpec{Kind: "codex", Model: "gpt-5.6-luna", Effort: "medium"})
	paths, err := filepath.Glob(filepath.Join(root, ".woody", "TASK-*.md"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("task packet missing: %v %v", paths, err)
	}
	raw, err := os.ReadFile(paths[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Use the Agent tool") {
		t.Fatal("Codex binary fan-out must not ask the leader to spawn a second nested agent roster")
	}
}

func TestCodexOrchestrationCountsOnlyStartedProcesses(t *testing.T) {
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	runnerCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "missing-codex.exe"))
	}
	ctx := loopCtx{Root: t.TempDir(), Subagents: 2}
	res := ctx.invokeCodexOrchestrated("codex", "review task", runnerSpec{Kind: "codex", Model: "gpt-5.6-luna", Effort: "medium"}, "88")
	if res.AgentRequested != 2 || res.AgentStarted != 0 || res.AgentCompleted != 0 {
		t.Fatalf("failed starts must not be counted: %+v", res)
	}
}

func TestCodexOrchestrationHelperProcess(t *testing.T) {
	if os.Getenv("AW_CODEX_ORCH_HELPER") != "1" {
		return
	}
	fmt.Println(`{"type":"thread.started","thread_id":"native-agent"}`)
	fmt.Println(`{"type":"turn.started"}`)
	fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"review complete"}}`)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`)
	os.Exit(0)
}
