package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const learnRuleSchemaVersion = "air-worker.learn.rule/v1"

var learnCheckTypes = map[string]bool{
	"hook":   true,
	"gate":   true,
	"script": true,
}

type learnRuleRecord struct {
	Schema              string `json:"schema"`
	ProposalID          string `json:"proposal_id"`
	Class               string `json:"class"`
	Rule                string `json:"rule"`
	Trigger             string `json:"trigger"`
	CheckType           string `json:"check_type"`
	CheckSpec           string `json:"check_spec"`
	TestCase            string `json:"test_case"`
	Status              string `json:"status"`
	ApprovedAt          string `json:"approved_at"`
	Approval            string `json:"approval"`
	VerifiedAt          string `json:"verified_at"`
	VerificationReceipt string `json:"verification_receipt"`
}

func cleanLearnSpecText(name, value string, max int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("-%s is required", name)
	}
	if len([]rune(value)) > max {
		return "", fmt.Errorf("-%s is too long", name)
	}
	return value, nil
}

func validateLearnProposalSpec(p learnProposal) error {
	if _, err := cleanLearnSpecText("trigger", p.Trigger, 4000); err != nil {
		return err
	}
	checkType := strings.ToLower(strings.TrimSpace(p.CheckType))
	if !learnCheckTypes[checkType] {
		return fmt.Errorf("invalid -check-type %q (want hook|gate|script)", p.CheckType)
	}
	if _, err := cleanLearnSpecText("check-spec", p.CheckSpec, 12000); err != nil {
		return err
	}
	if _, err := cleanLearnSpecText("test-case", p.TestCase, 12000); err != nil {
		return err
	}
	return nil
}

func validateLearnRuleRecord(r learnRuleRecord) error {
	if r.Schema != learnRuleSchemaVersion {
		return fmt.Errorf("invalid rule schema %q", r.Schema)
	}
	if strings.TrimSpace(r.ProposalID) == "" {
		return errors.New("rule proposal_id is required")
	}
	if _, err := cleanLearnText("class", r.Class); err != nil {
		return err
	}
	if _, err := cleanLearnText("rule", r.Rule); err != nil {
		return err
	}
	if err := validateLearnProposalSpec(learnProposal{
		Trigger: r.Trigger, CheckType: r.CheckType, CheckSpec: r.CheckSpec, TestCase: r.TestCase,
	}); err != nil {
		return err
	}
	switch r.Status {
	case learnApplied, learnRuleStale, learnRuleArchived:
	default:
		return fmt.Errorf("rule %s has invalid lifecycle status %q", r.ProposalID, r.Status)
	}
	if _, err := time.Parse(time.RFC3339Nano, r.ApprovedAt); err != nil {
		return fmt.Errorf("rule %s invalid approved_at: %w", r.ProposalID, err)
	}
	if strings.TrimSpace(r.Approval) == "" {
		return fmt.Errorf("rule %s approval evidence is required", r.ProposalID)
	}
	if _, err := time.Parse(time.RFC3339Nano, r.VerifiedAt); err != nil {
		return fmt.Errorf("rule %s invalid verified_at: %w", r.ProposalID, err)
	}
	if strings.TrimSpace(r.VerificationReceipt) == "" {
		return fmt.Errorf("rule %s verification_receipt is required", r.ProposalID)
	}
	return nil
}

func readLearnRuleRecords(paths learnPathsSet) ([]learnRuleRecord, bool, error) {
	entries, err := os.ReadDir(paths.RulesDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var out []learnRuleRecord
	found := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		found = true
		path := filepath.Join(paths.RulesDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, true, err
		}
		var rule learnRuleRecord
		if err := json.Unmarshal(raw, &rule); err != nil {
			return nil, true, fmt.Errorf("%s: %w", path, err)
		}
		if err := validateLearnRuleRecord(rule); err != nil {
			return nil, true, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProposalID < out[j].ProposalID })
	return out, found, nil
}

func readActiveLearnRuleRecords(paths learnPathsSet) ([]learnRuleRecord, bool, error) {
	rows, found, err := readLearnRuleRecords(paths)
	if err != nil {
		return nil, found, err
	}
	out := rows[:0]
	for _, row := range rows {
		if row.Status == learnApplied {
			out = append(out, row)
		}
	}
	return out, found, nil
}

func renderActiveLearnRuleRecords(rows []learnRuleRecord) []byte {
	if len(rows) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString("# AirWorker verified active rules\n\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- [%s] class=%s trigger=%s check=%s :: %s\n",
			row.ProposalID, row.Class, row.Trigger, row.CheckType, row.Rule)
	}
	return []byte(b.String())
}

func approvedLegacyLearnRules(product string) ([]byte, error) {
	paths := learnPaths(product)
	rules, err := os.ReadFile(paths.Rules)
	if err != nil {
		return nil, err
	}
	ledger, err := readLearnLedger(paths.Ledger)
	if err != nil {
		return nil, err
	}
	var last *learnLedgerRecord
	for i := len(ledger) - 1; i >= 0; i-- {
		if ledger[i].Action == "apply" || ledger[i].Action == "rollback" {
			last = &ledger[i]
			break
		}
	}
	if last == nil || last.AfterSHA256 == "" || last.AfterSHA256 != learnSHA(rules) {
		return nil, errLearnRulesUnapproved
	}
	return rules, nil
}

func approvedLearnRules(product string) ([]byte, error) {
	paths := learnPaths(product)
	typed, found, err := readActiveLearnRuleRecords(paths)
	if err != nil {
		return nil, err
	}
	typedText := renderActiveLearnRuleRecords(typed)

	legacy, legacyErr := approvedLegacyLearnRules(product)
	if legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) {
		return nil, legacyErr
	}
	if len(typedText) == 0 && (legacyErr == nil && len(legacy) > 0) {
		return legacy, nil
	}
	if len(typedText) == 0 && !found {
		return nil, os.ErrNotExist
	}
	if len(legacy) == 0 {
		return typedText, nil
	}
	var out []byte
	out = append(out, typedText...)
	out = append(out, []byte("\n# Legacy ledger-verified rules\n\n")...)
	out = append(out, legacy...)
	return out, nil
}
