//go:build windows

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func startSharedLearningReviewProcess(product, runID string) error {
	return startSharedLearningReviewBatchProcess([]sharedDeferredReviewJob{{Product: product, RunID: runID}})
}

// ONE Windows CreateProcess call per host hook, irrespective of owners.
// Reviewer callbacks run after both completion events are durable.
func startSharedLearningReviewBatchProcess(jobs []sharedDeferredReviewJob) error {
	if len(jobs) == 0 {
		return nil
	}
	if len(jobs) > 2*sharedReviewRecoveryMaxDispatch {
		return errors.New("too many review jobs")
	}
	for _, job := range jobs {
		if strings.TrimSpace(job.Product) == "" || strings.TrimSpace(job.RunID) == "" {
			return errors.New("deferred review requires product and run_id")
		}
	}
	// Tests exercise the hook inline; never detach a copy of go test's
	// own executable. A separate built CLI integration test covers dispatch.
	if flag.Lookup("test.v") != nil {
		return nil
	}
	b, err := json.Marshal(jobs)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"learn", "review-batch", "-jobs-json", string(b)}
	const (
		createNewProcessGroup  uint32 = 0x00000200
		detachedProcess        uint32 = 0x00000008
		createBreakawayFromJob uint32 = 0x01000000
	)
	start := func(flags uint32) error {
		cmd := exec.Command(self, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := cmd.Start(); err != nil {
			return err
		}
		return cmd.Process.Release()
	}
	if err := start(createNewProcessGroup | detachedProcess | createBreakawayFromJob); err == nil {
		return nil
	}
	return start(createNewProcessGroup | detachedProcess)
}
