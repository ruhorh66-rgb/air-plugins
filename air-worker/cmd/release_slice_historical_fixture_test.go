package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/ruhorh66-rgb/air-modules/learning"
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
// archival source. No product PLAN, plan/N-* or LEARN journal is modified.
var n042HistoricalR02Status = regexp.MustCompile(`("id"\s*:\s*"(R0\.11\.9-0[2345])"\s*,\s*"status"\s*:\s*")(closed|open|planned)(")`)

func n042HistoricalR02SourceBytes() ([]byte, error) {
	raw, err := os.ReadFile("testdata/release-slices-pre-n124-cancel.json")
	if err != nil {
		return nil, err
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	if sum := fmt.Sprintf("%x", sha256.Sum256(raw)); sum != "4914785397401d3dc314e3d77c89a9c8220d91b8a54a2cabdbf866a91a533881" {
		return nil, fmt.Errorf("N042 historical source SHA-256 mismatch: %s", sum)
	}
	matches := n042HistoricalR02Status.FindAllSubmatchIndex(raw, -1)
	if len(matches) != 4 {
		return nil, fmt.Errorf("N042 historical transition fixture: found %d status tokens, expected four", len(matches))
	}
	// The pinned source checkpoint is R05 with earlier signed statuses preserved.
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

// n044HistoricalR03R04PositiveOnly makes the version-dependent test contract
// explicit: the original R03/R04 signed positive path belongs to 0.11.9/v0.1.0,
// which is validated on a separate frozen source/QA lane. New consumers prove
// an exact fail-closed refusal here; they never impersonate the old binary.
func n044HistoricalR03R04PositiveOnly(t *testing.T) bool {
	t.Helper()
	if version == "0.11.9" && learning.Version == "0.1.0" {
		return true
	}
	// Preserve the original signed-positive fixture only for 0.11.9/v0.1.0.
	// Known 0.11.11 consumers (including rc.4) must fail closed before R03/R04
	// instead of executing a historical release under a newer binary.
	knownNewConsumer := version == "0.11.11-rc.2" || version == "0.11.11-rc.3" || version == "0.11.11-rc.4" || version == "0.11.11-rc.5"
	if !knownNewConsumer || learning.Version != "0.1.1" {
		t.Fatalf("historical R03/R04 test has unknown version pair: air-worker=%s learning=%s", version, learning.Version)
	}
	for _, step := range []struct {
		id   string
		kind string
	}{
		{id: nativeReleaseSliceR03, kind: "source"},
		{id: nativeReleaseSliceR04, kind: "test"},
	} {
		state := releaseSlicesSummary{
			Found: true, ActiveVersion: "0.11.9", HasActiveStep: true,
			NextStep: step.id, ActiveStep: releaseStep{ID: step.id, Status: "open", Kind: step.kind},
		}
		if err := releaseSliceLoopSupported(state, version); err == nil {
			t.Fatalf("0.11.11-rc.2 executed historical %s despite wrong binary identity", step.id)
		}
	}
	t.Log("N044 exact-version gate PASS; historical signed positive suite must separately PASS on 0.11.9/v0.1.0")
	return false
}
