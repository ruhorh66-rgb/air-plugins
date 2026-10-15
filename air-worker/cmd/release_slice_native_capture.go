package main

import (
	"bytes"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const releaseSliceReviewDeferEnv = "AIR_WORKER_R02_DEFER_REVIEW"

func shouldDeferOwnCLIReviewForR02(argv []string, rc int) bool {
	if os.Getenv(releaseSliceReviewDeferEnv) != "1" || rc != 2 || len(argv) != 4 ||
		argv[0] != "validate" || argv[1] != "-product" || argv[3] != "-json" {
		return false
	}
	return ownCLITrustedFailureCause(argv, rc) == "validate-run-config-missing" && releaseCaptureTargetUnderScratch(argv[2])
}

func releaseCaptureScratchRoot() (string, error) {
	raw := strings.TrimSpace(os.Getenv("TMPDIR"))
	if raw == "" {
		return "", errors.New("TMPDIR is required for the disposable R02 capture root")
	}
	root, err := filepath.Abs(raw)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("R02 scratch root is not an ordinary directory")
	}
	return filepath.Clean(root), nil
}

func releaseCaptureTargetUnderScratch(target string) bool {
	scratch, err := releaseCaptureScratchRoot()
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(scratch, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return false
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	if _, err := os.Lstat(filepath.Join(abs, "run-config.json")); !errors.Is(err, os.ErrNotExist) {
		return false
	}
	return true
}

func withReleaseReviewDeferred(env []string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		key, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(key, releaseSliceReviewDeferEnv) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, releaseSliceReviewDeferEnv+"=1")
}

func ownCLIReceiptJSON(raw []byte) (ownCLIFailureReceipt, error) {
	var rec ownCLIFailureReceipt
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&rec); err != nil {
		return rec, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return rec, errors.New("CLI receipt contains more than one JSON value")
		}
		return rec, err
	}
	return rec, nil
}

func verifyR02CLIReceipt(owner selfLearningOwner, runID string, raw []byte) (ownCLIFailureReceipt, error) {
	rec, err := ownCLIReceiptJSON(raw)
	if err != nil {
		return rec, err
	}
	if rec.Schema != ownCLIFailureSchema || rec.RunID != runID || rec.Action != "validate" ||
		rec.ExitCode != 2 || rec.CauseCode != "validate-run-config-missing" ||
		rec.Source != "air-worker-cli" || rec.Principal != "air-worker" || rec.Version != version ||
		rec.ProcessID <= 0 || rec.Observed != ownCLIFailureObservation("validate", 2, "validate-run-config-missing") {
		return rec, errors.New("R02 CLI receipt does not match the required validate preflight evidence")
	}
	if len(rec.InvocationMAC) != 64 || len(rec.ReceiptMAC) != 64 {
		return rec, errors.New("R02 CLI receipt MAC has invalid length")
	}
	if _, err := hex.DecodeString(rec.InvocationMAC); err != nil {
		return rec, errors.New("R02 CLI invocation MAC is malformed")
	}
	if _, err := hex.DecodeString(rec.ReceiptMAC); err != nil {
		return rec, errors.New("R02 CLI receipt MAC is malformed")
	}
	if _, err := time.Parse(time.RFC3339Nano, rec.CapturedAt); err != nil {
		return rec, errors.New("R02 CLI receipt timestamp is malformed")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return rec, err
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	if !hmac.Equal([]byte(rec.ReceiptMAC), []byte(ownCLIReceiptMAC(key, rec))) {
		return rec, errors.New("R02 CLI receipt HMAC verification failed")
	}
	return rec, nil
}

func verifyR02CLIReceiptForTarget(owner selfLearningOwner, runID string, raw []byte, targetRoot string) (ownCLIFailureReceipt, error) {
	rec, err := verifyR02CLIReceipt(owner, runID, raw)
	if err != nil {
		return rec, err
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return rec, err
	}
	want := ownCLIInvocationMAC(key, []string{"validate", "-product", targetRoot, "-json"})
	for i := range key {
		key[i] = 0
	}
	if !hmac.Equal([]byte(rec.InvocationMAC), []byte(want)) {
		return rec, errors.New("R02 CLI invocation does not authenticate the exact disposable product root")
	}
	return rec, nil
}

func countR02CLIEvents(runtimeRoot, runID string) (int, error) {
	count := 0
	err := scanLearnJSONL(filepath.Join(runtimeRoot, "events.jsonl"), func(raw []byte) error {
		var event map[string]any
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		if event["schema"] == "air.learning.event/v1" && event["run_id"] == runID &&
			event["kind"] == "run_completed" && event["source"] == "air-worker-cli" &&
			event["class"] == "air-worker-cli-preflight" && event["principal"] == "air-worker" {
			count++
		}
		return nil
	})
	return count, err
}

func snapshotR02CLIReceipts(runtimeRoot string) (map[string]string, error) {
	out := map[string]string{}
	dir := filepath.Join(runtimeRoot, "cli-failures")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, err := os.Lstat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("non-regular CLI receipt entry blocks R02: %q", entry.Name())
		}
		raw, err := readLearningBounded(path, 32*1024)
		if err != nil {
			return nil, err
		}
		out[entry.Name()] = learnSHA(raw)
	}
	return out, nil
}

func newR02CLIReceipt(before, after map[string]string) (string, error) {
	name := ""
	for oldName, oldSHA := range before {
		if newSHA, ok := after[oldName]; !ok || newSHA != oldSHA {
			return "", fmt.Errorf("pre-existing CLI receipt changed during R02 capture: %q", oldName)
		}
	}
	for newName := range after {
		if _, existed := before[newName]; existed {
			continue
		}
		if name != "" {
			return "", errors.New("more than one CLI receipt appeared during R02 capture")
		}
		name = newName
	}
	if name == "" || len(after) != len(before)+1 {
		return "", errors.New("R02 capture did not create exactly one new CLI receipt")
	}
	return strings.TrimSuffix(name, filepath.Ext(name)), nil
}

func releaseReceiptPath(productRoot string) string {
	return filepath.Join(productRoot, "docs", "receipts", "RELEASE_0_11_9_02.json")
}

func writeReleaseReceiptNoReplace(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".release-r02-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// Hard-link creation is atomic and fails rather than replacing a prior user receipt.
	if err := os.Link(tmpName, path); err != nil {
		return err
	}
	got, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(got, raw) {
		return errors.New("R02 release receipt readback mismatch")
	}
	return nil
}

type r02ReplayResponse struct {
	Schema        string `json:"schema"`
	RunID         string `json:"run_id"`
	Replayed      bool   `json:"replayed"`
	EventCount    int    `json:"event_count"`
	ReceiptSHA256 string `json:"receipt_sha256"`
}

type r02PendingAttempt struct {
	Schema                string            `json:"schema"`
	SliceID               string            `json:"slice_id"`
	CandidateVersion      string            `json:"candidate_version"`
	CandidateBinarySHA256 string            `json:"candidate_binary_sha256"`
	PlanBeforeSHA256      string            `json:"plan_before_sha256"`
	TargetRoot            string            `json:"target_root"`
	StartedAtUTC          string            `json:"started_at_utc"`
	BaselineReceipts      map[string]string `json:"baseline_receipts"`
}

func r02PendingPath(runtimeRoot string) string {
	return filepath.Join(runtimeRoot, "release-slice-r02-pending.json")
}

func readR02Pending(path string) (r02PendingAttempt, bool, error) {
	var attempt r02PendingAttempt
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return attempt, false, nil
	}
	if err != nil {
		return attempt, true, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return attempt, true, errors.New("R02 pending marker is not an ordinary file")
	}
	raw, err := readLearningBounded(path, 256*1024)
	if err != nil {
		return attempt, true, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&attempt); err != nil {
		return attempt, true, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return attempt, true, errors.New("R02 pending marker has trailing JSON")
		}
		return attempt, true, err
	}
	if attempt.Schema != "air-worker.release-slice.r02-pending/v1" || attempt.SliceID != nativeReleaseSliceR02 ||
		attempt.CandidateVersion != version || len(attempt.CandidateBinarySHA256) != 64 || len(attempt.PlanBeforeSHA256) != 64 ||
		len(attempt.BaselineReceipts) > 4096 || !releaseCaptureTargetUnderScratch(attempt.TargetRoot) {
		return attempt, true, errors.New("R02 pending marker identity or target is invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, attempt.StartedAtUTC); err != nil {
		return attempt, true, errors.New("R02 pending marker timestamp is invalid")
	}
	return attempt, true, nil
}

func loadOrCreateR02Pending(owner selfLearningOwner, planSHA, binarySHA string, scratch string) (r02PendingAttempt, bool, error) {
	path := r02PendingPath(owner.Settings.RuntimeRoot)
	attempt, found, err := readR02Pending(path)
	if err != nil {
		return attempt, found, err
	}
	if found {
		if attempt.PlanBeforeSHA256 != planSHA || attempt.CandidateBinarySHA256 != binarySHA {
			return attempt, true, errors.New("R02 pending attempt belongs to a different plan or binary; refusing replay")
		}
		return attempt, true, nil
	}
	baseline, err := snapshotR02CLIReceipts(owner.Settings.RuntimeRoot)
	if err != nil {
		return attempt, false, err
	}
	target, err := os.MkdirTemp(scratch, "air-worker-r02-capture-")
	if err != nil {
		return attempt, false, err
	}
	attempt = r02PendingAttempt{
		Schema: "air-worker.release-slice.r02-pending/v1", SliceID: nativeReleaseSliceR02,
		CandidateVersion: version, CandidateBinarySHA256: binarySHA, PlanBeforeSHA256: planSHA,
		TargetRoot: target, StartedAtUTC: time.Now().UTC().Format(time.RFC3339Nano), BaselineReceipts: baseline,
	}
	raw, err := json.MarshalIndent(attempt, "", "  ")
	if err == nil {
		err = writeReleaseReceiptNoReplace(path, append(raw, '\n'))
	}
	if err != nil {
		_ = os.RemoveAll(target)
		return r02PendingAttempt{}, false, err
	}
	return attempt, false, nil
}

func r02PendingNewReceipt(owner selfLearningOwner, attempt r02PendingAttempt) (string, []byte, bool, error) {
	after, err := snapshotR02CLIReceipts(owner.Settings.RuntimeRoot)
	if err != nil {
		return "", nil, false, err
	}
	name := ""
	for oldName, oldSHA := range attempt.BaselineReceipts {
		if newSHA, ok := after[oldName]; !ok || newSHA != oldSHA {
			return "", nil, false, fmt.Errorf("baseline CLI receipt changed during pending R02: %q", oldName)
		}
	}
	for newName := range after {
		if _, existed := attempt.BaselineReceipts[newName]; existed {
			continue
		}
		if name != "" {
			return "", nil, false, errors.New("multiple new CLI receipts appeared during pending R02")
		}
		name = newName
	}
	if name == "" {
		if len(after) != len(attempt.BaselineReceipts) {
			return "", nil, false, errors.New("CLI receipt inventory changed without a new R02 receipt")
		}
		return "", nil, false, nil
	}
	if len(after) != len(attempt.BaselineReceipts)+1 || !strings.HasSuffix(strings.ToLower(name), ".json") {
		return "", nil, false, errors.New("unexpected CLI receipt inventory change during R02")
	}
	runID := strings.TrimSuffix(name, filepath.Ext(name))
	path := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", name)
	raw, err := readLearningBounded(path, 32*1024)
	if err != nil {
		return "", nil, false, err
	}
	rec, err := verifyR02CLIReceipt(owner, runID, raw)
	if err != nil {
		return "", nil, false, err
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return "", nil, false, err
	}
	wantInvocation := ownCLIInvocationMAC(key, []string{"validate", "-product", attempt.TargetRoot, "-json"})
	for i := range key {
		key[i] = 0
	}
	if !hmac.Equal([]byte(rec.InvocationMAC), []byte(wantInvocation)) {
		return "", nil, false, errors.New("new CLI receipt does not authenticate the disposable R02 validate target")
	}
	if count, err := countR02CLIEvents(owner.Settings.RuntimeRoot, runID); err != nil || count != 1 {
		return "", nil, false, errors.New("R02 capture event is absent or duplicated")
	}
	return runID, raw, true, nil
}

func ensureR02Target(attempt r02PendingAttempt, recreate bool) error {
	if releaseCaptureTargetUnderScratch(attempt.TargetRoot) {
		return nil
	}
	if !recreate {
		return errors.New("pending R02 target is missing or unsafe")
	}
	if err := os.Mkdir(attempt.TargetRoot, 0o700); err != nil {
		return err
	}
	if !releaseCaptureTargetUnderScratch(attempt.TargetRoot) {
		return errors.New("recreated R02 target is unsafe")
	}
	return nil
}

func r02TargetEmpty(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func cleanupR02Pending(owner selfLearningOwner, attempt r02PendingAttempt) error {
	if err := os.RemoveAll(attempt.TargetRoot); err != nil {
		return err
	}
	err := os.Remove(r02PendingPath(owner.Settings.RuntimeRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func cleanupR02PendingBestEffort(owner selfLearningOwner) {
	attempt, found, err := readR02Pending(r02PendingPath(owner.Settings.RuntimeRoot))
	if err == nil && found {
		_ = cleanupR02Pending(owner, attempt)
	}
}

func runNativeReleaseSliceR02(productRoot string, state releaseSlicesSummary) int {
	root, err := filepath.Abs(productRoot)
	if err != nil {
		line("ОТКАЗ: product root cannot be normalized")
		return 2
	}
	if err := releaseSliceLoopSupported(state, version); err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	runLock, free := acquireLock(lockName("release-slice-run", root))
	if !free {
		line("ОТКАЗ: another release-slice operation is active for this product")
		return 2
	}
	defer runLock.release()
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(owner.Selector.ProductRoot, root) {
		line("ОТКАЗ: active AirWorker self-learning owner does not match the release product")
		return 2
	}
	planPath := filepath.Join(root, "plan", "release-slices.json")
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		line("ОТКАЗ: release-slices.json cannot be read")
		return 2
	}
	current := inspectReleaseSlices(root)
	if err := releaseSliceLoopSupported(current, version); err != nil {
		line("ОТКАЗ: release-slice plan changed or is invalid")
		return 2
	}
	receiptName := "docs/receipts/RELEASE_0_11_9_02.json"
	if err := safeReleaseReceipt(root, receiptName); err != nil {
		line("ОТКАЗ: unsafe R02 receipt path")
		return 2
	}
	releasePath := releaseReceiptPath(root)
	if info, err := os.Lstat(releasePath); err == nil {
		if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			line("ОТКАЗ: existing R02 receipt is not an ordinary file")
			return 2
		}
		if err := transitionR02ToR03(root); err != nil {
			line("ОТКАЗ: existing R02 evidence failed transition verification: " + err.Error())
			return 2
		}
		cleanupR02PendingBestEffort(owner)
		return releaseSliceTransitionReadback(root)
	} else if !errors.Is(err, os.ErrNotExist) {
		line("ОТКАЗ: existing R02 receipt cannot be inspected")
		return 2
	}
	scratch, err := releaseCaptureScratchRoot()
	if err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	exePath, err := os.Executable()
	if err != nil {
		line("ОТКАЗ: candidate executable path unavailable")
		return 2
	}
	exeRaw, err := os.ReadFile(exePath)
	if err != nil {
		line("ОТКАЗ: candidate executable cannot be hashed")
		return 2
	}
	binarySHA := learnSHA(exeRaw)
	pending, _, err := loadOrCreateR02Pending(owner, learnSHA(planRaw), binarySHA, scratch)
	if err != nil {
		line("ОТКАЗ: R02 pending attempt is missing, invalid, or bound to another candidate")
		return 2
	}
	targetRoot := pending.TargetRoot
	runID, captureRaw, captured, err := r02PendingNewReceipt(owner, pending)
	if err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	if !captured {
		if err := ensureR02Target(pending, true); err != nil {
			line("ОТКАЗ: pending disposable R02 target is missing or unsafe")
			return 2
		}
		empty, err := r02TargetEmpty(targetRoot)
		if err != nil || !empty {
			line("ОТКАЗ: pending target contains unverified data; refusing to repeat validate")
			return 2
		}
		validateArgs := []string{"validate", "-product", targetRoot, "-json"}
		cmd := exec.Command(exePath, validateArgs...)
		cmd.Env = withReleaseReviewDeferred(os.Environ())
		cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
		runErr := cmd.Run()
		validateExit := 0
		if runErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(runErr, &exitErr) {
				line("ОТКАЗ: native validate child process could not be started; pending attempt retained")
				return 2
			}
			validateExit = exitErr.ExitCode()
		}
		if validateExit != 2 {
			line("ОТКАЗ: disposable validate did not return the expected native preflight exit; pending attempt retained")
			return 2
		}
		runID, captureRaw, captured, err = r02PendingNewReceipt(owner, pending)
		if err != nil || !captured {
			line("ОТКАЗ: signed native CLI receipt was not durably captured; pending attempt retained")
			return 2
		}
	} else if err := ensureR02Target(pending, false); err != nil {
		line("ОТКАЗ: capture exists but disposable target is unavailable for exact replay")
		return 2
	}
	capturePath := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", runID+".json")
	if _, err := verifyR02CLIReceiptForTarget(owner, runID, captureRaw, targetRoot); err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	captureSHA := learnSHA(captureRaw)
	if count, err := countR02CLIEvents(owner.Settings.RuntimeRoot, runID); err != nil || count != 1 {
		line("ОТКАЗ: native capture event is missing or duplicated")
		return 2
	}
	replayCmd := exec.Command(exePath, "internal-self-cli-replay", "-run-id", runID, "-product", targetRoot)
	var replayOut, replayErr bytes.Buffer
	replayCmd.Stdout, replayCmd.Stderr = &replayOut, &replayErr
	if err := replayCmd.Run(); err != nil {
		line("ОТКАЗ: new-process replay of signed CLI receipt failed")
		return 2
	}
	var replay r02ReplayResponse
	dec := json.NewDecoder(bytes.NewReader(replayOut.Bytes()))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&replay); err != nil || replay.Schema != "air-worker.r02-replay/v1" ||
		replay.RunID != runID || !replay.Replayed || replay.EventCount != 1 || replay.ReceiptSHA256 != captureSHA {
		line("ОТКАЗ: new-process replay readback did not match the original capture")
		return 2
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		line("ОТКАЗ: replay process returned extra output")
		return 2
	}
	replayedRaw, err := readLearningBounded(capturePath, 32*1024)
	if err != nil || learnSHA(replayedRaw) != captureSHA {
		line("ОТКАЗ: replay modified the immutable signed CLI receipt")
		return 2
	}
	if count, err := countR02CLIEvents(owner.Settings.RuntimeRoot, runID); err != nil || count != 1 {
		line("ОТКАЗ: replay created a duplicate native event")
		return 2
	}
	evidence := releaseSliceCaptureReceipt{
		Schema: "air-worker.release-slice.capture/v1", SliceID: nativeReleaseSliceR02,
		CandidateVersion: version, CandidateBinarySHA256: binarySHA, PlanBeforeSHA256: learnSHA(planRaw),
		RunID: runID, CaptureReceiptSHA256: captureSHA, ReplayReceiptSHA256: learnSHA(replayedRaw),
		ValidateExitCode: 2, ReplayExitCode: 0, CauseCode: "validate-run-config-missing",
		EventCount: 1, ReplayEventCount: 1, CreatedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	releaseRaw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		line("ОТКАЗ: cannot encode R02 release receipt")
		return 2
	}
	if err := writeReleaseReceiptNoReplace(releasePath, append(releaseRaw, '\n')); err != nil {
		line("ОТКАЗ: cannot create immutable R02 release receipt")
		return 2
	}
	if err := cleanupR02Pending(owner, pending); err != nil {
		line("ПРЕДУПРЕЖДЕНИЕ: disposable R02 scratch cleanup was incomplete")
	}
	if err := transitionR02ToR03(root); err != nil {
		line("ОТКАЗ: receipt-bound R02 transition failed: " + err.Error())
		return 2
	}
	return releaseSliceTransitionReadback(root)
}

func releaseSliceTransitionReadback(root string) int {
	state := inspectReleaseSlices(root)
	if len(state.Problems) != 0 || state.NextStep != "R0.11.9-03" || !state.HasActiveStep || state.ActiveStep.Status != "open" {
		line("ОТКАЗ: native R02 transition readback did not open R0.11.9-03")
		return 2
	}
	line("R0.11.9-02 закрыт; R0.11.9-03 открыт после проверки signed capture и replay.")
	return 0
}

func cmdInternalSelfCLIReplay(argv []string) int {
	fs := flag.NewFlagSet("internal-self-cli-replay", flag.ContinueOnError)
	runID := fs.String("run-id", "", "immutable native capture run id")
	product := fs.String("product", "", "disposable product root")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || strings.TrimSpace(*runID) == "" || strings.TrimSpace(*product) == "" {
		return 2
	}
	if !releaseCaptureTargetUnderScratch(*product) {
		return 2
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		return 2
	}
	path := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", *runID+".json")
	before, err := readLearningBounded(path, 32*1024)
	if err != nil {
		return 2
	}
	if _, err := verifyR02CLIReceiptForTarget(owner, *runID, before, *product); err != nil {
		return 2
	}
	if count, err := countR02CLIEvents(owner.Settings.RuntimeRoot, *runID); err != nil || count != 1 {
		return 2
	}
	args := []string{"validate", "-product", *product, "-json"}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, *runID, nil); err != nil {
		return 2
	}
	after, err := readLearningBounded(path, 32*1024)
	if err != nil || !bytes.Equal(before, after) {
		return 2
	}
	count, err := countR02CLIEvents(owner.Settings.RuntimeRoot, *runID)
	if err != nil || count != 1 {
		return 2
	}
	out := r02ReplayResponse{Schema: "air-worker.r02-replay/v1", RunID: *runID, Replayed: true, EventCount: count, ReceiptSHA256: learnSHA(after)}
	encoded, err := json.Marshal(out)
	if err != nil {
		return 2
	}
	fmt.Println(string(encoded))
	return 0
}
