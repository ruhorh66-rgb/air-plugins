package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedPlanNodeProduct(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# Plan\n\nЦель → этапы.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPlanNodeNewAndCloseKeepsHistory(t *testing.T) {
	root := seedPlanNodeProduct(t)
	oldNow := planNodeNow
	defer func() { planNodeNow = oldNow }()
	t0 := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	planNodeNow = func() time.Time { return t0 }

	code := cmdPlanNodeNew([]string{
		"-product", root,
		"-title", "Проверить релиз",
		"-parent", "Этап 1",
		"-owner", "AC·DEV·AirWorker",
		"-done-when", "factual PASS",
		"-trigger", "решение ЛПР 28.09",
	})
	if code != 0 {
		t.Fatalf("node new code=%d", code)
	}

	nodes, err := listPlanNodes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes=%d want 1", len(nodes))
	}
	n := nodes[0]
	if n.Status != "open" || n.Parent != "Этап 1" || n.Owner != "AC·DEV·AirWorker" {
		t.Fatalf("unexpected node: %#v", n)
	}
	planRaw, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	planText := string(planRaw)
	if !strings.Contains(planText, planNodeMarker) || !strings.Contains(planText, "[["+n.ID+"]]") ||
		!strings.Contains(planText, "- [open]") {
		t.Fatalf("spine link missing:\n%s", planText)
	}

	planNodeNow = func() time.Time { return t0.Add(time.Hour) }
	if code := cmdPlanNodeClose([]string{
		n.ID, "-product", root, "-receipt", "02_RESULTS/pass.md",
	}); code != 0 {
		t.Fatalf("node close code=%d", code)
	}
	closed, err := readPlanNode(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != "closed" || len(closed.Receipts) != 1 || closed.Receipts[0] != "02_RESULTS/pass.md" {
		t.Fatalf("closed node=%#v", closed)
	}
	if closed.UpdatedAt == closed.CreatedAt {
		t.Fatalf("updated_at did not advance: %#v", closed)
	}
	if _, err := os.Stat(n.Path); err != nil {
		t.Fatalf("closed node was removed: %v", err)
	}
	planRaw, _ = os.ReadFile(filepath.Join(root, "PLAN.md"))
	if !strings.Contains(string(planRaw), "- [closed]") || !strings.Contains(string(planRaw), "[["+n.ID+"]]") {
		t.Fatalf("closed spine state missing:\n%s", planRaw)
	}
}

func TestPlanNodeCloseFailsClosedWithoutSpineLink(t *testing.T) {
	root := seedPlanNodeProduct(t)
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	n := planNode{
		ID: "N-001_orphan", Title: "Orphan", Parent: "Этап X",
		Trigger: "trigger", Owner: "worker", DoneWhen: "done", Status: "open",
		ReturnTo: "Этап X", Receipts: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(planNodeDir(root), n.ID+".md")
	if err := writeFileAtomic(path, renderPlanNode(n)); err != nil {
		t.Fatal(err)
	}

	if code := cmdPlanNodeClose([]string{n.ID, "-product", root, "-receipt", "r1"}); code != 2 {
		t.Fatalf("close without spine link code=%d want 2", code)
	}
	got, err := readPlanNode(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "open" || len(got.Receipts) != 0 {
		t.Fatalf("orphan node mutated despite failed close: %#v", got)
	}
}

func TestPlanNodeSchemaRejectsInvalidStatus(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	n := planNode{
		ID: "N-001_x", Title: "x", Parent: "p", Trigger: "t", Owner: "o",
		DoneWhen: "d", Status: "done-ish", ReturnTo: "p", CreatedAt: now, UpdatedAt: now,
	}
	if err := validatePlanNode(n); err == nil {
		t.Fatal("invalid status accepted")
	}
}

func TestNextPlanNodeIDIsDeterministicAndMonotonic(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"N-001_one.md", "N-007_seven.md", "README.md"} {
		if err := os.WriteFile(filepath.Join(planNodeDir(root), name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	id, err := nextPlanNodeID(root, "Новый этап")
	if err != nil {
		t.Fatal(err)
	}
	if id != "N-008_новый-этап" {
		t.Fatalf("id=%q", id)
	}
}

func TestListPlanNodesReadsOpenClosedAndOwnerless(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	for _, n := range []planNode{
		{ID: "N-001_open", Title: "open", Parent: "p1", Trigger: "t", Owner: "", DoneWhen: "d", Status: "open", ReturnTo: "p1", CreatedAt: now, UpdatedAt: now},
		{ID: "N-002_closed", Title: "closed", Parent: "p2", Trigger: "t", Owner: "w", DoneWhen: "d", Status: "closed", ReturnTo: "p2", CreatedAt: now, UpdatedAt: now, Receipts: []string{"r"}},
	} {
		if err := writeFileAtomic(filepath.Join(planNodeDir(root), n.ID+".md"), renderPlanNode(n)); err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := listPlanNodes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 || nodes[0].ID != "N-001_open" || nodes[1].ID != "N-002_closed" {
		t.Fatalf("nodes=%#v", nodes)
	}
	if nodes[0].Owner != "" || nodes[1].Status != "closed" {
		t.Fatalf("node fields lost: %#v", nodes)
	}
}
