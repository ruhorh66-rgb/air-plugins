//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// runReviewerAttempt runs one controlled attempt the way the adapter does.
func runReviewerAttempt(t *testing.T, scope sessionScope, step, sha string, args ...string) ([]byte, error) {
	t.Helper()
	cmd := exec.Command("cmd", append([]string{"/c"}, args...)...)
	cmd.Dir = scope.Root
	return runReceiptedWithMeta(t.Context(), scope, step, "learning-reviewer", cmd,
		jobReceiptMeta{Role: "learning-reviewer", ReviewTaskSHA256: sha})
}

func readFiles(t *testing.T, paths ...string) [][]byte {
	t.Helper()
	var out [][]byte
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	return out
}

func fileExists(p string) bool { _, err := os.Lstat(p); return err == nil }

// A terminally failed attempt, proven by its own receipt, lets the next
// attempt start under a new name; the first receipt and output stay intact.
func TestA25FailedAttemptStartsNextAttempt(t *testing.T) {
	scope := legacyScope(t.TempDir())
	sha := learnSHA([]byte("a25-retry"))
	first := learningReviewerAttemptStep(sha, 1)
	if first != learningReviewerStep(sha) || learningReviewerAttemptStep(sha, 2) != first+"-a2" {
		t.Fatalf("attempt names: %s %s", first, learningReviewerAttemptStep(sha, 2))
	}
	if _, err := runReviewerAttempt(t, scope, first, sha, "exit", "3"); err == nil {
		t.Fatal("controlled attempt must fail")
	}
	receipt1, output1 := jobReceiptPaths(scope, first, "learning-reviewer")
	r, err := readJobReceipt(receipt1)
	if err != nil || r.Status != jobStatusFailed || r.ExitCode == nil || *r.ExitCode != 3 || !r.ProcessTreeVerified || r.OutputSHA256 == "" {
		t.Fatalf("first attempt not sealed as terminal: %+v %v", r, err)
	}
	before := readFiles(t, receipt1, output1)

	_, found, step, err := recoverLearningReviewerJob(scope, sha)
	if err != nil || found || step != first+"-a2" {
		t.Fatalf("next attempt not offered: found=%v step=%q err=%v", found, step, err)
	}
	if _, err := runReviewerAttempt(t, scope, step, sha, "echo", "second"); err != nil {
		t.Fatal(err)
	}
	out, found, got, err := recoverLearningReviewerJob(scope, sha)
	if err != nil || !found || got != step || !strings.Contains(string(out), "second") {
		t.Fatalf("second attempt result not recovered: %q found=%v step=%q err=%v", out, found, got, err)
	}
	for i, b := range readFiles(t, receipt1, output1) {
		if !bytes.Equal(b, before[i]) {
			t.Fatal("first attempt evidence changed")
		}
	}
}

// Two terminal failures exhaust the module's limit: no process starts.
func TestA25TwoFailedAttemptsAreExhausted(t *testing.T) {
	product := t.TempDir()
	scope := legacyScope(product)
	sha := learnSHA([]byte("a25-exhausted"))
	for n := 1; n <= learningReviewerAttempts; n++ {
		if _, err := runReviewerAttempt(t, scope, learningReviewerAttemptStep(sha, n), sha, "exit", "4"); err == nil {
			t.Fatal("controlled attempt must fail")
		}
	}
	_, found, step, err := recoverLearningReviewerJob(scope, sha)
	if err == nil || found || step != "" {
		t.Fatalf("exhausted attempts not refused: found=%v step=%q err=%v", found, step, err)
	}
	if reason := learningFailureReason([]byte("learning reviewer: stage=invoke: " + err.Error() + "\n")); reason != "reviewer_attempts_exhausted" {
		t.Fatalf("reason %q", reason)
	}
	if _, err := invokeLearningReviewerCodex(sha, product, "prompt", "{}", "m", "e"); err == nil || !strings.Contains(err.Error(), "attempts exhausted") {
		t.Fatalf("adapter did not refuse after the limit: %v", err)
	}
	if r, _ := jobReceiptPaths(scope, learningReviewerAttemptStep(sha, 3), "learning-reviewer"); fileExists(r) {
		t.Fatal("a third attempt was started")
	}
}

// A running next attempt is not repeated: the start guard refuses it.
func TestA25RunningAttemptIsNotRepeated(t *testing.T) {
	scope := legacyScope(t.TempDir())
	sha := learnSHA([]byte("a25-running"))
	if _, err := runReviewerAttempt(t, scope, learningReviewerAttemptStep(sha, 1), sha, "exit", "3"); err == nil {
		t.Fatal("controlled attempt must fail")
	}
	second := learningReviewerAttemptStep(sha, 2)
	writeReviewerJob(t, scope, second, sha, jobStatusRunning, "")
	_, found, step, err := recoverLearningReviewerJob(scope, sha)
	if err != nil || found || step != second {
		t.Fatalf("running attempt: found=%v step=%q err=%v", found, step, err)
	}
	if _, err := runReviewerAttempt(t, scope, step, sha, "exit", "0"); err == nil || !strings.Contains(err.Error(), "existing job evidence") {
		t.Fatalf("second start over a running attempt was not refused: %v", err)
	}
}

// A completed first attempt answers a repeated review with its own result.
func TestA25DoneFirstAttemptAnswersRetry(t *testing.T) {
	scope := legacyScope(t.TempDir())
	sha := learnSHA([]byte("a25-done"))
	if _, err := runReviewerAttempt(t, scope, learningReviewerAttemptStep(sha, 1), sha, "echo", "first"); err != nil {
		t.Fatal(err)
	}
	out, found, step, err := recoverLearningReviewerJob(scope, sha)
	if err != nil || !found || step != learningReviewerAttemptStep(sha, 1) || !strings.Contains(string(out), "first") {
		t.Fatalf("done attempt not reused: %q found=%v step=%q err=%v", out, found, step, err)
	}
}

// A FAILED receipt that does not prove its own terminal state, or whose
// evidence was changed, blocks the next attempt instead of being passed over.
func TestA25UnprovenOrTamperedFailureIsMismatch(t *testing.T) {
	sha := learnSHA([]byte("a25-tampered"))
	cases := map[string]func(t *testing.T, scope sessionScope){
		"no tree proof (old receipt)": func(t *testing.T, s sessionScope) {
			writeReviewerJob(t, s, learningReviewerAttemptStep(sha, 1), sha, jobStatusFailed, "old")
		},
		"output changed": func(t *testing.T, s sessionScope) {
			_, _ = runReviewerAttempt(t, s, learningReviewerAttemptStep(sha, 1), sha, "exit", "3")
			_, out := jobReceiptPaths(s, learningReviewerAttemptStep(sha, 1), "learning-reviewer")
			if err := os.WriteFile(out, []byte("forged"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"exit code removed": func(t *testing.T, s sessionScope) {
			_, _ = runReviewerAttempt(t, s, learningReviewerAttemptStep(sha, 1), sha, "exit", "3")
			receipt, _ := jobReceiptPaths(s, learningReviewerAttemptStep(sha, 1), "learning-reviewer")
			r, err := readJobReceipt(receipt)
			if err != nil {
				t.Fatal(err)
			}
			r.ExitCode = nil
			if err := writeJobReceipt(receipt, r); err != nil {
				t.Fatal(err)
			}
		},
		"other event": func(t *testing.T, s sessionScope) {
			_, _ = runReviewerAttempt(t, s, learningReviewerAttemptStep(sha, 1), learnSHA([]byte("other")), "exit", "3")
		},
	}
	for name, setup := range cases {
		scope := legacyScope(t.TempDir())
		setup(t, scope)
		receipt, _ := jobReceiptPaths(scope, learningReviewerAttemptStep(sha, 1), "learning-reviewer")
		before := readFiles(t, receipt)
		_, found, _, err := recoverLearningReviewerJob(scope, sha)
		if found || err == nil {
			t.Fatalf("%s: found=%v err=%v", name, found, err)
		}
		if reason := learningFailureReason([]byte("learning reviewer: stage=invoke: " + err.Error() + "\n")); reason != "reviewer_job_evidence_mismatch" {
			t.Fatalf("%s: reason %q", name, reason)
		}
		if next, _ := jobReceiptPaths(scope, learningReviewerAttemptStep(sha, 2), "learning-reviewer"); fileExists(next) {
			t.Fatalf("%s: a next attempt was started", name)
		}
		if !bytes.Equal(readFiles(t, receipt)[0], before[0]) {
			t.Fatalf("%s: refused receipt changed", name)
		}
	}
}

// Regression of Dev Work32's diagnostic (diagnose_a2_failed_retry_test.go,
// SHA-256 bfe89d73…): with the real learning module, the retry it offers after
// a failed review now reaches a second attempt instead of a mismatch, and the
// first receipt stays byte-identical.
func TestA25ModuleRetryReachesSecondAttempt(t *testing.T) {
	product, settings := sharedProductFixture(t, false)
	cfg := sharedLearningConfig(product, settings)
	calls := 0
	var receiptPath string
	var before []byte
	scope := legacyScope(product)
	cfg.Reviewer = func(ctx context.Context, raw json.RawMessage) (json.RawMessage, error) {
		calls++
		var packet map[string]any
		if err := json.Unmarshal(raw, &packet); err != nil {
			return nil, err
		}
		event, err := json.Marshal(packet["event"])
		if err != nil {
			return nil, err
		}
		sha := learnSHA(event)
		_, found, step, err := recoverLearningReviewerJob(scope, sha)
		if err != nil || found || step != learningReviewerAttemptStep(sha, calls) {
			t.Fatalf("call %d: found=%v step=%q err=%v", calls, found, step, err)
		}
		cmd := exec.Command("cmd", "/c", "exit", "3")
		cmd.Dir = product
		_, runErr := runReceiptedWithMeta(ctx, scope, step, "learning-reviewer", cmd, jobReceiptMeta{Role: "learning-reviewer", ReviewTaskSHA256: sha})
		if runErr == nil {
			t.Fatal("controlled reviewer must fail")
		}
		if calls == 1 {
			receiptPath, _ = jobReceiptPaths(scope, step, "learning-reviewer")
			if before, err = os.ReadFile(receiptPath); err != nil {
				t.Fatal(err)
			}
		} else if _, _, _, err := recoverLearningReviewerJob(scope, sha); err == nil || !strings.Contains(err.Error(), "attempts exhausted") {
			t.Fatalf("limit not reached after two failures: %v", err)
		}
		return nil, runErr
	}
	execOp := func(op string, data any) {
		t.Helper()
		b, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		if res, err := learning.Execute(context.Background(), cfg, learning.Request{Operation: op, Version: learning.Version, Data: b}); err != nil {
			t.Fatalf("%s: %+v %v", op, res, err)
		}
	}
	execOp("observe", map[string]any{"run_id": "a25-retry-regression", "kind": "run_completed", "observed": "controlled failure", "class": "diagnostic", "defer_review": true})
	execOp("review", map[string]string{"run_id": "a25-retry-regression"})
	execOp("review", map[string]string{"run_id": "a25-retry-regression"})
	execOp("review", map[string]string{"run_id": "a25-retry-regression"})
	if calls != 2 {
		t.Fatalf("module must offer exactly two attempts, got %d", calls)
	}
	if after, err := os.ReadFile(receiptPath); err != nil || !bytes.Equal(after, before) {
		t.Fatalf("first attempt receipt changed: %v", err)
	}
}
