package main

import (
	"regexp"
	"strings"
)

// A reviewer narrative is not an authority to claim machine execution.
// Accept only four narrow, affirmed native feedback instruction templates.
// All other text remains an uncredited, informational learned procedure.
var nativeFeedbackNegativeWord = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(?:not|no|never|neither|nor|avoid|without|refrain|skip|omit|prevent|prohibit|cannot|can't|don't|shouldn't|mustn't|forbid|instead)([^a-z0-9_]|$)`)

func normalizeNativeFeedbackStep(raw string) string {
	lower := strings.ToLower(strings.ReplaceAll(raw, string(rune(96)), ""))
	line := strings.Join(strings.Fields(strings.TrimSpace(lower)), " ")
	return strings.TrimSuffix(line, ".")
}

func nativePositiveFeedbackStep(raw string) bool {
	line := normalizeNativeFeedbackStep(raw)
	if len(line) == 0 || len(line) > 350 || nativeFeedbackNegativeWord.MatchString(line) {
		return false
	}
	switch line {
	case "report the defect with the native air-worker feedback add command, using a stable -run-id and bounded -data-file json containing the exact error":
		return true
	case "call air-worker feedback add with a stable -run-id, correct product and real error evidence":
		return true
	case "report the defect with native feedback path and a stable run id":
		return true
	case "submit any defect through the native feedback path with a stable run id":
		return true
	}
	return false
}

// A negated instruction about the command/preceding action anywhere in the
// real Procedure section voids the positive line. Unrelated cautions about
// evidence (e.g. "loading is not using") do not void a valid positive step.
func nativeFeedbackProcedureContradictory(steps []string) bool {
	// Fail closed on ANY visible negative or ambiguous instruction within
	// the actual Procedure, not a finite blacklist of referents. "it",
	// "preceding step", "that operation", and split instructions must never
	// become a bypass for machine use attribution.
	//
	// Only the exact previously observed harmless caution in the genuine
	// reviewer lesson is exempt. It describes the loaded-vs-used boundary,
	// and does not negate or modify the native feedback action.
	const harmless = "4. select the exact sha-pinned procedure deliberately in the next session and verify its effect independently; loading a skill alone does not prove it was used"
	for _, raw := range steps {
		line := normalizeNativeFeedbackStep(raw)
		if !nativeFeedbackNegativeWord.MatchString(line) {
			continue
		}
		if line == harmless {
			continue
		}
		return true
	}
	return false
}
