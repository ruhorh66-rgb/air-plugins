package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func codingFixtureGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func TestChatGPTCoordinatorTwoCodexWorkersOwnRealEdits(t *testing.T) {
	if testing.Short() {
		t.Skip("requires local Git worktree lifecycle")
	}
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	t.Setenv("AW_CODEX_TWO_WORKER_HELPER", "1")
	root := t.TempDir()
	for path, body := range map[string]string{
		".gitignore": ".woody/\n",
		"a.go":       "package demo\nfunc A() string { return \"OLD_A\" }\n",
		"b.go":       "package demo\nfunc B() string { return \"OLD_B\" }\n",
	} {
		if err := os.WriteFile(filepath.Join(root, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	codingFixtureGit(t, root, "init")
	codingFixtureGit(t, root, "config", "--local", "user.name", "AirWorker Test")
	codingFixtureGit(t, root, "config", "--local", "user.email", "airworker-test@example.invalid")
	codingFixtureGit(t, root, "add", ".")
	codingFixtureGit(t, root, "commit", "-m", "synthetic base")
	runnerCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestTwoWorkerIsolatedHelperProcess")
	}
	cfg := runConfig{Orchestration: orchestrationSpec{
		Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true,
	}}
	ctx := &loopCtx{Root: root, Cfg: cfg, Subagents: 2,
		Principal: "gpt", SessionKey: "unit-external-coordinator"}
	base := "Worker 1 edit a.go to NEW_A; worker 2 edit b.go to NEW_B."
	doc := ctx.runIsolatedCodingWorkers("codex", base, "external-coordinator-unit", runnerSpec{
		Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"})
	if doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" || len(doc.Workers) != 2 {
		t.Fatalf("independent coding workspaces not proven: %+v", doc)
	}
	// A and B both physically changed THEIR worktrees while the shared source
	// has not been touched: the third Codex "leader" did not run.
	for i, w := range doc.Workers {
		if w.Status != "CODE_WRITTEN_IN_OWN_WORKTREE" || w.PatchSHA256 == "" ||
			w.Receipt == "" || w.Patch == "" || len(w.Changed) != 1 ||
			!strings.HasSuffix(w.Changed[0], fmt.Sprintf("%c.go", 'a'+i)) {
			t.Fatalf("worker %d did not author its own actual source change: %+v", i+1, w)
		}
	}
	originalA, _ := os.ReadFile(filepath.Join(root, "a.go"))
	originalB, _ := os.ReadFile(filepath.Join(root, "b.go"))
	if !strings.Contains(string(originalA), "OLD_A") || !strings.Contains(string(originalB), "OLD_B") {
		t.Fatal("a model wrote the shared source behind ChatGPT coordinator's back")
	}
	// The same session+step+source may never start worker processes twice.
	// This check happens BEFORE coordinator integration changes the base tree.
	again := ctx.runIsolatedCodingWorkers("codex", base, "external-coordinator-unit", runnerSpec{
		Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"})
	if again.Status != "NOT_PROVEN" || !strings.Contains(again.Error, "already exists") {
		t.Fatalf("duplicate run accepted after coordinator handoff: %+v", again)
	}
	// ChatGPT coordinator now integrates real files from independent workers,
	// without dispatching a third model process.
	for _, w := range doc.Workers {
		codingFixtureGit(t, root, "apply", "--check", w.Patch)
		codingFixtureGit(t, root, "apply", w.Patch)
	}
	afterA, _ := os.ReadFile(filepath.Join(root, "a.go"))
	afterB, _ := os.ReadFile(filepath.Join(root, "b.go"))
	if !strings.Contains(string(afterA), "NEW_A") || !strings.Contains(string(afterB), "NEW_B") {
		t.Fatal("ChatGPT coordinator could not integrate the two real worker changes")
	}
}

func TestTwoWorkerIsolatedHelperProcess(t *testing.T) {
	if os.Getenv("AW_CODEX_TWO_WORKER_HELPER") != "1" {
		return
	}
	root, err := os.Getwd()
	if err != nil {
		os.Exit(3)
	}
	switch filepath.Base(root) {
	case "worker-1":
		_ = os.WriteFile(filepath.Join(root, "a.go"),
			[]byte("package demo\nfunc A() string { return \"NEW_A\" }\n"), 0600)
	case "worker-2":
		_ = os.WriteFile(filepath.Join(root, "b.go"),
			[]byte("package demo\nfunc B() string { return \"NEW_B\" }\n"), 0600)
	default:
		os.Exit(4)
	}
	fmt.Println("{\"type\":\"thread.started\",\"thread_id\":\"two-worker-helper\"}")
	fmt.Println("{\"type\":\"turn.started\"}")
	fmt.Println("{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"Edited my own worktree\"}}")
	fmt.Println("{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}")
	os.Exit(0)
}
