package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const a24ReviewerOut = `{"type":"thread.started","thread_id":"t"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"agent_message","text":"{\"apply\":false,\"content\":\"\"}"}}
{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}
`

// writeReviewerJob seals a reviewer job the way finishJobReceiptWithExit does.
func writeReviewerJob(t *testing.T, scope sessionScope, step, eventSHA, status, output string) (string, string) {
	t.Helper()
	receiptPath, outputPath := jobReceiptPaths(scope, step, "learning-reviewer")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		t.Fatal(err)
	}
	r := &jobReceipt{
		JobID: "job-" + step, StartedAt: time.Now().UTC(), Status: status, OutputPath: outputPath,
		Product: scope.Root, Principal: scope.ID.Principal, Session: scope.ID.SessionKey,
		Step: step, Operation: "learning-reviewer", Role: "learning-reviewer", ReviewTaskSHA256: eventSHA,
		ProcessStarted: true,
	}
	if status != jobStatusRunning {
		r.OutputSHA256 = learnSHA([]byte(output))
	}
	if err := writeJobReceipt(receiptPath, r); err != nil {
		t.Fatal(err)
	}
	return receiptPath, outputPath
}

func TestA24ReviewerJobNamePerEvent(t *testing.T) {
	a, b := learnSHA([]byte("event-a")), learnSHA([]byte("event-b"))
	if learningReviewerStep(a) == learningReviewerStep(b) || !strings.HasPrefix(learningReviewerStep(a), "learning-") {
		t.Fatalf("events share a job name: %s %s", learningReviewerStep(a), learningReviewerStep(b))
	}
	for _, bad := range []string{"", "x", strings.ToUpper(a), a[:63]} {
		if got := learningReviewerStep(bad); got != "learning" {
			t.Fatalf("invalid event SHA %q gave job name %q", bad, got)
		}
	}
}

// The receipt left by the old fixed job name no longer blocks a new review.
func TestA24LegacyReceiptDoesNotBlockNewReview(t *testing.T) {
	scope := legacyScope(t.TempDir())
	writeReviewerJob(t, scope, "learning", "", jobStatusDone, a24ReviewerOut)
	sha := learnSHA([]byte("new-event"))
	out, found, err := recoverLearningReviewerJob(scope, learningReviewerStep(sha), sha)
	if err != nil || found || out != nil {
		t.Fatalf("legacy receipt blocked or answered a new review: found=%v err=%v", found, err)
	}
	newReceipt, _ := jobReceiptPaths(scope, learningReviewerStep(sha), "learning-reviewer")
	oldReceipt, _ := jobReceiptPaths(scope, "learning", "learning-reviewer")
	if newReceipt == oldReceipt {
		t.Fatal("new review would reuse the legacy receipt path")
	}
}

// A retry of the same event takes the sealed result instead of a second run.
func TestA24SameEventRecoversSealedResult(t *testing.T) {
	product := t.TempDir()
	scope := legacyScope(product)
	sha := learnSHA([]byte("same-event"))
	writeReviewerJob(t, scope, learningReviewerStep(sha), sha, jobStatusDone, a24ReviewerOut)
	got, err := invokeLearningReviewerCodex(sha, product, "prompt", "{}", "m", "e")
	if err != nil || got != `{"apply":false,"content":""}` {
		t.Fatalf("sealed result not recovered: %q %v", got, err)
	}
	// Windows paths are case-insensitive: the same product named with other
	// letter case still recovers its own sealed result.
	if runtime.GOOS == "windows" {
		got, err = invokeLearningReviewerCodex(sha, strings.ToUpper(product), "prompt", "{}", "m", "e")
		if err != nil || got != `{"apply":false,"content":""}` {
			t.Fatalf("recovery refused for differently cased product path: %q %v", got, err)
		}
	}
}

func TestA24ForeignOrTamperedEvidenceIsRefused(t *testing.T) {
	sha := learnSHA([]byte("event"))
	other := learnSHA([]byte("other"))
	cases := []struct {
		name   string
		setup  func(scope sessionScope)
		refuse bool
	}{
		{"other event", func(s sessionScope) {
			writeReviewerJob(t, s, learningReviewerStep(sha), other, jobStatusDone, a24ReviewerOut)
		}, true},
		{"failed job", func(s sessionScope) {
			writeReviewerJob(t, s, learningReviewerStep(sha), sha, jobStatusFailed, a24ReviewerOut)
		}, true},
		{"tampered output", func(s sessionScope) {
			_, out := writeReviewerJob(t, s, learningReviewerStep(sha), sha, jobStatusDone, a24ReviewerOut)
			if err := os.WriteFile(out, []byte(strings.Replace(a24ReviewerOut, "false", "true", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
		}, true},
		{"unreadable receipt", func(s sessionScope) {
			receipt, _ := jobReceiptPaths(s, learningReviewerStep(sha), "learning-reviewer")
			_ = os.MkdirAll(filepath.Dir(receipt), 0o700)
			if err := os.WriteFile(receipt, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, true},
	}
	for _, tc := range cases {
		scope := legacyScope(t.TempDir())
		tc.setup(scope)
		_, found, err := recoverLearningReviewerJob(scope, learningReviewerStep(sha), sha)
		if found || (err == nil) == tc.refuse {
			t.Fatalf("%s: found=%v err=%v", tc.name, found, err)
		}
		if reason := learningFailureReason([]byte("learning reviewer: stage=invoke: " + err.Error() + "\n")); reason != "reviewer_job_evidence_mismatch" {
			t.Fatalf("%s: reason %q", tc.name, reason)
		}
	}
}

// A running job is not taken over: the start guard in job_receipt.go still
// refuses a second start, so the evidence protection is unchanged.
func TestA24RunningJobKeepsDuplicateStartGuard(t *testing.T) {
	scope := legacyScope(t.TempDir())
	sha := learnSHA([]byte("running"))
	step := learningReviewerStep(sha)
	writeReviewerJob(t, scope, step, sha, jobStatusRunning, "")
	if _, found, err := recoverLearningReviewerJob(scope, step, sha); found || err != nil {
		t.Fatalf("running job recovered or refused early: found=%v err=%v", found, err)
	}
	_, err := runReceiptedWithMeta(t.Context(), scope, step, "learning-reviewer", exec.Command("cmd", "/c", "exit", "0"), jobReceiptMeta{Role: "learning-reviewer"})
	if err == nil || !strings.Contains(err.Error(), "existing job evidence") {
		t.Fatalf("second start over a running job was not refused: %v", err)
	}
	if reason := learningFailureReason([]byte("learning reviewer: stage=invoke: " + err.Error() + "\n")); reason != "reviewer_job_evidence_exists" {
		t.Fatalf("reason %q", reason)
	}
}
