package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n110CLIArgs() []string {
	return []string{"plan", "node", "new", "-parent", "N-999_missing", "private-password-WORKER-SECRET-DO-NOT-RECORD"}
}

func n110CountFailureEvents(t *testing.T, runtimeRoot, runID string) (int, map[string]any) {
	t.Helper()
	var result map[string]any
	count := 0
	err := scanLearnJSONL(filepath.Join(runtimeRoot, "events.jsonl"), func(raw []byte) error {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		if item["schema"] == "air.learning.event/v1" && item["run_id"] == runID {
			count++
			result = item
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count, result
}

func TestN110OwnCLIFailureDurablyCapturedAndPrivate(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	run := "AW-CLI-N110-test-001"
	started := 0
	trigger := func(jobs []sharedDeferredReviewJob) error {
		started++
		if len(jobs) != 1 || jobs[0].RunID != run || jobs[0].Product != product {
			t.Fatalf("native reviewer dispatch not bound to own self owner: %+v", jobs)
		}
		n, _ := n110CountFailureEvents(t, settings.RuntimeRoot, run)
		if n != 1 {
			t.Fatalf("review dispatch before durable CLI event: %d", n)
		}
		return nil
	}
	if err := autoCaptureOwnCLIExitWithRunID(n110CLIArgs(), 2, run, trigger); err != nil {
		t.Fatal(err)
	}
	if started != 1 {
		t.Fatalf("native deferred review dispatches=%d", started)
	}
	n, event := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if n != 1 || event["kind"] != "run_completed" ||
		event["source"] != "air-worker-cli" ||
		event["class"] != "air-worker-cli-failure" ||
		event["principal"] != "air-worker" {
		t.Fatalf("native event has wrong identity: count=%d event=%v", n, event)
	}
	if strings.Contains(event["observed"].(string), "WORKER-SECRET") ||
		event["outcome"] == "pass" {
		t.Fatalf("CLI event leaked arguments or invented success: %+v", event)
	}
	path := filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt ownCLIFailureReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Schema != ownCLIFailureSchema || receipt.RunID != run ||
		receipt.Action != "plan node new" || receipt.ExitCode != 2 ||
		receipt.Principal != "air-worker" ||
		strings.Contains(string(raw), "WORKER-SECRET") {
		t.Fatalf("invalid or private immutable CLI receipt: %s", raw)
	}
	pending, err := pendingSharedLearningReviewRuns(product)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0] != run {
		t.Fatalf("CLI failure missing from native deferred reviewer recovery: %v", pending)
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("CLI failure must not create false procedure_used count=%d", got)
	}
}

func TestN110ExactIdempotencyAndConflictingRunFailClosed(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	run := "AW-CLI-N110-retry-002"
	calls := 0
	starter := func([]sharedDeferredReviewJob) error { calls++; return nil }
	args := n110CLIArgs()
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, starter); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")
	before, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, starter); err != nil {
		t.Fatalf("same native run replay failed: %v", err)
	}
	after, err := os.ReadFile(original)
	if err != nil || string(after) != string(before) || calls != 1 {
		t.Fatalf("duplicate replaced receipt or relaunched reviewer: calls=%d err=%v", calls, err)
	}
	n, _ := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if n != 1 {
		t.Fatalf("same run emitted %d native events", n)
	}
	if err := autoCaptureOwnCLIExitWithRunID(args, 3, run, starter); err == nil {
		t.Fatal("same stable run accepted conflicting native exit code")
	}
	if err := autoCaptureOwnCLIExitWithRunID([]string{"validate", "-product", "other"}, 2, run, starter); err == nil {
		t.Fatal("same stable run accepted different CLI action")
	}
	if calls != 1 {
		t.Fatal("conflicting run id relaunched reviewer")
	}
}

func TestN110ReviewDispatchFailureLeavesDurablePendingEvent(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	run := "AW-CLI-N110-starter-failed-003"
	err := autoCaptureOwnCLIExitWithRunID([]string{"validate", "-product", product}, 2, run,
		func([]sharedDeferredReviewJob) error { return errors.New("reviewer unavailable") })
	if err == nil || !strings.Contains(err.Error(), "recorded") {
		t.Fatalf("reviewer dispatch failure should be explicit, not hide durable event: %v", err)
	}
	n, event := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if n != 1 || event["kind"] != "run_completed" {
		t.Fatal("review failure lost native event")
	}
	if _, err := os.Stat(filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")); err != nil {
		t.Fatalf("durable failure receipt missing: %v", err)
	}
	pending, err := pendingSharedLearningReviewRuns(product)
	if err != nil || len(pending) != 1 || pending[0] != run {
		t.Fatalf("deferred recoverability lost: pending=%v err=%v", pending, err)
	}
}

func TestN110SuccessAndInternalCommandsNeverPolluteLearn(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	cases := []struct {
		cmd  []string
		exit int
	}{
		{[]string{"validate", "-product", "x"}, 0},
		{[]string{"hook", "Stop"}, 2},
		{[]string{"learn", "review-batch"}, 2},
		{[]string{"learning-adapter"}, 2},
		{[]string{"version"}, 2},
		{[]string{"unknown"}, 2},
	}
	for i, item := range cases {
		called := 0
		err := autoCaptureOwnCLIExitWithRunID(item.cmd, item.exit, "AW-CLI-N110-skipped-00"+string(rune('0'+i)), func([]sharedDeferredReviewJob) error { called++; return nil })
		if err != nil || called != 0 {
			t.Fatalf("ineligible native CLI unexpectedly captured: cmd=%v err=%v calls=%d", item.cmd, err, called)
		}
	}
	path := filepath.Join(settings.RuntimeRoot, "cli-failures")
	if items, err := os.ReadDir(path); err == nil && len(items) != 0 {
		t.Fatalf("nonfailed or internal CLI created %d own failure receipts", len(items))
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "run_completed"); got != 0 {
		t.Fatalf("nonfailed or internal command created %d LEARN run_completed events", got)
	}
}
