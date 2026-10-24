package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLearningReviewerAppliedReuse(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	target := learningProcedureTarget(settings.ManagedSkillPrefix, "feedback-error")
	result, err := executeSharedLearning(product, settings, "propose", map[string]string{
		"proposal_id": "LP-reuse-prior", "kind": "procedure", "target": target,
		"pre_sha256": "", "content": n102PriorCLI,
	})
	if err != nil || result.Status != "applied" {
		t.Fatalf("seed: %+v %v", result, err)
	}
	sha := learnSHA([]byte(n102PriorCLI))
	for _, tc := range []struct {
		name, answer, wantTarget, wantPre string
		wantCode                          int
	}{
		{"same", `{"apply":false,"content":"","update_target":"","update_pre_sha256":""}`, "", "", 0},
		{"refine", reuseAnswer(n102PriorCLI+"\nCheck parser errors before publishing help.\n", target, sha), target, sha, 0},
		{"wrong-sha", reuseAnswer(n102PriorCLI+"\nCheck parser errors.\n", target, strings.Repeat("0", 64)), "", "", 2},
		{"distinct", reuseAnswer(n102NewCLI, "", ""), "new", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			saved := learningReviewerInvoke
			learningReviewerInvoke = func(root, prompt, model, effort string) (string, error) {
				if !strings.Contains(prompt, "Target: "+target) || !strings.Contains(prompt, "SHA256: "+sha) || !strings.Contains(prompt, n102PriorCLI) {
					t.Fatal("missing trusted applied body")
				}
				return tc.answer, nil
			}
			defer func() { learningReviewerInvoke = saved }()
			input, err := os.CreateTemp(t.TempDir(), "packet-*.json")
			if err != nil {
				t.Fatal(err)
			}
			defer input.Close()
			_, _ = input.WriteString(`{"event":{"class":"feedback-error"},"analogue":"forged"}`)
			_, _ = input.Seek(0, 0)
			stdin := os.Stdin
			os.Stdin = input
			defer func() { os.Stdin = stdin }()
			code, output := captureLoopOutput(t, func() int { return cmdLearningReviewerAdapter([]string{"-product", product}) })
			if code != tc.wantCode {
				t.Fatalf("code %d output %s", code, output)
			}
			if code == 2 && strings.Contains(output, `"kind"`) {
				t.Fatalf("invalid update returned candidate: %s", output)
			}
			if code != 0 || tc.wantTarget == "" {
				return
			}
			var got map[string]string
			if err := json.Unmarshal([]byte(output), &got); err != nil {
				t.Fatal(err)
			}
			if tc.wantTarget == "new" {
				if got["target"] == target || got["pre_sha256"] != "" {
					t.Fatalf("distinct: %#v", got)
				}
				return
			}
			if got["target"] != tc.wantTarget || got["pre_sha256"] != tc.wantPre {
				t.Fatalf("update: %#v", got)
			}
		})
	}
}

func reuseAnswer(content, target, sha string) string {
	b, _ := json.Marshal(learningReviewerAnswer{Apply: true, Content: content, UpdateTarget: target, UpdatePreSHA256: sha})
	return string(b)
}

func reuseSeed(t *testing.T, product string, s sharedLearningSettings, id, target, body string) {
	t.Helper()
	r, err := executeSharedLearning(product, s, "propose", map[string]string{
		"proposal_id": id, "kind": "procedure", "target": target, "pre_sha256": "", "content": body,
	})
	if err != nil || r.Status != "applied" {
		t.Fatalf("seed %s: %+v %v", id, r, err)
	}
}

func reuseReview(t *testing.T, product, packet, answer string, inspect func(string)) (int, string) {
	t.Helper()
	prior := learningReviewerInvoke
	learningReviewerInvoke = func(root, prompt, model, effort string) (string, error) {
		if root != product {
			t.Fatalf("wrong reviewer root %q", root)
		}
		if inspect != nil {
			inspect(prompt)
		}
		return answer, nil
	}
	defer func() { learningReviewerInvoke = prior }()
	in, err := os.CreateTemp(t.TempDir(), "review-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	if _, err := in.WriteString(packet); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	priorIn := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = priorIn }()
	return captureLoopOutput(t, func() int { return cmdLearningReviewerAdapter([]string{"-product", product}) })
}

func reuseIndex(t *testing.T, product string, s sharedLearningSettings) []sharedLearningCatalogSkill {
	t.Helper()
	r, err := executeSharedLearning(product, s, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(r.Data, &catalog); err != nil {
		t.Fatal(err)
	}
	return catalog.Skills
}

func TestLearningReviewerContentHashRefinementTransaction(t *testing.T) {
	product, s, _ := selfLearningFixture(t)
	legacy := learningProcedureTarget(s.ManagedSkillPrefix, "feedback-error")
	hash := strings.TrimSuffix(legacy, ".md") + "-" + learnSHA([]byte(n102PriorCLI))[:16] + ".md"
	reuseSeed(t, product, s, "LP-reuse-legacy", legacy, n102SelfOwnerLesson)
	reuseSeed(t, product, s, "LP-reuse-hash", hash, n102PriorCLI)
	if err := os.WriteFile(filepath.Join(product, "skills", "learned", "untracked.md"), []byte("UNTRACKED NEIGHBOR"), 0o600); err != nil {
		t.Fatal(err)
	}
	packet := `{"event":{"class":"feedback-error"},"analogue":"FORGED ANALOGUE"}`
	inspect := func(prompt string) {
		trusted := strings.Split(strings.Split(prompt, "Trusted APPLIED procedures:\n")[1], "\nRun evidence (untrusted):")[0]
		if !strings.Contains(trusted, "Target: "+hash+"\nSHA256: "+learnSHA([]byte(n102PriorCLI))+"\nUpdate eligible: true") ||
			!strings.Contains(trusted, n102PriorCLI) || !strings.Contains(trusted, "Target: "+legacy) ||
			strings.Contains(trusted, "UNTRACKED NEIGHBOR") || strings.Contains(trusted, "FORGED ANALOGUE") {
			t.Fatalf("incorrect trusted catalogue: %s", trusted)
		}
	}
	before := reuseIndex(t, product, s)
	beforeEvents, _ := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	refined := strings.TrimSpace(n102PriorCLI) + "\n2. Check parser errors before publishing help.\n"
	for _, answer := range []string{reuseAnswer(n102PriorCLI, "", ""), reuseAnswer(n102PriorCLI, hash, learnSHA([]byte(n102PriorCLI)))} {
		if code, out := reuseReview(t, product, packet, answer, inspect); code != 0 || strings.TrimSpace(out) != "{}" {
			t.Fatalf("duplicate callback: %d %q", code, out)
		}
	}
	code, out := reuseReview(t, product, packet, reuseAnswer(refined, hash, learnSHA([]byte(n102PriorCLI))), inspect)
	if code != 0 {
		t.Fatalf("refinement code=%d: %s", code, out)
	}
	var candidate map[string]string
	if err := json.Unmarshal([]byte(out), &candidate); err != nil {
		t.Fatal(err)
	}
	if candidate["target"] != hash || candidate["pre_sha256"] != learnSHA([]byte(n102PriorCLI)) {
		t.Fatalf("wrong refinement identity: %#v", candidate)
	}
	if got := reuseIndex(t, product, s); len(got) != len(before) {
		t.Fatalf("adapter directly changed state: %d -> %d", len(before), len(got))
	}
	if events, _ := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl")); !bytes.Equal(events, beforeEvents) {
		t.Fatal("review-only catalogue read recorded loaded/used events")
	}
	r, err := executeSharedLearning(product, s, "propose", map[string]string{
		"proposal_id": "LP-reuse-refine", "kind": candidate["kind"], "target": candidate["target"],
		"pre_sha256": candidate["pre_sha256"], "content": candidate["content"],
	})
	if err != nil || r.Status != "applied" {
		t.Fatalf("module update: %+v %v", r, err)
	}
	after := reuseIndex(t, product, s)
	if len(after) != len(before) {
		t.Fatalf("update changed count: %d -> %d", len(before), len(after))
	}
	found := false
	for _, skill := range after {
		if skill.Target == hash && skill.SHA256 == learnSHA([]byte(refined)) {
			found = true
		}
	}
	if !found {
		t.Fatalf("updated SHA absent: %#v", after)
	}
	if b, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "backups", learnSHA([]byte(n102PriorCLI)))); err != nil || string(b) != n102PriorCLI {
		t.Fatalf("backup absent or changed: %v", err)
	}
	if code, out := reuseReview(t, product, packet, reuseAnswer(refined, "", ""), nil); code != 0 || strings.TrimSpace(out) != "{}" {
		t.Fatalf("repeated callback: %d %q", code, out)
	}
	ledgerBefore, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "ledger.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if repeat, err := executeSharedLearning(product, s, "propose", map[string]string{
		"proposal_id": "LP-reuse-refine", "kind": candidate["kind"], "target": candidate["target"],
		"pre_sha256": candidate["pre_sha256"], "content": candidate["content"],
	}); err != nil || repeat.ID != r.ID {
		t.Fatalf("repeat did not return original transaction: status=%s id=%s err=%v", repeat.Status, repeat.ID, err)
	}
	if ledgerAfter, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "ledger.jsonl")); err != nil || !bytes.Equal(ledgerBefore, ledgerAfter) {
		t.Fatalf("repeat added a transaction: %v", err)
	}
	r, err = executeSharedLearning(product, s, "rollback", map[string]string{"proposal_id": "LP-reuse-refine"})
	if err != nil || r.Status != "rolled_back" {
		t.Fatalf("module rollback: %+v %v", r, err)
	}
	if b, err := os.ReadFile(filepath.Join(product, filepath.FromSlash(hash))); err != nil || string(b) != n102PriorCLI {
		t.Fatalf("rollback failed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(product, filepath.FromSlash(legacy))); err != nil || string(b) != n102SelfOwnerLesson {
		t.Fatalf("unrelated lesson changed: %v", err)
	}
}

func TestLearningReviewerUpdateIdentityAndDuplicates(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	a := learningProcedureTarget(s.ManagedSkillPrefix, "feedback-error")
	b := learningProcedureTarget(s.ManagedSkillPrefix, "other")
	reuseSeed(t, product, s, "LP-reuse-a", a, n102PriorCLI)
	reuseSeed(t, product, s, "LP-reuse-b", b, n102NewCLI)
	sha := learnSHA([]byte(n102PriorCLI))
	packet := `{"event":{"class":"feedback-error"}}`
	for _, tc := range []struct {
		name, answer string
		code         int
	}{
		{"update-to-b", reuseAnswer(n102NewCLI, a, sha), 0},
		{"missing-sha", reuseAnswer(n102NewCLI, a, ""), 2},
		{"missing-target", reuseAnswer(n102NewCLI, "", sha), 2},
		{"wrong-sha", reuseAnswer(n102NewCLI, a, strings.Repeat("0", 64)), 2},
		{"malformed-sha", reuseAnswer(n102NewCLI, a, "not-sha"), 2},
		{"untracked", reuseAnswer(n102NewCLI, "skills/learned/untracked.md", sha), 2},
		{"unsafe", reuseAnswer(n102NewCLI, "../escape.md", sha), 2},
		{"foreign", reuseAnswer(n102NewCLI, "other/foreign.md", sha), 2},
		{"false-apply", `{"apply":false,"content":"","update_target":"` + a + `","update_pre_sha256":"` + sha + `"}`, 2},
		{"duplicate-key", `{"apply":true,"apply":false,"content":""}`, 2},
		{"case-alias-key", `{"apply":true,"Apply":false,"content":""}`, 2},
		{"unknown-field", `{"apply":false,"content":"","extra":"x"}`, 2},
		{"non-object", `null`, 2},
		{"empty-object", `{}`, 2},
		{"missing-apply", `{"content":""}`, 2},
		{"missing-content", `{"apply":false}`, 2},
		{"null-apply", `{"apply":null,"content":""}`, 2},
		{"null-content", `{"apply":false,"content":null}`, 2},
		{"null-update-target", `{"apply":false,"content":"","update_target":null,"update_pre_sha256":""}`, 2},
		{"null-update-sha", `{"apply":false,"content":"","update_target":"","update_pre_sha256":null}`, 2},
		{"case-alias-only", `{"Apply":false,"content":""}`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := reuseIndex(t, product, s)
			code, out := reuseReview(t, product, packet, tc.answer, nil)
			if code != tc.code || (code == 0 && strings.TrimSpace(out) != "{}") || strings.Contains(out, `"kind"`) {
				t.Fatalf("code=%d output=%q", code, out)
			}
			if got := reuseIndex(t, product, s); len(got) != len(before) {
				t.Fatalf("module state changed: %#v", got)
			}
			if body, err := os.ReadFile(filepath.Join(product, filepath.FromSlash(a))); err != nil || string(body) != n102PriorCLI {
				t.Fatalf("A erased: %v", err)
			}
		})
	}
}

func TestLearningReviewerProtectedCaseAlias(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	target := learningProcedureTarget(s.ManagedSkillPrefix, "feedback-error")
	reuseSeed(t, product, s, "LP-protected", target, n102PriorCLI)
	s.ProtectedTargets = []string{strings.ToUpper(target)}
	if err := writeSharedLearningSettings(product, s); err != nil {
		t.Fatal(err)
	}
	code, out := reuseReview(t, product, `{"event":{"class":"feedback-error"}}`, reuseAnswer(n102NewCLI, target, learnSHA([]byte(n102PriorCLI))), func(prompt string) {
		if !strings.Contains(prompt, "Target: "+target+"\nSHA256: "+learnSHA([]byte(n102PriorCLI))+"\nUpdate eligible: false") {
			t.Fatal("protected update was advertised")
		}
	})
	if code != 2 || strings.Contains(out, `"kind"`) {
		t.Fatalf("protected candidate: %d %q", code, out)
	}
}

func TestLearningReviewerFixedDiagnostics(t *testing.T) {
	for _, tc := range []struct{ message, reason string }{
		{"applied catalogue exceeds reviewer procedure limit", "reviewer_context_limit"},
		{"applied catalogue exceeds reviewer byte limit", "reviewer_context_limit"},
		{"applied catalogue body is unavailable or changed", "reviewer_context_unavailable"},
		{"applied catalogue changed during review", "reviewer_context_changed"},
		{"settings changed during review", "reviewer_settings_changed"},
		{"self owner changed during review", "reviewer_owner_changed"},
		{"incomplete update identity", "reviewer_invalid_update"},
		{"update requires apply", "reviewer_invalid_update"},
		{"update target is unavailable or protected", "reviewer_invalid_update"},
	} {
		t.Run(tc.reason+tc.message, func(t *testing.T) {
			got := learningFailureReason([]byte(fmt.Sprintf("learning reviewer: %s syntheticSECRET\n", tc.message)))
			if got != tc.reason || strings.Contains(got, "syntheticSECRET") {
				t.Fatalf("unsafe diagnostic %q", got)
			}
		})
	}
}

func TestLearningReviewerRejectsDuringProviderDrift(t *testing.T) {
	for _, mode := range []string{"body", "module", "settings", "disabled", "owner-disabled", "owner-replaced"} {
		t.Run(mode, func(t *testing.T) {
			product, s, _ := selfLearningFixture(t)
			target := learningProcedureTarget(s.ManagedSkillPrefix, "feedback-error")
			reuseSeed(t, product, s, "LP-drift-old", target, n102PriorCLI)
			path := filepath.Join(product, filepath.FromSlash(target))
			mutated := false
			answer := reuseAnswer(n102NewCLI, target, learnSHA([]byte(n102PriorCLI)))
			code, out := reuseReview(t, product, `{"event":{"class":"feedback-error"}}`, answer, func(string) {
				if mutated {
					return
				}
				mutated = true
				switch mode {
				case "body":
					if err := os.WriteFile(path, []byte(n102SelfOwnerLesson), 0o600); err != nil {
						t.Fatal(err)
					}
				case "module":
					r, err := executeSharedLearning(product, s, "propose", map[string]string{"proposal_id": "LP-drift-module", "kind": "procedure", "target": target, "pre_sha256": learnSHA([]byte(n102PriorCLI)), "content": n102SelfOwnerLesson})
					if err != nil || r.Status != "applied" {
						t.Fatalf("module drift: %+v %v", r, err)
					}
				case "settings", "disabled":
					changed := s
					if mode == "settings" {
						changed.ProtectedTargets = []string{"skills/learned/protected.md"}
					} else {
						changed.Schema = "disabled"
					}
					if err := writeSharedLearningSettings(product, changed); err != nil {
						t.Fatal(err)
					}
				case "owner-disabled", "owner-replaced":
					sel, found, err := readAirWorkerSelfLearningSelector()
					if err != nil || !found {
						t.Fatalf("selector: %v", err)
					}
					if mode == "owner-disabled" {
						sel.Enabled = false
					} else {
						sel.ProductRoot = filepath.Join(t.TempDir(), "replacement")
					}
					if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
						t.Fatal(err)
					}
				}
			})
			if !mutated || code != 2 || strings.Contains(out, `"kind"`) {
				t.Fatalf("drift accepted: code=%d output=%q", code, out)
			}
			if mode == "body" || mode == "module" {
				body, err := os.ReadFile(path)
				if err != nil || string(body) != n102SelfOwnerLesson {
					t.Fatalf("adapter modified deliberate drift: %v", err)
				}
			} else if body, err := os.ReadFile(path); err != nil || string(body) != n102PriorCLI {
				t.Fatalf("adapter modified old body: %v", err)
			}
		})
	}
}

func TestLearningReviewerRejectsMachineEnabledRefinements(t *testing.T) {
	capsule := nativeLearnedActionMarkdownPrefix + stringMustJSON(supportedNativePreflightAction()) + nativeLearnedActionMarkdownSuffix
	for _, tc := range []struct{ name, old, next string }{
		{"n093-first", n093ReviewedSafePreflightText, n102PriorCLI},
		{"n093-second", n093ReviewedSafePreflightSecondReal, n102PriorCLI},
		{"n118", n118SafePreflightProcedure, n102PriorCLI},
		{"capsule", capsule, n102PriorCLI},
		{"feedback-old", n102PriorCLI + "\n" + nativeFeedbackUseAction + "\n", n102NewCLI},
		{"feedback-new", n102PriorCLI, n102NewCLI + "\n" + nativeFeedbackUseAction + "\n"},
		{"reserved-new", n102PriorCLI, "# AirWorker Native Action\n\n## When to apply\nX\n## Procedure\nX\n## Pitfalls\nX\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			product, s := sharedProductFixture(t, false)
			target := learningProcedureTarget(s.ManagedSkillPrefix, "feedback-error")
			reuseSeed(t, product, s, "LP-machine-old", target, tc.old)
			code, out := reuseReview(t, product, `{"event":{"class":"feedback-error"}}`, reuseAnswer(tc.next, target, learnSHA([]byte(tc.old))), func(prompt string) {
				if strings.Contains(tc.name, "old") || tc.name == "capsule" || tc.name == "n093-first" || tc.name == "n093-second" || tc.name == "n118" {
					if !strings.Contains(prompt, "Update eligible: false") {
						t.Fatal("machine target advertised as eligible")
					}
				}
			})
			if code != 2 || strings.Contains(out, `"kind"`) {
				t.Fatalf("machine update accepted: %d %q", code, out)
			}
			if body, err := os.ReadFile(filepath.Join(product, filepath.FromSlash(target))); err != nil || string(body) != tc.old {
				t.Fatalf("machine contract erased: %v", err)
			}
		})
	}
}

func stringMustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

func TestLearningReviewerCatalogueFailsBeforeProvider(t *testing.T) {
	for _, mode := range []string{"count", "bytes", "changed", "unsafe"} {
		t.Run(mode, func(t *testing.T) {
			product, s := sharedProductFixture(t, false)
			count, body := 1, n102PriorCLI
			switch mode {
			case "count":
				count = 33
			case "bytes":
				count, body = 3, n102PriorCLI+strings.Repeat("safe detail ", 4000)
			}
			var first string
			for i := 0; i < count; i++ {
				target := learningProcedureTarget(s.ManagedSkillPrefix, fmt.Sprintf("catalogue-%d", i))
				if i == 0 {
					first = target
				}
				reuseSeed(t, product, s, fmt.Sprintf("LP-limit-%d", i), target, body)
			}
			if mode == "changed" {
				if err := os.WriteFile(filepath.Join(product, filepath.FromSlash(first)), []byte(n102NewCLI), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if mode == "unsafe" {
				path := filepath.Join(product, filepath.FromSlash(first))
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(product, sharedLearningConfigFile), path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			}
			calls := 0
			code, out := reuseReview(t, product, `{"event":{"class":"feedback-error"}}`, reuseAnswer(n102NewCLI, "", ""), func(string) { calls++ })
			if code != 2 || calls != 0 || strings.Contains(out, `"kind"`) {
				t.Fatalf("partial context reached provider: code=%d calls=%d output=%q", code, calls, out)
			}
		})
	}
}
