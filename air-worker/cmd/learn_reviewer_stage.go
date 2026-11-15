package main

import (
	"fmt"
	"os"
	"strings"
)

// An unknown reviewer failure used to collapse into one withheld reason, so
// the journal could not tell a settings error from a model error. The stage
// is a fixed code from this list; the error text itself stays withheld.
var reviewerStages = map[string]bool{
	"product": true, "settings": true, "input": true,
	"catalog": true, "invoke": true, "answer": true,
}

// Stages that fail the same way on every retry for the same event and
// configuration: retrying them only multiplies the load.
var deterministicReviewerStages = map[string]bool{
	"product": true, "settings": true, "input": true, "catalog": true,
}

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

// reviewerFailureIsDeterministic reads the reason the module stored for a
// completed review error. Only fixed reason codes are trusted; anything else,
// including an invoke failure or a withheld detail, may still be retried.
func reviewerFailureIsDeterministic(reviewError string) bool {
	start := strings.Index(reviewError, "(")
	if start < 0 {
		return false
	}
	end := strings.IndexAny(reviewError[start+1:], ");")
	if end < 0 {
		return false
	}
	reason := reviewError[start+1 : start+1+end]
	switch reason {
	case "reviewer_product_missing", "reviewer_learning_disabled", "reviewer_input_too_large",
		"reviewer_invalid_input", "reviewer_event_missing", "reviewer_context_limit":
		return true
	}
	if stage, ok := strings.CutPrefix(reason, "reviewer_"); ok {
		if s, ok := strings.CutSuffix(stage, "_failed"); ok {
			return deterministicReviewerStages[s]
		}
	}
	return false
}
