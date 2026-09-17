package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

var runnerCommand = exec.Command

func codexEnv(base []string) []string {
	inheritSystemProxy := base == nil
	if base == nil {
		base = os.Environ()
	}
	out := make([]string, 0, len(base)+4)
	for _, e := range base {
		key := strings.ToUpper(strings.SplitN(e, "=", 2)[0])
		if strings.HasPrefix(key, "CLAUDE_") || strings.HasPrefix(key, "ANTHROPIC_") {
			continue
		}
		out = append(out, e)
	}
	if inheritSystemProxy {
		out = append(out, systemProxyFallbackEnv()...)
	}
	return out
}

func isCodexVolumeRoot(path string) bool {
	clean := filepath.Clean(path)
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	return clean == string(filepath.Separator) ||
		(volume != "" && (rest == "" || rest == `\` || rest == "/"))
}

func validateCodexAddDirs(paths []string) error {
	for i, path := range paths {
		raw := strings.TrimSpace(path)
		if raw == "" {
			return fmt.Errorf("entry %d is empty", i+1)
		}
		if !filepath.IsAbs(raw) {
			return fmt.Errorf("entry %d must be absolute: %q", i+1, path)
		}
		clean := filepath.Clean(raw)
		if isCodexVolumeRoot(clean) {
			return fmt.Errorf("entry %d must not be a volume root: %q", i+1, path)
		}
		info, err := os.Stat(clean)
		if err != nil {
			return fmt.Errorf("entry %d is unavailable: %q: %w", i+1, path, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("entry %d is not a directory: %q", i+1, path)
		}
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return fmt.Errorf("entry %d cannot be resolved: %q: %w", i+1, path, err)
		}
		if isCodexVolumeRoot(resolved) {
			return fmt.Errorf("entry %d resolves to a volume root: %q", i+1, path)
		}
	}
	return nil
}

func codexArgsForSandbox(root, prompt string, runner runnerSpec, sandbox string) []string {
	args := []string{"exec", "--json", "--skip-git-repo-check", "-s", sandbox, "-C", root}
	if runner.Model != "" {
		args = append(args, "-m", runner.Model)
	}
	if runner.Effort != "" {
		args = append(args, "-c", "model_reasoning_effort="+runner.Effort)
	}
	for _, dir := range runner.AddDirs {
		args = append(args, "--add-dir", filepath.Clean(strings.TrimSpace(dir)))
	}
	return append(args, prompt)
}

func codexArgs(root, prompt string, runner runnerSpec) []string {
	return codexArgsForSandbox(root, prompt, runner, "workspace-write")
}

func codexReceiptMeta(runner runnerSpec) jobReceiptMeta {
	return jobReceiptMeta{Runner: "codex", Provider: "openai", Model: runner.Model, Effort: runner.Effort}
}

func (c *loopCtx) invokeCodex(exePath, prompt string, runner runnerSpec, stepID string) stepResult {
	if err := validateCodexAddDirs(runner.AddDirs); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "codex add_dirs: " + err.Error()}
	}
	cmd := runnerCommand(exePath, codexArgs(c.Root, prompt, runner)...)
	cmd.Dir = c.Root
	cmd.Env = codexEnv(nil)
	cmd.Stdin = nil
	// К42 — durable job receipt пишется RUNNING ДО запуска исполнителя Codex.
	out, runErr := runReceiptedWithMeta(context.Background(), c.scope(), stepID, "executor-codex", cmd, codexReceiptMeta(runner))
	return parseCodexResult(decodeOutput(out), runErr)
}

type codexSubagentRun struct {
	index   int
	started bool
	result  stepResult
}

func (c *loopCtx) invokeCodexOrchestrated(exePath, prompt string, runner runnerSpec, stepID string) stepResult {
	if err := validateCodexAddDirs(runner.AddDirs); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "codex add_dirs: " + err.Error()}
	}
	requested := c.Subagents
	if requested < 1 {
		requested = 1
	}
	effort := runner.Effort
	if effort == "" {
		effort = "default"
	}
	line(fmt.Sprintf("  orchestration core: AirWorker · transport codex · model %s · effort %s · subagents %d",
		runner.Model, effort, requested))

	results := make(chan codexSubagentRun, requested)
	var wg sync.WaitGroup
	for i := 1; i <= requested; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			reviewPrompt := fmt.Sprintf(
				"You are AirWorker native read-only subagent %d/%d. Analyze the task independently. Do not edit files. Return concrete risks, an implementation or review plan, and exact checks.\n\nCoordinator task:\n%s",
				index, requested, prompt)
			cmd := runnerCommand(exePath, codexArgsForSandbox(c.Root, reviewPrompt, runner, "read-only")...)
			cmd.Dir = c.Root
			cmd.Env = codexEnv(nil)
			agentStep := fmt.Sprintf("%s-agent-%d", stepID, index)
			out, runErr := runReceiptedWithMeta(context.Background(), c.scope(), agentStep, "executor-codex-subagent", cmd, codexReceiptMeta(runner))
			receiptPath, _ := jobReceiptPaths(c.scope(), agentStep, "executor-codex-subagent")
			receipt, _ := readJobReceipt(receiptPath)
			results <- codexSubagentRun{index: index, started: receipt != nil && receipt.ProcessStarted, result: parseCodexResult(decodeOutput(out), runErr)}
		}(i)
	}
	wg.Wait()
	close(results)

	ordered := make([]stepResult, requested)
	started, completed := 0, 0
	ids := make([]string, 0, requested)
	var problems []string
	for run := range results {
		ordered[run.index-1] = run.result
		if run.started {
			started++
		}
		if run.result.Session != "" {
			ids = append(ids, run.result.Session)
		}
		if run.result.Ok {
			completed++
		} else {
			problems = append(problems, fmt.Sprintf("subagent %d failed: %s %s", run.index, run.result.Subtype, run.result.Detail))
		}
	}
	if completed != requested {
		problems = append(problems, fmt.Sprintf("requested %d subagents, completed %d", requested, completed))
		return stepResult{Subtype: "orchestration_not_proven", AgentRequested: requested, AgentStarted: started,
			AgentCompleted: completed, AgentIDs: ids, AgentIssue: strings.Join(problems, "; ")}
	}

	var evidence strings.Builder
	evidence.WriteString("\n\nAIRWORKER NATIVE ORCHESTRATION: the kernel completed independent read-only reviews. Reconcile them before editing.\n")
	for i, res := range ordered {
		detail := res.Detail
		if len(detail) > 12000 {
			detail = detail[:12000]
		}
		fmt.Fprintf(&evidence, "\nSUBAGENT %d/%d (%s):\n%s\n", i+1, requested, runner.Model, detail)
	}
	leader := c.invokeCodex(exePath, prompt+evidence.String(), runner, stepID)
	leader.AgentRequested = requested
	leader.AgentStarted = started
	leader.AgentCompleted = completed
	leader.AgentIDs = ids
	return leader
}

func parseCodexResult(raw string, runErr error) stepResult {
	turns := 0
	completed, failed := false, false
	subtype := "unknown"
	var cost *float64
	session := ""
	var detail []string
	for _, l := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
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
			if e.Item == nil {
				continue
			}
			if e.Item.Type == "error" && strings.TrimSpace(e.Item.Message) != "" {
				detail = append(detail, "warning: "+strings.TrimSpace(e.Item.Message))
			}
			if e.Item.Type == "agent_message" && strings.TrimSpace(e.Item.Text) != "" {
				detail = append(detail, strings.TrimSpace(e.Item.Text))
			}
		case "turn.completed":
			completed = true
			if !failed {
				subtype = "success"
			}
			if e.Usage != nil {
				cost = e.Usage.CostUSD
			}
		case "turn.failed":
			failed, subtype = true, "runner_error"
			if e.Error != nil && strings.TrimSpace(e.Error.Message) != "" {
				detail = append(detail, "turn.failed: "+strings.TrimSpace(e.Error.Message))
			}
		case "error":
			failed, subtype = true, "runner_error"
			if strings.TrimSpace(e.Message) != "" {
				detail = append(detail, "error: "+strings.TrimSpace(e.Message))
			}
		}
	}
	if runErr != nil && !completed && !failed {
		failed, subtype = true, "runner_error"
		detail = append(detail, "codex process: "+runErr.Error())
	}
	if !completed && !failed {
		subtype = "unparsed"
		detail = append(detail, "codex stream ended without terminal event")
	}
	return stepResult{
		Ok: completed && !failed, Cost: cost, Turns: &turns, Session: session,
		Subtype: subtype, Detail: strings.Join(detail, "\n"),
	}
}
