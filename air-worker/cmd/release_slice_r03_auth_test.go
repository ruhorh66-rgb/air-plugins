package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type r03TestFixture struct {
	Product      string
	Settings     sharedLearningSettings
	Prior        ownCLIFailureReceipt
	Before       []byte
	ReviewedRule selfLearningLoadedRule
}

func r03PreparedReviewedFixture(t *testing.T) r03TestFixture {
	t.Helper()
	product, settings, args, rule := n093NativeTypedFixture(t)
	priorRoot, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("native owner missing %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(priorRoot, args)
	if err != nil || !found {
		t.Fatalf("signed native failure missing %v", err)
	}
	original, err := n042HistoricalR02SourceBytes()
	if err != nil {
		t.Fatal(err)
	}
	r03State := expectedReleaseSliceR02ToR03(original)
	if r03State == nil {
		t.Fatal("R02 canonical status format changed")
	}
	if err := os.MkdirAll(filepath.Join(product, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	// Seal the R02 release envelope against the authentic PRE-transition
	// plan before advancing this disposable fixture to R03.
	if err := os.WriteFile(filepath.Join(product, "plan", "release-slices.json"), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(product, "docs", "receipts"), 0700); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	signedBytes, err := n093ReadRuntimeEvidence(settings.RuntimeRoot, "cli-failures/"+prior.RunID+".json", 32*1024)
	if err != nil {
		t.Fatal(err)
	}
	capture := releaseSliceCaptureReceipt{
		Schema: releaseSliceCaptureSchema, SliceID: nativeReleaseSliceR02,
		CandidateVersion: version, CandidateBinarySHA256: learnSHA(executable),
		PlanBeforeSHA256: learnSHA(original), RunID: prior.RunID,
		CaptureReceiptSHA256: learnSHA(signedBytes), ReplayReceiptSHA256: learnSHA(signedBytes),
		ValidateExitCode: 2, ReplayExitCode: 0, CauseCode: "validate-run-config-missing",
		EventCount: 1, ReplayEventCount: 1, CreatedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	// Use the source-owned production signer; never mint unsigned test
	// evidence and then accidentally treat it as a positive R03 proof.
	if err := sealR02ReleaseCaptureReceipt(priorRoot, product, &capture); err != nil {
		t.Fatalf("native signed R02 fixture: %v", err)
	}
	raw, err := json.MarshalIndent(capture, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "docs", "receipts", "RELEASE_0_11_9_02.json"), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "plan", "release-slices.json"), r03State, 0600); err != nil {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(product)
	if len(state.Problems) != 0 || state.NextStep != nativeReleaseSliceR03 {
		t.Fatalf("invalid R03 fixture: %+v", state)
	}
	return r03TestFixture{Product: product, Settings: settings, Prior: prior, Before: r03State, ReviewedRule: rule}
}

func TestR03AuthenticatedHistoricalReviewAdvancesOnlyReleaseCheckpoint(t *testing.T) {
	if !n044HistoricalR03R04PositiveOnly(t) {
		return
	}
	f := r03PreparedReviewedFixture(t)
	priorEvent, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, func() selfLearningOwner {
		o, enabled, e := activeAirWorkerSelfLearningOwner()
		if e != nil || !enabled {
			t.Fatal(e)
		}
		return o
	}())
	if err != nil {
		t.Fatalf("signed R02 predecessor rejected: %v", err)
	}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("owner: %v", err)
	}
	nativeReceipt, err := releaseR03TrustedReviewedAction(owner, source, f.Before)
	if err != nil {
		t.Fatalf("signed reviewer/APPLIED capsule rejected: %v", err)
	}
	if nativeReceipt.SourceRunID != f.Prior.RunID || nativeReceipt.TargetSHA256 != f.ReviewedRule.SHA256 ||
		nativeReceipt.AppliedLedgerCount != 1 || nativeReceipt.SourceEventID == "" ||
		nativeReceipt.ReviewID == "" || nativeReceipt.ReviewerResponseSHA256 == "" ||
		nativeReceipt.ReviewerCompletionSHA256 == "" {
		t.Fatalf("R03 signed evidence incomplete: %+v", nativeReceipt)
	}
	// A source-owned release receipt records the historical, authenticated
	// reviewer/APPLIED checkpoint. It grants NO native use and cannot keep an
	// already rolled-back procedure active.
	if err := releaseR03PersistReviewedReceipt(f.Product, nativeReceipt); err != nil {
		t.Fatalf("signed R03 fixture receipt could not be saved: %v", err)
	}
	if _, err := releaseR03VerifyReceipt(f.Product, owner, inspectReleaseSlices(f.Product), f.Before); err != nil {
		t.Fatalf("source-only signed receipt did not revalidate: %v", err)
	}
	if err := transitionR03ToR04(f.Product); err != nil {
		t.Fatalf("authenticated R03 checkpoint failed to transition under native plan writer: %v", err)
	}
	state := inspectReleaseSlices(f.Product)
	if len(state.Problems) != 0 || state.NextStep != "R0.11.9-04" ||
		state.ActiveStep.ID != "R0.11.9-04" || state.ActiveStep.Status != "open" {
		t.Fatalf("native R03 historical checkpoint did not open exactly R04: %+v", state)
	}
	if rc := runNativeReleaseSliceR03(f.Product, state); rc == 0 {
		t.Fatal("replayed R03 was allowed to rerun reviewer after R04 became active")
	}
	if rc := runNativeReleaseSliceR04(f.Product, state); rc == 0 {
		t.Fatal("R04 credited R03 review without a signed later actual operation")
	}
	planBytes, err := os.ReadFile(filepath.Join(f.Product, "plan", "release-slices.json"))
	if err != nil || !bytes.Equal(planBytes, expectedReleaseSliceR03ToR04(f.Before)) {
		t.Fatal("R03 native transition did not preserve byte-exact unrelated release data")
	}
	receipt, err := releaseR03ReadReceipt(f.Product)
	if err != nil {
		t.Fatalf("isolated source-only reviewer receipt missing: %v", err)
	}
	if receipt.SourceRunID != f.Prior.RunID || receipt.TargetSHA256 != f.ReviewedRule.SHA256 ||
		receipt.PlanBeforeSHA256 != learnSHA(f.Before) || receipt.ReviewerResponseSHA256 == "" {
		t.Fatalf("source SHA/plan/receipt mismatch: %+v", receipt)
	}
	currentEvents, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(priorEvent, currentEvents) {
		t.Fatal("R03 reprocessed reviewer or altered shared module LEARN events despite existing candidate")
	}
	if n104Events(t, f.Settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("R03 reviewer stage fabricated R04 actual-use event")
	}
}

func TestR03InvalidR02CaptureOrReviewAttestationFailsBeforePlanChange(t *testing.T) {
	for _, variant := range []string{"r02-hash", "missing-completion", "tampered-candidate"} {
		t.Run(variant, func(t *testing.T) {
			f := r03PreparedReviewedFixture(t)
			original := append([]byte(nil), f.Before...)
			owner, enabled, err := activeAirWorkerSelfLearningOwner()
			if err != nil || !enabled {
				t.Fatal(err)
			}
			switch variant {
			case "r02-hash":
				path := filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_02.json")
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var capture releaseSliceCaptureReceipt
				if err := json.Unmarshal(data, &capture); err != nil {
					t.Fatal(err)
				}
				capture.CaptureReceiptSHA256 = strings.Repeat("a", 64)
				edited, err := json.Marshal(capture)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, edited, 0600); err != nil {
					t.Fatal(err)
				}
			case "missing-completion":
				source, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, owner)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(f.Settings.RuntimeRoot, "review-attestations", source.SourceEventID+"-complete.json")
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "tampered-candidate":
				source, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, owner)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(f.Settings.RuntimeRoot, "reviews", source.SourceEventID+".json")
				n093EditReviewFixture(t, path, func(m map[string]any) { m["candidate_sha256"] = strings.Repeat("0", 64) })
			}
			state := inspectReleaseSlices(f.Product)
			source, err := releaseR03AuthenticatedSnapshot(f.Product, state, f.Before, owner)
			if variant == "r02-hash" {
				if err == nil {
					t.Fatal("forged R02 signed capture receipt selected as R03 source")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, err := releaseR03TrustedReviewedAction(owner, source, f.Before); err == nil {
					t.Fatal("unauthenticated reviewer was accepted as R03 safe procedure")
				}
			}
			current, err := os.ReadFile(filepath.Join(f.Product, "plan", "release-slices.json"))
			if err != nil || !bytes.Equal(current, original) {
				t.Fatal("R03 negative evidence mutated release plan")
			}
			if _, err := os.Stat(filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_03.json")); !os.IsNotExist(err) {
				t.Fatalf("R03 negative evidence created release receipt: %v", err)
			}
		})
	}
}

func TestR03NativeRefusesTamperedImmutableReviewerReceipt(t *testing.T) {
	f := r03PreparedReviewedFixture(t)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(f.Product)
	originalPlan := append([]byte(nil), f.Before...)
	source, err := releaseR03AuthenticatedSnapshot(f.Product, state, f.Before, owner)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := releaseR03TrustedReviewedAction(owner, source, f.Before)
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseR03PersistReviewedReceipt(f.Product, receipt); err != nil {
		t.Fatal(err)
	}
	if err := releaseR03PersistReviewedReceipt(f.Product, receipt); err == nil {
		t.Fatal("R03 overwrote the original source-owned receipt")
	}
	receipt.ProposalID = "LP-FORGED-REVIEWER"
	target := filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_03.json")
	forged, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, forged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseR03VerifyReceipt(f.Product, owner, inspectReleaseSlices(f.Product), originalPlan); err == nil {
		t.Fatal("forged R03 reviewer receipt passed strict module/proposal verification")
	}
	if err := transitionR03ToR04(f.Product); err == nil {
		t.Fatal("tampered R03 reviewer receipt authorized native plan transition")
	}
	after, err := os.ReadFile(filepath.Join(f.Product, "plan", "release-slices.json"))
	if err != nil || !bytes.Equal(after, originalPlan) {
		t.Fatal("failed R03 receipt corrupted native plan")
	}
	if n104Events(t, f.Settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("forged release review manufactured a later native use")
	}
}

func TestR03StatusGuardRefusesReviewBeforeCoreR02Completion(t *testing.T) {
	f := r03PreparedReviewedFixture(t)
	initial, err := n042HistoricalR02SourceBytes()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(f.Product, "plan", "release-slices.json")
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(f.Product)
	if state.NextStep != "R0.11.9-02" {
		t.Fatalf("fixture predecessor mismatch %+v", state)
	}
	if rc := runNativeReleaseSliceR03(f.Product, state); rc == 0 {
		t.Fatal("R03 ran before the only supported contiguous R02 transition")
	}
	if _, err := os.Stat(filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_03.json")); !os.IsNotExist(err) {
		t.Fatal("R03 minted a release receipt with R02 still OPEN")
	}
}

// An isolated helper reviewer may produce a safe-looking module candidate, but
// it is NOT the production Codex adapter. The native R03 loop must refuse to
// publish/advance without the actual model-response signer receipt.
func TestR03UnattestedTestReviewerCannotPromoteOrRetry(t *testing.T) {
	if !n044HistoricalR03R04PositiveOnly(t) {
		return
	}
	product, settings, _ := selfLearningFixture(t)
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight-n093")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	selector, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("missing self owner selector: %v", err)
	}
	selector.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(selector); err != nil {
		t.Fatal(err)
	}
	args := []string{"validate", "-product", product, "-json"}
	runID := "AW-CLI-R03-UNATTESTED-REVIEWER-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, runID, nil); err != nil {
		t.Fatal(err)
	}
	original, err := n042HistoricalR02SourceBytes()
	if err != nil {
		t.Fatal(err)
	}
	r03 := expectedReleaseSliceR02ToR03(original)
	if r03 == nil {
		t.Fatal("invalid R02 predecessor plan")
	}
	if err := os.MkdirAll(filepath.Join(product, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(product, "docs", "receipts"), 0700); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(product, "plan", "release-slices.json")
	// The outer R02 envelope must be signed against its original release
	// plan. Only AFTER signing do we simulate the R02->R03 status transition.
	if err := os.WriteFile(planPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	captureRaw, err := n093ReadRuntimeEvidence(settings.RuntimeRoot, "cli-failures/"+runID+".json", 32*1024)
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	exeRaw, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	cap := releaseSliceCaptureReceipt{
		Schema: releaseSliceCaptureSchema, SliceID: nativeReleaseSliceR02,
		CandidateVersion: version, CandidateBinarySHA256: learnSHA(exeRaw),
		PlanBeforeSHA256: learnSHA(original), RunID: runID,
		CaptureReceiptSHA256: learnSHA(captureRaw), ReplayReceiptSHA256: learnSHA(captureRaw),
		ValidateExitCode: 2, ReplayExitCode: 0, CauseCode: "validate-run-config-missing",
		EventCount: 1, ReplayEventCount: 1, CreatedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("native signing owner missing: %v", err)
	}
	if err := sealR02ReleaseCaptureReceipt(owner, product, &cap); err != nil {
		t.Fatalf("sign native R02 envelope before source fixture review: %v", err)
	}
	data, err := json.Marshal(cap)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "docs", "receipts", "RELEASE_0_11_9_02.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, r03, 0600); err != nil {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(product)
	if len(state.Problems) != 0 || state.NextStep != nativeReleaseSliceR03 {
		t.Fatalf("bad release fixture: %+v", state)
	}
	if rc := runNativeReleaseSliceR03(product, state); rc == 0 {
		t.Fatal("test-only model output without production response attestation promoted R03")
	}
	// The existing module reviewer may apply a safe candidate. However the
	// native R03 must reject it without the actual Codex response HMAC and
	// must not make a second model attempt when retrying the same run.
	reviews, err := filepath.Glob(filepath.Join(settings.RuntimeRoot, "reviews", "RV-*.json"))
	if err != nil || len(reviews) != 1 {
		t.Fatalf("expected exactly one terminal module reviewer attempt: files=%v err=%v", reviews, err)
	}
	beforeRV, err := os.ReadFile(reviews[0])
	if err != nil {
		t.Fatal(err)
	}
	beforeEvents, err := os.ReadFile(filepath.Join(settings.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if rc := runNativeReleaseSliceR03(product, inspectReleaseSlices(product)); rc == 0 {
		t.Fatal("second R03 invocation repeated a missing-production-attestation reviewer")
	}
	afterRV, err := os.ReadFile(reviews[0])
	if err != nil || !bytes.Equal(afterRV, beforeRV) {
		t.Fatal("replayed R03 mutated the original canonical reviewer")
	}
	afterEvents, err := os.ReadFile(filepath.Join(settings.RuntimeRoot, "events.jsonl"))
	if err != nil || !bytes.Equal(afterEvents, beforeEvents) {
		t.Fatal("replayed R03 invoked another model or mutated the module event log")
	}
	observed, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(observed, r03) {
		t.Fatal("unattested reviewer changed R03 release plan")
	}
	if _, err := os.Stat(filepath.Join(product, "docs", "receipts", "RELEASE_0_11_9_03.json")); !os.IsNotExist(err) {
		t.Fatalf("R03 minted receipt without native production reviewer attestation: %v", err)
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("R03 helper reviewer fabricated an R04 use event")
	}
}

func TestR03RefusesOrphanedErrorReviewWithoutAnotherModelAttempt(t *testing.T) {
	f := r03PreparedReviewedFixture(t)
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatal(err)
	}
	source, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, owner)
	if err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(f.Settings.RuntimeRoot, "reviews", source.SourceEventID+".json")
	row, _, err := n093ReviewCanonicalRecord(owner, source.SourceEventID)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := row["review_id"].(string)
	canonical := filepath.Join(f.Settings.RuntimeRoot, "reviews", id+".json")
	if err := os.Remove(index); err != nil {
		t.Fatal(err)
	}
	n093EditReviewFixture(t, canonical, func(r map[string]any) {
		r["phase"] = "complete"
		r["outcome"] = "error"
		r["error"] = "simulated interrupted reviewer callback"
		r["attempt"] = 1
	})
	before, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	seen, err := releaseR03HasExistingReviewerAttempt(owner, f.Prior.RunID, source.SourceEventID)
	if err != nil || !seen {
		t.Fatalf("orphan RV without event-index did not block model re-invocation: seen=%v err=%v", seen, err)
	}
	if err := releaseR03RunNativeReviewerOnce(f.Product, owner, f.Prior.RunID, source.SourceEventID); err == nil {
		t.Fatal("orphan failed RV caused a second production reviewer invocation")
	}
	after, err := os.ReadFile(canonical)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatal("orphan reviewer attempt was rewritten")
	}
	eventsAfter, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil || !bytes.Equal(eventsAfter, events) {
		t.Fatal("orphan reviewer caused new LEARN event")
	}
	if _, err := os.Stat(index); !os.IsNotExist(err) {
		t.Fatal("R03 recreated an event-index to retry orphan RV")
	}
}

func TestR03ConcurrentNativeReviewAttemptsStayAtMostOne(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	args := []string{"validate", "-product", product, "-json"}
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight-n093")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	selector, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatal(err)
	}
	selector.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(selector); err != nil {
		t.Fatal(err)
	}
	runID := "AW-CLI-R03-CONCURRENT-SIGNED-RUN-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, runID, nil); err != nil {
		t.Fatal(err)
	}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatal(err)
	}
	signed, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatal(err)
	}
	eventID, err := r03OriginalEventID(owner, signed, func() string {
		raw, e := n093ReadRuntimeEvidence(settings.RuntimeRoot, "cli-failures/"+runID+".json", 32*1024)
		if e != nil {
			t.Fatal(e)
		}
		return learnSHA(raw)
	}())
	if err != nil {
		t.Fatal(err)
	}
	ch := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { ch <- releaseR03RunNativeReviewerOnce(product, owner, runID, eventID) }()
	}
	first, second := <-ch, <-ch
	// The test helper is not the production adapter and intentionally lacks
	// a signed model-response proof. Both attempts therefore fail closed.
	if first == nil || second == nil {
		t.Fatalf("concurrent test-only reviewer was accepted: first=%v second=%v", first, second)
	}
	files, err := filepath.Glob(filepath.Join(settings.RuntimeRoot, "reviews", "RV-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("concurrent R03 calls created %d canonical reviewer attempts; want <=1", len(files))
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	var rv struct {
		Attempt int `json:"attempt"`
	}
	if err := json.Unmarshal(raw, &rv); err != nil {
		t.Fatal(err)
	}
	if rv.Attempt != 1 {
		t.Fatalf("shared module repeated production model attempt %d times", rv.Attempt)
	}
}

func TestR03RollbackAfterSignedReviewPreventsReleaseStatusPromotion(t *testing.T) {
	f := r03PreparedReviewedFixture(t)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(f.Product)
	source, err := releaseR03AuthenticatedSnapshot(f.Product, state, f.Before, owner)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := releaseR03TrustedReviewedAction(owner, source, f.Before)
	if err != nil {
		t.Fatal(err)
	}
	if err := releaseR03PersistReviewedReceipt(f.Product, receipt); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseR03VerifyReceipt(f.Product, owner, state, f.Before); err != nil {
		t.Fatal(err)
	}
	rb, err := executeSharedLearning(f.Product, f.Settings, "rollback", map[string]string{"proposal_id": "LP-" + source.SourceEventID})
	if err != nil {
		t.Fatalf("isolated module rollback failed: status=%s err=%v", rb.Status, err)
	}
	if _, err := releaseR03VerifyReceipt(f.Product, owner, inspectReleaseSlices(f.Product), f.Before); err == nil {
		t.Fatal("reviewer/APPLIED receipt remained live after module-owned rollback")
	}
	if err := transitionR03ToR04(f.Product); err == nil {
		t.Fatal("native R03 permitted historical release checkpoint after earlier rollback")
	}
	now, err := os.ReadFile(filepath.Join(f.Product, "plan", "release-slices.json"))
	if err != nil || !bytes.Equal(now, f.Before) {
		t.Fatal("R03 plan advanced during rollback race")
	}
}

func TestR03ReleaseReceiptsRejectExternalNTFSHardlinks(t *testing.T) {
	for _, kind := range []string{"r02", "r03"} {
		t.Run(kind, func(t *testing.T) {
			f := r03PreparedReviewedFixture(t)
			owner, on, err := activeAirWorkerSelfLearningOwner()
			if err != nil || !on {
				t.Fatal(err)
			}
			source, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, owner)
			if err != nil {
				t.Fatal(err)
			}
			r03Receipt := filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_03.json")
			if kind == "r03" {
				receipt, err := releaseR03TrustedReviewedAction(owner, source, f.Before)
				if err != nil {
					t.Fatal(err)
				}
				if err := releaseR03PersistReviewedReceipt(f.Product, receipt); err != nil {
					t.Fatal(err)
				}
			}
			path := r03Receipt
			if kind == "r02" {
				path = filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_02.json")
			}
			extra := filepath.Join(t.TempDir(), "outside-native-release-receipt")
			if err := os.Link(path, extra); err != nil {
				t.Skipf("test filesystem cannot create external hardlink without elevation: %v", err)
			}
			if kind == "r02" {
				if _, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, owner); err == nil {
					t.Fatal("external hardlinked original R02 was trusted as module-owned proof")
				}
			} else {
				if _, err := releaseR03ReadReceipt(f.Product); err == nil {
					t.Fatal("external hardlinked R03 was trusted as immutable native receipt")
				}
			}
			current, err := os.ReadFile(filepath.Join(f.Product, "plan", "release-slices.json"))
			if err != nil || !bytes.Equal(current, f.Before) {
				t.Fatal("linked evidence changed native release plan")
			}
		})
	}
}

func TestR03RejectsDuplicateOriginalEventIDFromAnotherRun(t *testing.T) {
	f := r03PreparedReviewedFixture(t)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatal(err)
	}
	source, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, owner)
	if err != nil {
		t.Fatal(err)
	}
	forged := map[string]string{
		"schema": "air.learning.event/v1", "event_id": source.SourceEventID,
		"product_id": "air-worker", "run_id": "AW-CLI-FORGED-OTHER-SOURCE",
		"kind": "run_completed", "source": "air-worker-cli",
		"principal": "air-worker", "class": "air-worker-cli-preflight",
	}
	b, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	fpath := filepath.Join(f.Settings.RuntimeRoot, "events.jsonl")
	j, err := os.OpenFile(fpath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = j.Write(append(b, '\n')); err != nil {
		j.Close()
		t.Fatal(err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := releaseR03AuthenticatedSnapshot(f.Product, inspectReleaseSlices(f.Product), f.Before, owner); err == nil {
		t.Fatal("duplicate original event ID on unrelated run was accepted")
	}
}

func TestR03NativeRunnerReusesAuthenticatedReviewedCheckpointExactlyOnce(t *testing.T) {
	if !n044HistoricalR03R04PositiveOnly(t) {
		return
	}
	f := r03PreparedReviewedFixture(t)
	originalEvents, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	before := append([]byte(nil), f.Before...)
	state := inspectReleaseSlices(f.Product)
	if code := runNativeReleaseSliceR03(f.Product, state); code != 0 {
		t.Fatalf("module-reviewed R03 history did not transition the disposable plan: rc=%d", code)
	}
	after := inspectReleaseSlices(f.Product)
	if len(after.Problems) != 0 || after.NextStep != "R0.11.9-04" ||
		!after.HasActiveStep || after.ActiveStep.Status != "open" {
		t.Fatalf("no exact historical R03 to R04 plan transition: %+v", after)
	}
	plan, err := os.ReadFile(filepath.Join(f.Product, "plan", "release-slices.json"))
	if err != nil || !bytes.Equal(plan, expectedReleaseSliceR03ToR04(before)) {
		t.Fatal("native R03 changed bytes outside approved contiguous status transition")
	}
	receiptPath := filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_03.json")
	receiptBefore, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := runNativeReleaseSliceR03(f.Product, after); code == 0 {
		t.Fatal("R03 replay executed despite already-open R04")
	}
	receiptAfter, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(receiptBefore, receiptAfter) {
		t.Fatal("R03 replay replaced the immutable historical reviewer receipt")
	}
	currentEvents, err := os.ReadFile(filepath.Join(f.Settings.RuntimeRoot, "events.jsonl"))
	if err != nil || !bytes.Equal(originalEvents, currentEvents) {
		t.Fatal("R03 reexecuted a reviewer or added module LEARN events")
	}
	if got := n104Events(t, f.Settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("historical reviewer applied event forged %d native uses", got)
	}
	if rc := runNativeReleaseSliceR04(f.Product, after); rc == 0 {
		t.Fatal("R04 credited historical R03 status without a real later operation")
	}
}

func TestR03ModuleRollbackAfterHistoricalCheckpointCannotCreditR04Use(t *testing.T) {
	if !n044HistoricalR03R04PositiveOnly(t) {
		return
	}
	f := r03PreparedReviewedFixture(t)
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("self owner unavailable: %v", err)
	}
	state := inspectReleaseSlices(f.Product)
	source, err := releaseR03AuthenticatedSnapshot(f.Product, state, f.Before, owner)
	if err != nil {
		t.Fatal(err)
	}
	if code := runNativeReleaseSliceR03(f.Product, state); code != 0 {
		t.Fatalf("valid R03 historical checkpoint was not recorded: rc=%d", code)
	}
	after := inspectReleaseSlices(f.Product)
	if after.NextStep != "R0.11.9-04" {
		t.Fatalf("missing R04 checkpoint: %+v", after)
	}
	planPath := filepath.Join(f.Product, "plan", "release-slices.json")
	checkpointBytes, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	receiptPath := filepath.Join(f.Product, "docs", "receipts", "RELEASE_0_11_9_03.json")
	receiptBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	// The published v0.1.0 module owns the rollback/ledger mutation.
	result, err := executeSharedLearning(f.Product, f.Settings, "rollback",
		map[string]string{"proposal_id": "LP-" + source.SourceEventID})
	if err != nil {
		t.Fatalf("native shared-module rollback failed: status=%q err=%v", result.Status, err)
	}
	if _, err := releaseR03TrustedReviewedAction(owner, source, f.Before); err == nil {
		t.Fatal("R03 historical receipt was mistaken for current APPLIED after module rollback")
	}
	if rc := runNativeReleaseSliceR04(f.Product, after); rc == 0 {
		t.Fatal("R04 accepted a rolled-back learned procedure as a current measured use")
	}
	if code := runNativeReleaseSliceR03(f.Product, after); code == 0 {
		t.Fatal("rolled-back R03 history allowed replaying an already completed release slice")
	}
	planAfter, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(planAfter, checkpointBytes) {
		t.Fatal("later module rollback rewrote a historical, source-owned release checkpoint")
	}
	receiptAfter, err := os.ReadFile(receiptPath)
	if err != nil || !bytes.Equal(receiptAfter, receiptBytes) {
		t.Fatal("module rollback changed source-owned immutable historical reviewer receipt")
	}
	if got := n104Events(t, f.Settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("historical R03 checkpoint or later module rollback fabricated %d uses", got)
	}
}
