package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlanReviewFixture(t *testing.T) (string, runConfig, string, []workStep) {
	t.Helper()
	root := t.TempDir()
	plan := "**Ц1.** deliver reviewed work\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n" +
		"|---|---|---|---|\n" +
		"| К1 | Ц1 | fact complete | факт `f01` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | implement feature | `terra:medium` | К1 |\n"
	planPath := filepath.Join(root, "PLAN.md")
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "goal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "goal", "checklist.json"),
		[]byte("{\"items\":[{\"id\":\"f01\",\"status\":\"completed\"}]}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runConfig{
		Plan:   "PLAN.md",
		Judge:  judgeSpec{Checklist: "goal/checklist.json", MinFacts: 1},
		Ladder: []string{"script", "terra:medium", "sol:medium"},
		Runners: map[string]runnerSpec{
			"terra": {Kind: "codex", Model: "gpt-5.6-terra"},
			"sol":   {Kind: "codex", Model: "gpt-5.6-sol"},
		},
	}
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, cfg, planPath, readPlanSteps(planPath)
}

func planReviewPASS(req planReviewRequest) string {
	v := planReviewVerdict{
		Verdict: "PASS", PlanSHA256: req.PlanSHA256,
		CompositionSHA256: req.CompositionSHA256,
		Findings:          []planReviewFinding{}, Next: "continue",
	}
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestPlanCompositionHashIgnoresTextAndDone(t *testing.T) {
	base := []workStep{
		{Num: "1", Tier: "terra:medium", Title: "one"},
		{Num: "2", Tier: "gate", Gate: true, Title: "approval"},
		{Num: "3", Tier: "sol:medium", Title: "three"},
	}
	want := planCompositionSHA(base)

	textOnly := append([]workStep(nil), base...)
	textOnly[0].Title = "renamed text"
	textOnly[0].Done = true
	if got := planCompositionSHA(textOnly); got != want {
		t.Fatalf("title/done changed composition: %s != %s", got, want)
	}

	reordered := []workStep{base[2], base[1], base[0]}
	if got := planCompositionSHA(reordered); got == want {
		t.Fatal("reorder did not change composition")
	}
	tierChanged := append([]workStep(nil), base...)
	tierChanged[0].Tier = "sol:medium"
	if got := planCompositionSHA(tierChanged); got == want {
		t.Fatal("tier change did not change composition")
	}
	added := append(append([]workStep(nil), base...), workStep{Num: "4", Tier: "script"})
	if got := planCompositionSHA(added); got == want {
		t.Fatal("added step did not change composition")
	}
}

func TestParsePlanReviewVerdictFailsClosed(t *testing.T) {
	planSHA := strings.Repeat("a", 64)
	compSHA := strings.Repeat("b", 64)
	valid := planReviewVerdict{
		Verdict: "COMMENTS", PlanSHA256: planSHA, CompositionSHA256: compSHA,
		Findings: []planReviewFinding{{Kind: "ORDER_RISK", Detail: "gate follows dependent live step"}},
		Next:     "move the gate before live work",
	}
	raw, _ := json.Marshal(valid)
	if _, err := parsePlanReviewVerdict(string(raw), planSHA, compSHA); err != nil {
		t.Fatalf("valid COMMENTS rejected: %v", err)
	}

	stale := valid
	stale.PlanSHA256 = strings.Repeat("c", 64)
	raw, _ = json.Marshal(stale)
	if _, err := parsePlanReviewVerdict(string(raw), planSHA, compSHA); err == nil {
		t.Fatal("stale plan hash accepted")
	}

	passWithFinding := valid
	passWithFinding.Verdict = "PASS"
	raw, _ = json.Marshal(passWithFinding)
	if _, err := parsePlanReviewVerdict(string(raw), planSHA, compSHA); err == nil {
		t.Fatal("PASS with finding accepted")
	}

	if _, err := parsePlanReviewVerdict("", planSHA, compSHA); err == nil {
		t.Fatal("empty output accepted")
	}
	if _, err := parsePlanReviewVerdict(`{"verdict":"PASS","plan_sha256":"`+planSHA+`","composition_sha256":"`+compSHA+`","findings":[],"next":"ok","extra":1}`, planSHA, compSHA); err == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestRunPlanReviewUsesOppositeVendorAndPreservesPlan(t *testing.T) {
	root, _, planPath, steps := writePlanReviewFixture(t)
	before, _ := os.ReadFile(planPath)

	old := planReviewInvoke
	defer func() { planReviewInvoke = old }()
	calls := 0
	var reviewer runnerSpec
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		calls++
		reviewer = req.Reviewer
		return planReviewPASS(req), "session", nil, intPtr(1), nil
	}

	run := runPlanReview(root, planPath, steps, legacyScope(root), runnerSpec{Kind: "codex", Model: "gpt-5.6-terra"})
	if run.State != "PASS" || calls != 1 {
		t.Fatalf("run=%#v calls=%d", run, calls)
	}
	if reviewer.Kind != "claude" {
		t.Fatalf("reviewer=%#v, want opposite claude", reviewer)
	}
	after, _ := os.ReadFile(planPath)
	if string(after) != string(before) {
		t.Fatal("read-only plan review changed PLAN bytes")
	}
}

func TestRunPlanReviewDetectsTamper(t *testing.T) {
	root, _, planPath, steps := writePlanReviewFixture(t)
	old := planReviewInvoke
	defer func() { planReviewInvoke = old }()
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		f, err := os.OpenFile(req.PlanPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return "", "", nil, nil, err
		}
		_, _ = f.WriteString("\n# reviewer tamper\n")
		_ = f.Close()
		return planReviewPASS(req), "", nil, nil, nil
	}
	run := runPlanReview(root, planPath, steps, legacyScope(root), runnerSpec{Kind: "codex"})
	if run.State != "NOT_PROVEN" || !strings.Contains(run.Record.Error, "changed during") {
		t.Fatalf("tamper was not fail-closed: %#v", run)
	}
}

func TestPlanReviewCacheUsesCompositionNotTextHash(t *testing.T) {
	root, _, planPath, steps := writePlanReviewFixture(t)
	old := planReviewInvoke
	defer func() { planReviewInvoke = old }()
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		return planReviewPASS(req), "", nil, nil, nil
	}
	run := runPlanReview(root, planPath, steps, legacyScope(root), runnerSpec{Kind: "codex"})
	if run.State != "PASS" {
		t.Fatalf("initial review=%#v", run)
	}

	raw, _ := os.ReadFile(planPath)
	changed := strings.Replace(string(raw), "implement feature", "implement feature with clearer wording", 1)
	if err := os.WriteFile(planPath, []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}
	newSteps := readPlanSteps(planPath)
	if planCompositionSHA(newSteps) != planCompositionSHA(steps) {
		t.Fatal("text-only edit changed composition")
	}
	rec, state, ok := readCurrentPlanReview(root, planCompositionSHA(newSteps))
	if !ok || state != "PASS" {
		t.Fatalf("text-only edit invalidated composition cache: ok=%v state=%q rec=%#v", ok, state, rec)
	}
	if rec.PlanSHA256 == sha256Hex([]byte(changed)) {
		t.Fatal("test did not actually change full-file hash")
	}
}

func TestEnsurePlanReviewCallsOncePerComposition(t *testing.T) {
	root, cfg, planPath, steps := writePlanReviewFixture(t)
	c := &loopCtx{Root: root, Cfg: cfg, Ladder: cfg.Ladder}
	old := planReviewInvoke
	defer func() { planReviewInvoke = old }()
	calls := 0
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		calls++
		return planReviewPASS(req), "", nil, nil, nil
	}

	if state, code := ensurePlanReview(c, planPath, steps); state != "PASS" || code != 0 {
		t.Fatalf("first state=%q code=%d", state, code)
	}
	if state, code := ensurePlanReview(c, planPath, steps); state != "PASS" || code != 0 || calls != 1 {
		t.Fatalf("cached state=%q code=%d calls=%d", state, code, calls)
	}

	textOnly := append([]workStep(nil), steps...)
	textOnly[0].Title = "different title"
	if state, code := ensurePlanReview(c, planPath, textOnly); state != "PASS" || code != 0 || calls != 1 {
		t.Fatalf("text-only state=%q code=%d calls=%d", state, code, calls)
	}

	tierChanged := append([]workStep(nil), steps...)
	tierChanged[0].Tier = "sol:medium"
	if state, code := ensurePlanReview(c, planPath, tierChanged); state != "PASS" || code != 0 || calls != 2 {
		t.Fatalf("tier-change state=%q code=%d calls=%d", state, code, calls)
	}
}

func TestPlanReviewUnavailableIsCachedNotProven(t *testing.T) {
	root, cfg, planPath, steps := writePlanReviewFixture(t)
	c := &loopCtx{Root: root, Cfg: cfg, Ladder: cfg.Ladder}
	old := planReviewInvoke
	defer func() { planReviewInvoke = old }()
	calls := 0
	planReviewInvoke = func(req planReviewRequest) (string, string, *float64, *int, error) {
		calls++
		return "", "", nil, nil, errors.New("opposite vendor unavailable")
	}
	if state, code := ensurePlanReview(c, planPath, steps); state != "NOT_PROVEN" || code != 2 {
		t.Fatalf("first state=%q code=%d", state, code)
	}
	if state, code := ensurePlanReview(c, planPath, steps); state != "NOT_PROVEN" || code != 2 || calls != 1 {
		t.Fatalf("cached state=%q code=%d calls=%d", state, code, calls)
	}
}

func TestFirstOpenModelExecutorSkipsScriptAndUsesConfiguredVendor(t *testing.T) {
	cfg := runConfig{
		Ladder:  []string{"script", "terra:medium"},
		Runners: map[string]runnerSpec{"terra": {Kind: "codex", Model: "gpt-5.6-terra"}},
	}
	steps := []workStep{
		{Num: "1", Tier: "script"},
		{Num: "2", Tier: "gate", Gate: true},
		{Num: "3", Tier: "terra:medium"},
	}
	runner, ok, err := firstOpenModelExecutor(cfg, cfg.Ladder, steps)
	if err != nil || !ok || runner.Kind != "codex" {
		t.Fatalf("runner=%#v ok=%v err=%v", runner, ok, err)
	}
}

func TestPlanReviewCacheRejectsSameVendorOrMismatchedHashes(t *testing.T) {
	root, _, _, steps := writePlanReviewFixture(t)
	comp := planCompositionSHA(steps)
	planSHA := strings.Repeat("a", 64)
	rec := planReviewRecord{
		Schema: planReviewSchemaVersion, Subject: "plan",
		PlanSHA256: planSHA, CompositionSHA256: comp,
		ExecutorVendor: "codex", ReviewerVendor: "codex", ReviewerSandbox: "read-only",
		Verdict: &planReviewVerdict{
			Verdict: "PASS", PlanSHA256: planSHA, CompositionSHA256: comp,
			Findings: []planReviewFinding{}, Next: "continue",
		},
	}
	publishPlanReview(root, rec)
	if _, state, ok := readCurrentPlanReview(root, comp); !ok || state != "NOT_PROVEN" {
		t.Fatalf("same-vendor cached PASS accepted: ok=%v state=%q", ok, state)
	}

	rec.ReviewerVendor = "claude"
	rec.Verdict.CompositionSHA256 = strings.Repeat("b", 64)
	publishPlanReview(root, rec)
	if _, state, ok := readCurrentPlanReview(root, comp); !ok || state != "NOT_PROVEN" {
		t.Fatalf("mismatched cached hash accepted: ok=%v state=%q", ok, state)
	}
}

func TestPlanReviewCommentsContinueAndDriftStops(t *testing.T) {
	for _, tc := range []struct {
		verdict string
		code    int
	}{
		{"COMMENTS", 0},
		{"DRIFT", 1},
		{"NOT_PROVEN", 2},
	} {
		if got := planReviewExit(tc.verdict); got != tc.code {
			t.Fatalf("%s code=%d want=%d", tc.verdict, got, tc.code)
		}
	}
}
