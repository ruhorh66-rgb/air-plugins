package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

const learnRuleUseSource = "air-worker-rule"

type learnRuleUsageStat struct {
	ProposalID   string `json:"proposal_id"`
	Class        string `json:"class"`
	Status       string `json:"status"`
	UseCount     int    `json:"use_count"`
	LastActivity string `json:"last_activity,omitempty"`
	IdleHours    int64  `json:"idle_hours"`
}

func recordLearnRuleUse(product string, rule learnRuleRecord, outcome, evidence string, now time.Time) error {
	id, err := newLearnID("LR", now.UTC())
	if err != nil {
		return err
	}
	row := learnJournalRecord{
		Schema: learnSchemaVersion, ID: id, CreatedAt: now.UTC().Format(time.RFC3339Nano),
		Class:    rule.Class,
		Observed: fmt.Sprintf("rule %s used: %s", rule.ProposalID, strings.TrimSpace(outcome)),
		Evidence: strings.TrimSpace(evidence),
		Kind:     "check", Source: learnRuleUseSource, Actor: "core", Reference: rule.ProposalID,
	}
	if err := validateLearnEvent(row); err != nil {
		return err
	}
	return appendLearnJSON(learnPaths(product).Journal, row)
}

func collectLearnRuleUsage(product string, now time.Time) ([]learnRuleUsageStat, error) {
	paths := learnPaths(product)
	rules, _, err := readLearnRuleRecords(paths)
	if err != nil {
		return nil, err
	}
	events, err := readLearnJournal(paths.Journal)
	if err != nil {
		return nil, err
	}
	type agg struct {
		count int
		last  time.Time
	}
	byID := map[string]agg{}
	for _, event := range events {
		if event.Source != learnRuleUseSource || strings.TrimSpace(event.Reference) == "" {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, event.CreatedAt)
		if err != nil {
			continue
		}
		cur := byID[event.Reference]
		cur.count++
		if cur.last.IsZero() || at.After(cur.last) {
			cur.last = at
		}
		byID[event.Reference] = cur
	}
	out := make([]learnRuleUsageStat, 0, len(rules))
	for _, rule := range rules {
		base, _ := time.Parse(time.RFC3339Nano, rule.ApprovedAt)
		a := byID[rule.ProposalID]
		last := a.last
		if last.IsZero() {
			last = base
		}
		idle := int64(0)
		if !last.IsZero() && now.After(last) {
			idle = int64(now.Sub(last) / time.Hour)
		}
		stat := learnRuleUsageStat{
			ProposalID: rule.ProposalID, Class: rule.Class, Status: rule.Status,
			UseCount: a.count, IdleHours: idle,
		}
		if !last.IsZero() {
			stat.LastActivity = last.UTC().Format(time.RFC3339Nano)
		}
		out = append(out, stat)
	}
	return out, nil
}

func cmdLearnUsage(argv []string) int {
	fs := flag.NewFlagSet("learn usage", flag.ContinueOnError)
	product := fs.String("product", "", "managed product root")
	asJSON := fs.Bool("json", false, "JSON output")
	nowRaw := fs.String("now", "", "override current time RFC3339")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := normalizeLearnProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	now := time.Now().UTC()
	if strings.TrimSpace(*nowRaw) != "" {
		now, err = time.Parse(time.RFC3339, strings.TrimSpace(*nowRaw))
		if err != nil {
			fmt.Fprintln(os.Stderr, "learn usage -now:", err)
			return 2
		}
	}
	stats, err := collectLearnRuleUsage(root, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn usage:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"schema": "air-worker.learn.usage/v1", "generated_at": now.Format(time.RFC3339Nano), "rules": stats,
		})
		return 0
	}
	for _, stat := range stats {
		fmt.Printf("%s | class=%s | status=%s | use=%d | last=%s | idle_h=%d%s",
			stat.ProposalID, stat.Class, stat.Status, stat.UseCount, stat.LastActivity, stat.IdleHours, lineEnding)
	}
	return 0
}
