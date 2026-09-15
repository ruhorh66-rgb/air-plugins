package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInvokeCodexWritesProductRoot(t *testing.T) {
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	t.Setenv("AW_CODEX_HELPER", "1")
	t.Setenv("CLAUDE_MARKER", "must-not-leak")
	runnerCommand = func(_ string, args ...string) *exec.Cmd {
		a := []string{"-test.run=TestCodexHelperProcess", "--"}
		a = append(a, args...)
		return exec.Command(os.Args[0], a...)
	}
	root := t.TempDir()
	ctx := loopCtx{Root: root}
	res := ctx.invokeCodex("codex", "task", runnerSpec{Kind: "codex", Model: "gpt-test", Effort: "medium"}, "73")
	if !res.Ok || res.Subtype != "success" {
		t.Fatalf("Codex invocation failed: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "codex-write.txt")); err != nil {
		t.Fatalf("Codex did not write product root: %v", err)
	}
}

func TestCodexHelperProcess(t *testing.T) {
	if os.Getenv("AW_CODEX_HELPER") != "1" {
		return
	}
	var root, sandbox string
	for i, a := range os.Args {
		if a == "-C" && i+1 < len(os.Args) {
			root = os.Args[i+1]
		}
		if a == "-s" && i+1 < len(os.Args) {
			sandbox = os.Args[i+1]
		}
	}
	if os.Getenv("CLAUDE_MARKER") != "" {
		fmt.Println(`{"type":"error","message":"foreign environment leaked"}`)
		os.Exit(9)
	}
	if root == "" || sandbox != "workspace-write" {
		fmt.Println(`{"type":"turn.failed","error":{"message":"wrong invocation"}}`)
		os.Exit(8)
	}
	_ = os.WriteFile(filepath.Join(root, "codex-write.txt"), []byte("ok"), 0o644)
	fmt.Println(`{"type":"thread.started","thread_id":"helper-thread"}`)
	fmt.Println(`{"type":"turn.started"}`)
	fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"DONE"}}`)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`)
	os.Exit(0)
}
