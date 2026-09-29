package main

import (
	"fmt"
	"strings"
)

const learnCheckOperationalProcedure = "operational-procedure-v1"

func verifyOperationalProcedure(p learnProposal) (string, error) {
	if !strings.EqualFold(strings.TrimSpace(p.CheckType), "gate") {
		return "", fmt.Errorf("operational procedure must use check_type=gate")
	}
	if strings.TrimSpace(p.CheckSpec) != learnCheckOperationalProcedure {
		return "", fmt.Errorf("operational procedure check_spec=%q want=%q", p.CheckSpec, learnCheckOperationalProcedure)
	}
	if strings.TrimSpace(p.Trigger) == "" || strings.TrimSpace(p.Rule) == "" || strings.TrimSpace(p.TestCase) == "" {
		return "", fmt.Errorf("operational procedure requires trigger, rule and testcase")
	}
	testCase := strings.TrimSpace(p.TestCase)
	if !strings.Contains(testCase, "->") && !strings.Contains(testCase, "→") {
		return "", fmt.Errorf("operational procedure testcase must state condition -> expected action")
	}
	return "builtin:" + learnCheckOperationalProcedure + ":v1 structural=pass", nil
}

func init() {
	registerExecutableLearnCheck(executableLearnCheck{
		CheckType: "gate",
		CheckSpec: learnCheckOperationalProcedure,
		Verify: func(p learnProposal) (string, error) {
			return verifyOperationalProcedure(p)
		},
	})
}
