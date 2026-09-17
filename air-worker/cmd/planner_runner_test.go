package main

import (
	"strings"
	"testing"
)

func TestPlannerUsesTopExecutorVendor(t *testing.T) {
	cfg := runConfig{Runners: map[string]runnerSpec{
		"sol":  {Kind: "codex", Model: "gpt-5.6-sol"},
		"opus": {Kind: "claude", Model: "opus"},
	}}
	r, err := plannerRunner(cfg, "sol:high")
	if err != nil || r.Kind != "codex" || r.Model != "gpt-5.6-sol" || r.Effort != "high" {
		t.Fatalf("Codex top rung resolved incorrectly: %+v %v", r, err)
	}
	r, err = plannerRunner(cfg, "opus:max")
	if err != nil || r.Kind != "claude" || r.Model != "opus" || r.Effort != "max" {
		t.Fatalf("Claude top rung resolved incorrectly: %+v %v", r, err)
	}
}

func TestCodexPlannerSandboxIsReadOnly(t *testing.T) {
	args := plannerCodexArgs(`X:\product`, "task", runnerSpec{Kind: "codex", Model: "gpt-test", Effort: "high"})
	joined := " "
	for _, a := range args {
		joined += a + " "
	}
	if !strings.Contains(joined, "read-only") || !strings.Contains(joined, "gpt-test") || !strings.Contains(joined, "model_reasoning_effort=high") {
		t.Fatalf("planner args incorrect: %s", joined)
	}
	if strings.Contains(joined, "workspace-write") {
		t.Fatalf("planner unexpectedly has write sandbox: %s", joined)
	}
	meta := codexReceiptMeta(runnerSpec{Kind: "codex", Model: "gpt-test", Effort: "high"}, "planner", "read-only")
	if meta.Role != "planner" || meta.Sandbox != "read-only" {
		t.Fatalf("planner receipt hides effective access: %+v", meta)
	}
}
