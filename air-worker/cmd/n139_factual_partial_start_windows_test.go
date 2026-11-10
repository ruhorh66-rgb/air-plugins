//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
)

func TestN139FactualPartialAssignmentFailure(t *testing.T) {
	scope := newScope(t.TempDir(), sessionIdentity{"gpt", "n139-partial-start"})
	const step, operation = "N139", "factual-partial-start"
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	injected := errors.New("injected assignment failure")
	originalAssign := routerAssignJobCall
	t.Cleanup(func() { routerAssignJobCall = originalAssign })
	var originalID string
	var child syscall.Handle
	var seamErr error
	routerAssignJobCall = func(job, process uintptr) (uintptr, uintptr, error) {
		r, err := readJobReceipt(receiptPath)
		if err != nil {
			seamErr = err
		} else {
			originalID = r.JobID
		}
		pid, _, _ := routerGetProcessID.Call(process)
		if pid == 0 {
			seamErr = errors.New("assignment seam received no process ID")
		} else {
			child, err = syscall.OpenProcess(routerSynchronize, false, uint32(pid))
			if err != nil {
				seamErr = err
			}
		}
		return 0, 0, injected
	}
	cmd := exec.Command("cmd.exe", "/c", "echo should-not-run")
	_, err := runReceiptedWithMeta(context.Background(), scope, step, operation, cmd, jobReceiptMeta{Role: "factual-judge"})
	if child != 0 {
		defer syscall.CloseHandle(child)
	}
	if seamErr != nil || originalID == "" || child == 0 {
		t.Fatalf("assignment seam did not observe owned child and receipt: id=%q handle=%v error=%v", originalID, child, seamErr)
	}
	if !errors.Is(err, injected) || cmd.Process == nil || cmd.ProcessState == nil {
		t.Fatalf("assignment failure did not wait for child: error=%v process=%v state=%v", err, cmd.Process, cmd.ProcessState)
	}
	if status, waitErr := syscall.WaitForSingleObject(child, 0); waitErr != nil || status != syscall.WAIT_OBJECT_0 {
		t.Fatalf("owned child remains live: status=%d error=%v", status, waitErr)
	}
	r, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if r.JobID != originalID || r.PID != cmd.Process.Pid || !r.ProcessStarted || r.Status != jobStatusFailed || r.ExitCode == nil || *r.ExitCode != cmd.ProcessState.ExitCode() || r.OutputSHA256 != fmt.Sprintf("%x", sha256.Sum256(output)) || r.OutputBytes != int64(len(output)) {
		t.Fatalf("partial start did not seal original child exit and output: %+v", r)
	}
	if lockHeld(scopedLockName(jobLockKind(step, operation), scope)) {
		t.Fatal("job lock remains held after child cleanup")
	}
	again := exec.Command("cmd.exe", "/c", "echo duplicate")
	if _, err := runReceiptedWithMeta(context.Background(), scope, step, operation, again, jobReceiptMeta{Role: "factual-judge"}); err == nil || again.Process != nil {
		t.Fatalf("duplicate child was accepted or started: %v", err)
	}
}

func TestN139FactualPartialAssignmentAndPIDReceiptFailure(t *testing.T) {
	scope := newScope(t.TempDir(), sessionIdentity{"gpt", "n139-partial-receipt-failure"})
	const step, operation = "N139", "factual-partial-receipt-failure"
	receiptPath, _ := jobReceiptPaths(scope, step, operation)
	assignmentErr := errors.New("injected assignment failure")
	writeErr := errors.New("injected PID receipt failure")
	originalAssign, originalWrite := routerAssignJobCall, writeJobReceiptFunc
	t.Cleanup(func() {
		routerAssignJobCall = originalAssign
		writeJobReceiptFunc = originalWrite
	})
	var before []byte
	var originalID string
	var child syscall.Handle
	var seamErr error
	var failedPID int
	routerAssignJobCall = func(job, process uintptr) (uintptr, uintptr, error) {
		before, seamErr = os.ReadFile(receiptPath)
		if seamErr == nil {
			var r *jobReceipt
			r, seamErr = readJobReceipt(receiptPath)
			if seamErr == nil {
				originalID = r.JobID
			}
		}
		pid, _, _ := routerGetProcessID.Call(process)
		if pid == 0 {
			seamErr = errors.New("assignment seam received no process ID")
		} else {
			var err error
			child, err = syscall.OpenProcess(routerSynchronize, false, uint32(pid))
			if err != nil {
				seamErr = err
			}
		}
		return 0, 0, assignmentErr
	}
	writeJobReceiptFunc = func(path string, r *jobReceipt) error {
		if r.ProcessStarted {
			failedPID = r.PID
			return writeErr
		}
		return originalWrite(path, r)
	}
	cmd := exec.Command("cmd.exe", "/c", "echo should-not-run")
	_, err := runReceiptedWithMeta(context.Background(), scope, step, operation, cmd, jobReceiptMeta{Role: "factual-judge"})
	if child != 0 {
		defer syscall.CloseHandle(child)
	}
	if seamErr != nil || originalID == "" || child == 0 || cmd.Process == nil || failedPID != cmd.Process.Pid {
		t.Fatalf("failure seams did not observe original receipt and owned child: id=%q handle=%v pid=%d error=%v", originalID, child, failedPID, seamErr)
	}
	if !errors.Is(err, assignmentErr) || cmd.ProcessState == nil {
		t.Fatalf("assignment failure did not wait for child: error=%v state=%v", err, cmd.ProcessState)
	}
	if status, waitErr := syscall.WaitForSingleObject(child, 0); waitErr != nil || status != syscall.WAIT_OBJECT_0 {
		t.Fatalf("owned child remains live: status=%d error=%v", status, waitErr)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	r, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) || r.JobID != originalID || r.Status != jobStatusRunning || r.ProcessStarted || r.PID != os.Getpid() || r.ExitCode != nil {
		t.Fatalf("original receipt changed or recorded a false terminal state: %+v", r)
	}
	lockName := scopedLockName(jobLockKind(step, operation), scope)
	if !lockHeld(lockName) {
		t.Fatal("job lock released without terminal evidence")
	}
	again := exec.Command("cmd.exe", "/c", "echo duplicate")
	if _, err := runReceiptedWithMeta(context.Background(), scope, step, operation, again, jobReceiptMeta{Role: "factual-judge"}); err == nil || again.Process != nil {
		t.Fatalf("duplicate child was accepted or started: %v", err)
	}
	if !lockHeld(lockName) {
		t.Fatal("duplicate attempt released the original job lock")
	}
}
