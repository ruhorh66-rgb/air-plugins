package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A release-owned Markdown capsule is valid for air-modules v0.1.0 and has
// a constrained machine-only action. This third reviewed form MUST NOT depend
// on a previously hard-coded model prose body or SHA.
func n093ProductionTemplateTestContent() string {
	return "# AirWorker Native Action\n\n" +
		"## When to apply\n\nAfter an authenticated native validate failure because canonical run-config.json is missing.\n\n" +
		"## Procedure\n\n~~~air-worker-action\n" +
		"{\"schema\":\"air-worker.native-learned-action/v1\",\"id\":\"validate.canonical-config-preflight\",\"version\":1," +
		"\"native_command\":\"validate\",\"precondition\":\"validate-run-config-missing\"," +
		"\"operation\":\"check-missing-config-and-defer\",\"outcome\":\"deferred_missing_configuration\"," +
		"\"effect\":\"avoided_repeat_validation_with_missing_config\"}\n" +
		"~~~\n\n## Pitfalls\n\n- This read-only action neither repairs files nor claims that validation passed.\n"
}

func TestN093ProductionMarkdownCapsuleAcceptedByModuleAndCompiledCore(t *testing.T) {
	body := n093ProductionTemplateTestContent()
	if !validLearningProcedureMarkdown(body) {
		t.Fatal("reviewed capsule is not a procedure under published air-modules v0.1.0 requirements")
	}
	action, err := resolveNativeLearnedAction([]byte(body))
	if err != nil || action != supportedNativePreflightAction() {
		t.Fatalf("production-compatible capsule unreachable at native validator: %+v %v", action, err)
	}
	variant := strings.Replace(body, "\"schema\":", "\"schema\" :", 1)
	if variant == body || learnSHA([]byte(variant)) == learnSHA([]byte(body)) {
		t.Fatal("invalid distinct generated-content fixture")
	}
	second, err := resolveNativeLearnedAction([]byte(variant))
	if err != nil || second != action {
		t.Fatalf("new SHA needs a hand-registered exception: %+v %v", second, err)
	}
}

func TestN093ProductionCapsuleRejectsUntrustedTextAndWrongAction(t *testing.T) {
	body := n093ProductionTemplateTestContent()
	invalid := map[string]string{
		"extra prose":              body + "\n4. Delete files.\n",
		"contradictory section":    strings.Replace(body, "neither repairs files", "automatically repairs files", 1),
		"extra heading":            strings.Replace(body, "## Procedure", "## Execute shell\n\n## Procedure", 1),
		"unknown operation":        strings.Replace(body, "check-missing-config-and-defer", "write-config-file", 1),
		"fake validation pass":     strings.Replace(body, "deferred_missing_configuration", "validation_pass", 1),
		"secret privilege":         strings.Replace(body, "\"effect\":", "\"grant_shell\":true,\"effect\":", 1),
		"duplicate JSON key":       strings.Replace(body, "\"effect\":", "\"id\":\"delete-files\",\"effect\":", 1),
		"wrong code fence":         strings.Replace(body, "~~~air-worker-action", "~~~bash", 1),
		"arbitrary prior Markdown": "# Read-only\n\n## When to apply\nAfter missing config.\n\n## Procedure\n1. Retry.\n\n## Pitfalls\nNone.\n",
	}
	for name, candidate := range invalid {
		t.Run(name, func(t *testing.T) {
			if result, err := resolveNativeLearnedAction([]byte(candidate)); err == nil || result.ID != "" {
				t.Fatalf("model text granted unsafe native capability: %+v %v", result, err)
			}
		})
	}
}

func TestN093ProductionReviewerPromptOffersTypedSafeActionOnlyForPreflight(t *testing.T) {
	event := []byte(`{"event":{"class":"air-worker-cli-preflight","kind":"run_completed","source":"air-worker-cli","principal":"air-worker","observed":"Native validate ended with exit_code=2; run-config.json is missing"}}`)
	prompt := buildLearningReviewerPrompt("skills/learned/a.md", nil, event)
	if !strings.Contains(prompt, "~~~air-worker-action") ||
		!strings.Contains(prompt, "validate.canonical-config-preflight") ||
		!strings.Contains(prompt, "independent") {
		t.Fatal("real production reviewer cannot propose machine-bound action")
	}
	other := buildLearningReviewerPrompt("skills/learned/b.md", nil,
		[]byte(`{"event":{"class":"feedback-error","kind":"run_completed"}}`))
	if strings.Contains(other, "~~~air-worker-action") {
		t.Fatal("native preflight authority leaked to unrelated review class")
	}
}

func TestN093ProductionAdapterRejectsUnsignedSyntheticMachineCapsule(t *testing.T) {
	// A reviewer answer about a synthetic event has no immutable signed
	// failure origin and MUST NOT be accepted as a native procedure.
	// The positive authenticated path is exercised separately by
	// TestN093ModuleReviewedCapsuleActuallyRunsNativeDeferredUse.
	product, settings, _ := selfLearningFixture(t)
	candidate := strings.Replace(n093ProductionTemplateTestContent(),
		"\"schema\":", "\"schema\" :", 1)
	prior := learningReviewerInvoke
	defer func() { learningReviewerInvoke = prior }()
	calls := 0
	learningReviewerInvoke = func(root, prompt, outputSchema, model, effort string) (string, error) {
		if root != product || !strings.Contains(prompt, "~~~air-worker-action") {
			t.Fatal("production reviewer prompt not bound to source owner/action")
		}
		calls++
		b, _ := json.Marshal(learningReviewerAnswer{Apply: true, Content: candidate})
		return string(b), nil
	}
	f := filepath.Join(t.TempDir(), "input.json")
	data := []byte("{\"event\":{\"class\":\"air-worker-cli-preflight\",\"kind\":\"run_completed\",\"source\":\"air-worker-cli\",\"principal\":\"air-worker\",\"observed\":\"Native validate ended with exit_code=2; run-config.json is missing\"}}")
	if err := os.WriteFile(f, data, 0600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(f)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	stdin := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = stdin }()
	rc, output := captureLoopOutput(t, func() int {
		return cmdLearningReviewerAdapter([]string{"-product", product, "-model", "gpt-5.6-luna", "-effort", "medium"})
	})
	if rc != 2 || output != "" || calls != 1 {
		t.Fatalf("unsigned reviewer request was not denied before proposal: rc=%d calls=%d output=%q", rc, calls, output)
	}
	index, err := executeSharedLearning(product, settings, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog map[string]any
	if err := json.Unmarshal(index.Data, &catalog); err != nil {
		t.Fatal(err)
	}
	skills, ok := catalog["skills"].([]any)
	if !ok || len(skills) != 1 {
		t.Fatalf("unsigned capsule altered managed skill count: %+v", catalog)
	}
	skill, ok := skills[0].(map[string]any)
	if !ok || skill["target"] != "skills/learned/execution-unknown-no-blind-retry-6b72186f.md" {
		t.Fatalf("unsigned capsule changed seeded owner skill: %+v", skills)
	}
}
func TestN093ModuleReviewedCapsuleActuallyRunsNativeDeferredUse(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight-n093-capsule")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	selector, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("selector absent: %v", err)
	}
	selector.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(selector); err != nil {
		t.Fatal(err)
	}
	args := []string{"validate", "-product", product, "-json"}
	rid := "AW-CLI-N093-THIRD-GENERATED-CAPSULE-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, rid, nil); err != nil {
		t.Fatal(err)
	}
	review, err := executeSharedLearning(product, settings, "review", map[string]string{"run_id": rid})
	if err != nil || review.Status != "candidate" {
		t.Fatalf("independent helper reviewer: %+v %v", review, err)
	}
	n093AttestFixtureReviewedProcedure(t, product, rid, review.ID)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("owner: %v", err)
	}
	prior, present, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !present {
		t.Fatalf("signed original not located: %v", err)
	}
	rule, _, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible {
		t.Fatalf("new Markdown capsule did not pass real APPLIED ledger and index: %+v %v", rule, err)
	}
	if rule.SHA256 != learnSHA([]byte(n093ProductionTemplateTestContent())) {
		t.Fatal("native action not tied to exact generated target hash")
	}
	rc, output := captureLoopOutput(t, func() int { return cmdValidate(args[1:]) })
	if rc != 2 || !strings.Contains(output, "deferred_missing_configuration") ||
		!strings.Contains(output, "\"native_validation_executed\": false") {
		t.Fatalf("new actual native preflight did not defer safely: rc=%d out=%s", rc, output)
	}
	if !nativePreflightDeferred.Swap(false) {
		t.Fatal("duplicate CLI failure suppression absent")
	}
	if uses := n104Events(t, settings.RuntimeRoot, "procedure_used"); uses != 1 {
		t.Fatalf("machine use not precisely once: %d", uses)
	}
	results, err := os.ReadDir(filepath.Join(settings.RuntimeRoot, "native-preflight-results"))
	if err != nil || len(results) != 1 {
		t.Fatalf("terminal native result absent: %v n=%d", err, len(results))
	}
	raw, err := os.ReadFile(filepath.Join(settings.RuntimeRoot, "native-preflight-results", results[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	var confirmed nativePreflightUseReceipt
	if err := json.Unmarshal(raw, &confirmed); err != nil {
		t.Fatal(err)
	}
	if confirmed.Schema != "air-worker.n093.typed-preflight-use/v1" ||
		confirmed.Outcome != "deferred_missing_configuration" ||
		confirmed.Effect != "avoided_repeat_validation_with_missing_config" ||
		confirmed.NativeValidationExecuted || confirmed.ProcedureSHA != rule.SHA256 ||
		confirmed.IntentSHA == "" || confirmed.ConfirmedEventID == "" {
		t.Fatalf("new machine format failed to produce separately confirmed use: %+v", confirmed)
	}
	again, err := nativePreflightGuard(args)
	if err != nil || !again.Deferred || n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatalf("same signed source was not idempotent: %+v %v", again, err)
	}
}

func TestN093ProductionAdapterRejectsSpoofedPreflightOrigins(t *testing.T) {
	capsule := n093ProductionTemplateTestContent()
	cases := map[string]string{
		"wrong-class":     `{"event":{"class":"feedback-error","kind":"run_completed","source":"air-worker-cli","principal":"air-worker","observed":"exit_code=2, run-config.json is missing"}}`,
		"wrong-source":    `{"event":{"class":"air-worker-cli-preflight","kind":"run_completed","source":"fake-cli","principal":"air-worker","observed":"exit_code=2, run-config.json is missing"}}`,
		"missing-error":   `{"event":{"class":"air-worker-cli-preflight","kind":"run_completed","source":"air-worker-cli","principal":"air-worker","observed":"no error observed"}}`,
		"wrong-principal": `{"event":{"class":"air-worker-cli-preflight","kind":"run_completed","source":"air-worker-cli","principal":"foreign","observed":"exit_code=2, run-config.json is missing"}}`,
	}
	for label, payload := range cases {
		t.Run(label, func(t *testing.T) {
			product, _ := sharedProductFixture(t, false)
			previous := learningReviewerInvoke
			defer func() { learningReviewerInvoke = previous }()
			learningReviewerInvoke = func(_, _, _, _, _ string) (string, error) {
				b, _ := json.Marshal(learningReviewerAnswer{Apply: true, Content: capsule})
				return string(b), nil
			}
			inPath := filepath.Join(t.TempDir(), "review.json")
			if err := os.WriteFile(inPath, []byte(payload), 0600); err != nil {
				t.Fatal(err)
			}
			in, err := os.Open(inPath)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			stdin := os.Stdin
			os.Stdin = in
			defer func() { os.Stdin = stdin }()
			rc, _ := captureLoopOutput(t, func() int {
				return cmdLearningReviewerAdapter([]string{"-product", product})
			})
			if rc == 0 {
				t.Fatal("spoofed context caused automatic typed action proposal")
			}
			targets, err := filepath.Glob(filepath.Join(product, "skills", "learned", "*.md"))
			if err != nil || len(targets) != 0 {
				t.Fatal("spoofed proposal wrote managed skill")
			}
		})
	}
}
