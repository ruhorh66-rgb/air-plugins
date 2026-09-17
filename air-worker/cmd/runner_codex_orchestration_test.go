package main

import (
	"fmt"
	"os"
	"os/exec"
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
