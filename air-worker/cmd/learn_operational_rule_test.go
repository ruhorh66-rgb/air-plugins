package main

import (
	"strings"
	"testing"
)

func operationalProposal(id, class, rule, trigger, testCase string) learnProposal {
	return learnProposal{
		Schema:    learnSchemaVersion,
		ID:        id,
		CreatedAt: "2026-09-29T15:00:00Z",
		Status:    learnPending,
		Class:     class,
		Rule:      rule,
		Trigger:   trigger,
		CheckType: "gate",
		CheckSpec: learnCheckOperationalProcedure,
		TestCase:  testCase,
	}
}

func TestOperationalProcedureVerifier(t *testing.T) {
	p := operationalProposal(
		"LP-op-proc",
		"process-stall-diagnosis",
		"Inspect PID, age, descendants and completed output before retrying.",
		"A tool process is silent or appears stuck.",
		"silent process -> inspect process state before any retry",
	)
	receipt, err := verifyOperationalProcedure(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(receipt, learnCheckOperationalProcedure) || !strings.Contains(receipt, "structural=pass") {
		t.Fatalf("unexpected receipt %q", receipt)
	}

	p.CheckType = "hook"
	if _, err := verifyOperationalProcedure(p); err == nil {
		t.Fatal("non-gate operational rule must be rejected")
	}
}

func TestOperationalProcedureAppliesAndAppearsInSkillIndex(t *testing.T) {
	product := t.TempDir()
	p := operationalProposal(
		"LP-stall-proc",
		"process-stall-diagnosis",
		"Inspect PID existence, age, descendants, CPU and completed output before retrying; never duplicate a heavy start because polling is quiet.",
		"A long-running process appears silent or the chat suspects a hang.",
		"no new output -> inspect original process and completion evidence before any retry",
	)
	if err := appendLearnJSON(learnPaths(product).Proposals, p); err != nil {
		t.Fatal(err)
	}
	grantLearnProposal(t, product, p.ID)
	if code := cmdLearnApply([]string{"-product", product, "-id", p.ID}); code != 0 {
		t.Fatalf("learn apply code=%d", code)
	}

	index, err := approvedLearnSkillIndex(product)
	if err != nil {
		t.Fatal(err)
	}
	text := string(index)
	if !strings.Contains(text, p.ID) || !strings.Contains(text, p.Class) {
		t.Fatalf("skill index missing applied rule: %s", text)
	}

	body, err := approvedLearnSkillBodies(product, p.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	bodyText := string(body)
	for _, want := range []string{"## When to apply", "## Procedure", learnCheckOperationalProcedure, "before any retry"} {
		if !strings.Contains(bodyText, want) {
			t.Fatalf("skill body missing %q: %s", want, bodyText)
		}
	}
}
