package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func deferredReviewEvents(t *testing.T, runtime, runID string) []map[string]any {
	t.Helper()
	var selected []map[string]any
	var err error
	for attempt := 0; attempt < 12; attempt++ {
		selected = nil
		err = scanLearnJSONL(filepath.Join(runtime, "events.jsonl"), func(raw []byte) error {
			var row map[string]any
			if decodeErr := json.Unmarshal(raw, &row); decodeErr != nil {
				return decodeErr
			}
			if row["kind"] == "run_completed" && row["run_id"] == runID {
				selected = append(selected, row)
			}
			return nil
		})
		if (errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33))) && attempt < 11 {
			// A concurrent Windows atomic journal replacement can deny
			// readers briefly; reset partial results on retry.
			time.Sleep(10 * time.Millisecond)
			continue
		}
		break
	}
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func TestAirWorkerStopDefersSlowReviewUntilBothOwnersAreDurable(t *testing.T) {
	self, selfSettings, stateDir := selfLearningFixture(t)
	target, targetSettings := sharedProductFixture(t, false)

	// The configured reviewer would take ten seconds if called synchronously.
	selfSettings.Reviewer = learningAdapterFixture(t, "timeout")
	if err := writeSharedLearningSettings(self, selfSettings); err != nil {
		t.Fatal(err)
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("self selector: %v", err)
	}
	sel.ConfigSHA256, err = selfLearningConfigSHA(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	targetSettings.Reviewer = learningAdapterFixture(t, "timeout")
	if err := writeSharedLearningSettings(target, targetSettings); err != nil {
		t.Fatal(err)
	}

	in := declareLearningTestSession(t, stateDir, "gpt", "slow-review-stop", target)
	in.RunID = "slow-review-stop-run"
	in.LastAssistantMessage = "artifact reconciled"
	var started []string
	startedAt := time.Now()
	_, err = handleStopLearningWithReviewStarter(in, func(product, runID string) error {
		if runID != in.RunID {
			t.Fatalf("dispatch identity %q", runID)
		}
		for _, runtime := range []string{selfSettings.RuntimeRoot, targetSettings.RuntimeRoot} {
			if got := len(deferredReviewEvents(t, runtime, in.RunID)); got != 1 {
				t.Fatalf("review began before dual completion: runtime=%s count=%d", runtime, got)
			}
			if artifacts, _ := filepath.Glob(filepath.Join(runtime, "reviews", "RV-*.json")); len(artifacts) != 0 {
				t.Fatalf("reviewer ran before dispatch: %v", artifacts)
			}
		}
		started = append(started, product)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(startedAt); elapsed > 4*time.Second {
		t.Fatalf("5-second Stop hook endangered: %s", elapsed)
	}
	if len(started) != 2 || started[0] != self || started[1] != target {
		t.Fatalf("deferred review starts=%v", started)
	}
}

func TestAirWorkerStopDeferredReviewRecoversAfterFailedLaunchAndReplay(t *testing.T) {
	self, selfSettings, stateDir := selfLearningFixture(t)
	target, targetSettings := sharedProductFixture(t, true)
	in := declareLearningTestSession(t, stateDir, "gpt", "recovery-stop", target)
	in.RunID = "recovery-stop-run"
	in.LastAssistantMessage = "artifact reconciliation was completed"

	launches := 0
	_, err := handleStopLearningWithReviewStarter(in, func(_, _ string) error {
		launches++
		return errors.New("simulated child launch failure")
	})
	if err == nil || launches != 1 {
		t.Fatalf("failed dispatch not surfaced: err=%v launches=%d", err, launches)
	}
	for _, runtime := range []string{selfSettings.RuntimeRoot, targetSettings.RuntimeRoot} {
		if got := len(deferredReviewEvents(t, runtime, in.RunID)); got != 1 {
			t.Fatalf("launch failure lost completion at %s: %d", runtime, got)
		}
	}
	var recovered []string
	if _, err := handleLearningContextWithReviewStarter(in, func(product, runID string) error {
		if runID != in.RunID {
			t.Fatalf("recovered run id=%q", runID)
		}
		recovered = append(recovered, product)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(recovered) != 2 || recovered[0] != self || recovered[1] != target {
		t.Fatalf("new session did not recover both reviews: %v", recovered)
	}
	for _, item := range []struct {
		product  string
		settings sharedLearningSettings
	}{
		{self, selfSettings}, {target, targetSettings},
	} {
		res, err := executeSharedLearning(item.product, item.settings, "review", map[string]string{"run_id": in.RunID})
		if err != nil || (res.Status != "candidate" && res.Status != "no_candidate") {
			t.Fatalf("deferred review did not finish: product=%s result=%+v err=%v", item.product, res, err)
		}
		pending, err := pendingSharedLearningReviewRuns(item.product)
		if err != nil || len(pending) != 0 {
			t.Fatalf("completed review still pending: %s %v %v", item.product, pending, err)
		}
	}
	if _, err := handleStopLearningWithReviewStarter(in, nil); err != nil {
		t.Fatalf("duplicate stop should be idempotent: %v", err)
	}
	for _, runtime := range []string{selfSettings.RuntimeRoot, targetSettings.RuntimeRoot} {
		if got := len(deferredReviewEvents(t, runtime, in.RunID)); got != 1 {
			t.Fatalf("duplicate stop created another completion: %s %d", runtime, got)
		}
	}
}

func TestAirWorkerStopBadSelfConfigCannotSuppressTargetCompletion(t *testing.T) {
	_, _, stateDir := selfLearningFixture(t)
	target, targetSettings := sharedProductFixture(t, false)
	in := declareLearningTestSession(t, stateDir, "gpt", "bad-self-stop", target)
	in.RunID = "bad-self-run"
	if err := os.WriteFile(airWorkerSelfLearningPath(), []byte(`{"enabled":true,"enabled":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := handleStopLearningWithReviewStarter(in, nil)
	if err == nil || !strings.Contains(err.Error(), "self-selector") {
		t.Fatalf("malformed self selector wasn't surfaced: %v", err)
	}
	if got := len(deferredReviewEvents(t, targetSettings.RuntimeRoot, in.RunID)); got != 1 {
		t.Fatalf("target completion suppressed by independent selector: %d", got)
	}
}

func TestAirWorkerStopTargetFailureKeepsSelfCompletionAndDoesNotDispatchReview(t *testing.T) {
	_, selfSettings, stateDir := selfLearningFixture(t)
	target, _ := sharedProductFixture(t, false)
	in := declareLearningTestSession(t, stateDir, "gpt", "bad-target-stop", target)
	in.RunID = "bad-target-run"
	if err := os.WriteFile(filepath.Join(target, sharedLearningConfigFile), []byte("not-json"), 0600); err != nil {
		t.Fatal(err)
	}
	launches := 0
	_, err := handleStopLearningWithReviewStarter(in, func(_, _ string) error { launches++; return nil })
	if err == nil || launches != 0 {
		t.Fatalf("target failure did not defer review safely: err=%v starts=%d", err, launches)
	}
	if got := len(deferredReviewEvents(t, selfSettings.RuntimeRoot, in.RunID)); got != 1 {
		t.Fatalf("self completion was lost when target failed: %d", got)
	}
}
