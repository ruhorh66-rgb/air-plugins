package main

import (
	"bytes"
	"errors"
	"regexp"
)

// R04 records one measured learned procedure use. Only statuses change; the
// release owner, LPR scope, other steps and the whole PLAN.md stay untouched.
var r04StatusFields = regexp.MustCompile(`("id"\s*:\s*"(R0\.11\.9-0[345])"\s*,\s*"status"\s*:\s*")(closed|open|planned)(")`)

func transformR04Statuses(raw []byte, expected, next map[string]string) []byte {
	matches := r04StatusFields.FindAllSubmatchIndex(raw, -1)
	if len(matches) != 3 {
		return nil
	}
	seen := map[string]bool{}
	out := append([]byte(nil), raw...)
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		id := string(raw[m[4]:m[5]])
		prev := string(raw[m[6]:m[7]])
		if seen[id] || expected[id] != prev || expected[id] == "" || next[id] == "" {
			return nil
		}
		seen[id] = true
		out = append(append(append([]byte{}, out[:m[6]]...), []byte(next[id])...), out[m[7]:]...)
	}
	if len(seen) != 3 || bytes.Equal(out, raw) {
		return nil
	}
	return out
}

func expectedReleaseSliceR04ToR05(raw []byte) []byte {
	return transformR04Statuses(raw,
		map[string]string{"R0.11.9-03": "closed", "R0.11.9-04": "open", "R0.11.9-05": "planned"},
		map[string]string{"R0.11.9-03": "closed", "R0.11.9-04": "closed", "R0.11.9-05": "open"})
}
func expectedReleaseSliceR04ToR03(raw []byte) []byte {
	return transformR04Statuses(raw,
		map[string]string{"R0.11.9-03": "closed", "R0.11.9-04": "open", "R0.11.9-05": "planned"},
		map[string]string{"R0.11.9-03": "open", "R0.11.9-04": "planned", "R0.11.9-05": "planned"})
}
func releaseR04State(root string, state releaseSlicesSummary, raw []byte) ([]byte, error) {
	if state.ActiveVersion != "0.11.9" || state.NextStep != nativeReleaseSliceR04 ||
		!state.HasActiveStep || state.ActiveStep.ID != nativeReleaseSliceR04 ||
		state.ActiveStep.Kind != "test" || state.ActiveStep.Status != "open" ||
		len(state.Problems) != 0 {
		return nil, errors.New("R04: native measured-use test is not current active release slice")
	}
	intermediate := expectedReleaseSliceR04ToR03(raw)
	if intermediate == nil || expectedReleaseSliceR04ToR05(raw) == nil ||
		expectedReleaseSliceR03ToR02(intermediate) == nil {
		return nil, errors.New("R04: status cannot reconstruct the exact signed R02/R03 history")
	}
	return intermediate, nil
}
