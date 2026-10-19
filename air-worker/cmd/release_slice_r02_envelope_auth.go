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

// The R02 outer release envelope is itself a trusted AirWorker-origin record.
// A signed inner CLI failure alone does NOT bind that old run to today's
// candidate binary or release-plan preimage. A separate domain prevents any
// accidental reuse of the native CLI-failure HMAC for release authorization.
const r02ReleaseCaptureMACDomain = "air-worker.release-slice.capture/v2\x00"

func r02ReleaseCaptureMAC(key []byte, receipt releaseSliceCaptureReceipt) (string, error) {
	if len(key) < 32 {
		return "", errors.New("R02: release envelope signer requires a private native key")
	}
	receipt.ReleaseHMAC = ""
	raw, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(r02ReleaseCaptureMACDomain))
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil)), nil
}

func r02ReleaseCaptureShape(owner selfLearningOwner, root string, receipt releaseSliceCaptureReceipt) error {
	normalized, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if owner.Settings.ProductID != "air-worker" || receipt.ProductID != owner.Settings.ProductID ||
		!sameLearningPath(owner.Selector.ProductRoot, normalized) ||
		!sameLearningPath(receipt.ProductRoot, normalized) ||
		receipt.Schema != releaseSliceCaptureSchema || receipt.SliceID != nativeReleaseSliceR02 ||
		receipt.CandidateVersion != version || !n093SafeEventID(receipt.RunID) ||
		receipt.CauseCode != "validate-run-config-missing" ||
		receipt.ValidateExitCode != 2 || receipt.ReplayExitCode != 0 ||
		receipt.EventCount != 1 || receipt.ReplayEventCount != 1 ||
		receipt.CaptureReceiptSHA256 != receipt.ReplayReceiptSHA256 {
		return errors.New("R02: release envelope owner, plan/run identity or native result fields invalid")
	}
	for _, digest := range []string{receipt.PlanBeforeSHA256, receipt.CandidateBinarySHA256,
		receipt.CaptureReceiptSHA256, receipt.ReplayReceiptSHA256} {
		if len(digest) != 64 || strings.ToLower(digest) != digest {
			return errors.New("R02: release envelope digest has invalid length or case")
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return errors.New("R02: release envelope digest is malformed")
		}
	}
	if receipt.TargetRoot != "" {
		if !filepath.IsAbs(receipt.TargetRoot) || filepath.Clean(receipt.TargetRoot) != receipt.TargetRoot ||
			!strings.HasPrefix(filepath.Base(receipt.TargetRoot), "air-worker-r02-capture-") ||
			filepath.Dir(receipt.TargetRoot) == receipt.TargetRoot {
			return errors.New("R02: signed scratch target has an invalid identity")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, receipt.CreatedAtUTC); err != nil {
		return errors.New("R02: release envelope timestamp invalid")
	}
	return nil
}

// Called solely by the native R02 capture boundary, after a real signed
// disposable validate failure and replay. Unit fixtures call the same
// compiled signer with genuine module-owned events and isolated products.
func sealR02ReleaseCaptureReceipt(owner selfLearningOwner, root string, receipt *releaseSliceCaptureReceipt) error {
	if receipt == nil || receipt.ReleaseHMAC != "" {
		return errors.New("R02: missing or previously sealed release envelope")
	}
	normalized, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	receipt.ProductID = owner.Settings.ProductID
	receipt.ProductRoot = filepath.Clean(normalized)
	if err := r02ReleaseCaptureShape(owner, normalized, *receipt); err != nil {
		return err
	}
	plan, err := os.ReadFile(filepath.Join(normalized, "plan", "release-slices.json"))
	if err != nil || learnSHA(plan) != receipt.PlanBeforeSHA256 {
		return errors.New("R02: cannot sign an envelope for a different release-plan preimage")
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	binary, err := os.ReadFile(executable)
	if err != nil || learnSHA(binary) != receipt.CandidateBinarySHA256 {
		return errors.New("R02: cannot sign another candidate executable identity")
	}
	signedRaw, err := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot,
		"cli-failures/"+receipt.RunID+".json", 32*1024)
	if err != nil || learnSHA(signedRaw) != receipt.CaptureReceiptSHA256 {
		return errors.New("R02: cannot sign unrelated original native CLI evidence")
	}
	prior, err := verifyR02CLIReceipt(owner, receipt.RunID, signedRaw)
	if err != nil {
		return fmt.Errorf("R02: original CLI failure signature invalid: %w", err)
	}
	if _, err := r03OriginalEventID(owner, prior, receipt.CaptureReceiptSHA256); err != nil {
		return fmt.Errorf("R02: cannot sign without one module-owned original completion: %w", err)
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	mac, err := r02ReleaseCaptureMAC(key, *receipt)
	n093ZeroKey(key)
	if err != nil {
		return err
	}
	receipt.ReleaseHMAC = mac
	return verifyR02ReleaseCaptureReceipt(owner, normalized, *receipt)
}

// Every native R02 transition and R03 reviewer selection must verify the
// sealed OUTER envelope before consulting an otherwise genuine older run.
// Never "repair" or sign an unsigned envelope on a read or replay path.
func verifyR02ReleaseCaptureReceipt(owner selfLearningOwner, root string, receipt releaseSliceCaptureReceipt) error {
	if err := r02ReleaseCaptureShape(owner, root, receipt); err != nil {
		return err
	}
	if len(receipt.ReleaseHMAC) != 64 || strings.ToLower(receipt.ReleaseHMAC) != receipt.ReleaseHMAC {
		return errors.New("R02: missing or malformed signed release envelope")
	}
	if _, err := hex.DecodeString(receipt.ReleaseHMAC); err != nil {
		return errors.New("R02: release envelope HMAC is not valid hex")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	expected, err := r02ReleaseCaptureMAC(key, receipt)
	n093ZeroKey(key)
	if err != nil {
		return err
	}
	if !hmac.Equal([]byte(receipt.ReleaseHMAC), []byte(expected)) {
		return errors.New("R02: release envelope HMAC fails to bind owner/run/binary/plan")
	}
	return nil
}
