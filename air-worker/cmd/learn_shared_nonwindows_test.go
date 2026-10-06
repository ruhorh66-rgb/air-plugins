//go:build !windows

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedContextRejectsParentSwapAroundOpen(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	target := "skills/learned/nonwindows-race.md"
	content := sharedFixtureProcedure + "\nNon-Windows handle confinement marker.\n"
	if _, err := executeSharedLearning(product, s, "propose", map[string]string{
		"proposal_id": "LP-nonwindows-race",
		"kind":        "procedure",
		"target":      target,
		"pre_sha256":  "",
		"content":     content,
	}); err != nil {
		t.Fatal(err)
	}

	index, err := executeSharedLearning(product, s, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(index.Data, &catalog); err != nil || len(catalog.Skills) != 1 {
		t.Fatalf("catalog: %v %s", err, index.Data)
	}

	learned := filepath.Join(product, "skills", "learned")
	saved := learned + "-saved"
	external := t.TempDir()
	externalTarget := filepath.Join(external, "nonwindows-race.md")
	if err := os.WriteFile(externalTarget, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	swapped := false
	reader := func(root, rel string, limit int64) ([]byte, error) {
		return readSharedContextSkillWithHook(root, rel, limit, func(stage string) error {
			switch stage {
			case "before-open":
				if err := os.Rename(learned, saved); err != nil {
					return err
				}
				if err := os.Symlink(external, learned); err != nil {
					_ = os.Rename(saved, learned)
					return err
				}
				swapped = true
			case "after-open":
				if err := os.Remove(learned); err != nil {
					return err
				}
				if err := os.Rename(saved, learned); err != nil {
					return err
				}
				swapped = false
			}
			return nil
		})
	}
	t.Cleanup(func() {
		if swapped {
			_ = os.Remove(learned)
			_ = os.Rename(saved, learned)
		}
	})

	res, err := sharedLearningContextFromCatalogWithReader(
		product,
		s,
		hookInput{SessionID: "nonwindows-race", Principal: "gpt"},
		"nonwindows-race-run",
		catalog.Skills,
		reader,
	)
	if err == nil {
		t.Fatalf("parent swap around open must fail closed, context=%q", res.Context)
	}

	rows := sharedEventRows(t, s)
	loaded, failed := 0, 0
	for _, row := range rows {
		switch row["kind"] {
		case "skill_loaded":
			loaded++
		case "skill_delivery_failed":
			failed++
		}
	}
	if loaded != 0 || failed != 1 {
		t.Fatalf("race receipt mismatch: loaded=%d failed=%d rows=%v", loaded, failed, rows)
	}
}
