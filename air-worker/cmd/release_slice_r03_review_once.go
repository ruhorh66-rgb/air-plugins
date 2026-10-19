package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// A missing event-index is not proof of a never-started reviewer. The native
// module stores canonical RV attempts by ID; a crash can leave an orphan RV
// that the module would otherwise auto-retry with its default 2-attempt cap.
func releaseR03HasExistingReviewerAttempt(owner selfLearningOwner, runID, eventID string) (bool, error) {
	if !n093SafeEventID(runID) || !n093SafeEventID(eventID) {
		return false, errors.New("R03: invalid signed reviewer source identity")
	}
	rt := owner.Settings.RuntimeRoot
	_, err := n093ReadRuntimeEvidence(rt, "reviews/"+eventID+".json", 256*1024)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("R03: reviewer index is ambiguous or unreadable: %w", err)
	}
	dir := filepath.Join(rt, "reviews")
	if err := n093CheckOrdinaryDir(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return false, err
	}
	if len(files) > 4096 {
		return false, errors.New("R03: reviewer directory has an unbounded attempt history")
	}
	for _, entry := range files {
		name := entry.Name()
		if !strings.HasPrefix(name, "RV-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !n093SafeEventID(id) || !strings.HasPrefix(id, "RV-") {
			return false, errors.New("R03: malformed canonical reviewer identity")
		}
		raw, err := n093ReadRuntimeEvidence(rt, "reviews/"+name, 256*1024)
		if err != nil {
			return false, fmt.Errorf("R03: reviewer attempt may not be linked outside runtime: %w", err)
		}
		if err := rejectDuplicateJSONKeys(raw); err != nil {
			return false, err
		}
		var row struct {
			Schema   string `json:"schema"`
			ReviewID string `json:"review_id"`
			EventID  string `json:"event_id"`
			RunID    string `json:"run_id"`
			Phase    string `json:"phase"`
			Outcome  string `json:"outcome"`
		}
		if err := json.Unmarshal(raw, &row); err != nil {
			return false, err
		}
		if row.Schema != "air.learning.review/v1" || row.ReviewID != id {
			return false, errors.New("R03: conflicting canonical reviewer file")
		}
		if row.EventID == eventID || row.RunID == runID {
			// Also reject orphaned partial/error/completed review records.
			return true, nil
		}
	}
	return false, nil
}

// The module's own per-run review lease serializes the final check and
// reviewer call. ReviewMaxAttempts=1 additionally prevents recovery retries
// if another reviewer raced our earlier read-only directory inspection.
func releaseR03RunNativeReviewerOnce(root string, owner selfLearningOwner, runID, eventID string) error {
	existing, err := releaseR03HasExistingReviewerAttempt(owner, runID, eventID)
	if err != nil {
		return err
	}
	if existing {
		return errors.New("R03: native reviewer had a prior attempt; refusing to repeat the model call")
	}
	c := sharedLearningConfig(root, owner.Settings)
	if c.Reviewer == nil {
		return errors.New("R03: production reviewer adapter is not configured")
	}
	c.ReviewMaxAttempts = 1
	reqData, err := json.Marshal(map[string]string{"run_id": runID})
	if err != nil {
		return err
	}
	result, err := learning.Execute(context.Background(), c, learning.Request{
		Operation: "review", Version: learning.Version, Data: reqData,
	})
	if err != nil {
		return fmt.Errorf("R03: native shared module reviewer failed: %w", err)
	}
	if result.Status != "candidate" || !n093SafeEventID(result.ID) {
		return fmt.Errorf("R03: native reviewer returned noncandidate terminal status %q", result.Status)
	}
	// A producer-side signed model response must already exist.
	// The only source of canonical reviewed SHA is the published module.
	if err := n093SealCompletedNativeReview(root, runID, result.ID); err != nil {
		return fmt.Errorf("R03: reviewer completion lacks trusted model-response proof: %w", err)
	}
	return nil
}
