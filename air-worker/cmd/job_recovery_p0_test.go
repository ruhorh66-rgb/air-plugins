package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestN123DurableTwoReattachmentsNoDuplicate(t *testing.T) {
	root := setupSessionProduct(t)
	id := sessionIdentity{Principal: "gpt", SessionKey: "n123-two-reconnections"}
	scope := newScope(root, id)
	cmd := slowHelperCmd(t, 900)
	expectedArgs := jobCommandFingerprint(cmd)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := runReceipted(ctx, scope, "N123", "single-native-command", cmd); !errors.Is(err, errJobStillRunning) {
		t.Fatalf("transport loss must preserve the running native job: %v", err)
	}
	receiptPath, outputPath := jobReceiptPaths(scope, "N123", "single-native-command")
	first := pollReceiptStatus(t, receiptPath, jobStatusRunning, 3*time.Second)
	if !first.ProcessStarted || first.PID <= 0 || first.JobID == "" {
		t.Fatalf("missing native process ownership: %+v", first)
	}
	if first.ArgsSHA256 != expectedArgs || len(first.RecoveryRef) < 20 {
		t.Fatalf("missing stable argument / recovery identity: %+v", first)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		// Simulate a fresh chat/transport caller: only read the durable receipt;
		// never redispatch or kill the child process.
		reconnected := newScope(root, sessionIdentity{Principal: id.Principal, SessionKey: id.SessionKey})
		view, err := inspectOwnedJob(reconnected, "N123", "single-native-command")
		if err != nil {
			t.Fatal(err)
		}
		if view.JobID != first.JobID || view.Status != jobStatusRunning || view.Terminal ||
			view.ArgsSHA256 != expectedArgs || view.RecoveryRef != first.RecoveryRef {
			t.Fatalf("reconnection %d did not preserve running job: %+v", attempt, view)
		}
		if _, otherLock, err := startJobReceipt(reconnected, "N123", "single-native-command", receiptPath, outputPath); err == nil {
			if otherLock != nil {
				otherLock.release()
			}
			t.Fatalf("reconnection %d started a duplicate command", attempt)
		} else if otherLock != nil {
			otherLock.release()
		}
	}
	after := pollReceiptStatus(t, receiptPath, jobStatusDone, 5*time.Second)
	if after.JobID != first.JobID || after.ArgsSHA256 != first.ArgsSHA256 ||
		after.OutputSHA256 == "" || after.ExitCode == nil || *after.ExitCode != 0 {
		t.Fatalf("terminal job did not retain exact original identity/exit/seal: %+v", after)
	}
	sha := sha256.Sum256([]byte(jobReceiptStdoutMarker))
	finalView, err := inspectOwnedJob(newScope(root, id), "N123", "single-native-command")
	if err != nil || !finalView.Terminal || finalView.OutputSHA256 != hex.EncodeToString(sha[:]) ||
		finalView.JobID != first.JobID || finalView.ExitCode == nil || *finalView.ExitCode != 0 {
		t.Fatalf("reconnected terminal readback differs from child: %+v %v", finalView, err)
	}
	if _, otherLock, err := startJobReceipt(scope, "N123", "single-native-command", receiptPath, outputPath); err == nil {
		if otherLock != nil {
			otherLock.release()
		}
		t.Fatal("terminal job re-executed on another reconnect")
	} else if otherLock != nil {
		otherLock.release()
	}
}

func TestN123FailingChildExitReceipt(t *testing.T) {
	if os.Getenv("AW_N123_FAILING_CHILD") == "1" {
		os.Stdout.WriteString("AW-N123-EXIT-7")
		os.Exit(7)
	}
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "n123-failure-exit"})
	cmd := exec.Command(os.Args[0], "-test.run=TestN123FailingChildExitReceipt", "--")
	cmd.Env = append(os.Environ(), "AW_N123_FAILING_CHILD=1")
	_, err := runReceipted(context.Background(), scope, "N123", "exit-seven", cmd)
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 7 {
		t.Fatalf("native child exit 7 must be an actual failure: %v", err)
	}
	receipt, err := readJobReceipt(func() string {
		path, _ := jobReceiptPaths(scope, "N123", "exit-seven")
		return path
	}())
	if err != nil || receipt.Status != jobStatusFailed || receipt.ExitCode == nil ||
		*receipt.ExitCode != 7 || receipt.OutputSHA256 == "" {
		t.Fatalf("native terminal FAILURE must preserve actual exit 7 and output: %+v %v", receipt, err)
	}
}

func TestN123CommandAndSourceFingerprint(t *testing.T) {
	a := exec.Command("go", "test", "-run", "TestN123")
	b := exec.Command("go", "test", "-run", "TestN123")
	c := exec.Command("go", "test", "-run", "TestN139")
	if jobCommandFingerprint(a) != jobCommandFingerprint(b) ||
		jobCommandFingerprint(a) == jobCommandFingerprint(c) ||
		len(jobCommandFingerprint(a)) != 64 {
		t.Fatal("bounded native argument SHA must be stable and discriminate differing arguments")
	}
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	head := jobSourceHead(root)
	expected, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil || head != expected || !operationalSHA.MatchString(head) {
		t.Fatalf("current owned source HEAD could not be independently verified: %q / %q / %v", head, expected, err)
	}
	if jobSourceHead(t.TempDir()) != "" {
		t.Fatal("non-Git product must not invent source HEAD")
	}
}

func TestN123FinishRejectsSuccessWithNonzeroActualExit(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "n123-invalid-exit"})
	receiptPath, outputPath := jobReceiptPaths(scope, "N123", "bad-exit")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	_, lock, err := startJobReceipt(scope, "N123", "bad-exit", receiptPath, outputPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	nonzero := 1
	if err := finishJobReceiptWithExit(receiptPath, jobStatusDone, &nonzero); err == nil {
		t.Fatal("DONE with nonzero exit was accepted")
	}
	r, err := readJobReceipt(receiptPath)
	if err != nil || r.Status != jobStatusRunning {
		t.Fatalf("bad finalization mutated running status: %+v %v", r, err)
	}
}

func TestN123NoRawCommandArgsInReceipt(t *testing.T) {
	root := setupSessionProduct(t)
	scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "n123-args-privacy"})
	secret := "DO_NOT_PERSIST_THIS_ARGUMENT"
	cmd := exec.Command(os.Args[0], "-test.run=TestJobReceiptSlowHelperProcess", "--", secret)
	cmd.Env = append(os.Environ(), "AW_JOB_RECEIPT_HELPER=1", "AW_JOB_RECEIPT_HELPER_DELAY_MS=10")
	if _, err := runReceipted(context.Background(), scope, "N123", "privacy", cmd); err != nil {
		t.Fatal(err)
	}
	receiptPath, _ := jobReceiptPaths(scope, "N123", "privacy")
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secret) {
		t.Fatal("raw command argument leaked into durable job receipt")
	}
	if !strings.Contains(string(raw), "args_sha256") {
		t.Fatal("arguments digest missing from native receipt")
	}
}
