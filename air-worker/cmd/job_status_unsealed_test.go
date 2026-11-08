package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsealedTerminalReceiptIsUnprovenWithoutReplay(t *testing.T) {
	for _, status := range []string{jobStatusDone, jobStatusFailed} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "fixture"})
			step, operation := "N123", "probe"
			receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
			if err := os.MkdirAll(filepath.Dir(receiptPath), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outputPath, []byte("unsealed output"), 0600); err != nil {
				t.Fatal(err)
			}
			r := &jobReceipt{JobID: "fixture-job", Status: status, Product: root, Principal: "gpt", Session: "fixture", Step: step, Operation: operation, OutputPath: outputPath}
			if err := writeJobReceipt(receiptPath, r); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			got, err := inspectOwnedJob(scope, step, operation)
			if err != nil {
				t.Fatal(err)
			}
			if got.Terminal || got.Status != status || got.OutputSHA256 != "" || !strings.Contains(got.Reason, "unproven") {
				t.Fatalf("unexpected status: %+v", got)
			}
			after, err := os.ReadFile(receiptPath)
			if err != nil || string(after) != string(before) {
				t.Fatalf("readback changed receipt: %v", err)
			}
			_, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
			if lock != nil {
				lock.release()
			}
			if err == nil {
				t.Fatal("unsealed evidence allowed a duplicate start")
			}
		})
	}
}
