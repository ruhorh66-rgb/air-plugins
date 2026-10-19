package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func r04PreparedReviewerFixture(t *testing.T) (r03TestFixture, selfLearningOwner) {
	t.Helper()
	f := r03PreparedReviewedFixture(t)
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("R04 fixture owner: %v", err)
	}
	state := inspectReleaseSlices(f.Product)
	source, err := releaseR03AuthenticatedSnapshot(f.Product, state, f.Before, owner)
	if err != nil {
		t.Fatal(err)
	}
	r03, err := releaseR03TrustedReviewedAction(owner, source, f.Before)
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseR03PersistReviewedReceipt(f.Product, r03); err != nil {
		t.Fatal(err)
	}
	if err := transitionR03ToR04(f.Product); err != nil {
		t.Fatal(err)
	}
	state = inspectReleaseSlices(f.Product)
	if len(state.Problems) != 0 || state.NextStep != nativeReleaseSliceR04 ||
		state.ActiveStep.Status != "open" || state.ActiveStep.Kind != "test" {
		t.Fatalf("R04 fixture did not advance: %+v", state)
	}
	return f, owner
}

func TestR04PlanTransitionPreservesAllNonstatusBytes(t *testing.T) {
	original, err := os.ReadFile("../plan/release-slices.json")
	if err != nil {
		t.Fatal(err)
	}
	r03 := expectedReleaseSliceR02ToR03(original)
	if r03 == nil {
		t.Fatal("R02 to R03 predecessor not recognized")
	}
	r04 := expectedReleaseSliceR03ToR04(r03)
	if r04 == nil {
		t.Fatal("R03 to R04 predecessor not recognized")
	}
	back := expectedReleaseSliceR04ToR03(r04)
	if !bytes.Equal(back, r03) {
		t.Fatal("R04 inverse changed source bytes")
	}
	r05 := expectedReleaseSliceR04ToR05(r04)
	if r05 == nil || bytes.Equal(r05, r04) {
		t.Fatal("R04 to R05 does not advance exact step")
	}
	if expectedReleaseSliceR04ToR05(original) != nil ||
		expectedReleaseSliceR04ToR05(r03) != nil ||
		expectedReleaseSliceR04ToR05(append(append([]byte(nil), r04...), r04...)) != nil {
		t.Fatal("R04 stale, duplicate or premature release-plan state accepted")
	}
	// The authoritative release parser must select exactly the QA gate.
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan", "release-slices.json"), r05, 0600); err != nil {
		t.Fatal(err)
	}
	s := inspectReleaseSlices(dir)
	if len(s.Problems) != 0 || s.NextStep != "R0.11.9-05" || s.ActiveStep.Kind != "qa_submit" {
		t.Fatalf("R05 post-transition state invalid: %+v", s)
	}
}

func TestR04NativeMeasuredEffectExactOneAndReplayNoNewReviewer(t *testing.T) {
	f, owner := r04PreparedReviewerFixture(t)
	planPath := filepath.Join(f.Product, "plan", "release-slices.json")
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := releaseR04Predecessor(f.Product, owner, inspectReleaseSlices(f.Product), before)
	if err != nil {
		t.Fatalf("R04 genuine R02/R03 reviewer not authenticated: %v", err)
	}
	if _, err := releaseR04ObservedUse(f.Product, owner, p, before); err == nil {
		t.Fatal("R04 credited a loaded/reviewed procedure without a real native operation")
	}
	if err := releaseR04SealAuthorization(f.Product, owner, p, before); err != nil {
		t.Fatalf("signed R04 OPEN authorization failed: %v", err)
	}
	args := []string{"validate", "-product", f.Product, "-json"}
	use, err := nativePreflightGuard(args)
	if err != nil || !use.Deferred || use.ReceiptPath == "" {
		t.Fatalf("fresh native action did not yield sealed use: %+v %v", use, err)
	}
	got, err := releaseR04ObservedUse(f.Product, owner, p, before)
	if err != nil {
		t.Fatalf("R04 failed to authenticate actual learned procedure use: %v", err)
	}
	if got.Target != f.ReviewedRule.Target || got.TargetSHA != f.ReviewedRule.SHA256 ||
		got.OriginalRunID != f.Prior.RunID ||
		got.ValidationExecuted || got.Outcome != "deferred_missing_configuration" ||
		got.Effect != "avoided_repeat_validation_with_missing_config" ||
		got.ConfirmedEventID == "" || got.SignedResultSHA == "" {
		t.Fatalf("R04 effect is inaccurate: %+v", got)
	}
	initialEvents, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if rc := runNativeReleaseSliceR04(f.Product, inspectReleaseSlices(f.Product)); rc != 0 {
		t.Fatalf("R04 core failed with module evidence already present: %d", rc)
	}
	st := inspectReleaseSlices(f.Product)
	if st.NextStep != "R0.11.9-05" || st.ActiveStep.Status != "open" {
		t.Fatalf("R04 did not advance exact next release step: %+v", st)
	}
	saved, err := releaseR04ReadReceipt(f.Product)
	if err != nil {
		t.Fatal(err)
	}
	if saved.ConfirmedEventID != got.ConfirmedEventID ||
		saved.SignedIntentSHA != got.SignedIntentSHA ||
		saved.SignedResultSHA != got.SignedResultSHA ||
		saved.Effect != got.Effect || saved.ValidationExecuted {
		t.Fatalf("R04 readback is not the exact authenticated proof: %+v", saved)
	}
	if _, err := releaseR04ObservedUse(f.Product, owner, p, before); err != nil {
		t.Fatalf("R04 source-only receipt was not stable on replay: %v", err)
	}
	if rc := runNativeReleaseSliceR04(f.Product, st); rc == 0 {
		t.Fatal("already completed R04 was dispatched again")
	}
	afterEvents, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil || !bytes.Equal(initialEvents, afterEvents) {
		t.Fatal("R04 finalization/replay altered module-owned LEARN events")
	}
}

func TestR04RefusesUnmeasuredUseAndModuleRollback(t *testing.T) {
	f, owner := r04PreparedReviewerFixture(t)
	planPath := filepath.Join(f.Product, "plan", "release-slices.json")
	before, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := releaseR04Predecessor(f.Product, owner, inspectReleaseSlices(f.Product), before)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releaseR04ObservedUse(f.Product, owner, p, before); err == nil {
		t.Fatal("R04 fabricated use from only an APPLIED review")
	}
	// A plausible untrusted result is not independently HMAC-signed/module-owned.
	data := map[string]any{"schema": "air-worker.n093.typed-preflight-use/v1",
		"outcome": "deferred_missing_configuration", "effect": "avoided_repeat_validation_with_missing_config"}
	raw, _ := json.Marshal(data)
	dir := filepath.Join(f.Settings.RuntimeRoot, "native-preflight-results")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	id := learnSHA([]byte(f.Prior.RunID + "\x00" + p.Source.SourceEventID + "\x00" +
		f.ReviewedRule.Target + "@" + f.ReviewedRule.SHA256))
	if err := os.WriteFile(filepath.Join(dir, "N118-USED-"+id[:24]+".json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseR04ObservedUse(f.Product, owner, p, before); err == nil {
		t.Fatal("forged positive text became measured native effect")
	}
	if err := os.Remove(filepath.Join(dir, "N118-USED-"+id[:24]+".json")); err != nil {
		t.Fatal(err)
	}
	rb, err := executeSharedLearning(f.Product, f.Settings, "rollback", map[string]string{
		"proposal_id": "LP-" + p.Source.SourceEventID,
	})
	if err != nil || rb.Status == "" {
		t.Fatalf("module rollback setup failed: %+v %v", rb, err)
	}
	if _, err := releaseR04Predecessor(f.Product, owner, inspectReleaseSlices(f.Product), before); err == nil {
		t.Fatal("R04 still trusted rolled-back APPLIED evidence")
	}
	after, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("rollback/forged-use negative test changed R04 release-plan state")
	}
}

func TestR04RejectsRealUseRecordedBeforeSignedReleaseOpen(t *testing.T) {
	f, owner := r04PreparedReviewerFixture(t)
	planPath := filepath.Join(f.Product, "plan", "release-slices.json")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	p, err := releaseR04Predecessor(f.Product, owner, inspectReleaseSlices(f.Product), plan)
	if err != nil {
		t.Fatal(err)
	}
	// A real previous N093 effect is still NOT an R04 measured operation.
	// Only the native R04 runner may authorize its own subsequent attempt.
	args := []string{"validate", "-product", f.Product, "-json"}
	use, err := nativePreflightGuard(args)
	if err != nil || !use.Deferred {
		t.Fatalf("setup native use: %+v %v", use, err)
	}
	if err := releaseR04SealAuthorization(f.Product, owner, p, plan); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseR04ObservedUse(f.Product, owner, p, plan); err == nil {
		t.Fatal("R04 incorrectly credited a valid native use from before R04 OPEN authorization")
	}
	if after, err := os.ReadFile(planPath); err != nil || !bytes.Equal(after, plan) {
		t.Fatal("rejected earlier operation changed R04 release plan")
	}
}

func TestR04PinnedScratchRejectsWindowsAncestorJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("NTFS junction fixture is Windows-only")
	}
	owned := filepath.Join(t.TempDir(), "owned")
	if err := os.MkdirAll(owned, 0700); err != nil {
		t.Fatal(err)
	}
	safe := filepath.Join(owned, "scratch")
	if err := os.Mkdir(safe, 0700); err != nil {
		t.Fatal(err)
	}
	pinned, err := releaseR04OpenPinnedScratch(safe)
	if err != nil {
		t.Fatalf("ordinary scratch unexpectedly rejected: %v", err)
	}
	_ = pinned.Close()
	// mklink /J does not need elevated permissions on typical NTFS. If a
	// restricted host disallows it, preserve the OS policy; never request UAC.
	offroot := filepath.Join(t.TempDir(), "elsewhere")
	if err := os.MkdirAll(filepath.Join(offroot, "scratch"), 0700); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(owned, "alias")
	command := exec.Command("cmd", "/c", "mklink", "/J", junction, offroot)
	if output, err := command.CombinedOutput(); err != nil {
		t.Skipf("junction not available on this host: %v %s", err, string(output))
	}
	escaped := filepath.Join(junction, "scratch")
	if external, err := releaseR04OpenPinnedScratch(escaped); err == nil {
		_ = external.Close()
		t.Fatal("external parent junction was accepted for R04 signed scratch creation")
	}
}

func TestR04SignedScratchHandlesBlockAncestorRenameWhileChildWouldRun(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows handle sharing semantics")
	}
	root := t.TempDir()
	parent := filepath.Join(root, "parent")
	scratch := filepath.Join(parent, "scratch")
	target := filepath.Join(scratch, "air-worker-r02-capture-signed-hold")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	release, err := releaseR04HoldSignedScratchUntilChild(target)
	if err != nil {
		t.Fatalf("cannot hold signed scratch chain: %v", err)
	}
	defer release()
	renamed := filepath.Join(root, "redirected-parent")
	if err := os.Rename(parent, renamed); err == nil {
		_ = os.Rename(renamed, parent)
		t.Fatal("R04 physical scratch parent was renamable during native child hold")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("R04 target disappeared despite directory handles: %v", err)
	}
}
