package main

import (
	"errors"
	"strings"
	"testing"
)

func TestN130FailedClaudeReviewerCannotPromoteValidLookingPASS(t *testing.T) {
	stream := "{\"type\":\"result\",\"result\":\"VERDICT_PASS\",\"session_id\":\"safe-session\",\"is_error\":false}\n"
	parsed, session, _, _, err := parseClaudeSemanticResult(stream, errors.New("exit status 9"))
	if err == nil || !strings.Contains(err.Error(), "exit status 9") {
		t.Fatalf("nonzero Claude process accepted: result=%q session=%q err=%v", parsed, session, err)
	}
	if parsed != "" {
		t.Fatalf("failed Claude reviewer returned a success payload: %q", parsed)
	}
	if session != "safe-session" {
		t.Fatalf("safe diagnostic session lost on failed process: %q", session)
	}
	good, _, _, _, goodErr := parseClaudeSemanticResult(stream, nil)
	if goodErr != nil || good != "VERDICT_PASS" {
		t.Fatalf("normal successful result incorrectly blocked: result=%q err=%v", good, goodErr)
	}
}

func TestN130ClaudeReviewerAbsentAndIsErrorAreNotPASS(t *testing.T) {
	cases := []struct {
		label, raw string
		processErr error
	}{
		{"no_result", "{\"type\":\"system\",\"message\":\"ready\"}\n", nil},
		{"process_error_no_result", "", errors.New("exit status 1")},
		{"is_error", "{\"type\":\"result\",\"is_error\":true,\"result\":\"review failed\"}\n", nil},
		{"invalid_result", "garbage\n", nil},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			result, _, _, _, err := parseClaudeSemanticResult(tc.raw, tc.processErr)
			if err == nil || result != "" {
				t.Fatalf("unproven Claude run promoted: result=%q err=%v", result, err)
			}
		})
	}
}
