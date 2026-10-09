package main

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// The shared module's ledger-backed index is canonical but alphabetically
// ordered. SessionStart has a bounded eight-skill context window: without
// ledger recency a newly learned skill may never reach the next session.
// Reordering changes only delivery priority, never ledger ownership/SHA.
func prioritizeSharedSkills(runtimeRoot string, skills []sharedLearningCatalogSkill) ([]sharedLearningCatalogSkill, error) {
	order := map[string]int{}
	next := 0
	err := scanLearnJSONL(filepath.Join(runtimeRoot, "ledger.jsonl"), func(b []byte) error {
		var entry struct {
			Schema     string `json:"schema"`
			Kind       string `json:"kind"`
			Target     string `json:"target"`
			Status     string `json:"status"`
			PostSHA256 string `json:"post_sha256"`
		}
		if err := json.Unmarshal(b, &entry); err != nil {
			return err
		}
		if entry.Schema == "air.learning.ledger/v1" && entry.Kind == "procedure" && entry.Status == "applied" {
			next++
			key := strings.ToLower(filepath.ToSlash(entry.Target)) + "@" + strings.ToLower(entry.PostSHA256)
			order[key] = next
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	arranged := append([]sharedLearningCatalogSkill(nil), skills...)
	rank := func(s sharedLearningCatalogSkill) int {
		key := strings.ToLower(filepath.ToSlash(s.Target)) + "@" + strings.ToLower(s.SHA256)
		return order[key]
	}
	sort.SliceStable(arranged, func(i, j int) bool { return rank(arranged[i]) > rank(arranged[j]) })
	return arranged, nil
}
