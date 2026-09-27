//go:build !windows

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
	cmd := exec.Command(self, "learn", "review", "-product", product, "-transcript", transcript, "-session", session, "-review-id", reviewID)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
