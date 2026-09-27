package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeStepJournal(t *testing.T, root string, rows ...map[string]any) {
	t.Helper()
	var b strings.Builder
	for _, row := range rows {
		raw, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(root, "steps.jsonl"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAgentLifecycleFieldsPersistAllCounters(t *testing.T) {
	row := map[string]any{}
	addAgentLifecycleFields(row, stepResult{
		AgentRequested: 3, AgentStarted: 3, AgentCompleted: 1,
		AgentAborted: 1, AgentOrphan: 1, AgentLive: 0,
		AgentIDs: []string{"a1", "a2", "a3"}, AgentIssue: "fixture issue",
	})
	for key, want := range map[string]int{
		"agents_requested":     3,
		"agents_started":       3,
		"agents_completed":     1,
		"agents_aborted":       1,
		"agent_orphan_results": 1,
		"agents_live":          0,
	} {
		got, ok := row[key].(int)
		if !ok || got != want {
			t.Fatalf("%s=%v want %d", key, row[key], want)
		}
	}
	if row["agent_issue"] != "fixture issue" {
		t.Fatalf("agent_issue=%v", row["agent_issue"])
	}
}

func TestReadSpendAggregatesAttemptsWithoutDoubleCountingIteration(t *testing.T) {
	root := t.TempDir()
	writeStepJournal(t, root,
		map[string]any{"iteration": 1, "tier": "terra:medium", "whatif": true},
		map[string]any{"iteration": 0, "tier": "script", "spent_usd": 0.0},
		map[string]any{
			"event": "vendor_limit_fallback", "iteration": 1, "tier": "terra:medium",
			"total_cost_usd": 0.2, "num_turns": 3, "spent_usd": 0.2,
			"agents_requested": 2, "agents_started": 2, "agents_completed": 1,
			"agents_aborted": 1, "agent_orphan_results": 0, "agents_live": 0,
		},
		map[string]any{
			"iteration": 1, "tier": "sol:medium",
			"total_cost_usd": 0.5, "num_turns": 4, "spent_usd": 0.7,
			"agents_requested": 2, "agents_started": 2, "agents_completed": 2,
		},
		map[string]any{"iteration": 2, "tier": "script", "spent_usd": 0.7},
	)
	got := readSpend(root, 20)
	if got.Iterations != 2 {
		t.Fatalf("iterations=%d want 2; retry rows must not double-count iteration", got.Iterations)
	}
	if got.DryRuns != 1 || got.NoWorkRuns != 1 {
		t.Fatalf("dry/no-work=%d/%d", got.DryRuns, got.NoWorkRuns)
	}
	if got.LeaderTurnsTotal != 7 || got.LeaderTurnsKnown != 2 || got.LeaderTurnsUnknown != 0 {
		t.Fatalf("leader turns total/known/unknown=%d/%d/%d", got.LeaderTurnsTotal, got.LeaderTurnsKnown, got.LeaderTurnsUnknown)
	}
	if got.AgentsRequested != 4 || got.AgentsStarted != 4 || got.AgentsCompleted != 3 ||
		got.AgentsAborted != 1 || got.AgentOrphanResults != 0 || got.AgentsLive != 0 {
		t.Fatalf("agent totals=%#v", got)
	}
	if got.Total < 0.6999 || got.Total > 0.7001 {
		t.Fatalf("total cost=%f want 0.7", got.Total)
	}
	if got.RunSpent == nil || *got.RunSpent != 0.7 {
		t.Fatalf("run_spent=%v", got.RunSpent)
	}
}

func TestReadSpendKeepsUnknownLeaderTurnsDistinctFromZero(t *testing.T) {
	root := t.TempDir()
	writeStepJournal(t, root,
		map[string]any{"iteration": 1, "tier": "terra:medium", "spent_usd": 0.1},
		map[string]any{"iteration": 2, "tier": "script", "spent_usd": 0.1},
	)
	got := readSpend(root, 20)
	if got.LeaderTurnsKnown != 0 || got.LeaderTurnsTotal != 0 || got.LeaderTurnsUnknown != 1 {
		t.Fatalf("unknown model turns collapsed to zero or counted script: %#v", got)
	}
}

func TestReportTopologyUsesOneRoleContractAndMixedVendorRunner(t *testing.T) {
	cfg := runConfig{
		Ladder: []string{"script", "terra:medium"},
		Runners: map[string]runnerSpec{
			"terra": {Kind: "codex", Model: "gpt-5.6-terra", Effort: "medium"},
		},
	}
	got := buildReportTopology(cfg, reportStep{Num: "7", Tier: "terra:medium", Found: true})
	if got.Orchestrator != roleOrchestrator || got.Engine != roleEngine || got.Planner != rolePlanner ||
		got.Guard != roleGuard || got.FactualJudge != roleFactualJudge {
		t.Fatalf("role constants drifted: %#v", got)
	}
	if got.Executor != "codex" || got.SemanticJudge != "claude" || got.CurrentRunner != "codex" ||
		got.CurrentProvider != "openai" || got.CurrentModel != "gpt-5.6-terra" || got.CurrentTier != "terra:medium" {
		t.Fatalf("mixed vendor topology wrong: %#v", got)
	}

	script := buildReportTopology(cfg, reportStep{Num: "1", Tier: "script", Found: true})
	if script.Executor != "script" || script.SemanticJudge != "none" || script.CurrentRunner != "script" {
		t.Fatalf("script topology wrong: %#v", script)
	}
}

func TestProductReportJSONAndTextShareTopologyAndExecutionMetrics(t *testing.T) {
	root := t.TempDir()
	writePortfolioPlan(t, filepath.Join(root, "PLAN.md"))
	cfg := runConfig{
		Plan:    "PLAN.md",
		Ladder:  []string{"script", "terra:medium"},
		Runners: map[string]runnerSpec{"terra": {Kind: "codex", Model: "gpt-5.6-terra"}},
	}
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	writeStepJournal(t, root, map[string]any{
		"iteration": 1, "tier": "terra:medium", "num_turns": 2,
		"agents_requested": 1, "agents_started": 1, "agents_completed": 1,
	})
	report := productReport{
		Product: root, Spend: readSpend(root, 20),
		Next: reportStep{Num: "2", Tier: "terra:medium", Found: true},
	}
	report.Topology = buildReportTopology(cfg, report.Next)
	text := report.text()
	for _, needle := range []string{"ходы ведущей known=1 total=2 unknown=0", "agents req/start/done/abort/orphan/live=1/1/1/0/0/0", "executor=codex", "semantic-judge=claude"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("text report missing %q:\n%s", needle, text)
		}
	}
	js, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, needle := range []string{`"leader_turns_total":2`, `"agents_completed":1`, `"semantic_judge":"claude"`} {
		if !strings.Contains(string(js), needle) {
			t.Fatalf("JSON report missing %q: %s", needle, js)
		}
	}
}
