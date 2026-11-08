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

func codingMicrostepTask(t *testing.T) string {
	t.Helper()
	task := map[string]any{
		"schema": "air-worker.coding-microsteps/v1", "context": "Update the two independent demo functions.",
		"workers": []map[string]any{
			{"index": 1, "title": "Update function A", "file": "a.go", "done_when": "A returns NEW_A", "instructions": "Edit only a.go to return NEW_A."},
			{"index": 2, "title": "Update function B", "file": "b.go", "done_when": "B returns NEW_B", "instructions": "Edit only b.go to return NEW_B."},
		},
	}
	data, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

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
	base := codingMicrostepTask(t)
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
	if doc.TaskSHA256 != shaCodingBytes([]byte(base)) {
		t.Fatalf("original task identity changed: %+v", doc)
	}
	for i, w := range doc.Workers {
		body, err := os.ReadFile(filepath.Join(w.Product, ".woody", "TASK-CODE-WORKER.md"))
		if err != nil {
			t.Fatal(err)
		}
		own, other := "Edit only a.go to return NEW_A.", "Edit only b.go to return NEW_B."
		if i == 1 {
			own, other = other, own
		}
		if !strings.Contains(string(body), own) || strings.Contains(string(body), other) ||
			!strings.Contains(string(body), doc.TaskSHA256) || !strings.Contains(string(body), doc.SourceHead) {
			t.Fatalf("worker %d received wrong task: %s", i+1, body)
		}
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
	provenanceBytes, err := os.ReadFile(doc.Workers[0].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, value string
		mutate      func(*jobReceipt, string)
	}{
		{"missing source SHA", "", func(r *jobReceipt, value string) { r.SourceHead = value }},
		{"malformed source SHA", "not-a-sha256", func(r *jobReceipt, value string) { r.SourceHead = value }},
		{"nonhex source SHA", strings.Repeat("g", 40), func(r *jobReceipt, value string) { r.SourceHead = value }},
		{"missing arguments SHA", "", func(r *jobReceipt, value string) { r.ArgsSHA256 = value }},
		{"malformed arguments SHA", "not-a-sha256", func(r *jobReceipt, value string) { r.ArgsSHA256 = value }},
		{"nonhex arguments SHA", strings.Repeat("g", 64), func(r *jobReceipt, value string) { r.ArgsSHA256 = value }},
		{"missing recovery ref", "", func(r *jobReceipt, value string) { r.RecoveryRef = value }},
		{"wrong recovery ref", "wrong-recovery-ref", func(r *jobReceipt, value string) { r.RecoveryRef = value }},
	} {
		var damaged jobReceipt
		if err := json.Unmarshal(provenanceBytes, &damaged); err != nil {
			t.Fatal(err)
		}
		test.mutate(&damaged, test.value)
		if err := writeJobReceipt(doc.Workers[0].Receipt, &damaged); err != nil {
			t.Fatal(err)
		}
		_, result := status(task, id.SessionKey)
		if err := os.WriteFile(doc.Workers[0].Receipt, provenanceBytes, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprint(result), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") {
			t.Fatalf("%s accepted: %+v", test.name, result)
		}
	}
	assertRecovered()
	mutated := []byte(base + "\n")
	if err := os.WriteFile(task, mutated, 0600); err != nil {
		t.Fatal(err)
	}
	if rc, result := status(task, id.SessionKey); rc != 2 || !strings.Contains(fmt.Sprint(result), "NOT_PROVEN") || strings.Contains(fmt.Sprint(result), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") {
		t.Fatalf("same-path task mutation: rc=%d %+v", rc, result)
	}
	if err := os.WriteFile(task, []byte(base), 0600); err != nil {
		t.Fatal(err)
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
	if err := os.WriteFile(task, mutated, 0600); err != nil {
		t.Fatal(err)
	}
	if rc, result := status(task, id.SessionKey); rc != 2 || !strings.Contains(fmt.Sprint(result), "NOT_PROVEN") || strings.Contains(fmt.Sprint(result), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") {
		t.Fatalf("receipt-only wrong task: rc=%d %+v", rc, result)
	}
	if err := os.WriteFile(task, []byte(base), 0600); err != nil {
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
	var missingExit jobReceipt
	if err := json.Unmarshal(receiptBytes, &missingExit); err != nil {
		t.Fatal(err)
	}
	if !missingExit.ProcessStarted || missingExit.OutputSHA256 == "" {
		t.Fatal("fixture did not start and seal process")
	}
	missingExit.ExitCode = nil
	if err := writeJobReceipt(doc.Workers[0].Receipt, &missingExit); err != nil {
		t.Fatal(err)
	}
	_, unknownExit := status(task, id.SessionKey)
	if !strings.Contains(fmt.Sprint(unknownExit), "EXECUTION_UNKNOWN") || strings.Contains(fmt.Sprint(unknownExit), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") {
		t.Fatalf("missing exit recovery: %+v", unknownExit)
	}
	var missingPID jobReceipt
	if err := json.Unmarshal(receiptBytes, &missingPID); err != nil {
		t.Fatal(err)
	}
	if !missingPID.ProcessStarted || missingPID.ExitCode == nil {
		t.Fatal("fixture missing started process exit")
	}
	missingPID.PID = 0
	if err := writeJobReceipt(doc.Workers[0].Receipt, &missingPID); err != nil {
		t.Fatal(err)
	}
	_, unknownPID := status(task, id.SessionKey)
	if !strings.Contains(fmt.Sprint(unknownPID), "EXECUTION_UNKNOWN") || strings.Contains(fmt.Sprint(unknownPID), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") {
		t.Fatalf("missing PID recovery: %+v", unknownPID)
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
	base := codingMicrostepTask(t)
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

func TestCodingMicrostepDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("requires local Git worktrees")
	}
	root := t.TempDir()
	for name, body := range map[string]string{
		".gitignore": ".woody/\n", "a.go": "package demo\n", "b.go": "package demo\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	codingFixtureGit(t, root, "init")
	codingFixtureGit(t, root, "config", "--local", "user.name", "Test")
	codingFixtureGit(t, root, "config", "--local", "user.email", "test@example.invalid")
	codingFixtureGit(t, root, "add", ".")
	codingFixtureGit(t, root, "commit", "-m", "base")
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	t.Setenv("AW_CODEX_TWO_WORKER_HELPER", "1")
	var jobs atomic.Int32
	runnerCommand = func(_ string, _ ...string) *exec.Cmd {
		jobs.Add(1)
		return exec.Command(os.Args[0], "-test.run=TestTwoWorkerIsolatedHelperProcess")
	}
	ctx := &loopCtx{Root: root, Subagents: 2, Principal: "gpt", SessionKey: "dispatch-test",
		Cfg: runConfig{Orchestration: orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}}}
	runner := runnerSpec{Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"}
	valid := codingMicrostepTask(t)
	for name, task := range map[string]string{
		"legacy":             "Worker 1 edits a.go; worker 2 edits b.go",
		"malformed":          "{",
		"missing worker":     strings.Replace(valid, `,{"done_when":"B returns NEW_B","file":"b.go","index":2,"instructions":"Edit only b.go to return NEW_B.","title":"Update function B"}`, "", 1),
		"duplicate index":    strings.Replace(valid, `"index":2`, `"index":1`, 1),
		"overlap":            strings.Replace(valid, `"file":"b.go"`, `"file":"a.go"`, 1),
		"traversal":          strings.Replace(valid, `"file":"a.go"`, `"file":"../a.go"`, 1),
		"absolute":           strings.Replace(valid, `"file":"a.go"`, `"file":"/a.go"`, 1),
		"protected":          strings.Replace(valid, `"file":"a.go"`, `"file":".git/config"`, 1),
		"empty instructions": strings.Replace(valid, `Edit only a.go to return NEW_A.`, "", 1),
		"unknown key":        strings.Replace(valid, `"schema":`, `"extra":true,"schema":`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if task == valid {
				t.Fatal("test did not alter task")
			}
			doc := ctx.runIsolatedCodingWorkers("codex", task, "invalid-"+name, runner)
			if doc.Status != "NOT_PROVEN" || jobs.Load() != 0 || len(doc.Workers) != 0 {
				t.Fatalf("invalid task launched workers: %+v, jobs=%d", doc, jobs.Load())
			}
		})
	}
	t.Setenv("AW_CODING_OUT_OF_SCOPE", "1")
	doc := ctx.runIsolatedCodingWorkers("codex", valid, "out-of-scope", runner)
	if doc.Status != "NOT_PROVEN" || jobs.Load() != 2 || len(doc.Workers) != 2 || doc.Workers[0].Status == "CODE_WRITTEN_IN_OWN_WORKTREE" {
		t.Fatalf("out-of-scope edit accepted: %+v, jobs=%d", doc, jobs.Load())
	}
	for _, w := range doc.Workers {
		r, err := readJobReceipt(w.Receipt)
		if err != nil || r == nil || !strings.HasSuffix(r.Operation, "-task-"+shaCodingBytes([]byte(valid))) || r.SourceHead != doc.SourceHead {
			t.Fatalf("lost receipt identity: %+v, %v", r, err)
		}
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
		if os.Getenv("AW_CODING_OUT_OF_SCOPE") == "1" {
			_ = os.WriteFile(filepath.Join(root, "b.go"), []byte("out of scope\n"), 0600)
		}
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

// TestCodingEscalation uses actual receipted helper processes; no model is invoked.
func TestCodingEscalation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires local Git worktrees")
	}
	for _, scenario := range []string{"vendor_limit", "narrative", "running", "unknown", "stdin_forged", "stdin_missing", "task_changed", "worker_task_changed", "head_changed"} {
		t.Run(scenario, func(t *testing.T) {
			root, state := t.TempDir(), t.TempDir()
			for name, body := range map[string]string{
				".gitignore": ".woody/\n", "a.go": "package demo\nvar A = 1\n", "b.go": "package demo\nvar B = 1\n",
				"TASK.md": codingMicrostepTask(t),
			} {
				if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			cfg := executorTestConfig()
			cfg.Orchestration = orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}
			raw, err := json.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			codingFixtureGit(t, root, "init")
			codingFixtureGit(t, root, "config", "--local", "user.name", "Test")
			codingFixtureGit(t, root, "config", "--local", "user.email", "test@example.invalid")
			codingFixtureGit(t, root, "add", ".")
			codingFixtureGit(t, root, "commit", "-m", "base")
			id := sessionIdentity{Principal: "gpt", SessionKey: "escalation-test"}
			if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
				t.Fatal(err)
			}
			if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
				t.Fatal(err)
			}
			t.Setenv("AIR_CODEX_EXE", os.Args[0])
			t.Setenv("AW_ESCALATION_HELPER", "1")
			old := runnerCommand
			t.Cleanup(func() { runnerCommand = old })
			var jobs atomic.Int32
			runnerCommand = func(_ string, args ...string) *exec.Cmd {
				jobs.Add(1)
				// Select the failure/success deterministically from ACTUAL model
				// effort, not invocation count: fingerprint readback may create
				// a command object without starting another model job.
				mode := "success"
				if strings.Contains(strings.Join(args, " "), "model_reasoning_effort=low") {
					mode = "failure"
					if scenario == "narrative" {
						mode = "narrative"
					}
				}
				return exec.Command(os.Args[0], "-test.run=TestCodingEscalationHelper", "--", mode)
			}
			receiptJobs := func() int {
				entries, err := os.ReadDir(filepath.Join(root, ".woody", "jobs"))
				if err != nil {
					return 0
				}
				n := 0
				for _, entry := range entries {
					if strings.HasSuffix(entry.Name(), ".receipt.json") {
						n++
					}
				}
				return n
			}
			taskPath := filepath.Join(root, "TASK.md")
			body, _ := os.ReadFile(taskPath)
			head, err := gitCodingOutput(root, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			ctx := &loopCtx{Root: root, Cfg: cfg, Subagents: 2, Principal: id.Principal, SessionKey: id.SessionKey}
			original := ctx.runIsolatedCodingWorkers("codex", string(body), "coord-TASK.md", resolveRunner(cfg, "gpt6-luna:low"))
			if len(original.Workers) != 2 || jobs.Load() != 2 {
				t.Fatalf("initial jobs: %+v, count=%d", original, jobs.Load())
			}
			for _, w := range original.Workers {
				r, err := readJobReceipt(w.Receipt)
				if err != nil || r == nil || r.SourceHead != head || r.ArgsSHA256 == "" || r.StdinSHA256 == "" || r.OutputSHA256 == "" || r.ExitCode == nil || r.RecoveryRef == "" {
					t.Fatalf("unsealed receipt: %+v %v", r, err)
				}
				if scenario != "narrative" && (r.Status != jobStatusFailed || *r.ExitCode == 0) {
					t.Fatalf("expected failed receipt: %+v", r)
				}
				output, err := os.ReadFile(r.OutputPath)
				if err != nil || !strings.Contains(string(output), "INLINE_TASK_PRESENT") {
					t.Fatalf("original task was not supplied inline: %v %s", err, output)
				}
			}
			if scenario == "running" || scenario == "unknown" || scenario == "stdin_forged" || scenario == "stdin_missing" {
				r, _ := readJobReceipt(original.Workers[0].Receipt)
				switch scenario {
				case "running":
					r.Status = jobStatusRunning
				case "unknown":
					r.OutputSHA256 = ""
				case "stdin_forged":
					r.StdinSHA256 = strings.Repeat("0", 64)
				case "stdin_missing":
					r.StdinSHA256 = ""
				}
				if err := writeJobReceipt(original.Workers[0].Receipt, r); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "task_changed" {
				if err := os.WriteFile(taskPath, []byte("different task\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "worker_task_changed" {
				workerTask := filepath.Join(original.Workers[0].Product, ".woody", "TASK-CODE-WORKER.md")
				if err := os.WriteFile(workerTask, []byte("different worker task\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "head_changed" {
				if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package demo\nvar A = 2\n"), 0600); err != nil {
					t.Fatal(err)
				}
				codingFixtureGit(t, root, "add", "a.go")
				codingFixtureGit(t, root, "commit", "-m", "changed head")
			}
			call := func(action string) (int, string) {
				t.Helper()
				// A multi-attempt trace can exceed the Windows anonymous pipe
				// buffer. Capture to a bounded temp file while the command runs;
				// do not deadlock before reading a full JSON result.
				output, err := os.CreateTemp(t.TempDir(), "escalation-*.json")
				if err != nil {
					t.Fatal(err)
				}
				defer output.Close()
				oldOut := os.Stdout
				os.Stdout = output
				rc := cmdOrchestrateCode([]string{action, "-product", root, "-task-file", taskPath, "-principal", id.Principal, "-session-key", id.SessionKey, "-state-dir", state, "-json"})
				os.Stdout = oldOut
				if _, err := output.Seek(0, io.SeekStart); err != nil {
					t.Fatal(err)
				}
				out, err := io.ReadAll(io.LimitReader(output, 2<<20))
				if err != nil {
					t.Fatal(err)
				}
				var v any
				if json.Unmarshal(out, &v) != nil {
					t.Fatalf("invalid JSON: %s", out)
				}
				return rc, string(out)
			}
			rc, result := call("escalate")
			if scenario != "vendor_limit" {
				if rc == 0 || !strings.Contains(result, "NOT_PROVEN") || receiptJobs() != 2 {
					t.Fatalf("unsafe escalation: rc=%d receipts=%d %s", rc, receiptJobs(), result)
				}
				return
			}
			if rc != 0 || receiptJobs() != 4 {
				t.Fatalf("escalation: rc=%d receipts=%d %s", rc, receiptJobs(), result)
			}
			for _, want := range []string{head, shaCodingBytes(body), "vendor_limit", "gpt6-luna:low", "gpt6-luna:medium", original.Workers[0].JobID, original.Workers[1].JobID} {
				if !strings.Contains(result, want) {
					t.Errorf("missing %q in %s", want, result)
				}
			}
			entries, err := os.ReadDir(filepath.Join(root, ".woody", "jobs"))
			if err != nil {
				t.Fatal(err)
			}
			var receipts []*jobReceipt
			for _, e := range entries {
				if strings.HasSuffix(e.Name(), ".receipt.json") {
					r, err := readJobReceipt(filepath.Join(root, ".woody", "jobs", e.Name()))
					if err != nil {
						t.Fatal(err)
					}
					receipts = append(receipts, r)
				}
			}
			if len(receipts) != 4 {
				t.Fatalf("expected four receipts, got %d", len(receipts))
			}
			seen := map[string]bool{}
			for _, r := range receipts {
				if seen[r.JobID] || r.SourceHead != head || !strings.Contains(result, r.JobID) {
					t.Errorf("receipt missing or duplicated: %+v", r)
				}
				seen[r.JobID] = true
			}
			for _, action := range []string{"status", "escalate"} {
				_, out := call(action)
				if !strings.Contains(out, head) {
					t.Errorf("%s lost source: %s", action, out)
				}
			}
			if receiptJobs() != 4 {
				t.Fatalf("readback/retry created new jobs: %d", receiptJobs())
			}
		})
	}
}

func TestCodingEscalationHelper(t *testing.T) {
	if os.Getenv("AW_ESCALATION_HELPER") != "1" {
		return
	}
	input, _ := io.ReadAll(os.Stdin)
	if task, err := os.ReadFile(filepath.Join(".woody", "TASK-CODE-WORKER.md")); err == nil && strings.Contains(string(input), string(task)) {
		fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"INLINE_TASK_PRESENT"}}`)
	}
	mode := ""
	if len(os.Args) >= 2 {
		mode = os.Args[len(os.Args)-1]
	}
	switch mode {
	case "failure":
		fmt.Println(`{"type":"turn.failed","error":{"message":"HTTP 429 rate limit"}}`)
		os.Exit(1)
	case "narrative":
		fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"vendor_limit HTTP 429 rate limit"}}`)
		fmt.Println(`{"type":"turn.completed"}`)
		os.Exit(0)
	}
	if mode != "success" {
		os.Exit(5)
	}
	root, err := os.Getwd()
	if err != nil {
		os.Exit(3)
	}
	name := "a.go"
	if filepath.Base(root) == "worker-2" {
		name = "b.go"
	}
	if err := os.WriteFile(filepath.Join(root, name), []byte("package demo\nvar Edited = true\n"), 0600); err != nil {
		os.Exit(4)
	}
	fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"Edited"}}`)
	fmt.Println(`{"type":"turn.completed"}`)
	os.Exit(0)
}
