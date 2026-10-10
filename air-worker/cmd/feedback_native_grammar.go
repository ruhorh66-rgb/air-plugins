package main

import "strings"

// A native-feedback use receipt does NOT attest arbitrary generated prose.
// The only eligible source is one of these complete, release-owned,
// positively supported safe Procedure templates. Everything else remains
// a learned informational skill; even one extra sentence fails closed.
// This is intentionally stricter than looking for negative words, which
// cannot safely interpret Unicode, pronouns or split continuations.
func normalizeNativeFeedbackStep(raw string) string {
	lower := strings.ToLower(strings.ReplaceAll(raw, string(rune(96)), ""))
	return strings.TrimSuffix(strings.Join(strings.Fields(strings.TrimSpace(lower)), " "), ".")
}

const n108StepReportFull = "report the defect with the native air-worker feedback add command, using a stable -run-id and bounded -data-file json containing the exact error"
const n108StepCall = "call air-worker feedback add with a stable -run-id, correct product and real error evidence"
const n108StepReportShort = "report the defect with native feedback path and a stable run id"
const n108StepSubmit = "submit any defect through the native feedback path with a stable run id"

// A finite typed allowlist is the boundary for machine evidence, NOT the
// vocabulary of procedures the reviewer may learn and auto-apply as notes.
func nativeFeedbackProcedureApproved(steps []string) bool {
	var got []string
	for _, raw := range steps {
		s := normalizeNativeFeedbackStep(raw)
		if s != "" {
			got = append(got, s)
		}
	}
	if len(got) == 0 || len(got) > 4 {
		return false
	}
	allowed := [][]string{
		{"1. " + n108StepReportFull},
		{"2. " + n108StepCall},
		{"1. " + n108StepReportShort},
		{"1. " + n108StepSubmit},
		// Actual auto-reviewed AirWorker 0.11.9 lesson. A new arbitrary
		// sentence cannot silently alter its native contract.
		{"1. " + n108StepReportFull,
			"2. confirm the newly recorded event, its immutable fb-*.json receipt, and one matching plan candidate",
			"3. preserve the source evidence and installed version alongside the report",
			"4. select the exact sha-pinned procedure deliberately in the next session and verify its effect independently; loading a skill alone does not prove it was used"},
		// Release-owned positive fixture verifies isolated native use.
		{"1. select this learned skill by its exact current target@sha",
			"2. " + n108StepCall,
			"3. require the native event, immutable feedback receipt, and one plan candidate"},
		// Historical reviewer fixture in N-104 regression.
		{"1. inspect the exact installed binary version",
			"2. " + n108StepSubmit,
			"3. compare source event, receipt and plan candidate"},
	}
	for _, template := range allowed {
		if len(template) != len(got) {
			continue
		}
		match := true
		for i := range template {
			if got[i] != template[i] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// Retained for diagnostics/tests of exact individual positive actions.
// Positive wording alone is NEVER sufficient: the full Procedure must
// match nativeFeedbackProcedureApproved.
func nativePositiveFeedbackStep(raw string) bool {
	s := normalizeNativeFeedbackStep(raw)
	return s == n108StepReportFull || s == n108StepCall ||
		s == n108StepReportShort || s == n108StepSubmit
}
