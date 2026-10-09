package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n096Fixture(t *testing.T) (string, sharedLearningSettings, []string, string) {
	t.Helper()
	product, settings := sharedProductFixture(t, false)
	if err := os.WriteFile(filepath.Join(product, "PLAN.md"), []byte("# Product fixture\n\n## Execution tasks\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	runID := "AW-N096-durable-feedback-run"
	args := []string{"add", "-product", product, "-kind", "error", "-text", "installed self-owner command is unavailable",
		"-source", "gpt-window", "-ref", "receipt/source.json", "-run-id", runID,
		"-severity", "P0", "-type", "defect", "-source-version", "0.11.8",
		"-expected", "self owner is active", "-reproduction", "air-worker learn self status -json",
		"-workaround", "explicit product root learning", "-proposed-outcome", "persistent owner"}
	return product, settings, args, runID
}

func TestN096SharedFeedbackPreservesCanonicalCandidateAndResumes(t *testing.T) {
	product, settings, args, runID := n096Fixture(t)
	run := func() (int, string) {
		return captureLoopOutput(t, func() int {
			return cmdFeedback(args)
		})
	}
	code, output := run()
	if code != 0 {
		t.Fatalf("native feedback failed: code=%d data=%s", code, output)
	}
	var result struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(output), &result); err != nil || result.Status != "recorded" {
		t.Fatalf("wrong feedback response: %#v err=%v", result, err)
	}
	path := filepath.Join(product, filepath.FromSlash(feedbackEvidenceRel("FB-"+learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24])))
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted feedbackRecord
	if err := json.Unmarshal(original, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.RunID != runID || persisted.EventID == "" || persisted.Severity != "P0" ||
		persisted.Type != "defect" || persisted.SourceVersion != "0.11.8" {
		t.Fatalf("canonical typed feedback wrong: %+v", persisted)
	}
	plan, err := os.ReadFile(filepath.Join(product, "PLAN.md"))
	if err != nil || !strings.Contains(string(plan), persisted.FeedbackID) {
		t.Fatalf("canonical feedback candidate not linked from PLAN: %v", err)
	}
	countEvents := func() int {
		count := 0
		err := scanLearnJSONL(filepath.Join(settings.RuntimeRoot, "events.jsonl"), func(row []byte) error {
			var item map[string]any
			if err := json.Unmarshal(row, &item); err != nil {
				return err
			}
			if item["schema"] == "air.learning.event/v1" && item["run_id"] == runID {
				count++
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	if countEvents() != 1 {
		t.Fatalf("event missing or duplicated")
	}
	code, output = run()
	if code != 0 || countEvents() != 1 {
		t.Fatalf("idempotent retry failed: code=%d out=%s", code, output)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(original) {
		t.Fatalf("immutable feedback rewritten: %v", err)
	}
	planAfter, _ := os.ReadFile(filepath.Join(product, "PLAN.md"))
	if strings.Count(string(planAfter), "- feedback `"+persisted.FeedbackID+"`") != 1 {
		t.Fatalf("duplicate candidate in PLAN: %s", planAfter)
	}
}

func TestN096SharedFeedbackConflictingImmutableEvidenceFailsClosed(t *testing.T) {
	product, settings, args, runID := n096Fixture(t)
	code, _ := captureLoopOutput(t, func() int { return cmdFeedback(args) })
	if code != 0 {
		t.Fatal("initial feedback failed")
	}
	id := "FB-" + learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24]
	path := filepath.Join(product, filepath.FromSlash(feedbackEvidenceRel(id)))
	if err := os.WriteFile(path, []byte("conflicting foreign bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, output := captureLoopOutput(t, func() int { return cmdFeedback(args) })
	if code == 0 || !strings.Contains(output, "partial") {
		t.Fatalf("conflicting evidence not rejected: rc=%d data=%s", code, output)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "conflicting foreign bytes\n" {
		t.Fatal("foreign evidence overwritten")
	}
	n := 0
	_ = scanLearnJSONL(filepath.Join(settings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var x map[string]any
		if json.Unmarshal(b, &x) == nil && x["schema"] == "air.learning.event/v1" && x["run_id"] == runID {
			n++
		}
		return nil
	})
	if n != 1 {
		t.Fatalf("replay made %d events", n)
	}
}

func TestN096FeedbackIntakeSurvivesReviewerFailure(t *testing.T) {
	product, s, args, runID := n096Fixture(t)
	t.Setenv("AW_LEARNING_ADAPTER_HELPER", "1")
	s.Reviewer = learningAdapterFixture(t, "judge")
	if err := writeSharedLearningSettings(product, s); err != nil {
		t.Fatal(err)
	}
	code, response := captureLoopOutput(t, func() int { return cmdFeedback(args) })
	// A judge-shaped answer cannot count as an applied learning proposal.
	var observed struct {
		Data struct {
			ReviewOutcome string `json:"review_outcome"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(response), &observed); err != nil {
		t.Fatalf("feedback response is not machine JSON: %v", err)
	}
	if code == 0 && observed.Data.ReviewOutcome == "candidate" {
		t.Fatalf("invalid reviewer output accepted as candidate: %s", response)
	}
	id := "FB-" + learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24]
	path := filepath.Join(product, filepath.FromSlash(feedbackEvidenceRel(id)))
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("feedback lost canonical candidate after reviewer failure: %v response=%s", err, response)
	}
	plan, err := os.ReadFile(filepath.Join(product, "PLAN.md"))
	if err != nil || !strings.Contains(string(plan), id) {
		t.Fatalf("feedback candidate not retained: %v", err)
	}
}

func TestN096RawSharedLearningConsumerWithoutPlanKeepsLegacyEventContract(t *testing.T) {
	product, settings := sharedProductFixture(t, false)
	if _, err := os.Stat(filepath.Join(product, "PLAN.md")); !os.IsNotExist(err) {
		t.Fatal("raw learning fixture unexpectedly has PLAN")
	}
	runID := "N096-raw-shared-no-plan"
	code, output := captureLoopOutput(t, func() int {
		return cmdFeedback([]string{
			"add", "-product", product, "-kind", "error",
			"-source", "worker", "-text", "bounded learning without a project PLAN",
			"-run-id", runID,
		})
	})
	if code != 0 {
		t.Fatalf("raw shared-learning consumer lost compatibility: code=%d %s", code, output)
	}
	if _, err := os.Stat(filepath.Join(product, "PLAN.md")); !os.IsNotExist(err) {
		t.Fatalf("invented a bogus product PLAN: %v", err)
	}
	matching := 0
	err := scanLearnJSONL(filepath.Join(settings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var row map[string]any
		if err := json.Unmarshal(b, &row); err != nil {
			return err
		}
		if row["schema"] == "air.learning.event/v1" && row["run_id"] == runID {
			matching++
		}
		return nil
	})
	if err != nil || matching != 1 {
		t.Fatalf("raw shared event was not recorded once: n=%d err=%v", matching, err)
	}
}
