package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const learnWeeklyInterval = 7 * 24 * time.Hour

type curatorWeeklyView struct {
	Schema      string `json:"schema"`
	Due         bool   `json:"due"`
	Ran         bool   `json:"ran"`
	LastRunID   string `json:"last_run_id,omitempty"`
	LastRunAt   string `json:"last_run_at,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	Transitions int    `json:"transitions,omitempty"`
	MergeCount  int    `json:"merge_proposals,omitempty"`
	Report      string `json:"report,omitempty"`
	Error       string `json:"error,omitempty"`
}

func latestLearnWeeklyRun(product string) (*learnWeeklyRun, time.Time, error) {
	dir := learnWeeklyDir(product)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, time.Time{}, nil
	}
	if err != nil {
		return nil, time.Time{}, err
	}
	type candidate struct {
		run learnWeeklyRun
		at  time.Time
	}
	var candidates []candidate
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name(), "run.json"))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, time.Time{}, err
		}
		var run learnWeeklyRun
		if err := json.Unmarshal(raw, &run); err != nil {
			return nil, time.Time{}, err
		}
		atRaw := strings.TrimSpace(run.StartedAt)
		if atRaw == "" {
			atRaw = strings.TrimSpace(run.FinishedAt)
		}
		at, err := time.Parse(time.RFC3339Nano, atRaw)
		if err != nil {
			return nil, time.Time{}, err
		}
		candidates = append(candidates, candidate{run: run, at: at})
	}
	if len(candidates) == 0 {
		return nil, time.Time{}, nil
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].at.After(candidates[j].at) })
	run := candidates[0].run
	return &run, candidates[0].at, nil
}

func learnWeeklyDue(product string, now time.Time) (bool, *learnWeeklyRun, time.Time, error) {
	last, at, err := latestLearnWeeklyRun(product)
	if err != nil {
		return false, nil, time.Time{}, err
	}
	if last == nil {
		return true, nil, time.Time{}, nil
	}
	return !at.After(now.Add(-learnWeeklyInterval)), last, at, nil
}

func runDueLearnWeekly(product string, now time.Time) (curatorWeeklyView, error) {
	view := curatorWeeklyView{Schema: "air-worker.curator.weekly/v1"}
	due, last, lastAt, err := learnWeeklyDue(product, now)
	if err != nil {
		return view, err
	}
	view.Due = due
	if last != nil {
		view.LastRunID = last.RunID
		view.LastRunAt = lastAt.UTC().Format(time.RFC3339Nano)
	}
	if !due {
		return view, nil
	}
	run, err := runLearnWeekly(product, now)
	if err != nil {
		return view, err
	}
	view.Ran = true
	view.RunID = run.RunID
	view.Transitions = len(run.Transitions)
	view.MergeCount = len(run.MergeProposals)
	view.Report = run.Report
	if run.MergeReviewErr != "" {
		view.Error = run.MergeReviewErr
	}
	return view, nil
}
