package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const semanticMaxOutputBytes = 4096

const semanticSchemaJSON = `{
  "type":"object",
  "additionalProperties":false,
  "required":["verdict","step_done","drift","evidence","next"],
  "properties":{
    "verdict":{"type":"string","enum":["PASS","DRIFT","NOT_PROVEN"]},
    "step_done":{"type":"string","enum":["yes","no","partial"]},
    "drift":{"type":"string","enum":["none","minor","major"]},
    "evidence":{"type":"array","maxItems":3,"items":{"type":"string","minLength":1}},
    "next":{"type":"string","minLength":1}
  }
}`

type semanticVerdict struct {
	Verdict  string   `json:"verdict"`
	StepDone string   `json:"step_done"`
	Drift    string   `json:"drift"`
	Evidence []string `json:"evidence"`
	Next     string   `json:"next"`
}

type semanticRun struct {
	Verdict  semanticVerdict
	Reviewer runnerSpec
	Session  string
	Cost     *float64
	Turns    *int
	Error    string
}
type semanticStepPacket struct {
	Num      string   `json:"num"`
	Title    string   `json:"title"`
	Tier     string   `json:"tier"`
	Criteria []string `json:"criteria"`
}

type semanticFactualPacket struct {
	Scope   string   `json:"scope"`
	Code    int      `json:"code"`
	Text    string   `json:"text"`
	Passed  []string `json:"criteria_passed,omitempty"`
	Failed  []string `json:"criteria_failed,omitempty"`
	Unknown []string `json:"criteria_unknown,omitempty"`
}

type semanticPacket struct {
	Subject       string                `json:"subject"`
	Goals         []planGoal            `json:"goals"`
	Criteria      []planCriterion       `json:"criteria"`
	Step          semanticStepPacket    `json:"step"`
	Factual       semanticFactualPacket `json:"factual_judge"`
	GitStatus     string                `json:"git_status"`
	Diff          string                `json:"diff"`
	ExecutorClaim string                `json:"executor_claim"`
}

type semanticRecord struct {
	At               string           `json:"at"`
	Subject          string           `json:"subject"`
	Step             string           `json:"step"`
	ExecutorVendor   string           `json:"executor_vendor"`
	ReviewerVendor   string           `json:"reviewer_vendor"`
	ReviewerModel    string           `json:"reviewer_model,omitempty"`
	ReviewerEffort   string           `json:"reviewer_effort,omitempty"`
	ReviewerOverride bool             `json:"reviewer_override,omitempty"`
	ReleaseVersion   string           `json:"release_version,omitempty"`
	ReviewerRole     string           `json:"reviewer_role,omitempty"`
	ReviewerSandbox  string           `json:"reviewer_sandbox,omitempty"`
	Session          string           `json:"session_id,omitempty"`
	FactualCode      int              `json:"factual_code"`
	Verdict          *semanticVerdict `json:"verdict,omitempty"`
	Error            string           `json:"error,omitempty"`
	CostUSD          *float64         `json:"cost_usd,omitempty"`
	Turns            *int             `json:"turns,omitempty"`
}

var semanticCommand = exec.Command

func parseSemanticVerdict(raw string) (semanticVerdict, error) {
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "\ufeff"))
	if raw == "" {
		return semanticVerdict{}, errors.New("semantic judge returned empty output")
	}
	if len([]byte(raw)) > semanticMaxOutputBytes {
		return semanticVerdict{}, fmt.Errorf("semantic judge output is %d bytes; limit is %d", len([]byte(raw)), semanticMaxOutputBytes)
	}
	dec := json.NewDecoder(bytes.NewBufferString(raw))
	dec.DisallowUnknownFields()
	var v semanticVerdict
	if err := dec.Decode(&v); err != nil {
		return semanticVerdict{}, fmt.Errorf("semantic judge returned invalid JSON contract: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			return semanticVerdict{}, errors.New("semantic judge returned more than one JSON value")
		}
		return semanticVerdict{}, fmt.Errorf("semantic judge returned trailing content: %w", err)
	}
	switch v.Verdict {
	case "PASS", "DRIFT", "NOT_PROVEN":
	default:
		return semanticVerdict{}, fmt.Errorf("invalid semantic verdict %q", v.Verdict)
	}
	switch v.StepDone {
	case "yes", "no", "partial":
	default:
		return semanticVerdict{}, fmt.Errorf("invalid step_done %q", v.StepDone)
	}
	switch v.Drift {
	case "none", "minor", "major":
	default:
		return semanticVerdict{}, fmt.Errorf("invalid drift %q", v.Drift)
	}
	if len(v.Evidence) > 3 {
		return semanticVerdict{}, fmt.Errorf("semantic judge returned %d evidence items; limit is 3", len(v.Evidence))
	}
	for i, e := range v.Evidence {
		if strings.TrimSpace(e) == "" {
			return semanticVerdict{}, fmt.Errorf("evidence[%d] is empty", i)
		}
	}
	if strings.TrimSpace(v.Next) == "" {
		return semanticVerdict{}, errors.New("semantic judge returned empty next")
	}
	if v.Verdict == "PASS" && (v.StepDone != "yes" || v.Drift != "none") {
		return semanticVerdict{}, errors.New("PASS requires step_done=yes and drift=none")
	}
	if v.Verdict == "DRIFT" && v.Drift == "none" {
		return semanticVerdict{}, errors.New("DRIFT requires drift=minor or major")
	}
	return v, nil
}

func combinedAcceptance(factualCode int, semantic semanticVerdict) string {
	if factualCode == 1 {
		return "FAIL"
	}
	if factualCode != 0 {
		return "NOT_PROVEN"
	}
	switch semantic.Verdict {
	case "PASS":
		return "PASS"
	case "DRIFT":
		return "DRIFT"
	default:
		return "NOT_PROVEN"
	}
}
func oppositeSemanticReviewer(executor runnerSpec) (runnerSpec, error) {
	switch executor.Kind {
	case "claude", "router":
		// Legacy non-OpenAI executors keep the independent Codex reviewer path.
		return runnerSpec{Kind: "codex", Effort: "high"}, nil
	case "codex", "openai":
		if strings.EqualFold(strings.TrimSpace(executor.Model), "gpt-6-astra") {
			return runnerSpec{Kind: "claude", Model: "claude-opus-5-5", Effort: "low"}, nil
		}
		return runnerSpec{Kind: "claude", Model: "claude-sonnet-5-5", Effort: "low"}, nil
	case "chatgpt":
		return runnerSpec{Kind: "claude", Model: "claude-sonnet-5-5", Effort: "low"}, nil
	default:
		return runnerSpec{}, fmt.Errorf("semantic judge requires model executor; got %q", executor.Kind)
	}
}

// semanticJudgePolicy is a release-scoped override. A configured route is active
// only when the product's goal/goal.json names the same release version; otherwise
// the default cross-vendor reviewer remains in force.
type semanticJudgePolicy struct {
	ReleaseVersion string `json:"release_version"`
	Kind           string `json:"kind"`
	Model          string `json:"model"`
	Effort         string `json:"effort"`
}

func (p semanticJudgePolicy) Validate() error {
	release := strings.TrimSpace(p.ReleaseVersion)
	kind := strings.ToLower(strings.TrimSpace(p.Kind))
	model := strings.TrimSpace(p.Model)
	effort := strings.ToLower(strings.TrimSpace(p.Effort))
	if release == "" && kind == "" && model == "" && effort == "" {
		return nil
	}
	parts := strings.Split(release, ".")
	if len(parts) != 3 {
		return fmt.Errorf("semantic_judge.release_version must be SemVer X.Y.Z")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("semantic_judge.release_version must be SemVer X.Y.Z")
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return fmt.Errorf("semantic_judge.release_version must be SemVer X.Y.Z")
			}
		}
	}
	if kind != "codex" && kind != "claude" {
		return fmt.Errorf("semantic_judge.kind must be codex or claude")
	}
	if model == "" {
		return fmt.Errorf("semantic_judge.model is required")
	}
	if !allowedModelEffort(effort) {
		return fmt.Errorf("semantic_judge.effort must be low, medium, or high")
	}
	return nil
}

func (p semanticJudgePolicy) Resolve(executor runnerSpec, activeReleaseVersion string) (runnerSpec, bool, error) {
	if err := p.Validate(); err != nil {
		return runnerSpec{}, true, err
	}
	if strings.TrimSpace(p.Kind) == "" || !strings.EqualFold(strings.TrimSpace(p.ReleaseVersion), strings.TrimSpace(activeReleaseVersion)) {
		reviewer, err := oppositeSemanticReviewer(executor)
		return reviewer, false, err
	}
	return runnerSpec{
		Kind:   strings.ToLower(strings.TrimSpace(p.Kind)),
		Model:  strings.TrimSpace(p.Model),
		Effort: strings.ToLower(strings.TrimSpace(p.Effort)),
	}, true, nil
}

func activeProductReleaseVersion(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "goal", "goal.json"))
	if err != nil {
		return ""
	}
	var goal struct {
		ReleaseVersion string `json:"release_version"`
	}
	if json.Unmarshal(raw, &goal) != nil {
		return ""
	}
	return strings.TrimSpace(goal.ReleaseVersion)
}

func semanticCodexArgs(root string, reviewer runnerSpec, schemaPath string) []string {
	args := []string{"exec", "--json", "--skip-git-repo-check", "-s", "read-only", "-C", root,
		"--output-schema", schemaPath}
	if reviewer.Model != "" {
		args = append(args, "-m", reviewer.Model)
	}
	if reviewer.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+reviewer.Effort)
	}
	return append(args, "-")
}

func semanticClaudeArgs(reviewer runnerSpec) []string {
	args := []string{"-p", "--output-format", "json", "--max-turns", "20",
		"--allowed-tools", "Read,Glob,Grep"}
	if reviewer.Model != "" {
		args = append(args, "--model", reviewer.Model)
	}
	if reviewer.Effort != "" {
		args = append(args, "--effort", reviewer.Effort)
	}
	return args
}
func semanticClaudeCommand(exePath, root, prompt string, reviewer runnerSpec) *exec.Cmd {
	cmd := semanticCommand(exePath, semanticClaudeArgs(reviewer)...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(prompt)
	return cmd
}

func semanticCodexCommand(exePath, root, prompt string, reviewer runnerSpec, schemaPath string) *exec.Cmd {
	cmd := semanticCommand(exePath, semanticCodexArgs(root, reviewer, schemaPath)...)
	cmd.Dir = root
	cmd.Env = codexEnv(nil)
	cmd.Stdin = strings.NewReader(ponytailPrompt(prompt))
	return cmd
}

func supportedCodexItemType(itemType string) bool {
	switch itemType {
	case "agent_message", "reasoning", "command_execution", "file_change", "mcp_tool_call", "collab_tool_call", "web_search", "todo_list", "error":
		return true
	default:
		return false
	}
}

func validateUniqueJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err != nil {
			return err
		}
		return errors.New("multiple JSON values")
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := make([]string, 0)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			for _, existing := range seen {
				if strings.EqualFold(existing, key) {
					return fmt.Errorf("duplicate JSON object key %q (case-insensitive collision with %q)", key, existing)
				}
			}
			seen = append(seen, key)
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("malformed JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("malformed JSON array")
		}
	default:
		return errors.New("unexpected JSON delimiter")
	}
	return nil
}

func parseCodexReviewerStream(out []byte, runErr error, subject string) (string, string, *float64, *int, error) {
	completed, failed := false, false
	threadStarted, turnOpen := false, false
	session, turns := "", 0
	var cost *float64
	var currentMessages, failures []string
	for _, line := range strings.Split(strings.ReplaceAll(decodeOutput(out), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if err := validateUniqueJSONKeys([]byte(t)); err != nil {
			failed = true
			failures = append(failures, fmt.Sprintf("%s stream contained invalid JSON event: %v", subject, err))
			continue
		}
		var e codexEvent
		if err := json.Unmarshal([]byte(t), &e); err != nil {
			failed = true
			failures = append(failures, subject+" stream contained invalid JSON event")
			continue
		}
		switch e.Type {
		case "thread.started":
			if threadStarted || turns > 0 || turnOpen {
				failed = true
				failures = append(failures, subject+" stream contained thread.started out of order")
			}
			threadStarted = true
			session = e.ThreadID
			if strings.TrimSpace(session) == "" {
				failed = true
				failures = append(failures, subject+" stream contained thread.started missing thread_id")
			}
		case "turn.started":
			if !threadStarted {
				failed = true
				failures = append(failures, subject+" stream started turn without thread.started")
			}
			if turnOpen {
				failed = true
				failures = append(failures, subject+" stream started a new turn before the prior turn terminated")
			}
			completed = false
			currentMessages = nil
			cost = nil
			turns++
			turnOpen = true
		case "item.started", "item.updated":
			if !turnOpen {
				failed = true
				failures = append(failures, subject+" stream contained item without an active turn")
			}
			if e.Item == nil || strings.TrimSpace(e.Item.Type) == "" {
				failed = true
				failures = append(failures, subject+" stream contained item event without a valid item")
			} else if !supportedCodexItemType(e.Item.Type) {
				failed = true
				failures = append(failures, fmt.Sprintf("%s stream contained unsupported item type %q", subject, e.Item.Type))
			} else if e.Item.Type == "error" {
				failed = true
				message := strings.TrimSpace(e.Item.Message)
				if message == "" {
					message = subject + " returned error item"
				}
				failures = append(failures, message)
			}
		case "item.completed":
			if completed {
				failed = true
				failures = append(failures, subject+" stream contained item after terminal event")
				continue
			}
			if !turnOpen {
				failed = true
				failures = append(failures, subject+" stream contained item without an active turn")
				continue
			}
			if e.Item == nil || strings.TrimSpace(e.Item.Type) == "" {
				failed = true
				failures = append(failures, subject+" stream contained item.completed without a valid item")
				continue
			}
			if !supportedCodexItemType(e.Item.Type) {
				failed = true
				failures = append(failures, fmt.Sprintf("%s stream contained unsupported item type %q", subject, e.Item.Type))
				continue
			}
			if e.Item.Type == "agent_message" {
				message := strings.TrimSpace(e.Item.Text)
				if message == "" {
					failed = true
					failures = append(failures, subject+" stream contained empty agent_message")
					continue
				}
				currentMessages = append(currentMessages, message)
			}
			if e.Item.Type == "error" {
				failed = true
				message := strings.TrimSpace(e.Item.Message)
				if message == "" {
					message = subject + " returned error item"
				}
				failures = append(failures, message)
			}
		case "turn.completed":
			if !turnOpen {
				failed = true
				failures = append(failures, subject+" stream contained turn.completed without an active turn")
				continue
			}
			completed = true
			turnOpen = false
			if e.Usage != nil {
				cost = e.Usage.CostUSD
			}
		case "turn.failed":
			failed = true
			turnOpen = false
			if e.Error != nil && strings.TrimSpace(e.Error.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Error.Message))
			}
		case "error":
			failed = true
			if strings.TrimSpace(e.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Message))
			}
		default:
			failed = true
			failures = append(failures, fmt.Sprintf("%s stream contained unsupported event type %q", subject, e.Type))
		}
	}
	if runErr != nil {
		failures = append(failures, fmt.Sprintf("%s process: %v", subject, runErr))
	}
	if runErr != nil || failed || !completed || len(failures) > 0 {
		if len(failures) == 0 {
			failures = append(failures, subject+" stream ended without successful terminal event")
		}
		return "", session, cost, intPtr(turns), errors.New(strings.Join(failures, "; "))
	}
	if len(currentMessages) == 0 {
		return "", session, cost, intPtr(turns), errors.New(subject + " returned no agent_message")
	}
	return currentMessages[len(currentMessages)-1], session, cost, intPtr(turns), nil
}

func invokeSemanticCodex(scope sessionScope, exePath, prompt string, reviewer runnerSpec, step string) (string, string, *float64, *int, error) {
	root := scope.Root
	if err := requirePonytailSkill(); err != nil {
		return "", "", nil, nil, fmt.Errorf("Ponytail: %w", err)
	}
	schema, err := os.CreateTemp("", "air-worker-semantic-schema-*.json")
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("create semantic output schema: %w", err)
	}
	schemaPath := schema.Name()
	defer os.Remove(schemaPath)
	if _, err := schema.WriteString(semanticSchemaJSON); err != nil {
		_ = schema.Close()
		return "", "", nil, nil, fmt.Errorf("write semantic output schema: %w", err)
	}
	if err := schema.Close(); err != nil {
		return "", "", nil, nil, fmt.Errorf("close semantic output schema: %w", err)
	}
	cmd := semanticCodexCommand(exePath, root, prompt, reviewer, schemaPath)
	out, runErr := runReceiptedWithMeta(context.Background(), scope, step, "semantic-reviewer", cmd, jobReceiptMeta{
		Runner: reviewer.Kind, Provider: reviewer.Kind, Model: reviewer.Model, Effort: reviewer.Effort,
		Role: "semantic-reviewer", Sandbox: "read-only",
	})
	return parseCodexReviewerStream(out, runErr, "codex semantic judge")
}
func invokeSemanticClaude(scope sessionScope, exePath, prompt string, reviewer runnerSpec, step string) (string, string, *float64, *int, error) {
	root := scope.Root
	cmd := semanticClaudeCommand(exePath, root, prompt, reviewer)
	_ = applyClaudeRunnerEnv(cmd)
	out, runErr := runReceiptedWithMeta(context.Background(), scope, step, "semantic-reviewer", cmd, jobReceiptMeta{
		Runner: reviewer.Kind, Provider: reviewer.Kind, Model: reviewer.Model, Effort: reviewer.Effort,
		Role: "semantic-reviewer", Sandbox: "read-only",
	})
	raw := decodeOutput(out)
	var res *claudeResult
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "{") {
			continue
		}
		var item claudeResult
		if json.Unmarshal([]byte(t), &item) != nil {
			continue
		}
		if item.Type == "result" || item.TotalCostUSD != nil {
			cp := item
			res = &cp
		}
	}
	if res == nil {
		if runErr != nil {
			return "", "", nil, nil, fmt.Errorf("claude semantic process: %w", runErr)
		}
		return "", "", nil, nil, errors.New("claude semantic judge returned no result object")
	}
	if res.IsError {
		return "", res.SessionID, res.TotalCostUSD, res.NumTurns,
			fmt.Errorf("claude semantic judge failed: %s", strings.TrimSpace(res.Result))
	}
	return strings.TrimSpace(res.Result), res.SessionID, res.TotalCostUSD, res.NumTurns, nil
}

func semanticWorkspace(root string) (string, string) {
	statusCmd := exec.Command("git", "status", "--short", "--", ".")
	statusCmd.Dir = root
	statusOut, _ := statusCmd.Output()

	diffCmd := exec.Command("git", "diff", "--no-ext-diff", "--", ".")
	diffCmd.Dir = root
	diffOut, _ := diffCmd.Output()

	status := strings.TrimSpace(decodeOutput(statusOut))
	diff := strings.TrimSpace(decodeOutput(diffOut))
	const maxDiff = 64 * 1024
	if len(diff) > maxDiff {
		diff = diff[:maxDiff] + "\n[DIFF TRUNCATED BY AIR-WORKER]"
	}
	return status, diff
}

func buildSemanticPacket(c *loopCtx, step workStep, factual semanticFactualPacket, executorClaim string) semanticPacket {
	criteriaIDs := stepCriteria(step)
	var criteria []planCriterion
	for _, id := range criteriaIDs {
		if cr, ok := c.goals.criterion(id); ok {
			criteria = append(criteria, cr)
		}
	}
	status, diff := semanticWorkspace(c.Root)
	return semanticPacket{
		Subject:  "step",
		Goals:    c.goals.Goals,
		Criteria: criteria,
		Step: semanticStepPacket{
			Num: step.Num, Title: step.Title, Tier: step.Tier, Criteria: criteriaIDs,
		},
		Factual:       factual,
		GitStatus:     status,
		Diff:          diff,
		ExecutorClaim: strings.TrimSpace(executorClaim),
	}
}

func semanticPrompt(packet semanticPacket) string {
	body, _ := json.MarshalIndent(packet, "", "  ")
	return `You are Air Worker Semantic Judge. Execute this review NOW; do not acknowledge the assignment and do not ask for a future submission.
The PACKET at the end of this prompt IS the complete current submission: it contains the goal, criterion, step, factual-judge result, workspace status/diff, and executor claim.
Independently decide whether this current step is actually done and whether the observed work moves toward the stated goal.
The factual judge is authoritative for machine facts: never override a factual FAIL with PASS.
You are read-only. Do not edit files. Inspect the workspace only when needed to verify the PACKET.
If PACKET fields are populated, never claim that no goal/result/evidence was submitted; judge the evidence that is present.

Your entire final response MUST be EXACTLY one JSON object, no acknowledgment, markdown, or text before/after it:
{"verdict":"PASS|DRIFT|NOT_PROVEN","step_done":"yes|no|partial","drift":"none|minor|major","evidence":["up to 3 short factual items"],"next":"one short next action"}

PASS requires the step done with supporting evidence and drift=none.
DRIFT means the work is materially off-goal. NOT_PROVEN means evidence is insufficient or contradictory.
Do not invent facts. Output must stay under 500 tokens.

PACKET TO REVIEW NOW:
` + string(body) + `

Perform the review now and return only the schema-conforming JSON verdict.`
}
func publishSemantic(root string, rec semanticRecord) {
	dir := filepath.Join(root, ".woody")
	_ = os.MkdirAll(dir, 0o755)
	latest := filepath.Join(dir, "semantic-verdict.json")
	if b, err := json.MarshalIndent(rec, "", "  "); err == nil {
		_ = os.WriteFile(latest, append(b, '\n'), 0o644)
	}
	history := filepath.Join(dir, "semantic-history.jsonl")
	if f, err := os.OpenFile(history, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		_ = json.NewEncoder(f).Encode(rec)
		_ = f.Close()
	}
}

func (c *loopCtx) semanticJudge(step workStep, executor runnerSpec, factual semanticFactualPacket, executorClaim string) semanticRun {
	reviewer, explicitOverride, err := c.Cfg.SemanticJudge.Resolve(executor, activeProductReleaseVersion(c.Root))
	run := semanticRun{Reviewer: reviewer}
	rec := semanticRecord{
		At:               time.Now().UTC().Format(time.RFC3339Nano),
		Subject:          "step",
		Step:             step.Num,
		ExecutorVendor:   executor.Kind,
		ReviewerVendor:   reviewer.Kind,
		ReviewerModel:    reviewer.Model,
		ReviewerEffort:   reviewer.Effort,
		ReviewerOverride: explicitOverride,
		ReleaseVersion:   activeProductReleaseVersion(c.Root),
		ReviewerRole:     "semantic-reviewer",
		FactualCode:      factual.Code,
	}
	if err != nil {
		run.Error = err.Error()
		rec.Error = run.Error
		publishSemantic(c.Root, rec)
		return run
	}
	exePath, err := resolveRunnerTool(reviewer.Kind)
	if err != nil {
		run.Error = err.Error()
		rec.Error = run.Error
		publishSemantic(c.Root, rec)
		return run
	}
	rec.ReviewerSandbox = "read-only"
	line(fmt.Sprintf("  semantic reviewer: %s · model %s · role semantic-reviewer · sandbox %s",
		reviewer.Kind, reviewer.Model, rec.ReviewerSandbox))
	packet := buildSemanticPacket(c, step, factual, executorClaim)
	prompt := semanticPrompt(packet)
	var raw string
	switch reviewer.Kind {
	case "codex":
		raw, run.Session, run.Cost, run.Turns, err = invokeSemanticCodex(c.scope(), exePath, prompt, reviewer, step.Num)
	case "claude":
		raw, run.Session, run.Cost, run.Turns, err = invokeSemanticClaude(c.scope(), exePath, prompt, reviewer, step.Num)
	default:
		err = fmt.Errorf("unsupported semantic reviewer %q", reviewer.Kind)
	}
	rec.Session, rec.CostUSD, rec.Turns = run.Session, run.Cost, run.Turns
	if err != nil {
		run.Error = err.Error()
		rec.Error = run.Error
		publishSemantic(c.Root, rec)
		return run
	}
	v, err := parseSemanticVerdict(raw)
	if err != nil {
		run.Error = err.Error()
		rec.Error = run.Error
		publishSemantic(c.Root, rec)
		return run
	}
	run.Verdict = v
	rec.Verdict = &v
	publishSemantic(c.Root, rec)
	return run
}
func semanticEvidenceText(v semanticVerdict) string {
	var parts []string
	if len(v.Evidence) > 0 {
		parts = append(parts, "evidence: "+strings.Join(v.Evidence, "; "))
	}
	if strings.TrimSpace(v.Next) != "" {
		parts = append(parts, "next: "+strings.TrimSpace(v.Next))
	}
	return strings.Join(parts, " · ")
}
