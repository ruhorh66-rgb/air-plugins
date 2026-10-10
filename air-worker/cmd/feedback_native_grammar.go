package main

import (
	"regexp"
	"strings"
)

// A learned paragraph cannot authorize an execution claim. This deliberately
// limited positive grammar only classifies an unambiguous imperative about the
// one native AirWorker feedback API, and its result is still checked against
// the original auto-review, ledger, explicit selection and machine receipts.
var unsafeNativeFeedbackInstruction = regexp.MustCompile(`(?i)(^|[^a-z0-9_])(?:not|no|never|neither|nor|avoid|without|refrain|skip|omit|prevent|prohibit|cannot|can't|don't|shouldn't|mustn't|forbid|instead)([^a-z0-9_]|$)`)

func nativePositiveFeedbackStep(raw string) bool {
	step := strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(raw)), " "))
	if step == "" || len(step) > 300 ||
		strings.ContainsAny(step, `;"'`) ||
		unsafeNativeFeedbackInstruction.MatchString(step) {
		return false
	}
	words := strings.Fields(step)
	if len(words) < 5 {
		return false
	}
	verb := words[0]
	switch verb {
	case "call":
		if !(strings.HasPrefix(step, "call air-worker feedback add ") ||
			strings.HasPrefix(step, "call the native air-worker feedback add ") ||
			strings.HasPrefix(step, "call the air-worker feedback add ")) {
			return false
		}
	case "submit", "report", "record", "file":
		// "Record that the call must not be made" is not a command.
		i := 1
		if words[i] == "the" || words[i] == "a" || words[i] == "an" || words[i] == "any" {
			i++
		}
		if i >= len(words) {
			return false
		}
		switch words[i] {
		case "defect", "error", "issue", "feedback", "failure", "cli":
		default:
			return false
		}
	default:
		return false
	}
	hasAPI := strings.Contains(step, "air-worker feedback add") ||
		strings.Contains(step, "native feedback path")
	stable := strings.Contains(step, "-run-id") ||
		strings.Contains(step, "stable run id") ||
		strings.Contains(step, "stable run ")
	return hasAPI && stable
}

// A second, contradictory procedure line invalidates even a positive line.
// Only one unambiguous supported native action may be credited by the binary.
func nativeFeedbackProcedureContradictory(rawProcedure string) bool {
	for _, line := range strings.Split(rawProcedure, string(byte(10))) {
		step := strings.TrimSpace(strings.ReplaceAll(strings.ToLower(line), "`", ""))
		if (strings.Contains(step, "air-worker feedback add") || strings.Contains(step, "native feedback path")) &&
			unsafeNativeFeedbackInstruction.MatchString(step) {
			return true
		}
	}
	return false
}
