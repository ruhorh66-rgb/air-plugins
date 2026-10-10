package main

import (
	"errors"
	"fmt"
	"strings"
)

const feedbackSectionHeading = "## Operational feedback candidates"

// The byte offset points after the one real, non-fenced marker.
// A quoted example is never an authoritative control-plane candidate.
func canonicalFeedbackCandidatePlacement(raw []byte, rec feedbackRecord, relativePath string) (bool, int, error) {
	text := string(raw)
	expected := feedbackCandidateLine(rec, relativePath)
	candidatePrefix := "- feedback " + string(rune(96)) + rec.FeedbackID + string(rune(96))
	inFence, inComment := false, false
	fenceKind := ""
	atHeading, seenHeading := false, false
	markerEnd, markers, count := -1, 0, 0
	offset := 0
	for _, segment := range strings.SplitAfter(text, "\n") {
		if segment == "" {
			continue
		}
		line := strings.TrimSuffix(strings.TrimSuffix(segment, "\n"), "\r")
		trimmed := strings.TrimSpace(line)
		start := offset
		offset += len(segment)
		if strings.HasPrefix(trimmed, "~~~") || strings.HasPrefix(trimmed, "```") {
			kind := trimmed[:3]
			if !inFence {
				inFence = true
				fenceKind = kind
			} else if fenceKind == kind {
				inFence = false
			}
			continue
		}
		if inFence {
			if strings.Contains(line, candidatePrefix) {
				return false, -1, errors.New("feedback candidate only exists inside a fenced code example")
			}
			continue
		}
		if inComment {
			if strings.Contains(line, candidatePrefix) {
				return false, -1, errors.New("feedback candidate quoted inside an HTML comment")
			}
			if strings.Contains(line, "-->") {
				inComment = false
			}
			continue
		}
		// feedbackMarker is a comment: trusted ONLY beneath the real H2.
		if trimmed == feedbackMarker {
			if !atHeading {
				return false, -1, errors.New("feedback marker is outside the real Operational feedback candidates section")
			}
			markers++
			if markers != 1 {
				return false, -1, errors.New("duplicate feedback marker")
			}
			markerEnd = start + len(line)
			continue
		}
		if strings.HasPrefix(trimmed, "<!--") {
			if strings.Contains(line, candidatePrefix) {
				return false, -1, errors.New("feedback candidate quoted inside HTML comment")
			}
			if !strings.Contains(trimmed, "-->") {
				inComment = true
			}
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
				return false, -1, fmt.Errorf("misplaced or conflicting feedback candidate %s", rec.FeedbackID)
			}
			count++
		}
	}
	if count > 1 {
		return false, -1, fmt.Errorf("duplicate canonical feedback candidate %s", rec.FeedbackID)
	}
	if markers == 0 && count != 0 {
		return false, -1, errors.New("feedback candidate exists without an authoritative marker")
	}
	return count == 1, markerEnd, nil
}

func canonicalFeedbackCandidateState(raw []byte, r feedbackRecord, relativePath string) (bool, error) {
	exists, _, err := canonicalFeedbackCandidatePlacement(raw, r, relativePath)
	return exists, err
}
