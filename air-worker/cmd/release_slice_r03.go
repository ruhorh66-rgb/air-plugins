package main

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
)

// A release-slice state transition has exactly three authoritative status
// tokens: R02, R03, R04. Refuse to edit malformed or ambiguous JSON and
// preserve ALL other bytes (including the approved LPR scope and ownership).
var r03StatusFields = regexp.MustCompile(`("id"\s*:\s*"(R0\.11\.9-0[234])"\s*,\s*"status"\s*:\s*")(closed|open|planned)(")`)

func transformR03SliceStatuses(raw []byte, expected, next map[string]string) []byte {
	matches := r03StatusFields.FindAllSubmatchIndex(raw, -1)
	if len(matches) != 3 {
		return nil
	}
	found := map[string]bool{}
	replacements := map[int]string{}
	for _, m := range matches {
		id := string(raw[m[4]:m[5]])
		previous := string(raw[m[6]:m[7]])
		if found[id] || expected[id] == "" || expected[id] != previous || next[id] == "" {
			return nil
		}
		found[id] = true
		replacements[m[6]] = next[id]
	}
	if len(found) != 3 {
		return nil
	}
	out := append([]byte(nil), raw...)
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		replace := replacements[m[6]]
		out = append(append(append([]byte(nil), out[:m[6]]...), []byte(replace)...), out[m[7]:]...)
	}
	if bytes.Equal(out, raw) {
		return nil
	}
	return out
}

func expectedReleaseSliceR03ToR04(raw []byte) []byte {
	return transformR03SliceStatuses(raw,
		map[string]string{"R0.11.9-02": "closed", "R0.11.9-03": "open", "R0.11.9-04": "planned"},
		map[string]string{"R0.11.9-02": "closed", "R0.11.9-03": "closed", "R0.11.9-04": "open"})
}

// The inverse is READ-ONLY. It authenticates that R03's source plan is exactly
// the original R02 plan plus the two core-controlled status transitions.
func expectedReleaseSliceR03ToR02(raw []byte) []byte {
	return transformR03SliceStatuses(raw,
		map[string]string{"R0.11.9-02": "closed", "R0.11.9-03": "open", "R0.11.9-04": "planned"},
		map[string]string{"R0.11.9-02": "open", "R0.11.9-03": "planned", "R0.11.9-04": "planned"})
}

func releaseR03PlanSnapshot(root string, state releaseSlicesSummary, raw []byte) ([]byte, error) {
	if state.NextStep != "R0.11.9-03" || !state.HasActiveStep ||
		state.ActiveStep.ID != "R0.11.9-03" || state.ActiveStep.Status != "open" ||
		state.ActiveStep.Kind != "source" || state.ActiveVersion != "0.11.9" ||
		len(state.Problems) != 0 {
		return nil, errors.New("R03 is not the current open native source slice")
	}
	before := expectedReleaseSliceR03ToR02(raw)
	if before == nil || expectedReleaseSliceR03ToR04(raw) == nil {
		return nil, fmt.Errorf("R03 status bytes are not the canonical R02-authenticated intermediate state for %s", root)
	}
	return before, nil
}
