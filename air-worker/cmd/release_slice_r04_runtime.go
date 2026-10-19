package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func releaseR04ReadReceipt(root string) (releaseR04Receipt, error) {
	var out releaseR04Receipt
	if err := safeReleaseReceipt(root, releaseR04ReceiptRef); err != nil {
		return out, err
	}
	raw, err := n093ReadRuntimeEvidence(root, releaseR04ReceiptRef, 32*1024)
	if err != nil {
		return out, err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return out, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil {
		return out, err
	}
	var tail any
	if err := dec.Decode(&tail); !errors.Is(err, io.EOF) {
		return out, errors.New("R04: trailing or invalid release receipt")
	}
	if out.Schema != releaseR04ReceiptSchema || out.SliceID != nativeReleaseSliceR04 {
		return out, errors.New("R04: invalid release receipt schema")
	}
	if _, err := time.Parse(time.RFC3339Nano, out.CreatedAtUTC); err != nil {
		return out, err
	}
	return out, nil
}
func releaseR04PersistReceipt(root string, proof releaseR04Receipt) error {
	if err := safeReleaseReceipt(root, releaseR04ReceiptRef); err != nil {
		return err
	}
	target := filepath.Join(root, filepath.FromSlash(releaseR04ReceiptRef))
	if _, err := os.Lstat(target); err == nil {
		return errors.New("R04: immutable native result receipt already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	if err := writeReleaseReceiptNoReplace(target, append(raw, '\n')); err != nil {
		return err
	}
	back, err := n093ReadRuntimeEvidence(root, releaseR04ReceiptRef, 32*1024)
	if err != nil || !bytes.Equal(back, append(raw, '\n')) {
		return errors.New("R04: immutable release receipt readback mismatch")
	}
	return nil
}

// Recreate ONLY the exact same disposable scratch target used in R02. Its
// random pathname is inside the signed OUTER R02 HMAC envelope, and the CLI
// invocation HMAC must independently match the original signed native call.
func releaseR04ScratchTarget(owner selfLearningOwner, src releaseR03Source) (string, error) {
	target := src.R02.TargetRoot
	if target == "" || !filepath.IsAbs(target) || filepath.Clean(target) != target ||
		!strings.HasPrefix(filepath.Base(target), "air-worker-r02-capture-") {
		return "", errors.New("R04: R02 has no signed recoverable disposable invocation")
	}
	scratch, err := releaseCaptureScratchRoot()
	if err != nil {
		return "", err
	}
	if !sameLearningPath(filepath.Dir(target), scratch) {
		return "", errors.New("R04: signed R02 scratch target is outside current isolated scratch root")
	}
	signed, err := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot,
		"cli-failures/"+src.Prior.RunID+".json", 32*1024)
	if err != nil {
		return "", err
	}
	if _, err := verifyR02CLIReceiptForTarget(owner, src.Prior.RunID, signed, target); err != nil {
		return "", fmt.Errorf("R04: signed scratch invocation does not match prior run: %w", err)
	}
	root, err := releaseR04OpenPinnedScratch(scratch)
	if err != nil {
		return "", err
	}
	defer root.Close()
	child := filepath.Base(target)
	if err := root.Mkdir(child, 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return "", fmt.Errorf("R04: confined scratch creation failed: %w", err)
	}
	info, err := root.Lstat(child)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("R04: confined target is not an ordinary directory")
	}
	if _, err := root.Readlink(child); err == nil {
		return "", errors.New("R04: confined scratch child is a reparse link")
	}
	dir, err := root.Open(child)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	actual, err := openedFileFinalPath(dir)
	if err != nil || !sameLearningPath(actual, target) {
		return "", errors.New("R04: opened target handle escaped signed scratch directory")
	}
	entries, err := dir.ReadDir(1)
	if len(entries) != 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return "", errors.New("R04: scratch target is nonempty or unreadable")
	}
	if !releaseCaptureTargetUnderScratch(target) {
		return "", errors.New("R04: scratch path/configuration identity changed after creation")
	}
	return target, nil
}

func releaseR04CallNativeUse(root string, owner selfLearningOwner, src releaseR03Source) error {
	target, err := releaseR04ScratchTarget(owner, src)
	if err != nil {
		return err
	}
	// Native CLI child receives the original signed textual path. Keep
	// non-deletable handles on the target and every replaceable ancestor
	// through the entire subprocess; a junction swap after scratch validation
	// must not redirect the child's real filesystem operation.
	releasePhysical, err := releaseR04HoldSignedScratchUntilChild(target)
	if err != nil {
		return err
	}
	defer releasePhysical()
	// The first os.Root validation closes its handle before the non-delete
	// holds are acquired. Never run the child based only on that earlier
	// check: re-open and verify the SAME empty physical target while every
	// replaceable directory remains pinned until the child has exited.
	if err := releaseR04RecheckHeldScratchTarget(target); err != nil {
		return fmt.Errorf("R04: signed scratch changed between check and child: %w", err)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// A failure on this ONE child must never spawn a second Codex reviewer.
	// Native N093 itself is the only permitted module-owned use recorder.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, exe, "validate", "-product", target, "-json")
	c.Env = withReleaseReviewDeferred(os.Environ())
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	err = c.Run()
	if ctx.Err() != nil {
		return fmt.Errorf("R04: native validate use attempt timed out: %w", ctx.Err())
	}
	if err == nil {
		return errors.New("R04: unexpected full validation instead of guarded missing-config use")
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 {
		return fmt.Errorf("R04: native learned action ended without bounded deferral: %v", err)
	}
	if !bytes.Contains(stdout.Bytes(), []byte("deferred_missing_configuration")) ||
		!bytes.Contains(stdout.Bytes(), []byte("native_validation_executed")) {
		return errors.New("R04: native validator did not report the expected deferred action")
	}
	return nil
}

func runNativeReleaseSliceR04(productRoot string, state releaseSlicesSummary) int {
	root, err := filepath.Abs(productRoot)
	if err != nil {
		return 2
	}
	if err := releaseSliceLoopSupported(state, version); err != nil {
		line("ОТКАЗ R04: " + err.Error())
		return 2
	}
	lock, ok := acquireLock(lockName("release-slice-run", root))
	if !ok {
		line("ОТКАЗ R04: another release stage owns this product")
		return 2
	}
	defer lock.release()
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(owner.Selector.ProductRoot, root) {
		line("ОТКАЗ R04: source learning owner is not active release product")
		return 2
	}
	plan, err := os.ReadFile(filepath.Join(root, "plan", "release-slices.json"))
	if err != nil {
		line("ОТКАЗ R04: release plan unavailable")
		return 2
	}
	p, err := releaseR04Predecessor(root, owner, inspectReleaseSlices(root), plan)
	if err != nil {
		line("ОТКАЗ R04: " + err.Error())
		return 2
	}
	receiptPath := filepath.Join(root, filepath.FromSlash(releaseR04ReceiptRef))
	if _, err := os.Lstat(receiptPath); err == nil {
		if err := transitionR04ToR05(root); err != nil {
			line("ОТКАЗ R04: stale previous effect: " + err.Error())
			return 2
		}
		return releaseR04TransitionReadback(root)
	} else if !errors.Is(err, os.ErrNotExist) {
		return 2
	}
	// If an HMAC-confirmed final result exists, a crash may be recovered
	// without a duplicate native operation or reviewer.
	ruleID := p.Review.Target + "@" + p.Review.TargetSHA256
	identity := learnSHA([]byte(p.Source.Prior.RunID + "\x00" + p.Source.SourceEventID + "\x00" + ruleID))
	usedID := "N118-USED-" + identity[:24]
	_, resultErr := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot,
		"native-preflight-results/"+usedID+".json", 32*1024)
	if errors.Is(resultErr, os.ErrNotExist) {
		// A prior pending intent means an interrupted operation. Never
		// blindly generate another event or repeat the failed source.
		_, intentErr := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot,
			"native-preflight-uses/"+usedID+".json", 32*1024)
		if intentErr == nil || !errors.Is(intentErr, os.ErrNotExist) {
			line("ОТКАЗ R04: prior pending use requires signed recovery")
			return 2
		}
		// One immutable HMAC authorization is minted only after R04 OPEN.
		// Its timestamp and exact plan SHA must precede N093's signed intent.
		// A previous authorized attempt without a final result is not retried.
		if _, err := releaseR04ReadAuthorization(root, owner, p, plan); err == nil ||
			!errors.Is(err, os.ErrNotExist) {
			line("ОТКАЗ R04: earlier authorization exists or is invalid; no blind retry")
			return 2
		}
		if _, err := releaseR04ScratchTarget(owner, p.Source); err != nil {
			line("ОТКАЗ R04: " + err.Error())
			return 2
		}
		if err := releaseR04SealAuthorization(root, owner, p, plan); err != nil {
			line("ОТКАЗ R04: cannot sign current release authorization: " + err.Error())
			return 2
		}
		if err := releaseR04CallNativeUse(root, owner, p.Source); err != nil {
			line("ОТКАЗ R04: " + err.Error())
			return 2
		}
	} else if resultErr != nil {
		line("ОТКАЗ R04: prior result is unreadable or linked")
		return 2
	}
	proof, err := releaseR04ObservedUse(root, owner, p, plan)
	if err != nil {
		line("ОТКАЗ R04: no authenticated later effect: " + err.Error())
		return 2
	}
	if err := releaseR04PersistReceipt(root, proof); err != nil {
		line("ОТКАЗ R04: " + err.Error())
		return 2
	}
	if err := transitionR04ToR05(root); err != nil {
		line("ОТКАЗ R04: signed use exists; transition needs recovery: " + err.Error())
		return 2
	}
	return releaseR04TransitionReadback(root)
}

func transitionR04ToR05(root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	lock, ok := acquireLock(lockName("release-slices", root))
	if !ok {
		return errors.New("R04: another release plan writer owns this product")
	}
	defer lock.release()
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !on || !sameLearningPath(owner.Selector.ProductRoot, root) {
		return errors.New("R04: release learning owner identity changed")
	}
	path := filepath.Join(root, "plan", "release-slices.json")
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	state := inspectReleaseSlices(root)
	if _, err := releaseR04VerifyReceipt(root, owner, state, before); err != nil {
		return err
	}
	after := expectedReleaseSliceR04ToR05(before)
	if after == nil {
		return errors.New("R04: plan has no unambiguous measured-use step")
	}
	recheck, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, recheck) {
		return errors.New("R04: concurrent release plan change")
	}
	if err := writeFileAtomicDurable(path, after); err != nil {
		return err
	}
	confirmed, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, confirmed) {
		return errors.New("R04: release plan CAS readback failed")
	}
	st := inspectReleaseSlices(root)
	if len(st.Problems) != 0 || st.NextStep != "R0.11.9-05" ||
		!st.HasActiveStep || st.ActiveStep.ID != "R0.11.9-05" || st.ActiveStep.Status != "open" {
		return errors.New("R04: exactly R05 was not promoted")
	}
	return nil
}

func releaseR04TransitionReadback(root string) int {
	s := inspectReleaseSlices(root)
	if len(s.Problems) != 0 || !s.HasActiveStep ||
		s.NextStep != "R0.11.9-05" || s.ActiveStep.ID != "R0.11.9-05" || s.ActiveStep.Status != "open" {
		line("ОТКАЗ R04: signed use was not preserved in the R05 plan")
		return 2
	}
	line("R04 CLOSED, R05 OPEN: signed actual learned procedure use and bounded effect confirmed. Full validation did not execute.")
	return 0
}
