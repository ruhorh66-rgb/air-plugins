package main

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const nativePreflightMaxFailureFiles = 256

// findPriorSignedPreflightFailure is a read-only candidate selector for a
// later native validate operation. An immutable CLI failure receipt is
// necessary evidence of an earlier exact invocation, but it is NOT by itself
// evidence that a learned procedure was selected, used, or effective.
//
// The original argv is never stored in LEARN or receipts: the caller's
// newly supplied native argv is matched against the per-user DPAPI-keyed HMAC.
// Unknown/tampered or ambiguous source records fail closed.
func findPriorSignedPreflightFailure(owner selfLearningOwner, argv []string) (ownCLIFailureReceipt, bool, error) {
	var empty ownCLIFailureReceipt
	if len(argv) == 0 || argv[0] != "validate" {
		return empty, false, nil
	}
	root := strings.TrimSpace(owner.Settings.RuntimeRoot)
	if !filepath.IsAbs(root) {
		return empty, false, errors.New("self-learning runtime root is not absolute")
	}
	failureDir := filepath.Join(root, "cli-failures")
	dirInfo, err := os.Lstat(failureDir)
	if errors.Is(err, os.ErrNotExist) {
		return empty, false, nil
	}
	if err != nil {
		return empty, false, err
	}
	if !dirInfo.IsDir() || dirInfo.Mode()&os.ModeSymlink != 0 {
		return empty, false, errors.New("own CLI failure index is not an ordinary directory")
	}
	if _, err := os.Readlink(failureDir); err == nil {
		return empty, false, errors.New("own CLI failure index is a link or reparse directory")
	}
	entries, err := os.ReadDir(failureDir)
	if err != nil {
		return empty, false, err
	}
	if len(entries) > nativePreflightMaxFailureFiles {
		return empty, false, errors.New("owned CLI failures exceed bounded preflight inventory")
	}
	hasJSON := false
	for _, entry := range entries {
		if strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			hasJSON = true
			break
		}
	}
	if !hasJSON {
		return empty, false, nil
	}
	key, err := ownCLIPrivateKey(root)
	if err != nil {
		return empty, false, err
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	want := ownCLIInvocationMAC(key, argv)
	var matched ownCLIFailureReceipt
	count := 0
	for _, entry := range entries {
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(failureDir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil {
			return empty, false, err
		}
		if !info.Mode().IsRegular() || info.Size() > 32*1024 || info.Mode()&os.ModeSymlink != 0 {
			return empty, false, fmt.Errorf("own CLI failure receipt is not a bounded ordinary file: %s", entry.Name())
		}
		if _, err := os.Readlink(path); err == nil {
			return empty, false, errors.New("own CLI failure receipt is a link or reparse file")
		}
		raw, err := readLearningBounded(path, 32*1024)
		if err != nil {
			return empty, false, err
		}
		var receipt ownCLIFailureReceipt
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return empty, false, fmt.Errorf("invalid native CLI failure receipt: %w", err)
		}
		// Historical v1/v2 records cannot supply v3 signed evidence and
		// cannot be silently upgraded by this read-only selector.
		if receipt.Schema != ownCLIFailureSchema {
			continue
		}
		if receipt.InvocationMAC != want || receipt.Action != "validate" {
			continue
		}
		if receipt.RunID == "" || entry.Name() != receipt.RunID+".json" ||
			receipt.Source != "air-worker-cli" || receipt.Principal != "air-worker" ||
			receipt.ExitCode != 2 || receipt.Version != version ||
			receipt.CauseCode != "validate-run-config-missing" ||
			receipt.Observed != ownCLIFailureObservation("validate", 2, receipt.CauseCode) ||
			len(receipt.ReceiptMAC) != 64 ||
			!hmac.Equal([]byte(receipt.ReceiptMAC), []byte(ownCLIReceiptMAC(key, receipt))) {
			return empty, false, errors.New("signed prior native CLI failure is invalid or was altered")
		}
		count++
		if count > 1 {
			return empty, false, errors.New("more than one exact prior native CLI failure; ambiguous preflight history")
		}
		matched = receipt
	}
	return matched, count == 1, nil
}
