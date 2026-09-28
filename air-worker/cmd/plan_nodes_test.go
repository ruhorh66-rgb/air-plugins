package main

import (
	"encoding/json"
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

func TestPlanMigratePreservesSectionsWritesEventsAndCloseKeepsBody(t *testing.T) {
	root := t.TempDir()
	original := "# Product plan\n\nGoal preamble.\n\n## First stage\nalpha line\n\n### Child heading\nchild stays here\n\n## Second stage\nbeta line\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	oldNow := planNodeNow
	defer func() { planNodeNow = oldNow }()
	t0 := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	planNodeNow = func() time.Time { return t0 }

	if code := cmdPlanMigrate([]string{"-product", root, "-trigger", "решение ЛПР"}); code != 0 {
		t.Fatalf("migrate code=%d", code)
	}
	nodes, err := listPlanNodes(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("migrated nodes=%d want 2", len(nodes))
	}
	firstBody, err := readPlanNodeBody(nodes[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	wantFirst := "## First stage\nalpha line\n\n### Child heading\nchild stays here\n\n"
	if firstBody != wantFirst {
		t.Fatalf("first section not preserved verbatim\nwant=%q\ngot =%q", wantFirst, firstBody)
	}
	secondBody, err := readPlanNodeBody(nodes[1].Path)
	if err != nil {
		t.Fatal(err)
	}
	if secondBody != "## Second stage\nbeta line\n" {
		t.Fatalf("second section not preserved: %q", secondBody)
	}
	spineRaw, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	spine := string(spineRaw)
	if !strings.Contains(spine, planNodeMarker) || !strings.Contains(spine, "[["+nodes[0].ID+"]]") || !strings.Contains(spine, "[["+nodes[1].ID+"]]") {
		t.Fatalf("spine links missing:\n%s", spine)
	}
	if strings.Contains(spine, "alpha line") || strings.Contains(spine, "beta line") {
		t.Fatalf("migrated section bodies leaked back into compact spine:\n%s", spine)
	}

	eventRaw, err := os.ReadFile(planNodeEventsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var migrationEvents int
	for _, line := range strings.Split(strings.TrimSpace(string(eventRaw)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["source"] == "plan-migrate" && row["class"] == "plan-node" {
			migrationEvents++
		}
	}
	if migrationEvents != 2 {
		t.Fatalf("migration events=%d want 2\n%s", migrationEvents, eventRaw)
	}

	planNodeNow = func() time.Time { return t0.Add(time.Hour) }
	if code := cmdPlanNodeClose([]string{nodes[0].ID, "-product", root, "-receipt", "receipt-1"}); code != 0 {
		t.Fatalf("close code=%d", code)
	}
	closedBody, err := readPlanNodeBody(nodes[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if closedBody != wantFirst {
		t.Fatalf("close destroyed migrated body\nwant=%q\ngot =%q", wantFirst, closedBody)
	}
	eventRaw, err = os.ReadFile(planNodeEventsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(strings.TrimSpace(string(eventRaw)), "\n") + 1; lines != 3 {
		t.Fatalf("events after close=%d want 3\n%s", lines, eventRaw)
	}
}

func TestPlanMigrateFailsClosedWhenNodeDirectoryAlreadyHasNodes(t *testing.T) {
	root := t.TempDir()
	plan := []byte("# Plan\n\n## Stage\nbody\n")
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), plan, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planNodeDir(root), "N-001_existing.md"), []byte("occupied"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if code := cmdPlanMigrate([]string{"-product", root}); code != 2 {
		t.Fatalf("migrate with existing nodes code=%d want 2", code)
	}
	after, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if string(before) != string(after) {
		t.Fatal("failed migration mutated PLAN.md")
	}
	if _, err := os.Stat(planNodeEventsPath(root)); !os.IsNotExist(err) {
		t.Fatalf("failed migration wrote event journal: %v", err)
	}
}

func TestPlanNodeNewAndCloseWriteCanonicalEvents(t *testing.T) {
	root := seedPlanNodeProduct(t)
	oldNow := planNodeNow
	defer func() { planNodeNow = oldNow }()
	t0 := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	planNodeNow = func() time.Time { return t0 }
	if code := cmdPlanNodeNew([]string{
		"-product", root, "-title", "LPR input", "-parent", "Stage",
		"-owner", "AC-DEV", "-done-when", "receipt", "-trigger", "слово ЛПР",
	}); code != 0 {
		t.Fatalf("new code=%d", code)
	}
	nodes, err := listPlanNodes(root)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes=%#v err=%v", nodes, err)
	}
	planNodeNow = func() time.Time { return t0.Add(time.Minute) }
	if code := cmdPlanNodeClose([]string{nodes[0].ID, "-product", root, "-receipt", "r"}); code != 0 {
		t.Fatalf("close code=%d", code)
	}
	raw, err := os.ReadFile(planNodeEventsPath(root))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(rows) != 2 {
		t.Fatalf("events=%d want 2\n%s", len(rows), raw)
	}
	for i, line := range rows {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["schema"] != learnSchemaVersion || row["source"] != "plan-node" || row["reference"] != nodes[0].ID {
			t.Fatalf("event %d incompatible: %#v", i, row)
		}
	}
}

func TestCalculatePlanNodeStatsCountsOpenOwnerlessAndStale(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	old := now.Add(-25 * time.Hour).Format(time.RFC3339Nano)
	fresh := now.Add(-time.Hour).Format(time.RFC3339Nano)
	nodes := []planNode{
		{ID: "N-001_stale", Title: "stale", Parent: "p", Trigger: "t", Owner: "", DoneWhen: "d", Status: "open", ReturnTo: "p", CreatedAt: old, UpdatedAt: old},
		{ID: "N-002_blocked", Title: "blocked", Parent: "p", Trigger: "t", Owner: "worker", DoneWhen: "d", Status: "blocked", ReturnTo: "p", CreatedAt: fresh, UpdatedAt: fresh},
		{ID: "N-003_closed", Title: "closed", Parent: "p", Trigger: "t", Owner: "", DoneWhen: "d", Status: "closed", ReturnTo: "p", CreatedAt: old, UpdatedAt: old},
	}
	for _, n := range nodes {
		if err := writeFileAtomic(filepath.Join(planNodeDir(root), n.ID+".md"), renderPlanNode(n)); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := calculatePlanNodeStats(root, now)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Open != 2 || stats.NoOwner != 1 || stats.Stale24h != 1 {
		t.Fatalf("stats=%+v", stats)
	}
}

func TestCuratorTickReportsNodeCounts(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if err := os.MkdirAll(planNodeDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	old := now.Add(-25 * time.Hour).Format(time.RFC3339Nano)
	fresh := now.Add(-time.Hour).Format(time.RFC3339Nano)
	for _, n := range []planNode{
		{ID: "N-001_stale", Title: "stale", Parent: "p", Trigger: "t", Owner: "", DoneWhen: "d", Status: "open", ReturnTo: "p", CreatedAt: old, UpdatedAt: old},
		{ID: "N-002_blocked", Title: "blocked", Parent: "p", Trigger: "t", Owner: "worker", DoneWhen: "d", Status: "blocked", ReturnTo: "p", CreatedAt: fresh, UpdatedAt: fresh},
	} {
		if err := writeFileAtomic(filepath.Join(planNodeDir(root), n.ID+".md"), renderPlanNode(n)); err != nil {
			t.Fatal(err)
		}
	}
	code, out := captureLoopOutput(t, func() int {
		return cmdCuratorTick([]string{"-product", root, "-now", now.Format(time.RFC3339)})
	})
	if code != 0 {
		t.Fatalf("tick code=%d out=%q", code, out)
	}
	want := "открытых узлов 2, без владельца 1, без движения >24 ч 1"
	if !strings.Contains(out, want) {
		t.Fatalf("tick output=%q want substring %q", out, want)
	}
}
