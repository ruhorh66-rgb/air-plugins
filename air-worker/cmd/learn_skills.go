package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const learnSkillDescriptionRunes = 60

func learnSkillDescription(rule string) string {
	text := strings.Join(strings.Fields(strings.TrimSpace(rule)), " ")
	runes := []rune(text)
	if len(runes) <= learnSkillDescriptionRunes {
		return text
	}
	if learnSkillDescriptionRunes <= 1 {
		return string(runes[:learnSkillDescriptionRunes])
	}
	return string(runes[:learnSkillDescriptionRunes-1]) + "…"
}

func renderLearnSkillIndex(rows []learnRuleRecord, legacyAvailable bool) []byte {
	if len(rows) == 0 && !legacyAvailable {
		return nil
	}
	var b strings.Builder
	b.WriteString("# AirWorker verified learned-skill index\n\n")
	b.WriteString("Load a typed skill body on demand with `air-worker learn context -product <root> -id <proposal-id>` or `-class <class>`.\n\n")
	for _, row := range rows {
		fmt.Fprintf(&b, "- [%s] %s — %s\n", row.ProposalID, row.Class, learnSkillDescription(row.Rule))
	}
	if legacyAvailable {
		b.WriteString("- [legacy-ledger] legacy rules — verified legacy body available with `air-worker learn context -product <root> -legacy`\n")
	}
	return []byte(b.String())
}

func approvedLearnSkillIndex(product string) ([]byte, error) {
	paths := learnPaths(product)
	typed, found, err := readActiveLearnRuleRecords(paths)
	if err != nil {
		return nil, err
	}
	legacy, legacyErr := approvedLegacyLearnRules(product)
	if legacyErr != nil && !errors.Is(legacyErr, os.ErrNotExist) {
		return nil, legacyErr
	}
	legacyAvailable := legacyErr == nil && len(legacy) > 0
	out := renderLearnSkillIndex(typed, legacyAvailable)
	if len(out) == 0 && !found {
		return nil, os.ErrNotExist
	}
	return out, nil
}

func renderLearnSkillBody(rule learnRuleRecord) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", rule.Class)
	fmt.Fprintf(&b, "Proposal: %s\n", rule.ProposalID)
	fmt.Fprintf(&b, "Description: %s\n\n", learnSkillDescription(rule.Rule))
	b.WriteString("## When to apply\n\n")
	b.WriteString(strings.TrimSpace(rule.Trigger) + "\n\n")
	b.WriteString("## Procedure\n\n")
	b.WriteString(strings.TrimSpace(rule.Rule) + "\n\n")
	b.WriteString("## Pitfalls\n\n")
	b.WriteString("Do not bypass LPR approval, declared judges, safety controls, or release gates.\n\n")
	b.WriteString("## Verification\n\n")
	fmt.Fprintf(&b, "- type: %s\n", rule.CheckType)
	fmt.Fprintf(&b, "- spec: %s\n", rule.CheckSpec)
	fmt.Fprintf(&b, "- test: %s\n", rule.TestCase)
	fmt.Fprintf(&b, "- receipt: %s\n", rule.VerificationReceipt)
	return []byte(b.String())
}

func approvedLearnSkillBodies(product, proposalID, class string) ([]byte, error) {
	rows, _, err := readActiveLearnRuleRecords(learnPaths(product))
	if err != nil {
		return nil, err
	}
	proposalID = strings.TrimSpace(proposalID)
	class = strings.TrimSpace(class)
	var matched []learnRuleRecord
	for _, row := range rows {
		if proposalID != "" && row.ProposalID == proposalID {
			matched = append(matched, row)
			continue
		}
		if proposalID == "" && class != "" && strings.EqualFold(row.Class, class) {
			matched = append(matched, row)
		}
	}
	if len(matched) == 0 {
		if proposalID != "" {
			return nil, fmt.Errorf("active learned skill not found: %s", proposalID)
		}
		return nil, fmt.Errorf("active learned skill class not found: %s", class)
	}
	var out []byte
	for i, row := range matched {
		if i > 0 {
			out = append(out, []byte("\n---\n\n")...)
		}
		out = append(out, renderLearnSkillBody(row)...)
	}
	return out, nil
}
