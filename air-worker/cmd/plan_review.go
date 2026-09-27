package main

import (
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
}

type planReviewRun struct {
	Record planReviewRecord
	State  string // PASS | COMMENTS | DRIFT | NOT_PROVEN
}

var planReviewInvoke = invokePlanReviewModel

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
	if b, err := json.MarshalIndent(rec, "", "  "); err == nil {
		_ = writeFileAtomic(latest, append(b, '\n'))
	}
	if f, err := os.OpenFile(history, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_ = json.NewEncoder(f).Encode(rec)
		_ = f.Close()
	}
}

func readCurrentPlanReview(root, compositionSHA string) (planReviewRecord, string, bool) {
	latest, _ := planReviewPaths(root)
	var rec planReviewRecord
	if readJSON(latest, &rec) != nil || rec.Schema != planReviewSchemaVersion || rec.Subject != "plan" {
		return planReviewRecord{}, "", false
	}
	if !strings.EqualFold(rec.CompositionSHA256, compositionSHA) {
		return rec, "", false
	}
	if strings.TrimSpace(rec.Error) != "" || rec.Verdict == nil {
		return rec, "NOT_PROVEN", true
	}
	if !strings.EqualFold(rec.Verdict.PlanSHA256, rec.PlanSHA256) ||
		!strings.EqualFold(rec.Verdict.CompositionSHA256, rec.CompositionSHA256) ||
		strings.TrimSpace(rec.ReviewerVendor) == "" ||
		strings.EqualFold(rec.ReviewerVendor, rec.ExecutorVendor) ||
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
	out, runErr := runReceiptedWithMeta(context.Background(), req.Scope, "plan", "semantic-plan-reviewer", cmd, jobReceiptMeta{
		Runner: req.Reviewer.Kind, Provider: req.Reviewer.Kind, Model: req.Reviewer.Model, Effort: req.Reviewer.Effort,
		Role: "semantic-plan-reviewer", Sandbox: "read-only",
	})
	completed, failed := false, false
	session, turns := "", 0
	var cost *float64
	var messages, failures []string
	for _, line := range strings.Split(strings.ReplaceAll(decodeOutput(out), "\r\n", "\n"), "\n") {
		text := strings.TrimSpace(line)
		if !strings.HasPrefix(text, "{") {
			continue
		}
		var event codexEvent
		if json.Unmarshal([]byte(text), &event) != nil {
			continue
		}
		switch event.Type {
		case "thread.started":
			session = event.ThreadID
		case "turn.started":
			turns++
		case "item.completed":
			if event.Item != nil && event.Item.Type == "agent_message" && strings.TrimSpace(event.Item.Text) != "" {
				messages = append(messages, strings.TrimSpace(event.Item.Text))
			}
			if event.Item != nil && event.Item.Type == "error" && strings.TrimSpace(event.Item.Message) != "" {
				failures = append(failures, strings.TrimSpace(event.Item.Message))
			}
		case "turn.completed":
			completed = true
			if event.Usage != nil {
				cost = event.Usage.CostUSD
			}
		case "turn.failed":
			failed = true
			if event.Error != nil && strings.TrimSpace(event.Error.Message) != "" {
				failures = append(failures, strings.TrimSpace(event.Error.Message))
			}
		case "error":
			failed = true
			if strings.TrimSpace(event.Message) != "" {
				failures = append(failures, strings.TrimSpace(event.Message))
			}
		}
	}
	if runErr != nil && !completed {
		failures = append(failures, runErr.Error())
	}
	if failed || !completed {
		if len(failures) == 0 {
			failures = append(failures, "codex plan review ended without successful terminal event")
		}
		return "", session, cost, intPtr(turns), errors.New(strings.Join(failures, "; "))
	}
	if len(messages) == 0 {
		return "", session, cost, intPtr(turns), errors.New("codex plan reviewer returned no agent_message")
	}
	return messages[len(messages)-1], session, cost, intPtr(turns), nil
}

func invokePlanReviewModel(req planReviewRequest) (string, string, *float64, *int, error) {
	exePath, err := resolveRunnerTool(req.Reviewer.Kind)
	if err != nil {
		return "", "", nil, nil, err
	}
	switch req.Reviewer.Kind {
	case "claude":
		return invokeSemanticClaude(req.Scope, exePath, req.Prompt, req.Reviewer, "plan")
	case "codex":
		return invokePlanReviewCodex(req, exePath)
	default:
		return "", "", nil, nil, fmt.Errorf("unsupported plan reviewer %q", req.Reviewer.Kind)
	}
}

func runPlanReview(root, planPath string, steps []workStep, scope sessionScope, executor runnerSpec) planReviewRun {
	rawBefore, err := os.ReadFile(planPath)
	planSHA := ""
	if err == nil {
		planSHA = sha256Hex(rawBefore)
	}
	compositionSHA := planCompositionSHA(steps)
	reviewer, reviewerErr := oppositeSemanticReviewer(executor)
	rec := planReviewRecord{
		Schema: planReviewSchemaVersion, At: time.Now().UTC().Format(time.RFC3339Nano), Subject: "plan",
		Plan: planPath, PlanSHA256: planSHA, CompositionSHA256: compositionSHA,
		ExecutorVendor: executor.Kind, ReviewerVendor: reviewer.Kind, ReviewerModel: reviewer.Model,
		ReviewerRole: "semantic-plan-reviewer", ReviewerSandbox: "read-only",
	}
	fail := func(e error) planReviewRun {
		rec.Error = e.Error()
		publishPlanReview(root, rec)
		return planReviewRun{Record: rec, State: "NOT_PROVEN"}
	}
	if err != nil {
		return fail(err)
	}
	if reviewerErr != nil {
		return fail(reviewerErr)
	}
	if reviewer.Kind == executor.Kind || reviewer.Kind == "" {
		return fail(fmt.Errorf("plan reviewer must be opposite vendor: executor=%s reviewer=%s", executor.Kind, reviewer.Kind))
	}
	req := planReviewRequest{
		Scope: scope, PlanPath: planPath, PlanSHA256: planSHA, CompositionSHA256: compositionSHA,
		Reviewer: reviewer, Prompt: planReviewPrompt(planPath, planSHA, compositionSHA),
	}
	output, session, cost, turns, invokeErr := planReviewInvoke(req)
	rec.Session, rec.CostUSD, rec.Turns = session, cost, turns
	rawAfter, afterErr := os.ReadFile(planPath)
	if afterErr != nil {
		return fail(fmt.Errorf("PLAN unreadable after review: %w", afterErr))
	}
	if !bytes.Equal(rawBefore, rawAfter) {
		return fail(errors.New("PLAN changed during read-only semantic review"))
	}
	if invokeErr != nil {
		return fail(invokeErr)
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
	compositionSHA := planCompositionSHA(steps)
	if rec, state, ok := readCurrentPlanReview(c.Root, compositionSHA); ok {
		line(fmt.Sprintf("plan review: cached %s · composition %s · reviewer %s",
			state, compositionSHA[:12], rec.ReviewerVendor))
		return state, planReviewExit(state)
	}
	if c.WhatIf {
		line("plan review: WHATIF — composition changed/unreviewed; model call skipped")
		return "NOT_PROVEN", 0
	}
	run := runPlanReview(c.Root, planPath, steps, c.scope(), executor)
	line(fmt.Sprintf("plan review: %s · composition %s · reviewer %s",
		run.State, compositionSHA[:12], run.Record.ReviewerVendor))
	return run.State, planReviewExit(run.State)
}

func cmdPlanReview(argv []string) int {
	fs := flag.NewFlagSet("plan-review", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	configPath := fs.String("config", "", "путь к run-config.json")
	executorName := fs.String("executor", "auto", "кто будет исполнять model-step: auto|claude|codex|chatgpt|router")
	force := fs.Bool("force", false, "повторить review текущего composition даже при сохранённом verdict")
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
	var cfg runConfig
	if err := readJSON(cfgPath, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, "plan-review:", err)
		return 2
	}
	planPath := nativePlanPath(root, cfg)
	steps := readPlanSteps(planPath)
	compositionSHA := planCompositionSHA(steps)
	if !*force {
		if rec, state, ok := readCurrentPlanReview(root, compositionSHA); ok {
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
	run := runPlanReview(root, planPath, steps, legacyScope(root), executor)
	if *asJSON {
		b, _ := json.MarshalIndent(map[string]any{"state": run.State, "cached": false, "record": run.Record}, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("plan-review: %s · composition %s · reviewer %s%s",
			run.State, compositionSHA[:12], run.Record.ReviewerVendor, lineEnding)
	}
	return planReviewExit(run.State)
}
