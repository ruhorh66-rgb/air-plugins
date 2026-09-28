package main

import (
	"fmt"
	"strings"
	"time"
)

// executableLearnCheck is the runtime/compiler registry for learned checks.
// learn apply knows only (check_type, check_spec); concrete implementations register here.
// This keeps proposal approval/ledger/rollback generic and lets new checks be added without
// teaching the transaction layer about their class or semantics.
type executableLearnCheck struct {
	CheckType string
	CheckSpec string
	Verify    func(learnProposal) (string, error)
	PreTool   func(product string, rule learnRuleRecord, command string, now time.Time) (bool, string, error)
}

var executableLearnChecks = map[string]executableLearnCheck{}

func executableLearnKey(checkType, checkSpec string) string {
	return strings.ToLower(strings.TrimSpace(checkType)) + "\x00" + strings.TrimSpace(checkSpec)
}

func registerExecutableLearnCheck(check executableLearnCheck) {
	check.CheckType = strings.ToLower(strings.TrimSpace(check.CheckType))
	check.CheckSpec = strings.TrimSpace(check.CheckSpec)
	if check.CheckType == "" || check.CheckSpec == "" {
		panic("executable learn check requires type and spec")
	}
	key := executableLearnKey(check.CheckType, check.CheckSpec)
	if _, exists := executableLearnChecks[key]; exists {
		panic("duplicate executable learn check: " + check.CheckType + "/" + check.CheckSpec)
	}
	executableLearnChecks[key] = check
}

func executableLearnCheckFor(checkType, checkSpec string) (executableLearnCheck, error) {
	key := executableLearnKey(checkType, checkSpec)
	check, ok := executableLearnChecks[key]
	if !ok {
		return executableLearnCheck{}, fmt.Errorf("unsupported executable check %s/%s",
			strings.ToLower(strings.TrimSpace(checkType)), strings.TrimSpace(checkSpec))
	}
	if check.Verify == nil {
		return executableLearnCheck{}, fmt.Errorf("executable check %s/%s has no verifier", check.CheckType, check.CheckSpec)
	}
	return check, nil
}

func supportsExecutableLearnProposal(p learnProposal) error {
	_, err := executableLearnCheckFor(p.CheckType, p.CheckSpec)
	return err
}

func verifiedExecutableRule(p learnProposal, grant learnApprovalGrant, now time.Time) (learnRuleRecord, error) {
	check, err := executableLearnCheckFor(p.CheckType, p.CheckSpec)
	if err != nil {
		return learnRuleRecord{}, err
	}
	receipt, err := check.Verify(p)
	if err != nil {
		return learnRuleRecord{}, err
	}
	return learnRuleRecord{
		Schema:              learnRuleSchemaVersion,
		ProposalID:          p.ID,
		Class:               p.Class,
		Rule:                p.Rule,
		Trigger:             p.Trigger,
		CheckType:           strings.ToLower(strings.TrimSpace(p.CheckType)),
		CheckSpec:           strings.TrimSpace(p.CheckSpec),
		TestCase:            p.TestCase,
		Status:              learnApplied,
		ApprovedAt:          now.UTC().Format(time.RFC3339Nano),
		Approval:            "grant:" + grant.ID,
		VerifiedAt:          now.UTC().Format(time.RFC3339Nano),
		VerificationReceipt: receipt,
	}, nil
}

func enforceExecutableLearnHooks(product, command string, now time.Time) (bool, string, error) {
	rows, _, err := readActiveLearnRuleRecords(learnPaths(product))
	if err != nil {
		return false, "", err
	}
	for _, rule := range rows {
		if !strings.EqualFold(rule.CheckType, "hook") {
			continue
		}
		check, err := executableLearnCheckFor(rule.CheckType, rule.CheckSpec)
		if err != nil {
			return false, "", fmt.Errorf("active learned rule %s is not executable: %w", rule.ProposalID, err)
		}
		if check.PreTool == nil {
			return false, "", fmt.Errorf("active learned hook %s has no PreTool handler", rule.ProposalID)
		}
		blocked, reason, err := check.PreTool(product, rule, command, now)
		if err != nil || blocked {
			return blocked, reason, err
		}
	}
	return false, "", nil
}
