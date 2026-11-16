package main

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func runReviewerAdapterOnce(t *testing.T, product, packet, answer string, sawPrompt *string) (int, string) {
	t.Helper()
	saved := learningReviewerInvoke
	learningReviewerInvoke = func(root, prompt, outputSchema, model, effort string) (string, error) {
		if sawPrompt != nil {
			*sawPrompt = prompt
		}
		return answer, nil
	}
	defer func() { learningReviewerInvoke = saved }()
	input, err := os.CreateTemp(t.TempDir(), "packet-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	_, _ = input.WriteString(packet)
	_, _ = input.Seek(0, 0)
	stdin := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = stdin }()
	return captureLoopOutput(t, func() int { return cmdLearningReviewerAdapter([]string{"-product", product}) })
}

// A3.1: a new lesson of a class that already has an applied procedure
// patches that procedure; it never adds a second digest file of the class.
func TestA3NewLessonOfExistingClassPatchesProcedure(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	target := learningProcedureTarget(settings.ManagedSkillPrefix, "feedback-error")
	if res, err := executeSharedLearning(product, settings, "propose", map[string]string{
		"proposal_id": "LP-a3-prior", "kind": "procedure", "target": target, "pre_sha256": "", "content": n102PriorCLI,
	}); err != nil || res.Status != "applied" {
		t.Fatalf("seed: %+v %v", res, err)
	}
	sha := learnSHA([]byte(n102PriorCLI))

	code, out := runReviewerAdapterOnce(t, product, `{"event":{"class":"feedback-error"}}`, reuseAnswer(n102NewCLI, "", ""), nil)
	if code != 0 {
		t.Fatalf("code %d output %s", code, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if got["target"] != target || got["pre_sha256"] != sha {
		t.Fatalf("lesson of an existing class created a new procedure: %#v", got)
	}

	// A class without a procedure still gets its first one.
	code, out = runReviewerAdapterOnce(t, product, `{"event":{"class":"other-class"}}`, reuseAnswer(n102NewCLI, "", ""), nil)
	if code != 0 {
		t.Fatalf("code %d output %s", code, out)
	}
	got = nil
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	if got["pre_sha256"] != "" || !strings.Contains(got["target"], "other-class") {
		t.Fatalf("first lesson of a new class not created: %#v", got)
	}
}

func TestA3FamilyTargetSkipsProtectedAndNativeLessons(t *testing.T) {
	s := sharedLearningSettings{ManagedSkillPrefix: "skills/learned", ProtectedTargets: []string{"skills/learned/x-protected.md"}}
	class := "skills/learned/x.md"
	skills := []reviewerSkill{
		{sharedLearningCatalogSkill{Target: "skills/learned/x-protected.md", SHA256: "p"}, "plain"},
		{sharedLearningCatalogSkill{Target: "skills/learned/x-native.md", SHA256: "n"}, "# AirWorker Native Action\n"},
		{sharedLearningCatalogSkill{Target: "skills/learned/xy-other.md", SHA256: "o"}, "plain"},
	}
	if target, sha := reviewerFamilyTarget(s, class, skills); target != "" || sha != "" {
		t.Fatalf("ineligible or foreign target chosen: %s %s", target, sha)
	}
	skills = append(skills, reviewerSkill{sharedLearningCatalogSkill{Target: "skills/learned/x-abc.md", SHA256: "a"}, "plain"})
	if target, sha := reviewerFamilyTarget(s, class, skills); target != "skills/learned/x-abc.md" || sha != "a" {
		t.Fatalf("eligible family procedure not chosen: %s %s", target, sha)
	}
}

// A3.2: the reviewer does not receive "judge not configured" as lesson
// material; any other verdict reaches it unchanged.
func TestA3UnconfiguredJudgeVerdictIsNotLessonMaterial(t *testing.T) {
	product, _, _ := selfLearningFixture(t)
	none := `{"apply":false,"content":"","update_target":"","update_pre_sha256":""}`
	var prompt string
	packet := `{"event":{"class":"air-worker-cli-failure"},"verdict":{"status":"unjudged","reason":"judge not configured","verdict_id":"VD-x"}}`
	if code, out := runReviewerAdapterOnce(t, product, packet, none, &prompt); code != 0 {
		t.Fatalf("code %d output %s", code, out)
	}
	if strings.Contains(prompt, "judge not configured") || strings.Contains(prompt, "VD-x") || !strings.Contains(prompt, `"configured": false`) {
		t.Fatalf("unconfigured-judge verdict reached the reviewer as material:\n%s", prompt)
	}
	for _, kept := range []string{
		`{"status":"fail","reason":"criterion K1 failed"}`,
		`{"status":"unjudged","reason":"judge unavailable"}`,
	} {
		prompt = ""
		if code, out := runReviewerAdapterOnce(t, product, `{"event":{"class":"c"},"verdict":`+kept+`}`, none, &prompt); code != 0 {
			t.Fatalf("code %d output %s", code, out)
		}
		var v map[string]string
		_ = json.Unmarshal([]byte(kept), &v)
		if !strings.Contains(prompt, v["reason"]) {
			t.Fatalf("real verdict %s was hidden from the reviewer", kept)
		}
	}
}
