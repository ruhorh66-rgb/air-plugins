//go:build windows

package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestN139FactualStartFailureHelper(t *testing.T) {
	if marker := os.Getenv("AW_N139_START_FAILURE_MARKER"); marker != "" {
		if err := os.WriteFile(marker, []byte("started"), 0o600); err != nil {
			os.Exit(2)
		}
	}
}

func TestN139FactualStartFailureDoesNotRedispatch(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"canceled", "missing"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "started")
			scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "n139-start-failure"})
			const step, operation = "N139", "factual-go-test"
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			program := exe
			if mode == "canceled" {
				cancel()
			} else {
				program = filepath.Join(root, "missing.exe")
			}
			cmd := exec.CommandContext(ctx, program, "-test.run=^TestN139FactualStartFailureHelper$")
			cmd.Env = append(os.Environ(), "AW_N139_START_FAILURE_MARKER="+marker)
			meta := jobReceiptMeta{Role: "factual-judge", Sandbox: "owned-product"}
			out, err := runReceiptedWithMeta(ctx, scope, step, operation, cmd, meta)
			if err == nil || cmd.Process != nil || len(out) != 0 {
				t.Fatalf("failed start returned output or launched a child: output=%q process=%v err=%v", out, cmd.Process, err)
			}
			if lockHeld(scopedLockName(jobLockKind(step, operation), scope)) {
				t.Fatal("failed start leaked job lock")
			}
			receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
			receipt, err := readJobReceipt(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.JobID == "" || receipt.Role != meta.Role || receipt.ProcessStarted || receipt.ExitCode != nil || receipt.Status == jobStatusDone {
				t.Fatalf("failed start recorded successful child evidence: %+v", receipt)
			}
			beforeReceipt, err := os.ReadFile(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			beforeOutput, err := os.ReadFile(outputPath)
			if err != nil {
				t.Fatal(err)
			}
			again := exec.CommandContext(context.Background(), exe, "-test.run=^TestN139FactualStartFailureHelper$")
			again.Env = cmd.Env
			out, err = runReceiptedWithMeta(context.Background(), scope, step, operation, again, meta)
			if err == nil || again.Process != nil || len(out) != 0 {
				t.Fatalf("duplicate start accepted: output=%q process=%v err=%v", out, again.Process, err)
			}
			afterReceipt, err := os.ReadFile(receiptPath)
			if err != nil || !bytes.Equal(beforeReceipt, afterReceipt) {
				t.Fatalf("duplicate changed receipt: %v", err)
			}
			afterOutput, err := os.ReadFile(outputPath)
			if err != nil || !bytes.Equal(beforeOutput, afterOutput) {
				t.Fatalf("duplicate changed output: %v", err)
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("child marker exists or cannot be checked: %v", err)
			}
		})
	}
}
