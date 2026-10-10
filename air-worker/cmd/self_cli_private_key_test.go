package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestN110PrivateHMACDoesNotExposeGuessableArguments(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	args := n110CLIArgs()
	runID := "AW-CLI-N110-DPAPI-PRIVACY-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, runID, nil); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(settings.RuntimeRoot, "cli-failures", runID+".json")
	receiptRaw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec ownCLIFailureReceipt
	if err := json.Unmarshal(receiptRaw, &rec); err != nil {
		t.Fatal(err)
	}
	keyPath := ownCLIPrivateKeyPath(settings.RuntimeRoot)
	if keyPath == receiptPath {
		t.Fatal("key is not stored separately from immutable receipt")
	}
	sealed, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	want := ownCLIInvocationMAC(key, args)
	if rec.InvocationMAC != want || len(want) != 64 || rec.Version != version {
		t.Fatalf("private invocation or version not matched: %+v", rec)
	}
	framed, _ := json.Marshal(args)
	if bytes.Contains(sealed, []byte("WORKER-SECRET")) ||
		bytes.Contains(receiptRaw, []byte("WORKER-SECRET")) ||
		bytes.Contains(receiptRaw, []byte("private-password")) ||
		bytes.Contains(receiptRaw, framed) {
		t.Fatal("private argv was exposed in a stored receipt or key envelope")
	}
	// A public SHA-256 of argv is independently brute-forceable. The recorded
	// HMAC must be different and require the secret key unavailable in receipts.
	publicGuess := learnSHA(append([]byte("air-worker.cli.argv-hmac/v2\x00"), framed...))
	if rec.InvocationMAC == publicGuess {
		t.Fatal("HMAC is equivalent to public argv SHA; offline guessing enabled")
	}
	if runtime.GOOS == "windows" && bytes.Equal(sealed, key) {
		t.Fatal("Windows key was stored without DPAPI encryption")
	}
	for i := range key {
		key[i] = 0
	}
}

func TestN110CrossProcessReplayHelper(t *testing.T) {
	mode := os.Getenv("AIR_N110_REPLAY_TEST")
	if mode == "" {
		return
	}
	run := os.Getenv("AIR_N110_REPLAY_RUN")
	args := n110CLIArgs()
	if mode == "changed" {
		args = append(args, "-product", "different-isolated-product")
	}
	err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil)
	if mode == "same" && err != nil {
		t.Fatalf("same cross-process run must replay: %v", err)
	}
	if mode == "changed" && err == nil {
		t.Fatal("changed argv in fresh process reused prior run")
	}
}

func TestN110PrivateKeyReplayAcrossFreshGoProcesses(t *testing.T) {
	_, settings, stateDir := selfLearningFixture(t)
	run := "AW-CLI-N110-XPROC-002"
	args := n110CLIArgs()
	count := 0
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, func([]sharedDeferredReviewJob) error {
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")
	before, err := os.ReadFile(receiptPath)
	if err != nil || count != 1 {
		t.Fatalf("initial receipt / dispatch missing: %v calls=%d", err, count)
	}
	for _, mode := range []string{"same", "changed"} {
		child := exec.Command(os.Args[0], "-test.run=^TestN110CrossProcessReplayHelper$", "-test.v")
		child.Env = append(os.Environ(),
			hookStateDirEnv+"="+stateDir,
			"AIR_N110_REPLAY_TEST="+mode,
			"AIR_N110_REPLAY_RUN="+run)
		out, err := child.CombinedOutput()
		if err != nil {
			t.Fatalf("fresh process %s failed: %v output=%s", mode, err, out)
		}
	}
	after, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("fresh process replaced immutable receipt: %v", err)
	}
	n, _ := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if n != 1 {
		t.Fatalf("fresh processes produced %d events for one run", n)
	}
}

func TestN110LostPrivateKeyAndVersionMismatchFailClosed(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	args := n110CLIArgs()
	run := "AW-CLI-N110-TAMPER-003"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	recPath := filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")
	raw, err := os.ReadFile(recPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec ownCLIFailureReceipt
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Version = "0.11.9-other-build"
	tampered, _ := json.Marshal(rec)
	if err := os.WriteFile(recPath, tampered, 0600); err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err == nil ||
		!strings.Contains(err.Error(), "conflicting immutable evidence") {
		t.Fatalf("different executable version accepted old run: %v", err)
	}
	if err := os.WriteFile(recPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	keyPath := ownCLIPrivateKeyPath(settings.RuntimeRoot)
	if err := os.WriteFile(keyPath, []byte("corrupted DPAPI envelope"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err == nil {
		t.Fatal("corrupt protected key accepted in replay")
	}
	if err := os.Remove(keyPath); err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err == nil ||
		!strings.Contains(err.Error(), "key missing with prior receipts") {
		t.Fatalf("missing key was silently regenerated: %v", err)
	}
	if _, err := os.Stat(keyPath); !os.IsNotExist(err) {
		t.Fatalf("key was regenerated on conflicting old receipts: %v", err)
	}
	n, _ := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if n != 1 {
		t.Fatalf("tamper caused repeated native events: %d", n)
	}
}
