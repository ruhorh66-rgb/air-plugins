package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func releaseSliceLoopFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, srcRel := range []string{"../run-config.json", "../plan/release-slices.json"} {
		src, err := os.ReadFile(srcRel)
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, filepath.FromSlash(srcRel[3:]))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, src, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("| 93 | stale legacy row | script | old route |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func airWorkerRootWithoutReleasePlanFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	cfg, err := os.ReadFile("../run-config.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("| 93 | stale legacy row | script | old route |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	moduleDir := filepath.Join(root, "cmd")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module airos/air-worker\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCmdLoopRoutesReleaseSliceBeforeLegacyNumericPlan(t *testing.T) {
	root := releaseSliceLoopFixture(t)
	before, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	code := cmdLoopWithReleaseSliceRunner([]string{"-product", root}, func(gotRoot string, _ runConfig, state releaseSlicesSummary) int {
		calls++
		if gotRoot != root || state.NextStep != "R0.11.9-02" || !state.HasActiveStep || state.ActiveStep.Kind != "source" {
			t.Fatalf("release runner received wrong active target: root=%q state=%+v", gotRoot, state)
		}
		return 17
	})
	if code != 17 || calls != 1 {
		t.Fatalf("release runner dispatch code=%d calls=%d; want 17 and exactly one call", code, calls)
	}
	after, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("legacy numeric PLAN was changed: err=%v before=%q after=%q", err, before, after)
	}
}

func TestCmdLoopFailsClosedForUnsupportedReleaseSlice(t *testing.T) {
	root := releaseSliceLoopFixture(t)
	path := filepath.Join(root, "plan", "release-slices.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	releases := doc["releases"].([]any)
	steps := releases[0].(map[string]any)["steps"].([]any)
	steps[1].(map[string]any)["status"] = "closed"
	steps[2].(map[string]any)["status"] = "open"
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	code := cmdLoopWithReleaseSliceRunner([]string{"-product", root}, func(string, runConfig, releaseSlicesSummary) int {
		calls++
		return 0
	})
	if code == 0 || calls != 0 {
		t.Fatalf("unsupported slice was not blocked before legacy fallback: code=%d calls=%d", code, calls)
	}
}

func TestCmdLoopFailsClosedWhenAirWorkerReleasePlanIsMissing(t *testing.T) {
	root := airWorkerRootWithoutReleasePlanFixture(t)
	calls := 0
	code := cmdLoopWithReleaseSliceRunner([]string{"-product", root}, func(string, runConfig, releaseSlicesSummary) int {
		calls++
		return 0
	})
	if code == 0 || calls != 0 {
		t.Fatalf("missing AirWorker release plan fell back to numeric PLAN: code=%d calls=%d", code, calls)
	}
}

func TestAdapterStatusFailsClosedWhenAirWorkerReleasePlanIsMissing(t *testing.T) {
	root := airWorkerRootWithoutReleasePlanFixture(t)
	status := buildAdapterStatus(root, filepath.Join(root, "run-config.json"), "", "")
	if status.StopReason != "release_slices_missing" || status.CurrentStep == "93" {
		t.Fatalf("missing release plan fell back to legacy adapter status: %+v", status)
	}
}

func releaseSliceTransitionFixture(t *testing.T) (string, []byte, string) {
	t.Helper()
	product, _, _ := selfLearningFixture(t)
	planRaw, err := os.ReadFile("../plan/release-slices.json")
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(product, "plan", "release-slices.json")
	if err := os.MkdirAll(filepath.Dir(planPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, planRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "PLAN.md"), []byte("| 93 | stale legacy row | script | old route |\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if state := inspectReleaseSlices(product); len(state.Problems) != 0 || state.NextStep != nativeReleaseSliceR02 {
		t.Fatalf("invalid transition fixture: %+v", state)
	}
	return product, planRaw, planPath
}

func writeReleaseSliceCaptureFixture(t *testing.T, product string, planRaw []byte, runID string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "disposable-product-root")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(target, "run-config.json")); !os.IsNotExist(err) {
		t.Fatalf("disposable target unexpectedly has run-config.json: %v", err)
	}
	argv := []string{"validate", "-product", target, "-json"}
	if err := autoCaptureOwnCLIExitWithRunID(argv, 2, runID, nil); err != nil {
		t.Fatal(err)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(owner.Selector.ProductRoot, product) {
		t.Fatalf("active owner mismatch: on=%v owner=%q product=%q err=%v", on, owner.Selector.ProductRoot, product, err)
	}
	capturePath := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", runID+".json")
	captureRaw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	if count, _ := n110CountFailureEvents(t, owner.Settings.RuntimeRoot, runID); count != 1 {
		t.Fatalf("fixture capture event count=%d", count)
	}
	exePath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exeRaw, err := os.ReadFile(exePath)
	if err != nil {
		t.Fatal(err)
	}
	evidence := releaseSliceCaptureReceipt{
		Schema: "air-worker.release-slice.capture/v1", SliceID: nativeReleaseSliceR02,
		CandidateVersion: version, CandidateBinarySHA256: learnSHA(exeRaw),
		PlanBeforeSHA256: learnSHA(planRaw), RunID: runID,
		CaptureReceiptSHA256: learnSHA(captureRaw), ReplayReceiptSHA256: learnSHA(captureRaw),
		ValidateExitCode: 2, ReplayExitCode: 0, CauseCode: "validate-run-config-missing",
		EventCount: 1, ReplayEventCount: 1, CreatedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	receiptRaw, err := json.MarshalIndent(evidence, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(product, "docs", "receipts", "RELEASE_0_11_9_02.json")
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, append(receiptRaw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return receiptPath
}

func TestReleaseSliceTransitionRequiresSignedCaptureAndPreservesNumericPlan(t *testing.T) {
	product, planRaw, planPath := releaseSliceTransitionFixture(t)
	writeReleaseSliceCaptureFixture(t, product, planRaw, "AW-CLI-R02-transition-test-001")
	legacyBefore, err := os.ReadFile(filepath.Join(product, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := transitionR02ToR03(product); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(product)
	if len(state.Problems) != 0 || state.NextStep != "R0.11.9-03" || state.ActiveStep.Status != "open" {
		t.Fatalf("R02 transition readback failed: %+v", state)
	}
	if string(after) != string(expectedReleaseSliceR02ToR03(planRaw)) {
		t.Fatal("transition changed bytes outside the two release-slice status tokens")
	}
	legacyAfter, err := os.ReadFile(filepath.Join(product, "PLAN.md"))
	if err != nil || string(legacyAfter) != string(legacyBefore) {
		t.Fatalf("legacy numeric PLAN changed: err=%v before=%q after=%q", err, legacyBefore, legacyAfter)
	}
}

func TestReleaseSliceTransitionRejectsConcurrentPlanChange(t *testing.T) {
	product, planRaw, planPath := releaseSliceTransitionFixture(t)
	writeReleaseSliceCaptureFixture(t, product, planRaw, "AW-CLI-R02-transition-test-002")
	changed := append(append([]byte(nil), planRaw...), '\n')
	if err := os.WriteFile(planPath, changed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := transitionR02ToR03(product); err == nil {
		t.Fatal("concurrent plan change was not rejected")
	}
	got, err := os.ReadFile(planPath)
	if err != nil || string(got) != string(changed) {
		t.Fatalf("failed transition modified concurrent plan: err=%v", err)
	}
}

func TestReleaseSliceTransitionRejectsTamperedHMACEvenWithUpdatedDigest(t *testing.T) {
	product, planRaw, planPath := releaseSliceTransitionFixture(t)
	releasePath := writeReleaseSliceCaptureFixture(t, product, planRaw, "AW-CLI-R02-transition-test-tampered")
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("active owner unavailable: on=%v err=%v", on, err)
	}
	capturePath := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", "AW-CLI-R02-transition-test-tampered.json")
	captureRaw, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatal(err)
	}
	var cli map[string]any
	if err := json.Unmarshal(captureRaw, &cli); err != nil {
		t.Fatal(err)
	}
	cli["receipt_hmac_sha256"] = strings.Repeat("0", 64)
	tamperedCapture, err := json.MarshalIndent(cli, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(capturePath, append(tamperedCapture, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	releaseRaw, err := os.ReadFile(releasePath)
	if err != nil {
		t.Fatal(err)
	}
	var release releaseSliceCaptureReceipt
	if err := json.Unmarshal(releaseRaw, &release); err != nil {
		t.Fatal(err)
	}
	release.CaptureReceiptSHA256 = learnSHA(append(tamperedCapture, '\n'))
	release.ReplayReceiptSHA256 = release.CaptureReceiptSHA256
	updatedRelease, err := json.MarshalIndent(release, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(releasePath, append(updatedRelease, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := transitionR02ToR03(product); err == nil {
		t.Fatal("tampered HMAC passed transition validation")
	}
	got, err := os.ReadFile(planPath)
	if err != nil || string(got) != string(planRaw) {
		t.Fatalf("HMAC rejection changed release plan: err=%v", err)
	}
}

func TestR02ReviewDeferralIsLimitedToDisposableValidate(t *testing.T) {
	scratch, err := os.MkdirTemp("", "r02-review-deferral-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(scratch)
	t.Setenv("TMPDIR", scratch)
	t.Setenv(releaseSliceReviewDeferEnv, "1")
	target, err := os.MkdirTemp(scratch, "target-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(target)
	args := []string{"validate", "-product", target, "-json"}
	if !shouldDeferOwnCLIReviewForR02(args, 2) {
		t.Fatal("exact disposable validate preflight should defer reviewer dispatch")
	}
	if shouldDeferOwnCLIReviewForR02([]string{"plan", "node", "new"}, 2) ||
		shouldDeferOwnCLIReviewForR02(args, 0) ||
		shouldDeferOwnCLIReviewForR02([]string{"validate", "-product", target}, 2) {
		t.Fatal("R02 reviewer deferral escaped its exact action/exit/argv boundary")
	}
	if err := os.WriteFile(filepath.Join(target, "run-config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if shouldDeferOwnCLIReviewForR02(args, 2) {
		t.Fatal("disposable target with a run-config must not defer reviewer dispatch")
	}
}

const r02ReplayHelperEnv = "AIR_WORKER_R02_REPLAY_HELPER"
const r02ReplayArgsEnv = "AIR_WORKER_R02_REPLAY_ARGS"

func TestR02ReplayProcessHelper(t *testing.T) {
	if os.Getenv(r02ReplayHelperEnv) != "1" {
		return
	}
	var args []string
	if err := json.Unmarshal([]byte(os.Getenv(r02ReplayArgsEnv)), &args); err != nil {
		t.Fatal(err)
	}
	if code := cmdInternalSelfCLIReplay(args); code != 0 {
		t.Fatalf("internal replay command exit=%d", code)
	}
}

func TestR02ReplayDoesNotDuplicateAcrossFreshProcess(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	scratch, err := releaseCaptureScratchRoot()
	if err != nil {
		t.Fatal(err)
	}
	target, err := os.MkdirTemp(scratch, "r02-replay-target-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(target)
	runID := "AW-CLI-R02-process-replay-test-001"
	args := []string{"validate", "-product", target, "-json"}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, runID, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(settings.RuntimeRoot, "cli-failures", runID+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	argsJSON, err := json.Marshal([]string{"-run-id", runID, "-product", target})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestR02ReplayProcessHelper$")
	cmd.Env = replaceTestEnv(os.Environ(), r02ReplayHelperEnv, "1")
	cmd.Env = replaceTestEnv(cmd.Env, r02ReplayArgsEnv, string(argsJSON))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("fresh-process replay failed: %v output=%s", err, output)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(before) {
		t.Fatalf("fresh-process replay modified immutable capture receipt: err=%v", err)
	}
	count, event := n110CountFailureEvents(t, settings.RuntimeRoot, runID)
	if count != 1 || event["kind"] != "run_completed" {
		t.Fatalf("fresh-process replay event count=%d event=%v", count, event)
	}
}

func replaceTestEnv(env []string, key, value string) []string {
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		name, _, ok := strings.Cut(entry, "=")
		if ok && strings.EqualFold(name, key) {
			continue
		}
		out = append(out, entry)
	}
	return append(out, key+"="+value)
}
