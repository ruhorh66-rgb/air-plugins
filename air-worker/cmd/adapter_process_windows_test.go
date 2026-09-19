//go:build windows

package main

import (
	"os"
	"testing"
	"time"
)

func TestAdapterProcessMatchesReceiptStartOnWindows(t *testing.T) {
	if !adapterProcessMatches(os.Getpid(), time.Now().UTC()) {
		t.Fatal("current process must match a fresh receipt")
	}
	if adapterProcessMatches(os.Getpid(), time.Time{}) {
		t.Fatal("missing receipt start time must fail closed")
	}
	if adapterProcessMatches(os.Getpid(), time.Now().UTC().Add(-24*time.Hour)) {
		t.Fatal("a process created after a stale receipt must not count as its worker")
	}
}
