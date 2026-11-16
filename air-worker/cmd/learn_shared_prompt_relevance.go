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

// selectSharedSkillsForTurn is the per-turn delivery of A4: an ordinary
// UserPromptSubmit gets only the existing APPLIED procedures whose index
// title or target matches the prompt, most relevant first, ties in the prior
// newest-first order. A prompt that matches nothing gets no procedure, so a
// turn never carries procedures of an unrelated task class. The caller's
// 8-procedure and 16 KiB context limit still applies. No body or ledger is
// read and no event is minted here.
func selectSharedSkillsForTurn(skills []sharedLearningCatalogSkill, prompt string) []sharedLearningCatalogSkill {
	promptTerms := learningRelevanceTerms(prompt, maxLearningRelevancePromptBytes)
	if len(promptTerms) == 0 {
		return nil
	}
	type ranked struct {
		skill sharedLearningCatalogSkill
		score int
	}
	var matched []ranked
	for _, skill := range skills {
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
		if score > 0 {
			matched = append(matched, ranked{skill: skill, score: score})
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].score > matched[j].score })
	result := make([]sharedLearningCatalogSkill, len(matched))
	for i, entry := range matched {
		result[i] = entry.skill
	}
	return result
}
