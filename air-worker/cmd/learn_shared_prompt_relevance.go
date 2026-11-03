package main

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Prompt relevance is a presentation priority within one already-authorized
// product catalog, never permission to load a foreign or unapproved skill.
// Like Hermes skills_list -> skill_view, use the existing title/target index
// first. Full bodies still come from the native SHA-checked context reader.
const (
	maxLearningRelevancePromptBytes = 4096
	maxLearningRelevanceTitleBytes  = 512
	maxLearningRelevanceTargetBytes = 1024
	maxLearningRelevanceTerms       = 96
)

var genericLearningTerms = map[string]bool{
	"air": true, "worker": true, "skills": true, "skill": true,
	"learned": true, "lesson": true, "lessons": true,
	"handle": true, "handling": true, "native": true,
	"procedure": true, "procedures": true, "reviewer": true,
	"error": true, "failure": true, "failures": true,
	"проверка": true, "ошибка": true, "навык": true,
	"процедура": true, "модуль": true,
}

// learningRelevanceTerms is bounded and lexical, not a model or a policy
// decision. Empty/generic requests keep the existing recency ordering.
func learningRelevanceTerms(input string, maxBytes int) map[string]struct{} {
	if len(input) > maxBytes {
		input = input[:maxBytes]
	}
	fields := strings.FieldsFunc(strings.ToLower(strings.ToValidUTF8(input, " ")),
		func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	terms := make(map[string]struct{})
	for _, field := range fields {
		if utf8.RuneCountInString(field) < 4 || genericLearningTerms[field] {
			continue
		}
		terms[field] = struct{}{}
		if len(terms) >= maxLearningRelevanceTerms {
			break
		}
	}
	return terms
}

// prioritizeSharedSkillsForPrompt moves relevant existing APPLIED procedures
// ahead of the prior newest-first list for an ordinary UserPromptSubmit event.
// Ties preserve recency; nonmatches remain eligible under the existing 8-skill
// and 16 KiB host-context bounds. No body/ledger is read and no event is minted.
func prioritizeSharedSkillsForPrompt(skills []sharedLearningCatalogSkill, prompt string) []sharedLearningCatalogSkill {
	promptTerms := learningRelevanceTerms(prompt, maxLearningRelevancePromptBytes)
	if len(promptTerms) == 0 || len(skills) < 2 {
		return skills
	}
	type ranked struct {
		skill sharedLearningCatalogSkill
		score int
	}
	ordered := make([]ranked, len(skills))
	anyMatch := false
	for i, skill := range skills {
		titleTerms := learningRelevanceTerms(skill.Title, maxLearningRelevanceTitleBytes)
		targetTerms := learningRelevanceTerms(filepath.Base(filepath.FromSlash(skill.Target)), maxLearningRelevanceTargetBytes)
		score := 0
		for term := range promptTerms {
			if _, ok := titleTerms[term]; ok {
				score += 3
			} else if _, ok := targetTerms[term]; ok {
				score++
			}
		}
		ordered[i] = ranked{skill: skill, score: score}
		if score > 0 {
			anyMatch = true
		}
	}
	if !anyMatch {
		return skills
	}
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].score > ordered[j].score })
	result := make([]sharedLearningCatalogSkill, len(ordered))
	for i, entry := range ordered {
		result[i] = entry.skill
	}
	return result
}
