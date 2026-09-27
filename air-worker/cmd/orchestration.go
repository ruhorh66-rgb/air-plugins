package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

type agentLifecycle struct {
	Requested     int
	Started       int
	Completed     int
	Aborted       int
	OrphanResults int
	Live          int
	Background    int
	IDs           []string
	Problems      []string
}

type claudeStreamEvent struct {
	Type            string          `json:"type"`
	ParentToolUseID string          `json:"parent_tool_use_id"`
	SessionID       string          `json:"session_id"`
	IsError         bool            `json:"is_error"`
	TotalCostUSD    *float64        `json:"total_cost_usd"`
	NumTurns        *int            `json:"num_turns"`
	Subtype         string          `json:"subtype"`
	DurationAPI     *int            `json:"duration_api_ms"`
	Result          string          `json:"result"`
	Message         json.RawMessage `json:"message"`
}

type claudeStreamMessage struct {
	Content []json.RawMessage `json:"content"`
}

type claudeStreamBlock struct {
	Type      string          `json:"type"`
	Name      string          `json:"name"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Input     json.RawMessage `json:"input"`
}

func ensureCSVItem(csv, item string) string {
	seen := map[string]bool{}
	out := make([]string, 0)
	for _, raw := range strings.Split(csv, ",") {
		v := strings.TrimSpace(raw)
		if v == "" || seen[strings.ToLower(v)] {
			continue
		}
		seen[strings.ToLower(v)] = true
		out = append(out, v)
	}
	if !seen[strings.ToLower(item)] {
		out = append(out, item)
	}
	return strings.Join(out, ",")
}

func orchestrationInstructions(n int) []string {
	if n < 1 {
		n = 1
	}
	return []string{
		fmt.Sprintf("ORCHESTRATION CONTRACT: launch exactly %d native Agent tool calls before editing.", n),
		"Launch independent Agent calls together where possible. Every Agent call MUST set run_in_background=false so its result is observable in this turn.",
		"Use read-only Explore or Plan agents for analysis, impact search and test strategy; the leader is the only writer in the current product tree.",
		"After all Agent results return, reconcile them, implement the step, run only the checks available to this executor, and report the integrated result.",
		"If Agent is unavailable, blocked, or fewer agents return than requested, say so and do not claim orchestration succeeded.",
	}
}

func parseClaudeStream(raw string, requested int) (*claudeResult, agentLifecycle) {
	stats := agentLifecycle{Requested: requested}
	allAgent := map[string]bool{}
	countedStart := map[string]bool{}
	syncAgent := map[string]bool{}
	completed := map[string]bool{}
	var result *claudeResult

	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var ev claudeStreamEvent
		if json.Unmarshal([]byte(line), &ev) != nil {
			continue
		}
		if ev.Type == "result" {
			result = &claudeResult{IsError: ev.IsError, TotalCostUSD: ev.TotalCostUSD, NumTurns: ev.NumTurns, SessionID: ev.SessionID, Subtype: ev.Subtype, DurationAPI: ev.DurationAPI, Type: ev.Type, Result: ev.Result}
		}
		if len(ev.Message) == 0 {
			continue
		}
		var msg claudeStreamMessage
		if json.Unmarshal(ev.Message, &msg) != nil {
			continue
		}
		for _, rawBlock := range msg.Content {
			var block claudeStreamBlock
			if json.Unmarshal(rawBlock, &block) != nil {
				continue
			}
			if block.Type == "tool_use" && block.Name == "Agent" {
				allAgent[block.ID] = true
				if ev.ParentToolUseID != "" {
					continue
				}
				countedStart[block.ID] = true
				stats.Started++
				stats.IDs = append(stats.IDs, block.ID)
				var input struct {
					RunInBackground *bool `json:"run_in_background"`
				}
				_ = json.Unmarshal(block.Input, &input)
				if input.RunInBackground == nil || *input.RunInBackground {
					stats.Background++
					stats.Problems = append(stats.Problems, "Agent "+block.ID+" was not synchronous (run_in_background=false required)")
				} else {
					syncAgent[block.ID] = true
				}
				continue
			}
			if block.Type != "tool_result" || block.ToolUseID == "" {
				continue
			}
			if syncAgent[block.ToolUseID] && !completed[block.ToolUseID] {
				completed[block.ToolUseID] = true
				stats.Completed++
				continue
			}
			// A result for a nested Agent has no counted top-level start. Name it instead
			// of silently treating starts/stops as balanced.
			if allAgent[block.ToolUseID] && !countedStart[block.ToolUseID] {
				stats.OrphanResults++
			}
		}
	}

	// parseClaudeStream is called after the parent process/stream has terminated. A
	// synchronous Agent that started but never produced its tool_result is therefore
	// aborted, not "still live". Background Agents are unsupported and may outlive the
	// parent, so they remain explicitly live/unsafe.
	for id := range syncAgent {
		if !completed[id] {
			stats.Aborted++
		}
	}
	stats.Live = stats.Background

	if stats.Started != requested {
		stats.Problems = append(stats.Problems, fmt.Sprintf("requested %d agents, observed %d starts", requested, stats.Started))
	}
	if stats.Completed != requested {
		stats.Problems = append(stats.Problems, fmt.Sprintf("requested %d agents, observed %d completed synchronous results", requested, stats.Completed))
	}
	if stats.Aborted > 0 {
		stats.Problems = append(stats.Problems, fmt.Sprintf("%d synchronous agents ended without a result", stats.Aborted))
	}
	if stats.OrphanResults > 0 {
		stats.Problems = append(stats.Problems, fmt.Sprintf("%d agent results had no counted top-level start", stats.OrphanResults))
	}
	if stats.Live > 0 {
		stats.Problems = append(stats.Problems, fmt.Sprintf("%d background agents may still be live", stats.Live))
	}
	return result, stats
}

func orchestrationProven(stats agentLifecycle) bool {
	return stats.Requested > 0 &&
		stats.Started == stats.Requested &&
		stats.Completed == stats.Requested &&
		stats.Aborted == 0 &&
		stats.OrphanResults == 0 &&
		stats.Live == 0 &&
		stats.Background == 0
}

func orchestrationProblem(stats agentLifecycle) string {
	if len(stats.Problems) == 0 {
		return "orchestration evidence incomplete"
	}
	return strings.Join(stats.Problems, "; ")
}

func applyAgentLifecycle(r stepResult, stats agentLifecycle) stepResult {
	r.AgentRequested = stats.Requested
	r.AgentStarted = stats.Started
	r.AgentCompleted = stats.Completed
	r.AgentAborted = stats.Aborted
	r.AgentOrphan = stats.OrphanResults
	r.AgentLive = stats.Live
	r.AgentIDs = append([]string(nil), stats.IDs...)
	if !orchestrationProven(stats) {
		r.AgentIssue = orchestrationProblem(stats)
	}
	return r
}

func addAgentLifecycleFields(row map[string]any, r stepResult) {
	row["agents_requested"] = r.AgentRequested
	row["agents_started"] = r.AgentStarted
	row["agents_completed"] = r.AgentCompleted
	row["agents_aborted"] = r.AgentAborted
	row["agent_orphan_results"] = r.AgentOrphan
	row["agents_live"] = r.AgentLive
	row["agent_ids"] = r.AgentIDs
	row["agent_issue"] = nullIfEmpty(r.AgentIssue)
}

func (c *loopCtx) invokeClaudeOrchestrated(exePath, prompt string, runner runnerSpec, stepID string) stepResult {
	args := []string{"-p", prompt, "--model", runner.Model, "--output-format", "stream-json",
		"--forward-subagent-text", "--verbose", "--max-turns", fmt.Sprintf("%d", c.MaxTurns)}
	if c.Permission != "" {
		args = append(args, "--permission-mode", c.Permission)
	}
	if c.Tools != "" {
		args = append(args, "--allowed-tools", c.Tools)
	}
	if runner.Effort != "" {
		args = append(args, "--effort", runner.Effort)
	}
	if left := c.MaxUSD - c.spent; left > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", left))
	}
	cmd := exec.Command(exePath, args...)
	cmd.Dir = c.Root
	if env, took := runnerEnv(); took {
		cmd.Env = env
		line("  токен взят из окружения пользователя (в процессе его не было)")
	}
	// К42 — durable job receipt пишется RUNNING ДО запуска оркестрованного исполнителя.
	operation := runnerReceiptOperation("executor-claude-orchestrated", runner, c.iter)
	out, _ := runReceiptedWithMeta(context.Background(), c.scope(), stepID, operation, cmd, runnerReceiptMetaFor(runner))
	raw := decodeOutput(out)
	res, agents := parseClaudeStream(raw, c.Subagents)
	if res == nil {
		if isVendorLimit(raw) {
			return applyAgentLifecycle(stepResult{Subtype: "vendor_limit", Detail: strings.TrimSpace(raw)}, agents)
		}
		line("  claude orchestration stream не вернул разбираемый result")
		return applyAgentLifecycle(stepResult{Subtype: "unparsed_orchestration"}, agents)
	}
	sub := res.Subtype
	if res.IsError {
		sub = "runner_error"
		if isVendorLimit(res.Result) {
			sub = "vendor_limit"
		}
	}
	if sub == "vendor_limit" {
		return applyAgentLifecycle(stepResult{Ok: false, Cost: res.TotalCostUSD, Turns: res.NumTurns, Session: res.SessionID, Subtype: sub,
			ApiMs: res.DurationAPI, Detail: strings.TrimSpace(res.Result)}, agents)
	}
	if !orchestrationProven(agents) {
		sub = "orchestration_not_proven"
		return applyAgentLifecycle(stepResult{Ok: false, Cost: res.TotalCostUSD, Turns: res.NumTurns, Session: res.SessionID, Subtype: sub,
			ApiMs: res.DurationAPI, Detail: strings.TrimSpace(res.Result)}, agents)
	}
	if !res.IsError && reNeedsPermission.MatchString(res.Result) && !c.treeChanged() {
		sub = "needs_permission"
	}
	return applyAgentLifecycle(stepResult{Ok: !res.IsError, Cost: res.TotalCostUSD, Turns: res.NumTurns, Session: res.SessionID, Subtype: sub,
		ApiMs: res.DurationAPI, Detail: strings.TrimSpace(res.Result)}, agents)
}

func cmdOrchestrate(argv []string) int {
	args := make([]string, 0, len(argv)+1)
	args = append(args, "-orchestrate")
	args = append(args, argv...)
	return cmdLoop(args)
}
