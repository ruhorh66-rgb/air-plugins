package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	planReviewSchemaVersion = "air-worker.plan-review/v1"
	planReviewMaxOutput     = 8192
)

const planReviewSchemaJSON = `{
  "type":"object",
  "additionalProperties":false,
  "required":["verdict","plan_sha256","composition_sha256","findings","next"],
  "properties":{
    "verdict":{"type":"string","enum":["PASS","COMMENTS","DRIFT"]},
    "plan_sha256":{"type":"string","pattern":"^[0-9a-fA-F]{64}$"},
    "composition_sha256":{"type":"string","pattern":"^[0-9a-fA-F]{64}$"},
    "findings":{
      "type":"array",
      "maxItems":3,
      "items":{
        "type":"object",
        "additionalProperties":false,
        "required":["kind","detail"],
        "properties":{
          "kind":{"type":"string","enum":["MISSING","REDUNDANT","ORDER_RISK"]},
          "detail":{"type":"string","minLength":1}
        }
      }
    },
    "next":{"type":"string","minLength":1}
  }
}`

type planReviewFinding struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

type planReviewVerdict struct {
	Verdict           string              `json:"verdict"`
	PlanSHA256        string              `json:"plan_sha256"`
	CompositionSHA256 string              `json:"composition_sha256"`
	Findings          []planReviewFinding `json:"findings"`
	Next              string              `json:"next"`
}

type planReviewRecord struct {
	Schema            string             `json:"schema"`
	At                string             `json:"at"`
	Subject           string             `json:"subject"`
	Plan              string             `json:"plan"`
	PlanSHA256        string             `json:"plan_sha256"`
	CompositionSHA256 string             `json:"composition_sha256"`
	ExecutorVendor    string             `json:"executor_vendor"`
	ReviewerVendor    string             `json:"reviewer_vendor"`
	ReviewerModel     string             `json:"reviewer_model,omitempty"`
	ReviewerEffort    string             `json:"reviewer_effort,omitempty"`
	ReviewerOverride  bool               `json:"reviewer_override,omitempty"`
	ReleaseVersion    string             `json:"release_version,omitempty"`
	ReviewerRole      string             `json:"reviewer_role"`
	ReviewerSandbox   string             `json:"reviewer_sandbox"`
	Session           string             `json:"session_id,omitempty"`
	Verdict           *planReviewVerdict `json:"verdict,omitempty"`
	Error             string             `json:"error,omitempty"`
	CostUSD           *float64           `json:"cost_usd,omitempty"`
	Turns             *int               `json:"turns,omitempty"`
}

type planReviewRequest struct {
	Scope             sessionScope
	PlanPath          string
	PlanSHA256        string
	CompositionSHA256 string
	Reviewer          runnerSpec
	Prompt            string
	AttemptKey        string
}

type planReviewRun struct {
	Record planReviewRecord
	State  string // PASS | COMMENTS | DRIFT | NOT_PROVEN
	Cached bool
}

var planReviewInvoke = invokePlanReviewModel
var errPlanReviewAlreadyAttempted = errors.New("already attempted; reconcile original evidence before retry")

func planReviewJobStep(req planReviewRequest) string {
	return "plan-" + req.PlanSHA256 + "-" + req.CompositionSHA256 + "-" + req.AttemptKey
}

func planReviewMarker(step string) string {
	return filepath.Join(".woody", "jobs", step+".started")
}

func planReviewOperation(reviewer runnerSpec) string {
	if reviewer.Kind == "codex" {
		return "semantic-plan-reviewer"
	}
	return "semantic-reviewer"
}

func reservePlanReviewAttempt(req planReviewRequest) error {
	if err := ensureOwnedJobsDir(req.Scope.Root); err != nil {
		return err
	}
	root, err := os.OpenRoot(req.Scope.Root)
	if err != nil {
		return err
	}
	defer root.Close()
	step := planReviewJobStep(req)
	name := planReviewMarker(step)
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return fmt.Errorf("plan review %s: %w", step, errPlanReviewAlreadyAttempted)
	}
	if err != nil {
		return err
	}
	// Preserve the original session-scoped job references across interruptions.
	receipt, _ := jobReceiptPaths(req.Scope, step, planReviewOperation(req.Reviewer))
	_, writeErr := fmt.Fprintln(f, filepath.Base(receipt))
	if writeErr == nil {
		writeErr = f.Sync()
	}
	return errors.Join(writeErr, f.Close())
}

// Recover an already-started attempt without launching a reviewer or changing
// the original receipt. Unknown/nonterminal/failed evidence remains blocked.
func recoverPlanReviewAttempt(req planReviewRequest) (string, string, *float64, *int, error) {
	no := func(err error) (string, string, *float64, *int, error) {
		return "", "", nil, nil, fmt.Errorf("reconcile original evidence: %w", err)
	}
	rootPath := req.Scope.Root
	if err := validateJobDir(rootPath); err != nil {
		return no(err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return no(err)
	}
	defer root.Close()
	step, operation := planReviewJobStep(req), planReviewOperation(req.Reviewer)
	marker := planReviewMarker(step)
	read := func(name string, limit int64) ([]byte, error) {
		info, err := root.Lstat(name)
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() || info.Size() > limit {
			return nil, errors.New("review evidence is not a bounded ordinary file")
		}
		f, err := root.Open(name)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		b, err := io.ReadAll(io.LimitReader(f, limit+1))
		if err == nil && int64(len(b)) > limit {
			return nil, errors.New("review evidence exceeds limit")
		}
		return b, err
	}
	b, err := read(marker, 1024)
	if err != nil {
		return no(err)
	}
	name := strings.TrimSpace(string(b))
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, "/\\:") || !strings.HasSuffix(name, ".receipt.json") {
		return no(errors.New("invalid review receipt reference"))
	}
	receipt := filepath.Join(rootPath, ".woody", "jobs", name)
	if err := jobOwnedEvidencePath(rootPath, receipt); err != nil {
		return no(fmt.Errorf("original receipt unavailable: %w", err))
	}
	r, err := readJobReceipt(receipt)
	if err != nil {
		return no(err)
	}
	id, err := parseIdentity(r.Principal, r.Session)
	if err != nil {
		return no(err)
	}
	original := newScope(rootPath, id)
	want, _ := jobReceiptPaths(original, step, operation)
	if receipt != want || r.Step != step || r.Operation != operation ||
		!strings.EqualFold(r.Provider, req.Reviewer.Kind) || !strings.EqualFold(r.Runner, req.Reviewer.Kind) ||
		!strings.EqualFold(r.Model, req.Reviewer.Model) || !strings.EqualFold(r.Effort, req.Reviewer.Effort) ||
		r.Role != operation || r.Sandbox != "read-only" {
		return no(errors.New("original receipt does not match this revision and reviewer route"))
	}
	prompt := req.Prompt
	if req.Reviewer.Kind == "codex" {
		prompt = ponytailPrompt(prompt)
	}
	if r.StdinSHA256 != sha256Hex([]byte(prompt)) {
		return no(errors.New("original reviewer input does not match this exact plan"))
	}
	status, err := inspectOwnedJob(original, step, operation)
	if err != nil {
		return no(err)
	}
	if status.JobID != r.JobID || !status.Terminal || status.Status != jobStatusDone ||
		status.ExitCode == nil || *status.ExitCode != 0 || !r.ProcessStarted || r.PID <= 0 {
		return no(fmt.Errorf("original review not proven successful: %s %s", status.Status, status.Reason))
	}
	out, err := read(filepath.Join(".woody", "jobs", filepath.Base(status.OutputPath)), 32<<20)
	if err != nil {
		return no(err)
	}
	if int64(len(out)) != status.OutputBytes || sha256Hex(out) != status.OutputSHA256 {
		return no(errors.New("original output changed after verification"))
	}
	switch req.Reviewer.Kind {
	case "claude":
		return parseClaudeSemanticResult(decodeOutput(out), nil)
	case "codex":
		return parseCodexReviewerStream(out, nil, "codex plan reviewer")
	default:
		return no(fmt.Errorf("unsupported reviewer %q", req.Reviewer.Kind))
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func planCompositionSHA(steps []workStep) string {
	type entry struct {
		Num  string `json:"num"`
		Tier string `json:"tier"`
		Gate bool   `json:"gate"`
	}
	rows := make([]entry, 0, len(steps))
	for _, step := range steps {
		rows = append(rows, entry{
			Num: strings.TrimSpace(step.Num), Tier: strings.TrimSpace(step.Tier), Gate: step.Gate,
		})
	}
	raw, _ := json.Marshal(rows)
	return sha256Hex(raw)
}

func parsePlanReviewVerdict(raw, wantPlanSHA, wantCompositionSHA string) (planReviewVerdict, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if raw == "" {
		return planReviewVerdict{}, errors.New("plan reviewer returned empty output")
	}
	if len([]byte(raw)) > planReviewMaxOutput {
		return planReviewVerdict{}, fmt.Errorf("plan reviewer output is %d bytes; limit is %d", len([]byte(raw)), planReviewMaxOutput)
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	var v planReviewVerdict
	if err := dec.Decode(&v); err != nil {
		return planReviewVerdict{}, fmt.Errorf("plan reviewer returned invalid JSON contract: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return planReviewVerdict{}, errors.New("plan reviewer returned more than one JSON value")
		}
		return planReviewVerdict{}, fmt.Errorf("plan reviewer returned trailing content: %w", err)
	}
	switch v.Verdict {
	case "PASS", "COMMENTS", "DRIFT":
	default:
		return planReviewVerdict{}, fmt.Errorf("invalid plan verdict %q", v.Verdict)
	}
	if !strings.EqualFold(v.PlanSHA256, wantPlanSHA) {
		return planReviewVerdict{}, fmt.Errorf("stale plan_sha256: got %s want %s", v.PlanSHA256, wantPlanSHA)
	}
	if !strings.EqualFold(v.CompositionSHA256, wantCompositionSHA) {
		return planReviewVerdict{}, fmt.Errorf("stale composition_sha256: got %s want %s", v.CompositionSHA256, wantCompositionSHA)
	}
	if len(v.Findings) > 3 {
		return planReviewVerdict{}, fmt.Errorf("plan reviewer returned %d findings; limit is 3", len(v.Findings))
	}
	for i, finding := range v.Findings {
		switch finding.Kind {
		case "MISSING", "REDUNDANT", "ORDER_RISK":
		default:
			return planReviewVerdict{}, fmt.Errorf("findings[%d].kind=%q is invalid", i, finding.Kind)
		}
		if strings.TrimSpace(finding.Detail) == "" {
			return planReviewVerdict{}, fmt.Errorf("findings[%d].detail is empty", i)
		}
	}
	if strings.TrimSpace(v.Next) == "" {
		return planReviewVerdict{}, errors.New("plan reviewer returned empty next")
	}
	if v.Verdict == "PASS" && len(v.Findings) != 0 {
		return planReviewVerdict{}, errors.New("PASS requires zero findings")
	}
	if (v.Verdict == "COMMENTS" || v.Verdict == "DRIFT") && len(v.Findings) == 0 {
		return planReviewVerdict{}, fmt.Errorf("%s requires at least one finding", v.Verdict)
	}
	return v, nil
}

func planReviewPaths(root string) (latest, history string) {
	dir := filepath.Join(root, ".woody")
	return filepath.Join(dir, "plan-semantic-verdict.json"), filepath.Join(dir, "plan-semantic-history.jsonl")
}

func publishPlanReview(root string, rec planReviewRecord) {
	latest, history := planReviewPaths(root)
	_ = os.MkdirAll(filepath.Dir(latest), 0o755)
	_ = writePlanReviewLatest(latest, rec)
	if f, err := os.OpenFile(history, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_ = json.NewEncoder(f).Encode(rec)
		_ = f.Close()
	}
}

func writePlanReviewLatest(path string, rec planReviewRecord) error {
	b, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(b, '\n'))
}

type planReviewExpectation struct {
	Executor         runnerSpec
	Reviewer         runnerSpec
	ExplicitOverride bool
	ReleaseVersion   string
}

func planReviewAttemptKey(e planReviewExpectation) string {
	route, _ := json.Marshal([]string{
		strings.ToLower(e.Executor.Kind), strings.ToLower(e.Reviewer.Kind),
		strings.ToLower(e.Reviewer.Model), strings.ToLower(e.Reviewer.Effort),
		fmt.Sprint(e.ExplicitOverride), strings.ToLower(e.ReleaseVersion),
	})
	return sha256Hex(route)
}

func readCurrentPlanReview(root, planSHA, compositionSHA string, expectations ...planReviewExpectation) (planReviewRecord, string, bool) {
	latest, _ := planReviewPaths(root)
	var rec planReviewRecord
	if readJSON(latest, &rec) != nil {
		return planReviewRecord{}, "", false
	}
	return matchPlanReview(rec, planSHA, compositionSHA, expectations...)
}

func readHistoricalPlanReview(root, planSHA, compositionSHA string, expected planReviewExpectation) (planReviewRecord, string, bool, error) {
	_, history := planReviewPaths(root)
	f, err := os.Open(history)
	if errors.Is(err, os.ErrNotExist) {
		return planReviewRecord{}, "", false, nil
	}
	if err != nil {
		return planReviewRecord{}, "", false, err
	}
	defer f.Close()
	var record planReviewRecord
	var state string
	var found bool
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		var rec planReviewRecord
		if json.Unmarshal(scan.Bytes(), &rec) == nil {
			if rec, stat, ok := matchPlanReview(rec, planSHA, compositionSHA, expected); ok {
				record, state, found = rec, stat, true
			}
		}
	}
	return record, state, found, scan.Err()
}

func matchPlanReview(rec planReviewRecord, planSHA, compositionSHA string, expectations ...planReviewExpectation) (planReviewRecord, string, bool) {
	if rec.Schema != planReviewSchemaVersion || rec.Subject != "plan" {
		return planReviewRecord{}, "", false
	}
	if !strings.EqualFold(rec.PlanSHA256, planSHA) || !strings.EqualFold(rec.CompositionSHA256, compositionSHA) {
		return rec, "", false
	}
	if len(expectations) > 0 {
		expected := expectations[0]
		if !strings.EqualFold(rec.ExecutorVendor, expected.Executor.Kind) ||
			!strings.EqualFold(rec.ReviewerVendor, expected.Reviewer.Kind) ||
			!strings.EqualFold(rec.ReviewerModel, expected.Reviewer.Model) ||
			!strings.EqualFold(rec.ReviewerEffort, expected.Reviewer.Effort) ||
			rec.ReviewerOverride != expected.ExplicitOverride ||
			!strings.EqualFold(rec.ReleaseVersion, expected.ReleaseVersion) {
			return rec, "", false
		}
	}
	if strings.TrimSpace(rec.Error) != "" || rec.Verdict == nil {
		return rec, "NOT_PROVEN", true
	}
	allowSameVendor := len(expectations) > 0 && expectations[0].ExplicitOverride && rec.ReviewerOverride
	if !strings.EqualFold(rec.Verdict.PlanSHA256, planSHA) ||
		!strings.EqualFold(rec.Verdict.CompositionSHA256, compositionSHA) ||
		strings.TrimSpace(rec.ReviewerVendor) == "" ||
		(strings.EqualFold(rec.ReviewerVendor, rec.ExecutorVendor) && !allowSameVendor) ||
		rec.ReviewerSandbox != "read-only" {
		return rec, "NOT_PROVEN", true
	}
	switch rec.Verdict.Verdict {
	case "PASS", "COMMENTS", "DRIFT":
		return rec, rec.Verdict.Verdict, true
	default:
		return rec, "NOT_PROVEN", true
	}
}

func planReviewPrompt(planPath, planSHA, compositionSHA string) string {
	return `You are Air Worker Semantic Plan Judge. Execute this review NOW.
Review the DECOMPOSITION and ORDER of the current PLAN, not whether implementation is already complete.
Read the PLAN file from the workspace. You are read-only: do not edit any file and do not propose edits outside the verdict.
Look only for MISSING work needed by the stated goals, REDUNDANT duplicated work, and ORDER_RISK where dependencies/gates are sequenced unsafely.
Do not close LPR/human gates. Do not replace deterministic validate/plan-lint with your judgment.
The plan file MUST remain byte-identical during this review.

Expected full-file plan_sha256: ` + planSHA + `
Expected composition_sha256: ` + compositionSHA + `
PLAN path: ` + planPath + `

Your entire final response MUST be exactly one JSON object:
{"verdict":"PASS|COMMENTS|DRIFT","plan_sha256":"<exact expected hash>","composition_sha256":"<exact expected hash>","findings":[{"kind":"MISSING|REDUNDANT|ORDER_RISK","detail":"short factual finding"}],"next":"one short next action"}

Rules:
- PASS: zero findings; current decomposition/order is acceptable.
- COMMENTS: up to 3 non-blocking findings.
- DRIFT: up to 3 findings that make the decomposition/order unsafe or materially off-goal.
- Never invent hashes. Echo the exact hashes above only after reviewing this exact PLAN.
- Output JSON only, no markdown or acknowledgment.
`
}

func invokePlanReviewCodex(req planReviewRequest, exePath string) (string, string, *float64, *int, error) {
	if err := requirePonytailSkill(); err != nil {
		return "", "", nil, nil, fmt.Errorf("Ponytail: %w", err)
	}
	schema, err := os.CreateTemp("", "air-worker-plan-review-schema-*.json")
	if err != nil {
		return "", "", nil, nil, err
	}
	schemaPath := schema.Name()
	defer os.Remove(schemaPath)
	if _, err := schema.WriteString(planReviewSchemaJSON); err != nil {
		_ = schema.Close()
		return "", "", nil, nil, err
	}
	if err := schema.Close(); err != nil {
		return "", "", nil, nil, err
	}
	cmd := semanticCodexCommand(exePath, req.Scope.Root, req.Prompt, req.Reviewer, schemaPath)
	out, runErr := runReceiptedWithMeta(context.Background(), req.Scope, planReviewJobStep(req), "semantic-plan-reviewer", cmd, jobReceiptMeta{
		Runner: req.Reviewer.Kind, Provider: req.Reviewer.Kind, Model: req.Reviewer.Model, Effort: req.Reviewer.Effort,
		Role: "semantic-plan-reviewer", Sandbox: "read-only",
	})
	return parseCodexReviewerStream(out, runErr, "codex plan reviewer")
}

func invokePlanReviewModel(req planReviewRequest) (string, string, *float64, *int, error) {
	exePath, err := resolveRunnerTool(req.Reviewer.Kind)
	if err != nil {
		return "", "", nil, nil, err
	}
	switch req.Reviewer.Kind {
	case "claude":
		return invokeSemanticClaude(req.Scope, exePath, req.Prompt, req.Reviewer, planReviewJobStep(req))
	case "codex":
		return invokePlanReviewCodex(req, exePath)
	default:
		return "", "", nil, nil, fmt.Errorf("unsupported plan reviewer %q", req.Reviewer.Kind)
	}
}

func runPlanReview(root, planPath string, steps []workStep, scope sessionScope, executor runnerSpec, policies ...runConfig) planReviewRun {
	rawBefore, err := os.ReadFile(planPath)
	planSHA := ""
	if err == nil {
		planSHA = sha256Hex(rawBefore)
	}
	compositionSHA := planCompositionSHA(steps)
	cfg := runConfig{}
	if len(policies) > 0 {
		cfg = policies[0]
	}
	release := activeProductReleaseVersion(root)
	reviewer, explicitOverride, reviewerErr := cfg.SemanticJudge.Resolve(executor, release)
	rec := planReviewRecord{
		Schema: planReviewSchemaVersion, At: time.Now().UTC().Format(time.RFC3339Nano), Subject: "plan",
		Plan: planPath, PlanSHA256: planSHA, CompositionSHA256: compositionSHA,
		ExecutorVendor: executor.Kind, ReviewerVendor: reviewer.Kind, ReviewerModel: reviewer.Model,
		ReviewerEffort: reviewer.Effort, ReviewerOverride: explicitOverride,
		ReleaseVersion: release,
		ReviewerRole:   "semantic-plan-reviewer", ReviewerSandbox: "read-only",
	}
	blocked := func(e error) planReviewRun {
		rec.Error = e.Error()
		return planReviewRun{Record: rec, State: "NOT_PROVEN"}
	}
	if err != nil {
		return blocked(err)
	}
	if reviewerErr != nil {
		return blocked(reviewerErr)
	}
	if err := enforceIndependentAnthropicReviewer(cfg, executor, reviewer); err != nil {
		return blocked(err)
	}
	if reviewer.Kind == executor.Kind && !explicitOverride || reviewer.Kind == "" {
		return blocked(fmt.Errorf("plan reviewer must be opposite vendor unless explicitly configured: executor=%s reviewer=%s", executor.Kind, reviewer.Kind))
	}
	// Lock the published verdict across sessions on Windows; the exclusive
	// durable reservation below prevents duplicate matching attempts after a crash.
	lock, ok := acquireLock(lockName("plan-review", root))
	if !ok {
		rec.Error = "plan review already in progress for product"
		return planReviewRun{Record: rec, State: "NOT_PROVEN"}
	}
	defer lock.release()
	fail := func(e error) planReviewRun {
		run := blocked(e)
		publishPlanReview(root, run.Record)
		return run
	}
	expected := planReviewExpectation{Executor: executor, Reviewer: reviewer, ExplicitOverride: explicitOverride, ReleaseVersion: release}
	if saved, state, ok := readCurrentPlanReview(root, planSHA, compositionSHA, expected); ok {
		return planReviewRun{Record: saved, State: state, Cached: true}
	}
	if saved, state, ok, err := readHistoricalPlanReview(root, planSHA, compositionSHA, expected); err != nil {
		return blocked(err)
	} else if ok {
		latest, _ := planReviewPaths(root)
		if err := writePlanReviewLatest(latest, saved); err != nil {
			return blocked(err)
		}
		return planReviewRun{Record: saved, State: state, Cached: true}
	}
	req := planReviewRequest{
		Scope: scope, PlanPath: planPath, PlanSHA256: planSHA, CompositionSHA256: compositionSHA,
		Reviewer: reviewer, Prompt: planReviewPrompt(planPath, planSHA, compositionSHA),
		AttemptKey: planReviewAttemptKey(expected),
	}
	var output, session string
	var cost *float64
	var turns *int
	var invokeErr error
	if err := reservePlanReviewAttempt(req); err == nil {
		output, session, cost, turns, invokeErr = planReviewInvoke(req)
	} else if errors.Is(err, errPlanReviewAlreadyAttempted) {
		output, session, cost, turns, invokeErr = recoverPlanReviewAttempt(req)
		if invokeErr != nil {
			return blocked(invokeErr)
		}
	} else {
		return blocked(err)
	}
	rec.Session, rec.CostUSD, rec.Turns = session, cost, turns
	rawAfter, afterErr := os.ReadFile(planPath)
	if afterErr != nil {
		return fail(fmt.Errorf("PLAN unreadable after review: %w", afterErr))
	}
	if !bytes.Equal(rawBefore, rawAfter) {
		return fail(errors.New("PLAN changed during read-only semantic review"))
	}
	if invokeErr != nil {
		// A transport error does not prove the reviewer did not finish.
		// Leave the reservation for a later readback, without caching a failure.
		return blocked(invokeErr)
	}
	verdict, parseErr := parsePlanReviewVerdict(output, planSHA, compositionSHA)
	if parseErr != nil {
		return fail(parseErr)
	}
	rec.Verdict = &verdict
	publishPlanReview(root, rec)
	return planReviewRun{Record: rec, State: verdict.Verdict}
}

func firstOpenModelExecutor(cfg runConfig, ladder []string, steps []workStep) (runnerSpec, bool, error) {
	for _, step := range steps {
		if step.Done || step.Gate || tierName(step.Tier) == "script" {
			continue
		}
		match := resolveLadderTier(ladder, step.Tier)
		if !match.Found {
			return runnerSpec{}, false, fmt.Errorf("step %s tier %q is not reachable in ladder", step.Num, step.Tier)
		}
		runner := resolveRunner(cfg, ladder[match.Index])
		if runner.Kind == "" || runner.Kind == "script" {
			return runnerSpec{}, false, fmt.Errorf("step %s tier %q resolves to invalid model runner %q", step.Num, step.Tier, runner.Kind)
		}
		return runner, true, nil
	}
	return runnerSpec{}, false, nil
}

func planReviewExit(state string) int {
	switch state {
	case "PASS", "COMMENTS":
		return 0
	case "DRIFT":
		return 1
	default:
		return 2
	}
}

func ensurePlanReview(c *loopCtx, planPath string, steps []workStep) (string, int) {
	executor, needed, err := firstOpenModelExecutor(c.Cfg, c.Ladder, steps)
	if err != nil {
		line("plan review: NOT_PROVEN — " + err.Error())
		return "NOT_PROVEN", 2
	}
	if !needed {
		return "NOT_REQUIRED", 0
	}
	reviewer, explicitOverride, err := c.Cfg.SemanticJudge.Resolve(executor, activeProductReleaseVersion(c.Root))
	if err != nil {
		line("plan review: NOT_PROVEN — " + err.Error())
		return "NOT_PROVEN", 2
	}
	expectation := planReviewExpectation{Executor: executor, Reviewer: reviewer, ExplicitOverride: explicitOverride, ReleaseVersion: activeProductReleaseVersion(c.Root)}
	compositionSHA := planCompositionSHA(steps)
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		line("plan review: NOT_PROVEN — " + err.Error())
		return "NOT_PROVEN", 2
	}
	planSHA := sha256Hex(planBytes)
	if rec, state, ok := readCurrentPlanReview(c.Root, planSHA, compositionSHA, expectation); ok {
		line(fmt.Sprintf("plan review: cached %s · composition %s · reviewer %s",
			state, compositionSHA[:12], rec.ReviewerVendor))
		return state, planReviewExit(state)
	}
	if c.WhatIf {
		if rec, state, ok, err := readHistoricalPlanReview(c.Root, planSHA, compositionSHA, expectation); err == nil && ok {
			line(fmt.Sprintf("plan review: historical %s · composition %s · reviewer %s", state, compositionSHA[:12], rec.ReviewerVendor))
			return state, planReviewExit(state)
		}
		line("plan review: WHATIF — plan bytes/composition changed or unreviewed; model call skipped")
		return "NOT_PROVEN", 0
	}
	run := runPlanReview(c.Root, planPath, steps, c.scope(), executor, c.Cfg)
	if run.Record.Error != "" {
		line("plan review: " + run.Record.Error)
	}
	line(fmt.Sprintf("plan review: %s · composition %s · reviewer %s",
		run.State, compositionSHA[:12], run.Record.ReviewerVendor))
	return run.State, planReviewExit(run.State)
}

func cmdPlanReview(argv []string) int {
	fs := flag.NewFlagSet("plan-review", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	configPath := fs.String("config", "", "путь к run-config.json")
	executorName := fs.String("executor", "auto", "кто будет исполнять model-step: auto|claude|codex|chatgpt|router")
	force := fs.Bool("force", false, "перепроверить запись текущей редакции без повторного запуска уже начатого review")
	asJSON := fs.Bool("json", false, "машинный вывод")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan-review:", err)
		return 2
	}
	validation, validationCode := validateProduct(root)
	if validationCode != 0 {
		if *asJSON {
			b, _ := json.MarshalIndent(map[string]any{"state": "NOT_PROVEN", "validation": validation}, "", "  ")
			fmt.Println(string(b))
		} else {
			fmt.Printf("plan-review: NOT_PROVEN — validate found %d problems%s", len(validation.Problems), lineEnding)
		}
		return 2
	}
	cfgPath := strings.TrimSpace(*configPath)
	if cfgPath == "" {
		cfgPath = filepath.Join(root, "run-config.json")
	} else if !filepath.IsAbs(cfgPath) {
		cfgPath = filepath.Join(root, cfgPath)
	}
	if err := enforceCanonicalNativeRouteConfig(root, cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, "plan-review:", err)
		return 2
	}
	var cfg runConfig
	if err := readJSON(cfgPath, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "plan-review:", err)
		return 2
	}
	planPath := nativePlanPath(root, cfg)
	steps := readPlanSteps(planPath)
	compositionSHA := planCompositionSHA(steps)
	planBytes, err := os.ReadFile(planPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan-review:", err)
		return 2
	}
	planSHA := sha256Hex(planBytes)
	var executor runnerSpec
	if strings.EqualFold(strings.TrimSpace(*executorName), "auto") || strings.TrimSpace(*executorName) == "" {
		ladder := cfg.Ladder
		if len(ladder) == 0 {
			ladder = []string{"script", "haiku", "sonnet", "opus"}
		}
		var needed bool
		executor, needed, err = firstOpenModelExecutor(cfg, ladder, steps)
		if err != nil {
			fmt.Fprintln(os.Stderr, "plan-review:", err)
			return 2
		}
		if !needed {
			if *asJSON {
				fmt.Println(`{"state":"NOT_REQUIRED","cached":false}`)
			} else {
				fmt.Print("plan-review: NOT_REQUIRED — открытых model-step нет" + lineEnding)
			}
			return 0
		}
	} else {
		executor, err = semanticExecutorSpec(*executorName)
		if err != nil {
			fmt.Fprintln(os.Stderr, "plan-review:", err)
			return 2
		}
	}
	reviewer, explicitOverride, err := cfg.SemanticJudge.Resolve(executor, activeProductReleaseVersion(root))
	if err != nil {
		fmt.Fprintln(os.Stderr, "plan-review:", err)
		return 2
	}
	if err := enforceIndependentAnthropicReviewer(cfg, executor, reviewer); err != nil {
		fmt.Fprintln(os.Stderr, "plan-review:", err)
		return 2
	}
	expectation := planReviewExpectation{Executor: executor, Reviewer: reviewer, ExplicitOverride: explicitOverride, ReleaseVersion: activeProductReleaseVersion(root)}
	if !*force {
		if rec, state, ok := readCurrentPlanReview(root, planSHA, compositionSHA, expectation); ok {
			if *asJSON {
				b, _ := json.MarshalIndent(map[string]any{"state": state, "cached": true, "record": rec}, "", "  ")
				fmt.Println(string(b))
			} else {
				fmt.Printf("plan-review: cached %s · composition %s · reviewer %s%s",
					state, compositionSHA[:12], rec.ReviewerVendor, lineEnding)
			}
			return planReviewExit(state)
		}
	}
	run := runPlanReview(root, planPath, steps, legacyScope(root), executor, cfg)
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"state": run.State, "cached": run.Cached, "record": run.Record}, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("plan-review: %s · composition %s · reviewer %s%s",
			run.State, compositionSHA[:12], run.Record.ReviewerVendor, lineEnding)
	}
	return planReviewExit(run.State)
}
