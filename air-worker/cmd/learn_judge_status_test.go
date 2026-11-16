package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// A3.3: the CLI itself says what an unconfigured judge means.
func TestA33JudgeStatusAndEventsNote(t *testing.T) {
	if st := learningJudgeStatus(sharedLearningSettings{}); st["configured"] != false || st["note"] != unconfiguredJudgeNote {
		t.Fatalf("unconfigured judge status: %v", st)
	}
	if st := learningJudgeStatus(sharedLearningSettings{Judge: &learningProcessAdapter{Executable: "@self"}}); st["configured"] != true || st["note"] != nil {
		t.Fatalf("configured judge status: %v", st)
	}
	// Rows other than the matching verdict keep their exact bytes.
	unrelated := []string{
		`{"schema":"air.learning.event/v1","event_id":"EV-0","n":12345678901234567890,"dup":1,"dup":2}`,
		`{ "schema" : "air.learning.event/v1", "observed" : "<a&b>" }`,
	}
	rows := `[` + unrelated[0] + `,` + unrelated[1] + `,` +
		`{"schema":"air.learning.verdict/v1","status":"unjudged","reason":"judge not configured","event_id":"EV-1"},` +
		`{"schema":"air.learning.verdict/v1","status":"unjudged","reason":"judge unavailable","event_id":"EV-2"},` +
		`{"schema":"air.learning.verdict/v1","status":"fail","reason":"K1","event_id":"EV-3"}]`
	got := annotateUnconfiguredJudgeVerdicts(learning.Response{Status: "ok", Data: json.RawMessage(rows)})
	for _, row := range unrelated {
		if !strings.Contains(string(got.Data), row) {
			t.Fatalf("an unrelated row was rewritten:\n%s", got.Data)
		}
	}
	var out []map[string]any
	if err := json.Unmarshal(got.Data, &out); err != nil || len(out) != 5 {
		t.Fatalf("annotated events unreadable: %v %s", err, got.Data)
	}
	for i, row := range out {
		_, has := row["note"]
		if has != (i == 2) {
			t.Fatalf("row %d note=%v: %v", i, has, row)
		}
	}
	if out[2]["note"] != unconfiguredJudgeNote || out[2]["reason"] != "judge not configured" {
		t.Fatalf("unconfigured verdict lost its fields or note: %v", out[2])
	}
	other := learning.Response{Status: "ok", Data: json.RawMessage(`{"skills":[]}`)}
	if string(annotateUnconfiguredJudgeVerdicts(other).Data) != `{"skills":[]}` {
		t.Fatal("non-list answer was changed")
	}
}

func publishTestVerdict(t *testing.T, root string, code int, failed []string) {
	t.Helper()
	mv := machineVerdict{Code: code, ChecksFailed: len(failed), CriteriaFailed: failed}
	raw, _ := json.Marshal(mv)
	notePublishedVerdict(root, mv, raw)
	t.Cleanup(resetPublishedVerdict)
}

// A3.4: a judge FAIL of this process carries the failed criterion IDs and the
// verdict hash, never free text, a path, or a verdict from elsewhere.
func TestA34JudgeFailureIsTypedFromThisRunsVerdict(t *testing.T) {
	product := t.TempDir()
	argv := []string{"judge", "-product", product, "-json"}
	publishTestVerdict(t, product, 1, []string{"К1 — probe failed with secret text", "K2;rm", "  "})
	if cause := ownCLITrustedFailureCause(argv, 1); cause != "judge-criteria-failed" {
		t.Fatalf("this run's FAIL verdict not typed: %q", cause)
	}
	obs := ownCLIFailureObservationFor(argv, "judge", 1, "judge-criteria-failed")
	if !strings.Contains(obs, "failed criteria К1, K2rm") || !strings.Contains(obs, "verdict .goal-verdict.json#sha256=") ||
		strings.Contains(obs, "probe failed") || strings.Contains(obs, "secret") || strings.Contains(obs, "K2;rm") ||
		strings.Contains(obs, filepath.ToSlash(product)) || strings.Contains(obs, product) {
		t.Fatalf("observation carries free text or a path, or lacks facts: %s", obs)
	}
	if cause := ownCLITrustedFailureCause([]string{"judge", "-product=" + product}, 1); cause != "judge-criteria-failed" {
		t.Fatalf("-product= form not read: %q", cause)
	}
	if cause := ownCLITrustedFailureCause(argv, 2); cause != "" {
		t.Fatalf("judge exit 2 typed as %q", cause)
	}

	// A verdict of another product, a passing verdict, or a FAIL that exists
	// only as a file (written by some other process) is not this run's FAIL.
	other := t.TempDir()
	publishTestVerdict(t, other, 1, []string{"K1"})
	if cause := ownCLITrustedFailureCause(argv, 1); cause != "" {
		t.Fatalf("verdict of another product typed as %q", cause)
	}
	publishTestVerdict(t, product, 0, nil)
	if cause := ownCLITrustedFailureCause(argv, 1); cause != "" {
		t.Fatalf("passing verdict typed as %q", cause)
	}
	resetPublishedVerdict()
	b, _ := json.Marshal(machineVerdict{Code: 1, CriteriaFailed: []string{"K9"}})
	if err := os.WriteFile(filepath.Join(product, ".goal-verdict.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	if cause := ownCLITrustedFailureCause(argv, 1); cause != "" {
		t.Fatalf("verdict file not published by this process typed as %q", cause)
	}
	if obs := ownCLIFailureObservationFor(argv, "judge", 1, ""); !strings.Contains(obs, "No specific precondition") {
		t.Fatalf("generic observation changed: %s", obs)
	}
}

// End to end: the captured learning event of a judge FAIL names the criteria.
func TestA34JudgeFailureEventCarriesCriteria(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	publishTestVerdict(t, product, 1, []string{"K7 — thing"})
	run := "AW-CLI-A34-judge-fail-01"
	if err := autoCaptureOwnCLIExitWithRunID([]string{"judge", "-product", product}, 1, run, nil); err != nil {
		t.Fatal(err)
	}
	n, event := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if n != 1 || !strings.Contains(jsonString(event["observed"]), "failed criteria K7") {
		t.Fatalf("judge FAIL event lacks criteria: %d %v", n, event)
	}
}
