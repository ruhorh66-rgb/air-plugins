package main

import (
	"strings"
	"testing"
)

func TestN108NativeFeedbackRejectsNegatedGuidance(t *testing.T) {
	negatives := []string{
		"1. Record that air-worker feedback add must not be executed with a stable -run-id.",
		"1. Call neither air-worker feedback add nor the native feedback path with a stable -run-id.",
		"1. Avoid calling air-worker feedback add with a stable -run-id.",
		"1. Report the defect with air-worker feedback add and a stable -run-id; never execute it.",
		"1. Report the defect with native feedback path and stable run ID but do not publish.",
		"1. Report that air-worker feedback add with stable -run-id should be skipped.",
		"1. Call air-worker feedback add with a stable -run-id without recording any receipt.",
	}
	for _, line := range negatives {
		content := "# Dangerous guidance\n\n## When to apply\nAfter error.\n\n## Procedure\n" + line +
			"\n\n## Pitfalls\nDo not claim a fix.\n"
		bound, err := bindNativeFeedbackProof("feedback-error", content)
		if err != nil || strings.Contains(bound, nativeFeedbackUseAction) {
			t.Errorf("negative procedure awarded machine proof: %q err=%v", line, err)
		}
	}
}
func TestN108NativeFeedbackPositiveProcedureGrammar(t *testing.T) {
	positives := []string{
		"1. Report the defect with the native air-worker feedback add command, using a stable -run-id and bounded -data-file JSON containing the exact error.",
		"2. Call air-worker feedback add with a stable -run-id, correct product and real error evidence.",
		"1. Report the defect with native feedback path and a stable run ID.",
	}
	for _, line := range positives {
		body := "# Native safe feedback\n\n## When to apply\nAfter an actual failure.\n\n## Procedure\n" +
			line + "\n\n## Pitfalls\nDo not invent a successful fix.\n"
		result, err := bindNativeFeedbackProof("feedback-error", body)
		if err != nil || strings.Count(result, nativeFeedbackUseAction) != 1 {
			t.Errorf("safe positive procedural action not available for %q: %v", line, err)
		}
	}
}
func TestN108NativeFeedbackContradictionDeniesAction(t *testing.T) {
	body := "# Contradicted\n\n## When to apply\nWhen a tool fails.\n\n## Procedure\n" +
		"1. Report the defect with native feedback path and stable run ID.\n" +
		"2. Never call air-worker feedback add with a stable -run-id.\n\n" +
		"## Pitfalls\nDo not invent fixes.\n"
	bound, err := bindNativeFeedbackProof("feedback-error", body)
	if err != nil || strings.Contains(bound, nativeFeedbackUseAction) {
		t.Fatalf("conflicting procedure got native evidence: %v", err)
	}
}
