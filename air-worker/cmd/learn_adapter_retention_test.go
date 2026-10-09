package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const n102PriorCLI = "# Keep CLI command discovery and routing self-consistent\n\n" +
	"## When to apply\nWhen a CLI exposes shared or legacy subcommands.\n\n" +
	"## Procedure\n1. Define one CLI contract and verify help, flags, and typed errors.\n\n" +
	"## Pitfalls\nDo not advertise commands the parser rejects.\n"
const n102SelfOwnerLesson = "# Verify persistent self-learning ownership\n\n" +
	"## When to apply\nA product uses shared learning but its own self-owner is absent.\n\n" +
	"## Procedure\n1. Inspect the installed binary and compare self-owner vs product-scoped status.\n" +
	"2. Record outcome evidence; never claim loaded guidance was used.\n\n" +
	"## Pitfalls\nDo not infer ownership from product-scoped status.\n"
const n102NewCLI = "# Reconcile missing product flag diagnostics\n\n" +
	"## When to apply\nA shared CLI action is recognized but -product is absent.\n\n" +
	"## Procedure\n1. Report the missing required flag instead of unknown action.\n\n" +
	"## Pitfalls\nDo not route into the legacy writer.\n"

func invokeN102Reviewer(t *testing.T, product string, response string) (int, string) {
	t.Helper()
	saved := learningReviewerInvoke
	learningReviewerInvoke = func(root, prompt, model, effort string) (string, error) {
		if root != product || !strings.Contains(prompt, n102PriorCLI[:55]) {
			t.Fatalf("old learning not supplied or wrong root")
		}
		return response, nil
	}
	defer func() { learningReviewerInvoke = saved }()
	input, err := os.CreateTemp(t.TempDir(), "review-event-*.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := input.WriteString(`{"event":{"event_id":"EV-n102","class":"feedback-error","kind":"run_completed"}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := input.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous }()
	return captureLoopOutput(t, func() int {
		return cmdLearningReviewerAdapter([]string{"-product", product})
	})
}
func n102Answer(content string) string {
	b, _ := json.Marshal(map[string]any{"apply": true, "content": content})
	return string(b)
}
func TestN102LearningAutoReviewerNeverOverwritesPriorSkill(t *testing.T) {
	product, config := sharedProductFixture(t, false)
	oldTarget := learningProcedureTarget(config.ManagedSkillPrefix, "feedback-error")
	old, err := executeSharedLearning(product, config, "propose", map[string]string{
		"proposal_id": "LP-n102-legacy", "kind": "procedure", "target": oldTarget,
		"pre_sha256": "", "content": n102PriorCLI,
	})
	if err != nil || old.Status != "applied" {
		t.Fatalf("old learning failed: %+v %v", old, err)
	}
	oldPath := filepath.Join(product, filepath.FromSlash(oldTarget))
	originalBytes, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, candidate := range []string{n102SelfOwnerLesson, n102NewCLI} {
		code, text := invokeN102Reviewer(t, product, n102Answer(candidate))
		if code != 0 {
			t.Fatalf("reviewer #%d failed: %s", i, text)
		}
		var got map[string]string
		if err := json.Unmarshal([]byte(text), &got); err != nil {
			t.Fatal(err)
		}
		wantSHA := learnSHA([]byte(strings.TrimSpace(candidate) + "\n"))
		if got["target"] == oldTarget || got["pre_sha256"] != "" ||
			!strings.Contains(got["target"], wantSHA[:16]) {
			t.Fatalf("unsafe proposed overwrite: %#v", got)
		}
		result, err := executeSharedLearning(product, config, "propose", map[string]string{
			"proposal_id": "LP-n102-new-" + string(rune('a'+i)),
			"kind":        "procedure", "target": got["target"],
			"pre_sha256": got["pre_sha256"], "content": got["content"],
		})
		if err != nil || result.Status != "applied" {
			t.Fatalf("new distinct procedure failed: %#v %v", result, err)
		}
		retained, err := os.ReadFile(oldPath)
		if err != nil || string(retained) != string(originalBytes) {
			t.Fatalf("prior guidance erased in review #%d", i)
		}
		repeatCode, repeatText := invokeN102Reviewer(t, product, n102Answer(candidate))
		if repeatCode != 0 || strings.TrimSpace(repeatText) != "{}" {
			t.Fatalf("duplicate proposal not suppressed: %d %s", repeatCode, repeatText)
		}
	}
	skills, err := executeSharedLearning(product, config, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []struct {
			Target string `json:"target"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(skills.Data, &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Skills) != 3 {
		t.Fatalf("expected prior and 2 distinct skills, got %d", len(catalog.Skills))
	}
}
func TestN102NoCandidateLeavesPriorKnowledgeUnchanged(t *testing.T) {
	product, config := sharedProductFixture(t, false)
	target := learningProcedureTarget(config.ManagedSkillPrefix, "feedback-error")
	_, err := executeSharedLearning(product, config, "propose", map[string]string{
		"proposal_id": "LP-n102-existing", "kind": "procedure", "target": target,
		"pre_sha256": "", "content": n102PriorCLI,
	})
	if err != nil {
		t.Fatal(err)
	}
	code, text := invokeN102Reviewer(t, product, `{"apply":false,"content":""}`)
	if code != 0 || strings.TrimSpace(text) != "{}" {
		t.Fatalf("no_candidate failed: %d %s", code, text)
	}
	b, err := os.ReadFile(filepath.Join(product, filepath.FromSlash(target)))
	if err != nil || string(b) != n102PriorCLI {
		t.Fatalf("old knowledge changed: %v", err)
	}
}
