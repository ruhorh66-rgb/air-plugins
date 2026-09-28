package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedUsageRule(t *testing.T, product, id string, approvedAt time.Time) learnRuleRecord {
	t.Helper()
	paths := learnPaths(product)
	if err := os.MkdirAll(paths.RulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rule := learnRuleRecord{
		Schema: learnRuleSchemaVersion, ProposalID: id, Class: "usage-test",
		Rule: "Use the approved check.", Trigger: "before action", CheckType: "hook",
		CheckSpec: learnCheckSGTCommitFreshness, TestCase: "bad -> block",
		Status: learnApplied, ApprovedAt: approvedAt.UTC().Format(time.RFC3339Nano),
		Approval: "grant:test", VerifiedAt: approvedAt.UTC().Format(time.RFC3339Nano),
		VerificationReceipt: "fixture:pass",
	}
	raw, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paths.RulesDir, id+".json"), append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestLearnUsageFallsBackToApprovedAtWithoutUses(t *testing.T) {
	product := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	approved := now.Add(-10 * time.Hour)
	seedUsageRule(t, product, "LP-usage-none", approved)

	stats, err := collectLearnRuleUsage(product, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].UseCount != 0 {
		t.Fatalf("unexpected usage stats: %#v", stats)
	}
	if stats[0].LastActivity != approved.Format(time.RFC3339Nano) || stats[0].IdleHours != 10 {
		t.Fatalf("fallback activity mismatch: %#v", stats[0])
	}
}

func TestLearnUsageCountsCanonicalRuleUseEvents(t *testing.T) {
	product := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)
	rule := seedUsageRule(t, product, "LP-usage-two", now.Add(-48*time.Hour))
	if err := recordLearnRuleUse(product, rule, "pass", "fixture one", now.Add(-2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := recordLearnRuleUse(product, rule, "block", "fixture two", now.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}

	stats, err := collectLearnRuleUsage(product, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 1 || stats[0].UseCount != 2 || stats[0].IdleHours != 1 {
		t.Fatalf("usage aggregation mismatch: %#v", stats)
	}
	if stats[0].LastActivity != now.Add(-time.Hour).Format(time.RFC3339Nano) {
		t.Fatalf("last activity mismatch: %#v", stats[0])
	}
	events, err := readLearnJournal(learnPaths(product).Journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Source != learnRuleUseSource || events[1].Reference != rule.ProposalID {
		t.Fatalf("rule use was not written to canonical events: %#v", events)
	}
}
