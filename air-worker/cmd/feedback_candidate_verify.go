package main

import (
	"errors"
	"fmt"
	"strings"
)

const feedbackSectionHeading = "## Operational feedback candidates"

// An exact feedback row is authoritative only inside the real section.
// A quotation, nested/four-backtick fence, HTML block or comment must never
// be used as a machine result. The byte offset is after the real marker.
func canonicalFeedbackCandidatePlacement(raw []byte, rec feedbackRecord, relativePath string) (bool, int, error) {
	expected := feedbackCandidateLine(rec, relativePath)
	candidatePrefix := "- feedback " + string(rune(96)) + rec.FeedbackID + string(rune(96))
	var surface markdownControlSurface
	atHeading, seenHeading := false, false
	markerEnd, markers, count := -1, 0, 0
	offset := 0
	for _, segment := range strings.SplitAfter(string(raw), "\n") {
		if segment == "" {
			continue
		}
		line := strings.TrimSuffix(strings.TrimSuffix(segment, "\n"), "\r")
		trimmed := strings.TrimSpace(line)
		start := offset
		offset += len(segment)
		if surface.hidden(line, feedbackMarker) {
			if strings.Contains(line, candidatePrefix) {
				return false, -1, errors.New("feedback candidate inside a Markdown or HTML example is not machine evidence")
			}
			continue
		}
		if trimmed == feedbackMarker {
			if !atHeading {
				return false, -1, errors.New("feedback marker outside the real Operational feedback candidates section")
			}
			markers++
			if markers != 1 {
				return false, -1, errors.New("duplicate canonical feedback marker")
			}
			markerEnd = start + len(line)
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			atHeading = trimmed == feedbackSectionHeading
			if atHeading {
				if seenHeading {
					return false, -1, errors.New("duplicate canonical feedback heading")
				}
				seenHeading = true
			}
		}
		if strings.Contains(line, candidatePrefix) {
			if !atHeading || markerEnd < 0 || line != expected {
				return false, -1, fmt.Errorf("misplaced or conflicting candidate for %s", rec.FeedbackID)
			}
			count++
		}
	}
	if surface.htmlMalformed || surface.htmlPending != "" || len(surface.htmlStack) > 0 || surface.inlineRun > 0 || surface.fenceLen > 0 || surface.comment {
		return false, -1, errors.New("PLAN contains an unclosed or ambiguous Markdown/HTML context; feedback cannot be attested")
	}
	if count > 1 {
		return false, -1, fmt.Errorf("duplicate canonical candidate for %s", rec.FeedbackID)
	}
	if markers == 0 && count != 0 {
		return false, -1, errors.New("feedback candidate without authoritative marker")
	}
	return count == 1, markerEnd, nil
}

func canonicalFeedbackCandidateState(raw []byte, r feedbackRecord, relativePath string) (bool, error) {
	exists, _, err := canonicalFeedbackCandidatePlacement(raw, r, relativePath)
	return exists, err
}
