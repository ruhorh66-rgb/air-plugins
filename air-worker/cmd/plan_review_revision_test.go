package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestPlanReviewRevisionCreatesIndependentReceipts(t *testing.T) {
	root, cfg, planPath, steps := writePlanReviewFixture(t)
	var calls int
	var bStep string
	fakeJob := func(req planReviewRequest, step string) (string, error) {
		if err := ensureOwnedJobsDir(root); err != nil {
			return "", fmt.Errorf("ensure jobs: %w", err)
		}
		receipt, output := jobReceiptPaths(req.Scope, step, "semantic-reviewer")
		_, lock, err := startJobReceipt(req.Scope, step, "semantic-reviewer", receipt, output)
		if err != nil {
			return "", fmt.Errorf("start receipt: %w", err)
		}
		defer lock.release()
		calls++
		result := planReviewPASS(req)
		if err := writeOwnedJobOutput(root, output, []byte(result)); err != nil {
			return "", fmt.Errorf("write output: %w", err)
		}
		if err := finishJobReceipt(receipt, jobStatusDone); err != nil {
			return "", fmt.Errorf("finish receipt: %w", err)
		}
		return result, nil
	}
	beforePlan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	aSHA, aComposition := sha256Hex(beforePlan), planCompositionSHA(steps)
	executor, _, err := firstOpenModelExecutor(cfg, cfg.Ladder, steps)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, override, err := cfg.SemanticJudge.Resolve(executor, activeProductReleaseVersion(root))
	if err != nil {
		t.Fatal(err)
	}
	reqA := planReviewRequest{Scope: legacyScope(root), PlanPath: planPath, PlanSHA256: aSHA, CompositionSHA256: aComposition, Reviewer: reviewer}
	out, err := fakeJob(reqA, "plan")
	if err != nil {
		t.Fatalf("legacy A job: %v", err)
	}
	verdict, err := parsePlanReviewVerdict(out, aSHA, aComposition)
	if err != nil {
		t.Fatal(err)
	}
	publishPlanReview(root, planReviewRecord{Schema: planReviewSchemaVersion, Subject: "plan", Plan: planPath, PlanSHA256: aSHA, CompositionSHA256: aComposition, ExecutorVendor: executor.Kind, ReviewerVendor: reviewer.Kind, ReviewerModel: reviewer.Model, ReviewerEffort: reviewer.Effort, ReviewerOverride: override, ReviewerRole: "semantic-plan-reviewer", ReviewerSandbox: "read-only", ReleaseVersion: activeProductReleaseVersion(root), Verdict: &verdict})
	oldReceipt, _ := jobReceiptPaths(legacyScope(root), "plan", "semantic-reviewer")
	before, err := os.ReadFile(oldReceipt)
	if err != nil {
		t.Fatalf("A receipt: %v", err)
	}
	oldInvoke := planReviewInvoke
	defer func() { planReviewInvoke = oldInvoke }()
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		bStep = planReviewJobStep(req)
		s, e := fakeJob(req, bStep)
		return s, "fake-model", nil, nil, e
	}
	revised := append(append([]byte{}, beforePlan...), []byte("\nRevision B.\n")...)
	if err := os.WriteFile(planPath, revised, 0600); err != nil {
		t.Fatal(err)
	}
	c := &loopCtx{Root: root, Cfg: cfg, Ladder: cfg.Ladder}
	if state, code := ensurePlanReview(c, planPath, readPlanSteps(planPath)); state != "PASS" || code != 0 {
		t.Fatalf("revision B: %s rc=%d calls=%d", state, code, calls)
	}
	if state, code := ensurePlanReview(c, planPath, readPlanSteps(planPath)); state != "PASS" || code != 0 {
		t.Fatalf("repeat B: %s rc=%d", state, code)
	}
	after, err := os.ReadFile(oldReceipt)
	if err != nil || string(after) != string(before) {
		t.Fatalf("A receipt changed: %v", err)
	}
	if calls != 2 {
		t.Fatalf("model calls = %d, want exactly A and B", calls)
	}
	bReceipt, _ := jobReceiptPaths(legacyScope(root), bStep, "semantic-reviewer")
	if oldReceipt == bReceipt {
		t.Fatal("revision B reused legacy receipt path")
	}
	rec, err := readJobReceipt(bReceipt)
	if err != nil || rec.Step != bStep || rec.Status != jobStatusDone {
		t.Fatalf("B receipt: %#v %v", rec, err)
	}
	if bStep == "plan" {
		t.Fatal("B job identity missing plan revision")
	}
	if got := sha256Hex(revised); got == aSHA {
		t.Fatal("fixture revisions unexpectedly equal")
	} else if current, state, ok := readCurrentPlanReview(root, got, aComposition); !ok || state != "PASS" || current.Verdict.PlanSHA256 != got {
		t.Fatalf("B verdict not bound to correct plan SHA: %#v %s", current, state)
	}
	if err := os.WriteFile(planPath, beforePlan, 0600); err != nil {
		t.Fatal(err)
	}
	if state, code := ensurePlanReview(c, planPath, readPlanSteps(planPath)); state != "PASS" || code != 0 || calls != 2 {
		t.Fatalf("A→B→A should restore authentic verdict without provider replay: %s rc=%d calls=%d", state, code, calls)
	}
	if after, err := os.ReadFile(oldReceipt); err != nil || string(after) != string(before) {
		t.Fatalf("A receipt changed after returning to A: %v", err)
	}
}

func TestPlanReviewConcurrentDoesNotRepeatProvider(t *testing.T) {
	root, cfg, planPath, steps := writePlanReviewFixture(t)
	old := planReviewInvoke
	defer func() { planReviewInvoke = old }()
	entered := make(chan struct{}, 2)
	done := make(chan struct{})
	defer func() {
		select {
		case <-done:
		default:
			close(done)
		}
	}()
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		entered <- struct{}{}
		<-done
		return planReviewPASS(req), "fake-model", nil, nil, nil
	}
	first := make(chan planReviewRun, 1)
	executor := runnerSpec{Kind: "codex"}
	go func() { first <- runPlanReview(root, planPath, steps, legacyScope(root), executor, cfg) }()
	select {
	case <-entered:
	case run := <-first:
		t.Fatalf("first attempt did not reach provider: %#v", run.Record)
	}
	id, err := parseIdentity("other", "another-session")
	if err != nil {
		t.Fatal(err)
	}
	second := make(chan planReviewRun, 1)
	go func() { second <- runPlanReview(root, planPath, steps, newScope(root, id), executor, cfg) }()
	select {
	case run := <-second:
		if run.State != "NOT_PROVEN" {
			t.Errorf("in-flight return = %s, want NOT_PROVEN", run.State)
		}
		close(done)
	case <-entered:
		close(done)
		<-second
		<-first
		t.Fatal("concurrent duplicate provider call")
	}
	if run := <-first; run.State != "PASS" {
		t.Fatalf("first attempt = %#v", run.Record)
	}
	if run := runPlanReview(root, planPath, steps, newScope(root, id), executor, cfg); !run.Cached || run.State != "PASS" {
		t.Fatalf("completed attempt was not reused: %#v", run.Record)
	}
	select {
	case <-entered:
		t.Fatal("provider called more than once")
	default:
	}
}

func TestPlanReviewInterruptedAttemptRequiresOriginalEvidence(t *testing.T) {
	root, cfg, planPath, steps := writePlanReviewFixture(t)
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	executor := runnerSpec{Kind: "codex"}
	release := activeProductReleaseVersion(root)
	reviewer, override, err := cfg.SemanticJudge.Resolve(executor, release)
	if err != nil {
		t.Fatal(err)
	}
	expected := planReviewExpectation{Executor: executor, Reviewer: reviewer, ExplicitOverride: override, ReleaseVersion: release}
	req := planReviewRequest{
		Scope: legacyScope(root), PlanPath: planPath, PlanSHA256: sha256Hex(plan),
		CompositionSHA256: planCompositionSHA(steps), Reviewer: reviewer, AttemptKey: planReviewAttemptKey(expected),
	}
	if err := reservePlanReviewAttempt(req); err != nil {
		t.Fatal(err)
	}
	id, err := parseIdentity("other", "another-session")
	if err != nil {
		t.Fatal(err)
	}
	old := planReviewInvoke
	defer func() { planReviewInvoke = old }()
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		t.Fatal("provider invoked a second time after ambiguous interrupted start")
		return "", "", nil, nil, nil
	}
	run := runPlanReview(root, planPath, steps, newScope(root, id), executor, cfg)
	if run.State != "NOT_PROVEN" || !strings.Contains(run.Record.Error, "reconcile original evidence") {
		t.Fatalf("interrupted start was not preserved: %#v", run.Record)
	}
	latest, _ := planReviewPaths(root)
	if _, err := os.Stat(latest); !os.IsNotExist(err) {
		t.Fatalf("interrupted attempt must not publish a verdict: %v", err)
	}
}
