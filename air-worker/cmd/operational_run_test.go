package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
