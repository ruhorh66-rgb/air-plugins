package main

import (
	"strings"
	"testing"
	"time"
)

func TestCuratorCourseBuildsProgressStallsGatesAndActions(t *testing.T) {
	root := seedPlanNodeProduct(t)
	oldNow := planNodeNow
	defer func() { planNodeNow = oldNow }()
	t0 := time.Date(2026, 9, 28, 8, 0, 0, 0, time.UTC)
	planNodeNow = func() time.Time { return t0 }

	for _, title := range []string{"old open work", "closed work"} {
		if code := cmdPlanNodeNew([]string{
			"-product", root, "-title", title, "-parent", "milestone",
			"-owner", "AC·DEV·Test", "-done-when", "receipt exists",
			"-trigger", "test trigger",
		}); code != 0 {
			t.Fatalf("node new %q code=%d", title, code)
		}
	}
	nodes, err := listPlanNodes(root)
	if err != nil || len(nodes) != 2 {
		t.Fatalf("nodes=%#v err=%v", nodes, err)
	}
	planNodeNow = func() time.Time { return t0.Add(30 * time.Minute) }
	if code := cmdPlanNodeClose([]string{nodes[1].ID, "-product", root, "-receipt", "receipt:test"}); code != 0 {
		t.Fatalf("close code=%d", code)
	}

	seedLearnProposal(t, root, "LP-course", "scope", "keep scope")
	patrol := &curatorPatrolCoverage{
		Schema: "air-worker.curator.patrol/v1", Source: "plan/N-012.md",
		Expected: 2, Checked: 2, Complete: true, Summary: "проверено 2 из 2", Judge: "PASS",
		Rows: []curatorPatrolWindow{
			{Window: "AC·DEV·Idle", State: "idle"},
			{Window: "AC·DEV·Busy", State: "active"},
		},
	}
	course, err := buildCuratorCourse(root, t0.Add(2*time.Hour), patrol, true)
	if err != nil {
		t.Fatal(err)
	}
	if course.Progress != "1/2" || course.Completed != 1 || course.Total != 2 {
		t.Fatalf("progress=%#v", course)
	}
	joinedStalls := strings.Join(course.Stalls1h, "|")
	if !strings.Contains(joinedStalls, "node:"+nodes[0].ID) || !strings.Contains(joinedStalls, "window:AC·DEV·Idle") {
		t.Fatalf("stalls=%#v", course.Stalls1h)
	}
	if len(course.IdleWindows) != 1 || course.IdleWindows[0] != "AC·DEV·Idle" {
		t.Fatalf("idle=%#v", course.IdleWindows)
	}
	if !strings.Contains(strings.Join(course.PendingGates, "|"), "learn:LP-course") {
		t.Fatalf("gates=%#v", course.PendingGates)
	}
	kinds := map[string]bool{}
	for _, action := range course.Actions {
		kinds[action.Kind] = true
	}
	for _, want := range []string{"resume-or-reassign", "dispatch-work", "deliver-gate"} {
		if !kinds[want] {
			t.Fatalf("missing action %q in %#v", want, course.Actions)
		}
	}
}

func TestCuratorTickJSONCarriesCourseV1(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if code := cmdPlanNodeNew([]string{
		"-product", root, "-title", "work", "-parent", "milestone",
		"-owner", "AC·DEV·Test", "-done-when", "done", "-trigger", "test",
	}); code != 0 {
		t.Fatalf("node new code=%d", code)
	}
	code, out := captureLoopOutput(t, func() int {
		return cmdCuratorTick([]string{"-product", root, "-json", "-now", "2026-09-28T12:00:00Z"})
	})
	if code != 0 {
		t.Fatalf("tick code=%d out=%s", code, out)
	}
	if !strings.Contains(out, `"schema":"air-worker.curator.tick/v3"`) ||
		!strings.Contains(out, `"course":{"schema":"air-worker.curator.course/v1"`) {
		t.Fatalf("course missing from tick: %s", out)
	}
}
