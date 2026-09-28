package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
