package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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
	testCodingWorkersStatusRecovery(t, "unit-external-coordinator")
}

func TestCodingWorkersStatusRecoveryLongIdentity(t *testing.T) {
	testCodingWorkersStatusRecovery(t, strings.Repeat("s", 120))
}

func testCodingWorkersStatusRecovery(t *testing.T, sessionKey string) {
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
		Principal: "gpt", SessionKey: sessionKey}
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
	ackAbsent := func(label string, scope sessionScope, task string) {
		t.Helper()
		if path, err := ackCodingIntegrated(root, scope, "coord-TASK.md", task); err == nil {
			t.Fatalf("%s: acknowledgement accepted: %s", label, path)
		}
		if jobs.Load() != 2 {
			t.Fatalf("%s: acknowledgement launched a job", label)
		}
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
		if len(sessionKey) > 100 && !strings.HasPrefix(filepath.Base(w.Receipt), "awjob-") {
			t.Fatalf("long identity did not use bounded name: %s", w.Receipt)
		}
		receipt, err := readJobReceipt(w.Receipt)
		if err != nil || receipt == nil || receipt.Role != "coding-subagent/worker" {
			t.Fatalf("worker %d receipt role: %+v, %v", i+1, receipt, err)
		}
		if !strings.Contains(receipt.Operation, "-v2-task-"+doc.TaskSHA256) || len(filepath.Base(w.Receipt)) > 255 {
			t.Fatalf("worker %d operation or receipt name: %+v", i+1, receipt)
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
		outCh := make(chan []byte, 1)
		errCh := make(chan error, 1)
		go func() { b, e := io.ReadAll(r); outCh <- b; errCh <- e }()
		rc := cmdOrchestrateCode([]string{"status", "-product", root, "-task-file", taskPath, "-principal", id.Principal, "-session-key", session, "-state-dir", state, "-json"})
		os.Stdout = oldStdout
		_ = w.Close()
		out := <-outCh
		err = <-errCh
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
	reportBefore, err := os.ReadFile(doc.Report)
	if err != nil {
		t.Fatal(err)
	}
	receiptBefore, err := os.ReadFile(doc.Workers[0].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	unrelatedPath := filepath.Join(filepath.Dir(doc.Workers[0].Receipt), "awjob-"+strings.Repeat("f", 64)+".receipt.json")
	if err := os.WriteFile(unrelatedPath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		rc, result := status(task, id.SessionKey)
		workers, _ := result["Workers"].([]any)
		retry, _ := result["retry"].(map[string]any)
		if retry["Status"] != "EXECUTION_UNKNOWN" {
			t.Fatalf("unknown retry not preserved: %+v", retry)
		}
		if rc != 0 || result["Status"] != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" || len(workers) != 2 || jobs.Load() != 2 {
			t.Fatalf("unrelated damaged receipt poisoned handoff: rc=%d result=%+v jobs=%d", rc, result, jobs.Load())
		}
		for _, worker := range doc.Workers {
			if !strings.Contains(fmt.Sprint(workers), worker.JobID) {
				t.Fatalf("unrelated damaged receipt changed workers: %+v", workers)
			}
		}
	}
	if body, err := os.ReadFile(unrelatedPath); err != nil || string(body) != "{" {
		t.Fatalf("status changed unrelated receipt: %q, %v", body, err)
	}
	if body, err := os.ReadFile(doc.Report); err != nil || string(body) != string(reportBefore) {
		t.Fatalf("status changed handoff: %v", err)
	}
	if body, err := os.ReadFile(doc.Workers[0].Receipt); err != nil || string(body) != string(receiptBefore) {
		t.Fatalf("status changed owned receipt: %v", err)
	}
	if err := os.Remove(unrelatedPath); err != nil {
		t.Fatal(err)
	}
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
		{"missing stdin SHA", "", func(r *jobReceipt, value string) { r.StdinSHA256 = value }},
		{"forged stdin SHA", strings.Repeat("0", 64), func(r *jobReceipt, value string) { r.StdinSHA256 = value }},
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
		rc, result := status(task, id.SessionKey)
		if err := os.WriteFile(doc.Workers[0].Receipt, provenanceBytes, 0600); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(fmt.Sprint(result), "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION") || jobs.Load() != 2 {
			t.Fatalf("%s accepted or launched jobs: rc=%d result=%+v jobs=%d", test.name, rc, result, jobs.Load())
		}
		if strings.Contains(test.name, "stdin SHA") && !strings.Contains(fmt.Sprint(result), "EXECUTION_UNKNOWN") {
			t.Fatalf("%s did not preserve uncertainty: %+v", test.name, result)
		}
	}
	assertRecovered()
	if err := os.WriteFile(doc.Workers[0].Receipt, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if rc, result := status(task, id.SessionKey); rc != 2 || result["Status"] != "NOT_PROVEN" || jobs.Load() != 2 {
		t.Fatalf("owned damaged receipt accepted: rc=%d result=%+v jobs=%d", rc, result, jobs.Load())
	}
	if err := os.WriteFile(doc.Workers[0].Receipt, provenanceBytes, 0600); err != nil {
		t.Fatal(err)
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
	ackAbsent("damaged patch", ctx.scope(), base)
	if _, err := verifyCodingIntegrated(root, ctx.scope(), "coord-TASK.md", base); err == nil {
		t.Fatal("verifier accepted damaged worker patch")
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
	if err := os.WriteFile(unrelatedPath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if rc, result := status(task, id.SessionKey); rc != 2 || result["Status"] != "NOT_PROVEN" || jobs.Load() != 2 {
		t.Fatalf("receipt-only damaged unknown hash accepted: rc=%d result=%+v jobs=%d", rc, result, jobs.Load())
	}
	if err := os.Remove(unrelatedPath); err != nil {
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
	var legacy jobReceipt
	if err := json.Unmarshal(receiptBytes, &legacy); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(legacy.Operation, "-v2-task-"+doc.TaskSHA256) {
		t.Fatalf("new receipt lacks v2 marker: %s", legacy.Operation)
	}
	legacy.Operation = strings.Replace(legacy.Operation, "-v2-task-", "-task-", 1)
	legacy.StdinSHA256 = ""
	legacyPath, legacyOutput := jobReceiptPaths(ctx.scope(), legacy.Step, legacy.Operation)
	oldOutput := legacy.OutputPath
	if err := os.Rename(oldOutput, legacyOutput); err != nil {
		t.Fatal(err)
	}
	legacy.OutputPath = legacyOutput
	if legacyPath == doc.Workers[0].Receipt || len(filepath.Base(legacyPath)) > 255 {
		t.Fatalf("invalid legacy receipt path: %s", legacyPath)
	}
	legacy.RecoveryRef = filepath.ToSlash(legacyPath) + "#job_id=" + legacy.JobID
	if err := os.Remove(doc.Workers[0].Receipt); err != nil {
		t.Fatal(err)
	}
	if err := writeJobReceipt(legacyPath, &legacy); err != nil {
		t.Fatal(err)
	}
	if rc, result := status(task, id.SessionKey); rc != 0 || !strings.Contains(fmt.Sprint(result), "LEGACY_UNMEASURED") || strings.Contains(fmt.Sprint(result), "NOT_PROVEN") || jobs.Load() != 2 {
		t.Fatalf("legacy receipt readback: rc=%d result=%+v jobs=%d", rc, result, jobs.Load())
	}
	if _, err := verifyCodingIntegrated(root, ctx.scope(), "coord-TASK.md", base); err == nil {
		t.Fatal("verifier accepted unmeasured legacy receipt")
	}
	ackAbsent("legacy unmeasured", ctx.scope(), base)
	if err := os.Rename(legacyOutput, oldOutput); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(legacyPath); err != nil {
		t.Fatal(err)
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
	verify := func(want bool, label string) {
		t.Helper()
		beforeJobs := jobs.Load()
		beforeReport, err := os.ReadFile(doc.Report)
		if err != nil {
			t.Fatal(err)
		}
		beforeReceipt, err := os.ReadFile(doc.Workers[0].Receipt)
		if err != nil {
			t.Fatal(err)
		}
		_, err = verifyCodingIntegrated(root, ctx.scope(), "coord-TASK.md", base)
		if (err == nil) != want {
			t.Fatalf("%s: integration verification error = %v, want success %v", label, err, want)
		}
		afterReport, reportErr := os.ReadFile(doc.Report)
		afterReceipt, receiptErr := os.ReadFile(doc.Workers[0].Receipt)
		if reportErr != nil || receiptErr != nil || string(afterReport) != string(beforeReport) || string(afterReceipt) != string(beforeReceipt) || jobs.Load() != beforeJobs {
			t.Fatalf("%s: verifier changed evidence or launched a job", label)
		}
	}
	ackAbsent("before integration", ctx.scope(), base)
	verify(false, "before integration")
	// ChatGPT coordinator now integrates real files from independent workers,
	// without dispatching a third model process.
	for i, w := range doc.Workers {
		codingFixtureGit(t, root, "apply", "--check", w.Patch)
		codingFixtureGit(t, root, "apply", w.Patch)
		if i == 0 {
			ackAbsent("one integrated patch", ctx.scope(), base)
		}
		verify(i == 1, fmt.Sprintf("after %d patches", i+1))
	}
	ackAbsent("changed task", ctx.scope(), base+"\n")
	wrongScope := ctx.scope()
	wrongScope.ID.SessionKey += "-wrong"
	ackAbsent("wrong scope", wrongScope, base)
	output, err := os.ReadFile(doc.Workers[0].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := readJobReceipt(doc.Workers[0].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	workerOutput, err := os.ReadFile(sealed.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sealed.OutputPath, append(append([]byte(nil), workerOutput...), 'x'), 0600); err != nil {
		t.Fatal(err)
	}
	ackAbsent("damaged output", ctx.scope(), base)
	if err := os.WriteFile(sealed.OutputPath, workerOutput, 0600); err != nil {
		t.Fatal(err)
	}
	ackPath, err := ackCodingIntegrated(root, ctx.scope(), "coord-TASK.md", base)
	if err != nil {
		t.Fatalf("exact integration acknowledgement: %v", err)
	}
	ackBytes, err := os.ReadFile(ackPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{doc.TaskSHA256, doc.SourceHead, doc.Workers[0].JobID, doc.Workers[1].JobID, doc.Workers[0].PatchSHA256, doc.Workers[1].PatchSHA256, ctx.SessionKey} {
		if !bytes.Contains(ackBytes, []byte(value)) {
			t.Errorf("acknowledgement missing %q", value)
		}
	}
	if bytes.Contains(ackBytes, []byte(base)) {
		t.Fatal("acknowledgement embedded raw task")
	}
	if again, err := ackCodingIntegrated(root, ctx.scope(), "coord-TASK.md", base); err == nil && again != ackPath {
		t.Fatalf("duplicate acknowledgement created: %s", again)
	}
	unchanged, err := os.ReadFile(ackPath)
	if err != nil || !bytes.Equal(ackBytes, unchanged) || jobs.Load() != 2 {
		t.Fatal("duplicate acknowledgement changed evidence or launched a job")
	}
	finalReceipt, err := os.ReadFile(doc.Workers[0].Receipt)
	if err != nil || !bytes.Equal(output, finalReceipt) {
		t.Fatal("acknowledgement changed worker receipt")
	}
	for _, name := range []string{"a.go", "b.go"} {
		path := filepath.Join(root, name)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, append(append([]byte(nil), body...), 'x'), 0600); err != nil {
			t.Fatal(err)
		}
		verify(false, "changed byte in "+name)
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "unrelated.go"), []byte("package demo\n"), 0600); err != nil {
		t.Fatal(err)
	}
	verify(false, "unrelated untracked file")
	codingFixtureGit(t, root, "add", "unrelated.go")
	verify(false, "unrelated tracked file")
	codingFixtureGit(t, root, "reset", "--", "unrelated.go")
	if err := os.Remove(filepath.Join(root, "unrelated.go")); err != nil {
		t.Fatal(err)
	}
	verify(true, "restored exact integration")
	if err := os.WriteFile(doc.Workers[0].Patch, append(append([]byte(nil), patchBytes...), 'x'), 0600); err != nil {
		t.Fatal(err)
	}
	verify(false, "damaged patch after integration")
	if err := os.WriteFile(doc.Workers[0].Patch, patchBytes, 0600); err != nil {
		t.Fatal(err)
	}
	verify(true, "restored worker patch")
	legacyReceipt, err := readJobReceipt(doc.Workers[0].Receipt)
	if err != nil {
		t.Fatal(err)
	}
	legacyReceipt.Operation = strings.Replace(legacyReceipt.Operation, "-v2-task-", "-task-", 1)
	legacyReceipt.StdinSHA256 = ""
	if err := writeJobReceipt(doc.Workers[0].Receipt, legacyReceipt); err != nil {
		t.Fatal(err)
	}
	verify(false, "unmeasured legacy after integration")
	if err := os.WriteFile(doc.Workers[0].Receipt, receiptBytes, 0600); err != nil {
		t.Fatal(err)
	}
	verify(true, "restored measured receipt")
	if err := os.WriteFile(ackPath, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if path, err := ackCodingIntegrated(root, ctx.scope(), "coord-TASK.md", base); err == nil {
		t.Fatalf("partial acknowledgement accepted: %s", path)
	}
	partial, err := os.ReadFile(ackPath)
	if err != nil || string(partial) != "{" || jobs.Load() != 2 {
		t.Fatal("partial acknowledgement overwritten or launched a job")
	}
	afterA, _ := os.ReadFile(filepath.Join(root, "a.go"))
	afterB, _ := os.ReadFile(filepath.Join(root, "b.go"))
	if !strings.Contains(string(afterA), "NEW_A") || !strings.Contains(string(afterB), "NEW_B") {
		t.Fatal("ChatGPT coordinator could not integrate the two real worker changes")
	}
	codingFixtureGit(t, root, "add", "a.go", "b.go")
	codingFixtureGit(t, root, "commit", "-m", "integrate workers")
	verify(false, "changed HEAD")
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
		if err != nil || r == nil || !strings.HasSuffix(r.Operation, "-v2-task-"+shaCodingBytes([]byte(valid))) || r.SourceHead != doc.SourceHead || len(filepath.Base(w.Receipt)) > 255 {
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
	for _, scenario := range []string{"vendor_limit", "vendor_limit_codex", "narrative", "running", "unknown", "unknown_hash", "stdin_forged", "stdin_missing", "task_changed", "worker_task_changed", "head_changed", "missing_handoff", "missing_receipt", "missing_output", "tampered_output", "unstarted"} {
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
			if scenario == "vendor_limit_codex" {
				id.Principal = "codex"
			}
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
				if scenario == "vendor_limit" || scenario == "vendor_limit_codex" {
					return exec.Command(os.Args[0], append([]string{"-test.run=^TestG06FingerprintHelper$", "--"}, args...)...)
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
				version := "v2"
				if id.Principal == "codex" {
					version = "v3"
				}
				if err != nil || r == nil || r.SourceHead != head || !strings.HasSuffix(r.Operation, "-"+version+"-task-"+shaCodingBytes(body)) || r.ArgsSHA256 == "" || r.StdinSHA256 == "" || r.OutputSHA256 == "" || r.ExitCode == nil || r.RecoveryRef == "" || len(filepath.Base(w.Receipt)) > 255 {
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
			if scenario == "running" || scenario == "unknown" || scenario == "stdin_forged" || scenario == "stdin_missing" || scenario == "unstarted" {
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
				case "unstarted":
					r.ProcessStarted = false
				}
				if err := writeJobReceipt(original.Workers[0].Receipt, r); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "missing_handoff" {
				if err := os.Remove(original.Report); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "missing_receipt" {
				if err := os.Remove(original.Workers[0].Receipt); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "missing_output" || scenario == "tampered_output" {
				r, err := readJobReceipt(original.Workers[0].Receipt)
				if err != nil {
					t.Fatal(err)
				}
				if scenario == "missing_output" {
					err = os.Remove(r.OutputPath)
				} else {
					err = os.WriteFile(r.OutputPath, []byte("tampered"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "unknown_hash" {
				if err := os.WriteFile(filepath.Join(root, ".woody", "jobs", "awjob-"+strings.Repeat("f", 64)+".receipt.json"), []byte("{"), 0600); err != nil {
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
			scope, err := executorScopeForSession(root, id.Principal, id.SessionKey, state)
			if err != nil || !reflect.DeepEqual(scope, ctx.scope()) {
				t.Fatalf("fixture scope mismatch: %+v %+v %v", scope, ctx.scope(), err)
			}
			snapshot := func() map[string]string {
				t.Helper()
				files := map[string]string{}
				for _, dir := range []string{root, state, original.Workers[0].Product, original.Workers[1].Product} {
					err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
						if err != nil {
							return err
						}
						if entry.Name() == ".git" {
							if entry.IsDir() {
								return filepath.SkipDir
							}
							return nil
						}
						if entry.IsDir() {
							files[path] = "directory"
							return nil
						}
						data, err := os.ReadFile(path)
						if err == nil {
							files[path] = shaCodingBytes(data)
						}
						return err
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				return files
			}
			before, receiptsBefore := snapshot(), receiptJobs()
			currentBody, err := os.ReadFile(taskPath)
			if err != nil {
				t.Fatal(err)
			}
			status, statusErr := codingStatus(root, scope, "coord-TASK.md", string(currentBody))
			if scenario == "vendor_limit" || scenario == "vendor_limit_codex" || scenario == "head_changed" {
				if statusErr != nil || status.Status != "FAILED" || status.SourceHead != head || status.TaskSHA256 != shaCodingBytes(body) || len(status.Workers) != 2 || status.Workers[0].JobID != original.Workers[0].JobID || status.Workers[1].JobID != original.Workers[1].JobID {
					t.Fatalf("failed status lost evidence: %+v, %v", status, statusErr)
				}
			} else if statusErr == nil && status.Status != "EXECUTION_UNKNOWN" {
				t.Fatalf("incomplete evidence did not preserve uncertainty: %+v", status)
			}
			if after := snapshot(); !reflect.DeepEqual(before, after) || receiptJobs() != receiptsBefore || jobs.Load() != 2 {
				t.Fatalf("status mutated fixture or created jobs: receipts %d -> %d, jobs=%d", receiptsBefore, receiptJobs(), jobs.Load())
			}
			historical, historicalErr := validateHistoricalCodingEscalation(root, cfg, scope, "coord-TASK.md", currentBody)
			if scenario == "vendor_limit" || scenario == "vendor_limit_codex" || scenario == "head_changed" {
				if historicalErr != nil || historical.SourceHead != head || historical.TaskSHA256 != shaCodingBytes(body) || len(historical.Attempt1.Workers) != 2 || historical.Attempt1.Workers[0].JobID != original.Workers[0].JobID || historical.Attempt1.Workers[1].JobID != original.Workers[1].JobID {
					t.Fatalf("historical evidence lost: %+v, %v", historical, historicalErr)
				}
			} else if historicalErr == nil {
				t.Fatalf("historical verification accepted incomplete evidence: %+v", historical)
			}
			if after := snapshot(); !reflect.DeepEqual(before, after) || receiptJobs() != receiptsBefore {
				t.Fatalf("historical verification mutated fixture or created jobs: receipts %d -> %d", receiptsBefore, receiptJobs())
			}
			if scenario == "vendor_limit" {
				// Alter one binding at a time, then restore the sealed evidence.
				receiptPath := original.Workers[0].Receipt
				receiptBytes, err := os.ReadFile(receiptPath)
				if err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"command", "source", "stdin"} {
					r, err := readJobReceipt(receiptPath)
					if err != nil {
						t.Fatal(err)
					}
					switch field {
					case "command":
						r.ArgsSHA256 = strings.Repeat("0", 64)
					case "source":
						r.SourceHead = strings.Repeat("0", len(head))
					case "stdin":
						r.StdinSHA256 = strings.Repeat("0", 64)
					}
					if err := writeJobReceipt(receiptPath, r); err != nil {
						t.Fatal(err)
					}
					tampered := snapshot()
					if result, err := validateHistoricalCodingEscalation(root, cfg, scope, "coord-TASK.md", body); err == nil {
						t.Errorf("historical verification accepted tampered %s: %+v", field, result)
					}
					if after := snapshot(); !reflect.DeepEqual(tampered, after) || receiptJobs() != receiptsBefore {
						t.Fatalf("historical verification mutated tampered %s fixture", field)
					}
					if err := os.WriteFile(receiptPath, receiptBytes, 0600); err != nil {
						t.Fatal(err)
					}
				}
				otherScope := newScope(root, sessionIdentity{Principal: id.Principal, SessionKey: "other-session"})
				otherCfg := cfg
				otherCfg.Ladder = []string{"gpt6-luna:medium"}
				for name, check := range map[string]func() error{
					"session": func() error {
						_, err := validateHistoricalCodingEscalation(root, cfg, otherScope, "coord-TASK.md", body)
						return err
					},
					"config": func() error {
						_, err := validateHistoricalCodingEscalation(root, otherCfg, scope, "coord-TASK.md", body)
						return err
					},
					"task": func() error {
						_, err := validateHistoricalCodingEscalation(root, cfg, scope, "coord-TASK.md", []byte("different task"))
						return err
					},
				} {
					if err := check(); err == nil {
						t.Errorf("historical verification accepted changed %s", name)
					}
				}
				if after := snapshot(); !reflect.DeepEqual(before, after) || receiptJobs() != receiptsBefore {
					t.Fatal("historical verification mutated fixture or created jobs")
				}
			}
			trace, next, exe, err := validateCodingEscalation(root, cfg, scope, "coord-TASK.md", currentBody)
			if scenario == "vendor_limit" || scenario == "vendor_limit_codex" {
				if err != nil {
					t.Fatalf("eligible escalation rejected: %v", err)
				}
				expectedExe, resolveErr := resolveRunnerTool("codex")
				if resolveErr != nil {
					t.Fatal(resolveErr)
				}
				if trace.TaskSHA256 != shaCodingBytes(body) || trace.SourceHead != head || trace.FromTier != "gpt6-luna:low" || trace.ToTier != "gpt6-luna:medium" || trace.Cause != "vendor_limit" || len(trace.Attempt1.Workers) != 2 || trace.Attempt1.Workers[0].JobID != original.Workers[0].JobID || trace.Attempt1.Workers[1].JobID != original.Workers[1].JobID || !reflect.DeepEqual(next, resolveRunner(cfg, "gpt6-luna:medium")) || exe != expectedExe {
					t.Fatalf("unexpected eligibility: %+v next=%+v exe=%q", trace, next, exe)
				}
			} else if err == nil {
				t.Fatalf("unsafe escalation accepted: %+v", trace)
			}
			if after := snapshot(); !reflect.DeepEqual(before, after) || receiptJobs() != receiptsBefore {
				t.Fatalf("validation mutated fixture or created jobs: receipts %d -> %d", receiptsBefore, receiptJobs())
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
			jobsBeforeLaunch := jobs.Load()
			rc, result := call("escalate")
			if scenario == "head_changed" && jobs.Load() != jobsBeforeLaunch {
				t.Fatalf("changed HEAD launched jobs: %d -> %d", jobsBeforeLaunch, jobs.Load())
			}
			if scenario == "unknown_hash" || scenario == "missing_receipt" {
				want := 3
				if scenario == "missing_receipt" {
					want = 1
				}
				if rc == 0 || !strings.Contains(result, "NOT_PROVEN") || receiptJobs() != want {
					t.Fatalf("missing or unknown receipt allowed retry: rc=%d receipts=%d %s", rc, receiptJobs(), result)
				}
				return
			}
			if scenario != "vendor_limit" && scenario != "vendor_limit_codex" {
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
			var escalation codingEscalation
			if err := json.Unmarshal([]byte(result), &escalation); err != nil || len(escalation.Attempt2.Workers) != 2 {
				t.Fatalf("retry handoff: %+v %v", escalation, err)
			}
			for _, w := range escalation.Attempt2.Workers {
				codingFixtureGit(t, root, "apply", "--check", w.Patch)
				codingFixtureGit(t, root, "apply", w.Patch)
			}
			for label, path := range map[string]string{
				"original receipt": original.Workers[0].Receipt,
				"original handoff": original.Report,
				"retry receipt":    escalation.Attempt2.Workers[0].Receipt,
				"retry patch":      escalation.Attempt2.Workers[0].Patch,
				"retry handoff":    escalation.Attempt2.Report,
			} {
				saved, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := ackCodingIntegrated(root, scope, "coord-TASK.md", string(body)); err == nil {
					t.Errorf("accepted damaged %s", label)
				}
				if err := os.WriteFile(path, saved, 0600); err != nil {
					t.Fatal(err)
				}
			}
			foreign := scope
			foreign.ID.SessionKey = "foreign"
			if _, err := ackCodingIntegrated(root, foreign, "coord-TASK.md", string(body)); err == nil {
				t.Error("accepted foreign session")
			}
			if _, err := ackCodingIntegrated(root, scope, "coord-TASK.md", string(body)+" "); err == nil {
				t.Error("accepted changed task")
			}
			if _, err := ackCodingIntegrated(root, scope, "coord-TASK.md", string(body)); err != nil {
				t.Fatalf("original step ACK: %v", err)
			}
			ack, err := readCodingIntegrationAck(root, scope, "coord-TASK.md", string(body))
			if err != nil || ack.Status != "ACK_RECORDED" || ack.JobIDs[0] != escalation.Attempt2.Workers[0].JobID || ack.JobIDs[1] != escalation.Attempt2.Workers[1].JobID {
				t.Fatalf("original step readback: %+v %v", ack, err)
			}
			codingFixtureGit(t, root, "add", "a.go", "b.go")
			codingFixtureGit(t, root, "commit", "-m", "integrate retry patches")
			if _, err := verifyCodingCommitted(root, scope, "coord-TASK.md", string(body), ack); err != nil {
				t.Fatalf("original step commit verification: %v", err)
			}
			for label, path := range map[string]string{
				"original receipt": original.Workers[0].Receipt,
				"original handoff": original.Report,
				"retry receipt":    escalation.Attempt2.Workers[0].Receipt,
				"retry patch":      escalation.Attempt2.Workers[0].Patch,
				"retry handoff":    escalation.Attempt2.Report,
			} {
				saved, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
					t.Fatal(err)
				}
				if _, err := readCodingIntegrationAck(root, scope, "coord-TASK.md", string(body)); err == nil {
					t.Errorf("readback accepted damaged %s", label)
				}
				if _, err := verifyCodingCommitted(root, scope, "coord-TASK.md", string(body), ack); err == nil {
					t.Errorf("commit verification accepted damaged %s", label)
				}
				if err := os.WriteFile(path, saved, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if receiptJobs() != 4 {
				t.Fatalf("ACK/readback/verify created jobs: %d", receiptJobs())
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

func TestG06FingerprintHelper(t *testing.T) {
	if os.Getenv("AW_ESCALATION_HELPER") != "1" {
		return
	}
	args := os.Args
	for len(args) > 0 && args[0] != "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		os.Exit(5)
	}
	input, _ := io.ReadAll(os.Stdin)
	if task, err := os.ReadFile(filepath.Join(".woody", "TASK-CODE-WORKER.md")); err == nil && strings.Contains(string(input), string(task)) {
		fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"INLINE_TASK_PRESENT"}}`)
	}
	joined := strings.Join(args[1:], " ")
	if strings.Contains(joined, "model_reasoning_effort=low") {
		fmt.Println(`{"type":"turn.failed","error":{"message":"HTTP 429 rate limit"}}`)
		os.Exit(1)
	}
	if !strings.Contains(joined, "model_reasoning_effort=medium") {
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
	fmt.Println(`{"type":"turn.completed"}`)
	os.Exit(0)
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

func TestDW32IntegrationNestedProduct(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "air-worker")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	for n, b := range map[string]string{".gitignore": ".woody/\n", "a.go": "package demo\nfunc A() string { return \"OLD_A\" }\n", "b.go": "package demo\nfunc B() string { return \"OLD_B\" }\n"} {
		if err := os.WriteFile(filepath.Join(root, n), []byte(b), 0600); err != nil {
			t.Fatal(err)
		}
	}
	codingFixtureGit(t, repo, "init")
	codingFixtureGit(t, repo, "config", "--local", "user.name", "Test")
	codingFixtureGit(t, repo, "config", "--local", "user.email", "test@example.invalid")
	codingFixtureGit(t, repo, "add", ".")
	codingFixtureGit(t, repo, "commit", "-m", "base")
	t.Setenv("AW_DW32_NESTED_HELPER", "1")
	old := runnerCommand
	defer func() { runnerCommand = old }()
	runnerCommand = func(string, ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestDW32NestedHelperProcess")
	}
	c := &loopCtx{Root: root, Subagents: 2, Principal: "gpt", SessionKey: "nested-fixture", Cfg: runConfig{Orchestration: orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}}}
	task := codingMicrostepTask(t)
	step := "nested-task"
	doc := c.runIsolatedCodingWorkers("codex", task, step, runnerSpec{Kind: "codex", Model: "gpt-6-luna", Effort: "low"})
	if doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" {
		t.Fatalf("handoff: %+v", doc)
	}
	for _, w := range doc.Workers {
		codingFixtureGit(t, repo, "apply", "--check", w.Patch)
		codingFixtureGit(t, repo, "apply", w.Patch)
	}
	if _, err := verifyCodingIntegrated(root, c.scope(), step, task); err != nil {
		t.Fatalf("exact nested integration rejected: %v", err)
	}

	codingFixtureGit(t, repo, "add", "air-worker/a.go")
	if _, err := verifyCodingIntegrated(root, c.scope(), step, task); err == nil {
		t.Fatal("staged source unexpectedly accepted")
	}
	codingFixtureGit(t, repo, "reset", "--", "air-worker/a.go")
	if err := os.WriteFile(filepath.Join(repo, "outside.txt"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := verifyCodingIntegrated(root, c.scope(), step, task); err == nil {
		t.Fatal("unrelated repository change accepted")
	}
}

func TestDW32NestedHelperProcess(t *testing.T) {
	if os.Getenv("AW_DW32_NESTED_HELPER") != "1" {
		return
	}
	root, err := os.Getwd()
	if err != nil {
		os.Exit(3)
	}
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil || os.WriteFile(filepath.Join(root, ".woody", "test-stdin"), stdin, 0600) != nil {
		os.Exit(5)
	}
	switch filepath.Base(filepath.Dir(root)) {
	case "worker-1":
		_ = os.WriteFile(filepath.Join(root, "a.go"), []byte("package demo\nfunc A() string { return \"NEW_A\" }\n"), 0600)
	case "worker-2":
		_ = os.WriteFile(filepath.Join(root, "b.go"), []byte("package demo\nfunc B() string { return \"NEW_B\" }\n"), 0600)
	default:
		os.Exit(4)
	}
	fmt.Println(`{"type":"thread.started","thread_id":"nested-helper"}`)
	fmt.Println(`{"type":"item.completed","item":{"type":"agent_message","text":"Edited"}}`)
	fmt.Println(`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`)
	os.Exit(0)
}

func TestDW32N139BoundCoordinatorAck(t *testing.T) {
	for _, principal := range []string{"gpt", "codex"} {
		t.Run(principal, func(t *testing.T) { testDW32N139BoundCoordinatorAck(t, principal) })
	}
}

func testDW32N139BoundCoordinatorAck(t *testing.T, principal string) {
	repo := t.TempDir()
	root := filepath.Join(repo, "air-worker")
	for _, d := range []string{"cmd", "plan", "goal", "docs/tasks"} {
		if e := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0700); e != nil {
			t.Fatal(e)
		}
	}
	task := codingMicrostepTask(t)
	name := "docs/tasks/N139.json"
	cfg := executorTestConfig()
	cfg.Orchestration = orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}
	cfgRaw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	release, e := os.ReadFile(filepath.Join("..", "plan", "release-slices.json"))
	if e != nil {
		t.Fatal(e)
	}
	node := fmt.Sprintf("---\nid: %q\ntitle: \"N139\"\nparent: %q\ntrigger: \"test\"\nowner: \"test\"\ndone_when: \"test\"\nstatus: \"open\"\nreturn_to: \"test\"\ncreated_at: \"2026-11-07T03:07:15Z\"\nupdated_at: \"2026-11-07T03:07:15Z\"\nreceipts:\n---\n\n<!-- air-worker-coding-task: %s sha256=%s -->\n", n139ID, "N-136_prove-autonomous-plan-coding-factual-anthropic-c", name, sha256Hex([]byte(task)))
	for n, b := range map[string][]byte{".gitignore": []byte(".woody/\n/goal/operational-goal.json\n"), "run-config.json": cfgRaw, "cmd/go.mod": []byte("module airos/air-worker\n\ngo 1.26\n"), "PLAN.md": []byte("[[" + n139ID + "]]\n"), "plan/" + n139ID + ".md": []byte(node), "plan/release-slices.json": release, name: []byte(task), "a.go": []byte("package demo\nfunc A() string { return \"OLD_A\" }\n"), "b.go": []byte("package demo\nfunc B() string { return \"OLD_B\" }\n")} {
		if e := os.WriteFile(filepath.Join(root, filepath.FromSlash(n)), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	codingFixtureGit(t, repo, "init")
	codingFixtureGit(t, repo, "config", "--local", "user.name", "Test")
	codingFixtureGit(t, repo, "config", "--local", "user.email", "test@example.invalid")
	codingFixtureGit(t, repo, "add", ".")
	codingFixtureGit(t, repo, "commit", "-m", "bound plan")
	head := n124Git(t, repo, "rev-parse", "HEAD")
	goal, e := operationalGoalInputs(root, n139ID, head)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(goal)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(operationalGoalPath(root), raw, 0600); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AW_DW32_NESTED_HELPER", "1")
	old := runnerCommand
	defer func() { runnerCommand = old }()
	runnerCommand = func(string, ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestDW32NestedHelperProcess")
	}
	c := &loopCtx{Root: root, Subagents: 2, Principal: principal, SessionKey: "n139-fixture", Cfg: runConfig{Orchestration: orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}}}
	doc := c.runIsolatedCodingWorkers("codex", task, n139ID, runnerSpec{Kind: "codex", Model: "gpt-6-luna", Effort: "low"})
	if doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" {
		t.Fatalf("handoff: %+v", doc)
	}
	for _, w := range doc.Workers {
		stdin, err := os.ReadFile(filepath.Join(w.Product, ".woody", "test-stdin"))
		if err != nil {
			t.Fatal(err)
		}
		taskPath := filepath.Join(w.Product, ".woody", "TASK-CODE-WORKER.md")
		workerTask, err := os.ReadFile(taskPath)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := readJobReceipt(w.Receipt)
		if err != nil || receipt == nil {
			t.Fatalf("receipt: %+v, %v", receipt, err)
		}
		version, coordinator := "v2", "The ChatGPT conversation is your coordinator."
		if principal == "codex" {
			version, coordinator = "v3", "The Codex conversation is your coordinator."
		}
		if !strings.Contains(string(stdin), coordinator) || !strings.HasSuffix(receipt.Operation, "-"+version+"-task-"+shaCodingBytes([]byte(task))) || receipt.StdinSHA256 != shaCodingBytes(stdin) {
			t.Fatalf("%s prompt or receipt mismatch: operation=%s stdin=%s", principal, receipt.Operation, stdin)
		}
		if principal == "gpt" && string(stdin) != ponytailPrompt(codingWorkerPrompt(taskPath, string(workerTask))) {
			t.Fatal("GPT stdin changed from the v2 prompt")
		}
		codingFixtureGit(t, repo, "apply", "--check", w.Patch)
		codingFixtureGit(t, repo, "apply", w.Patch)
	}
	for _, mutation := range []string{"version", "stdin"} {
		path := doc.Workers[0].Receipt
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		receipt, err := readJobReceipt(path)
		if err != nil {
			t.Fatal(err)
		}
		if mutation == "version" {
			if principal == "codex" {
				receipt.Operation = strings.Replace(receipt.Operation, "-v3-task-", "-v2-task-", 1)
			} else {
				receipt.Operation = strings.Replace(receipt.Operation, "-v2-task-", "-v3-task-", 1)
			}
		} else {
			receipt.StdinSHA256 = strings.Repeat("0", 64)
		}
		if err := writeJobReceipt(path, receipt); err != nil {
			t.Fatal(err)
		}
		if _, err := ackCodingIntegrated(root, c.scope(), n139ID, task); err == nil {
			t.Fatalf("%s mutation accepted before ACK", mutation)
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	foreignBeforeAck := c.scope()
	if principal == "codex" {
		foreignBeforeAck.ID.Principal = "gpt"
	} else {
		foreignBeforeAck.ID.Principal = "codex"
	}
	if _, err := ackCodingIntegrated(root, foreignBeforeAck, n139ID, task); err == nil {
		t.Fatal("foreign principal acknowledged jobs")
	}
	if _, e := ackCodingIntegrated(root, c.scope(), n139ID, task+" "); e == nil {
		t.Fatal("modified task accepted")
	}
	selected, selectErr := selectN139CodingTask(root, goal)
	if selectErr != nil || selected != task {
		t.Fatalf("direct selector: %v", selectErr)
	}
	state := t.TempDir()
	id := sessionIdentity{Principal: principal, SessionKey: "n139-fixture"}
	if e = writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}); e != nil {
		t.Fatal(e)
	}
	if e = writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, Principal: id.Principal, SessionKey: id.SessionKey}); e != nil {
		t.Fatal(e)
	}
	out, e := os.CreateTemp(t.TempDir(), "ack-output-*.json")
	if e != nil {
		t.Fatal(e)
	}
	oldStdout := os.Stdout
	os.Stdout = out
	rc := cmdOrchestrateCode([]string{"ack-integration", "-product", root, "-task-file", filepath.Join(root, filepath.FromSlash(name)), "-step", n139ID, "-principal", id.Principal, "-session-key", id.SessionKey, "-state-dir", state, "-json"})
	os.Stdout = oldStdout
	out.Close()
	if rc != 0 {
		t.Fatalf("native ack rc=%d", rc)
	}
	ack := filepath.Join(filepath.Dir(doc.Report), "coordinator-integration.json")
	before, e := os.ReadFile(ack)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ackCodingIntegrated(root, c.scope(), n139ID, task); e == nil {
		t.Fatal("duplicate acknowledgement accepted")
	}
	after, e := os.ReadFile(ack)
	if e != nil || !bytes.Equal(before, after) {
		t.Fatal("acknowledgement changed")
	}
	receipts := [2][]byte{}
	for i, w := range doc.Workers {
		var readErr error
		receipts[i], readErr = os.ReadFile(w.Receipt)
		if readErr != nil {
			t.Fatal(readErr)
		}
	}
	var jobs atomic.Int32
	runnerCommand = func(string, ...string) *exec.Cmd {
		jobs.Add(1)
		return exec.Command(os.Args[0], "-test.run=TestDW32NestedHelperProcess")
	}
	read := func() (codingIntegrationAck, error) {
		t.Helper()
		return readCodingIntegrationAck(root, c.scope(), n139ID, task)
	}
	readCLI := func() (int, map[string]string) {
		t.Helper()
		f, err := os.CreateTemp(t.TempDir(), "ack-status-*.json")
		if err != nil {
			t.Fatal(err)
		}
		prev := os.Stdout
		os.Stdout = f
		rc := cmdOrchestrateCode([]string{"ack-status", "-product", root, "-task-file", filepath.Join(root, filepath.FromSlash(name)), "-step", n139ID, "-principal", id.Principal, "-session-key", id.SessionKey, "-state-dir", state, "-json"})
		os.Stdout = prev
		if _, err = f.Seek(0, io.SeekStart); err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		var result map[string]string
		if err = json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		return rc, result
	}
	if rc, result := readCLI(); rc != 0 || result["status"] != "ACK_RECORDED" || result["ack_sha256"] != shaCodingBytes(before) {
		t.Fatalf("native ack status: rc=%d %+v", rc, result)
	}
	valid, e := read()
	if e != nil {
		t.Fatalf("valid acknowledgement rejected: %v", e)
	}
	var recorded codingIntegrationAck
	if valid.AckSHA256 != shaCodingBytes(before) {
		t.Fatal("ack SHA mismatch")
	}
	valid.AckSHA256 = ""
	if e := json.Unmarshal(before, &recorded); e != nil || !reflect.DeepEqual(valid, recorded) {
		t.Fatalf("acknowledgement fields changed: got %+v, want %+v: %v", valid, recorded, e)
	}
	if valid.Status != "ACK_RECORDED" || valid.Step != n139ID || valid.TaskSHA256 != shaCodingBytes([]byte(task)) ||
		valid.SourceHead != doc.SourceHead || valid.HandoffSHA256 == "" ||
		valid.JobIDs != [2]string{doc.Workers[0].JobID, doc.Workers[1].JobID} ||
		valid.PatchSHA256 != [2]string{doc.Workers[0].PatchSHA256, doc.Workers[1].PatchSHA256} {
		t.Fatalf("incomplete acknowledgement readback: %+v", valid)
	}
	checkNoReplay := func(label string) {
		t.Helper()
		current, err := os.ReadFile(ack)
		if err != nil || !bytes.Equal(current, before) || jobs.Load() != 0 {
			t.Fatalf("%s changed acknowledgement or started a worker: %v, jobs=%d", label, err, jobs.Load())
		}
		for i, w := range doc.Workers {
			current, err := os.ReadFile(w.Receipt)
			if err != nil || !bytes.Equal(current, receipts[i]) {
				t.Fatalf("%s changed worker %d receipt: %v", label, i+1, err)
			}
		}
	}
	checkNoReplay("valid readback")
	for label, foreign := range map[string]sessionScope{
		"different session": func() sessionScope { s := c.scope(); s.ID.SessionKey = "foreign"; return s }(),
		"other namespace": func() sessionScope {
			s := c.scope()
			if principal == "codex" {
				s.ID.Principal = "gpt"
			} else {
				s.ID.Principal = "codex"
			}
			return s
		}(),
		"arbitrary principal": func() sessionScope { s := c.scope(); s.ID.Principal = "other"; return s }(),
	} {
		if got, err := readCodingIntegrationAck(root, foreign, n139ID, task); err == nil && got.Status == "ACK_RECORDED" {
			t.Errorf("%s read another coordinator's acknowledgement", label)
		}
		if _, err := ackCodingIntegrated(root, foreign, n139ID, task); err == nil {
			t.Errorf("%s acknowledged another coordinator's jobs", label)
		}
		checkNoReplay(label)
	}
	if e := os.Remove(ack); e != nil {
		t.Fatal(e)
	}
	if rc, result := readCLI(); rc != 0 || result["status"] != "WAIT_COORDINATOR_INTEGRATION" {
		t.Fatalf("native waiting status: rc=%d %+v", rc, result)
	}
	if waiting, e := read(); e != nil || waiting.Status != "WAIT_COORDINATOR_INTEGRATION" {
		t.Fatal("missing acknowledgement did not remain waiting")
	}
	for name, body := range map[string][]byte{
		"empty": {}, "partial": []byte(`{"schema":"air-worker.coordinator-integration/v1"}`),
		"duplicate key": append([]byte(`{"status":"FORGED",`), before[1:]...),
	} {
		if e := os.WriteFile(ack, body, 0600); e != nil {
			t.Fatal(e)
		}
		if name == "partial" {
			if rc, result := readCLI(); rc == 0 || result["status"] != "NOT_PROVEN" {
				t.Fatalf("native partial status: rc=%d %+v", rc, result)
			}
		}
		if _, e := read(); e == nil {
			t.Errorf("%s acknowledgement accepted", name)
		}
	}
	for name, change := range map[string]func(*codingIntegrationAck){
		"status":    func(a *codingIntegrationAck) { a.Status = "FORGED" },
		"principal": func(a *codingIntegrationAck) { a.Principal = "other" },
		"task":      func(a *codingIntegrationAck) { a.TaskSHA256 = strings.Repeat("0", 64) },
		"session":   func(a *codingIntegrationAck) { a.SessionKey = "other" },
		"step":      func(a *codingIntegrationAck) { a.Step = "coord-other" },
		"source":    func(a *codingIntegrationAck) { a.SourceHead = strings.Repeat("0", 40) },
		"job":       func(a *codingIntegrationAck) { a.JobIDs[0] = "other" },
		"patch":     func(a *codingIntegrationAck) { a.PatchSHA256[0] = strings.Repeat("0", 64) },
		"handoff":   func(a *codingIntegrationAck) { a.HandoffSHA256 = strings.Repeat("0", 64) },
	} {
		tampered := recorded
		change(&tampered)
		body, err := json.Marshal(tampered)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ack, body, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := read(); err == nil {
			t.Errorf("changed %s accepted", name)
		}
	}
	if e := os.WriteFile(ack, before, 0600); e != nil {
		t.Fatal(e)
	}
	sealed, e := readJobReceipt(doc.Workers[0].Receipt)
	if e != nil {
		t.Fatal(e)
	}
	for name, path := range map[string]string{
		"handoff":       doc.Report,
		"worker output": sealed.OutputPath,
	} {
		original, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := read(); err == nil {
			t.Errorf("damaged %s accepted", name)
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if e := os.WriteFile(filepath.Join(root, "a.go"), []byte("package demo\nfunc A() string { return \"CHANGED\" }\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if name == "partial" {
		if rc, result := readCLI(); rc == 0 || result["status"] != "NOT_PROVEN" {
			t.Fatalf("native partial status: rc=%d %+v", rc, result)
		}
	}
	if _, e := read(); e == nil {
		t.Fatal("changed Git state accepted")
	}
	checkNoReplay("tampered evidence")
	if e := os.WriteFile(filepath.Join(root, "a.go"), []byte("package demo\nfunc A() string { return \"NEW_A\" }\n"), 0600); e != nil {
		t.Fatal(e)
	}
	original := recorded
	original.AckSHA256 = shaCodingBytes(before)
	evidence := map[string][]byte{}
	for _, path := range []string{ack, doc.Report, doc.Workers[0].Receipt, doc.Workers[0].Patch, sealed.OutputPath} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		evidence[path] = body
	}
	if got := n124Git(t, repo, "rev-parse", "HEAD"); got != head {
		t.Fatalf("fixture base moved before commit: %s", got)
	}
	codingFixtureGit(t, repo, "add", "air-worker/a.go", "air-worker/b.go")
	codingFixtureGit(t, repo, "commit", "-m", "exact worker patches")
	committed := n124Git(t, repo, "rev-parse", "HEAD")
	if committed == head {
		t.Fatal("exact commit did not advance HEAD")
	}
	assertGoalPinned := func(label string) {
		t.Helper()
		current, err := os.ReadFile(operationalGoalPath(root))
		if err != nil || !bytes.Equal(current, raw) {
			t.Fatalf("%s advanced operational goal: %v", label, err)
		}
	}
	verify := func(label string, scope sessionScope, step, task string, ack codingIntegrationAck, accepted bool) {
		t.Helper()
		got, err := verifyCodingCommitted(root, scope, step, task, ack)
		if accepted && (err != nil || got != committed) {
			t.Fatalf("%s: exact commit rejected: head=%q err=%v", label, got, err)
		}
		if !accepted && err == nil {
			t.Fatalf("%s: tampered commit accepted: %s", label, got)
		}
		assertGoalPinned(label)
		checkNoReplay(label)
		for path, want := range evidence {
			current, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(current, want) {
				t.Fatalf("%s changed evidence %s: %v", label, path, err)
			}
		}
	}
	verify("clean N139 direct child", c.scope(), n139ID, task, original, true)
	verify("changed task", c.scope(), n139ID, task+" ", original, false)
	verify("changed node", c.scope(), "coord-other", task, original, false)
	foreign := c.scope()
	foreign.ID.SessionKey = "foreign"
	verify("foreign selector", foreign, n139ID, task, original, false)
	foreign = c.scope()
	if principal == "codex" {
		foreign.ID.Principal = "gpt"
	} else {
		foreign.ID.Principal = "codex"
	}
	verify("other namespace", foreign, n139ID, task, original, false)
	foreign.ID.Principal = "other"
	verify("wrong principal", foreign, n139ID, task, original, false)
	stale := original
	stale.SourceHead = committed
	verify("stale selector", c.scope(), n139ID, task, stale, false)
	stale = original
	stale.AckSHA256 = strings.Repeat("0", 64)
	verify("wrong ACK SHA", c.scope(), n139ID, task, stale, false)
	for label, path := range map[string]string{
		"ack": ack, "receipt": doc.Workers[0].Receipt, "patch": doc.Workers[0].Patch,
		"handoff": doc.Report, "output": sealed.OutputPath,
	} {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := verifyCodingCommitted(root, c.scope(), n139ID, task, original)
		if err == nil {
			t.Fatalf("damaged %s accepted: %s", label, got)
		}
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		assertGoalPinned(label)
		checkNoReplay(label)
	}
	for _, tc := range []struct{ label, path string }{
		{"unstaged", filepath.Join(root, "a.go")},
		{"untracked", filepath.Join(root, "extra.txt")},
		{"unrelated", filepath.Join(repo, "outside.txt")},
	} {
		if err := os.WriteFile(tc.path, []byte("changed"), 0600); err != nil {
			t.Fatal(err)
		}
		verify(tc.label, c.scope(), n139ID, task, original, false)
		if tc.label == "unstaged" {
			codingFixtureGit(t, repo, "restore", "air-worker/a.go")
		} else {
			if err := os.Remove(tc.path); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, repo, "add", "air-worker/a.go")
	verify("staged", c.scope(), n139ID, task, original, false)
	codingFixtureGit(t, repo, "restore", "--staged", "air-worker/a.go")
	codingFixtureGit(t, repo, "restore", "air-worker/a.go")
	codingFixtureGit(t, repo, "commit", "--allow-empty", "-m", "extra commit")
	verify("extra commit", c.scope(), n139ID, task, original, false)
	codingFixtureGit(t, repo, "reset", "--hard", committed)
	if err := os.WriteFile(filepath.Join(repo, "outside.txt"), []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, repo, "add", "outside.txt")
	codingFixtureGit(t, repo, "commit", "-m", "extra file")
	verify("extra committed file", c.scope(), n139ID, task, original, false)
	codingFixtureGit(t, repo, "reset", "--hard", committed)
	codingFixtureGit(t, repo, "checkout", "-b", "side", head)
	codingFixtureGit(t, repo, "commit", "--allow-empty", "-m", "side parent")
	codingFixtureGit(t, repo, "checkout", "-")
	codingFixtureGit(t, repo, "merge", "--no-ff", "-m", "merge parent", "side")
	verify("merge parent", c.scope(), n139ID, task, original, false)
}
