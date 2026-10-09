package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// A shared feedback event and immutable feedback candidate are separate
// artifacts for the SAME event, not separate learning journals. The shared
// module owns events/reviews; the canonical feedback file and PLAN candidate
// preserve the product-level defect backlog. Resume is keyed by stable run_id.
func findSharedFeedbackEvent(runtimeRoot, runID, encoded, observed, principal string) (map[string]string, error) {
	var match map[string]string
	err := scanLearnJSONL(filepath.Join(runtimeRoot, "events.jsonl"), func(b []byte) error {
		var rec map[string]any
		if err := json.Unmarshal(b, &rec); err != nil {
			return err
		}
		if rec["schema"] != "air.learning.event/v1" || rec["run_id"] != runID {
			return nil
		}
		if rec["source"] != "feedback" || rec["feedback"] != encoded ||
			rec["observed"] != observed || rec["principal"] != principal ||
			rec["kind"] != "run_completed" {
			return errors.New("feedback run_id is already bound to another event or payload")
		}
		if match != nil {
			return errors.New("multiple learning events share one feedback run_id")
		}
		id, idOK := rec["event_id"].(string)
		at, atOK := rec["at"].(string)
		if !idOK || !atOK || id == "" || at == "" {
			return errors.New("learning event has no reliable event_id or timestamp")
		}
		if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
			return err
		}
		match = map[string]string{"event_id": id, "at": at}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if match == nil {
		return nil, errors.New("shared feedback event was not durably recorded")
	}
	return match, nil
}

// Idempotent recovery for a content-identical immutable document. Unrelated
// or conflicting files are never overwritten. The existing core write helper
// still owns the PLAN mutation; plan-node and feedback locks serialize it.
func writeSharedFeedbackImmutable(path string, body []byte) error {
	existing, err := readLearningBounded(path, 512*1024)
	switch {
	case err == nil:
		if !bytes.Equal(existing, body) {
			return errors.New("existing shared feedback receipt conflicts with this run")
		}
		return nil
	case !errors.Is(err, os.ErrNotExist):
		return err
	default:
		return writeImmutableFeedbackFile(path, body)
	}
}

func sharedFeedbackRecord(root, runID string, event map[string]string, kind, text, source, ref string, extra map[string]*string) (feedbackRecord, error) {
	label := func(key string) string {
		if p, ok := extra[key]; ok && p != nil && strings.TrimSpace(*p) != "" {
			return strings.TrimSpace(*p)
		}
		return "not supplied"
	}
	fieldType := label("type")
	if fieldType == "not supplied" {
		fieldType = "idea"
		if kind == "error" {
			fieldType = "defect"
		}
		if kind == "lesson" {
			fieldType = "friction"
		}
	}
	// Explicit type must remain distinguishable from the broader learning
	// kind: a friction report may legitimately use feedback-error.
	if (fieldType == "defect" && kind != "error") ||
		(fieldType == "idea" && kind != "idea") ||
		(fieldType == "friction" && kind != "error" && kind != "lesson") {
		return feedbackRecord{}, errors.New("feedback type contradicts learning kind")
	}
	severity := strings.ToUpper(label("severity"))
	if severity == "NOT SUPPLIED" {
		severity = "P3"
	}
	id := "FB-" + learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24]
	inputSHA := ""
	if value, exists := extra["input-sha256"]; exists && value != nil {
		inputSHA = strings.TrimSpace(*value)
	}
	evidence := strings.TrimSpace(ref)
	if evidence == "" {
		evidence = "shared learning event " + event["event_id"]
	}
	rec := feedbackRecord{
		FeedbackID: id, RunID: runID, EventID: event["event_id"],
		InputSHA256: inputSHA, CreatedAt: event["at"], Status: "candidate",
		Product:       filepath.Base(filepath.Clean(root)),
		SourceVersion: label("source-version"), Type: fieldType,
		Severity: severity, Observed: text,
		Expected: label("expected"), Evidence: evidence,
		Reproduction: label("reproduction"), Workaround: label("workaround"),
		ProposedOutcome: label("proposed-outcome"),
	}
	if err := validateFeedback(rec); err != nil {
		return rec, err
	}
	return rec, nil
}

func preserveCanonicalSharedFeedback(root string, s sharedLearningSettings, runID, encoded, kind, observed, principal, ref string, extra map[string]*string) (feedbackWriteResult, error) {
	event, err := findSharedFeedbackEvent(s.RuntimeRoot, runID, encoded, observed, principal)
	if err != nil {
		return feedbackWriteResult{}, err
	}
	rec, err := sharedFeedbackRecord(root, runID, event, kind, observed, principal, ref, extra)
	if err != nil {
		return feedbackWriteResult{}, err
	}
	feedbackLock, ok := acquireLock(lockName("feedback", root))
	if !ok {
		return feedbackWriteResult{}, errors.New("shared feedback write already active")
	}
	defer feedbackLock.release()
	planLock, ok := acquireLock(lockName("plan-nodes", root))
	if !ok {
		return feedbackWriteResult{}, errors.New("product PLAN mutation already active; retry stable feedback run_id")
	}
	defer planLock.release()
	var cfg runConfig
	configErr := readJSON(filepath.Join(root, "run-config.json"), &cfg)
	if configErr != nil && !errors.Is(configErr, os.ErrNotExist) {
		return feedbackWriteResult{}, fmt.Errorf("cannot resolve canonical feedback PLAN: %w", configErr)
	}
	plan := planFilePath(root, cfg)
	if _, statErr := os.Stat(plan); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) && errors.Is(configErr, os.ErrNotExist) {
			// A raw shared-learning consumer is not necessarily an AirWorker
			// product with a PLAN. Keep its historical event-only feedback
			// contract. Do NOT invent a product PLAN or claim a candidate.
			return feedbackWriteResult{FeedbackID: rec.FeedbackID}, nil
		}
		return feedbackWriteResult{}, fmt.Errorf("canonical feedback PLAN unavailable: %w", statErr)
	}
	return writeFeedbackWithIO(root, plan, rec, feedbackIO{
		writeEvidence: writeSharedFeedbackImmutable,
		writePlan:     writeFileAtomicDurable,
	})
}
