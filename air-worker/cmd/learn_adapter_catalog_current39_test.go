package main

import (
	"fmt"
	"strings"
	"testing"
)

// The active AirWorker self-owner has 39 verified APPLIED procedures. The
// accepted frozen reviewer already handles 37 and rejects >64; this fixture
// prevents a later rc.3 source integration from silently restoring the old
// 32-item limit or pruning exact identities before Codex review.
func TestLearningReviewerCatalogueAllowsCompleteCurrent39(t *testing.T) {
	product, settings := sharedProductFixture(t, false)
	classTarget := learningProcedureTarget(settings.ManagedSkillPrefix, "external-owner-lessons")
	family := strings.TrimSuffix(classTarget, ".md")
	expected := make(map[string]bool, 39)
	for i := 0; i < 39; i++ {
		target := fmt.Sprintf("%s-%02d.md", family, i)
		reuseSeed(t, product, settings, fmt.Sprintf("LP-current39-%02d", i), target, n102PriorCLI)
		expected[target] = true
	}

	beforeLoaded := sharedUsageCount(t, settings.RuntimeRoot, "skill_loaded")
	beforeUsed := sharedUsageCount(t, settings.RuntimeRoot, "procedure_used")
	skills, err := reviewerCatalog(product, settings, classTarget)
	if err != nil {
		t.Fatalf("39 verified APPLIED procedures rejected before reviewer: %v", err)
	}
	if len(skills) != 39 {
		t.Fatalf("incomplete or truncated APPLIED catalogue: got %d want 39", len(skills))
	}

	trustedSHA := learnSHA([]byte(n102PriorCLI))
	prompt := buildLearningReviewerPromptWithSettings(
		classTarget, skills, []byte(`{"event":{"class":"external-owner-lessons"}}`), settings,
	)
	for _, skill := range skills {
		if !expected[skill.Target] || skill.SHA256 != trustedSHA || skill.Body != n102PriorCLI {
			t.Fatalf("unverified or unrelated procedure included: %q", skill.Target)
		}
		if !strings.Contains(prompt, "Target: "+skill.Target) ||
			!strings.Contains(prompt, "SHA256: "+skill.SHA256) {
			t.Fatalf("verified APPLIED procedure omitted from reviewer prompt: %q", skill.Target)
		}
		delete(expected, skill.Target)
	}
	if len(expected) != 0 {
		t.Fatalf("reviewer lost %d exact APPLIED identities", len(expected))
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "skill_loaded"); got != beforeLoaded {
		t.Fatal("read-only reviewer catalogue inspection claimed a new loaded procedure")
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "procedure_used"); got != beforeUsed {
		t.Fatal("read-only reviewer catalogue inspection claimed procedure use")
	}
}
