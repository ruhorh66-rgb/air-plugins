package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func seedTypedActiveLearnSkill(t *testing.T, product string) learnRuleRecord {
	t.Helper()
	paths := learnPaths(product)
	if err := os.MkdirAll(paths.RulesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rule := learnRuleRecord{
		Schema: learnRuleSchemaVersion, ProposalID: "LP-skill-index", Class: "verify-before-claim",
		Rule:    "Before reporting success, run the declared factual judge and cite its fresh machine receipt instead of relying on memory or narrative confidence.",
		Trigger: "before reporting PASS or closing a step", CheckType: "hook",
		CheckSpec: "block PASS claim without a fresh machine verdict", TestCase: "PASS without fresh verdict -> block",
		Status: learnApplied, ApprovedAt: now, Approval: "grant:test", VerifiedAt: now,
		VerificationReceipt: "receipt:skill-index-test",
	}
	raw, _ := json.Marshal(rule)
	if err := os.WriteFile(filepath.Join(paths.RulesDir, rule.ProposalID+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return rule
}

func TestLearnSkillIndexIsCompactAndBodyOnDemand(t *testing.T) {
	product := t.TempDir()
	rule := seedTypedActiveLearnSkill(t, product)

	index, err := approvedLearnSkillIndex(product)
	if err != nil {
		t.Fatal(err)
	}
	text := string(index)
	if !strings.Contains(text, rule.ProposalID) || !strings.Contains(text, rule.Class) {
		t.Fatalf("skill index missing identity: %s", text)
	}
	if strings.Contains(text, rule.Rule) {
		t.Fatalf("full rule body leaked into index: %s", text)
	}
	desc := learnSkillDescription(rule.Rule)
	if utf8.RuneCountInString(desc) > learnSkillDescriptionRunes {
		t.Fatalf("description is %d runes, want <= %d: %q", utf8.RuneCountInString(desc), learnSkillDescriptionRunes, desc)
	}
	if !strings.Contains(text, desc) {
		t.Fatalf("skill index missing compact description %q: %s", desc, text)
	}

	body, err := approvedLearnSkillBodies(product, rule.ProposalID, "")
	if err != nil {
		t.Fatal(err)
	}
	bodyText := string(body)
	for _, want := range []string{
		"## When to apply", "## Procedure", "## Pitfalls", "## Verification",
		rule.Trigger, rule.Rule, rule.CheckSpec, rule.TestCase, rule.VerificationReceipt,
	} {
		if !strings.Contains(bodyText, want) {
			t.Fatalf("skill body missing %q:\n%s", want, bodyText)
		}
	}
}

func TestLearningHookInjectsIndexNotTypedBody(t *testing.T) {
	product := t.TempDir()
	stateDir := t.TempDir()
	session := "skill-index-session"
	seedActiveLearningSession(t, product, stateDir, session)
	rule := seedTypedActiveLearnSkill(t, product)

	res, err := handleLearningContext(hookInput{SessionID: session, HookEventName: "UserPromptSubmit"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Context, "SKILL INDEX") || !strings.Contains(res.Context, rule.ProposalID) {
		t.Fatalf("hook context missing skill index: %q", res.Context)
	}
	if strings.Contains(res.Context, rule.Rule) {
		t.Fatalf("hook context leaked full typed rule body: %q", res.Context)
	}
}

func TestLearnSkillBodiesSelectByClassAndRejectInactive(t *testing.T) {
	product := t.TempDir()
	rule := seedTypedActiveLearnSkill(t, product)
	body, err := approvedLearnSkillBodies(product, "", rule.Class)
	if err != nil || !strings.Contains(string(body), rule.ProposalID) {
		t.Fatalf("class lookup body=%q err=%v", body, err)
	}

	paths := learnPaths(product)
	rule.Status = learnRuleStale
	raw, _ := json.Marshal(rule)
	if err := os.WriteFile(filepath.Join(paths.RulesDir, rule.ProposalID+".json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := approvedLearnSkillBodies(product, rule.ProposalID, ""); err == nil {
		t.Fatal("STALE rule must not load as active learned skill body")
	}
}
