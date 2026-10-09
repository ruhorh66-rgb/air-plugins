package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// A managed Markdown target is not proof of a reviewed lesson. Only one
// actual reviewer-generated feedback-error event followed by an applied
// ledger transaction may bind this narrow native feedback use contract.
func checkedAutoReviewedFeedbackRule(owner selfLearningOwner, rule selfLearningLoadedRule, body []byte) error {
	const suffix = "\n\n## Machine-verifiable action\nAction: air-worker.feedback.add/v1\n"
	raw := strings.ReplaceAll(string(body), "\r\n", "\n")
	if !strings.HasSuffix(raw, suffix) || strings.Count(raw, nativeFeedbackUseAction) != 1 {
		return errors.New("native feedback action was not added in the canonical adapter position")
	}
	withoutMarker := strings.TrimSuffix(raw, suffix) + "\n"
	rebound, err := bindNativeFeedbackProof("feedback-error", withoutMarker)
	if err != nil || rebound != raw {
		return errors.New("reviewed feedback guidance does not satisfy release-owned positive action rules")
	}

	type ledgerLine struct {
		Schema     string `json:"schema"`
		Kind       string `json:"kind"`
		Status     string `json:"status"`
		Target     string `json:"target"`
		PostSHA256 string `json:"post_sha256"`
		ProposalID string `json:"proposal_id"`
	}
	var matched ledgerLine
	ledgerMatches := 0
	err = scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "ledger.jsonl"), func(b []byte) error {
		var v ledgerLine
		if err := json.Unmarshal(b, &v); err != nil {
			return err
		}
		if v.Schema == "air.learning.ledger/v1" && v.Kind == "procedure" &&
			v.Status == "applied" && v.Target == rule.Target &&
			strings.EqualFold(v.PostSHA256, rule.SHA256) {
			ledgerMatches++
			matched = v
		}
		return nil
	})
	if err != nil {
		return err
	}
	if ledgerMatches != 1 || matched.ProposalID == "" {
		return errors.New("no unique applied lesson ledger transaction for native feedback use")
	}

	type eventLine struct {
		Schema    string `json:"schema"`
		EventID   string `json:"event_id"`
		Kind      string `json:"kind"`
		Class     string `json:"class"`
		Source    string `json:"source"`
		RunID     string `json:"run_id"`
		Principal string `json:"principal"`
	}
	var source eventLine
	eventMatches := 0
	// A review candidate is bound to the originating event's real product
	// type, not to arbitrary classes or later calls to native propose.
	err = scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var ev eventLine
		if err := json.Unmarshal(b, &ev); err != nil {
			return err
		}
		if ev.Schema == "air.learning.event/v1" && ev.Kind == "run_completed" &&
			ev.Class == "feedback-error" && ev.Source == "feedback" &&
			ev.EventID != "" && ev.Principal != "" && ev.RunID != "" {
			path := filepath.Join(owner.Settings.RuntimeRoot, "reviews", ev.EventID+".json")
			var review struct {
				Schema     string `json:"schema"`
				EventID    string `json:"event_id"`
				ReviewID   string `json:"review_id"`
				Outcome    string `json:"outcome"`
				Phase      string `json:"phase"`
				ProposalID string `json:"proposal_id"`
				Candidate  struct {
					Kind       string `json:"kind"`
					Target     string `json:"target"`
					Content    string `json:"content"`
					ProposalID string `json:"proposal_id"`
					ReviewID   string `json:"review_id"`
				} `json:"candidate"`
			}
			if err := readJSON(path, &review); err != nil {
				return nil
			}
			if review.Schema == "air.learning.review/v1" &&
				review.EventID == ev.EventID && review.ReviewID != "" &&
				review.Outcome == "candidate" && review.Phase == "complete" &&
				review.ProposalID == matched.ProposalID &&
				review.Candidate.ProposalID == matched.ProposalID &&
				review.Candidate.ReviewID == review.ReviewID &&
				review.Candidate.Kind == "procedure" &&
				review.Candidate.Target == rule.Target &&
				review.Candidate.Content == raw {
				eventMatches++
				source = ev
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if eventMatches != 1 || source.EventID == "" {
		return fmt.Errorf("native feedback contract has %d matching original auto-review receipts; expected exactly one", eventMatches)
	}
	return nil
}
