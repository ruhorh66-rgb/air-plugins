package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func reconcileJobFixture(t *testing.T) (sessionScope, string, string, string) {
	t.Helper()
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{"gpt", "n139-reconcile-test"})
	step, operation := "N-139_n136-native-operational-goal-selector-for-real-a", "factual-go-test"
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	_, lock, err := startJobReceipt(scope, step, operation, receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.release() })
	receipt, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt.ProcessStarted = true
	receipt.PID = 99
	if err := writeJobReceipt(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}
	const body = "ok  airos/air-worker 2.0s\n"
	if err := os.WriteFile(outputPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := finishJobReceipt(receiptPath, jobStatusDone); err != nil {
		t.Fatal(err)
	}
	return scope, step, operation, sha256Hex([]byte(body))
}

func TestN139ReconcileEvidenceValidTerminalAndSeal(t *testing.T) {
	scope, step, operation, expected := reconcileJobFixture(t)
	result, err := readN139ReconcileEvidence(scope, step, operation, expected)
	if err != nil {
		t.Fatal(err)
	}
	if result.LegacyUnsealed || result.OutputSHA256 != expected || result.JobID == "" ||
		!strings.Contains(string(result.Output), "airos/air-worker") {
		t.Fatalf("accepted native terminal evidence differs: %+v", result)
	}
}

func TestN139ReconcileEvidenceRejectsWrongHashTamperAndForeign(t *testing.T) {
	scope, step, operation, expected := reconcileJobFixture(t)
	if _, err := readN139ReconcileEvidence(scope, step, operation, strings.Repeat("a", 64)); err == nil {
		t.Fatal("wrong external frozen Go output SHA accepted")
	}
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.WriteFile(outputPath, []byte("tampered later"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readN139ReconcileEvidence(scope, step, operation, expected); err == nil {
		t.Fatal("post-DONE output mutation accepted")
	}
	if err := os.WriteFile(outputPath, []byte("ok  airos/air-worker 2.0s\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	receipt.Session = "foreign"
	if err := writeJobReceipt(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := readN139ReconcileEvidence(scope, step, operation, expected); err == nil {
		t.Fatal("foreign owned-session receipt accepted")
	}
}

func TestN139ReconcileEvidenceFlagsHistoricalUnsealed(t *testing.T) {
	scope, step, operation, expected := reconcileJobFixture(t)
	receiptPath, _ := jobReceiptPaths(scope, step, operation)
	old, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	old.OutputSHA256 = ""
	old.OutputBytes = 0
	if err := writeJobReceipt(receiptPath, old); err != nil {
		t.Fatal(err)
	}
	result, err := readN139ReconcileEvidence(scope, step, operation, expected)
	if err != nil || !result.LegacyUnsealed {
		t.Fatalf("historical unsealed evidence should be accurately flagged: %+v %v", result, err)
	}
}
