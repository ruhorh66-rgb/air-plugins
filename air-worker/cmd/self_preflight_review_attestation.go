package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The reviewer adapter is the only production boundary that receives the
// actual completed model response. The shared module subsequently chooses its
// own review ID and persists the candidate. The two signed records bind BOTH
// events: native model response first; durable canonical RV after module apply.
type n093ReviewerResponseProof struct {
	Schema             string `json:"schema"`
	ProductID          string `json:"product_id"`
	SourceRunID        string `json:"source_run_id"`
	SourceEventID      string `json:"source_event_id"`
	OriginalReceiptSHA string `json:"original_receipt_sha256"`
	ProposalID         string `json:"proposal_id"`
	Target             string `json:"target"`
	ContentSHA         string `json:"content_sha256"`
	Model              string `json:"reviewer_model"`
	Effort             string `json:"reviewer_effort"`
	ResponseSHA        string `json:"reviewer_response_sha256"`
	CreatedUTC         string `json:"created_utc"`
	MAC                string `json:"hmac_sha256"`
}
type n093ReviewerCompletionProof struct {
	Schema           string `json:"schema"`
	ProductID        string `json:"product_id"`
	SourceRunID      string `json:"source_run_id"`
	SourceEventID    string `json:"source_event_id"`
	ReviewID         string `json:"review_id"`
	ProposalID       string `json:"proposal_id"`
	Target           string `json:"target"`
	ContentSHA       string `json:"content_sha256"`
	CandidateSHA     string `json:"candidate_sha256"`
	ResponseProofSHA string `json:"response_proof_sha256"`
	CreatedUTC       string `json:"created_utc"`
	MAC              string `json:"hmac_sha256"`
}

const n093ReviewerResponseSchema = "air-worker.n093.reviewer-response/v1"
const n093ReviewerCompletionSchema = "air-worker.n093.reviewer-completion/v1"

func n093MAC(key, data []byte) string {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(data)
	return hex.EncodeToString(m.Sum(nil))
}
func n093ResponseMAC(key []byte, r n093ReviewerResponseProof) string {
	r.MAC = ""
	b, _ := json.Marshal(r)
	return n093MAC(key, b)
}
func n093CompletionMAC(key []byte, r n093ReviewerCompletionProof) string {
	r.MAC = ""
	b, _ := json.Marshal(r)
	return n093MAC(key, b)
}
func n093ZeroKey(key []byte) {
	for i := range key {
		key[i] = 0
	}
}

// Native reviewer receipts cannot overwrite a pre-existing reviewer result.
// This is a separate signer-owned journal, not the shared module LEARN ledger.
func n093WriteReviewProofOnce(rt, relative string, proof any) error {
	if !filepath.IsLocal(filepath.FromSlash(relative)) || strings.Contains(relative, "\\") {
		return errors.New("N093: unsafe native reviewer receipt path")
	}
	dir := filepath.Join(rt, filepath.Dir(filepath.FromSlash(relative)))
	if err := n093CheckOrdinaryDir(rt); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := n093CheckOrdinaryDir(dir); err != nil {
		return err
	}
	target := filepath.Join(rt, filepath.FromSlash(relative))
	if _, err := os.Lstat(target); err == nil {
		return errors.New("N093: reviewer attestation already exists and cannot be overwritten")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	raw, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".n093-attest-*.tmp")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err = f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Link(name, target); err != nil {
		return fmt.Errorf("N093: immutable native reviewer attestation conflict: %w", err)
	}
	// os.Link provides no-replace atomic publication, but until the temporary
	// name is removed the new receipt legitimately has TWO hardlinks.
	// Drop that transient link before verifying the single-link trust rule.
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("N093: cannot finalize single-link reviewer receipt: %w", err)
	}
	verified, err := n093ReadRuntimeEvidence(rt, relative, 32*1024)
	if err != nil || !bytes.Equal(verified, append(raw, '\n')) {
		return errors.New("N093: signed reviewer receipt exact readback failed")
	}
	return nil
}

func n093ReviewEventPacketEvidence(owner selfLearningOwner, packet []byte) (string, string, string, error) {
	var wrapper struct {
		Event struct {
			Schema     string `json:"schema"`
			ProductID  string `json:"product_id"`
			EventID    string `json:"event_id"`
			RunID      string `json:"run_id"`
			Kind       string `json:"kind"`
			Class      string `json:"class"`
			Source     string `json:"source"`
			Principal  string `json:"principal"`
			Observed   string `json:"observed"`
			OutcomeRef string `json:"outcome_ref"`
		} `json:"event"`
	}
	if err := json.Unmarshal(packet, &wrapper); err != nil {
		return "", "", "", err
	}
	event := wrapper.Event
	if !n093SafeEventID(event.EventID) || !n093SafeEventID(event.RunID) ||
		event.Schema != "air.learning.event/v1" || event.ProductID != owner.Settings.ProductID ||
		event.Kind != "run_completed" || event.Class != "air-worker-cli-preflight" ||
		event.Source != "air-worker-cli" || event.Principal != "air-worker" {
		return "", "", "", errors.New("N093: reviewer callback has no authenticated native failure origin")
	}
	rt := owner.Settings.RuntimeRoot
	priorBytes, err := n093ReadRuntimeEvidence(rt, "cli-failures/"+event.RunID+".json", 32*1024)
	if err != nil {
		return "", "", "", err
	}
	var prior ownCLIFailureReceipt
	if err := json.Unmarshal(priorBytes, &prior); err != nil {
		return "", "", "", err
	}
	key, err := ownCLIPrivateKey(rt)
	if err != nil {
		return "", "", "", err
	}
	defer n093ZeroKey(key)
	if prior.Schema != ownCLIFailureSchema || prior.RunID != event.RunID ||
		prior.Action != "validate" || prior.ExitCode != 2 ||
		prior.CauseCode != "validate-run-config-missing" ||
		prior.Source != "air-worker-cli" || prior.Principal != "air-worker" ||
		prior.Observed != event.Observed || prior.Version != version ||
		!hmac.Equal([]byte(prior.ReceiptMAC), []byte(ownCLIReceiptMAC(key, prior))) {
		return "", "", "", errors.New("N093: reviewer input does not match signed original native failure")
	}
	priorRef := filepath.ToSlash(filepath.Join(rt, "cli-failures", event.RunID+".json")) + "#sha256=" + learnSHA(priorBytes)
	if event.OutcomeRef != priorRef {
		return "", "", "", errors.New("N093: reviewer event outcome_ref mismatch")
	}
	count := 0
	err = n093ScanRuntimeEvidenceJSONL(rt, "events.jsonl", func(raw []byte) error {
		var found struct {
			Schema     string `json:"schema"`
			ProductID  string `json:"product_id"`
			EventID    string `json:"event_id"`
			RunID      string `json:"run_id"`
			Kind       string `json:"kind"`
			Class      string `json:"class"`
			Source     string `json:"source"`
			Principal  string `json:"principal"`
			Observed   string `json:"observed"`
			OutcomeRef string `json:"outcome_ref"`
		}
		if err := json.Unmarshal(raw, &found); err != nil {
			return err
		}
		if found.Kind == "run_completed" && (found.EventID == event.EventID || found.RunID == event.RunID) {
			count++
			if found.Schema != event.Schema || found.ProductID != event.ProductID ||
				found.EventID != event.EventID || found.RunID != event.RunID ||
				found.Kind != event.Kind || found.Class != event.Class ||
				found.Source != event.Source || found.Principal != event.Principal ||
				found.Observed != event.Observed || found.OutcomeRef != event.OutcomeRef {
				return errors.New("N093: reviewer source differs from module-owned original event")
			}
		}
		return nil
	})
	if err != nil || count != 1 {
		return "", "", "", errors.New("N093: no unique module-owned event for reviewer callback")
	}
	return event.RunID, event.EventID, learnSHA(priorBytes), nil
}

// Called only AFTER a terminal model response has been validated by the native
// production Codex adapter. No model text can choose a new machine action.
func n093SealProductionReviewerAnswer(product string, packet []byte, target, content, responseSHA, model, effort string) error {
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !on {
		return nil
	} // ordinary shared-product reviews cannot authorize N093
	if !sameLearningPath(owner.Selector.ProductRoot, product) || owner.Settings.ProductID != "air-worker" {
		return errors.New("N093: production reviewer owner does not match selected product")
	}
	action, err := resolveNativeLearnedAction([]byte(content))
	if err != nil || action != supportedNativePreflightAction() {
		return nil
	} // informational lesson is never an executable action
	if !validLearningProcedureMarkdown(content) || len(responseSHA) != 64 ||
		model == "" || len(model) > 128 || (effort != "low" && effort != "medium" && effort != "high") ||
		!strings.HasPrefix(target, "skills/learned/air-worker-cli-preflight-") ||
		!strings.HasSuffix(target, ".md") {
		return errors.New("N093: unsafe source-owned reviewer output attestation contract")
	}
	run, event, receiptSHA, err := n093ReviewEventPacketEvidence(owner, packet)
	if err != nil {
		return err
	}
	p := n093ReviewerResponseProof{
		Schema: n093ReviewerResponseSchema, ProductID: owner.Settings.ProductID,
		SourceRunID: run, SourceEventID: event, OriginalReceiptSHA: receiptSHA,
		ProposalID: "LP-" + event, Target: target, ContentSHA: learnSHA([]byte(content)),
		Model: model, Effort: effort, ResponseSHA: responseSHA,
		CreatedUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	p.MAC = n093ResponseMAC(key, p)
	n093ZeroKey(key)
	return n093WriteReviewProofOnce(owner.Settings.RuntimeRoot, "review-attestations/"+event+"-response.json", p)
}

func n093ReadReviewerResponseProof(owner selfLearningOwner, prior ownCLIFailureReceipt,
	eventID, target, contentSHA string) (n093ReviewerResponseProof, []byte, error) {
	var p n093ReviewerResponseProof
	if !n093SafeEventID(eventID) || !n093SafeEventID(prior.RunID) {
		return p, nil, errors.New("N093: invalid reviewer proof identity")
	}
	rt := owner.Settings.RuntimeRoot
	raw, err := n093ReadRuntimeEvidence(rt, "review-attestations/"+eventID+"-response.json", 32*1024)
	if err != nil {
		return p, nil, err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return p, nil, err
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, nil, err
	}
	priorRaw, err := n093ReadRuntimeEvidence(rt, "cli-failures/"+prior.RunID+".json", 32*1024)
	if err != nil {
		return p, nil, err
	}
	key, err := ownCLIPrivateKey(rt)
	if err != nil {
		return p, nil, err
	}
	signed := hmac.Equal([]byte(p.MAC), []byte(n093ResponseMAC(key, p)))
	priorSigned := hmac.Equal([]byte(prior.ReceiptMAC), []byte(ownCLIReceiptMAC(key, prior)))
	n093ZeroKey(key)
	if !signed || !priorSigned || p.Schema != n093ReviewerResponseSchema ||
		p.ProductID != owner.Settings.ProductID || p.SourceRunID != prior.RunID ||
		p.SourceEventID != eventID || p.OriginalReceiptSHA != learnSHA(priorRaw) ||
		p.ProposalID != "LP-"+eventID || p.Target != target || p.ContentSHA != contentSHA ||
		p.MAC == "" || p.Model == "" || p.Effort == "" || len(p.ResponseSHA) != 64 {
		return p, nil, errors.New("N093: production reviewer model-response HMAC/source identity invalid")
	}
	if _, err := time.Parse(time.RFC3339Nano, p.CreatedUTC); err != nil {
		return p, nil, err
	}
	return p, raw, nil
}

func n093ReviewCanonicalRecord(owner selfLearningOwner, eventID string) (map[string]any, []byte, error) {
	rt := owner.Settings.RuntimeRoot
	if !n093SafeEventID(eventID) {
		return nil, nil, errors.New("N093: invalid source event ID")
	}
	indexRaw, err := n093ReadRuntimeEvidence(rt, "reviews/"+eventID+".json", 256*1024)
	if err != nil {
		return nil, nil, err
	}
	if err := rejectDuplicateJSONKeys(indexRaw); err != nil {
		return nil, nil, err
	}
	var rv map[string]any
	if err := json.Unmarshal(indexRaw, &rv); err != nil {
		return nil, nil, err
	}
	reviewID, _ := rv["review_id"].(string)
	if !strings.HasPrefix(reviewID, "RV-") || !n093SafeEventID(reviewID) {
		return nil, nil, errors.New("N093: canonical RV identity invalid")
	}
	canonical, err := n093ReadRuntimeEvidence(rt, "reviews/"+reviewID+".json", 256*1024)
	if err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(indexRaw, canonical) {
		return nil, nil, errors.New("N093: module reviewer index differs from canonical RV")
	}
	return rv, indexRaw, nil
}

// The native deferred review-batch is the trusted completion boundary. A
// model-response HMAC was created ONLY in the adapter after a completed model
// call. The shared module now commits its independent reviewer/ledger data;
// the completion signer binds both identities and the exact module digest.
func n093SealCompletedNativeReview(product, runID, reviewID string) error {
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !on || !sameLearningPath(owner.Selector.ProductRoot, product) {
		return nil
	}
	rt := owner.Settings.RuntimeRoot
	var eventID string
	count := 0
	err = n093ScanRuntimeEvidenceJSONL(rt, "events.jsonl", func(line []byte) error {
		var ev struct {
			Schema    string `json:"schema"`
			EventID   string `json:"event_id"`
			RunID     string `json:"run_id"`
			Kind      string `json:"kind"`
			Class     string `json:"class"`
			Source    string `json:"source"`
			Principal string `json:"principal"`
		}
		if err := json.Unmarshal(line, &ev); err != nil {
			return err
		}
		if ev.RunID == runID && ev.Kind == "run_completed" && ev.Class == "air-worker-cli-preflight" {
			count++
			if ev.Schema != "air.learning.event/v1" || ev.Source != "air-worker-cli" ||
				ev.Principal != "air-worker" || !n093SafeEventID(ev.EventID) {
				return errors.New("N093: reviewer source event identity invalid")
			}
			eventID = ev.EventID
		}
		return nil
	})
	if err != nil {
		return err
	}
	if count == 0 {
		return nil
	} // unrelated learning reviews are not native preflight
	if count != 1 || reviewID == "" {
		return errors.New("N093: ambiguous trusted native review source")
	}
	rv, _, err := n093ReviewCanonicalRecord(owner, eventID)
	if err != nil {
		return err
	}
	get := func(key string) string { s, _ := rv[key].(string); return s }
	if get("schema") != "air.learning.review/v1" || get("run_id") != runID ||
		get("event_id") != eventID || get("review_id") != reviewID ||
		get("phase") != "complete" || get("outcome") != "candidate" ||
		get("proposal_id") != "LP-"+eventID {
		return errors.New("N093: module candidate is not a completed reviewer for this source")
	}
	candidate, ok := rv["candidate"].(map[string]any)
	if !ok {
		return errors.New("N093: reviewer candidate missing")
	}
	str := func(k string) string { s, _ := candidate[k].(string); return s }
	target, content := str("target"), str("content")
	if str("kind") != "procedure" || str("proposal_id") != get("proposal_id") ||
		str("review_id") != reviewID {
		return errors.New("N093: module reviewer candidate IDs mismatch")
	}
	canonicalCandidate, err := json.Marshal(candidate)
	if err != nil {
		return err
	}
	digest := learnSHA(canonicalCandidate)
	if digest != get("candidate_sha256") {
		return errors.New("N093: module candidate SHA differs")
	}
	action, err := resolveNativeLearnedAction([]byte(content))
	if err != nil || action != supportedNativePreflightAction() {
		return nil
	} // informational lesson is not an N093 action
	var prior ownCLIFailureReceipt
	priorBytes, err := n093ReadRuntimeEvidence(rt, "cli-failures/"+runID+".json", 32*1024)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(priorBytes, &prior); err != nil {
		return err
	}
	response, responseRaw, err := n093ReadReviewerResponseProof(owner, prior, eventID, target, learnSHA([]byte(content)))
	if err != nil {
		return fmt.Errorf("N093: no authenticated model reviewer response: %w", err)
	}
	// A prior APPLIED transaction and exact managed bytes must accompany the
	// candidate before the verifier trusts any signed review completion.
	ledgerMatches := 0
	err = n093ScanRuntimeEvidenceJSONL(rt, "ledger.jsonl", func(line []byte) error {
		var item struct {
			Schema     string `json:"schema"`
			ProductID  string `json:"product_id"`
			ProposalID string `json:"proposal_id"`
			Kind       string `json:"kind"`
			Status     string `json:"status"`
			Target     string `json:"target"`
			PostSHA    string `json:"post_sha256"`
		}
		if err := json.Unmarshal(line, &item); err != nil {
			return err
		}
		if item.ProposalID == get("proposal_id") {
			ledgerMatches++
			if item.Schema != "air.learning.ledger/v1" || item.ProductID != owner.Settings.ProductID ||
				item.Kind != "procedure" || item.Status != "applied" ||
				item.Target != target || item.PostSHA != response.ContentSHA {
				return errors.New("N093: candidate has no correct module APPLIED ledger")
			}
		}
		return nil
	})
	if err != nil || ledgerMatches != 1 {
		return errors.New("N093: no unique matching APPLIED transaction")
	}
	body, err := readSharedContextSkill(product, target, 64*1024)
	if err != nil || learnSHA(body) != response.ContentSHA {
		return errors.New("N093: current managed target SHA differs from completed review")
	}
	result := n093ReviewerCompletionProof{
		Schema: n093ReviewerCompletionSchema, ProductID: owner.Settings.ProductID,
		SourceRunID: runID, SourceEventID: eventID, ReviewID: reviewID,
		ProposalID: get("proposal_id"), Target: target, ContentSHA: response.ContentSHA,
		CandidateSHA: digest, ResponseProofSHA: learnSHA(responseRaw),
		CreatedUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	key, err := ownCLIPrivateKey(rt)
	if err != nil {
		return err
	}
	result.MAC = n093CompletionMAC(key, result)
	n093ZeroKey(key)
	return n093WriteReviewProofOnce(rt, "review-attestations/"+eventID+"-complete.json", result)
}

func n093VerifyCompletedNativeReview(owner selfLearningOwner, prior ownCLIFailureReceipt,
	eventID, reviewID, proposalID, target, candidateSHA, contentSHA string) error {
	if contentSHA == n118ApprovedPreflightDeferralSHA {
		// Historical N118 is an already compiled and SHA-audited legacy
		// action. New N093-generated instructions MUST have authenticated
		// production reviewer response and module completion receipts.
		return nil
	}
	rt := owner.Settings.RuntimeRoot
	_, responseRaw, err := n093ReadReviewerResponseProof(owner, prior, eventID, target, contentSHA)
	if err != nil {
		return err
	}
	raw, err := n093ReadRuntimeEvidence(rt, "review-attestations/"+eventID+"-complete.json", 32*1024)
	if err != nil {
		return err
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	var item n093ReviewerCompletionProof
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	key, err := ownCLIPrivateKey(rt)
	if err != nil {
		return err
	}
	valid := hmac.Equal([]byte(item.MAC), []byte(n093CompletionMAC(key, item)))
	n093ZeroKey(key)
	if !valid || item.MAC == "" || item.Schema != n093ReviewerCompletionSchema ||
		item.ProductID != owner.Settings.ProductID || item.SourceRunID != prior.RunID ||
		item.SourceEventID != eventID || item.ReviewID != reviewID || item.ProposalID != proposalID ||
		item.Target != target || item.ContentSHA != contentSHA || item.CandidateSHA != candidateSHA ||
		item.ResponseProofSHA != learnSHA(responseRaw) {
		return errors.New("N093: completed native reviewer attestation is missing, forged or stale")
	}
	if _, err := time.Parse(time.RFC3339Nano, item.CreatedUTC); err != nil {
		return err
	}
	return nil
}
