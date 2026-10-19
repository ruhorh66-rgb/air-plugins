package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	r02PendingSchema    = "air-worker.release-slice.r02-pending/v2"
	r02PendingMACDomain = "air-worker.release-slice.r02-pending/v2\x00"
)

func r02PendingMAC(key []byte, pending r02PendingAttempt) (string, error) {
	if len(key) < 32 {
		return "", errors.New("R02 pending authentication requires a private native key")
	}
	pending.PendingHMAC = ""
	raw, err := json.Marshal(pending)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(r02PendingMACDomain))
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func r02PendingShape(owner selfLearningOwner, pending r02PendingAttempt) error {
	if owner.Settings.ProductID != "air-worker" ||
		pending.Schema != r02PendingSchema ||
		pending.SliceID != nativeReleaseSliceR02 ||
		pending.CandidateVersion != version ||
		pending.ProductID != owner.Settings.ProductID ||
		!sameLearningPath(pending.ProductRoot, owner.Selector.ProductRoot) ||
		pending.ProductRoot == "" || !filepath.IsAbs(pending.ProductRoot) ||
		!filepath.IsAbs(pending.TargetRoot) ||
		!releaseCaptureTargetUnderScratch(pending.TargetRoot) ||
		len(pending.BaselineReceipts) > 4096 {
		return errors.New("R02: pending attempt is not bound to this product and scratch target")
	}
	for _, digest := range []string{pending.CandidateBinarySHA256, pending.PlanBeforeSHA256} {
		if len(digest) != 64 || strings.ToLower(digest) != digest {
			return errors.New("R02: pending candidate/plan digest is malformed")
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return errors.New("R02: pending candidate/plan digest is not hexadecimal")
		}
	}
	for name, digest := range pending.BaselineReceipts {
		if filepath.Base(name) != name || name == "" ||
			!strings.HasSuffix(strings.ToLower(name), ".json") ||
			len(name) > 255 || len(digest) != 64 ||
			strings.ToLower(digest) != digest {
			return errors.New("R02: pending CLI inventory entry invalid")
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return errors.New("R02: pending CLI inventory digest invalid")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, pending.StartedAtUTC); err != nil {
		return errors.New("R02: pending attempt timestamp malformed")
	}
	return nil
}

// Seal only at the TRUSTED native R02 attempt-creation boundary, before
// running the child validate. The signed preimage includes baseline receipts,
// release-plan SHA, binary SHA, product and disposable capture target.
// Never generate a MAC for a previously read or recovered pending marker.
func sealR02PendingAttempt(owner selfLearningOwner, pending *r02PendingAttempt) error {
	if pending == nil || pending.PendingHMAC != "" {
		return errors.New("R02: missing or already sealed pending attempt")
	}
	root, err := filepath.Abs(owner.Selector.ProductRoot)
	if err != nil {
		return err
	}
	pending.ProductRoot = filepath.Clean(root)
	pending.ProductID = owner.Settings.ProductID
	if err := r02PendingShape(owner, *pending); err != nil {
		return err
	}
	plan, err := os.ReadFile(filepath.Join(root, "plan", "release-slices.json"))
	if err != nil || learnSHA(plan) != pending.PlanBeforeSHA256 {
		return errors.New("R02: pending signer cannot bind another plan preimage")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(executable)
	if err != nil || learnSHA(binary) != pending.CandidateBinarySHA256 {
		return errors.New("R02: pending signer cannot bind another candidate binary")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	mac, err := r02PendingMAC(key, *pending)
	n093ZeroKey(key)
	if err != nil {
		return err
	}
	pending.PendingHMAC = mac
	return verifyR02PendingAttempt(owner, *pending)
}

// Recovery is verification ONLY: a v1/unsigned or altered pending marker
// is rejected, never silently migrated, resigned, replayed or cleaned up.
func verifyR02PendingAttempt(owner selfLearningOwner, pending r02PendingAttempt) error {
	if err := r02PendingShape(owner, pending); err != nil {
		return err
	}
	if len(pending.PendingHMAC) != 64 || strings.ToLower(pending.PendingHMAC) != pending.PendingHMAC {
		return errors.New("R02: unsigned or malformed pending attempt is not replayable")
	}
	if _, err := hex.DecodeString(pending.PendingHMAC); err != nil {
		return errors.New("R02: pending signature is malformed")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	expected, err := r02PendingMAC(key, pending)
	n093ZeroKey(key)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(pending.PendingHMAC), []byte(expected)) {
		return fmt.Errorf("R02: pending attempt HMAC mismatches source owner, plan, binary, target or receipt inventory")
	}
	return nil
}
