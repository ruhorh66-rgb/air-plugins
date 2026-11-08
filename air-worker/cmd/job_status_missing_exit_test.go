package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealedReceiptMissingProcessExitIsUnproven(t *testing.T) {
	for _, tc := range []struct {
		name, status      string
		process, terminal bool
	}{{"done-process", jobStatusDone, true, false}, {"failed-process", jobStatusFailed, true, false}, {"api-only", jobStatusDone, false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "fixture"})
			step, op := "N123", "missing-exit"
			rp, out := jobReceiptPaths(scope, step, op)
			if e := os.MkdirAll(filepath.Dir(rp), 0700); e != nil {
				t.Fatal(e)
			}
			data := []byte("sealed output")
			if e := os.WriteFile(out, data, 0600); e != nil {
				t.Fatal(e)
			}
			hash := sha256.Sum256(data)
			r := &jobReceipt{JobID: "fixture", PID: 123, Product: root, Principal: "gpt", Session: "fixture", Step: step, Operation: op, OutputPath: out, Status: tc.status, OutputSHA256: hex.EncodeToString(hash[:]), OutputBytes: int64(len(data)), ProcessStarted: tc.process, RequestStarted: !tc.process}
			if !tc.process {
				r.PID = 0
			}
			if e := writeJobReceipt(rp, r); e != nil {
				t.Fatal(e)
			}
			before, e := os.ReadFile(rp)
			if e != nil {
				t.Fatal(e)
			}
			s, e := inspectOwnedJob(scope, step, op)
			if e != nil {
				t.Fatal(e)
			}
			if s.Terminal != tc.terminal || s.Status != tc.status || s.OutputSHA256 != r.OutputSHA256 {
				t.Fatalf("unexpected snapshot: %+v", s)
			}
			if tc.process && !strings.Contains(s.Reason, "exit") {
				t.Fatalf("missing reason: %+v", s)
			}
			after, e := os.ReadFile(rp)
			if e != nil || string(before) != string(after) {
				t.Fatalf("readback mutated receipt: %v", e)
			}
			_, lock, e := startJobReceipt(scope, step, op, rp, out)
			if lock != nil {
				lock.release()
			}
			if e == nil {
				t.Fatal("duplicate start allowed")
			}
		})
	}
}

func TestSealedReceiptStartedProcessRequiresPositivePID(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pid      int
		process  bool
		terminal bool
		reason   string
	}{{"zero-pid", 0, true, false, "PID"}, {"negative-pid", -1, true, false, "PID"}, {"positive-pid", 123, true, true, ""}, {"api-only", 0, false, true, ""}} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			scope := newScope(root, sessionIdentity{Principal: "gpt", SessionKey: "fixture"})
			step, op := "N123", "pid-"+tc.name
			rp, out := jobReceiptPaths(scope, step, op)
			if e := os.MkdirAll(filepath.Dir(rp), 0700); e != nil {
				t.Fatal(e)
			}
			data := []byte("sealed output")
			if e := os.WriteFile(out, data, 0600); e != nil {
				t.Fatal(e)
			}
			hash := sha256.Sum256(data)
			code := 0
			r := &jobReceipt{JobID: "fixture", PID: tc.pid, Product: root, Principal: "gpt", Session: "fixture", Step: step, Operation: op,
				OutputPath: out, Status: jobStatusDone, ExitCode: &code, OutputSHA256: hex.EncodeToString(hash[:]),
				OutputBytes: int64(len(data)), ProcessStarted: tc.process, RequestStarted: !tc.process}
			if e := writeJobReceipt(rp, r); e != nil {
				t.Fatal(e)
			}
			before, e := os.ReadFile(rp)
			if e != nil {
				t.Fatal(e)
			}
			s, e := inspectOwnedJob(scope, step, op)
			if e != nil {
				t.Fatal(e)
			}
			if s.Terminal != tc.terminal || s.Status != jobStatusDone || s.OutputSHA256 != r.OutputSHA256 {
				t.Fatalf("unexpected snapshot: %+v", s)
			}
			if tc.reason != "" && !strings.Contains(s.Reason, tc.reason) {
				t.Fatalf("missing PID reason: %+v", s)
			}
			after, e := os.ReadFile(rp)
			if e != nil || string(before) != string(after) {
				t.Fatalf("readback mutated receipt: %v", e)
			}
			_, lock, e := startJobReceipt(scope, step, op, rp, out)
			if lock != nil {
				lock.release()
			}
			if e == nil {
				t.Fatal("duplicate start allowed")
			}
		})
	}
}
