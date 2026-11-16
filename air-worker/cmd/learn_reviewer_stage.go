package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// An unknown reviewer failure used to collapse into one withheld reason, so
// the journal could not tell a settings error from a model error. The stage
// is a fixed code from this list; the error text itself stays withheld.
var reviewerStages = map[string]bool{
	"product": true, "settings": true, "input": true,
	"catalog": true, "invoke": true, "answer": true,
}

// Stages that usually fail the same way on an immediate retry of the same
// event, because they depend on configuration or the applied catalogue rather
// than on the model call. Reading stdin is excluded: it can fail transiently.
var deterministicReviewerStages = map[string]bool{
	"product": true, "settings": true, "catalog": true,
}

// A deterministic failure is not retried at once, but it is not dropped
// either: a repaired configuration or a changed catalogue makes the same event
// reviewable again, and the stored review carries no configuration identity to
// prove that. One later retry after this delay keeps both properties; one
// hour removes the burst of immediate retries while delaying a transient
// failure only briefly (curator decision 15.11.2026).
const deterministicReviewerRetryDelay = time.Hour

func reviewerStageFailure(stage string, err error) {
	if !reviewerStages[stage] {
		stage = ""
	}
	if stage == "" {
		fmt.Fprintln(os.Stderr, "learning reviewer:", err)
		return
	}
	fmt.Fprintf(os.Stderr, "learning reviewer: stage=%s: %v\n", stage, err)
}

func cutReviewerStage(message string) (string, string) {
	rest, ok := strings.CutPrefix(message, "stage=")
	if !ok {
		return "", message
	}
	stage, tail, ok := strings.Cut(rest, ": ")
	if !ok || !reviewerStages[stage] {
		return "", message
	}
	return stage, tail
}

// storedReviewerReason extracts the fixed reason code from the exact error
// shape runLearningProcess stores: "learning adapter failed (<code>): <exit>"
// or "learning adapter failed (<code>; detail withheld): <exit>". Any other
// shape yields "" and is treated as unclassified.
func storedReviewerReason(reviewError string) string {
	inner, ok := strings.CutPrefix(reviewError, "learning adapter failed (")
	if !ok {
		return ""
	}
	inner, exit, ok := strings.Cut(inner, "): ")
	if !ok || !strings.HasPrefix(exit, "exit status ") {
		return ""
	}
	if code, suffix, withheld := strings.Cut(inner, "; "); withheld {
		if suffix != "detail withheld" {
			return ""
		}
		inner = code
	}
	if !strings.HasPrefix(inner, "reviewer_") || strings.Trim(inner, "abcdefghijklmnopqrstuvwxyz_") != "" {
		return ""
	}
	return inner
}

// reviewerFailureIsDeterministic reads the reason the module stored for a
// completed review error. Only fixed reason codes are trusted; anything else,
// including an invoke failure or a withheld detail, may still be retried.
func reviewerFailureIsDeterministic(reviewError string) bool {
	reason := storedReviewerReason(reviewError)
	switch reason {
	case "":
		return false
	case "reviewer_product_missing", "reviewer_learning_disabled", "reviewer_input_too_large",
		"reviewer_invalid_input", "reviewer_event_missing", "reviewer_context_limit":
		return true
	}
	if s, ok := strings.CutSuffix(strings.TrimPrefix(reason, "reviewer_"), "_failed"); ok {
		return deterministicReviewerStages[s]
	}
	return false
}

// reviewerRetryDue: transient failures are offered again at once, a
// deterministic one only after deterministicReviewerRetryDelay. An unreadable
// timestamp, or one in the future (clock skew or correction), keeps the old
// behaviour (retry) rather than hiding the event.
func reviewerRetryDue(reviewError, at string, now time.Time) bool {
	if !reviewerFailureIsDeterministic(reviewError) {
		return true
	}
	reviewed, err := time.Parse(time.RFC3339Nano, at)
	if err != nil || reviewed.After(now) {
		return true
	}
	return now.Sub(reviewed) >= deterministicReviewerRetryDelay
}
