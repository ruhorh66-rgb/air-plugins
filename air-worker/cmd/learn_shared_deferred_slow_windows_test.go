//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Only a built CLI is launched: the Go test executable must never become a
// detached review process. This exercises real cross-process Windows locks.
func n082TestHook(t *testing.T, exe string, in hookInput) time.Duration {
	t.Helper()
	packet, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "hook", "Stop")
	cmd.Stdin = bytes.NewReader(packet)
	cmd.Env = os.Environ()
	started := time.Now()
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Stop subprocess err=%v output=%s", err, out)
	}
	if elapsed >= 5*time.Second {
		t.Fatalf("Stop exceeded native five-second hook: %s", elapsed)
	}
	return elapsed
}

func n082ReviewPhase(t *testing.T, runtimeRoot, runID string) string {
	t.Helper()
	rows := deferredReviewEvents(t, runtimeRoot, runID)
	if len(rows) != 1 {
		return ""
	}
	id, _ := rows[0]["event_id"].(string)
	if id == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(runtimeRoot, "reviews", id+".json"))
	if os.IsNotExist(err) || errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33)) {
		return ""
	}
	if err != nil {
		t.Fatal(err)
	}
	var review struct {
		Phase string `json:"phase"`
	}
	if err := json.Unmarshal(raw, &review); err != nil {
		t.Fatal(err)
	}
	return review.Phase
}

func TestN082SecondStopWhilePreviousReviewerIsBlocked(t *testing.T) {
	self, selfSettings, stateDir := selfLearningFixture(t)
	selfSettings.Reviewer = learningAdapterFixture(t, "timeout")
	if err := writeSharedLearningSettings(self, selfSettings); err != nil {
		t.Fatal(err)
	}
	selector, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("selector: %v", err)
	}
	selector.ConfigSHA256, err = selfLearningConfigSHA(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(selector); err != nil {
		t.Fatal(err)
	}
	target, targetSettings := sharedProductFixture(t, true)
	in := declareLearningTestSession(t, stateDir, "claude", "n082-live-review", target)
	in.HookEventName = "Stop"
	in.LastAssistantMessage = "work step finished, pending independent review"
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "air-worker-n082.exe")
	build := exec.Command("go", "build", "-o", exe, ".")
	build.Dir = cwd
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	in.RunID = "n082-running-review-1"
	n082TestHook(t, exe, in)
	deadline := time.Now().Add(8 * time.Second)
	active := false
	for time.Now().Before(deadline) {
		if n082ReviewPhase(t, selfSettings.RuntimeRoot, in.RunID) == "reviewing" {
			active = true
			break
		}
		time.Sleep(40 * time.Millisecond)
	}
	if !active {
		t.Fatal("first review did not enter slow callback")
	}
	// Keep the first reviewer blocked with its already loaded adapter. Switch
	// only the fixture's subsequent runs to a fast reviewer so this integration
	// test proves lock freedom without doubling whole-suite latency.
	selfSettings.Reviewer = learningAdapterFixture(t, "review")
	if err := writeSharedLearningSettings(self, selfSettings); err != nil {
		t.Fatal(err)
	}
	selector, found, err = readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("selector refresh: %v", err)
	}
	selector.ConfigSHA256, err = selfLearningConfigSHA(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(selector); err != nil {
		t.Fatal(err)
	}
	in.RunID = "n082-running-review-2"
	elapsed := n082TestHook(t, exe, in)
	t.Logf("second Stop while first review active: %s", elapsed)
	for name, runtimeRoot := range map[string]string{"self": selfSettings.RuntimeRoot, "target": targetSettings.RuntimeRoot} {
		if count := len(deferredReviewEvents(t, runtimeRoot, in.RunID)); count != 1 {
			t.Fatalf("%s second run_completed count=%d", name, count)
		}
	}
	deadline = time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		allComplete := true
		for _, runID := range []string{"n082-running-review-1", "n082-running-review-2"} {
			for _, runtimeRoot := range []string{selfSettings.RuntimeRoot, targetSettings.RuntimeRoot} {
				if n082ReviewPhase(t, runtimeRoot, runID) != "complete" {
					allComplete = false
				}
			}
		}
		if allComplete {
			time.Sleep(200 * time.Millisecond)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	for _, runID := range []string{"n082-running-review-1", "n082-running-review-2"} {
		for label, runtimeRoot := range map[string]string{"self": selfSettings.RuntimeRoot, "target": targetSettings.RuntimeRoot} {
			t.Logf("unresolved review %s %s phase=%q", label, runID, n082ReviewPhase(t, runtimeRoot, runID))
		}
	}
	t.Fatal("detached reviewers did not reach terminal state; no PASS claimed")
}
