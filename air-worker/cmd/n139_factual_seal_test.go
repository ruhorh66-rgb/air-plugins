package main

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestN139FactualSealHelper(t *testing.T) {
	if os.Getenv("AW_N139_FACTUAL_SEAL_HELPER") != "1" {
		return
	}
	f, err := os.OpenFile(os.Getenv("AW_N139_FACTUAL_SEAL_MARKER"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		os.Exit(2)
	}
	_, _ = f.WriteString("started\n")
	_ = f.Close()
	fmt.Println("owned factual child started")
	time.Sleep(30 * time.Second)
	os.Exit(3)
}

func TestN139FactualTimeoutWaitsForActualSeal(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	marker := filepath.Join(root, "starts")
	scope := newScope(root, sessionIdentity{"gpt", "n139-factual-seal"})
	const step, operation = "N139", "factual-go-test"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestN139FactualSealHelper$")
	cmd.Env = append(os.Environ(), "AW_N139_FACTUAL_SEAL_HELPER=1", "AW_N139_FACTUAL_SEAL_MARKER="+marker)
	configureN139OwnedGoCommand(cmd)
	result := make(chan error, 1)
	go func() {
		_, err := runReceiptedWithMeta(ctx, scope, step, operation, cmd, jobReceiptMeta{})
		result <- err
	}()
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	var initial *jobReceipt
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		r, readErr := readJobReceipt(receiptPath)
		if readErr == nil && r.Status == jobStatusRunning && r.ProcessStarted {
			if b, markerErr := os.ReadFile(marker); markerErr == nil && string(b) == "started\n" {
				initial = r
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if initial == nil || initial.JobID == "" || initial.PID <= 0 {
		t.Fatal("owned child was not observed running before timeout")
	}
	if err := <-result; !errors.Is(err, errJobStillRunning) {
		t.Fatalf("expected transport timeout, got %v", err)
	}
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer waitCancel()
	if err := waitN139FactualSeal(waitCtx, scope, step, operation); err == nil || !strings.Contains(err.Error(), "verified terminal job") {
		t.Fatalf("terminal seal not verified: %v", err)
	}
	sealed, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.JobID != initial.JobID || sealed.PID != initial.PID || sealed.Status != jobStatusFailed || sealed.ExitCode == nil || *sealed.ExitCode == 0 || sealed.OutputSHA256 != fmt.Sprintf("%x", sha256.Sum256(output)) || sealed.OutputBytes != int64(len(output)) {
		t.Fatalf("timeout did not seal the original failed child: %+v", sealed)
	}
	again := exec.Command(exe, "-test.run=^TestN139FactualSealHelper$")
	again.Env = cmd.Env
	if _, err := runReceiptedWithMeta(context.Background(), scope, step, operation, again, jobReceiptMeta{}); err == nil || again.Process != nil {
		t.Fatalf("duplicate child was accepted or started: %v", err)
	}
	starts, err := os.ReadFile(marker)
	if err != nil || string(starts) != "started\n" {
		t.Fatalf("unexpected child starts: %q, %v", starts, err)
	}
}
