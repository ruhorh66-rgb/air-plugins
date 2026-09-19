//go:build !windows

package main

import (
	"os"
	"syscall"
	"time"
)

func adapterProcessMatches(pid int, receiptStartedAt time.Time) bool {
	if pid <= 0 || receiptStartedAt.IsZero() {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
