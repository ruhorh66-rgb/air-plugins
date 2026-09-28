package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func cmdCuratorTick(argv []string) int {
	fs := flag.NewFlagSet("curator tick", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	nowRaw := fs.String("now", "", "override current time RFC3339")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	now := time.Now().UTC()
	if strings.TrimSpace(*nowRaw) != "" {
		now, err = time.Parse(time.RFC3339, strings.TrimSpace(*nowRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "curator tick -now:", err)
			return 2
		}
	}
	judgeRefresh, err := ensureFreshMachineVerdict(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator tick judge:", err)
		return 3
	}
	nodes, err := listPlanNodes(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator tick:", err)
		return 2
	}
	stats, err := calculatePlanNodeStatsFromNodes(nodes, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator tick:", err)
		return 2
	}
	patrol, hasPatrol, err := buildCuratorPatrolCoverage(root, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator tick patrol:", err)
		return 3
	}
	weekly, err := runDueLearnWeekly(root, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator tick weekly:", err)
		return 3
	}
	course, err := buildCuratorCourseFromNodes(root, now, nodes, patrol, hasPatrol)
	if err != nil {
		fmt.Fprintln(os.Stderr, "curator tick course:", err)
		return 3
	}
	rc := 0
	if hasPatrol && !patrol.Complete {
		rc = 3
	}
	if weekly.Error != "" {
		rc = 3
	}
	if judgeRefresh.Code == 2 {
		rc = 3
	}
	if *asJSON {
		doc := map[string]any{
			"schema":       "air-worker.curator.tick/v3",
			"generated_at": now.UTC().Format(time.RFC3339Nano),
			"product":      root,
			"open_nodes":   stats.Open,
			"no_owner":     stats.NoOwner,
			"stale_24h":    stats.Stale24h,
			"judge":        judgeRefresh,
			"weekly":       weekly,
			"course":       course,
		}
		if hasPatrol {
			doc["patrol"] = patrol
		}
		if err := json.NewEncoder(os.Stdout).Encode(doc); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return rc
	}

	fmt.Printf("открытых узлов %d, без владельца %d, без движения >24 ч %d%s",
		stats.Open, stats.NoOwner, stats.Stale24h, lineEnding)
	fmt.Printf("judge refreshed=%t code=%d reason=%s%s", judgeRefresh.Refreshed, judgeRefresh.Code, judgeRefresh.Reason, lineEnding)
	fmt.Printf("weekly due=%t ran=%t last=%s run=%s transitions=%d merge=%d%s",
		weekly.Due, weekly.Ran, weekly.LastRunID, weekly.RunID, weekly.Transitions, weekly.MergeCount, lineEnding)
	if weekly.Error != "" {
		fmt.Printf("weekly error=%s%s", weekly.Error, lineEnding)
	}
	fmt.Printf("курс %s | тормозов >1ч %d | idle окон %d | гейтов %d | действий %d%s",
		course.Progress, len(course.Stalls1h), len(course.IdleWindows), len(course.PendingGates), len(course.Actions), lineEnding)
	for _, action := range course.Actions {
		fmt.Printf("action=%s target=%s reason=%s%s", action.Kind, action.Target, action.Reason, lineEnding)
	}
	if hasPatrol {
		for _, row := range patrol.Rows {
			files := "-"
			if len(row.Files30m) > 0 {
				files = strings.Join(row.Files30m, ",")
			}
			commit := row.Commit
			if commit == "" {
				commit = "-"
			}
			commitAt := row.CommitAt
			if commitAt == "" {
				commitAt = "-"
			}
			fmt.Printf("%s | commit=%s @ %s | files30m=%d [%s] | state=%s%s",
				row.Window, commit, commitAt, len(row.Files30m), files, row.State, lineEnding)
		}
		fmt.Printf("%s | judge=%s%s", patrol.Summary, patrol.Judge, lineEnding)
	}
	return rc
}
