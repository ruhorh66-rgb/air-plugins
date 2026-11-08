package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJobStatusReadsRealJudgeNamesAndSeals(t *testing.T) {
	for _, operation := range []string{"тесты", "двигатель достижим, потолки объявлены, планировщик ОС не нужен"} {
		t.Run(operation, func(t *testing.T) {
			root := setupSessionProduct(t)
			scope := newScope(root, sessionIdentity{"gpt", "real-judge-name"})
			const step = "judge"
			t.Setenv("AW_N123_CHILD_MARKER", filepath.Join(root, "child-started"))
			output, err := runReceipted(context.Background(), scope, step, operation,
				exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$"))
			if err != nil || string(output) != jobReceiptStdoutMarker {
				t.Fatalf("run real judge name: output=%q err=%v", output, err)
			}
			receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
			receipt, err := readJobReceipt(receiptPath)
			if err != nil {
				t.Fatal(err)
			}
			got, err := inspectOwnedJob(scope, step, operation)
			if err != nil {
				t.Fatal(err)
			}
			seal := sha256.Sum256(output)
			wantSHA := hex.EncodeToString(seal[:])
			if got.Product != root || got.Principal != scope.ID.Principal || got.SessionKey != scope.ID.SessionKey ||
				got.Step != step || got.Operation != operation || got.JobID != receipt.JobID ||
				got.ReceiptPath != receiptPath || got.OutputPath != outputPath ||
				got.Status != jobStatusDone || !got.Terminal || got.OutputSHA256 != wantSHA ||
				got.OutputBytes != int64(len(output)) || receipt.OutputSHA256 != wantSHA ||
				receipt.OutputBytes != int64(len(output)) {
				t.Fatalf("sealed judge readback mismatch: snapshot=%+v receipt=%+v", got, receipt)
			}
			if err := os.WriteFile(outputPath, []byte("altered output"), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := inspectOwnedJob(scope, step, operation); err == nil {
				t.Fatal("changed terminal output accepted")
			}
		})
	}
}

func TestN123JobStatusReadOnlyTerminalRecovery(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "readback-terminal"})
	step, operation := "N123", "native-codex"
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	r, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	const output = "verified native terminal bytes"
	if err := os.WriteFile(outputPath, []byte(output), 0o600); err != nil {
		lock.release()
		t.Fatal(err)
	}
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		lock.release()
		t.Fatal(err)
	}
	lock.release()
	before, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := inspectOwnedJob(scope, step, operation)
	if err != nil {
		t.Fatal(err)
	}
	wantSHA := sha256.Sum256([]byte(output))
	if got.JobID != r.JobID || got.Status != jobStatusDone || !got.Terminal ||
		got.OutputBytes != int64(len(output)) || got.OutputSHA256 != hex.EncodeToString(wantSHA[:]) {
		t.Fatalf("terminal recovery readback mismatch: %+v", got)
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("status inspection mutated receipt: %v", err)
	}
	if _, newLock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath); err == nil {
		if newLock != nil {
			newLock.release()
		}
		t.Fatal("read-only recovery must not permit repeat execution of terminal job")
	} else if newLock != nil {
		newLock.release()
	}
}

func TestN123JobStatusRunningNeverProvesTerminal(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "readback-running"})
	step, operation := "N123", "native-codex"
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	_, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	if err := os.WriteFile(outputPath, []byte("partial output"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := inspectOwnedJob(scope, step, operation)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != jobStatusRunning || got.Terminal || got.OutputSHA256 != "" ||
		!strings.Contains(got.Reason, "do not redispatch") {
		t.Fatalf("RUNNING receipt misrepresented as terminal: %+v", got)
	}
}

func TestN123JobStatusRejectsForeignReceiptAndMissingLog(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "readback-foreign"})
	step, operation := "N123", "native-codex"
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := &jobReceipt{JobID: "wrong-owned-job", Status: jobStatusDone, Product: root,
		Principal: "gpt", Session: "someone-else", Step: step, Operation: operation, OutputPath: outputPath}
	if err := writeJobReceipt(receiptPath, foreign); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectOwnedJob(scope, step, operation); err == nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("foreign receipt accepted: %v", err)
	}
	if err := os.Remove(receiptPath); err != nil {
		t.Fatal(err)
	}
	own := *foreign
	own.Session = scope.ID.SessionKey
	if err := writeJobReceipt(receiptPath, &own); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectOwnedJob(scope, step, operation); err == nil || !strings.Contains(err.Error(), "output") {
		t.Fatalf("missing terminal output accepted: %v", err)
	}
}

func TestN123JobStatusRejectsMalformedIdentifiers(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "bad-identifiers"})
	for _, bad := range []string{"", "a/b", `a\b`, "..", "a..b", "a\n", "a\t", "a\x00", string([]byte{0xff})} {
		for _, pair := range [][2]string{{bad, "native-codex"}, {"N123", bad}} {
			if _, err := inspectOwnedJob(scope, pair[0], pair[1]); err == nil {
				t.Fatalf("unsafe step/operation was accepted: %q / %q", pair[0], pair[1])
			}
		}
	}
}
