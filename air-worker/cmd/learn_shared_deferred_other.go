//go:build !windows

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func startSharedLearningReviewProcess(product, runID string) error {
	return startSharedLearningReviewBatchProcess([]sharedDeferredReviewJob{{Product: product, RunID: runID}})
}

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
	b, err := json.Marshal(jobs)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "learn", "review-batch", "-jobs-json", string(b))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
