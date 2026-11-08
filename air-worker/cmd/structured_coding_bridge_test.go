package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func TestDW32StructuredBridgeDispatch(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{".gitignore": ".woody/\n", "a.go": "package demo\nfunc A() string { return \"OLD_A\" }\n", "b.go": "package demo\nfunc B() string { return \"OLD_B\" }\n"} {
		if e := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); e != nil {
			t.Fatal(e)
		}
	}
	codingFixtureGit(t, root, "init")
	codingFixtureGit(t, root, "config", "--local", "user.name", "Test")
	codingFixtureGit(t, root, "config", "--local", "user.email", "test@example.invalid")
	codingFixtureGit(t, root, "add", ".")
	codingFixtureGit(t, root, "commit", "-m", "base")
	t.Setenv("AW_CODEX_TWO_WORKER_HELPER", "1")
	t.Setenv("AIR_CODEX_EXE", os.Args[0])
	old := runnerCommand
	defer func() { runnerCommand = old }()
	var calls atomic.Int32
	runnerCommand = func(string, ...string) *exec.Cmd {
		calls.Add(1)
		return exec.Command(os.Args[0], "-test.run=TestTwoWorkerIsolatedHelperProcess")
	}
	c := &loopCtx{Root: root, Subagents: 2, Orchestrate: true, Principal: "gpt", SessionKey: "bridge-fixture", Cfg: runConfig{Orchestration: orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}}}
	runner := runnerSpec{Kind: "codex", Model: "gpt-6-luna", Effort: "low"}
	bad := c.runModelStep(workStep{Num: "bad", Title: "plain text", CodingTask: "plain text"}, "gpt6-luna:low", "", runner)
	if bad.Ok || bad.Subtype != "invalid_coding_task" || calls.Load() != 0 {
		t.Fatalf("invalid input dispatched: %+v calls=%d", bad, calls.Load())
	}
	good := c.runModelStep(workStep{Num: "bridge", Title: "bounded task", CodingTask: codingMicrostepTask(t)}, "gpt6-luna:low", "", runner)
	if good.Ok || good.Subtype != "coordinator_handoff" || good.AgentStarted != 2 || good.AgentCompleted != 2 || calls.Load() != 2 {
		t.Fatalf("valid task did not yield two-worker handoff: %+v calls=%d", good, calls.Load())
	}
}
