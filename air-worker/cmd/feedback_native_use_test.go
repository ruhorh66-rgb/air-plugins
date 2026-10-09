package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const n104SafeFeedbackProcedure = "# Capture a real defect through native AirWorker feedback\n\n" +
	"## When to apply\nWhen an actual AirWorker command fails and the operator needs a durable defect candidate.\n\n" +
	"## Procedure\n1. Select this learned skill by its exact current target@SHA.\n" +
	"2. Call air-worker feedback add with a stable -run-id, correct product and real error evidence.\n" +
	"3. Require the native event, immutable feedback receipt, and one PLAN candidate.\n\n" +
	"## Pitfalls\nDo not claim a model successfully fixed the error or approve a blocking rule.\n\n" +
	"## Machine-verifiable action\nAction: air-worker.feedback.add/v1\n"

func n104Setup(t *testing.T) (string, sharedLearningSettings, string, string, string) {
	t.Helper()
	selfRoot, selfSettings, _ := selfLearningFixture(t)
	skill := "skills/learned/native-feedback-lesson-n104.md"
	res, err := executeSharedLearning(selfRoot, selfSettings, "propose", map[string]string{
		"proposal_id": "LP-N104-skill", "kind": "procedure", "target": skill,
		"pre_sha256": "", "content": n104SafeFeedbackProcedure,
	})
	if err != nil || res.Status != "applied" {
		t.Fatalf("seed governed safe skill failed: %+v %v", res, err)
	}
	product, _ := sharedProductFixture(t, false)
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte(`{"plan":"PLAN.md"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "PLAN.md"), []byte("# Target product\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return selfRoot, selfSettings, product, skill, learnSHA([]byte(n104SafeFeedbackProcedure))
}
func n104CLI(t *testing.T, product, runID, selection string) (int, string) {
	t.Helper()
	args := []string{"add", "-product", product, "-kind", "error",
		"-source", "n104-test-worker", "-run-id", runID,
		"-text", "real fixture subprocess exited nonzero",
		"-ref", "fixture/real-error.json", "-type", "defect", "-severity", "P1",
		"-source-version", "0.11.8", "-expected", "recovery after verified evidence",
		"-reproduction", "run supported native command then inspect exit",
		"-workaround", "no blind retry", "-proposed-outcome", "durable feedback",
	}
	if selection != "" {
		args = append(args, "-use-procedure", selection)
	}
	return captureLoopOutput(t, func() int { return cmdFeedback(args) })
}
func n104Events(t *testing.T, runtimeRoot string, kind string) int {
	t.Helper()
	n := 0
	if err := scanLearnJSONL(filepath.Join(runtimeRoot, "events.jsonl"), func(b []byte) error {
		var item map[string]any
		if err := json.Unmarshal(b, &item); err != nil {
			return err
		}
		if item["schema"] == "air.learning.event/v1" && item["kind"] == kind {
			n++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestN104NativeSelectedSafeProcedureRecordsVerifiedEffect(t *testing.T) {
	_, selfSettings, target, skill, hash := n104Setup(t)
	runID := "N104-native-feedback-use"
	selected := skill + "@" + hash
	code, out := n104CLI(t, target, runID, selected)
	if code != 0 {
		t.Fatalf("native learned feedback failed rc=%d out=%s", code, out)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("expected one machine-verified used event, got %d", got)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "run_completed"); got != 0 {
		t.Fatalf("the self-owner was polluted by product feedback: %d", got)
	}
	id := "FB-" + learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24]
	file := filepath.Join(target, filepath.FromSlash(feedbackEvidenceRel(id)))
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var r feedbackRecord
	if err := json.Unmarshal(b, &r); err != nil || r.RunID != runID || r.EventID == "" {
		t.Fatalf("canonical feedback inconsistent: %+v %v", r, err)
	}
	plan, err := os.ReadFile(filepath.Join(target, "PLAN.md"))
	if err != nil || strings.Count(string(plan), "- feedback `"+id+"`") != 1 {
		t.Fatalf("PLAN candidate does not match exactly one native operation: %v", err)
	}
	code, out = n104CLI(t, target, runID, selected)
	if code != 0 {
		t.Fatalf("native idempotent replay failed: rc=%d out=%s", code, out)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("retry duplicated verified use event: %d", got)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "skill_loaded"); got != 0 {
		t.Fatalf("loaded events unexpectedly changed schema: %d", got)
	}
}
func TestN104RejectsForeignStaleUnselectedAndUnsupportedProcedureUse(t *testing.T) {
	selfRoot, selfSettings, target, skill, hash := n104Setup(t)
	wrong := "skills/learned/not-owned.md@" + hash
	code, _ := n104CLI(t, target, "N104-foreign", wrong)
	if code == 0 {
		t.Fatal("foreign learned target selected")
	}
	code, _ = n104CLI(t, target, "N104-wrong-sha", skill+"@"+strings.Repeat("0", 64))
	if code == 0 {
		t.Fatal("incorrect SHA accepted")
	}
	orig := filepath.Join(selfRoot, filepath.FromSlash(skill))
	if err := os.WriteFile(orig, []byte(n104SafeFeedbackProcedure+"\nchanged unexpectedly\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _ = n104CLI(t, target, "N104-stale", skill+"@"+hash)
	if code == 0 {
		t.Fatal("modified learned procedure credited")
	}
	code, out := n104CLI(t, target, "N104-without-selection", "")
	if code != 0 {
		t.Fatalf("regular feedback should be unaffected rc=%d out=%s", code, out)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("unselected or foreign skill falsely used %d", got)
	}
}
func TestN104NoMachineActionMarkerMeansLoadedButNeverUsed(t *testing.T) {
	root, s, target, _, _ := n104Setup(t)
	other := "skills/learned/unsupported-informational-lesson.md"
	text := "# Inspect outcomes\n\n## When to apply\nWhen a job ended.\n\n## Procedure\n1. Examine the receipt.\n\n## Pitfalls\nDo not invent results.\n"
	result, err := executeSharedLearning(root, s, "propose", map[string]string{
		"proposal_id": "LP-N104-untyped", "kind": "procedure", "target": other, "pre_sha256": "", "content": text})
	if err != nil || result.Status != "applied" {
		t.Fatal(err)
	}
	code, _ := n104CLI(t, target, "N104-unsupported", other+"@"+learnSHA([]byte(text)))
	if code == 0 {
		t.Fatal("untyped prose falsely claimed native executable use")
	}
	if got := n104Events(t, s.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("false used=%d", got)
	}
}

func TestN104ReviewerOnlyBindsProvenSafeNativeAction(t *testing.T) {
	realLesson := "# Verify self-learning feedback intake\n\n" +
		"## When to apply\nWhen product self-learning is missing.\n\n" +
		"## Procedure\n1. Inspect the exact installed binary version.\n" +
		"2. Submit any defect through the native feedback path with a stable run ID.\n" +
		"3. Compare source event, receipt and PLAN candidate.\n\n" +
		"## Pitfalls\nDo not treat loaded guidance as used.\n"
	newLesson, err := bindNativeFeedbackProof("feedback-error", realLesson)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(newLesson, nativeFeedbackUseAction) != 1 {
		t.Fatalf("positive real reviewer procedure did not gain supported action: %s", newLesson)
	}
	for _, c := range []struct{ class, text string }{
		{"completed-turn", realLesson + "\n" + nativeFeedbackUseAction + "\n"},
		{"feedback-error", "# Unrelated\n\n## When to apply\nUnknown.\n\n## Procedure\n1. Do not submit via the native feedback path with a stable run ID.\n\n## Pitfalls\nNever claim success.\n\n" + nativeFeedbackUseAction + "\n"},
		{"feedback-error", newLesson + "\n" + nativeFeedbackUseAction + "\n"},
	} {
		if _, err := bindNativeFeedbackProof(c.class, c.text); err == nil {
			t.Fatalf("unsafe reviewer action was bound for class %s", c.class)
		}
	}
}

func TestN104CannotClaimRetroactiveOrDifferentLearnedRule(t *testing.T) {
	root, settings, target, skill, sha := n104Setup(t)
	run := "N104-retroactive"
	if code, response := n104CLI(t, target, run, ""); code != 0 {
		t.Fatalf("baseline unselected operation failed: %d %s", code, response)
	}
	if code, _ := n104CLI(t, target, run, skill+"@"+sha); code == 0 {
		t.Fatal("retroactive learned-skill selection accepted after feedback was recorded")
	}
	run2 := "N104-different-selected-rule"
	if code, response := n104CLI(t, target, run2, skill+"@"+sha); code != 0 {
		t.Fatalf("baseline selected operation failed: %d %s", code, response)
	}
	alt := "skills/learned/a-different-feedback-lesson.md"
	diff := strings.ReplaceAll(n104SafeFeedbackProcedure, "a real defect", "another real defect")
	result, err := executeSharedLearning(root, settings, "propose", map[string]string{
		"proposal_id": "LP-N104-other", "kind": "procedure", "target": alt,
		"pre_sha256": "", "content": diff,
	})
	if err != nil || result.Status != "applied" {
		t.Fatalf("second valid safe lesson did not apply: %v %+v", err, result)
	}
	if code, _ := n104CLI(t, target, run2, alt+"@"+learnSHA([]byte(diff))); code == 0 {
		t.Fatal("same recorded native feedback was attributed to a second learned rule")
	}
	if used := n104Events(t, settings.RuntimeRoot, "procedure_used"); used != 1 {
		t.Fatalf("two procedure uses were counted for one selection: %d", used)
	}
}
