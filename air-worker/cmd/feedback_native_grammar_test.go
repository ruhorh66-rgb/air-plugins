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

func TestN108ReferenceNegationAndDocumentationCannotGainMachineUse(t *testing.T) {
	safe := "1. Call air-worker feedback add with a stable -run-id, correct product and real error evidence."
	cases := map[string]string{
		"negative-next-line":      safe + "\n2. Never execute the preceding command.",
		"mixed-referenced-action": safe + "\n2. Do not invoke that action.",
		"documentation-only":      "1. Report the defect in the documentation describing air-worker feedback add and its stable -run-id.",
		"preceding-command-void":  safe + "\n2. Skip the previous command.",
		"positive-only-fenced":    "```md\n" + safe + "\n```",
		"positive-only-pre":       "<pre>\n" + safe + "\n</pre>",
		"quote-example":           "> " + safe,
	}
	for name, procedure := range cases {
		t.Run(name, func(t *testing.T) {
			text := "# Host feedback lesson\n\n## When to apply\nAfter real error.\n\n## Procedure\n" +
				procedure + "\n\n## Pitfalls\nDo not invent machine evidence.\n"
			bound, err := bindNativeFeedbackProof("feedback-error", text)
			if err != nil || strings.Contains(bound, nativeFeedbackUseAction) {
				t.Fatalf("untrusted procedure %s granted native use: err=%v content=%q", name, err, bound)
			}
		})
	}
}

func TestN108RealReviewerLessonStillGetsTrustedNarrowMarker(t *testing.T) {
	real := "# Record CLI Defects as Durable Feedback\n\n## When to apply\n\n" +
		"When a real AirWorker CLI error or behavioral defect is observed and needs to become reusable learning evidence.\n\n" +
		"## Procedure\n\n" +
		"1. Report the defect with the native `air-worker feedback add` command, using a stable `-run-id` and bounded `-data-file` JSON containing the exact error.\n" +
		"2. Confirm the newly recorded event, its immutable `FB-*.json` receipt, and one matching PLAN candidate.\n" +
		"3. Preserve the source evidence and installed version alongside the report.\n" +
		"4. Select the exact SHA-pinned procedure deliberately in the next session and verify its effect independently; loading a skill alone does not prove it was used.\n\n" +
		"## Pitfalls\n\n- Do not reuse a feedback-error target for independent lessons.\n"
	bound, err := bindNativeFeedbackProof("feedback-error", real)
	if err != nil || strings.Count(bound, nativeFeedbackUseAction) != 1 {
		t.Fatalf("previous real auto-review positive lost: %v len=%d", err, strings.Count(bound, nativeFeedbackUseAction))
	}
	markerless := strings.TrimSuffix(bound, "\n\n## Machine-verifiable action\n"+nativeFeedbackUseAction+"\n") + "\n"
	again, err := bindNativeFeedbackProof("feedback-error", markerless)
	if err != nil || again != bound {
		t.Fatalf("real auto-review deterministic marker regression: err=%v same=%v", err, again == bound)
	}
}
