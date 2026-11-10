//go:build windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestJobStatusOwnerExit(t *testing.T) {
	const step, operation = "N123", "owner-exit"
	if root := os.Getenv("AW_STATUS_OWNER_ROOT"); root != "" {
		scope := newScope(root, sessionIdentity{"gpt", "owner-exit"})
		receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
		_, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
		if err != nil || lock == nil {
			t.Fatalf("start receipt: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "ready"), []byte("ready"), 0o600); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(filepath.Join(root, "release")); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal("timed out waiting for release")
	}

	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "owner-exit"})
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestJobStatusOwnerExit$")
	child.Env = append(os.Environ(), "AW_STATUS_OWNER_ROOT="+root)
	var childOutput bytes.Buffer
	child.Stdout, child.Stderr = &childOutput, &childOutput
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- child.Wait() }()
	waited := false
	defer func() {
		_ = os.WriteFile(filepath.Join(root, "release"), []byte("release"), 0o600)
		if !waited {
			<-done
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(root, "ready")); err == nil {
			break
		}
		select {
		case err := <-done:
			waited = true
			t.Fatalf("owner exited before ready: %v; %s", err, childOutput.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for owner")
		}
		time.Sleep(20 * time.Millisecond)
	}
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := readJobReceipt(receiptPath)
	if err != nil || receipt.JobID == "" || receipt.Status != jobStatusRunning || receipt.ProcessStarted || receipt.ExitCode != nil {
		t.Fatalf("invalid pre-start receipt: %+v, %v", receipt, err)
	}
	lockName := scopedLockName(jobLockKind(step, operation), scope)
	if !lockHeld(lockName) {
		t.Fatal("owner mutex is absent")
	}
	live, err := inspectOwnedJob(scope, step, operation)
	if err != nil || live.JobID != receipt.JobID || live.Status != jobStatusRunning || live.Terminal {
		t.Fatalf("live owner status: %+v, %v", live, err)
	}
	if err := os.WriteFile(filepath.Join(root, "release"), []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		waited = true
		t.Fatalf("owner exit: %v; %s", err, childOutput.String())
	}
	waited = true
	if lockHeld(lockName) {
		t.Fatal("owner mutex remains after exit")
	}
	unknown, err := inspectOwnedJob(scope, step, operation)
	if err != nil || unknown.JobID != receipt.JobID || unknown.Status != "EXECUTION_UNKNOWN" || unknown.Terminal || unknown.ExitCode != nil || unknown.OutputSHA256 != "" {
		t.Fatalf("exited owner status: %+v, %v", unknown, err)
	}
	if _, duplicateLock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath); err == nil {
		if duplicateLock != nil {
			duplicateLock.release()
		}
		t.Fatal("duplicate start accepted")
	} else if duplicateLock != nil {
		duplicateLock.release()
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("receipt changed: %v", err)
	}
}
