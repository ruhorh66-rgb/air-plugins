package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLearningReviewerLockChild(t *testing.T) {
	if os.Getenv("AW_REVIEW_LOCK_CHILD") != "1" {
		return
	}
	if err := os.WriteFile(os.Getenv("AW_REVIEW_LOCK_READY"), []byte("ready"), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	unlock, err := acquireLearningReviewerLock(ctx, os.Getenv("AW_REVIEW_LOCK_PRODUCT"))
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if err := os.WriteFile(os.Getenv("AW_REVIEW_LOCK_MARKER"), []byte("started"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestLearningReviewerLockCrossProcessAndCancel(t *testing.T) {
	root := t.TempDir()
	product := filepath.Join(root, "product")
	other := filepath.Join(root, "other")
	for _, p := range []string{product, other} {
		if err := os.Mkdir(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(root, "started")
	childCtx, stopChildren := context.WithTimeout(context.Background(), 8*time.Second)
	defer stopChildren()
	cmd := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestLearningReviewerLockChild$")
	cmd.Env = append(os.Environ(), "AW_REVIEW_LOCK_CHILD=1", "AW_REVIEW_LOCK_PRODUCT="+product, "AW_REVIEW_LOCK_MARKER="+marker, "AW_REVIEW_LOCK_READY="+filepath.Join(root, "first-ready"))
	firstInput, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = firstInput.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child did not acquire lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	secondMarker := filepath.Join(root, "second")
	secondReady := filepath.Join(root, "second-ready")
	second := exec.CommandContext(childCtx, os.Args[0], "-test.run=^TestLearningReviewerLockChild$")
	second.Env = append(os.Environ(), "AW_REVIEW_LOCK_CHILD=1", "AW_REVIEW_LOCK_PRODUCT="+product, "AW_REVIEW_LOCK_MARKER="+secondMarker, "AW_REVIEW_LOCK_READY="+secondReady)
	secondInput, err := second.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := second.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = secondInput.Close(); _ = second.Process.Kill(); _ = second.Wait() }()
	for {
		if _, err := os.Stat(secondReady); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second child did not reach lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if unlock, err := acquireLearningReviewerLock(ctx, product); unlock != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued deadline: %v", err)
	}
	otherUnlock, err := acquireLearningReviewerLock(context.Background(), other)
	if err != nil {
		t.Fatalf("other product blocked: %v", err)
	}
	otherUnlock()
	// The queued callback must return on cancellation without starting its adapter.
	queuedMarker := filepath.Join(root, "queued-adapter")
	t.Setenv("AW_REVIEW_FAILURE_CHILD", "1")
	t.Setenv("AW_REVIEW_FAILURE_MODE", "success")
	t.Setenv("AW_REVIEW_FAILURE_MARKER", queuedMarker)
	settings := sharedLearningSettings{Reviewer: &learningProcessAdapter{Executable: "@self", SHA256: "@self", Args: []string{"-test.run=^TestLearningFailureAdapterChild$"}}}
	queuedCtx, queuedCancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer queuedCancel()
	if _, err := sharedLearningConfig(product, settings).Reviewer(queuedCtx, json.RawMessage(`{}`)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("queued callback: %v", err)
	}
	if _, err := os.Stat(queuedMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("queued adapter launched")
	}
	if _, err := os.Stat(secondMarker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("second process overlapped first")
	}
	_ = firstInput.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(2 * time.Second)
	for {
		if _, err := os.Stat(secondMarker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second child never acquired released slot")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_ = secondInput.Close()
	if err := second.Wait(); err != nil {
		t.Fatal(err)
	}
	unlock, err := acquireLearningReviewerLock(context.Background(), product)
	if err != nil {
		t.Fatalf("slot leaked: %v", err)
	}
	unlock()
}

func TestLearningFailureReasonBoundedAndRedacted(t *testing.T) {
	for _, tc := range []struct{ stderr, want string }{
		{"", ""},
		{"learning reviewer: model output is not the required JSON object\n", "reviewer_invalid_model_output"},
		{"learning reviewer: invalid JSON input\n", "reviewer_invalid_input"},
		{"learning reviewer: ОТКАЗ: job learning/learning-reviewer уже запущен в scope private\x00legacy — duplicate start внутри owned scope запрещён\n", "reviewer_job_busy"},
		{"learning reviewer: cannot read existing class guidance: open private/path: denied", "reviewer_context_read_failed"},
		{"learning reviewer: Authorization: Bearer syntheticSECRET", "reviewer_failed; detail withheld"},
		{"learning reviewer: {\"api_key\": \"syntheticSECRET\"}", "reviewer_failed; detail withheld"},
		{"learning reviewer: password=\"syntheticSECRET phrase with spaces\"", "reviewer_failed; detail withheld"},
		{"reason: arbitrary syntheticSECRET", "detail withheld"},
		{"unclassified syntheticSECRET", "detail withheld"},
		{"reason: invalid \xff", "detail withheld"},
		{"reason: " + strings.Repeat("я", 10000), "detail withheld"},
		{"learning reviewer: model output is not the required JSON object\nAuthorization: Bearer syntheticSECRET", "reviewer_invalid_model_output"},
	} {
		got := learningFailureReason([]byte(tc.stderr))
		if got != tc.want || strings.Contains(got, "syntheticSECRET") || strings.Contains(got, "private") || len(got) > 128 {
			t.Fatalf("diagnostic got %q, want %q", got, tc.want)
		}
	}
}

func TestLearningFailureAdapterChild(t *testing.T) {
	if os.Getenv("AW_REVIEW_FAILURE_CHILD") != "1" {
		return
	}
	if marker := os.Getenv("AW_REVIEW_FAILURE_MARKER"); marker != "" {
		_ = os.WriteFile(marker, []byte("started"), 0600)
	}
	mode := os.Getenv("AW_REVIEW_FAILURE_MODE")
	switch mode {
	case "exit":
		_, _ = os.Stderr.WriteString("learning reviewer: Authorization: Bearer syntheticSECRET12345678901234567890\n")
		os.Exit(2)
	case "safe-exit":
		_, _ = os.Stderr.WriteString("learning reviewer: model output is not the required JSON object\n")
		os.Exit(2)
	case "empty":
		os.Exit(2)
	case "success":
		_, _ = os.Stdout.WriteString("{}")
		os.Exit(0)
	case "invalid":
		_, _ = os.Stdout.WriteString("not-json")
		os.Exit(0)
	case "timeout":
		time.Sleep(2 * time.Second)
	case "native-invalid-model", "native-duplicate", "native-secret", "native-empty":
		// Run the production adapter command and printer. Only the provider seam
		// is deterministic; the persisted diagnostic contract is not mocked.
		learningReviewerInvoke = func(string, string, string, string) (string, error) {
			switch mode {
			case "native-invalid-model":
				return "not-json", nil
			case "native-duplicate":
				return "", errors.New("ОТКАЗ: job learning/learning-reviewer уже запущен в scope private\x00legacy — duplicate start внутри owned scope запрещён")
			case "native-secret":
				return "", errors.New("Authorization: Bearer syntheticSECRET12345678901234567890")
			default:
				return "{\"apply\":false,\"content\":\"\"}", nil
			}
		}
		os.Exit(cmdLearningReviewerAdapter(nil))
	}
}

func TestLearningAdapterFailureAndPreCancelledStart(t *testing.T) {
	t.Setenv("AW_REVIEW_FAILURE_CHILD", "1")
	product := t.TempDir()
	a := &learningProcessAdapter{Executable: "@self", SHA256: "@self", Args: []string{"-test.run=^TestLearningFailureAdapterChild$"}, TimeoutMS: 5000}
	for _, mode := range []string{"exit", "safe-exit", "empty", "success"} {
		t.Setenv("AW_REVIEW_FAILURE_MODE", mode)
		out, err := runLearningProcess(context.Background(), product, a, json.RawMessage(`{}`))
		if mode == "success" {
			if err != nil || string(out) != "{}" {
				t.Fatalf("success: %s %v", out, err)
			}
			continue
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("exit not wrapped: %v", err)
		}
		if mode == "exit" && (!strings.Contains(err.Error(), "detail withheld") || strings.Contains(err.Error(), "syntheticSECRET")) {
			t.Fatalf("unsafe/missing reason: %v", err)
		}
		if mode == "safe-exit" && !strings.Contains(err.Error(), "reviewer_invalid_model_output") {
			t.Fatalf("safe reason lost: %v", err)
		}
		if mode == "empty" && strings.Contains(err.Error(), "()") {
			t.Fatalf("empty reason: %v", err)
		}
	}
	t.Setenv("AW_REVIEW_FAILURE_MODE", "invalid")
	if _, err := runLearningProcess(context.Background(), product, a, json.RawMessage(`{}`)); err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("protocol category lost: %v", err)
	}
	t.Setenv("AW_REVIEW_FAILURE_MODE", "timeout")
	short := *a
	short.TimeoutMS = 30
	if _, err := runLearningProcess(context.Background(), product, &short, json.RawMessage(`{}`)); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout category lost: %v", err)
	}
	marker := filepath.Join(product, "launch")
	t.Setenv("AW_REVIEW_FAILURE_MARKER", marker)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := runLearningProcess(ctx, product, a, json.RawMessage(`{}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("cancelled child launched")
	}
}

func TestSharedReviewerFailurePersistsLinkedRVAndSuccessNoCandidate(t *testing.T) {
	for _, tc := range []struct{ mode, runID, wantOutcome, wantReason string }{
		{"native-invalid-model", "review-failed", "error", "reviewer_invalid_model_output"},
		{"native-duplicate", "review-busy", "error", "reviewer_job_busy"},
		{"native-secret", "review-secret", "error", "detail withheld"},
		{"native-empty", "review-empty", "no_candidate", ""},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			product, s := sharedProductFixture(t, false)
			t.Setenv("AW_REVIEW_FAILURE_CHILD", "1")
			t.Setenv("AW_REVIEW_FAILURE_MODE", tc.mode)
			s.Reviewer = &learningProcessAdapter{Executable: "@self", SHA256: "@self", Args: []string{"-test.run=^TestLearningFailureAdapterChild$"}, TimeoutMS: 5000}
			res, err := sharedLearningEvent(product, s, tc.runID, "run_completed", "fixture artifact", "evidence", "fixture", "gpt", "session", "", "")
			if err != nil {
				t.Fatal(err)
			}
			rows := deferredReviewEvents(t, s.RuntimeRoot, tc.runID)
			if len(rows) != 1 {
				t.Fatalf("events: %v", rows)
			}
			eventID, _ := rows[0]["event_id"].(string)
			if eventID == "" {
				t.Fatal("event id missing")
			}
			body, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "reviews", eventID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var rv map[string]any
			if err := json.Unmarshal(body, &rv); err != nil {
				t.Fatal(err)
			}
			var event map[string]any
			if err := json.Unmarshal(res.Data, &event); err != nil {
				t.Fatal(err)
			}
			if rv["event_id"] != eventID || rv["run_id"] != tc.runID || rv["outcome"] != tc.wantOutcome || res.Status != "recorded" || event["review_outcome"] != tc.wantOutcome || event["review_id"] != rv["review_id"] {
				t.Fatalf("review linkage/status: response=%+v rv=%s", res, body)
			}
			if tc.wantReason != "" && !strings.Contains(string(body), tc.wantReason) {
				t.Fatalf("diagnostic missing: %s", body)
			}
			if strings.Contains(string(body), "syntheticSECRET") {
				t.Fatal("synthetic credential persisted")
			}
		})
	}
}
