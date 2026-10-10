package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n112NativeNew(t *testing.T, root, title, parent string) planNode {
	t.Helper()
	code := cmdPlanNodeNew([]string{
		"-product", root, "-title", title, "-parent", parent,
		"-owner", "gpt-window-n112", "-actor-kind", "gpt-window",
		"-actor", "n112-test-worker", "-done-when", "judge PASS and immutable receipt",
		"-trigger", "isolated self-learning test",
	})
	if code != 0 {
		t.Fatalf("native plan node new failed for %q under %q: %d", title, parent, code)
	}
	all, err := listPlanNodes(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range all {
		if n.Title == title {
			return n
		}
	}
	t.Fatal("native new node not found")
	return planNode{}
}

func n112Fixture(t *testing.T) (string, planNode, planNode, string, string) {
	t.Helper()
	root := seedPlanNodeProduct(t)
	parent := n112NativeNew(t, root, "P0 n112 parent", "Stage: supervised tests")
	child := n112NativeNew(t, root, "P1 n112 child", parent.ID)
	original := child.Parent
	wrong := original + "s"
	childPath := filepath.Join(planNodeDir(root), child.ID+".md")
	child.Parent = wrong
	child.ReturnTo = wrong
	if err := writeFileAtomicDurable(childPath, renderPlanNode(child)); err != nil {
		t.Fatal(err)
	}
	if err := updateNodeInSpine(filepath.Join(root, "PLAN.md"), child); err != nil {
		t.Fatal(err)
	}
	evidence := filepath.Join(root, "n112-evidence.json")
	if err := os.WriteFile(evidence, []byte(`{"schema":"air-worker.n112.evidence/v1","result":"PASS"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, parent, child, wrong, evidence
}

func TestN112PlanNodeNewRejectsUnknownCanonicalParent(t *testing.T) {
	root := seedPlanNodeProduct(t)
	old, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	rc := cmdPlanNodeNew([]string{
		"-product", root, "-title", "reject unknown",
		"-parent", "N-999_missing-parent",
		"-owner", "gpt-window-n112", "-actor-kind", "gpt-window",
		"-actor", "n112-test-worker", "-done-when", "tested",
	})
	if rc == 0 {
		t.Fatal("native core created a node under unknown N-* parent")
	}
	updated, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != string(old) {
		t.Fatal("invalid parent mutated PLAN")
	}
	nodes, err := listPlanNodes(root)
	if err != nil || len(nodes) != 0 {
		t.Fatalf("invalid parent emitted nodes: %v nodes=%d", err, len(nodes))
	}
}

func TestN112NativeReparentRepairsExactLineageAndIsIdempotent(t *testing.T) {
	root, parent, child, wrong, evidence := n112Fixture(t)
	cmd := []string{
		child.ID, "-product", root, "-from", wrong, "-to", parent.ID,
		"-receipt", evidence, "-request-id", "N112-test-reparent-001",
		"-actor-kind", "gpt-window", "-actor", "n112-test-worker", "-json",
	}
	if code := cmdPlanNodeReparent(cmd); code != 0 {
		t.Fatalf("native repair returned %d", code)
	}
	got, err := readPlanNode(filepath.Join(planNodeDir(root), child.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Parent != parent.ID || got.ReturnTo != parent.ID || got.Status != "open" {
		t.Fatalf("node incorrect after native repair: %+v", got)
	}
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(plan), spineNodeLine(got)) != 1 ||
		strings.Contains(string(plan), "- [open] "+wrong+" · owner:"+got.Owner+" · [["+got.ID+"]]") {
		t.Fatal("spine did not agree with native repaired node")
	}
	eventPath := planNodeEventsPath(root)
	events, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, line := range strings.Split(string(events), string(byte(10))) {
		if !strings.Contains(line, child.ID) {
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(line), &doc); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(doc["observed"].(string), "reparented") {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one native reparented LEARN event, got %d", count)
	}
	if code := cmdPlanNodeReparent(cmd); code != 0 {
		t.Fatalf("idempotent replay rc=%d", code)
	}
	after, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(events) {
		t.Fatal("idempotent reparent duplicated native journal event")
	}
}

func TestN112NativeReparentRejectsWrongOrCyclicTargetBeforeMutation(t *testing.T) {
	root, parent, child, wrong, evidence := n112Fixture(t)
	nodePath := filepath.Join(planNodeDir(root), child.ID+".md")
	oldNode, _ := os.ReadFile(nodePath)
	planPath := filepath.Join(root, "PLAN.md")
	oldPlan, _ := os.ReadFile(planPath)
	for _, tt := range []struct{ name, from, to string }{
		{"foreign-old", "N-999_not_actual", parent.ID},
		{"self-cycle", wrong, child.ID},
		{"missing-parent", wrong, "N-999_missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rc := cmdPlanNodeReparent([]string{
				child.ID, "-product", root, "-from", tt.from, "-to", tt.to,
				"-receipt", evidence, "-request-id", "N112-negative-" + tt.name,
				"-actor-kind", "gpt-window", "-actor", "n112-test-worker",
			})
			if rc == 0 {
				t.Fatal("invalid reparent succeeded")
			}
			nodeNow, _ := os.ReadFile(nodePath)
			planNow, _ := os.ReadFile(planPath)
			if string(nodeNow) != string(oldNode) || string(planNow) != string(oldPlan) {
				t.Fatal("invalid reparent mutated node or spine")
			}
		})
	}
}
