package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestA2ReviewerStageIsRecordedWithoutDetail(t *testing.T) {
	cases := []struct {
		stderr, want string
	}{
		{"learning reviewer: stage=settings: open learning-module.json: syntheticSECRET\n", "reviewer_settings_failed; detail withheld"},
		{"learning reviewer: stage=product: not a product root syntheticSECRET\n", "reviewer_product_failed; detail withheld"},
		{"learning reviewer: stage=invoke: stream disconnected syntheticSECRET\n", "reviewer_invoke_failed; detail withheld"},
		{"learning reviewer: stage=answer: proof mismatch syntheticSECRET\n", "reviewer_answer_failed; detail withheld"},
		// A known fixed diagnostic keeps its own reason when it carries a stage.
		{"learning reviewer: stage=catalog: applied catalogue exceeds reviewer procedure limit\n", "reviewer_context_limit"},
		{"learning reviewer: stage=invoke: Ponytail: skill missing\n", "reviewer_skill_unavailable"},
		// An unknown stage name is not trusted and falls back to the old reason.
		{"learning reviewer: stage=syntheticSECRET: boom\n", "reviewer_failed; detail withheld"},
		{"learning reviewer: plain unknown failure\n", "reviewer_failed; detail withheld"},
	}
	for _, tc := range cases {
		got := learningFailureReason([]byte(tc.stderr))
		if got != tc.want || strings.Contains(got, "syntheticSECRET") {
			t.Fatalf("%q: got %q, want %q", tc.stderr, got, tc.want)
		}
	}
}

func TestA2ReviewerStageFailurePrintsFixedStageOnly(t *testing.T) {
	out, _ := captureStderr(t, func() int {
		reviewerStageFailure("settings", errors.New("cannot read config"))
		reviewerStageFailure("not-a-stage", errors.New("plain"))
		return 0
	})
	lines := strings.Split(strings.TrimSpace(strings.ReplaceAll(out, "\r\n", "\n")), "\n")
	if len(lines) != 2 || lines[0] != "learning reviewer: stage=settings: cannot read config" || lines[1] != "learning reviewer: plain" {
		t.Fatalf("unexpected stage output: %q", out)
	}
}

func TestA2DeterministicReviewerFailureClassification(t *testing.T) {
	cases := []struct {
		reviewError string
		want        bool
	}{
		{"learning adapter failed (reviewer_settings_failed; detail withheld): exit status 2", true},
		{"learning adapter failed (reviewer_product_failed; detail withheld): exit status 2", true},
		{"learning adapter failed (reviewer_catalog_failed; detail withheld): exit status 2", true},
		{"learning adapter failed (reviewer_context_limit): exit status 2", true},
		{"learning adapter failed (reviewer_learning_disabled): exit status 2", true},
		{"learning adapter failed (reviewer_invoke_failed; detail withheld): exit status 2", false},
		{"learning adapter failed (reviewer_answer_failed; detail withheld): exit status 2", false},
		{"learning adapter failed (reviewer_failed; detail withheld): exit status 2", false},
		{"learning adapter failed (reviewer_context_unavailable): exit status 2", false},
		{"learning adapter failed (reviewer_job_busy): exit status 2", false},
		{"learning adapter failed: exit status 2", false},
		{"context deadline exceeded", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := reviewerFailureIsDeterministic(tc.reviewError); got != tc.want {
			t.Fatalf("%q: deterministic=%v, want %v", tc.reviewError, got, tc.want)
		}
	}
}

// A2.2: a review that failed for a reason that repeats identically is not
// offered for another attempt; a transient one still is.
func TestA2RecoveryDoesNotRetryDeterministicFailure(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	events := []struct{ id, run, reviewError string }{
		{"EV-a2-settings-00000001", "AW-CLI-A2-settings-001", "learning adapter failed (reviewer_settings_failed; detail withheld): exit status 2"},
		{"EV-a2-context-000000002", "AW-CLI-A2-context-0002", "learning adapter failed (reviewer_context_limit): exit status 2"},
		{"EV-a2-invoke-0000000003", "AW-CLI-A2-invoke-00003", "learning adapter failed (reviewer_invoke_failed; detail withheld): exit status 2"},
		{"EV-a2-legacy-0000000004", "AW-CLI-A2-legacy-00004", "learning adapter failed (reviewer_failed; detail withheld): exit status 2"},
	}
	if err := os.MkdirAll(filepath.Join(s.RuntimeRoot, "reviews"), 0700); err != nil {
		t.Fatal(err)
	}
	journal, err := os.OpenFile(filepath.Join(s.RuntimeRoot, "events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		line, _ := json.Marshal(map[string]string{"schema": "air.learning.event/v1", "event_id": ev.id, "run_id": ev.run, "kind": "run_completed", "source": "air-worker-cli"})
		if _, err := journal.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
		review, _ := json.Marshal(map[string]any{"schema": "air.learning.review/v1", "event_id": ev.id, "run_id": ev.run, "phase": "complete", "outcome": "error", "attempt": 1, "error": ev.reviewError, "at": "2026-11-15T00:00:00Z"})
		if err := os.WriteFile(filepath.Join(s.RuntimeRoot, "reviews", ev.id+".json"), review, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatal(err)
	}
	pending, err := pendingSharedLearningReviewRuns(product)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, run := range pending {
		got[run] = true
	}
	if got["AW-CLI-A2-settings-001"] || got["AW-CLI-A2-context-0002"] {
		t.Fatalf("deterministic failures offered for retry: %v", pending)
	}
	if !got["AW-CLI-A2-invoke-00003"] || !got["AW-CLI-A2-legacy-00004"] {
		t.Fatalf("transient or unclassified failures no longer retried: %v", pending)
	}
}
