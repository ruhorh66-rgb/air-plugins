package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	learnCheckSGTCommitFreshness = "sgt-commit-added-lines-within-10m"
	learnRuleVerifyBeforeClaim   = "verify-before-claim"
	learnSGTMaxDeltaMinutes      = 10
)

var (
	sgtLabelPattern     = regexp.MustCompile(`\b([01]\d|2[0-3]):([0-5]\d)\s+SGT\b`)
	gitCommitPattern    = regexp.MustCompile(`(?i)(^|[;&|]\s*)\s*git(?:\.exe)?(?:\s+-C\s+(?:"[^"]+"|'[^']+'|\S+))?\s+commit(?:\s|$)`)
	gitCommitAllPattern = regexp.MustCompile(`(?i)(?:^|\s)(?:-a|--all)(?:\s|$)`)
)

type sgtLabelViolation struct {
	Label string
	Delta int
	Line  string
}

func sgtMinuteOfDay(now time.Time) int {
	sgt := now.UTC().Add(8 * time.Hour)
	return sgt.Hour()*60 + sgt.Minute()
}

func circularMinuteDelta(a, b int) int {
	d := a - b
	if d < 0 {
		d = -d
	}
	if d > 720 {
		d = 1440 - d
	}
	return d
}

func staleSGTLabels(lines []string, now time.Time) []sgtLabelViolation {
	current := sgtMinuteOfDay(now)
	var out []sgtLabelViolation
	for _, line := range lines {
		for _, match := range sgtLabelPattern.FindAllStringSubmatch(line, -1) {
			hour, _ := strconv.Atoi(match[1])
			minute, _ := strconv.Atoi(match[2])
			delta := circularMinuteDelta(hour*60+minute, current)
			if delta > learnSGTMaxDeltaMinutes {
				out = append(out, sgtLabelViolation{Label: match[0], Delta: delta, Line: line})
			}
		}
	}
	return out
}

func verifySGTCommitCheck() (string, error) {
	locNow := time.Date(2026, 9, 28, 11, 20, 0, 0, time.UTC) // 19:20 SGT
	cases := []struct {
		lines []string
		now   time.Time
		block bool
	}{
		{[]string{"status 19:15 SGT"}, locNow, false},
		{[]string{"status 19:10 SGT"}, locNow, false},
		{[]string{"status 19:09 SGT"}, locNow, true},
		{[]string{"status 23:59 SGT"}, time.Date(2026, 9, 28, 16, 5, 0, 0, time.UTC), false}, // 00:05 SGT
	}
	for i, tc := range cases {
		got := len(staleSGTLabels(tc.lines, tc.now)) > 0
		if got != tc.block {
			return "", fmt.Errorf("SGT freshness testcase %d block=%v want=%v", i+1, got, tc.block)
		}
	}
	return "builtin:" + learnCheckSGTCommitFreshness + ":v1 cases=4/4", nil
}

func init() {
	registerExecutableLearnCheck(executableLearnCheck{
		CheckType: "hook",
		CheckSpec: learnCheckSGTCommitFreshness,
		Verify: func(learnProposal) (string, error) {
			return verifySGTCommitCheck()
		},
		PreTool: enforceSGTCommitRuleRecord,
	})
}

func executableRulePath(paths learnPathsSet, proposalID string) string {
	return filepath.Join(paths.RulesDir, proposalID+".json")
}

func ledgerRuleAbsolutePath(root, rulePath string) (string, error) {
	rel := filepath.Clean(filepath.FromSlash(strings.TrimSpace(rulePath)))
	if rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid ledger rule_path %q", rulePath)
	}
	abs := filepath.Join(root, rel)
	rootClean := filepath.Clean(root)
	relCheck, err := filepath.Rel(rootClean, abs)
	if err != nil || relCheck == ".." || strings.HasPrefix(relCheck, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("ledger rule_path escapes product: %q", rulePath)
	}
	return abs, nil
}

func applyVerifiedExecutableProposal(root string, paths learnPathsSet, proposals []learnProposal, idx int, proposal learnProposal, grant learnApprovalGrant, grantClaimPath, grantPath string) (bool, error) {
	now := time.Now().UTC()
	rule, err := verifiedExecutableRule(proposal, grant, now)
	if err != nil {
		return false, err
	}
	rulePath := executableRulePath(paths, proposal.ID)
	if _, err := os.Stat(rulePath); err == nil {
		return false, fmt.Errorf("active rule manifest already exists: %s", rulePath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	ruleBytes, err := json.MarshalIndent(rule, "", "  ")
	if err != nil {
		return false, err
	}
	ruleBytes = append(ruleBytes, '\n')
	if err := validateLearnRuleRecord(rule); err != nil {
		return false, err
	}
	originalProposals, err := os.ReadFile(paths.Proposals)
	if err != nil {
		return false, err
	}
	ledgerID, err := newLearnID("LM", now)
	if err != nil {
		return false, err
	}
	proposals[idx].Status = learnApplied
	proposals[idx].AppliedAt = now.Format(time.RFC3339Nano)
	proposals[idx].ApprovedAt = now.Format(time.RFC3339Nano)
	proposals[idx].LedgerID = ledgerID
	proposals[idx].Approval = "grant:" + grant.ID
	proposalBytes, err := marshalLearnJSONL(proposals)
	if err != nil {
		return false, err
	}
	relRulePath := filepath.ToSlash(filepath.Join("learn", "rules", proposal.ID+".json"))
	ledger := learnLedgerRecord{
		Schema: learnSchemaVersion, ID: ledgerID, CreatedAt: now.Format(time.RFC3339Nano),
		Action: "apply", ProposalID: proposal.ID, Class: proposal.Class,
		RulePath: relRulePath, BeforeExists: false, AfterSHA256: learnSHA(ruleBytes),
		GitHead: learnGitHead(root), Approval: "grant:" + grant.ID,
	}

	if err := writeLearnAtomic(rulePath, ruleBytes); err != nil {
		return false, err
	}
	rollbackRule := func() { _ = os.Remove(rulePath) }
	if err := writeLearnAtomic(paths.Proposals, proposalBytes); err != nil {
		rollbackRule()
		return false, fmt.Errorf("proposal transaction failed: %w", err)
	}
	if err := appendLearnJSON(paths.Ledger, ledger); err != nil {
		rollbackRule()
		_ = writeLearnAtomic(paths.Proposals, originalProposals)
		return false, fmt.Errorf("ledger transaction failed; mutation rolled back: %w", err)
	}
	if err := consumeLearnApprovalGrant(grantClaimPath, grantPath, grant, ledger.ID); err != nil {
		return true, fmt.Errorf("rule applied and ledgered, but approval grant could not be published as consumed: %w", err)
	}
	fmt.Printf("%s APPLIED executable=%s ledger=%s grant=%s receipt=%s\n", proposal.ID, proposal.CheckSpec, ledger.ID, grant.ID, rule.VerificationReceipt)
	return true, nil
}

func activeSGTCommitRule(product string) (*learnRuleRecord, error) {
	rows, _, err := readActiveLearnRuleRecords(learnPaths(product))
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].CheckSpec == learnCheckSGTCommitFreshness && rows[i].CheckType == "hook" {
			return &rows[i], nil
		}
	}
	return nil, nil
}

func addedLinesForCommit(product string, includeTrackedWorktree bool) ([]string, error) {
	args := []string{"-C", product, "diff"}
	if includeTrackedWorktree {
		args = append(args, "HEAD")
	} else {
		args = append(args, "--cached")
	}
	args = append(args, "--unified=0", "--no-color", "--diff-filter=ACMR")
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff for commit freshness: %w", err)
	}
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "+++") {
			continue
		}
		if strings.HasPrefix(line, "+") {
			lines = append(lines, strings.TrimPrefix(line, "+"))
		}
	}
	return lines, sc.Err()
}

func enforceSGTCommitRuleRecord(product string, rule learnRuleRecord, command string, now time.Time) (bool, string, error) {
	if !gitCommitPattern.MatchString(command) {
		return false, "", nil
	}
	lines, err := addedLinesForCommit(product, gitCommitAllPattern.MatchString(command))
	if err != nil {
		return false, "", err
	}
	violations := staleSGTLabels(lines, now)
	evidence := fmt.Sprintf("check_spec=%s added_lines=%d violations=%d", rule.CheckSpec, len(lines), len(violations))
	if len(violations) == 0 {
		if err := recordLearnRuleUse(product, rule, "pass", evidence, now); err != nil {
			return false, "", fmt.Errorf("record rule use: %w", err)
		}
		return false, "", nil
	}
	v := violations[0]
	if err := recordLearnRuleUse(product, rule, "block", evidence, now); err != nil {
		return false, "", fmt.Errorf("record rule use: %w", err)
	}
	return true, fmt.Sprintf("%s: метка %s расходится с текущим SGT на %d мин (> %d); возьми время из curator-check перед коммитом", rule.ProposalID, v.Label, v.Delta, learnSGTMaxDeltaMinutes), nil
}

// Compatibility helper for focused tests and callers that ask specifically for the SGT rule.
// The live hook path dispatches every registered hook through enforceExecutableLearnHooks.
func enforceSGTCommitRule(product, command string, now time.Time) (bool, string, error) {
	rule, err := activeSGTCommitRule(product)
	if err != nil || rule == nil {
		return false, "", err
	}
	return enforceSGTCommitRuleRecord(product, *rule, command, now)
}
