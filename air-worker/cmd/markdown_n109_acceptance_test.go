package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN109CheckedInPLANRetainsRealFeedbackSection(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	rec := n108CanonicalRecord()
	relative := feedbackEvidenceRel(rec.FeedbackID)
	exists, err := canonicalFeedbackCandidateState(raw, rec, relative)
	if err != nil || exists {
		t.Fatalf("real product PLAN's inline angle brackets hid the canonical section: exists=%v err=%v", exists, err)
	}
	updated := planWithFeedbackCandidate(raw, rec, relative)
	active, err := canonicalFeedbackCandidateState(updated, rec, relative)
	if err != nil || !active {
		t.Fatalf("real PLAN candidate cannot be inserted and proved: %v", err)
	}
	replay := planWithFeedbackCandidate(updated, rec, relative)
	if !bytes.Equal(updated, replay) {
		t.Fatal("normal native feedback replay mutated the real PLAN candidate twice")
	}
}

func TestN109ActualProductPLANFeedbackDurableReadback(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	temp := t.TempDir()
	file := filepath.Join(temp, "PLAN.md")
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	rec := n108CanonicalRecord()
	result, err := writeFeedbackWithIO(temp, file, rec, defaultFeedbackIO())
	if err != nil || !result.EvidenceWritten || !result.PlanWritten {
		t.Fatalf("actual PLAN copy produced a partial or forged feedback outcome: %+v err=%v", result, err)
	}
	onDisk, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	active, err := canonicalFeedbackCandidateState(onDisk, rec, feedbackEvidenceRel(rec.FeedbackID))
	if err != nil || !active {
		t.Fatalf("actual product PLAN feedback effect cannot be verified on readback: %v", err)
	}
}

func TestN109InlineCodeAnglesAndWholeQuotedStep(t *testing.T) {
	rec := n108CanonicalRecord()
	relative := feedbackEvidenceRel(rec.FeedbackID)
	before := "# Product\n\nUse `air-worker.exe hook <event>` and `<ref>` as examples.\n" +
		"Escaped angle markup \\<event> is not a raw HTML block.\n\n"
	updated := planWithFeedbackCandidate([]byte(before), rec, relative)
	ok, err := canonicalFeedbackCandidateState(updated, rec, relative)
	if err != nil || !ok {
		t.Fatalf("inline-code angles incorrectly hid a real PLAN candidate: %v", err)
	}
	good := "1. Report the defect with the native `air-worker feedback add` command, using a stable `-run-id` and bounded `-data-file` JSON containing the exact error."
	lesson := "# Quoted reviewer\n\n## When to apply\nActual failure.\n\n## Procedure\n" +
		"``" + good + "``\n\n## Pitfalls\nNo invented effect.\n"
	answer, err := bindNativeFeedbackProof("feedback-error", lesson)
	if err != nil || strings.Contains(answer, nativeFeedbackUseAction) {
		t.Fatalf("whole inline-code example falsely obtained native Action: %v", err)
	}
}

func TestN109MultilineRawHTMLFailClosed(t *testing.T) {
	rec := n108CanonicalRecord()
	rel := feedbackEvidenceRel(rec.FeedbackID)
	row := feedbackCandidateLine(rec, rel)
	fake := "# Product\n\n<div class=\"demo\"\n>\n" + feedbackSectionHeading + "\n" +
		feedbackMarker + "\n" + row + "\n</div>\n"
	exists, err := canonicalFeedbackCandidateState([]byte(fake), rec, rel)
	if exists || err == nil {
		t.Fatalf("multiline raw HTML copied candidate falsely credited: %v %v", exists, err)
	}
	markerOnly := "# Product\n\n<div class=\"demo\"\n>\n" +
		feedbackSectionHeading + "\n" + feedbackMarker + "\n</div>\n"
	exists, err = canonicalFeedbackCandidateState([]byte(markerOnly), rec, rel)
	if err != nil || exists {
		t.Fatalf("closed multiline HTML example must stay hidden: exists=%v err=%v", exists, err)
	}
	appended := planWithFeedbackCandidate([]byte(markerOnly), rec, rel)
	active, err := canonicalFeedbackCandidateState(appended, rec, rel)
	if err != nil || !active {
		t.Fatalf("real native feedback outside the closed HTML example must verify: %v", err)
	}
	good := "1. Report the defect with the native `air-worker feedback add` command, using a stable `-run-id` and bounded `-data-file` JSON containing the exact error."
	lesson := "# Unsafe\n\n## When to apply\nFailure.\n\n## Procedure\n" +
		"<div class=\"demo\"\n>\n" + good + "\n</div>\n\n## Pitfalls\nNo invented success.\n"
	result, err := bindNativeFeedbackProof("feedback-error", lesson)
	if err != nil || strings.Contains(result, nativeFeedbackUseAction) {
		t.Fatalf("HTML wrapped learned text was credited as an executable action: %v", err)
	}
}
