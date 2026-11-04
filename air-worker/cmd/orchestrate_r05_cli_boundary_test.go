package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Guard the real user entrypoint, not just releaseSliceLoopSupported.
// Native orchestration must not silently launch Codex agents from obsolete
// numeric PLAN rows when historical R05 remains active and unsupported.
func TestOrchestrateCLIRefusesHistoricalR05WithoutAgentOrPlanMutation(t *testing.T) {
	root := releaseSliceLoopFixture(t)
	sidecarPath := filepath.Join(root, "plan", "release-slices.json")
	raw, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	var plan map[string]any
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	releases := plan["releases"].([]any)
	steps := releases[0].(map[string]any)["steps"].([]any)
	for i := 1; i <= 3; i++ {
		steps[i].(map[string]any)["status"] = "closed"
	}
	steps[4].(map[string]any)["status"] = "open"
	updated, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sidecarPath, updated, 0o600); err != nil {
		t.Fatal(err)
	}
	state := inspectReleaseSlices(root)
	if !state.Found || len(state.Problems) != 0 || !state.HasActiveStep ||
		state.NextStep != "R0.11.9-05" {
		t.Fatalf("fixture is not exact valid historical R05: %+v", state)
	}
	beforeSidecar, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, "PLAN.md")
	beforePlan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	code := cmdOrchestrate([]string{
		"-product", root, "-orchestrate", "-subagents", "2", "-whatif",
	})
	if code != 2 {
		t.Fatalf("unsupported historical R05 returned rc=%d, want refusal rc2", code)
	}
	afterSidecar, err := os.ReadFile(sidecarPath)
	if err != nil || !bytes.Equal(beforeSidecar, afterSidecar) {
		t.Fatalf("orchestrate mutated historical release plan: %v", err)
	}
	afterPlan, err := os.ReadFile(planPath)
	if err != nil || !bytes.Equal(beforePlan, afterPlan) {
		t.Fatalf("orchestrate mutated legacy numeric PLAN: %v", err)
	}
	receipts, err := filepath.Glob(filepath.Join(root, ".woody", "jobs", "*.receipt.json"))
	if err != nil || len(receipts) != 0 {
		t.Fatalf("blocked orchestration started model jobs: %v, %v", receipts, err)
	}
}
