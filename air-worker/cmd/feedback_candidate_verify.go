package main

import (
	"errors"
	"fmt"
	"strings"
)

// A mention of FB-ID is never a durable candidate. The only admissible
// candidate is the exact typed line in the single feedback section with
// the expected evidence path, status, type and severity.
func canonicalFeedbackCandidateState(raw []byte, r feedbackRecord, relativePath string) (bool, error) {
	body := strings.ReplaceAll(string(raw), "\r\n", "\n")
	marker := feedbackMarker
	if strings.Count(body, marker) > 1 {
		return false, errors.New("duplicate canonical feedback section marker")
	}
	expected := feedbackCandidateLine(r, relativePath)
	candidatePrefix := "- feedback " + string(rune(96)) + r.FeedbackID + string(rune(96))
	inSection := false
	count := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == marker {
			inSection = true
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(line), "## ") && inSection {
			inSection = false
		}
		if strings.Contains(line, candidatePrefix) {
			if !inSection || line != expected {
				return false, fmt.Errorf("conflicting or misplaced candidate for %s", r.FeedbackID)
			}
			count++
		}
	}
	if count > 1 {
		return false, fmt.Errorf("duplicate canonical feedback candidate for %s", r.FeedbackID)
	}
	return count == 1, nil
}
