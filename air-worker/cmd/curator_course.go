package main

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const curatorCourseSchema = "air-worker.curator.course/v1"

type curatorCourseAction struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

type curatorCourseView struct {
	Schema       string                `json:"schema"`
	Completed    int                   `json:"completed"`
	Total        int                   `json:"total"`
	Progress     string                `json:"progress"`
	Stalls1h     []string              `json:"stalls_1h"`
	IdleWindows  []string              `json:"idle_windows"`
	PendingGates []string              `json:"pending_gates"`
	Actions      []curatorCourseAction `json:"actions"`
}

func appendCourseUniqueString(dst []string, value string) []string {
	for _, existing := range dst {
		if existing == value {
			return dst
		}
	}
	return append(dst, value)
}

func appendCourseAction(dst []curatorCourseAction, action curatorCourseAction) []curatorCourseAction {
	for _, existing := range dst {
		if existing.Kind == action.Kind && existing.Target == action.Target {
			return dst
		}
	}
	return append(dst, action)
}

func buildCuratorCourseFromNodes(root string, now time.Time, nodes []planNode, patrol *curatorPatrolCoverage, hasPatrol bool) (curatorCourseView, error) {
	course := curatorCourseView{Schema: curatorCourseSchema}
	course.Total = len(nodes)
	for _, node := range nodes {
		if node.Status == "closed" {
			course.Completed++
			continue
		}
		if strings.TrimSpace(node.Owner) == "" {
			course.Actions = appendCourseAction(course.Actions, curatorCourseAction{
				Kind: "assign-owner", Target: node.ID, Reason: "open node has no owner",
			})
		}
		updated, parseErr := time.Parse(time.RFC3339Nano, node.UpdatedAt)
		if parseErr == nil && !updated.After(now.Add(-time.Hour)) {
			course.Stalls1h = appendCourseUniqueString(course.Stalls1h, "node:"+node.ID)
			course.Actions = appendCourseAction(course.Actions, curatorCourseAction{
				Kind: "resume-or-reassign", Target: node.ID, Reason: "no node movement for >1h",
			})
		}
		if node.Status == "blocked" {
			course.PendingGates = appendCourseUniqueString(course.PendingGates, "node:"+node.ID)
		}
	}
	course.Progress = fmt.Sprintf("%d/%d", course.Completed, course.Total)

	for _, step := range readPlanSteps(filepath.Join(root, "PLAN.md")) {
		if !step.Done && step.Gate {
			course.PendingGates = appendCourseUniqueString(course.PendingGates, "plan:"+step.Num)
		}
	}
	proposals, err := readLearnProposals(learnPaths(root).Proposals)
	if err != nil {
		return curatorCourseView{}, err
	}
	for _, proposal := range proposals {
		if proposal.Status != learnPending {
			continue
		}
		target := "learn:" + proposal.ID
		course.PendingGates = appendCourseUniqueString(course.PendingGates, target)
		course.Actions = appendCourseAction(course.Actions, curatorCourseAction{
			Kind: "deliver-gate", Target: proposal.ID, Reason: "PENDING_LPR requires explicit user approval",
		})
	}

	if hasPatrol && patrol != nil {
		for _, row := range patrol.Rows {
			switch row.State {
			case "idle":
				course.IdleWindows = appendCourseUniqueString(course.IdleWindows, row.Window)
				course.Stalls1h = appendCourseUniqueString(course.Stalls1h, "window:"+row.Window)
				course.Actions = appendCourseAction(course.Actions, curatorCourseAction{
					Kind: "dispatch-work", Target: row.Window, Reason: "window has no commit/file activity in patrol interval",
				})
			case "stale-dirty":
				course.Stalls1h = appendCourseUniqueString(course.Stalls1h, "window:"+row.Window)
				course.Actions = appendCourseAction(course.Actions, curatorCourseAction{
					Kind: "inspect-stale-work", Target: row.Window, Reason: "dirty work exists without recent movement",
				})
			case "unresolved":
				course.Stalls1h = appendCourseUniqueString(course.Stalls1h, "window:"+row.Window)
				course.Actions = appendCourseAction(course.Actions, curatorCourseAction{
					Kind: "repair-patrol-evidence", Target: row.Window, Reason: row.Error,
				})
			}
		}
		if !patrol.Complete {
			course.Actions = appendCourseAction(course.Actions, curatorCourseAction{
				Kind: "repair-patrol-coverage", Target: patrol.Source, Reason: patrol.Summary,
			})
		}
	}

	sort.Strings(course.Stalls1h)
	sort.Strings(course.IdleWindows)
	sort.Strings(course.PendingGates)
	sort.SliceStable(course.Actions, func(i, j int) bool {
		if course.Actions[i].Kind == course.Actions[j].Kind {
			return course.Actions[i].Target < course.Actions[j].Target
		}
		return course.Actions[i].Kind < course.Actions[j].Kind
	})
	return course, nil
}

func buildCuratorCourse(root string, now time.Time, patrol *curatorPatrolCoverage, hasPatrol bool) (curatorCourseView, error) {
	nodes, err := listPlanNodes(root)
	if err != nil {
		return curatorCourseView{}, err
	}
	return buildCuratorCourseFromNodes(root, now, nodes, patrol, hasPatrol)
}
