package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ruhorh66-rgb/air-modules/learning"
	"path/filepath"
	"strings"
)

// Native learned feedback proof is EXPLICIT and narrower than a generic
// LLM skill. It only proves a SHA-selected feedback action and durable
// canonical records; it never grants control or attests cognitive influence.
const nativeFeedbackUseAction = "Action: air-worker.feedback.add/v1"

type nativeFeedbackUsePlan struct {
	Owner  selfLearningOwner
	Rule   selfLearningLoadedRule
	Source string
	RunID  string
}

func prepareNativeFeedbackUse(spec, source, runID string, productSettings sharedLearningSettings) (*nativeFeedbackUsePlan, error) {
	if strings.TrimSpace(spec) == "" {
		return nil, nil
	}
	if strings.TrimSpace(runID) == "" {
		return nil, errors.New("learned feedback use requires stable -run-id")
	}
	sep := strings.LastIndex(spec, "@")
	if sep <= 0 || len(spec[sep+1:]) != 64 {
		return nil, errors.New("learned procedure must be exact target@SHA256")
	}
	rule := selfLearningLoadedRule{Target: spec[:sep], SHA256: strings.ToLower(spec[sep+1:])}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, errors.New("AirWorker self-learning owner is disabled")
	}
	if !currentSelfLearningRuleBytes(owner, rule) {
		return nil, errors.New("learned procedure stale or unsafe")
	}
	index, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "index", nil)
	if err != nil {
		return nil, err
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err = json.Unmarshal(index.Data, &catalog); err != nil {
		return nil, err
	}
	matched := false
	for _, skill := range catalog.Skills {
		if skill.Target == rule.Target && strings.EqualFold(skill.SHA256, rule.SHA256) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, errors.New("selected learned skill has no active ledger identity")
	}
	body, err := readSharedContextSkill(owner.Selector.ProductRoot, rule.Target, 64*1024)
	if err != nil {
		return nil, err
	}
	count := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(body), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == nativeFeedbackUseAction {
			count++
		}
	}
	if count != 1 || !validLearningProcedureMarkdown(string(body)) {
		return nil, errors.New("selected skill has no unique supported feedback-use contract")
	}
	if err := checkedAutoReviewedFeedbackRule(owner, rule, body); err != nil {
		return nil, fmt.Errorf("selected procedure is not uniquely bound to a trusted native auto-review: %w", err)
	}
	// Stable source/run selection forbids a replay claiming a second skill.
	loadID := "NW-LOADED-" + learnSHA([]byte(source + "\n" + runID))[:24]
	found, err := nativeSelfSkillLoaded(owner.Settings.RuntimeRoot, loadID, rule)
	if err != nil {
		return nil, err
	}
	if !found {
		// An already-completed native operation cannot choose a learned
		// procedure after the fact. The self-owner load must predate it.
		priorEvent := false
		err := scanLearnJSONL(filepath.Join(productSettings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
			var record struct {
				Schema string `json:"schema"`
				RunID  string `json:"run_id"`
			}
			if err := json.Unmarshal(b, &record); err != nil {
				return err
			}
			if record.Schema == "air.learning.event/v1" && record.RunID == runID {
				priorEvent = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if priorEvent {
			return nil, errors.New("cannot select a learned procedure after product feedback was already recorded")
		}
		result, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "load", map[string]string{
			"run_id": loadID, "target": rule.Target, "expected_sha256": rule.SHA256,
		})
		if err != nil {
			return nil, err
		}
		if result.Status != "loaded" || result.ID == "" {
			return nil, fmt.Errorf("learning module did not verify a fresh load: %s", result.Status)
		}
	}
	return &nativeFeedbackUsePlan{Owner: owner, Rule: rule, Source: source, RunID: runID}, nil
}

func nativeSelfSkillLoaded(root, runID string, rule selfLearningLoadedRule) (bool, error) {
	found := false
	err := scanLearnJSONL(filepath.Join(root, "events.jsonl"), func(b []byte) error {
		var row struct {
			Schema  string `json:"schema"`
			RunID   string `json:"run_id"`
			Target  string `json:"target"`
			SHA     string `json:"sha256"`
			Kind    string `json:"kind"`
			Outcome string `json:"outcome"`
		}
		if err := json.Unmarshal(b, &row); err != nil {
			return err
		}
		if row.RunID != runID {
			return nil
		}
		if found || row.Schema != "air.learning.usage/v1" || row.Kind != "skill_loaded" ||
			row.Target != rule.Target || !strings.EqualFold(row.SHA, rule.SHA256) ||
			row.Outcome != "loaded" {
			return errors.New("conflicting learned load history")
		}
		found = true
		return nil
	})
	return found, err
}

func nativeFeedbackCandidateEffect(root, runID, eventID string, expected feedbackRecord) (string, error) {
	id := "FB-" + learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24]
	path := filepath.Join(root, filepath.FromSlash(feedbackEvidenceRel(id)))
	b, err := readLearningBounded(path, 512*1024)
	if err != nil {
		return "", err
	}
	var rec feedbackRecord
	if err := json.Unmarshal(b, &rec); err != nil {
		return "", err
	}
	if rec.FeedbackID != id || rec.RunID != runID || rec.EventID != eventID || rec.Status != "candidate" || rec != expected {
		return "", errors.New("feedback receipt does not match the native event")
	}
	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		return "", err
	}
	plan := planFilePath(root, cfg)
	p, err := readLearningBounded(plan, 4*1024*1024)
	if err != nil {
		return "", err
	}
	present, err := canonicalFeedbackCandidateState(p, rec, feedbackEvidenceRel(id))
	if err != nil || !present {
		return "", fmt.Errorf("native feedback PLAN candidate has different status/type/source/evidence: %w", err)
	}
	return filepath.ToSlash(path) + "#sha256=" + learnSHA(b), nil
}

func recordNativeFeedbackProcedureUse(plan *nativeFeedbackUsePlan, productRoot, eventID string, expected feedbackRecord) error {
	if plan == nil {
		return nil
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !on || !sameLearningPath(owner.Selector.ProductRoot, plan.Owner.Selector.ProductRoot) ||
		!sameLearningPath(owner.Settings.RuntimeRoot, plan.Owner.Settings.RuntimeRoot) ||
		!currentSelfLearningRuleBytes(owner, plan.Rule) {
		return errors.New("self-owner or learned skill identity changed before use")
	}
	loadID := "NW-LOADED-" + learnSHA([]byte(plan.Source + "\n" + plan.RunID))[:24]
	confirmedLoad, err := nativeSelfSkillLoaded(owner.Settings.RuntimeRoot, loadID, plan.Rule)
	if err != nil {
		return fmt.Errorf("learned rule selection is not unique: %w", err)
	}
	if !confirmedLoad {
		return errors.New("self-owned source/run has no exact durable loaded rule")
	}
	ref, err := nativeFeedbackCandidateEffect(productRoot, plan.RunID, eventID, expected)
	if err != nil {
		return err
	}
	ruleID := plan.Rule.Target + "@" + plan.Rule.SHA256
	usageID := "NW-USED-" + learnSHA([]byte(plan.Source + "\n" + plan.RunID + "\n" + eventID + "\n" + ruleID + "\n" + ref))[:24]
	found := false
	err = scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var record map[string]any
		if err := json.Unmarshal(b, &record); err != nil {
			return err
		}
		if record["run_id"] != usageID {
			return nil
		}
		if found || record["schema"] != "air.learning.event/v1" ||
			record["kind"] != "procedure_used" || record["rule_id"] != ruleID ||
			record["principal"] != plan.Source || record["session"] != plan.RunID ||
			record["outcome"] != "pass" || record["outcome_ref"] != ref ||
			record["source"] != "native-feedback" {
			return errors.New("existing native feedback use conflicts with measured evidence")
		}
		found = true
		return nil
	})
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	response, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "observe", map[string]string{
		"run_id": usageID, "kind": "procedure_used", "class": "native-feedback-intake",
		"source": "native-feedback", "principal": plan.Source, "session": plan.RunID,
		"rule_id": ruleID, "outcome": "pass", "outcome_ref": ref,
		"observed": "Explicit SHA-pinned native feedback procedure selected, shared event and typed feedback receipt persisted, and exactly one canonical PLAN candidate verified.",
	})
	if err != nil {
		if errors.Is(err, learning.ErrConflict) && response.Status == "duplicate" {
			return errors.New("duplicate learning use requires a fresh identity readback")
		}
		return err
	}
	if response.Status != "recorded" || response.ID == "" {
		return fmt.Errorf("learning module did not record native use: %s", response.Status)
	}
	return nil
}

// The reviewer is not allowed to define a new proof mechanism. A supported
// safe clause about one exact native feedback action may receive the
// release-owned marker; all other prose is informational only.
func bindNativeFeedbackProof(class, content string) (string, error) {
	markerCount := 0
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == nativeFeedbackUseAction {
			markerCount++
		}
	}
	if markerCount != 0 {
		// A model or ordinary native propose may not self-issue the authority
		// to claim a machine-verifiable action. Only this adapter adds it.
		return "", errors.New("reviewer-supplied native action proof marker is forbidden")
	}
	if class != "feedback-error" {
		if markerCount != 0 {
			return "", errors.New("unsupported class claimed native feedback proof")
		}
		return content, nil
	}
	steps, err := nativeFeedbackProcedureVisibleLines(content)
	if err != nil {
		return "", err
	}
	if nativeFeedbackProcedureContradictory(steps) {
		// No machine proof if any visible Procedure line negates a
		// command or refers negatively to an earlier executable step.
		return content, nil
	}
	eligible := false
	for _, line := range steps {
		if len(line) < 4 || line[0] < '1' || line[0] > '6' || line[1] != '.' {
			continue
		}
		if nativePositiveFeedbackStep(strings.TrimSpace(line[2:])) {
			eligible = true
		}
	}
	if !eligible {
		return content, nil
	}
	content = strings.TrimSpace(content) + "\n\n## Machine-verifiable action\n" +
		nativeFeedbackUseAction + "\n"
	return content, nil
}

// A stable absolute selector directory, not caller CWD, owns this mutex.
// The source/run tuple is hashed with a separator, so two working dirs
// cannot choose different learned rules for the same observed operation.
func nativeFeedbackUseLockName(source, runID string) string {
	selector := airWorkerSelfLearningPath()
	id := learnSHA([]byte(source + "\x00" + runID))
	return lockName("native-feedback-use", filepath.Join(filepath.Dir(selector), "feedback-use-"+id[:32]))
}
