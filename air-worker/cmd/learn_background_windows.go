//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func startLearnReviewProcess(product, transcript, session, reviewID string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := []string{"learn", "review", "-product", product, "-transcript", transcript, "-session", session, "-review-id", reviewID}
	const (
		createNewProcessGroup  = 0x00000200
		detachedProcess        = 0x00000008
		createBreakawayFromJob = 0x01000000
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
