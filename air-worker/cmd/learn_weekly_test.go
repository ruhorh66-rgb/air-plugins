package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWeeklyRuleLifecycleUsesLastActivityAndKeepsSnapshot(t *testing.T) {
	cases := []struct {
		name       string
		age        time.Duration
		wantStatus string
		wantMove   bool
	}{
		{"before-stale", 13 * 24 * time.Hour, learnApplied, false},
		{"stale", 14 * 24 * time.Hour, learnRuleStale, true},
		{"archive", 30 * 24 * time.Hour, learnRuleArchived, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			product := t.TempDir()
			now := time.Now().UTC().Truncate(time.Second)
			rule := seedUsageRule(t, product, "LP-weekly-"+tc.name, now.Add(-tc.age))

			run, err := runLearnWeekly(product, now)
			if err != nil {
				t.Fatal(err)
			}
			if (len(run.Transitions) > 0) != tc.wantMove {
				t.Fatalf("transitions=%#v want move=%v", run.Transitions, tc.wantMove)
			}
			raw, err := os.ReadFile(filepath.Join(learnPaths(product).RulesDir, rule.ProposalID+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var got learnRuleRecord
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.wantStatus {
				t.Fatalf("status=%q want %q", got.Status, tc.wantStatus)
			}
			snapshotRaw, err := os.ReadFile(filepath.FromSlash(run.Snapshot))
			if err != nil {
				t.Fatalf("snapshot missing: %v", err)
			}
			var snapshot learnWeeklySnapshot
			if err := json.Unmarshal(snapshotRaw, &snapshot); err != nil {
				t.Fatal(err)
			}
			if len(snapshot.Rules) != 1 || snapshot.Rules[0].Status != learnApplied {
				t.Fatalf("snapshot must precede lifecycle mutation: %#v", snapshot.Rules)
			}
			if _, err := os.Stat(run.Report); err != nil {
				t.Fatalf("REPORT.md missing: %v", err)
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(run.Report), "run.json")); err != nil {
				t.Fatalf("run.json missing: %v", err)
			}
		})
	}
}

func TestWeeklyRecentUsePreventsStale(t *testing.T) {
	product := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	rule := seedUsageRule(t, product, "LP-weekly-recent", now.Add(-40*24*time.Hour))
	if err := recordLearnRuleUse(product, rule, "pass", "recent use", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	run, err := runLearnWeekly(product, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Transitions) != 0 {
		t.Fatalf("recently used rule transitioned: %#v", run.Transitions)
	}
	rows, _, err := readActiveLearnRuleRecords(learnPaths(product))
	if err != nil || len(rows) != 1 {
		t.Fatalf("recently used rule is not active: rows=%#v err=%v", rows, err)
	}
}

func TestWeeklyMergeReviewCreatesPendingProposalOnly(t *testing.T) {
	product := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	seedUsageRule(t, product, "LP-merge-a", now.Add(-time.Hour))
	seedUsageRule(t, product, "LP-merge-b", now.Add(-time.Hour))

	old := weeklyMergeReviewModel
	defer func() { weeklyMergeReviewModel = old }()
	weeklyMergeReviewModel = func(product, packetPath string) ([]learnReviewCandidate, error) {
		packet, err := os.ReadFile(packetPath)
		if err != nil {
			return nil, err
		}
		if !strings.Contains(string(packet), "LP-merge-a") || !strings.Contains(string(packet), "LP-merge-b") {
			t.Fatalf("merge packet misses source rules: %s", packet)
		}
		return []learnReviewCandidate{{
			Class: "usage-test", Rule: "Use one consolidated approved check.",
			Trigger: "before action", CheckType: "hook",
			CheckSpec: "consolidated-usage-check", TestCase: "violation -> block",
		}}, nil
	}

	run, err := runLearnWeekly(product, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.MergeProposals) != 1 || run.MergeReviewErr != "" {
		t.Fatalf("weekly merge result=%#v", run)
	}
	pending, err := readLearnProposals(learnPaths(product).Proposals)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Status != learnPending || len(pending[0].SourceIDs) != 1 ||
		!strings.HasPrefix(pending[0].SourceIDs[0], "weekly:") {
		t.Fatalf("merge review did not create PENDING_LPR only: %#v", pending)
	}
	active, _, err := readActiveLearnRuleRecords(learnPaths(product))
	if err != nil || len(active) != 2 {
		t.Fatalf("merge review mutated active source rules: active=%#v err=%v", active, err)
	}
}

func TestLearnWeeklyScheduleRunsImmediatelyThenWaitsSevenDays(t *testing.T) {
	product := t.TempDir()
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

	due, last, _, err := learnWeeklyDue(product, now)
	if err != nil || !due || last != nil {
		t.Fatalf("first due=%v last=%#v err=%v", due, last, err)
	}
	first, err := runDueLearnWeekly(product, now)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Due || !first.Ran || first.RunID == "" || first.Report == "" {
		t.Fatalf("first weekly view=%#v", first)
	}

	early, err := runDueLearnWeekly(product, now.Add(6*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if early.Due || early.Ran || early.LastRunID != first.RunID {
		t.Fatalf("weekly reran too early: first=%#v early=%#v", first, early)
	}

	dueAgain, _, _, err := learnWeeklyDue(product, now.Add(7*24*time.Hour))
	if err != nil || !dueAgain {
		t.Fatalf("weekly not due at 7d: due=%v err=%v", dueAgain, err)
	}
}
