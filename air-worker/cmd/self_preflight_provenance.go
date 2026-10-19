package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A learned Markdown file is not proof of use. The original signed native
// error must have an exact reviewed candidate and an applied module ledger.
// This narrow release-owned action contract accepts ONLY the exact previously
// reviewed, machine-hash-pinned safe preflight template. An arbitrary new
// Markdown instruction (even one mentioning run-config.json) may be safely
// stored for human reading but CANNOT claim the native defer action. New
// supported action templates require code/negative tests and their own gate.
const n118ApprovedPreflightDeferralSHA = "f6e6ba39ba7d8d3193c7a45be367facd781de004aca5989f3559e851b050313e"

func findAppliedPreflightRule(owner selfLearningOwner, prior ownCLIFailureReceipt) (selfLearningLoadedRule, string, bool, error) {
	var none selfLearningLoadedRule
	if prior.Schema != ownCLIFailureSchema || prior.CauseCode != "validate-run-config-missing" ||
		prior.Action != "validate" || prior.Source != "air-worker-cli" ||
		prior.Principal != "air-worker" || prior.RunID == "" {
		return none, "", false, nil
	}
	rt := owner.Settings.RuntimeRoot
	priorPath := filepath.Join(rt, "cli-failures", prior.RunID+".json")
	priorRaw, err := n093ReadRuntimeEvidence(rt, "cli-failures/"+prior.RunID+".json", 32*1024)
	if err != nil {
		return none, "", false, err
	}
	priorRef := filepath.ToSlash(priorPath) + "#sha256=" + learnSHA(priorRaw)
	type originEvent struct {
		Schema     string `json:"schema"`
		EventID    string `json:"event_id"`
		RunID      string `json:"run_id"`
		Kind       string `json:"kind"`
		Class      string `json:"class"`
		Source     string `json:"source"`
		Principal  string `json:"principal"`
		Observed   string `json:"observed"`
		OutcomeRef string `json:"outcome_ref"`
	}
	var source originEvent
	found := 0
	err = n093ScanRuntimeEvidenceJSONL(rt, "events.jsonl", func(b []byte) error {
		var ev originEvent
		if err := json.Unmarshal(b, &ev); err != nil {
			return err
		}
		if ev.Schema == "air.learning.event/v1" && ev.RunID == prior.RunID &&
			ev.Kind == "run_completed" && ev.Class == "air-worker-cli-preflight" && ev.Source == "air-worker-cli" {
			if ev.Principal != "air-worker" || ev.EventID == "" || ev.Observed != prior.Observed || ev.OutcomeRef != priorRef {
				return errors.New("preflight event origin differs from signed native error")
			}
			source = ev
			found++
		}
		return nil
	})
	if err != nil {
		return none, "", false, err
	}
	if found == 0 {
		return none, "", false, nil
	}
	if found != 1 {
		return none, "", false, errors.New("ambiguous native preflight event history")
	}
	// The event-index is a pointer, NOT independently authenticated reviewer
	// output. Require exact canonical RV bytes, source run/event binding, and
	// the module-owned candidate digest before any learned action is eligible.
	indexRaw, err := n093ReadRuntimeEvidence(rt, "reviews/"+source.EventID+".json", 256*1024)
	if errors.Is(err, os.ErrNotExist) {
		return none, "", false, nil
	}
	if err != nil {
		return none, "", false, err
	}
	if err := rejectDuplicateJSONKeys(indexRaw); err != nil {
		return none, "", false, err
	}
	var review struct {
		Schema          string `json:"schema"`
		EventID         string `json:"event_id"`
		RunID           string `json:"run_id"`
		ReviewID        string `json:"review_id"`
		Phase           string `json:"phase"`
		Outcome         string `json:"outcome"`
		ProposalID      string `json:"proposal_id"`
		CandidateSHA256 string `json:"candidate_sha256"`
		Candidate       struct {
			Kind       string `json:"kind"`
			Target     string `json:"target"`
			Content    string `json:"content"`
			ProposalID string `json:"proposal_id"`
			ReviewID   string `json:"review_id"`
		} `json:"candidate"`
	}
	if err := json.Unmarshal(indexRaw, &review); err != nil {
		return none, "", false, err
	}
	if !strings.HasPrefix(review.ReviewID, "RV-") ||
		!n093SafeEventID(review.ReviewID) {
		return none, "", false, errors.New("N093: invalid canonical reviewer identity")
	}
	canonicalRaw, err := n093ReadRuntimeEvidence(rt, "reviews/"+review.ReviewID+".json", 256*1024)
	if err != nil || !bytes.Equal(indexRaw, canonicalRaw) {
		return none, "", false, errors.New("N093: review event index does not equal canonical RV record")
	}
	if review.Schema != "air.learning.review/v1" || review.EventID != source.EventID ||
		review.RunID != prior.RunID || review.Phase != "complete" || review.Outcome != "candidate" ||
		review.ProposalID != "LP-"+source.EventID ||
		review.Candidate.ProposalID != review.ProposalID ||
		review.Candidate.ReviewID != review.ReviewID || review.Candidate.Kind != "procedure" {
		return none, "", false, errors.New("N093: reviewer is not bound to the signed source run/event")
	}
	var reviewEnvelope map[string]json.RawMessage
	if err := json.Unmarshal(indexRaw, &reviewEnvelope); err != nil {
		return none, "", false, err
	}
	var candidate map[string]any
	if err := json.Unmarshal(reviewEnvelope["candidate"], &candidate); err != nil {
		return none, "", false, err
	}
	canonicalCandidate, err := json.Marshal(candidate)
	if err != nil || len(review.CandidateSHA256) != 64 ||
		!strings.EqualFold(learnSHA(canonicalCandidate), review.CandidateSHA256) {
		return none, "", false, errors.New("N093: reviewer candidate digest not bound to module-owned proposal")
	}
	target := review.Candidate.Target
	if !strings.HasPrefix(target, "skills/learned/air-worker-cli-preflight-") || !strings.HasSuffix(target, ".md") {
		return none, "", false, errors.New("reviewed target is not a managed preflight procedure")
	}
	body, err := readSharedContextSkill(owner.Selector.ProductRoot, target, 64*1024)
	if err != nil {
		return none, "", false, err
	}
	if review.Candidate.Content != string(body) {
		return none, "", false, errors.New("reviewed candidate differs from exact on-disk procedure")
	}
	if !validLearningProcedureMarkdown(string(body)) &&
		!strings.HasPrefix(string(body), nativeLearnedActionEnvelopePrefix) {
		return none, "", false, errors.New("reviewed candidate is neither safe Markdown nor a typed machine-only procedure")
	}
	rule := selfLearningLoadedRule{Target: target, SHA256: learnSHA(body)}
	// The model's Markdown is neither authorization nor proof of use. Bind
	// the reviewed/APPLIED target@SHA to a strictly compiled native action.
	// Unknown or conflicting prose never gains a new permission; typed
	// manifests only select a fixed, read-only, source-owned operation.
	action, err := resolveNativeLearnedAction(body)
	if err != nil || action != supportedNativePreflightAction() {
		return none, "", false, errors.New("UNSUPPORTED: reviewed procedure has no compiled safe typed action")
	}
	if !currentSelfLearningRuleBytes(owner, rule) {
		return none, "", false, errors.New("preflight rule stale/linked after review")
	}
	type ledgerLine struct {
		Schema     string `json:"schema"`
		ProductID  string `json:"product_id"`
		Kind       string `json:"kind"`
		Status     string `json:"status"`
		ProposalID string `json:"proposal_id"`
		Target     string `json:"target"`
		PostSHA256 string `json:"post_sha256"`
	}
	count := 0
	err = n093ScanRuntimeEvidenceJSONL(rt, "ledger.jsonl", func(b []byte) error {
		var x ledgerLine
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		if x.Schema == "air.learning.ledger/v1" && x.ProposalID == review.ProposalID {
			if x.ProductID != owner.Settings.ProductID || x.Kind != "procedure" ||
				x.Status != "applied" || x.Target != target || !strings.EqualFold(x.PostSHA256, rule.SHA256) {
				return errors.New("preflight candidate has no exact applied safe ledger")
			}
			count++
		}
		return nil
	})
	if err != nil {
		return none, "", false, err
	}
	if count == 0 {
		return none, "", false, nil
	}
	if count != 1 {
		return none, "", false, errors.New("more than one ledger transaction for preflight candidate")
	}
	index, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "index", nil)
	if err != nil || index.Status != "ok" {
		return none, "", false, fmt.Errorf("cannot prove current module index: %w", err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(index.Data, &catalog); err != nil {
		return none, "", false, err
	}
	unique := 0
	for _, s := range catalog.Skills {
		if s.Target == target && strings.EqualFold(s.SHA256, rule.SHA256) && s.SkillID == review.ProposalID {
			unique++
		}
	}
	if unique != 1 {
		return none, "", false, errors.New("reviewed preflight procedure not uniquely active in module index")
	}
	// A canonical RV and correct ledger alone can be forged by editing
	// runtime files. New learned N093 actions also require the immutable
	// reviewer-adapter model-response HMAC and the signed post-apply RV
	// completion that binds review/proposal IDs and the candidate digest.
	if err := n093VerifyCompletedNativeReview(owner, prior, source.EventID,
		review.ReviewID, review.ProposalID, target, review.CandidateSHA256, rule.SHA256); err != nil {
		return none, "", false, fmt.Errorf("N093: unauthenticated native reviewer completion: %w", err)
	}
	return rule, source.EventID, true, nil
}
