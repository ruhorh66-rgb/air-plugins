package main

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestSharedPromptRelevancePrefersMatchingOldSkill(t *testing.T) {
	recent := sharedLearningCatalogSkill{
		Target: "skills/learned/native-judge-setup.md", Title: "Configure the native judge",
		SHA256: strings.Repeat("b", 64),
	}
	older := sharedLearningCatalogSkill{
		Target: "skills/learned/prior-lesson.md",
		Title:  "Reconcile execution unknown before retry",
		SHA256: strings.Repeat("a", 64),
	}
	input := []sharedLearningCatalogSkill{recent, older}
	got := prioritizeSharedSkillsForPrompt(input, "A command returned EXECUTION_UNKNOWN. Check before retry.")
	if got[0] != older || got[1] != recent {
		t.Fatalf("matching older skill not preferred: %+v", got)
	}
	if !reflect.DeepEqual(input, []sharedLearningCatalogSkill{recent, older}) {
		t.Fatal("relevance ranking modified the original newest-first list")
	}
}

func TestSharedPromptRelevanceFallsBackToTargetSlug(t *testing.T) {
	latest := sharedLearningCatalogSkill{Target: "skills/learned/other.md", Title: "General task"}
	older := sharedLearningCatalogSkill{Target: "skills/learned/encoding-gb2312.md", Title: ""}
	got := prioritizeSharedSkillsForPrompt([]sharedLearningCatalogSkill{latest, older},
		"Inspect encoding GB2312 before the next import")
	if got[0] != older {
		t.Fatalf("target metadata not used when title absent: %+v", got)
	}
}

func TestSharedPromptRelevancePreservesRecencyForEmptyOrGenericInput(t *testing.T) {
	latest := sharedLearningCatalogSkill{Target: "skills/learned/first.md", Title: "First skill"}
	older := sharedLearningCatalogSkill{Target: "skills/learned/second.md", Title: "Second skill"}
	input := []sharedLearningCatalogSkill{latest, older}
	for _, prompt := range []string{"", "air worker native skill failure", "a task with no matching detail"} {
		got := prioritizeSharedSkillsForPrompt(input, prompt)
		if !reflect.DeepEqual(got, input) {
			t.Fatalf("unexpected reordering for %q: %+v", prompt, got)
		}
	}
}

func TestSharedPromptRelevanceSupportsRussianTitleAndBoundedPrompt(t *testing.T) {
	first := sharedLearningCatalogSkill{Target: "skills/learned/other.md", Title: "Проверка стоимости"}
	older := sharedLearningCatalogSkill{Target: "skills/learned/encoding.md", Title: "Исправление кодировки отчёта"}
	input := []sharedLearningCatalogSkill{first, older}
	if got := prioritizeSharedSkillsForPrompt(input, "Перепроверь кодировки в отчёте"); got[0] != older {
		t.Fatalf("Cyrillic title not selected: %+v", got)
	}
	// A very long untrusted prompt cannot force scanning arbitrary trailing
	// text after the deliberately bounded prefix.
	long := strings.Repeat("x", maxLearningRelevancePromptBytes) + " кодировки"
	if got := prioritizeSharedSkillsForPrompt(input, long); !reflect.DeepEqual(got, input) {
		t.Fatalf("unbounded prompt tail changed selection: %+v", got)
	}
}

func TestSharedUserPromptDeliversRelevantOldSkillFromExistingModuleIndex(t *testing.T) {
	product, settings := sharedProductFixture(t, false)
	target := "skills/learned/older-guidance.md"
	for i := 0; i < 11; i++ {
		name := fmt.Sprintf("skills/learned/recent-workflow-%02d.md", i)
		body := fmt.Sprintf("# Generic workflow %d\n\n## When to apply\nWhen handling generic step %d.\n\n## Procedure\n1. Check step %d.\n\n## Pitfalls\nDo not invent results.\n", i, i, i)
		if i == 0 {
			name = target
			body = "# Resolve execution unknown before retry\n\n## When to apply\nWhen an execution result is unknown.\n\n## Procedure\n1. Inspect the original native receipt before retry.\n\n## Pitfalls\nDo not turn unknown into failure.\n"
		}
		res, err := executeSharedLearning(product, settings, "propose", map[string]string{
			"proposal_id": fmt.Sprintf("LP-prompt-relevance-%02d", i),
			"kind":        "procedure", "target": name, "pre_sha256": "", "content": body,
		})
		if err != nil || res.Status != "applied" {
			t.Fatalf("seed %d: %+v %v", i, res, err)
		}
	}
	in := hookInput{
		Principal: "claude", SessionID: "prompt-relevance", RunID: "relevant-session",
		HookEventName: "UserPromptSubmit", Prompt: "EXECUTION_UNKNOWN: reconcile before retry",
	}
	handled, res, err := sharedLearningContext(product, in)
	if !handled || err != nil {
		t.Fatalf("UserPromptSubmit context unavailable: handled=%v err=%v", handled, err)
	}
	if len(res.LoadedSkills) == 0 || res.LoadedSkills[0].Target != target {
		t.Fatalf("older matching skill was excluded by 8-skill window: %+v", res.LoadedSkills)
	}
	if !strings.Contains(res.Context, "Resolve execution unknown before retry") {
		t.Fatal("matching skill body was not delivered from existing native index")
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "skill_loaded"); got != 1 {
		t.Fatalf("context did not record one native loaded receipt: %d", got)
	}
}
