//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func completedSharedReview(t *testing.T, runtime, runID string) bool {
	t.Helper()
	events := deferredReviewEvents(t, runtime, runID)
	if len(events) != 1 {
		return false
	}
	eventID, _ := events[0]["event_id"].(string)
	if eventID == "" {
		return false
	}
	b, err := os.ReadFile(filepath.Join(runtime, "reviews", eventID+".json"))
	if os.IsNotExist(err) || errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33)) {
		// Atomic review/index publication on Windows can temporarily deny readers.
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	var rv struct {
		Phase   string `json:"phase"`
		Outcome string `json:"outcome"`
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(b, &rv); err != nil {
		t.Fatal(err)
	}
	return rv.EventID == eventID && rv.Phase == "complete" &&
		(rv.Outcome == "candidate" || rv.Outcome == "no_candidate")
}

func TestAirWorkerStopDetachedReviewIsRealProcessAndFinishesBothOwners(t *testing.T) {
	_, selfSettings, stateDir := selfLearningFixture(t)
	target, targetSettings := sharedProductFixture(t, true)
	in := declareLearningTestSession(t, stateDir, "claude", "n081-detached-e2e", target)
	in.RunID = "n081-detached-e2e-run"
	in.HookEventName = "Stop"
	in.LastAssistantMessage = "artifact reconciliation was completed"

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	candidate := filepath.Join(t.TempDir(), "air-worker-n081-candidate.exe")
	build := exec.Command("go", "build", "-o", candidate, ".")
	build.Dir = cwd
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("isolated CLI build failed: %v\n%s", err, out)
	}
	packet, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(candidate, "hook", "Stop")
	command.Stdin = bytes.NewReader(packet)
	command.Env = os.Environ()
	began := time.Now()
	output, err := command.CombinedOutput()
	elapsed := time.Since(began)
	t.Logf("isolated Stop duration=%s (limit=5s)", elapsed)
	if err != nil {
		t.Fatalf("isolated Stop exit: %v\n%s", err, output)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("isolated Stop exhausted five-second deadline: %s", elapsed)
	}
	for _, root := range []string{selfSettings.RuntimeRoot, targetSettings.RuntimeRoot} {
		if got := len(deferredReviewEvents(t, root, in.RunID)); got != 1 {
			t.Fatalf("Stop did not persist both owners: %s count=%d", root, got)
		}
	}
	deadline := time.Now().Add(18 * time.Second)
	for time.Now().Before(deadline) {
		if completedSharedReview(t, selfSettings.RuntimeRoot, in.RunID) &&
			completedSharedReview(t, targetSettings.RuntimeRoot, in.RunID) {
			return
		}
		time.Sleep(150 * time.Millisecond)
	}
	for label, runtime := range map[string]string{
		"self": selfSettings.RuntimeRoot, "target": targetSettings.RuntimeRoot,
	} {
		t.Logf("%s review completed=%v", label, completedSharedReview(t, runtime, in.RunID))
	}
	t.Fatal(fmt.Errorf("detached reviewers did not complete both module-owned reviews within bounded wait"))
}
