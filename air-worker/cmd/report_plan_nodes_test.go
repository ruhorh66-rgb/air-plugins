package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReportDoesNotResurrectLegacyRowAfterCanonicalNodesClose(t *testing.T) {
	root := t.TempDir()
	legacy := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | historical open row | `script` | К1 |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	node := planNode{
		ID: "N-001_closed", Title: "Completed canonical work", Parent: "root",
		Trigger: "test", Owner: "worker", DoneWhen: "accepted",
		Status: "closed", ReturnTo: "release", Receipts: []string{"receipt.json"},
		CreatedAt: now, UpdatedAt: now,
	}
	if err := os.WriteFile(filepath.Join(planNodeDir(root), node.ID+".md"), renderPlanNode(node), 0o644); err != nil {
		t.Fatal(err)
	}
	got := nextOpenStep(root, "PLAN.md")
	if got.Found {
		t.Fatalf("closed canonical node spine must not fall back to historical PLAN row: %#v", got)
	}
}

func TestReportNextPrefersOpenPlanNode(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# Plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	node := planNode{
		ID: "N-001_urgent-focus", Title: "Urgent focus", Parent: "0.11.2",
		Trigger: "LPR", Owner: "AC-DEV-AirWorker", DoneWhen: "accepted",
		Status: "open", ReturnTo: "release", CreatedAt: now, UpdatedAt: now,
	}
	if err := os.WriteFile(filepath.Join(planNodeDir(root), node.ID+".md"), renderPlanNode(node), 0o644); err != nil {
		t.Fatal(err)
	}

	got := nextOpenStep(root, "PLAN.md")
	if !got.Found || got.Source != "plan-node" || got.Num != node.ID || got.Title != node.Title || got.Owner != node.Owner {
		t.Fatalf("report did not select open plan node: %#v", got)
	}
	topology := buildReportTopology(runConfig{}, got)
	if topology.Executor != "session" || topology.CurrentTier != "node" || topology.CurrentRunner != node.Owner {
		t.Fatalf("plan-node topology must not invent a ladder runner: %#v", topology)
	}
}

func TestReportDoesNotFallbackToLegacyOnMalformedPlanNode(t *testing.T) {
	root := t.TempDir()
	planPath := filepath.Join(root, "PLAN.md")
	plan := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | legacy work | `script` | К1 |\n"
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planNodeDir(root), "N-999_bad.md"), []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := nextOpenStep(root, planPath); got.Found {
		t.Fatalf("report resurrected legacy work after canonical node read failure: %#v", got)
	}
	m := cachedUnknownMeasure(root, runConfig{Plan: "PLAN.md"}, "probe")
	if m.Verdict != verdictNotProven || m.PlanOpenSteps != nil || m.PlanClosedSteps != nil || m.PlanGates != 0 {
		t.Fatalf("cached report must expose unreadable node work as NOT_PROVEN: %#v", m)
	}
	if !strings.Contains(m.Note, "plan-node") {
		t.Fatalf("cached report did not name node-source error: %q", m.Note)
	}
}
