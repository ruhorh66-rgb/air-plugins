package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func codingFixtureGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
}

func TestCodingWorkersStatusRecovery(t *testing.T) {
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
	var jobs atomic.Int32
	runnerCommand = func(_ string, _ ...string) *exec.Cmd {
		jobs.Add(1)
		return exec.Command(os.Args[0], "-test.run=TestTwoWorkerIsolatedHelperProcess")
	}
	cfg := runConfig{Orchestration: orchestrationSpec{
		Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true,
	}}
	ctx := &loopCtx{Root: root, Cfg: cfg, Subagents: 2,
		Principal: "gpt", SessionKey: "unit-external-coordinator"}
	base := "Worker 1 edit a.go to NEW_A; worker 2 edit b.go to NEW_B."
	task := filepath.Join(root, "TASK.md")
	if err := os.WriteFile(task, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	cfg = executorTestConfig()
	cfg.Orchestration = orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}
	rawConfig, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), rawConfig, 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, root, "add", "TASK.md", "run-config.json")
	codingFixtureGit(t, root, "commit", "-m", "add task and config")
	ctx.Cfg = cfg
	state := t.TempDir()
	id := sessionIdentity{Principal: ctx.Principal, SessionKey: ctx.SessionKey}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	doc := ctx.runIsolatedCodingWorkers("codex", base, "coord-TASK.md", runnerSpec{
		Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"})
	if doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" || len(doc.Workers) != 2 {
		t.Fatalf("independent coding workspaces not proven: %+v", doc)
	}
	raw, err := os.ReadFile(doc.Report)
	if err != nil {
		t.Fatal(err)
	}
	var saved codingWorkersHandoff
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Report != doc.Report {
		t.Fatalf("handoff report path not persisted: %q, %v", saved.Report, err)
	}
	if jobs.Load() != 2 {
		t.Fatalf("expected exactly two model jobs, got %d", jobs.Load())
	}
	// A and B both physically changed THEIR worktrees while the shared source
	// has not been touched: the third Codex "leader" did not run.
	for i, w := range doc.Workers {
		receipt, err := readJobReceipt(w.Receipt)
		if err != nil || receipt == nil || receipt.Role != "coding-subagent/worker" {
			t.Fatalf("worker %d receipt role: %+v, %v", i+1, receipt, err)
		}
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
	again := ctx.runIsolatedCodingWorkers("codex", base, "coord-TASK.md", runnerSpec{
		Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"})
	if again.Status != "NOT_PROVEN" || !strings.Contains(again.Error, "already exists") {
		t.Fatalf("duplicate run accepted after coordinator handoff: %+v", again)
	}
	if jobs.Load() != 2 {
		t.Fatalf("duplicate launched model jobs: %d", jobs.Load())
	}
	status := func(taskPath, session string) (int, map[string]any) {
		t.Helper()
		oldStdout := os.Stdout
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Stdout = w
		rc := cmdOrchestrateCode([]string{"status", "-product", root, "-task-file", taskPath, "-principal", id.Principal, "-session-key", session, "-state-dir", state, "-json"})
		os.Stdout = oldStdout
		_ = w.Close()
		out, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]any
		if err := json.Unmarshal(out, &result); err != nil {
			t.Fatalf("status JSON %q: %v", out, err)
		}
		return rc, result
	}
	assertRecovered := func() {
		t.Helper()
		for i := 0; i < 2; i++ {
			rc, result := status(task, id.SessionKey)
			encoded, _ := json.Marshal(result)
			if rc != 0 || !strings.Contains(string(encoded), doc.SourceHead) {
				t.Fatalf("status rc=%d: %s", rc, encoded)
			}
			for _, worker := range doc.Workers {
				if !strings.Contains(string(encoded), worker.JobID) || !strings.Contains(string(encoded), worker.PatchSHA256) || !strings.Contains(string(encoded), "DONE") {
					t.Fatalf("missing recovered evidence: %s", encoded)
				}
			}
		}
		if jobs.Load() != 2 {
			t.Fatalf("status launched model jobs: %d", jobs.Load())
		}
	}
	assertRecovered()
	wrongTask := filepath.Join(root, "other.md")
	if err := os.WriteFile(wrongTask, []byte("other"), 0600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadDir(filepath.Dir(filepath.Dir(doc.Report)))
	if rc, result := status(wrongTask, id.SessionKey); rc == 0 && !strings.Contains(fmt.Sprint(result), "NOT_FOUND") {
		t.Fatalf("wrong task: %+v", result)
	}
	if rc, result := status(task, "wrong-session"); rc == 0 {
		t.Fatalf("wrong session: %+v", result)
	}
	after, _ := os.ReadDir(filepath.Dir(filepath.Dir(doc.Report)))
	if len(before) != len(after) {
		t.Fatal("unknown query created a workspace")
	}
	_ = os.Remove(wrongTask)

	reportBytes, err := os.ReadFile(doc.Report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc.Report, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if rc, result := status(task, id.SessionKey); rc != 2 || !strings.Contains(fmt.Sprint(result), "NOT_PROVEN") {
		t.Fatalf("malformed handoff: rc=%d %+v", rc, result)
	}
	if err := os.WriteFile(doc.Report, reportBytes, 0600); err != nil {
		t.Fatal(err)
	}
	patchBytes, err := os.ReadFile(doc.Workers[0].Patch)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc.Workers[0].Patch, append(patchBytes, 'x'), 0600); err != nil {
		t.Fatal(err)
	}
	if rc, result := status(task, id.SessionKey); rc != 2 || !strings.Contains(fmt.Sprint(result), "NOT_PROVEN") {
		t.Fatalf("tampered patch: rc=%d %+v", rc, result)
	}
	if err := os.WriteFile(doc.Workers[0].Patch, patchBytes, 0600); err != nil {
		t.Fatal(err)
	}

	// Receipt-only recovery must preserve uncertainty rather than claim success.
	if err := os.Remove(doc.Report); err != nil {
		t.Fatal(err)
	}
	receiptBytes, err := os.ReadFile(doc.Workers[0].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := readJobReceipt(doc.Workers[0].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Status = jobStatusRunning
	if err := writeJobReceipt(doc.Workers[0].Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	_, running := status(task, id.SessionKey)
	if !strings.Contains(fmt.Sprint(running), "RUNNING") || strings.Contains(fmt.Sprint(running), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") {
		t.Fatalf("running recovery: %+v", running)
	}
	receipt.Status, receipt.OutputSHA256 = jobStatusDone, ""
	if err := writeJobReceipt(doc.Workers[0].Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	_, unknown := status(task, id.SessionKey)
	if !strings.Contains(fmt.Sprint(unknown), "EXECUTION_UNKNOWN") || strings.Contains(fmt.Sprint(unknown), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") {
		t.Fatalf("unsealed recovery: %+v", unknown)
	}
	if err := os.WriteFile(doc.Workers[0].Receipt, receiptBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(doc.Report, reportBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if jobs.Load() != 2 {
		t.Fatalf("recovery launched model jobs: %d", jobs.Load())
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
	codingFixtureGit(t, root, "add", "a.go", "b.go")
	codingFixtureGit(t, root, "commit", "-m", "integrate workers")
	afterCommit := ctx.runIsolatedCodingWorkers("codex", base, "coord-TASK.md", runnerSpec{
		Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"})
	if afterCommit.SourceHead == doc.SourceHead || afterCommit.Status != "NOT_PROVEN" || !strings.Contains(afterCommit.Error, "already exists") {
		t.Fatalf("duplicate run accepted after integration commit: %+v", afterCommit)
	}
	assertRecovered()
	if jobs.Load() != 2 {
		t.Fatalf("integration or duplicate launched additional model jobs: %d", jobs.Load())
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
	var jobs atomic.Int32
	runnerCommand = func(_ string, _ ...string) *exec.Cmd {
		jobs.Add(1)
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
	raw, err := os.ReadFile(doc.Report)
	if err != nil {
		t.Fatal(err)
	}
	var saved codingWorkersHandoff
	if err := json.Unmarshal(raw, &saved); err != nil || saved.Report != doc.Report {
		t.Fatalf("handoff report path not persisted: %q, %v", saved.Report, err)
	}
	if jobs.Load() != 2 {
		t.Fatalf("expected exactly two model jobs, got %d", jobs.Load())
	}
	// A and B both physically changed THEIR worktrees while the shared source
	// has not been touched: the third Codex "leader" did not run.
	for i, w := range doc.Workers {
		receipt, err := readJobReceipt(w.Receipt)
		if err != nil || receipt == nil || receipt.Role != "coding-subagent/worker" {
			t.Fatalf("worker %d receipt role: %+v, %v", i+1, receipt, err)
		}
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
	if jobs.Load() != 2 {
		t.Fatalf("duplicate launched model jobs: %d", jobs.Load())
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
	codingFixtureGit(t, root, "add", "a.go", "b.go")
	codingFixtureGit(t, root, "commit", "-m", "integrate workers")
	afterCommit := ctx.runIsolatedCodingWorkers("codex", base, "external-coordinator-unit", runnerSpec{
		Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"})
	if afterCommit.SourceHead == doc.SourceHead || afterCommit.Status != "NOT_PROVEN" || !strings.Contains(afterCommit.Error, "already exists") {
		t.Fatalf("duplicate run accepted after integration commit: %+v", afterCommit)
	}
	if jobs.Load() != 2 {
		t.Fatalf("integration or duplicate launched additional model jobs: %d", jobs.Load())
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
