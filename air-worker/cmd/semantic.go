package main

import (
	"bytes"
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
	Code int    `json:"code"`
	Text string `json:"text"`
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
	At             string           `json:"at"`
	Subject        string           `json:"subject"`
	Step           string           `json:"step"`
	ExecutorVendor string           `json:"executor_vendor"`
	ReviewerVendor string           `json:"reviewer_vendor"`
	ReviewerModel  string           `json:"reviewer_model,omitempty"`
	Session        string           `json:"session_id,omitempty"`
	FactualCode    int              `json:"factual_code"`
	Verdict        *semanticVerdict `json:"verdict,omitempty"`
	Error          string           `json:"error,omitempty"`
	CostUSD        *float64         `json:"cost_usd,omitempty"`
	Turns          *int             `json:"turns,omitempty"`
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
	if factualCode != 0 {
		return "FAIL"
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
	case "claude":
		return runnerSpec{Kind: "codex", Effort: "high"}, nil
	case "codex":
		return runnerSpec{Kind: "claude", Model: "sonnet", Effort: "medium"}, nil
	default:
		return runnerSpec{}, fmt.Errorf("semantic judge requires model executor; got %q", executor.Kind)
	}
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

func semanticClaudeArgs(prompt string, reviewer runnerSpec) []string {
	args := []string{"-p", prompt, "--output-format", "json", "--max-turns", "20",
		"--allowed-tools", "Read,Glob,Grep"}
	if reviewer.Model != "" {
		args = append(args, "--model", reviewer.Model)
	}
	if reviewer.Effort != "" {
		args = append(args, "--effort", reviewer.Effort)
	}
	return args
}
func semanticCodexCommand(exePath, root, prompt string, reviewer runnerSpec, schemaPath string) *exec.Cmd {
	cmd := semanticCommand(exePath, semanticCodexArgs(root, reviewer, schemaPath)...)
	cmd.Dir = root
	cmd.Env = codexEnv(nil)
	cmd.Stdin = strings.NewReader(prompt)
	return cmd
}

func invokeSemanticCodex(root, exePath, prompt string, reviewer runnerSpec) (string, string, *float64, *int, error) {
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
	out, runErr := cmd.CombinedOutput()

	completed, failed := false, false
	session, turns := "", 0
	var cost *float64
	var messages, failures []string
	for _, line := range strings.Split(strings.ReplaceAll(decodeOutput(out), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "{") {
			continue
		}
		var e codexEvent
		if json.Unmarshal([]byte(t), &e) != nil {
			continue
		}
		switch e.Type {
		case "thread.started":
			session = e.ThreadID
		case "turn.started":
			turns++
		case "item.completed":
			if e.Item != nil && e.Item.Type == "agent_message" && strings.TrimSpace(e.Item.Text) != "" {
				messages = append(messages, strings.TrimSpace(e.Item.Text))
			}
			if e.Item != nil && e.Item.Type == "error" && strings.TrimSpace(e.Item.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Item.Message))
			}
		case "turn.completed":
			completed = true
			if e.Usage != nil {
				cost = e.Usage.CostUSD
			}
		case "turn.failed":
			failed = true
			if e.Error != nil && strings.TrimSpace(e.Error.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Error.Message))
			}
		case "error":
			failed = true
			if strings.TrimSpace(e.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Message))
			}
		}
	}
	if runErr != nil && !completed {
		failures = append(failures, runErr.Error())
	}
	if failed || !completed {
		if len(failures) == 0 {
			failures = append(failures, "codex semantic stream ended without successful terminal event")
		}
		return "", session, cost, intPtr(turns), errors.New(strings.Join(failures, "; "))
	}
	if len(messages) == 0 {
		return "", session, cost, intPtr(turns), errors.New("codex semantic judge returned no agent_message")
	}
	return messages[len(messages)-1], session, cost, intPtr(turns), nil
}
func invokeSemanticClaude(root, exePath, prompt string, reviewer runnerSpec) (string, string, *float64, *int, error) {
	cmd := semanticCommand(exePath, semanticClaudeArgs(prompt, reviewer)...)
	cmd.Dir = root
	if env, took := runnerEnv(); took {
		cmd.Env = env
	}
	out, runErr := cmd.CombinedOutput()
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

func buildSemanticPacket(c *loopCtx, step workStep, factualCode int, factualText, executorClaim string) semanticPacket {
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
		Factual:       semanticFactualPacket{Code: factualCode, Text: factualText},
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

func (c *loopCtx) semanticJudge(step workStep, executor runnerSpec, factualCode int, factualText, executorClaim string) semanticRun {
	reviewer, err := oppositeSemanticReviewer(executor)
	run := semanticRun{Reviewer: reviewer}
	rec := semanticRecord{
		At:             time.Now().UTC().Format(time.RFC3339Nano),
		Subject:        "step",
		Step:           step.Num,
		ExecutorVendor: executor.Kind,
		ReviewerVendor: reviewer.Kind,
		ReviewerModel:  reviewer.Model,
		FactualCode:    factualCode,
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
	packet := buildSemanticPacket(c, step, factualCode, factualText, executorClaim)
	prompt := semanticPrompt(packet)
	var raw string
	switch reviewer.Kind {
	case "codex":
		raw, run.Session, run.Cost, run.Turns, err = invokeSemanticCodex(c.Root, exePath, prompt, reviewer)
	case "claude":
		raw, run.Session, run.Cost, run.Turns, err = invokeSemanticClaude(c.Root, exePath, prompt, reviewer)
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
