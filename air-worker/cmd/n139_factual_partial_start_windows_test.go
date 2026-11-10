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
