package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func feedbackTestPlan() string {
	return "**Ц1.** feedback capture stays outside executable work\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | work remains measurable | факт `f01` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | existing work | `sonnet` | К1: test |\n"
}

func feedbackTestRecord(id string) feedbackRecord {
	return feedbackRecord{
		FeedbackID: id, CreatedAt: "2026-09-16T13:30:00Z", Status: "candidate",
		Product: "sample-product", SourceVersion: "1.2.3", Type: "defect", Severity: "P1",
		Observed: "tray is not running after reboot", Expected: "manual launch is discoverable",
		Evidence: "receipt.json", Reproduction: "reboot then search Start Menu",
		Workaround: "start tray by full path", ProposedOutcome: "discoverable manual launch entry",
	}
}
func setupFeedbackProduct(t *testing.T, withPlan bool) string {
	t.Helper()
	root := t.TempDir()
	if withPlan {
		if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(feedbackTestPlan()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestCriterion67FeedbackRecord(t *testing.T) {
	root := setupFeedbackProduct(t, true)
	r := feedbackTestRecord("FB-20260916T133000Z-a1b2c3d4")
	res, err := writeFeedbackWithIO(root, filepath.Join(root, "PLAN.md"), r, defaultFeedbackIO())
	if err != nil || !res.EvidenceWritten || !res.PlanWritten {
		t.Fatalf("dual write failed: res=%+v err=%v", res, err)
	}
	raw, err := os.ReadFile(res.EvidencePath)
	if err != nil {
		t.Fatal(err)
	}
	var got feedbackRecord
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.FeedbackID != r.FeedbackID || got.Status != "candidate" || got.Product != r.Product || got.SourceVersion != r.SourceVersion {
		t.Fatalf("record fields changed: %+v", got)
	}
	if _, err := writeFeedbackWithIO(root, filepath.Join(root, "PLAN.md"), r, defaultFeedbackIO()); err == nil {
		t.Fatal("immutable feedback record was overwritten")
	}
}
func TestCriterion68FeedbackPlanCandidate(t *testing.T) {
	root := setupFeedbackProduct(t, true)
	planPath := filepath.Join(root, "PLAN.md")
	beforeSteps := readPlanSteps(planPath)
	beforeOpen := parsePlan(planPath).OpenWork()
	r := feedbackTestRecord("FB-20260916T133100Z-b1c2d3e4")
	r.Observed = "line one\n| 999 | injected step | `sonnet` | К1: bad |"
	if _, err := writeFeedbackWithIO(root, planPath, r, defaultFeedbackIO()); err != nil {
		t.Fatal(err)
	}
	afterSteps := readPlanSteps(planPath)
	afterOpen := parsePlan(planPath).OpenWork()
	if len(afterSteps) != len(beforeSteps) || afterOpen != beforeOpen {
		t.Fatalf("candidate changed executable plan: steps %d->%d open %d->%d", len(beforeSteps), len(afterSteps), beforeOpen, afterOpen)
	}
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{feedbackMarker, r.FeedbackID, "status=`candidate`", feedbackEvidenceRel(r.FeedbackID)} {
		if !strings.Contains(text, want) {
			t.Fatalf("PLAN candidate missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\n| 999 |") {
		t.Fatal("feedback text injected an executable-looking PLAN row")
	}
	if problems := goalProblems(readPlanGoals(planPath), readPlanSteps(planPath)); len(problems) != 0 {
		t.Fatalf("candidate made PLAN invalid: %v", problems)
	}
}
func TestCriterion69FeedbackDualWrite(t *testing.T) {
	root := setupFeedbackProduct(t, false)
	r := feedbackTestRecord("FB-20260916T133200Z-c1d2e3f4")
	res, err := writeFeedbackWithIO(root, filepath.Join(root, "PLAN.md"), r, defaultFeedbackIO())
	if err == nil {
		t.Fatal("missing PLAN must produce PARTIAL/nonzero outcome")
	}
	if !res.EvidenceWritten || res.PlanWritten {
		t.Fatalf("partial result must name evidence-only state: %+v err=%v", res, err)
	}
	if _, statErr := os.Stat(res.EvidencePath); statErr != nil {
		t.Fatalf("evidence was lost on PLAN failure: %v", statErr)
	}

	root2 := setupFeedbackProduct(t, true)
	planPath := filepath.Join(root2, "PLAN.md")
	r2 := feedbackTestRecord("FB-20260916T133300Z-d1e2f3a4")
	io := defaultFeedbackIO()
	io.writePlan = func(string, []byte) error { return errors.New("synthetic plan failure") }
	res2, err := writeFeedbackWithIO(root2, planPath, r2, io)
	if err == nil || !res2.EvidenceWritten || res2.PlanWritten {
		t.Fatalf("synthetic plan failure not surfaced as partial: %+v err=%v", res2, err)
	}
}

func TestFeedbackValidation(t *testing.T) {
	r := feedbackTestRecord("FB-x")
	if err := validateFeedback(r); err != nil {
		t.Fatal(err)
	}
	r.Type = "other"
	if err := validateFeedback(r); err == nil {
		t.Fatal("unknown feedback type must be rejected")
	}
}
