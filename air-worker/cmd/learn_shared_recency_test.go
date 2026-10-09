package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestN102FreshLearningIsDeliveredBeforeOlderSkills(t *testing.T) {
	product, settings := sharedProductFixture(t, false)
	for i := 0; i < 11; i++ {
		target := fmt.Sprintf("skills/learned/lesson-%02d.md", i)
		body := fmt.Sprintf("# Lesson %d\n\n## When to apply\nFor operation %d.\n\n## Procedure\n1. Verify precise action %d.\n\n## Pitfalls\nDo not invent evidence.\n", i, i, i)
		res, err := executeSharedLearning(product, settings, "propose", map[string]string{
			"proposal_id": fmt.Sprintf("LP-n102-recency-%02d", i),
			"kind":        "procedure", "target": target, "pre_sha256": "", "content": body,
		})
		if err != nil || res.Status != "applied" {
			t.Fatalf("proposal %d: %v %+v", i, err, res)
		}
	}
	index, err := executeSharedLearning(product, settings, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(index.Data, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 11 {
		t.Fatalf("expected 11 skills, got %d", len(catalog.Skills))
	}
	// The module index is alphabetical and would otherwise omit lesson-10
	// from the eight-skill next-host context.
	if catalog.Skills[0].Target != "skills/learned/lesson-00.md" {
		t.Fatal("unexpected unsorted fixture")
	}
	ordered, err := prioritizeSharedSkills(settings.RuntimeRoot, catalog.Skills)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].Target != "skills/learned/lesson-10.md" || ordered[7].Target != "skills/learned/lesson-03.md" {
		t.Fatalf("newly reviewed skills not prioritized: first=%s eighth=%s", ordered[0].Target, ordered[7].Target)
	}
}

func TestN102FreshLearningOrderRejectsCorruptLedger(t *testing.T) {
	_, settings := sharedProductFixture(t, false)
	if err := os.MkdirAll(settings.RuntimeRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings.RuntimeRoot, "ledger.jsonl"), []byte("not-json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := prioritizeSharedSkills(settings.RuntimeRoot, []sharedLearningCatalogSkill{{Target: "skills/learned/a.md", SHA256: "abc"}})
	if err == nil {
		t.Fatal("corrupt ledger silently treated as no newer skills")
	}
}
