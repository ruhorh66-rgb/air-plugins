package main

import (
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
	priorRaw, err := readLearningBounded(priorPath, 32*1024)
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
	err = scanLearnJSONL(filepath.Join(rt, "events.jsonl"), func(b []byte) error {
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
	var review struct {
		Schema     string `json:"schema"`
		EventID    string `json:"event_id"`
		ReviewID   string `json:"review_id"`
		Phase      string `json:"phase"`
		Outcome    string `json:"outcome"`
		ProposalID string `json:"proposal_id"`
		Candidate  struct {
			Kind       string `json:"kind"`
			Target     string `json:"target"`
			Content    string `json:"content"`
			ProposalID string `json:"proposal_id"`
			ReviewID   string `json:"review_id"`
		} `json:"candidate"`
	}
	if err := readJSON(filepath.Join(rt, "reviews", source.EventID+".json"), &review); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return none, "", false, nil
		}
		return none, "", false, err
	}
	if review.Schema != "air.learning.review/v1" || review.EventID != source.EventID ||
		review.ReviewID == "" || review.Phase != "complete" || review.Outcome != "candidate" ||
		review.ProposalID == "" || review.Candidate.ProposalID != review.ProposalID ||
		review.Candidate.ReviewID != review.ReviewID || review.Candidate.Kind != "procedure" {
		return none, "", false, nil
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
	err = scanLearnJSONL(filepath.Join(rt, "ledger.jsonl"), func(b []byte) error {
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
	return rule, source.EventID, true, nil
}
