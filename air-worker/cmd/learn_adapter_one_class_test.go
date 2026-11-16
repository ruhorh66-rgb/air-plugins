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

// A3.1: a lesson of a class that already has an applied procedure must update
// that procedure; an answer that ignores it is refused, never silently turned
// into a replacement of the old guidance and never a second file of the class.
func TestA3NewLessonOfExistingClassPatchesProcedure(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	target := learningProcedureTarget(settings.ManagedSkillPrefix, "feedback-error")
	if res, err := executeSharedLearning(product, settings, "propose", map[string]string{
		"proposal_id": "LP-a3-prior", "kind": "procedure", "target": target, "pre_sha256": "", "content": n102PriorCLI,
	}); err != nil || res.Status != "applied" {
		t.Fatalf("seed: %+v %v", res, err)
	}
	sha := learnSHA([]byte(n102PriorCLI))

	var prompt string
	code, out := runReviewerAdapterOnce(t, product, `{"event":{"class":"feedback-error"}}`, reuseAnswer(n102NewCLI, "", ""), &prompt)
	if code != 2 || strings.Contains(out, `"kind"`) {
		t.Fatalf("answer ignoring the class procedure was not refused: %d %s", code, out)
	}
	if !strings.Contains(prompt, "One class has one procedure") {
		t.Fatal("reviewer prompt does not require updating the class procedure")
	}
	if reason := learningFailureReason([]byte("learning reviewer: class procedure exists; update it instead of adding a new one\n")); reason != "reviewer_class_update_required" {
		t.Fatalf("reason %q", reason)
	}

	merged := n102PriorCLI + "\n" + strings.TrimSpace(n102NewCLI) + "\n"
	code, out = runReviewerAdapterOnce(t, product, `{"event":{"class":"feedback-error"}}`, reuseAnswer(merged, target, sha), nil)
	if code != 0 {
		t.Fatalf("code %d output %s", code, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output %q: %v", out, err)
	}
	if got["target"] != target || got["pre_sha256"] != sha || !strings.Contains(got["content"], "Define one CLI contract") {
		t.Fatalf("class update lost the target, its SHA or the old guidance: %#v", got)
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
	// Another class whose name starts with this class's slug is not family,
	// nor is a short or malformed digest.
	skills = append(skills,
		reviewerSkill{sharedLearningCatalogSkill{Target: "skills/learned/x-0123456789abcdef-y.md", SHA256: "c"}, "plain"},
		reviewerSkill{sharedLearningCatalogSkill{Target: "skills/learned/x-abc.md", SHA256: "s"}, "plain"},
		reviewerSkill{sharedLearningCatalogSkill{Target: "skills/learned/x-0123456789ABCDEF.md", SHA256: "u"}, "plain"},
	)
	if target, sha := reviewerFamilyTarget(s, class, skills); target != "" || sha != "" {
		t.Fatalf("another class taken as family: %s %s", target, sha)
	}
	skills = append(skills, reviewerSkill{sharedLearningCatalogSkill{Target: "skills/learned/x-0123456789abcdef.md", SHA256: "a"}, "plain"})
	if target, sha := reviewerFamilyTarget(s, class, skills); target != "skills/learned/x-0123456789abcdef.md" || sha != "a" {
		t.Fatalf("eligible family procedure not chosen: %s %s", target, sha)
	}
	if target, _ := reviewerFamilyTarget(s, class, []reviewerSkill{{sharedLearningCatalogSkill{Target: class, SHA256: "e"}, "plain"}}); target != class {
		t.Fatalf("exact class target not chosen: %s", target)
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
		// Same status and reason but with results attached: not the empty row.
		`{"status":"unjudged","reason":"judge not configured","checks_failed":"K7 missing"}`,
	} {
		prompt = ""
		if code, out := runReviewerAdapterOnce(t, product, `{"event":{"class":"c"},"verdict":`+kept+`}`, none, &prompt); code != 0 {
			t.Fatalf("code %d output %s", code, out)
		}
		var v map[string]string
		_ = json.Unmarshal([]byte(kept), &v)
		if !strings.Contains(prompt, v["reason"]) || (v["checks_failed"] != "" && !strings.Contains(prompt, v["checks_failed"])) {
			t.Fatalf("real verdict %s was hidden from the reviewer", kept)
		}
	}
}
