package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const n104SafeFeedbackProcedure = "# Capture a real defect through native AirWorker feedback\n\n" +
	"## When to apply\nWhen an actual AirWorker command fails and the operator needs a durable defect candidate.\n\n" +
	"## Procedure\n1. Select this learned skill by its exact current target@SHA.\n" +
	"2. Call air-worker feedback add with a stable -run-id, correct product and real error evidence.\n" +
	"3. Require the native event, immutable feedback receipt, and one PLAN candidate.\n\n" +
	"## Pitfalls\nDo not claim a model successfully fixed the error or approve a blocking rule.\n\n" +
	"## Machine-verifiable action\nAction: air-worker.feedback.add/v1\n"

func n104Setup(t *testing.T) (string, sharedLearningSettings, string, string, string) {
	t.Helper()
	selfRoot, selfSettings, _ := selfLearningFixture(t)
	// A manually proposed Markdown action is not self-learning provenance.
	// Seed through the actual owned review/apply API with a bounded native
	// adapter fixture instead, which persists reviewer, event and ledger.
	selfSettings.Reviewer = learningAdapterFixture(t, "review-native-feedback")
	if err := writeSharedLearningSettings(selfRoot, selfSettings); err != nil {
		t.Fatal(err)
	}
	selector, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("self selector absent: %v", err)
	}
	selector.ConfigSHA256, err = selfLearningConfigSHA(selfRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(selector); err != nil {
		t.Fatal(err)
	}
	skill := "skills/learned/native-feedback-lesson-n104.md"
	res, err := executeSharedLearning(selfRoot, selfSettings, "observe", map[string]string{
		"run_id": "N104-real-review-fixture",
		"kind":   "run_completed", "class": "feedback-error",
		"source": "feedback", "principal": "gpt-n104-fixture",
		"observed": "actual AirWorker feedback native artifact and error run-id",
	})
	if err != nil || res.Status != "recorded" {
		t.Fatalf("native reviewer did not record controlled lesson: %+v %v", res, err)
	}
	if !currentSelfLearningRuleBytes(selfLearningOwner{Selector: selector, Settings: selfSettings},
		selfLearningLoadedRule{Target: skill, SHA256: learnSHA([]byte(n104SafeFeedbackProcedure))}) {
		t.Fatal("reviewer failed to apply expected exact skill")
	}
	product, _ := sharedProductFixture(t, false)
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte(`{"plan":"PLAN.md"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "PLAN.md"), []byte("# Target product\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return selfRoot, selfSettings, product, skill, learnSHA([]byte(n104SafeFeedbackProcedure))
}
func n104CLI(t *testing.T, product, runID, selection string) (int, string) {
	t.Helper()
	args := []string{"add", "-product", product, "-kind", "error",
		"-source", "n104-test-worker", "-run-id", runID,
		"-text", "real fixture subprocess exited nonzero",
		"-ref", "fixture/real-error.json", "-type", "defect", "-severity", "P1",
		"-source-version", "0.11.8", "-expected", "recovery after verified evidence",
		"-reproduction", "run supported native command then inspect exit",
		"-workaround", "no blind retry", "-proposed-outcome", "durable feedback",
	}
	if selection != "" {
		args = append(args, "-use-procedure", selection)
	}
	return captureLoopOutput(t, func() int { return cmdFeedback(args) })
}
func n104Events(t *testing.T, runtimeRoot string, kind string) int {
	t.Helper()
	n := 0
	if err := scanLearnJSONL(filepath.Join(runtimeRoot, "events.jsonl"), func(b []byte) error {
		var item map[string]any
		if err := json.Unmarshal(b, &item); err != nil {
			return err
		}
		if item["schema"] == "air.learning.event/v1" && item["kind"] == kind {
			n++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestN104NativeSelectedSafeProcedureRecordsVerifiedEffect(t *testing.T) {
	_, selfSettings, target, skill, hash := n104Setup(t)
	runID := "N104-native-feedback-use"
	selected := skill + "@" + hash
	code, out := n104CLI(t, target, runID, selected)
	if code != 0 {
		t.Fatalf("native learned feedback failed rc=%d out=%s", code, out)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("expected one machine-verified used event, got %d", got)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "run_completed"); got != 1 {
		t.Fatalf("self-owner must retain only its initial own learning trigger, not target product feedback: %d", got)
	}
	id := "FB-" + learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24]
	file := filepath.Join(target, filepath.FromSlash(feedbackEvidenceRel(id)))
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var r feedbackRecord
	if err := json.Unmarshal(b, &r); err != nil || r.RunID != runID || r.EventID == "" {
		t.Fatalf("canonical feedback inconsistent: %+v %v", r, err)
	}
	plan, err := os.ReadFile(filepath.Join(target, "PLAN.md"))
	if err != nil || strings.Count(string(plan), "- feedback `"+id+"`") != 1 {
		t.Fatalf("PLAN candidate does not match exactly one native operation: %v", err)
	}
	code, out = n104CLI(t, target, runID, selected)
	if code != 0 {
		t.Fatalf("native idempotent replay failed: rc=%d out=%s", code, out)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("retry duplicated verified use event: %d", got)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "skill_loaded"); got != 0 {
		t.Fatalf("loaded events unexpectedly changed schema: %d", got)
	}
}
func TestN104RejectsForeignStaleUnselectedAndUnsupportedProcedureUse(t *testing.T) {
	selfRoot, selfSettings, target, skill, hash := n104Setup(t)
	wrong := "skills/learned/not-owned.md@" + hash
	code, _ := n104CLI(t, target, "N104-foreign", wrong)
	if code == 0 {
		t.Fatal("foreign learned target selected")
	}
	code, _ = n104CLI(t, target, "N104-wrong-sha", skill+"@"+strings.Repeat("0", 64))
	if code == 0 {
		t.Fatal("incorrect SHA accepted")
	}
	orig := filepath.Join(selfRoot, filepath.FromSlash(skill))
	if err := os.WriteFile(orig, []byte(n104SafeFeedbackProcedure+"\nchanged unexpectedly\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _ = n104CLI(t, target, "N104-stale", skill+"@"+hash)
	if code == 0 {
		t.Fatal("modified learned procedure credited")
	}
	code, out := n104CLI(t, target, "N104-without-selection", "")
	if code != 0 {
		t.Fatalf("regular feedback should be unaffected rc=%d out=%s", code, out)
	}
	if got := n104Events(t, selfSettings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("unselected or foreign skill falsely used %d", got)
	}
}
func TestN104NoMachineActionMarkerMeansLoadedButNeverUsed(t *testing.T) {
	root, s, target, _, _ := n104Setup(t)
	other := "skills/learned/unsupported-informational-lesson.md"
	text := "# Inspect outcomes\n\n## When to apply\nWhen a job ended.\n\n## Procedure\n1. Examine the receipt.\n\n## Pitfalls\nDo not invent results.\n"
	result, err := executeSharedLearning(root, s, "propose", map[string]string{
		"proposal_id": "LP-N104-untyped", "kind": "procedure", "target": other, "pre_sha256": "", "content": text})
	if err != nil || result.Status != "applied" {
		t.Fatal(err)
	}
	code, _ := n104CLI(t, target, "N104-unsupported", other+"@"+learnSHA([]byte(text)))
	if code == 0 {
		t.Fatal("untyped prose falsely claimed native executable use")
	}
	if got := n104Events(t, s.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("false used=%d", got)
	}
}

func TestN104ReviewerOnlyBindsProvenSafeNativeAction(t *testing.T) {
	realLesson := "# Verify self-learning feedback intake\n\n" +
		"## When to apply\nWhen product self-learning is missing.\n\n" +
		"## Procedure\n1. Inspect the exact installed binary version.\n" +
		"2. Submit any defect through the native feedback path with a stable run ID.\n" +
		"3. Compare source event, receipt and PLAN candidate.\n\n" +
		"## Pitfalls\nDo not treat loaded guidance as used.\n"
	newLesson, err := bindNativeFeedbackProof("feedback-error", realLesson)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(newLesson, nativeFeedbackUseAction) != 1 {
		t.Fatalf("positive real reviewer procedure did not gain supported action: %s", newLesson)
	}
	for _, c := range []struct{ class, text string }{
		{"completed-turn", realLesson + "\n" + nativeFeedbackUseAction + "\n"},
		{"feedback-error", "# Unrelated\n\n## When to apply\nUnknown.\n\n## Procedure\n1. Do not submit via the native feedback path with a stable run ID.\n\n## Pitfalls\nNever claim success.\n\n" + nativeFeedbackUseAction + "\n"},
		{"feedback-error", newLesson + "\n" + nativeFeedbackUseAction + "\n"},
	} {
		if _, err := bindNativeFeedbackProof(c.class, c.text); err == nil {
			t.Fatalf("unsafe reviewer action was bound for class %s", c.class)
		}
	}
}

func TestN104CannotClaimRetroactiveOrDifferentLearnedRule(t *testing.T) {
	root, settings, target, skill, sha := n104Setup(t)
	run := "N104-retroactive"
	if code, response := n104CLI(t, target, run, ""); code != 0 {
		t.Fatalf("baseline unselected operation failed: %d %s", code, response)
	}
	if code, _ := n104CLI(t, target, run, skill+"@"+sha); code == 0 {
		t.Fatal("retroactive learned-skill selection accepted after feedback was recorded")
	}
	run2 := "N104-different-selected-rule"
	if code, response := n104CLI(t, target, run2, skill+"@"+sha); code != 0 {
		t.Fatalf("baseline selected operation failed: %d %s", code, response)
	}
	alt := "skills/learned/a-different-feedback-lesson.md"
	diff := strings.ReplaceAll(n104SafeFeedbackProcedure, "a real defect", "another real defect")
	result, err := executeSharedLearning(root, settings, "propose", map[string]string{
		"proposal_id": "LP-N104-other", "kind": "procedure", "target": alt,
		"pre_sha256": "", "content": diff,
	})
	if err != nil || result.Status != "applied" {
		t.Fatalf("second valid safe lesson did not apply: %v %+v", err, result)
	}
	if code, _ := n104CLI(t, target, run2, alt+"@"+learnSHA([]byte(diff))); code == 0 {
		t.Fatal("same recorded native feedback was attributed to a second learned rule")
	}
	if used := n104Events(t, settings.RuntimeRoot, "procedure_used"); used != 1 {
		t.Fatalf("two procedure uses were counted for one selection: %d", used)
	}
}

func TestN108NativeUseLockIdentityIndependentOfCWD(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("named mutex is Windows-specific")
	}
	dir := t.TempDir()
	t.Setenv("AIR_WORKER_HOOK_STATE_DIR", filepath.Join(dir, "hook-state"))
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(orig)
	one := filepath.Join(dir, "cwd-a")
	two := filepath.Join(dir, "cwd-b")
	for _, p := range []string{one, two} {
		if err := os.MkdirAll(p, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chdir(one); err != nil {
		t.Fatal(err)
	}
	first := nativeFeedbackUseLockName("gpt-airworker", "same-run")
	if err := os.Chdir(two); err != nil {
		t.Fatal(err)
	}
	second := nativeFeedbackUseLockName("gpt-airworker", "same-run")
	if first != second {
		t.Fatalf("CWD changed native use lock: %q != %q", first, second)
	}
	if first == nativeFeedbackUseLockName("gpt-airworker", "other-run") {
		t.Fatal("run-id not in lock identity")
	}
	if first == nativeFeedbackUseLockName("different-principal", "same-run") {
		t.Fatal("principal not in lock identity")
	}
	l, ok, err := acquireNativeFeedbackUseLock(first)
	if err != nil || !ok {
		t.Fatalf("cannot acquire strict native evidence lock: %v", err)
	}
	defer l.release()
	if next, got, err := acquireNativeFeedbackUseLock(second); got || err != nil {
		if next != nil {
			next.release()
		}
		t.Fatalf("duplicate source/run acquired independent strict mutex: got=%v err=%v", got, err)
	}
}

func TestN108RejectsManuallyProposedOrNegatedFakeLearningProof(t *testing.T) {
	root, settings, target, _, _ := n104Setup(t)
	fakeTarget := "skills/learned/manually-forged-native-feedback.md"
	res, err := executeSharedLearning(root, settings, "propose", map[string]string{
		"proposal_id": "LP-n108-not-reviewed",
		"kind":        "procedure", "target": fakeTarget, "pre_sha256": "",
		"content": n104SafeFeedbackProcedure,
	})
	if err != nil || res.Status != "applied" {
		t.Fatalf("cannot set up native proposal negative: %+v %v", res, err)
	}
	code, _ := n104CLI(t, target, "N108-fake-review", fakeTarget+"@"+learnSHA([]byte(n104SafeFeedbackProcedure)))
	if code == 0 {
		t.Fatal("manually proposed Markdown with Action marker was falsely treated as an auto-reviewed procedure")
	}
	if n := n104Events(t, settings.RuntimeRoot, "procedure_used"); n != 0 {
		t.Fatalf("unreviewed skill created %d use events", n)
	}
	negative := "# Never blindly use feedback\n\n## When to apply\nWhen an error is unknown.\n\n" +
		"## Procedure\n1. Avoid calling air-worker feedback add with a stable -run-id.\n\n" +
		"## Pitfalls\nDo not report a false fix.\n"
	bound, err := bindNativeFeedbackProof("feedback-error", negative)
	if err != nil || strings.Contains(bound, nativeFeedbackUseAction) {
		t.Fatalf("negated procedure got executable evidence marker: %v %q", err, bound)
	}
	if _, err := bindNativeFeedbackProof("feedback-error", n104SafeFeedbackProcedure); err == nil {
		t.Fatal("model-supplied native Action marker was accepted")
	}
}

func TestN108NativeLockHeldAcrossProcessesWithDifferentCWD(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows native mutex contract")
	}
	root := t.TempDir()
	state := filepath.Join(root, "same-hook-state")
	t.Setenv("AIR_WORKER_HOOK_STATE_DIR", state)
	otherDir := filepath.Join(root, "different-cwd")
	if err := os.MkdirAll(otherDir, 0o700); err != nil {
		t.Fatal(err)
	}
	name := nativeFeedbackUseLockName("n108-principal", "n108-run")
	held, ok, err := acquireNativeFeedbackUseLock(name)
	if err != nil || !ok {
		t.Fatalf("could not acquire strict native lock: %v", err)
	}
	defer held.release()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^TestN108ChildNativeFeedbackMutex$")
	child.Dir = otherDir
	child.Env = append(os.Environ(),
		"AW_N108_MUTEX_CHILD=1",
		"AIR_WORKER_HOOK_STATE_DIR="+state,
	)
	output, err := child.CombinedOutput()
	if err != nil {
		t.Fatalf("other working directory acquired duplicate mutex: %v %s", err, output)
	}
	if !strings.Contains(string(output), "N108_BUSY") {
		t.Fatalf("other process did not confirm shared native mutex: %s", output)
	}
}

func TestN108ChildNativeFeedbackMutex(t *testing.T) {
	if os.Getenv("AW_N108_MUTEX_CHILD") != "1" {
		t.Skip("isolated subprocess only")
	}
	name := nativeFeedbackUseLockName("n108-principal", "n108-run")
	lock, ok, err := acquireNativeFeedbackUseLock(name)
	if ok || err != nil {
		if lock != nil {
			lock.release()
		}
		t.Fatalf("separate process acquired or failed native lock: acquired=%v err=%v", ok, err)
	}
	fmt.Println("N108_BUSY")
}
