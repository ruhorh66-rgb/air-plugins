package main

import "testing"

func TestCriterion57StepFactualIgnoresFutureFailures(t *testing.T) {
	step := workStep{Num: "20", Judge: "К1: current step"}
	result := &judgeResult{
		CriteriaPassed: []string{"К1"},
		CriteriaFailed: []string{"К2 — future criterion failed"},
	}
	got := stepFactualVerdict(step, result)
	if got.Code != 0 || len(got.Passed) != 1 || got.Passed[0] != "К1" {
		t.Fatalf("future failure contaminated current step: %+v", got)
	}

	result.CriteriaPassed = nil
	result.CriteriaFailed = []string{"К1 — current criterion failed", "К2 — future failed"}
	got = stepFactualVerdict(step, result)
	if got.Code != 1 || len(got.Failed) != 1 {
		t.Fatalf("current failure not isolated: %+v", got)
	}
}

func TestCriterion57StepFactualUnknownIsNotProven(t *testing.T) {
	step := workStep{Num: "20", Judge: "К1: current step"}
	got := stepFactualVerdict(step, &judgeResult{CriteriaUnknown: []string{"К1 — test not written"}})
	if got.Code != 2 || len(got.Unknown) != 1 {
		t.Fatalf("unknown criterion must be NOT_PROVEN: %+v", got)
	}
}

func TestCriterion58StandaloneReviewerRouting(t *testing.T) {
	for _, tc := range []struct {
		executor string
		reviewer string
	}{
		{"claude", "codex"},
		{"codex", "claude"},
		{"chatgpt", "claude"},
	} {
		execSpec, err := semanticExecutorSpec(tc.executor)
		if err != nil {
			t.Fatalf("executor %s: %v", tc.executor, err)
		}
		reviewer, err := oppositeSemanticReviewer(execSpec)
		if err != nil || reviewer.Kind != tc.reviewer {
			t.Fatalf("%s reviewer = %+v, %v; want %s", tc.executor, reviewer, err, tc.reviewer)
		}
	}
}
