package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// Historical R03/R04 integration belongs to the pinned 0.11.9/v0.1.0
// source. Current 0.11.10/v0.1.1 must reject those original release
// checkpoints BEFORE any reviewer or measured-use code may run.
func TestN044CurrentConsumerRejectsHistoricalR03R04ReleaseWork(t *testing.T) {
	if version != "0.11.10" || learning.Version != "0.1.1" {
		t.Fatalf("N044 wrong consumer identity: air-worker=%s learning=%s", version, learning.Version)
	}
	r02, err := n042HistoricalR02SourceBytes()
	if err != nil {
		t.Fatal(err)
	}
	r03 := expectedReleaseSliceR02ToR03(r02)
	r04 := expectedReleaseSliceR03ToR04(r03)
	if r03 == nil || r04 == nil {
		t.Fatal("historical release fixture cannot reconstruct signed R03/R04 states")
	}
	for _, tc := range []struct {
		name string
		step string
		plan []byte
	}{
		{name: "r03", step: nativeReleaseSliceR03, plan: r03},
		{name: "r04", step: nativeReleaseSliceR04, plan: r04},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "plan"), 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(root, "plan", "release-slices.json")
			if err := os.WriteFile(path, tc.plan, 0600); err != nil {
				t.Fatal(err)
			}
			state := inspectReleaseSlices(root)
			if len(state.Problems) != 0 || state.NextStep != tc.step || !state.HasActiveStep {
				t.Fatalf("historical state is not a valid negative fixture: %+v", state)
			}
			if err := releaseSliceLoopSupported(state, version); err == nil || !strings.Contains(err.Error(), "exact 0.11.9 candidate") {
				t.Fatalf("legacy release step was not denied by exact version guard: %v", err)
			}
			if rc := cmdReleaseSliceLoop(root, runConfig{}, state); rc != 2 {
				t.Fatalf("legacy runner executed under 0.11.10, rc=%d", rc)
			}
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, tc.plan) {
				t.Fatalf("legacy mismatch changed release state: %v", err)
			}
			if _, err := os.Stat(filepath.Join(root, "docs", "receipts")); !os.IsNotExist(err) {
				t.Fatalf("version-rejected legacy runner persisted evidence: %v", err)
			}
		})
	}
}