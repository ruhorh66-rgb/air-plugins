package main

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// R04's effect is historical, but MUST be real and later than its signed
// R03 reviewer checkpoint. A loaded procedure or model prose is never use.
const releaseR04ReceiptSchema = "air-worker.release-slice.verified-use/v1"
const releaseR04ReceiptRef = "docs/receipts/RELEASE_0_11_9_04.json"

type releaseR04Receipt struct {
	Schema             string `json:"schema"`
	SliceID            string `json:"slice_id"`
	ProductID          string `json:"product_id"`
	CandidateBinarySHA string `json:"candidate_binary_sha256"`
	PlanBeforeSHA      string `json:"plan_before_sha256"`
	R02ReceiptSHA      string `json:"r02_receipt_sha256"`
	R03ReceiptSHA      string `json:"r03_receipt_sha256"`
	OriginalRunID      string `json:"original_run_id"`
	OriginalEventID    string `json:"original_event_id"`
	Target             string `json:"target"`
	TargetSHA          string `json:"target_sha256"`
	LoadRunID          string `json:"load_run_id"`
	UsedRunID          string `json:"used_run_id"`
	ConfirmedEventID   string `json:"confirmed_event_id"`
	SignedIntentSHA    string `json:"signed_intent_sha256"`
	SignedResultSHA    string `json:"signed_result_sha256"`
	Outcome            string `json:"outcome"`
	Effect             string `json:"effect"`
	ValidationExecuted bool   `json:"native_validation_executed"`
	CreatedAtUTC       string `json:"created_at_utc"`
}

type r04Predecessor struct {
	Source releaseR03Source
	Review releaseR03ReviewReceipt
	R03SHA string
}

func releaseR04Predecessor(root string, owner selfLearningOwner,
	state releaseSlicesSummary, current []byte) (r04Predecessor, error) {
	var out r04Predecessor
	r03Bytes, err := releaseR04State(root, state, current)
	if err != nil {
		return out, err
	}
	beforeR02 := expectedReleaseSliceR03ToR02(r03Bytes)
	if beforeR02 == nil {
		return out, errors.New("R04: no authentic R02 plan preimage")
	}
	source, err := readR03R02Receipt(root, owner, beforeR02)
	if err != nil {
		return out, fmt.Errorf("R04: original R02 HMAC source invalid: %w", err)
	}
	expected, err := releaseR03TrustedReviewedAction(owner, source, r03Bytes)
	if err != nil {
		return out, fmt.Errorf("R04: current reviewer/APPLIED index unavailable: %w", err)
	}
	saved, err := releaseR03ReadReceipt(root)
	if err != nil {
		return out, err
	}
	expected.CreatedAtUTC = saved.CreatedAtUTC
	if saved != expected {
		return out, errors.New("R04: R03 receipt is not the exact reviewed source")
	}
	raw, err := n093ReadRuntimeEvidence(root, releaseSliceR03ReceiptRef, 32*1024)
	if err != nil {
		return out, err
	}
	out = r04Predecessor{Source: source, Review: saved, R03SHA: learnSHA(raw)}
	return out, nil
}

func releaseR04ObservedUse(root string, owner selfLearningOwner, previous r04Predecessor,
	planBefore []byte) (releaseR04Receipt, error) {
	var none releaseR04Receipt
	src := previous.Source
	rev := previous.Review
	if rev.Target == "" || rev.TargetSHA256 == n118ApprovedPreflightDeferralSHA ||
		rev.SourceRunID != src.Prior.RunID || rev.SourceEventID != src.SourceEventID {
		return none, errors.New("R04: reviewer has no new safe signed learned procedure")
	}
	// The signed R04 OPEN authorization must PRECEDE the actual signed N093
	// operation intent; an earlier use between R03 reviewer and R03 CLOSE
	// cannot be retroactively credited as a new R04 measured effect.
	start, err := releaseR04ReadAuthorization(root, owner, previous, planBefore)
	if err != nil {
		return none, fmt.Errorf("R04: missing release-open authorization: %w", err)
	}
	startAt, err := time.Parse(time.RFC3339Nano, start.CreatedUTC)
	if err != nil {
		return none, err
	}
	rule := selfLearningLoadedRule{Target: rev.Target, SHA256: rev.TargetSHA256}
	ruleID := rule.Target + "@" + rule.SHA256
	identity := learnSHA([]byte(src.Prior.RunID + "\x00" + src.SourceEventID + "\x00" + ruleID))
	loadID := "N118-LOADED-" + identity[:24]
	usedID := "N118-USED-" + identity[:24]
	rt := owner.Settings.RuntimeRoot
	intentPath := filepath.Join(rt, "native-preflight-uses", usedID+".json")
	intentRaw, err := n093ReadRuntimeEvidence(rt, "native-preflight-uses/"+usedID+".json", 32*1024)
	if err != nil {
		return none, err
	}
	resultRaw, err := n093ReadRuntimeEvidence(rt, "native-preflight-results/"+usedID+".json", 32*1024)
	if err != nil {
		return none, err
	}
	if err := rejectDuplicateJSONKeys(intentRaw); err != nil {
		return none, err
	}
	if err := rejectDuplicateJSONKeys(resultRaw); err != nil {
		return none, err
	}
	var intent, result nativePreflightUseReceipt
	if err := json.Unmarshal(intentRaw, &intent); err != nil {
		return none, err
	}
	if err := json.Unmarshal(resultRaw, &result); err != nil {
		return none, err
	}
	key, err := ownCLIPrivateKey(rt)
	if err != nil {
		return none, err
	}
	defer n093ZeroKey(key)
	mac1, e1 := nativeTypedPreflightReceiptMAC(key, intent)
	mac2, e2 := nativeTypedPreflightReceiptMAC(key, result)
	if e1 != nil || e2 != nil || intent.ReceiptMAC == "" || result.ReceiptMAC == "" ||
		!hmac.Equal([]byte(intent.ReceiptMAC), []byte(mac1)) ||
		!hmac.Equal([]byte(result.ReceiptMAC), []byte(mac2)) {
		return none, errors.New("R04: source-owned native use attestation HMAC invalid")
	}
	intentAt, err := time.Parse(time.RFC3339Nano, intent.CreatedUTC)
	if err != nil || !intentAt.After(startAt) {
		return none, errors.New("R04: real native operation predates signed R04 OPEN")
	}
	if intent.Schema != "air-worker.n093.typed-preflight-intent/v1" ||
		intent.Status != "pending_procedure_used" || intent.Outcome != "pending" ||
		intent.Effect != "NOT_PROVEN" || intent.RunID != usedID ||
		intent.OriginalRunID != src.Prior.RunID || intent.OriginalEventID != src.SourceEventID ||
		intent.RuleID != ruleID || intent.ProcedureSHA != rule.SHA256 ||
		intent.InvocationMAC != src.Prior.InvocationMAC || intent.ConfirmedEventID != "" ||
		intent.PriorSignedReceiptSHA != src.R02.CaptureReceiptSHA256 ||
		intent.Principal != "air-worker" || intent.Session != src.Prior.RunID ||
		intent.ProcessID <= 0 || intent.PlanFingerprint == "" || intent.NativeValidationExecuted {
		return none, errors.New("R04: native signed intent is not correlated to R02/R03")
	}
	a := supportedNativePreflightAction()
	if intent.ActionSchema != a.Schema || intent.ActionID != a.ID ||
		intent.ActionVersion != a.Version || intent.NativeCommand != a.NativeCommand ||
		intent.Precondition != "validated_run_config_missing" {
		return none, errors.New("R04: learned action is not the compiled safe preflight")
	}
	if result.Schema != "air-worker.n093.typed-preflight-use/v1" ||
		result.Status != a.Outcome || result.Outcome != a.Outcome || result.Effect != a.Effect ||
		result.NativeValidationExecuted || result.IntentSHA != learnSHA(intentRaw) ||
		result.ConfirmedEventID == "" || !n093SafeEventID(result.ConfirmedEventID) ||
		result.RunID != usedID || result.OriginalRunID != src.Prior.RunID ||
		result.RuleID != ruleID || result.ProcedureSHA != rule.SHA256 ||
		result.PriorSignedReceiptSHA != intent.PriorSignedReceiptSHA ||
		result.InvocationMAC != intent.InvocationMAC || result.PlanFingerprint != intent.PlanFingerprint ||
		result.ProcessID != intent.ProcessID || result.Session != intent.Session ||
		result.Nonce != intent.Nonce {
		return none, errors.New("R04: final result cannot be attributed to signed intent")
	}
	planFingerprint, err := n093NativePlanFingerprint(root)
	if err != nil || planFingerprint != result.PlanFingerprint {
		return none, errors.New("R04: native PLAN drift after measured operation")
	}
	created, err := time.Parse(time.RFC3339Nano, result.CreatedUTC)
	if err != nil {
		return none, err
	}
	reviewAt, err := time.Parse(time.RFC3339Nano, rev.CreatedAtUTC)
	if err != nil || !created.After(reviewAt) {
		return none, errors.New("R04: claimed procedure use did not follow R03 review")
	}
	loaded, err := nativeSelfSkillLoaded(rt, loadID, rule)
	if err != nil || !loaded {
		return none, errors.New("R04: module has no exact source-owned skill_loaded")
	}
	confirmed := 0
	err = n093ScanRuntimeEvidenceJSONL(rt, "events.jsonl", func(raw []byte) error {
		var ev struct {
			Schema     string `json:"schema"`
			EventID    string `json:"event_id"`
			RunID      string `json:"run_id"`
			Kind       string `json:"kind"`
			Class      string `json:"class"`
			Source     string `json:"source"`
			Principal  string `json:"principal"`
			Session    string `json:"session"`
			RuleID     string `json:"rule_id"`
			Outcome    string `json:"outcome"`
			OutcomeRef string `json:"outcome_ref"`
		}
		if err := rejectDuplicateJSONKeys(raw); err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			return err
		}
		if ev.RunID != usedID {
			return nil
		}
		confirmed++
		ref := filepath.ToSlash(intentPath) + "#sha256=" + learnSHA(intentRaw)
		if ev.Schema != "air.learning.event/v1" || ev.EventID != result.ConfirmedEventID ||
			ev.Kind != "procedure_used" || ev.Class != "native-preflight-retry" ||
			ev.Source != "native-preflight-validate" || ev.Principal != "air-worker" ||
			ev.Session != src.Prior.RunID || ev.RuleID != ruleID ||
			ev.Outcome != "pass" || ev.OutcomeRef != ref {
			return errors.New("R04: module journal does not have the exact causal use event")
		}
		return nil
	})
	if err != nil || confirmed != 1 {
		return none, fmt.Errorf("R04: duplicate/absent module use confirmation: count=%d cause=%v", confirmed, err)
	}
	resultReceipt := releaseR04Receipt{
		Schema: releaseR04ReceiptSchema, SliceID: nativeReleaseSliceR04,
		ProductID: owner.Settings.ProductID, CandidateBinarySHA: src.BinarySHA,
		PlanBeforeSHA: learnSHA(planBefore), R02ReceiptSHA: src.R02SHA, R03ReceiptSHA: previous.R03SHA,
		OriginalRunID: src.Prior.RunID, OriginalEventID: src.SourceEventID,
		Target: rule.Target, TargetSHA: rule.SHA256, LoadRunID: loadID, UsedRunID: usedID,
		ConfirmedEventID: result.ConfirmedEventID,
		SignedIntentSHA:  learnSHA(intentRaw), SignedResultSHA: learnSHA(resultRaw),
		Outcome: result.Outcome, Effect: result.Effect, ValidationExecuted: false,
		CreatedAtUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	return resultReceipt, nil
}

// No learned ability is considered current solely because R04 was CLOSED.
// The follow-on QA must separately recheck this active module index.
func releaseR04VerifyReceipt(root string, owner selfLearningOwner,
	state releaseSlicesSummary, plan []byte) (releaseR04Receipt, error) {
	var zero releaseR04Receipt
	p, err := releaseR04Predecessor(root, owner, state, plan)
	if err != nil {
		return zero, err
	}
	expected, err := releaseR04ObservedUse(root, owner, p, plan)
	if err != nil {
		return zero, err
	}
	saved, err := releaseR04ReadReceipt(root)
	if err != nil {
		return zero, err
	}
	expected.CreatedAtUTC = saved.CreatedAtUTC
	if expected != saved {
		return zero, errors.New("R04: stored effect receipt differs from module-owned use and HMAC")
	}
	return saved, nil
}
