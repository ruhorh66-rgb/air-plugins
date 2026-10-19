package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const releaseSliceR03ReceiptSchema = "air-worker.release-slice.review/v1"
const releaseSliceR03ReceiptRef = "docs/receipts/RELEASE_0_11_9_03.json"

type releaseR03ReviewReceipt struct {
	Schema                   string `json:"schema"`
	SliceID                  string `json:"slice_id"`
	CandidateVersion         string `json:"candidate_version"`
	CandidateBinarySHA256    string `json:"candidate_binary_sha256"`
	PlanBeforeSHA256         string `json:"plan_before_sha256"`
	R02ReceiptSHA256         string `json:"r02_receipt_sha256"`
	CaptureReceiptSHA256     string `json:"capture_receipt_sha256"`
	SourceRunID              string `json:"source_run_id"`
	SourceEventID            string `json:"source_event_id"`
	ReviewID                 string `json:"review_id"`
	ProposalID               string `json:"proposal_id"`
	CandidateSHA256          string `json:"candidate_sha256"`
	Target                   string `json:"target"`
	TargetSHA256             string `json:"target_sha256"`
	ReviewerResponseSHA256   string `json:"reviewer_response_sha256"`
	ReviewerCompletionSHA256 string `json:"reviewer_completion_sha256"`
	AppliedLedgerCount       int    `json:"applied_ledger_count"`
	Outcome                  string `json:"outcome"`
	CreatedAtUTC             string `json:"created_at_utc"`
}

type releaseR03Source struct {
	R02           releaseSliceCaptureReceipt
	Prior         ownCLIFailureReceipt
	R02SHA        string
	SourceEventID string
	BinarySHA     string
}

func readR03R02Receipt(product string, owner selfLearningOwner, planBefore []byte) (releaseR03Source, error) {
	var out releaseR03Source
	if err := safeReleaseReceipt(product, "docs/receipts/RELEASE_0_11_9_02.json"); err != nil {
		return out, err
	}
	raw, err := n093ReadRuntimeEvidence(product, "docs/receipts/RELEASE_0_11_9_02.json", 32*1024)
	if err != nil {
		return out, err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return out, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out.R02); err != nil {
		return out, err
	}
	var tail any
	if err := dec.Decode(&tail); err != io.EOF {
		return out, errors.New("R03: R02 receipt contains extra JSON data")
	}
	exe, err := os.Executable()
	if err != nil {
		return out, err
	}
	exeRaw, err := os.ReadFile(exe)
	if err != nil {
		return out, err
	}
	out.BinarySHA = learnSHA(exeRaw)
	cap := out.R02
	// The R02 outer envelope must be independently signed. An HMAC of the
	// inner CLI failure is NOT authority to substitute another release run,
	// candidate binary or release-plan snapshot.
	if err := verifyR02ReleaseCaptureReceipt(owner, product, cap); err != nil {
		return out, fmt.Errorf("R03: unauthenticated native R02 release envelope: %w", err)
	}
	if cap.Schema != releaseSliceCaptureSchema || cap.SliceID != nativeReleaseSliceR02 ||
		cap.CandidateVersion != version || cap.CandidateBinarySHA256 != out.BinarySHA ||
		cap.PlanBeforeSHA256 != learnSHA(planBefore) || cap.RunID == "" ||
		!n093SafeEventID(cap.RunID) ||
		cap.CaptureReceiptSHA256 == "" || cap.CaptureReceiptSHA256 != cap.ReplayReceiptSHA256 ||
		cap.ValidateExitCode != 2 || cap.ReplayExitCode != 0 ||
		cap.CauseCode != "validate-run-config-missing" ||
		cap.EventCount != 1 || cap.ReplayEventCount != 1 {
		return out, errors.New("R03: signed predecessor R02 receipt is incomplete, stale or belongs to another binary/plan")
	}
	if _, err := time.Parse(time.RFC3339Nano, cap.CreatedAtUTC); err != nil {
		return out, errors.New("R03: predecessor receipt timestamp invalid")
	}
	signedRaw, err := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot, "cli-failures/"+cap.RunID+".json", 32*1024)
	if err != nil {
		return out, err
	}
	if learnSHA(signedRaw) != cap.CaptureReceiptSHA256 {
		return out, errors.New("R03: original native CLI receipt changed since R02")
	}
	prior, err := verifyR02CLIReceipt(owner, cap.RunID, signedRaw)
	if err != nil {
		return out, err
	}
	out.Prior = prior
	out.R02SHA = learnSHA(raw)
	out.SourceEventID, err = r03OriginalEventID(owner, prior, cap.CaptureReceiptSHA256)
	if err != nil {
		return out, err
	}
	return out, nil
}

func r03OriginalEventID(owner selfLearningOwner, prior ownCLIFailureReceipt, capturedSHA string) (string, error) {
	count := 0
	var id string
	eventIDCount := make(map[string]int)
	expected := filepath.ToSlash(filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", prior.RunID+".json")) + "#sha256=" + capturedSHA
	err := n093ScanRuntimeEvidenceJSONL(owner.Settings.RuntimeRoot, "events.jsonl", func(raw []byte) error {
		var row struct {
			Schema     string `json:"schema"`
			ProductID  string `json:"product_id"`
			EventID    string `json:"event_id"`
			RunID      string `json:"run_id"`
			Kind       string `json:"kind"`
			Class      string `json:"class"`
			Source     string `json:"source"`
			Principal  string `json:"principal"`
			Session    string `json:"session"`
			Observed   string `json:"observed"`
			OutcomeRef string `json:"outcome_ref"`
		}
		if err := rejectDuplicateJSONKeys(raw); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row.Schema == "air.learning.event/v1" && row.EventID != "" {
			eventIDCount[row.EventID]++
		}
		if row.RunID != prior.RunID || row.Kind != "run_completed" {
			return nil
		}
		count++
		if row.Schema != "air.learning.event/v1" || row.ProductID != owner.Settings.ProductID ||
			!n093SafeEventID(row.EventID) || row.Class != "air-worker-cli-preflight" ||
			row.Source != "air-worker-cli" || row.Principal != "air-worker" ||
			row.Session != prior.RunID || row.Observed != prior.Observed || row.OutcomeRef != expected {
			return errors.New("R03: module-owned capture event mismatches HMAC native CLI receipt")
		}
		id = row.EventID
		return nil
	})
	if err != nil {
		return "", err
	}
	if count != 1 || eventIDCount[id] != 1 {
		return "", fmt.Errorf("R03: expected one unique signed run_completed event, got run=%d event_identity=%d", count, eventIDCount[id])
	}
	return id, nil
}

func releaseR03TrustedReviewedAction(owner selfLearningOwner, source releaseR03Source,
	planBefore []byte) (releaseR03ReviewReceipt, error) {
	var out releaseR03ReviewReceipt
	prior := source.Prior
	rule, eventID, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible || eventID != source.SourceEventID {
		return out, fmt.Errorf("R03: no unique authenticated reviewer/APPLIED module procedure: %v", err)
	}
	if rule.SHA256 == n118ApprovedPreflightDeferralSHA {
		return out, errors.New("R03: historical N118 SHA-only skill is not a new authenticated production reviewer")
	}
	body, err := readSharedContextSkill(owner.Selector.ProductRoot, rule.Target, 64*1024)
	if err != nil {
		return out, err
	}
	action, err := resolveNativeLearnedAction(body)
	if err != nil || action != supportedNativePreflightAction() ||
		!validLearningProcedureMarkdown(string(body)) {
		return out, errors.New("R03: a reviewed model response cannot grant an unregistered machine action")
	}
	reviewed, _, err := n093ReviewCanonicalRecord(owner, eventID)
	if err != nil {
		return out, err
	}
	get := func(key string) string { value, _ := reviewed[key].(string); return value }
	candidate, ok := reviewed["candidate"].(map[string]any)
	if !ok {
		return out, errors.New("R03: canonical reviewer has no candidate")
	}
	candidateRaw, err := json.Marshal(candidate)
	if err != nil {
		return out, err
	}
	digest := learnSHA(candidateRaw)
	proposal := "LP-" + eventID
	if get("schema") != "air.learning.review/v1" || get("event_id") != eventID ||
		get("run_id") != prior.RunID || get("outcome") != "candidate" || get("phase") != "complete" ||
		get("proposal_id") != proposal || !n093SafeEventID(get("review_id")) ||
		get("candidate_sha256") != digest {
		return out, errors.New("R03: canonical RV provenance or candidate digest is invalid")
	}
	response, responseRaw, err := n093ReadReviewerResponseProof(owner, prior, eventID, rule.Target, rule.SHA256)
	if err != nil {
		return out, fmt.Errorf("R03: production reviewer signed response is missing: %w", err)
	}
	if response.Model == "" || response.Effort == "" || response.ProposalID != proposal {
		return out, errors.New("R03: no signed machine-bound production reviewer response")
	}
	if err := n093VerifyCompletedNativeReview(owner, prior, eventID,
		get("review_id"), proposal, rule.Target, digest, rule.SHA256); err != nil {
		return out, fmt.Errorf("R03: canonical module reviewer completion cannot be authenticated: %w", err)
	}
	completionRaw, err := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot,
		"review-attestations/"+eventID+"-complete.json", 32*1024)
	if err != nil {
		return out, err
	}
	if len(responseRaw) == 0 || len(completionRaw) == 0 {
		return out, errors.New("R03: reviewer response/completion attestation absent")
	}
	out = releaseR03ReviewReceipt{
		Schema:                   releaseSliceR03ReceiptSchema,
		SliceID:                  nativeReleaseSliceR03,
		CandidateVersion:         version,
		CandidateBinarySHA256:    source.BinarySHA,
		PlanBeforeSHA256:         learnSHA(planBefore),
		R02ReceiptSHA256:         source.R02SHA,
		CaptureReceiptSHA256:     source.R02.CaptureReceiptSHA256,
		SourceRunID:              prior.RunID,
		SourceEventID:            eventID,
		ReviewID:                 get("review_id"),
		ProposalID:               proposal,
		CandidateSHA256:          digest,
		Target:                   rule.Target,
		TargetSHA256:             rule.SHA256,
		ReviewerResponseSHA256:   learnSHA(responseRaw),
		ReviewerCompletionSHA256: learnSHA(completionRaw),
		AppliedLedgerCount:       1,
		Outcome:                  "reviewed_safe_procedure_applied",
		CreatedAtUTC:             time.Now().UTC().Format(time.RFC3339Nano),
	}
	return out, nil
}

func releaseR03AuthenticatedSnapshot(root string, state releaseSlicesSummary,
	planRaw []byte, owner selfLearningOwner) (releaseR03Source, error) {
	var none releaseR03Source
	previous, err := releaseR03PlanSnapshot(root, state, planRaw)
	if err != nil {
		return none, err
	}
	if !sameLearningPath(owner.Selector.ProductRoot, root) ||
		owner.Settings.ProductID != "air-worker" {
		return none, errors.New("R03: active self-learning owner is not the release product")
	}
	return readR03R02Receipt(root, owner, previous)
}

func releaseR03PersistReviewedReceipt(root string, record releaseR03ReviewReceipt) error {
	if err := safeReleaseReceipt(root, releaseSliceR03ReceiptRef); err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(releaseSliceR03ReceiptRef))
	if _, err := os.Lstat(path); err == nil {
		return errors.New("R03: a published receipt already exists and cannot be overwritten")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	encoded, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := writeReleaseReceiptNoReplace(path, append(encoded, '\n')); err != nil {
		return err
	}
	got, err := n093ReadRuntimeEvidence(root, releaseSliceR03ReceiptRef, 32*1024)
	if err != nil || !bytes.Equal(got, append(encoded, '\n')) {
		return errors.New("R03: immutable receipt failed exact readback")
	}
	return nil
}

func releaseR03ReadReceipt(root string) (releaseR03ReviewReceipt, error) {
	var result releaseR03ReviewReceipt
	if err := safeReleaseReceipt(root, releaseSliceR03ReceiptRef); err != nil {
		return result, err
	}
	raw, err := n093ReadRuntimeEvidence(root, releaseSliceR03ReceiptRef, 32*1024)
	if err != nil {
		return result, err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return result, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return result, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return result, errors.New("R03: source-owned release receipt has trailing data")
	}
	if _, err := time.Parse(time.RFC3339Nano, result.CreatedAtUTC); err != nil {
		return result, errors.New("R03: receipt timestamp is invalid")
	}
	return result, nil
}

func releaseR03VerifyReceipt(root string, owner selfLearningOwner,
	state releaseSlicesSummary, planBefore []byte) (releaseR03ReviewReceipt, error) {
	var empty releaseR03ReviewReceipt
	source, err := releaseR03AuthenticatedSnapshot(root, state, planBefore, owner)
	if err != nil {
		return empty, err
	}
	expected, err := releaseR03TrustedReviewedAction(owner, source, planBefore)
	if err != nil {
		return empty, err
	}
	actual, err := releaseR03ReadReceipt(root)
	if err != nil {
		return empty, err
	}
	expected.CreatedAtUTC = actual.CreatedAtUTC
	if actual != expected {
		return empty, errors.New("R03: published reviewer receipt mismatches current signed module evidence, plan or binary")
	}
	return actual, nil
}

// This is a native release-slice operation. It never writes PLAN.md,
// plan/N-*, or LEARN by hand; the published shared module owns reviewer,
// proposal, APPLIED ledger, and reviewer completion records.
func runNativeReleaseSliceR03(productRoot string, state releaseSlicesSummary) int {
	root, err := filepath.Abs(productRoot)
	if err != nil {
		line("ОТКАЗ R03: invalid release product root")
		return 2
	}
	if err := releaseSliceLoopSupported(state, version); err != nil {
		line("ОТКАЗ R03: " + err.Error())
		return 2
	}
	// A verified R03 is a historical, authenticated reviewer/APPLIED
	// checkpoint, not an ongoing use grant. The published v0.1.0 module
	// performs its own serialized apply/rollback; release status belongs
	// exclusively to the AirWorker plan core and never credits use.
	if err := requireR03PublishedModule(); err != nil {
		line("ОТКАЗ R03: " + err.Error())
		return 2
	}
	lock, free := acquireLock(lockName("release-slice-run", root))
	if !free {
		line("ОТКАЗ R03: another release-slice job already owns this product")
		return 2
	}
	defer lock.release()
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled || !sameLearningPath(owner.Selector.ProductRoot, root) {
		line("ОТКАЗ R03: self-learning owner must be the source release product")
		return 2
	}
	planPath := filepath.Join(root, "plan", "release-slices.json")
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		line("ОТКАЗ R03: release plan is unreadable")
		return 2
	}
	current := inspectReleaseSlices(root)
	source, err := releaseR03AuthenticatedSnapshot(root, current, planRaw, owner)
	if err != nil {
		line("ОТКАЗ R03: signed R02 predecessor is not authentic: " + err.Error())
		return 2
	}
	if err := safeReleaseReceipt(root, releaseSliceR03ReceiptRef); err != nil {
		line("ОТКАЗ R03: unsafe release receipt location: " + err.Error())
		return 2
	}
	receiptPath := filepath.Join(root, filepath.FromSlash(releaseSliceR03ReceiptRef))
	if _, err := os.Lstat(receiptPath); err == nil {
		if err := transitionR03ToR04(root); err != nil {
			line("ОТКАЗ R03: prior reviewer receipt could not be authenticated: " + err.Error())
			return 2
		}
		return releaseR03TransitionReadback(root)
	} else if !errors.Is(err, os.ErrNotExist) {
		line("ОТКАЗ R03: existing release reviewer receipt cannot be inspected")
		return 2
	}
	// R02 explicitly deferred reviewer dispatch. If and only if the native
	// module has not started a reviewer job, dispatch its existing one-shot
	// review-batch. An existing prepared/running/error RV MUST NOT trigger
	// an additional model call or fabricated reviewer completion.
	reviewRef := "reviews/" + source.SourceEventID + ".json"
	_, reviewErr := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot, reviewRef, 256*1024)
	switch {
	case reviewErr == nil:
		// A prior reviewer exists. Fail closed if it is incomplete or stale.
	case errors.Is(reviewErr, os.ErrNotExist):
		if err := releaseR03RunNativeReviewerOnce(root, owner, source.R02.RunID, source.SourceEventID); err != nil {
			line("ОТКАЗ R03: authenticated one-attempt native reviewer did not complete: " + err.Error())
			return 2
		}
	default:
		line("ОТКАЗ R03: reviewer event-index path may be linked, modified or unreadable")
		return 2
	}
	receipt, err := releaseR03TrustedReviewedAction(owner, source, planRaw)
	if err != nil {
		line("ОТКАЗ R03: no cryptographically reviewed/applied safe procedure: " + err.Error())
		return 2
	}
	freshPlan, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(freshPlan, planRaw) {
		line("ОТКАЗ R03: release-slices changed while reviewer was running; no R03 receipt published")
		return 2
	}
	if err := releaseR03PersistReviewedReceipt(root, receipt); err != nil {
		line("ОТКАЗ R03: immutable release receipt publication failed: " + err.Error())
		return 2
	}
	if err := transitionR03ToR04(root); err != nil {
		line("ОТКАЗ R03: reviewer receipt is saved but native transition needs recovery: " + err.Error())
		return 2
	}
	return releaseR03TransitionReadback(root)
}

func transitionR03ToR04(productRoot string) error {
	// Native R03 status records a past, authenticated safe application.
	// Module rollback after this checkpoint invalidates any LATER procedure
	// use, not this historical event. R04 must freshly verify APPLIED/index
	// and actual use with a separate signed effect; its runner is fail-closed
	// until that next-step contract is explicitly implemented and judged.
	if err := requireR03PublishedModule(); err != nil {
		return err
	}
	root, err := filepath.Abs(productRoot)
	if err != nil {
		return err
	}
	lock, free := acquireLock(lockName("release-slices", root))
	if !free {
		return errors.New("R03: another core release transition owns the plan")
	}
	defer lock.release()
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !enabled || !sameLearningPath(owner.Selector.ProductRoot, root) {
		return errors.New("R03: current self-learning owner does not match release product")
	}
	planPath := filepath.Join(root, "plan", "release-slices.json")
	before, err := os.ReadFile(planPath)
	if err != nil {
		return err
	}
	state := inspectReleaseSlices(root)
	if err := releaseSliceLoopSupported(state, version); err != nil {
		return err
	}
	if state.NextStep != nativeReleaseSliceR03 {
		return errors.New("R03: noncontiguous or already completed release step")
	}
	if _, err := releaseR03VerifyReceipt(root, owner, state, before); err != nil {
		return fmt.Errorf("R03: published reviewer receipt lacks live signed evidence: %w", err)
	}
	after := expectedReleaseSliceR03ToR04(before)
	if after == nil {
		return errors.New("R03: current plan cannot make an unambiguous R04 transition")
	}
	// The core's existing product-wide release-slices mutex is held.
	current, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(current, before) {
		return errors.New("R03: concurrent release plan modification refused")
	}
	if err := writeFileAtomicDurable(planPath, after); err != nil {
		return err
	}
	readback, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(after, readback) {
		return errors.New("R03: atomic plan transition did not persist exact bytes")
	}
	fresh := inspectReleaseSlices(root)
	if len(fresh.Problems) != 0 || fresh.ActiveVersion != "0.11.9" ||
		fresh.NextStep != "R0.11.9-04" || !fresh.HasActiveStep ||
		fresh.ActiveStep.ID != "R0.11.9-04" || fresh.ActiveStep.Status != "open" {
		return errors.New("R03: native transition did not activate exactly R04")
	}
	return nil
}

func releaseR03TransitionReadback(root string) int {
	state := inspectReleaseSlices(root)
	if len(state.Problems) != 0 || state.NextStep != "R0.11.9-04" ||
		!state.HasActiveStep || state.ActiveStep.ID != "R0.11.9-04" ||
		state.ActiveStep.Status != "open" {
		line("ОТКАЗ R03: native release-plan readback did not open R04")
		return 2
	}
	line("R0.11.9-03 CLOSED (historical reviewer/APPLIED checkpoint); R0.11.9-04 OPEN (new live SHA and use/effect verification required).")
	return 0
}
