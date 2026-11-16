package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	rows := `[{"schema":"air.learning.event/v1","event_id":"EV-1","kind":"run_completed"},` +
		`{"schema":"air.learning.verdict/v1","status":"unjudged","reason":"judge not configured","event_id":"EV-1"},` +
		`{"schema":"air.learning.verdict/v1","status":"unjudged","reason":"judge unavailable","event_id":"EV-2"},` +
		`{"schema":"air.learning.verdict/v1","status":"fail","reason":"K1","event_id":"EV-3"}]`
	got := annotateUnconfiguredJudgeVerdicts(learning.Response{Status: "ok", Data: json.RawMessage(rows)})
	var out []map[string]any
	if err := json.Unmarshal(got.Data, &out); err != nil || len(out) != 4 {
		t.Fatalf("annotated events unreadable: %v %s", err, got.Data)
	}
	for i, row := range out {
		_, has := row["note"]
		if has != (i == 1) {
			t.Fatalf("row %d note=%v: %v", i, has, row)
		}
	}
	if out[1]["note"] != unconfiguredJudgeNote || out[1]["reason"] != "judge not configured" {
		t.Fatalf("unconfigured verdict lost its fields or note: %v", out[1])
	}
	other := learning.Response{Status: "ok", Data: json.RawMessage(`{"skills":[]}`)}
	if string(annotateUnconfiguredJudgeVerdicts(other).Data) != `{"skills":[]}` {
		t.Fatal("non-list answer was changed")
	}
}

func writeGoalVerdict(t *testing.T, product string, code int, at time.Time, failed []string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"at": at.Format(time.RFC3339Nano), "code": code, "checks_failed": len(failed), "criteria_failed": failed})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, ".goal-verdict.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// A3.4: a judge FAIL of this run carries the failed criterion IDs and the
// verdict reference, never free text from checks.
func TestA34JudgeFailureIsTypedFromFreshVerdict(t *testing.T) {
	product := t.TempDir()
	argv := []string{"judge", "-product", product, "-json"}
	writeGoalVerdict(t, product, 1, time.Now(), []string{"К1 — probe failed with secret text", "K2;rm", "  "})
	if cause := ownCLITrustedFailureCause(argv, 1); cause != "judge-criteria-failed" {
		t.Fatalf("fresh FAIL verdict not typed: %q", cause)
	}
	obs := ownCLIFailureObservationFor(argv, "judge", 1, "judge-criteria-failed")
	if !strings.Contains(obs, "failed criteria К1, K2rm") || !strings.Contains(obs, ".goal-verdict.json#sha256=") ||
		strings.Contains(obs, "probe failed") || strings.Contains(obs, "secret") || strings.Contains(obs, "K2;rm") {
		t.Fatalf("observation carries free text or lacks facts: %s", obs)
	}
	if cause := ownCLITrustedFailureCause([]string{"judge", "-product=" + product}, 1); cause != "judge-criteria-failed" {
		t.Fatalf("-product= form not read: %q", cause)
	}

	for name, setup := range map[string]func(){
		"stale verdict": func() { writeGoalVerdict(t, product, 1, cliProcessStarted.Add(-time.Hour), []string{"K1"}) },
		"pass verdict":  func() { writeGoalVerdict(t, product, 0, time.Now(), nil) },
		"not a file": func() {
			_ = os.Remove(filepath.Join(product, ".goal-verdict.json"))
			if err := os.Mkdir(filepath.Join(product, ".goal-verdict.json"), 0o700); err != nil {
				t.Fatal(err)
			}
		},
	} {
		setup()
		if cause := ownCLITrustedFailureCause(argv, 1); cause != "" {
			t.Fatalf("%s: typed as %q", name, cause)
		}
		if obs := ownCLIFailureObservationFor(argv, "judge", 1, ""); !strings.Contains(obs, "No specific precondition") {
			t.Fatalf("%s: generic observation changed: %s", name, obs)
		}
		_ = os.RemoveAll(filepath.Join(product, ".goal-verdict.json"))
	}
	if cause := ownCLITrustedFailureCause(argv, 2); cause != "" {
		t.Fatalf("judge exit 2 typed as %q", cause)
	}
}

// End to end: the captured learning event of a judge FAIL names the criteria.
func TestA34JudgeFailureEventCarriesCriteria(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	writeGoalVerdict(t, product, 1, time.Now(), []string{"K7 — thing"})
	run := "AW-CLI-A34-judge-fail-01"
	if err := autoCaptureOwnCLIExitWithRunID([]string{"judge", "-product", product}, 1, run, nil); err != nil {
		t.Fatal(err)
	}
	n, event := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if n != 1 || !strings.Contains(jsonString(event["observed"]), "failed criteria K7") {
		t.Fatalf("judge FAIL event lacks criteria: %d %v", n, event)
	}
}
