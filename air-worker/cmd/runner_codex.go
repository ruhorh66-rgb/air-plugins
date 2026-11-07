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

func codexArgsForSandbox(root string, runner runnerSpec, sandbox string) []string {
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
	return append(args, "-")
}

func codexArgs(root string, runner runnerSpec) []string {
	return codexArgsForSandbox(root, runner, "workspace-write")
}

func codexCommandForSandbox(exePath, root, prompt string, runner runnerSpec, sandbox string) *exec.Cmd {
	cmd := runnerCommand(exePath, codexArgsForSandbox(root, runner, sandbox)...)
	cmd.Dir = root
	cmd.Env = codexEnv(nil)
	cmd.Stdin = strings.NewReader(ponytailPrompt(prompt))
	return cmd
}

func codexReceiptMeta(runner runnerSpec, role, sandbox string) jobReceiptMeta {
	return runnerReceiptMetaForRole(runner, role, sandbox)
}

func (c *loopCtx) invokeCodex(exePath, prompt string, runner runnerSpec, stepID string) stepResult {
	if err := validateCodexAddDirs(runner.AddDirs); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "codex add_dirs: " + err.Error()}
	}
	if err := requirePonytailSkill(); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "ponytail: " + err.Error()}
	}
	cmd := codexCommandForSandbox(exePath, c.Root, prompt, runner, "workspace-write")
	// К42 — durable job receipt пишется RUNNING ДО запуска исполнителя Codex.
	operation := runnerReceiptOperation("executor-codex", runner, c.iter)
	out, runErr := runReceiptedWithMeta(context.Background(), c.scope(), stepID, operation, cmd, codexReceiptMeta(runner, "executor/leader", "workspace-write"))
	return parseCodexResult(decodeOutput(out), runErr)
}

type codexSubagentRun struct {
	index   int
	started bool
	result  stepResult
}

// A Codex coding subagent is not credited for a narrative review. It must
// produce an identifiable unified source patch in its terminal model output.
// This is a candidate only; the leader and independent judges still own all
// changes, applicability, factual QA, and semantic acceptance. Never apply the
// untrusted diff automatically or give the subagent shared-tree write access.
func codexPatchCandidatePresent(detail string) bool {
	if len(detail) == 0 || len(detail) > 256*1024 || strings.Contains(detail, "GIT binary patch") {
		return false
	}
	var path string
	var oldFile, newFile, hunk, changed bool
	for _, line := range strings.Split(strings.ReplaceAll(detail, "\r\n", "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git a/"):
			fields := strings.Fields(line)
			if len(fields) != 4 || !strings.HasPrefix(fields[3], "b/") ||
				strings.TrimPrefix(fields[2], "a/") != strings.TrimPrefix(fields[3], "b/") {
				return false
			}
			path = strings.TrimPrefix(fields[2], "a/")
			if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "\\") ||
				strings.Contains(path, "../") || strings.Contains(path, "/..") || path == ".." {
				return false
			}
		case strings.HasPrefix(line, "--- a/"):
			oldFile = path != "" && strings.TrimPrefix(line, "--- a/") == path
		case strings.HasPrefix(line, "+++ b/"):
			newFile = path != "" && strings.TrimPrefix(line, "+++ b/") == path
		case strings.HasPrefix(line, "@@"):
			hunk = oldFile && newFile
		case hunk && (strings.HasPrefix(line, "+") || strings.HasPrefix(line, "-")):
			changed = true
		}
	}
	return path != "" && oldFile && newFile && hunk && changed
}

func (c *loopCtx) invokeCodexOrchestrated(exePath, prompt string, runner runnerSpec, stepID string) stepResult {
	if err := validateCodexAddDirs(runner.AddDirs); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "codex add_dirs: " + err.Error()}
	}
	if err := requirePonytailSkill(); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "ponytail: " + err.Error()}
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
	line(fmt.Sprintf("  process roster: %d Codex coding subagents (independent read-only patch authors) · leader workspace-write", requested))

	results := make(chan codexSubagentRun, requested)
	var wg sync.WaitGroup
	for i := 1; i <= requested; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			reviewPrompt := fmt.Sprintf(
				"You are AirWorker Codex CODING SUBAGENT %d/%d, operating independently of the other Codex subagent. Read the coordinator task and the actual source files first. AUTHOR A CONCRETE SOURCE PATCH: return one compilable unified diff with lines diff --git, --- a/, +++ b/ and @@. Do not edit the shared working tree: you are a read-only patch author and the leader is the only writer. Do not return review-only comments instead of code. Follow the task scope; no changes to protected plan, policies or judge. After the patch, state narrow checks.\n\nCoordinator task:\n%s",
				index, requested, prompt)
			cmd := codexCommandForSandbox(exePath, c.Root, reviewPrompt, runner, "read-only")
			agentStep := fmt.Sprintf("%s-agent-%d", stepID, index)
			operation := runnerReceiptOperation("executor-codex-subagent", runner, c.iter)
			out, runErr := runReceiptedWithMeta(context.Background(), c.scope(), agentStep, operation, cmd, codexReceiptMeta(runner, "coding-subagent/patch-author", "read-only"))
			receiptPath, _ := jobReceiptPaths(c.scope(), agentStep, operation)
			receipt, _ := readJobReceipt(receiptPath)
			results <- codexSubagentRun{index: index, started: receipt != nil && receipt.ProcessStarted, result: parseCodexResult(decodeOutput(out), runErr)}
		}(i)
	}
	wg.Wait()
	close(results)

	ordered := make([]stepResult, requested)
	started, completed, aborted, orphan := 0, 0, 0, 0
	ids := make([]string, 0, requested)
	var problems []string
	var vendorLimit string
	for run := range results {
		ordered[run.index-1] = run.result
		if run.started {
			started++
		}
		if run.result.Session != "" {
			ids = append(ids, run.result.Session)
		}
		switch {
		case run.started && run.result.Ok && codexPatchCandidatePresent(run.result.Detail):
			completed++
		case run.started && run.result.Ok:
			aborted++
			problems = append(problems, fmt.Sprintf("coding subagent %d supplied no unified source patch; review-only response cannot count as coding", run.index))
		case run.started && !run.result.Ok:
			aborted++
			problems = append(problems, fmt.Sprintf("subagent %d failed: %s %s", run.index, run.result.Subtype, run.result.Detail))
		case !run.started && run.result.Ok:
			orphan++
			problems = append(problems, fmt.Sprintf("subagent %d returned success without a proven process start", run.index))
		default:
			problems = append(problems, fmt.Sprintf("subagent %d did not start: %s %s", run.index, run.result.Subtype, run.result.Detail))
		}
		if run.result.Subtype == "vendor_limit" && vendorLimit == "" {
			vendorLimit = run.result.Detail
		}
	}
	stats := agentLifecycle{
		Requested: requested, Started: started, Completed: completed, Aborted: aborted,
		OrphanResults: orphan, Live: 0, IDs: ids, Problems: problems,
	}
	if vendorLimit != "" {
		return applyAgentLifecycle(stepResult{Subtype: "vendor_limit", Detail: vendorLimit}, stats)
	}
	if !orchestrationProven(stats) {
		if completed != requested {
			stats.Problems = append(stats.Problems, fmt.Sprintf("requested %d subagents, completed %d", requested, completed))
		}
		return applyAgentLifecycle(stepResult{Subtype: "orchestration_not_proven"}, stats)
	}

	var evidence strings.Builder
	evidence.WriteString("\n\nAIRWORKER NATIVE ORCHESTRATION: two independent Codex subagents authored SOURCE PATCH proposals in read-only sandboxes. Reconcile both candidate diffs; the leader is the SINGLE shared-tree writer. These candidate patches are NOT applied, tested, or independently accepted by their mere presence.\n")
	for i, res := range ordered {
		detail := res.Detail
		if len(detail) > 12000 {
			detail = detail[:12000]
		}
		fmt.Fprintf(&evidence, "\nCODEX PATCH AUTHOR %d/%d (%s):\n%s\n", i+1, requested, runner.Model, detail)
	}
	leader := c.invokeCodex(exePath, prompt+evidence.String(), runner, stepID)
	return applyAgentLifecycle(leader, stats)
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
	// Codex may write turn.completed before its OS process exits. A nonzero
	// process result is authoritative even when the stream claims completion:
	// the execution must not yield an Ok=true receipt or a model-step PASS.
	if runErr != nil {
		failed, subtype = true, "runner_error"
		detail = append(detail, "codex process: "+runErr.Error())
	}
	if !completed && !failed {
		subtype = "unparsed"
		detail = append(detail, "codex stream ended without terminal event")
	}
	if failed && isVendorLimit(raw+"\n"+strings.Join(detail, "\n")) {
		subtype = "vendor_limit"
	}
	return stepResult{
		Ok: completed && !failed, Cost: cost, Turns: &turns, Session: session,
		Subtype: subtype, Detail: strings.Join(detail, "\n"),
	}
}
