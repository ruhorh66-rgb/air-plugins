package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func plannerRunner(cfg runConfig, tier string) (runnerSpec, error) {
	r := resolveRunner(cfg, tier)
	if r.Kind == "script" {
		return runnerSpec{}, fmt.Errorf("planner tier %q resolves to script", tier)
	}
	if r.Kind != "claude" && r.Kind != "codex" {
		return runnerSpec{}, fmt.Errorf("unsupported planner runner %q", r.Kind)
	}
	return r, nil
}

func plannerCodexArgs(root, prompt string, runner runnerSpec) []string {
	args := []string{"exec", "--json", "--skip-git-repo-check", "-s", "read-only", "-C", root}
	if runner.Model != "" {
		args = append(args, "-m", runner.Model)
	}
	if runner.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+runner.Effort)
	}
	return append(args, prompt)
}

func callPlannerRunner(root, prompt string, runner runnerSpec, answerPath string, scope sessionScope) int {
	if runner.Kind == "claude" {
		return callClaudePlanner(root, prompt, runner.Model, answerPath, scope)
	}
	if runner.Kind != "codex" {
		line("ОТКАЗ: неизвестный вендор планировщика: " + runner.Kind)
		return 2
	}
	exe, err := resolveRunnerTool("codex")
	if err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	args := plannerCodexArgs(root, prompt, runner)
	line(fmt.Sprintf("зову планировщика Codex: role planner · model %s · effort %s · sandbox read-only", runner.Model, runner.Effort))
	cmd := runnerCommand(exe, args...)
	cmd.Dir = root
	cmd.Env = codexEnv(nil)
	cmd.Stdin = nil
	// К42 — durable job receipt пишется RUNNING ДО запуска разбивщика Codex.
	out, runErr := runReceiptedWithMeta(context.Background(), scope, "plan", "planner-codex", cmd,
		codexReceiptMeta(runner, "planner", "read-only"))
	res := parseCodexResult(decodeOutput(out), runErr)
	if !res.Ok {
		line("ОТКАЗ: планировщик Codex не завершил turn: " + res.Subtype)
		if strings.TrimSpace(res.Detail) != "" {
			line(strings.TrimSpace(res.Detail))
		}
		return 1
	}
	normalized := claudeResult{
		IsError:      false,
		TotalCostUSD: res.Cost,
		NumTurns:     res.Turns,
		SessionID:    res.Session,
		Subtype:      res.Subtype,
		Type:         "result",
		Result:       res.Detail,
	}
	b, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return 1
	}
	f, err := os.Create(answerPath)
	if err != nil {
		return 1
	}
	defer f.Close()
	if _, err := f.Write(b); err != nil {
		return 1
	}
	return 0
}
