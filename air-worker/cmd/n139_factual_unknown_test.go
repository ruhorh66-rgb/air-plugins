package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestN139FactualUnsealedTimeoutDoesNotRedispatch(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "n139-unknown"})
	const step, operation = "N139", "factual-go-test"
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	receipt, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	locked := true
	defer func() {
		if locked {
			lock.release()
		}
	}()
	receipt.ProcessStarted = true
	if err := writeJobReceipt(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := waitN139FactualSeal(ctx, scope, step, operation); err == nil || !strings.Contains(strings.ToLower(err.Error()), "not proven") {
		t.Fatalf("unsealed wait must report not proven, got %v", err)
	}
	status, err := inspectOwnedJob(scope, step, operation)
	if err != nil || status.Terminal || status.JobID != receipt.JobID {
		t.Fatalf("owned job must remain nonterminal with the same ID: %+v, %v", status, err)
	}
	lock.release()
	locked = false
	if _, duplicateLock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath); err == nil {
		if duplicateLock != nil {
			duplicateLock.release()
		}
		t.Fatal("existing receipt allowed duplicate dispatch")
	} else if duplicateLock != nil {
		duplicateLock.release()
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("wait or duplicate dispatch changed receipt bytes: %v", err)
	}
	got, err := readJobReceipt(receiptPath)
	if err != nil || got.JobID != receipt.JobID || got.Status != jobStatusRunning {
		t.Fatalf("receipt identity or RUNNING status changed: %+v, %v", got, err)
	}
}
