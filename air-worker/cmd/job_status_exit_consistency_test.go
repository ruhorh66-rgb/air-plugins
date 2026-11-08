package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestN123JobStatusRejectsDoneWithNonzeroExit(t *testing.T) {
	for _, tc := range []struct {
		name string
		exit int
	}{
		{name: "nonzero", exit: 7},
		{name: "zero", exit: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := setupSessionProduct(t)
			scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "done-exit-" + tc.name})
			const step, operation = "N123", "sealed-done"
			receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
			if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
				t.Fatal(err)
			}
			r, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.release()
			const output = "sealed terminal output"
			if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(outputPath, 0o600); err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256([]byte(output))
			r.ProcessStarted = true
			r.Status = jobStatusDone
			r.ExitCode = &tc.exit
			r.OutputSHA256 = hex.EncodeToString(sum[:])
			r.OutputBytes = int64(len(output))
			if err := writeJobReceipt(receiptPath, r); err != nil {
				t.Fatal(err)
			}
			lock.release()
			before, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}

			got, inspectErr := inspectOwnedJob(scope, step, operation)
			if tc.exit != 0 {
				if inspectErr == nil {
					t.Fatalf("DONE with exit %d accepted: %+v", tc.exit, got)
				}
			} else if inspectErr != nil || !got.Terminal || got.Status != jobStatusDone || got.JobID != r.JobID || got.ExitCode == nil || *got.ExitCode != 0 {
				t.Fatalf("valid sealed DONE not recovered: %+v %v", got, inspectErr)
			}
			after, err := os.ReadFile(receiptPath)
			if err != nil || string(after) != string(before) {
				t.Fatalf("inspection changed receipt bytes: %v", err)
			}
			if _, duplicateLock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath); err == nil {
				if duplicateLock != nil {
					duplicateLock.release()
				}
				t.Fatal("existing receipt allowed duplicate start")
			} else if duplicateLock != nil {
				duplicateLock.release()
			}
		})
	}
}
