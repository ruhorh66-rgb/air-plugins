package main

import (
	"bytes"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

const releaseSliceCaptureSchema = "air-worker.release-slice.capture/v2"

type releaseSliceCaptureReceipt struct {
	Schema                string `json:"schema"`
	SliceID               string `json:"slice_id"`
	ProductID             string `json:"product_id"`
	ProductRoot           string `json:"product_root"`
	TargetRoot            string `json:"target_root,omitempty"`
	ReleaseHMAC           string `json:"release_hmac_sha256,omitempty"`
	CandidateVersion      string `json:"candidate_version"`
	CandidateBinarySHA256 string `json:"candidate_binary_sha256"`
	PlanBeforeSHA256      string `json:"plan_before_sha256"`
	RunID                 string `json:"run_id"`
	CaptureReceiptSHA256  string `json:"capture_receipt_sha256"`
	ReplayReceiptSHA256   string `json:"replay_receipt_sha256"`
	ValidateExitCode      int    `json:"validate_exit_code"`
	ReplayExitCode        int    `json:"replay_exit_code"`
	CauseCode             string `json:"cause_code"`
	EventCount            int    `json:"event_count"`
	ReplayEventCount      int    `json:"replay_event_count"`
	CreatedAtUTC          string `json:"created_at_utc"`
}

var releaseStatusField = regexp.MustCompile(`("id"\s*:\s*"R0\.11\.9-0[23]"\s*,\s*"status"\s*:\s*")(open|planned)(")`)

func expectedReleaseSliceR02ToR03(raw []byte) []byte {
	matches := releaseStatusField.FindAllSubmatchIndex(raw, -1)
	if len(matches) != 2 {
		return nil
	}
	out := append([]byte(nil), raw...)
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		old, replacement := string(raw[m[4]:m[5]]), ""
		if bytes.Contains(raw[m[2]:m[3]], []byte("R0.11.9-02")) && old == "open" {
			replacement = "closed"
		}
		if bytes.Contains(raw[m[2]:m[3]], []byte("R0.11.9-03")) && old == "planned" {
			replacement = "open"
		}
		if replacement == "" {
			return nil
		}
		out = append(append(append([]byte(nil), out[:m[4]]...), replacement...), out[m[5]:]...)
	}
	return out
}

func transitionR02ToR03(productRoot string) error {
	root, err := filepath.Abs(productRoot)
	if err != nil {
		return err
	}
	lock, ok := acquireLock(lockName("release-slices", root))
	if !ok {
		return errors.New("another release-slices transition is in progress")
	}
	defer lock.release()
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !enabled || !sameLearningPath(owner.Selector.ProductRoot, root) {
		return errors.New("active self-learning owner must be this product")
	}
	planPath := filepath.Join(root, "plan", "release-slices.json")
	before, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	state := inspectReleaseSlices(root)
	if len(state.Problems) != 0 || !state.HasActiveStep || state.NextStep != nativeReleaseSliceR02 || state.ActiveStep.ID != nativeReleaseSliceR02 || state.ActiveVersion != "0.11.9" {
		return fmt.Errorf("release slice state is not the expected R02-before state: %+v", state.Problems)
	}
	if err := safeReleaseReceipt(root, "docs/receipts/RELEASE_0_11_9_02.json"); err != nil {
		return err
	}
	// Read the signed release envelope through the same bounded, OS-handle-
	// verified source reader as R03; ordinary os.Open may follow reparse links.
	raw, err := n093ReadRuntimeEvidence(root, "docs/receipts/RELEASE_0_11_9_02.json", 32*1024)
	if err != nil {
		return err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	var receipt releaseSliceCaptureReceipt
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&receipt); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err == nil {
		return errors.New("trailing capture receipt data")
	}
	if err := verifyR02ReleaseCaptureReceipt(owner, root, receipt); err != nil {
		return fmt.Errorf("R02 outer release envelope is not authenticated: %w", err)
	}
	if receipt.Schema != releaseSliceCaptureSchema || receipt.SliceID != nativeReleaseSliceR02 || receipt.CandidateVersion != version || receipt.PlanBeforeSHA256 != learnSHA(before) || receipt.RunID == "" || receipt.ValidateExitCode != 2 || receipt.ReplayExitCode != 0 || receipt.CauseCode != "validate-run-config-missing" || receipt.EventCount != 1 || receipt.ReplayEventCount != 1 || receipt.CaptureReceiptSHA256 == "" || receipt.ReplayReceiptSHA256 != receipt.CaptureReceiptSHA256 {
		return errors.New("capture receipt fields do not satisfy R02 transition contract")
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exeRaw, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if receipt.CandidateBinarySHA256 != learnSHA(exeRaw) {
		return errors.New("candidate executable SHA does not match receipt")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	captureRaw, err := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot, "cli-failures/"+receipt.RunID+".json", 32*1024)
	if err != nil {
		return err
	}
	if learnSHA(captureRaw) != receipt.CaptureReceiptSHA256 {
		return errors.New("CLI capture receipt SHA mismatch")
	}
	var cli ownCLIFailureReceipt
	if err := rejectDuplicateJSONKeys(captureRaw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(captureRaw))
	d.DisallowUnknownFields()
	if err := d.Decode(&cli); err != nil {
		return err
	}
	if cli.Schema != ownCLIFailureSchema || cli.RunID != receipt.RunID || cli.Action != "validate" || cli.ExitCode != 2 || cli.CauseCode != "validate-run-config-missing" || cli.Version != version || cli.Source != "air-worker-cli" || cli.Principal != "air-worker" || cli.Observed == "" || !hmac.Equal([]byte(cli.ReceiptMAC), []byte(ownCLIReceiptMAC(key, cli))) {
		return errors.New("CLI capture receipt is not correctly signed or bound")
	}
	// Both R02 and R03 must trust the same confined OS-handle journal reader:
	// unique module-owned source event, signed CLI invocation, session and
	// immutable HMAC receipt SHA. Never follow an external events.jsonl link.
	if _, err := r03OriginalEventID(owner, cli, receipt.CaptureReceiptSHA256); err != nil {
		return fmt.Errorf("R02: original module-owned completion is not uniquely authenticated: %w", err)
	}
	after := expectedReleaseSliceR02ToR03(before)
	if after == nil {
		return errors.New("release plan status layout is not the canonical R02 transition")
	}
	// Recheck exact bytes immediately before atomic publication while holding the product-wide lock.
	current, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, before) {
		return errors.New("release plan changed concurrently")
	}
	if err := writeFileAtomicDurable(planPath, after); err != nil {
		return err
	}
	readback, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(readback, after) {
		return errors.New("release plan transition readback mismatch")
	}
	state = inspectReleaseSlices(root)
	if len(state.Problems) != 0 || state.NextStep != "R0.11.9-03" || !state.HasActiveStep || state.ActiveStep.ID != "R0.11.9-03" || state.ActiveStep.Status != "open" {
		return fmt.Errorf("release plan transition verification failed: %+v", state.Problems)
	}
	return nil
}
