package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const n139ID = "N-139_n136-native-operational-goal-selector-for-real-a"

func n139Fixture(t *testing.T) *loopCtx {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"cmd", "plan", "goal"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for path, body := range map[string]string{
		"cmd/go.mod":             "module example.com/n139\n\ngo 1.22\n",
		"cmd/main.go":            "package main\nfunc main() {}\n",
		"cmd/main_test.go":       "package main\nimport \"testing\"\nfunc TestN139Fixture(t *testing.T) {}\n",
		"PLAN.md":                "node remains open\n",
		"plan/" + n139ID + ".md": "node\n",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(path)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return &loopCtx{Root: root, Cfg: runConfig{Ladder: []string{"script", "gpt6-luna:low"}, Runners: map[string]runnerSpec{"gpt6-luna": {Kind: "codex", Model: "gpt-6-luna"}}}}
}

func wrapN139TaskMarker(input string) string {
	var out strings.Builder
	for _, line := range strings.Split(input, "\n") {
		if strings.HasPrefix(line, "coding_task: ") {
			out.WriteString("<!-- air-worker-coding-task: " + strings.TrimPrefix(line, "coding_task: ") + " -->\n")
		} else if line != "" {
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}

func TestN139CodingTaskSelection(t *testing.T) {
	const taskPath = "docs/tasks/N139.json"
	for _, tc := range []struct {
		name   string
		marker func(string) string
		task   string
		mutate func(*testing.T, string)
		want   bool
	}{
		{"exact binding", func(h string) string { return "coding_task: " + taskPath + " sha256=" + h + "\n" }, "", nil, true},
		{"no marker", func(string) string { return "" }, "", nil, false},
		{"duplicate marker", func(h string) string { return strings.Repeat("coding_task: "+taskPath+" sha256="+h+"\n", 2) }, "", nil, false},
		{"wrong hash", func(string) string { return "coding_task: " + taskPath + " sha256=" + strings.Repeat("0", 64) + "\n" }, "", nil, false},
		{"malformed JSON", func(h string) string { return "coding_task: " + taskPath + " sha256=" + h + "\n" }, `{`, nil, false},
		{"untracked task", func(h string) string { return "coding_task: " + taskPath + " sha256=" + h + "\n" }, "", func(t *testing.T, root string) { n124Git(t, root, "rm", "--cached", taskPath) }, false},
		{"modified task", func(h string) string { return "coding_task: " + taskPath + " sha256=" + h + "\n" }, "", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, filepath.FromSlash(taskPath)), []byte(`{}`), 0o644)
		}, false},
		{"mismatched source HEAD", func(h string) string { return "coding_task: " + taskPath + " sha256=" + h + "\n" }, "", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "extra.txt"), []byte("next"), 0o644)
			n124Git(t, root, "add", "extra.txt")
			n124Git(t, root, "commit", "-m", "next")
		}, false},
		{"path traversal", func(h string) string { return "coding_task: docs/tasks/../../outside.json sha256=" + h + "\n" }, "", nil, false},
		{"node modification", func(h string) string { return "coding_task: " + taskPath + " sha256=" + h + "\n" }, "", func(t *testing.T, root string) {
			os.WriteFile(filepath.Join(root, "plan", n139ID+".md"), []byte("changed\n"), 0o644)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := n139Fixture(t)
			if err := os.MkdirAll(filepath.Join(c.Root, "docs", "tasks"), 0o755); err != nil {
				t.Fatal(err)
			}
			task := codingMicrostepTask(t)
			if tc.task != "" {
				task = tc.task
			}
			if err := os.WriteFile(filepath.Join(c.Root, filepath.FromSlash(taskPath)), []byte(task), 0o644); err != nil {
				t.Fatal(err)
			}
			node := fmt.Sprintf("---\nid: %q\nparent: %q\nstatus: open\nowner: test\n---\n%s", n139ID, "N-136_prove-autonomous-plan-coding-factual-anthropic-c", wrapN139TaskMarker(tc.marker(sha256Hex([]byte(task)))))
			if err := os.WriteFile(filepath.Join(c.Root, "plan", n139ID+".md"), []byte(node), 0o644); err != nil {
				t.Fatal(err)
			}
			n124Git(t, c.Root, "init")
			n124Git(t, c.Root, "config", "user.name", "N139")
			n124Git(t, c.Root, "config", "user.email", "n139@test.local")
			n124Git(t, c.Root, "add", ".")
			n124Git(t, c.Root, "commit", "-m", "bound task")
			goal := operationalGoal{NodeID: n139ID, SourceHead: n124Git(t, c.Root, "rev-parse", "HEAD")}
			if tc.mutate != nil {
				tc.mutate(t, c.Root)
			}
			got, err := selectN139CodingTask(c.Root, goal)
			if tc.want {
				if err != nil || got != task {
					t.Fatalf("selected task = %q, err = %v", got, err)
				}
			} else if err == nil || got != "" {
				t.Fatalf("invalid binding selected task = %q, err = %v", got, err)
			}
		})
	}
}

func TestN139AbsentOrStaleSelectorDoesNotDispatch(t *testing.T) {
	c := n139Fixture(t)
	calls := 0
	model := func(workStep, string, string, runnerSpec) stepResult { calls++; return stepResult{Ok: true} }
	review := func(workStep, runnerSpec, semanticFactualPacket, string) semanticRun { calls++; return semanticRun{} }
	for _, reason := range []string{"absent selector", "stale selector"} {
		verify := func(string, operationalGoal) error { return errors.New(reason) }
		if rc := runN139Step(c, operationalGoal{NodeID: n139ID}, verify, model, review); rc != 2 {
			t.Fatalf("%s: RC %d", reason, rc)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid selector dispatched %d calls", calls)
	}
}

func TestN139DoneReceiptDoesNotDispatch(t *testing.T) {
	c := n139Fixture(t)
	runner := resolveRunner(c.Cfg, "gpt6-luna:low")
	operation := runnerReceiptOperation("executor-codex", runner, 0)
	path, output := jobReceiptPaths(c.scope(), n139ID, operation)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("done"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJobReceipt(path, &jobReceipt{JobID: "finished", Product: c.Root, Step: n139ID, Operation: operation, Status: jobStatusDone, OutputPath: output}); err != nil {
		t.Fatal(err)
	}
	called := false
	if rc := runN139Step(c, operationalGoal{NodeID: n139ID}, func(string, operationalGoal) error { return nil },
		func(workStep, string, string, runnerSpec) stepResult { called = true; return stepResult{} },
		func(workStep, runnerSpec, semanticFactualPacket, string) semanticRun {
			called = true
			return semanticRun{}
		}); rc != 2 || called {
		t.Fatalf("DONE receipt redispatched: RC %d called %t", rc, called)
	}
}

func TestN139OrphanOutputDoesNotDispatch(t *testing.T) {
	c := n139Fixture(t)
	runner := resolveRunner(c.Cfg, "gpt6-luna:low")
	_, output := jobReceiptPaths(c.scope(), n139ID, runnerReceiptOperation("executor-codex", runner, 0))
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, []byte("orphan"), 0o600); err != nil {
		t.Fatal(err)
	}
	called := false
	if rc := runN139Step(c, operationalGoal{NodeID: n139ID}, func(string, operationalGoal) error { return nil },
		func(workStep, string, string, runnerSpec) stepResult { called = true; return stepResult{} },
		func(workStep, runnerSpec, semanticFactualPacket, string) semanticRun {
			called = true
			return semanticRun{}
		}); rc != 2 || called {
		t.Fatalf("orphan output redispatched: RC %d called %t", rc, called)
	}
}

func TestN139NonPassAnthropicCannotClose(t *testing.T) {
	c := n139Fixture(t)
	// The semantic-veto test is about the native decision boundary. A real
	// durable Go process is separately verified by TestN139FactualGoReceipt.
	original := n139FactualJudge
	n139FactualJudge = func(*loopCtx, workStep) (semanticFactualPacket, error) {
		return semanticFactualPacket{Scope: "step", Code: 0, Passed: []string{"fixture factual PASS"}}, nil
	}
	t.Cleanup(func() { n139FactualJudge = original })
	verify := func(string, operationalGoal) error { return nil }
	reviews := 0
	rc := runN139Step(c, operationalGoal{NodeID: n139ID}, verify,
		func(workStep, string, string, runnerSpec) stepResult {
			path := filepath.Join(c.Root, "cmd", "main.go")
			if err := os.WriteFile(path, []byte("package main\nfunc main() { println(1) }\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			return stepResult{Ok: true}
		},
		func(_ workStep, runner runnerSpec, fact semanticFactualPacket, _ string) semanticRun {
			reviews++
			if fact.Code != 0 || runner.Kind != "codex" {
				t.Fatalf("bad factual packet %+v runner %+v", fact, runner)
			}
			return semanticRun{Reviewer: runnerSpec{Kind: "claude"}, Verdict: semanticVerdict{Verdict: "NOT_PROVEN"}}
		})
	if rc != 2 || reviews != 1 {
		t.Fatalf("non-PASS reviewer: RC %d reviews %d", rc, reviews)
	}
	raw, err := os.ReadFile(filepath.Join(c.Root, "plan", n139ID+".md"))
	if err != nil || !strings.Contains(string(raw), "node") {
		t.Fatalf("node changed: %q %v", raw, err)
	}
}

func TestN139FactualGoReceiptAndNoDuplicate(t *testing.T) {
	c := n139Fixture(t)
	step := workStep{Num: n139ID}
	fact, err := runN139FactualGo(c, step)
	if err != nil {
		t.Fatalf("native receipted factual Go failed: %v", err)
	}
	if fact.Code != 0 || !strings.Contains(fact.Text, "output_sha256=") {
		t.Fatalf("factual Go did not report independently readable evidence: %+v", fact)
	}
	snapshot, err := inspectOwnedJob(c.scope(), step.Num, "factual-go-test")
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != jobStatusDone || !snapshot.Terminal || snapshot.OutputSHA256 == "" {
		t.Fatalf("factual Go receipt is not terminal with SHA: %+v", snapshot)
	}
	raw, err := os.ReadFile(snapshot.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runN139FactualGo(c, step); err == nil {
		t.Fatal("same factual Go step re-executed after DONE instead of failing closed")
	}
	after, err := os.ReadFile(snapshot.ReceiptPath)
	if err != nil || string(after) != string(raw) {
		t.Fatalf("repeat changed terminal factual receipt: %v", err)
	}
}

func TestN139FactualGoSourceHeadAllowsNewImmutableReceipt(t *testing.T) {
	c := n139Fixture(t)
	n124Git(t, c.Root, "init")
	n124Git(t, c.Root, "config", "user.name", "N139")
	n124Git(t, c.Root, "config", "user.email", "n139@test.local")
	n124Git(t, c.Root, "add", "cmd", "PLAN.md", "plan")
	n124Git(t, c.Root, "commit", "-m", "first source")
	operation := func() string {
		return n123GoJudgeOperation(c.Root, "factual-go-test", exec.Command("go", "-C", "cmd", "test", "-run", n139GoSelector, "-count=1", "."))
	}
	step := workStep{Num: n139ID}
	first := operation()
	if _, err := runN139FactualGo(c, step); err != nil {
		t.Fatal(err)
	}
	snapshot, err := inspectOwnedJob(c.scope(), step.Num, first)
	if err != nil || snapshot.Status != jobStatusDone || !snapshot.Terminal || snapshot.OutputSHA256 == "" {
		t.Fatalf("first receipt is not sealed DONE: %+v %v", snapshot, err)
	}
	raw, err := os.ReadFile(snapshot.ReceiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runN139FactualGo(c, step); err == nil {
		t.Fatal("same HEAD ran twice")
	}
	if after, err := os.ReadFile(snapshot.ReceiptPath); err != nil || string(after) != string(raw) {
		t.Fatalf("duplicate changed receipt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(c.Root, "PLAN.md"), []byte("new source generation\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n124Git(t, c.Root, "add", "PLAN.md")
	n124Git(t, c.Root, "commit", "-m", "second source")
	second := operation()
	if second == first {
		t.Fatal("new HEAD retained the same operation")
	}
	if _, err := runN139FactualGo(c, step); err != nil {
		t.Fatal(err)
	}
	next, err := inspectOwnedJob(c.scope(), step.Num, second)
	if err != nil || next.Status != jobStatusDone || !next.Terminal || next.OutputSHA256 == "" || next.ReceiptPath == snapshot.ReceiptPath {
		t.Fatalf("second receipt is not distinct sealed DONE: %+v %v", next, err)
	}
	if after, err := os.ReadFile(snapshot.ReceiptPath); err != nil || string(after) != string(raw) {
		t.Fatalf("new HEAD changed original receipt: %v", err)
	}
}

func TestN139FactualGoRejectsForeignStepWithoutStartingJob(t *testing.T) {
	c := n139Fixture(t)
	step := workStep{Num: "N-140_other-product"}
	if _, err := runN139FactualGo(c, step); err == nil {
		t.Fatal("foreign N-step was accepted as native factual N139")
	}
	receipt, _ := jobReceiptPaths(c.scope(), step.Num, "factual-go-test")
	if _, err := os.Stat(receipt); !os.IsNotExist(err) {
		t.Fatalf("foreign N-step created a job receipt: %v", err)
	}
}

func TestN139TerminalOutputIntegrityRejectsTamper(t *testing.T) {
	c := n139Fixture(t)
	scope := newScope(c.Root, sessionIdentity{"gpt", "n139-terminal-tamper"})
	const operation = "factual-go-test"
	receiptPath, outputPath := jobReceiptPaths(scope, n139ID, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	_, lock, err := startJobReceipt(scope, n139ID, operation, receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte("trusted native Go test output"), 0o600); err != nil {
		lock.release()
		t.Fatal(err)
	}
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		lock.release()
		t.Fatal(err)
	}
	lock.release()
	// An accepted terminal result is meaningful only if current output bytes
	// still match the digest sealed when the process really completed.
	if err := os.WriteFile(outputPath, []byte("tampered after DONE"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, err := inspectOwnedJob(scope, n139ID, operation); err == nil {
		t.Fatalf("changed terminal output accepted as original job: %+v", got)
	}
}

func TestN139OwnedGoTreeDeadlineHelper(t *testing.T) {
	switch os.Getenv("AW_N139_TREE_HELPER") {
	case "":
		t.Skip("helper process only")
	case "parent":
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		child := exec.Command(exe, "-test.run=^TestN139OwnedGoTreeDeadlineHelper$")
		child.Env = append(os.Environ(), "AW_N139_TREE_HELPER=child")
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(os.Getenv("AW_N139_TREE_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Second)
	case "child":
		time.Sleep(30 * time.Second)
	default:
		t.Fatal("unknown helper mode")
	}
}

func TestN139OwnedGoTreeDeadlineFailsWithoutDuplicate(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NOT_PROVEN: Windows process-tree termination requires Windows")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	pidPath := filepath.Join(root, "child.pid")
	scope := newScope(root, sessionIdentity{"gpt", "n139-tree-deadline"})
	const step, operation = "N139", "factual-go-test"
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestN139OwnedGoTreeDeadlineHelper$")
	cmd.Env = append(os.Environ(), "AW_N139_TREE_HELPER=parent", "AW_N139_TREE_PID="+pidPath)
	configureN139OwnedGoCommand(cmd)
	result := make(chan error, 1)
	go func() {
		_, err := runReceiptedWithMeta(ctx, scope, step, operation, cmd, jobReceiptMeta{})
		result <- err
	}()
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	var childPID string
	var parentPID int
	until := time.Now().Add(550 * time.Millisecond)
	for time.Now().Before(until) {
		if b, err := os.ReadFile(pidPath); err == nil {
			childPID = strings.TrimSpace(string(b))
		}
		if r, err := readJobReceipt(receiptPath); err == nil && r.Status == jobStatusRunning && r.ProcessStarted {
			parentPID = r.PID
		}
		if childPID != "" && parentPID > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if childPID == "" || parentPID <= 0 {
		t.Fatal("NOT_PROVEN: owned tree was not observed RUNNING before deadline")
	}
	select {
	case err := <-result:
		if err == nil || ctx.Err() != context.DeadlineExceeded {
			t.Fatalf("timeout credited as success: %v, context=%v", err, ctx.Err())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("NOT_PROVEN: deadline did not return")
	}
	_ = pollReceiptStatus(t, receiptPath, jobStatusFailed, 5*time.Second)
	view, err := inspectOwnedJob(scope, step, operation)
	if err != nil || !view.Terminal || view.Status != jobStatusFailed || view.ExitCode == nil || *view.ExitCode == 0 {
		t.Fatalf("NOT_PROVEN: timeout receipt was not failed: %+v %v", view, err)
	}
	for _, pid := range []string{strconv.Itoa(parentPID), childPID} {
		out, err := exec.Command("tasklist.exe", "/FI", "PID eq "+pid, "/FO", "CSV", "/NH").CombinedOutput()
		if err != nil || strings.Contains(string(out), `"`+pid+`"`) {
			t.Fatalf("NOT_PROVEN: owned process %s survived or could not be checked: %v %s", pid, err, out)
		}
	}
	beforeReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	again := exec.Command(exe, "-test.run=^TestN139OwnedGoTreeDeadlineHelper$")
	if _, err := runReceiptedWithMeta(context.Background(), scope, step, operation, again, jobReceiptMeta{}); err == nil {
		t.Fatal("failed timeout was credited or dispatched twice")
	}
	afterReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	afterOutput, err := os.ReadFile(outputPath)
	if err != nil || string(beforeReceipt) != string(afterReceipt) || string(beforeOutput) != string(afterOutput) {
		t.Fatalf("duplicate changed timeout evidence: %v", err)
	}
}

func TestN139ManagedSelectionControlsModelDispatch(t *testing.T) {
	c := n139Fixture(t)
	c.Orchestrate = true
	c.Subagents = 2
	c.Cfg.Orchestration = orchestrationSpec{Enabled: true, Subagents: 2, RequireNativeSubagents: true, RequireAnthropicReviewer: true}
	root := c.Root
	n124Git(t, root, "init")
	n124Git(t, root, "config", "user.name", "N139")
	n124Git(t, root, "config", "user.email", "n139@test.local")
	n124Git(t, root, "add", ".")
	n124Git(t, root, "commit", "-m", "unbound")
	goal := operationalGoal{NodeID: n139ID, SourceHead: n124Git(t, root, "rev-parse", "HEAD")}
	calls, reviews := 0, 0
	verify := func(string, operationalGoal) error { return nil }
	model := func(step workStep, _ string, _ string, _ runnerSpec) stepResult {
		calls++
		return stepResult{Subtype: "coordinator_handoff", Detail: step.CodingTask}
	}
	review := func(workStep, runnerSpec, semanticFactualPacket, string) semanticRun { reviews++; return semanticRun{} }
	if rc := runN139Step(c, goal, verify, model, review); rc != 2 || calls != 0 || reviews != 0 {
		t.Fatalf("unbound task dispatched: rc=%d calls=%d reviews=%d", rc, calls, reviews)
	}
	task := codingMicrostepTask(t)
	taskDir := filepath.Join(root, "docs", "tasks")
	if err := os.MkdirAll(taskDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskDir, "N139.json"), []byte(task), 0600); err != nil {
		t.Fatal(err)
	}
	node := filepath.Join(root, "plan", n139ID+".md")
	if err := os.WriteFile(node, []byte("<!-- air-worker-coding-task: docs/tasks/N139.json sha256="+sha256Hex([]byte(task))+" -->\n"), 0600); err != nil {
		t.Fatal(err)
	}
	n124Git(t, root, "add", ".")
	n124Git(t, root, "commit", "-m", "bind task")
	goal.SourceHead = n124Git(t, root, "rev-parse", "HEAD")
	model = func(step workStep, _ string, _ string, _ runnerSpec) stepResult {
		calls++
		if step.CodingTask != task {
			t.Fatal("wrong task passed")
		}
		return stepResult{Subtype: "coordinator_handoff"}
	}
	if rc := runN139Step(c, goal, verify, model, review); rc != 2 || calls != 1 || reviews != 0 {
		t.Fatalf("handoff incorrectly treated as acceptance: rc=%d calls=%d reviews=%d", rc, calls, reviews)
	}
}
