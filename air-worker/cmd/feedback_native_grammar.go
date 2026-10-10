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
	for _, raw := range steps {
		line := normalizeNativeFeedbackStep(raw)
		if !nativeFeedbackNegativeWord.MatchString(line) {
			continue
		}
		for _, subject := range []string{
			"air-worker feedback add", "native feedback path",
			"preceding command", "previous command", "above command",
			"the command", "that command", "this command",
			"preceding action", "previous action", "above action",
			"that action", "this action",
			"execute", "invoke", "run the", "call the", "perform the",
			"step 1", "step 2",
		} {
			if strings.Contains(line, subject) {
				return true
			}
		}
	}
	return false
}
