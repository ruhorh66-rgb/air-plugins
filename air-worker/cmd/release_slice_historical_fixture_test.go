package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"testing"
)

// Historical release transition tests must not read whatever the ACTIVE
// operational release checkpoint happens to be today. In 0.11.9 the real
// product advanced R02-R04 and is now at R05; replay-security tests still
// require the genuine, byte-preserving R02-before test scenario.
//
// This routine changes only bytes in the caller's t.TempDir fixture. It
// refuses unknown versions, absent/duplicate status tokens or a drifted
// operational source. No product PLAN, plan/N-* or LEARN journal is modified.
var n042HistoricalR02Status = regexp.MustCompile(`("id"\s*:\s*"(R0\.11\.9-0[2345])"\s*,\s*"status"\s*:\s*")(closed|open|planned)(")`)

func n042HistoricalR02SourceBytes() ([]byte, error) {
	raw, err := os.ReadFile("../plan/release-slices.json")
	if err != nil {
		return nil, err
	}
	matches := n042HistoricalR02Status.FindAllSubmatchIndex(raw, -1)
	if len(matches) != 4 {
		return nil, fmt.Errorf("N042 historical transition fixture: found %d status tokens, expected four", len(matches))
	}
	// Exact current source checkpoint is R05; all earlier finished
	// signed statuses are preserved in the canonical product plan.
	expected := map[string]string{
		"R0.11.9-02": "closed",
		"R0.11.9-03": "closed",
		"R0.11.9-04": "closed",
		"R0.11.9-05": "open",
	}
	historical := map[string]string{
		"R0.11.9-02": "open",
		"R0.11.9-03": "planned",
		"R0.11.9-04": "planned",
		"R0.11.9-05": "planned",
	}
	seen := map[string]bool{}
	out := append([]byte(nil), raw...)
	for i := len(matches) - 1; i >= 0; i-- {
		m := matches[i]
		id := string(raw[m[4]:m[5]])
		status := string(raw[m[6]:m[7]])
		if seen[id] || expected[id] == "" || status != expected[id] {
			return nil, fmt.Errorf("N042 actual R05 source diverged for %s status %q", id, status)
		}
		seen[id] = true
		out = append(append(append([]byte(nil), out[:m[6]]...), []byte(historical[id])...), out[m[7]:]...)
	}
	if len(seen) != 4 || bytes.Equal(out, raw) {
		return nil, errors.New("N042 historical fixture did not produce exact distinct R02 checkpoint")
	}
	return out, nil
}

func TestN042HistoricalR02FixtureIsPureAndOnlyRewindsTestState(t *testing.T) {
	before, err := os.ReadFile("../plan/release-slices.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := n042HistoricalR02SourceBytes()
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile("../plan/release-slices.json")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("N042 test fixture mutated canonical release plan: %v", err)
	}
	if expectedReleaseSliceR02ToR03(fixture) == nil {
		t.Fatal("R02 historical fixture cannot advance to R03 with canonical native rules")
	}
	changed := 0
	for _, m := range n042HistoricalR02Status.FindAllSubmatchIndex(fixture, -1) {
		if string(fixture[m[6]:m[7]]) == "open" {
			changed++
			if string(fixture[m[4]:m[5]]) != "R0.11.9-02" {
				t.Fatal("wrong active release slice in synthetic historical fixture")
			}
		}
	}
	if changed != 1 {
		t.Fatalf("historical R02 fixture has %d open release slices, want 1", changed)
	}
}
