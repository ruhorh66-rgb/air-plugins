package main

import (
	"fmt"
	"strings"
)

type stepFactualState struct {
	Code    int
	Text    string
	Passed  []string
	Failed  []string
	Unknown []string
}

func criterionEntry(entries []string, id string) (string, bool) {
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == id || strings.HasPrefix(entry, id+" — ") {
			return entry, true
		}
	}
	return "", false
}

func stepFactualVerdict(step workStep, result *judgeResult) stepFactualState {
	ids := stepCriteria(step)
	if len(ids) == 0 {
		return stepFactualState{Code: 2, Text: "STEP NOT_PROVEN: no criterion is bound to this work step"}
	}
	if result == nil {
		return stepFactualState{Code: 2, Text: "STEP NOT_PROVEN: structured factual result is unavailable", Unknown: ids}
	}
	out := stepFactualState{}
	for _, id := range ids {
		if _, ok := criterionEntry(result.CriteriaPassed, id); ok {
			out.Passed = append(out.Passed, id)
		} else if entry, ok := criterionEntry(result.CriteriaFailed, id); ok {
			out.Failed = append(out.Failed, entry)
		} else if entry, ok := criterionEntry(result.CriteriaUnknown, id); ok {
			out.Unknown = append(out.Unknown, entry)
		} else {
			out.Unknown = append(out.Unknown, id+" — criterion was not evaluated")
		}
	}
	if len(out.Unknown) > 0 {
		out.Code = 2
		out.Text = "STEP NOT_PROVEN: " + strings.Join(out.Unknown, "; ")
	} else if len(out.Failed) > 0 {
		out.Code = 1
		out.Text = "STEP FAIL: " + strings.Join(out.Failed, "; ")
	} else {
		out.Code = 0
		out.Text = fmt.Sprintf("STEP PASS: %s", strings.Join(out.Passed, ", "))
	}
	return out
}
