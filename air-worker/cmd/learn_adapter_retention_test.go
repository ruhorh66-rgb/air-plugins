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
	learningReviewerInvoke = func(root, prompt, outputSchema, model, effort string) (string, error) {
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
// Before A3.1 this test asserted that a new lesson of an existing class was
// always a NEW file (the reviewer never touched a prior skill). LPR decision
// 04.11.2026 ("при совпадающей теме сначала обновить существующий навык") and
// AIR_VIBECODING invariant 15 (apply with ledger SHA before/after, one-command
// rollback) replace that: the lesson patches the class procedure through the
// standard propose with pre_sha256, the prior version stays in the ledger, and
// rollback restores it. Skills of other classes are never touched.
func TestN102LearningAutoReviewerPatchesClassProcedureWithRollback(t *testing.T) {
	product, config, _ := selfLearningFixture(t)
	seedTarget := "skills/learned/execution-unknown-no-blind-retry-6b72186f.md"
	seedPath := filepath.Join(product, filepath.FromSlash(seedTarget))
	seedBefore, err := os.ReadFile(seedPath)
	if err != nil {
		t.Fatal(err)
	}
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
	code, text := invokeN102Reviewer(t, product, n102Answer(n102NewCLI))
	if code != 0 {
		t.Fatalf("reviewer failed: %s", text)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatal(err)
	}
	if got["target"] != oldTarget || got["pre_sha256"] != learnSHA(originalBytes) {
		t.Fatalf("lesson of the same class did not patch the class procedure: %#v", got)
	}
	result, err := executeSharedLearning(product, config, "propose", map[string]string{
		"proposal_id": "LP-n102-patch", "kind": "procedure", "target": got["target"],
		"pre_sha256": got["pre_sha256"], "content": got["content"],
	})
	if err != nil || result.Status != "applied" {
		t.Fatalf("patch of the class procedure failed: %#v %v", result, err)
	}
	patched, err := os.ReadFile(oldPath)
	if err != nil || string(patched) != got["content"] {
		t.Fatalf("patch not applied: %v", err)
	}
	if roll, err := executeSharedLearning(product, config, "rollback", map[string]string{"proposal_id": "LP-n102-patch"}); err != nil || roll.Status == "" {
		t.Fatalf("one-command rollback failed: %+v %v", roll, err)
	}
	restored, err := os.ReadFile(oldPath)
	if err != nil || string(restored) != string(originalBytes) {
		t.Fatalf("rollback did not restore the prior procedure: %v", err)
	}
	seedAfter, err := os.ReadFile(seedPath)
	if err != nil || string(seedBefore) != string(seedAfter) {
		t.Fatalf("procedure of another class was touched: %v", err)
	}
}

// What N102 protected against stays impossible: an existing procedure cannot
// be replaced without the exact pre_sha256 of its current bytes, and every
// accepted change is a ledger transaction.
func TestN102NoTraceOverwriteOfExistingProcedureIsImpossible(t *testing.T) {
	product, config, _ := selfLearningFixture(t)
	target := learningProcedureTarget(config.ManagedSkillPrefix, "feedback-error")
	if res, err := executeSharedLearning(product, config, "propose", map[string]string{
		"proposal_id": "LP-n102-base", "kind": "procedure", "target": target, "pre_sha256": "", "content": n102PriorCLI,
	}); err != nil || res.Status != "applied" {
		t.Fatalf("seed: %+v %v", res, err)
	}
	path := filepath.Join(product, filepath.FromSlash(target))
	before, _ := os.ReadFile(path)
	for name, pre := range map[string]string{"no pre_sha256": "", "wrong pre_sha256": strings.Repeat("0", 64)} {
		res, err := executeSharedLearning(product, config, "propose", map[string]string{
			"proposal_id": "LP-n102-blind-" + strings.ReplaceAll(name, " ", "-"), "kind": "procedure",
			"target": target, "pre_sha256": pre, "content": n102NewCLI,
		})
		if err == nil && res.Status == "applied" {
			t.Fatalf("%s: existing procedure overwritten without proof of its bytes: %+v", name, res)
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(before) {
			t.Fatalf("%s: procedure bytes changed", name)
		}
	}
	// The reviewer adapter itself never emits a patch without pre_sha256.
	code, text := invokeN102Reviewer(t, product, n102Answer(n102NewCLI))
	var got map[string]string
	if code != 0 || json.Unmarshal([]byte(text), &got) != nil || got["target"] != target || got["pre_sha256"] != learnSHA(before) {
		t.Fatalf("adapter patch lacks the current pre_sha256: %d %s", code, text)
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
