package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Release status transitions are always core-owned, byte-exact and restricted
// to the next contiguous step. These are disposable plan copies only.
func TestR03PlanTransitionByteExactAndOneOpenStep(t *testing.T) {
	raw, err := n042HistoricalR02SourceBytes()
	if err != nil {
		t.Fatal(err)
	}
	r03 := expectedReleaseSliceR02ToR03(raw)
	if r03 == nil {
		t.Fatal("native R02->R03 transition fixture missing")
	}
	r04 := expectedReleaseSliceR03ToR04(r03)
	if r04 == nil || bytes.Equal(r04, r03) {
		t.Fatal("native release kernel cannot advance exact R03->R04 status")
	}
	if bytes.Equal(r04, raw) || !bytes.Equal(expectedReleaseSliceR03ToR02(r03), raw) {
		t.Fatal("R03 current state does not reconstruct the exact R02-before plan")
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plan", "release-slices.json"), r04, 0600); err != nil {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(dir)
	if len(state.Problems) != 0 || state.NextStep != "R0.11.9-04" ||
		state.ActiveStep.Status != "open" || state.ActiveStep.ID != "R0.11.9-04" {
		t.Fatalf("invalid R03 transition result: %+v", state)
	}
	original := map[string]any{}
	changed := map[string]any{}
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(r04, &changed); err != nil {
		t.Fatal(err)
	}
	origStatus := []string{}
	newStatus := []string{}
	for _, re := range original["releases"].([]any) {
		for _, s := range re.(map[string]any)["steps"].([]any) {
			item := s.(map[string]any)
			if strings.HasPrefix(item["id"].(string), "R0.11.9-") {
				origStatus = append(origStatus, item["id"].(string)+":"+item["status"].(string))
			}
		}
	}
	for _, re := range changed["releases"].([]any) {
		for _, s := range re.(map[string]any)["steps"].([]any) {
			item := s.(map[string]any)
			if strings.HasPrefix(item["id"].(string), "R0.11.9-") {
				newStatus = append(newStatus, item["id"].(string)+":"+item["status"].(string))
			}
		}
	}
	if len(origStatus) != len(newStatus) {
		t.Fatalf("release step identities changed %v -> %v", origStatus, newStatus)
	}
}

func TestR03PlanTransitionFailClosedOnWrongStatusAndAmbiguousIDs(t *testing.T) {
	raw, err := n042HistoricalR02SourceBytes()
	if err != nil {
		t.Fatal(err)
	}
	if expectedReleaseSliceR03ToR04(raw) != nil {
		t.Fatal("R03 advance on an uncompleted R02 plan")
	}
	r03 := expectedReleaseSliceR02ToR03(raw)
	if r03 == nil {
		t.Fatal("missing signed predecessor fixture")
	}
	if expectedReleaseSliceR03ToR04(append(append([]byte(nil), r03...), r03...)) != nil {
		t.Fatal("duplicated release-slices plan accepted")
	}
	malformed := regexp.MustCompile(`"id"\s*:\s*"R0\.11\.9-04"`).ReplaceAll(r03, []byte(`"id": "R0.11.9-40"`))
	if bytes.Equal(malformed, r03) {
		t.Fatal("wrong-ID fixture did not change canonical bytes")
	}
	if expectedReleaseSliceR03ToR04(malformed) != nil {
		t.Fatal("unknown release step ID authorized a transition")
	}
	if expectedReleaseSliceR03ToR02(raw) != nil {
		t.Fatal("R02 original state incorrectly inverted")
	}
}

func TestR03SourceOnlyRunnerRequiresExactActiveR03(t *testing.T) {
	state := releaseSlicesSummary{Found: true, ActiveVersion: "0.11.9",
		HasActiveStep: true, NextStep: "R0.11.9-03",
		ActiveStep: releaseStep{ID: "R0.11.9-03", Status: "open", Kind: "source"}}
	if err := releaseSliceLoopSupported(state, "0.11.9"); err != nil {
		t.Fatalf("source-only native R03 should be a recognized next slice: %v", err)
	}
	state.ActiveStep.Status = "planned"
	if err := releaseSliceLoopSupported(state, "0.11.9"); err == nil {
		t.Fatal("planned R03 started without native R02 close")
	}
	state.ActiveStep.Status = "open"
	state.NextStep = "R0.11.9-04"
	if err := releaseSliceLoopSupported(state, "0.11.9"); err == nil {
		t.Fatal("R04 selected before its source-supported kernel")
	}
}

func TestCmdLoopRoutesR03OnlyAfterNativeR02StatusTransition(t *testing.T) {
	root := releaseSliceLoopFixture(t)
	path := filepath.Join(root, "plan", "release-slices.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	next := expectedReleaseSliceR02ToR03(raw)
	if next == nil {
		t.Fatal("R02 status transition no longer supported")
	}
	if err := os.WriteFile(path, next, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	gotRC := cmdLoopWithReleaseSliceRunner([]string{"-product", root},
		func(actual string, _ runConfig, state releaseSlicesSummary) int {
			calls++
			if actual != root || state.NextStep != nativeReleaseSliceR03 ||
				!state.HasActiveStep || state.ActiveStep.ID != nativeReleaseSliceR03 ||
				state.ActiveStep.Status != "open" {
				t.Fatalf("R03 native runner dispatch mismatched machine plan: %+v", state)
			}
			return 19
		})
	if gotRC != 19 || calls != 1 {
		t.Fatalf("R03 was skipped or dispatched twice: rc=%d calls=%d", gotRC, calls)
	}
	observed, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(observed, next) {
		t.Fatal("R03 dispatch mutated release plan outside native core")
	}
}
