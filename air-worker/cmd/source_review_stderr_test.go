package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceReviewSeparateStderrHelper(t *testing.T) {
	mode := os.Getenv("AW_REVIEW_STDERR_HELPER")
	if mode == "" {
		return
	}
	fmt.Fprintln(os.Stdout, `{"type":"result"}`)
	if mode == "diagnostic" {
		fmt.Fprintln(os.Stderr, "SessionEnd hook failed: Hook cancelled")
	}
}

func TestSourceReviewSeparateStderrReceipt(t *testing.T) {
	for _, mode := range []string{"diagnostic", "silent"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			scope := newScope(root, sessionIdentity{Principal: "codex", SessionKey: "stderr-test"})
			cmd := exec.Command(os.Args[0], "-test.run=^TestSourceReviewSeparateStderrHelper$")
			cmd.Env = append(os.Environ(), "AW_REVIEW_STDERR_HELPER="+mode)
			out, err := runReceiptedWithMeta(context.Background(), scope, "source-review-test", "semantic-reviewer", cmd, jobReceiptMeta{Role: "semantic-reviewer", SeparateStderr: true})
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), `{"type":"result"}`) || strings.Contains(string(out), "Hook cancelled") {
				t.Fatalf("stdout and stderr mixed: %q", out)
			}
			path, output := jobReceiptPaths(scope, "source-review-test", "semantic-reviewer")
			r, err := readJobReceipt(path)
			if err != nil || r.Status != jobStatusDone || r.StderrPath != output+".stderr" {
				t.Fatalf("stderr receipt missing: %+v %v", r, err)
			}
			data, err := os.ReadFile(r.StderrPath)
			if err != nil || r.StderrSHA256 != sha256Hex(data) || r.StderrBytes != int64(len(data)) || (mode == "diagnostic" && !strings.Contains(string(data), "Hook cancelled")) {
				t.Fatalf("stderr was not sealed: %+v %v", r, err)
			}
			checkErr := verifySourceReviewStderr(scope, "source-review-test")
			if (mode == "silent" && checkErr != nil) || (mode == "diagnostic" && checkErr == nil) {
				t.Fatalf("incorrect stderr decision: %v", checkErr)
			}
			if err := os.WriteFile(r.StderrPath, []byte("tampered"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := verifySourceReviewStderr(scope, "source-review-test"); err == nil {
				t.Fatal("tampered stderr accepted")
			}
		})
	}
}

func TestSourceReviewExistingStderrPreventsStart(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{Principal: "codex", SessionKey: "stderr-existing"})
	step, operation := "source-review-existing", "semantic-reviewer"
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := os.MkdirAll(filepath.Dir(outputPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outputPath+".stderr", []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestSourceReviewSeparateStderrHelper$")
	cmd.Env = append(os.Environ(), "AW_REVIEW_STDERR_HELPER=diagnostic")
	if _, err := runReceiptedWithMeta(context.Background(), scope, step, operation, cmd, jobReceiptMeta{Role: "semantic-reviewer", SeparateStderr: true}); err == nil {
		t.Fatal("existing stderr was overwritten")
	}
	r, err := readJobReceipt(receiptPath)
	if err != nil || r.Status != jobStatusFailed || r.ProcessStarted {
		t.Fatalf("pre-start error left misleading receipt: %+v %v", r, err)
	}
	if data, err := os.ReadFile(outputPath + ".stderr"); err != nil || string(data) != "preserve" {
		t.Fatal("existing stderr was modified")
	}
}
