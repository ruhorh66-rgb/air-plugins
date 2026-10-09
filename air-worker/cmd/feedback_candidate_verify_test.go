package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n108CanonicalRecord() feedbackRecord {
	return feedbackRecord{
		FeedbackID:      "FB-N108-001",
		CreatedAt:       "2026-10-09T07:24:00Z",
		Status:          "candidate",
		Product:         "air-worker",
		SourceVersion:   "0.11.8",
		Type:            "defect",
		Severity:        "P1",
		Observed:        "Real machine evidence",
		Expected:        "Machine evidence persists",
		Evidence:        "receipt/a.json",
		Reproduction:    "Run native feedback",
		Workaround:      "Do not retry blindly",
		ProposedOutcome: "Reconcile and verify",
	}
}

func TestN108CanonicalCandidateRejectsQuotedOrConflictingRows(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	line := feedbackCandidateLine(rec, rel)
	cases := []struct {
		name, plan string
		exists     bool
		bad        bool
	}{
		{"absent", "# Product\n\n", false, false},
		{"quoted-id", "# Product\n\nA quoted reference mentions " + rec.FeedbackID + "\n", false, false},
		{"exact", "# Product\n\n" + feedbackMarker + "\n" + line + "\n", true, false},
		{"wrong-status", "# Product\n\n" + feedbackMarker + "\n" + strings.Replace(line, "status="+string(rune(96))+"candidate", "status="+string(rune(96))+"closed", 1) + "\n", false, true},
		{"wrong-evidence", "# Product\n\n" + feedbackMarker + "\n" + strings.Replace(line, rel, ".air-worker/feedback/OTHER.json", 1) + "\n", false, true},
		{"exact-duplicate", "# Product\n\n" + feedbackMarker + "\n" + line + "\n" + line + "\n", false, true},
		{"outside-section", "# Product\n" + line + "\n\n" + feedbackMarker + "\n", false, true},
		{"double-marker", feedbackMarker + "\n" + feedbackMarker + "\n", false, true},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			active, err := canonicalFeedbackCandidateState([]byte(tt.plan), rec, rel)
			if tt.bad {
				if err == nil {
					t.Fatalf("invalid candidate falsely accepted: %s", tt.name)
				}
				return
			}
			if err != nil || active != tt.exists {
				t.Fatalf("%s active=%v err=%v", tt.name, active, err)
			}
			if !tt.exists {
				updated := planWithFeedbackCandidate([]byte(tt.plan), rec, rel)
				ok, err := canonicalFeedbackCandidateState(updated, rec, rel)
				if !ok || err != nil {
					t.Fatalf("insertion did not create canonical evidence: %v", err)
				}
			}
		})
	}
}

func TestN108DurableFeedbackReadbackRejectsNoopPlanWriter(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN.md")
	if err := os.WriteFile(plan, []byte("# Product\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rec := n108CanonicalRecord()
	io := defaultFeedbackIO()
	io.writePlan = func(path string, body []byte) error { return nil }
	result, err := writeFeedbackWithIO(root, plan, rec, io)
	if err == nil || result.PlanWritten {
		t.Fatalf("missing durable PLAN update falsely PASSed: %+v %v", result, err)
	}
	if !result.EvidenceWritten {
		t.Fatalf("evidence was not preserved before detected plan failure")
	}
}
