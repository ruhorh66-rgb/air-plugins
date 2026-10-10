package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n108CanonicalRecord() feedbackRecord {
	return feedbackRecord{
		FeedbackID: "FB-N108-001", CreatedAt: "2026-10-09T07:24:00Z",
		Status: "candidate", Product: "air-worker", SourceVersion: "0.11.8",
		Type: "defect", Severity: "P1", Observed: "Real machine evidence",
		Expected: "Machine evidence persists", Evidence: "receipt/a.json",
		Reproduction: "Run native feedback", Workaround: "Do not retry blindly",
		ProposedOutcome: "Reconcile and verify",
	}
}
func TestN108CanonicalCandidateRejectsQuotedOrConflictingRows(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	line := feedbackCandidateLine(rec, rel)
	heading := "\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n"
	cases := []struct {
		name, plan  string
		exists, bad bool
	}{
		{"absent", "# Product\n\n", false, false},
		{"quoted-id", "# Product\n\nA quoted reference mentions " + rec.FeedbackID + "\n", false, false},
		{"exact", "# Product\n" + heading + line + "\n", true, false},
		{"wrong-status", "# Product\n" + heading + strings.Replace(line, "status="+string(rune(96))+"candidate", "status="+string(rune(96))+"closed", 1) + "\n", false, true},
		{"wrong-evidence", "# Product\n" + heading + strings.Replace(line, rel, ".air-worker/feedback/OTHER.json", 1) + "\n", false, true},
		{"exact-duplicate", "# Product\n" + heading + line + "\n" + line + "\n", false, true},
		{"outside-section", "# Product\n" + line + "\n" + heading, false, true},
		{"double-marker", "# Product\n" + heading + feedbackMarker + "\n", false, true},
		{"marker-without-heading", "# Product\n" + feedbackMarker + "\n" + line + "\n", false, true},
		{"fenced-exact", "# Product\n\n```md\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n" + line + "\n```\n", false, true},
		{"comment-exact", "# Product\n\n<!-- copied example\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n" + line + "\n-->\n", false, true},
		{"wrong-heading", "# Product\n\n## Other feedback\n" + feedbackMarker + "\n" + line + "\n", false, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			active, err := canonicalFeedbackCandidateState([]byte(test.plan), rec, rel)
			if test.bad {
				if err == nil {
					t.Fatalf("invalid candidate falsely accepted: %s active=%v", test.name, active)
				}
				return
			}
			if err != nil || active != test.exists {
				t.Fatalf("%s active=%v err=%v", test.name, active, err)
			}
			if !test.exists {
				updated := planWithFeedbackCandidate([]byte(test.plan), rec, rel)
				ok, err := canonicalFeedbackCandidateState(updated, rec, rel)
				if !ok || err != nil {
					t.Fatalf("valid insertion not recognized: %v", err)
				}
			}
		})
	}
}
func TestN108QuotedMarkerOnlyCannotSteerInsertion(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	example := "# Product\n\n```markdown\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n```\n"
	present, err := canonicalFeedbackCandidateState([]byte(example), rec, rel)
	if err != nil || present {
		t.Fatalf("fenced example unexpectedly authoritative: %v", err)
	}
	updated := planWithFeedbackCandidate([]byte(example), rec, rel)
	present, err = canonicalFeedbackCandidateState(updated, rec, rel)
	if err != nil || !present {
		t.Fatalf("actual appended section absent: %v", err)
	}
	if strings.Count(string(updated), feedbackMarker) != 2 {
		t.Fatal("example was overwritten")
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
		t.Fatalf("no durable PLAN update falsely PASSed: %+v %v", result, err)
	}
	if !result.EvidenceWritten {
		t.Fatal("durable evidence should be persisted before PLAN failure")
	}
}

func TestN108NestedFourBacktickAndHTMLExamplesCannotProveFeedback(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	line := feedbackCandidateLine(rec, rel)
	examples := map[string]string{
		"four-backtick-inner-three": "# Product\n\n````md\n```\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n" + line + "\n````\n",
		"pre-block":                 "# Product\n\n<pre>\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n" + line + "\n</pre>\n",
		"block-quote":               "# Product\n\n> " + feedbackSectionHeading + "\n> " + feedbackMarker + "\n> " + line + "\n",
	}
	for name, source := range examples {
		t.Run(name, func(t *testing.T) {
			exists, err := canonicalFeedbackCandidateState([]byte(source), rec, rel)
			if exists || err == nil {
				t.Fatalf("false PLAN receipt from quoted example: exists=%v err=%v", exists, err)
			}
		})
	}
}

func TestN108QuotedMarkdownMarkerPreservesExampleOnRealInsertion(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	examples := map[string]string{
		"four-backtick": "# Product\n\n````md\n```\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n````\n",
		"pre-block":     "# Product\n\n<pre>\n" + feedbackSectionHeading + "\n" + feedbackMarker + "\n</pre>\n",
	}
	for name, source := range examples {
		t.Run(name, func(t *testing.T) {
			had, err := canonicalFeedbackCandidateState([]byte(source), rec, rel)
			if err != nil || had {
				t.Fatalf("untrusted marker was used: %v", err)
			}
			updated := planWithFeedbackCandidate([]byte(source), rec, rel)
			present, err := canonicalFeedbackCandidateState(updated, rec, rel)
			if err != nil || !present {
				t.Fatalf("true inserted section missing: %v", err)
			}
			if !strings.Contains(string(updated), source) {
				t.Fatal("insertion erased old example")
			}
		})
	}
}

func TestN108IndentedFenceAndTextareaCannotForgeCanonicalPLAN(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	row := feedbackCandidateLine(rec, rel)
	copied := feedbackSectionHeading + "\n" + feedbackMarker + "\n" + row + "\n"
	examples := map[string]string{
		"four-backtick-with-indented-closer":       "# Product\n\n````markdown\n    ````\n" + copied + "````\n",
		"four-backtick-with-three-backtick-closer": "# Product\n\n````markdown\n```\n" + copied + "````\n",
		"html-textarea": "# Product\n\n<textarea>\n" + copied + "</textarea>\n",
	}
	for label, text := range examples {
		t.Run(label, func(t *testing.T) {
			found, err := canonicalFeedbackCandidateState([]byte(text), rec, rel)
			if found || err == nil {
				t.Fatalf("example-only PLAN got machine credit: found=%v err=%v", found, err)
			}
		})
	}
}

func TestN108IndentedCloserOrHTMLExampleMarkerCannotRedirectInsertion(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	marker := feedbackSectionHeading + "\n" + feedbackMarker + "\n"
	examples := map[string]string{
		"four-backtick-indented-closer": "# Product\n\n````markdown\n    ````\n" + marker + "````\n",
		"textarea":                      "# Product\n\n<textarea>\n" + marker + "</textarea>\n",
	}
	for label, example := range examples {
		t.Run(label, func(t *testing.T) {
			present, err := canonicalFeedbackCandidateState([]byte(example), rec, rel)
			if err != nil || present {
				t.Fatalf("example marker already authoritative: present=%v err=%v", present, err)
			}
			appended := planWithFeedbackCandidate([]byte(example), rec, rel)
			present, err = canonicalFeedbackCandidateState(appended, rec, rel)
			if err != nil || !present {
				t.Fatalf("valid separate PLAN candidate not inserted: %v", err)
			}
			if strings.Count(string(appended), feedbackMarker) != 2 {
				t.Fatalf("untrusted example was rewritten or no real marker added")
			}
		})
	}
}

func TestN108GenericHTMLCannotClaimCanonicalPLAN(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	row := feedbackCandidateLine(rec, rel)
	copyText := feedbackSectionHeading + "\n" + feedbackMarker + "\n" + row + "\n"
	cases := map[string]string{
		"div":                "<div>\n" + copyText + "</div>\n",
		"div-with-attribute": "<div class=\"demo\">\n" + copyText + "</div>\n",
		"nested-div":         "<div><div>\n</div>\n" + copyText + "</div>\n",
		"section":            "<section>\n" + copyText + "</section>\n",
		"custom":             "<custom-card>\n" + copyText + "</custom-card>\n",
	}
	for label, html := range cases {
		t.Run(label, func(t *testing.T) {
			plan := "# Product\n\n" + html
			found, err := canonicalFeedbackCandidateState([]byte(plan), rec, rel)
			if found || err == nil {
				t.Fatalf("untrusted HTML example used as canonical PLAN receipt: %s found=%v err=%v", label, found, err)
			}
		})
	}
}

func TestN108GenericHTMLMarkerCannotRedirectPLANInsertion(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	marker := feedbackSectionHeading + "\n" + feedbackMarker + "\n"
	plans := map[string]string{
		"div":      "# Product\n\n<div>\n" + marker + "</div>\n",
		"nested":   "# Product\n\n<div><div>\n</div>\n" + marker + "</div>\n",
		"textarea": "# Product\n\n<textarea>\n" + marker + "</textarea>\n",
	}
	for label, plan := range plans {
		t.Run(label, func(t *testing.T) {
			active, err := canonicalFeedbackCandidateState([]byte(plan), rec, rel)
			if err != nil || active {
				t.Fatalf("HTML-only copied marker should be inert: %s err=%v", label, err)
			}
			written := planWithFeedbackCandidate([]byte(plan), rec, rel)
			active, err = canonicalFeedbackCandidateState(written, rec, rel)
			if err != nil || !active {
				t.Fatalf("could not create actual candidate outside HTML: %s err=%v", label, err)
			}
			if !strings.Contains(string(written), plan) {
				t.Fatalf("canonical insertion destroyed HTML example for %s", label)
			}
		})
	}
}
