package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// A conservative Windows unelevated sandbox profile: the product root and
// extra writable directories must be explicit, accessible and on the same
// volume. We do not silently permit cross-volume or linked writable roots.
// A different permitted volume/topology needs its own proven host policy.
func validateCodexWorkspaceRoots(root string, dirs []string) error {
	if err := validateCodexAddDirs(dirs); err != nil {
		return err
	}
	if runtime.GOOS != "windows" || len(dirs) == 0 {
		return nil
	}
	if !filepath.IsAbs(root) || isCodexVolumeRoot(root) {
		return fmt.Errorf("Windows Codex workspace root must be an explicit non-volume directory: %q", root)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("Windows Codex workspace root unavailable: %q: %v", root, err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("Windows Codex workspace root cannot be canonicalized: %q: %w", root, err)
	}
	volume := filepath.VolumeName(canonicalRoot)
	if volume == "" {
		return fmt.Errorf("Windows Codex workspace volume unavailable: %q", root)
	}
	seen := map[string]bool{}
	for index, dir := range dirs {
		canonical, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return fmt.Errorf("Windows Codex add_dir %d cannot be canonicalized: %w", index+1, err)
		}
		if !strings.EqualFold(filepath.VolumeName(canonical), volume) {
			return fmt.Errorf("Windows Codex add_dir %d is on a different volume; unelevated multi-volume writable roots NOT_PROVEN", index+1)
		}
		if !strings.EqualFold(filepath.Clean(dir), filepath.Clean(canonical)) {
			return fmt.Errorf("Windows Codex add_dir %d traverses a reparse/link root; refuse unverified writable alias", index+1)
		}
		key := strings.ToLower(filepath.Clean(canonical))
		if seen[key] {
			return fmt.Errorf("Windows Codex add_dir %d repeats a writable root", index+1)
		}
		seen[key] = true
		// An ancestor of the product would silently extend the writable
		// scope beyond the intended workspace.
		relative, relErr := filepath.Rel(canonical, canonicalRoot)
		if relErr == nil && relative != "." && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("Windows Codex add_dir %d contains the entire workspace and would broaden writable scope", index+1)
		}
	}
	return nil
}

func codexArgsForSandbox(root string, runner runnerSpec, sandbox string) []string {
	args := []string{"exec", "--json", "--skip-git-repo-check", "-s", sandbox, "-C", root}
	if runtime.GOOS == "windows" && sandbox == "workspace-write" {
		// Windows restricted-token split vs legacy slash-tmp roots. Restrict, never bypass sandbox.
		args = append(args, "-c", "sandbox_workspace_write.exclude_slash_tmp=true")
	}
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
	if err := validateCodexWorkspaceRoots(c.Root, runner.AddDirs); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "codex add_dirs: " + err.Error()}
	}
	if err := requirePonytailSkill(); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "ponytail: " + err.Error()}
	}
	cmd := codexCommandForSandbox(exePath, c.Root, prompt, runner, "workspace-write")
	// К42 — durable job receipt пишется RUNNING ДО запуска исполнителя Codex.
	operation := runnerReceiptOperation("executor-codex", runner, c.iter)
	out, runErr := runReceiptedWithMeta(context.Background(), c.scope(), stepID, operation, cmd, codexReceiptMeta(runner, "executor/leader", "workspace-write"))
	result := parseCodexResult(decodeOutput(out), runErr)
	receiptPath, _ := jobReceiptPaths(c.scope(), stepID, operation)
	if err := recordCodexModelOutcome(receiptPath, result); err != nil {
		return stepResult{Subtype: "outcome_receipt_error", Detail: err.Error()}
	}
	return result
}

type codexSubagentRun struct {
	index   int
	started bool
	result  stepResult
}

func (c *loopCtx) invokeCodexOrchestrated(exePath, prompt string, runner runnerSpec, stepID string) stepResult {
	if err := validateCodexWorkspaceRoots(c.Root, runner.AddDirs); err != nil {
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
	line(fmt.Sprintf("  process roster: %d subagents read-only · leader workspace-write", requested))

	results := make(chan codexSubagentRun, requested)
	var wg sync.WaitGroup
	for i := 1; i <= requested; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			reviewPrompt := fmt.Sprintf(
				"You are AirWorker native read-only subagent %d/%d. Analyze the task independently. Do not edit files. Return concrete risks, an implementation or review plan, and exact checks.\n\nCoordinator task:\n%s",
				index, requested, prompt)
			cmd := codexCommandForSandbox(exePath, c.Root, reviewPrompt, runner, "read-only")
			agentStep := fmt.Sprintf("%s-agent-%d", stepID, index)
			operation := runnerReceiptOperation("executor-codex-subagent", runner, c.iter)
			out, runErr := runReceiptedWithMeta(context.Background(), c.scope(), agentStep, operation, cmd, codexReceiptMeta(runner, "orchestration-subagent", "read-only"))
			receiptPath, _ := jobReceiptPaths(c.scope(), agentStep, operation)
			result := parseCodexResult(decodeOutput(out), runErr)
			if err := recordCodexModelOutcome(receiptPath, result); err != nil {
				result = stepResult{Subtype: "outcome_receipt_error", Detail: err.Error()}
			}
			receipt, _ := readJobReceipt(receiptPath)
			results <- codexSubagentRun{index: index, started: receipt != nil && receipt.ProcessStarted, result: result}
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
		case run.started && run.result.Ok:
			completed++
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
	evidence.WriteString("\n\nAIRWORKER NATIVE ORCHESTRATION: the kernel completed independent read-only reviews. Reconcile them before editing.\n")
	for i, res := range ordered {
		detail := res.Detail
		if len(detail) > 12000 {
			detail = detail[:12000]
		}
		fmt.Fprintf(&evidence, "\nSUBAGENT %d/%d (%s):\n%s\n", i+1, requested, runner.Model, detail)
	}
	leader := c.invokeCodex(exePath, prompt+evidence.String(), runner, stepID)
	return applyAgentLifecycle(leader, stats)
}

func codexPermissionRefused(text string) bool {
	lower := strings.ToLower(text)
	for _, marker := range []string{
		"access is denied", "access denied", "permission denied",
		"required privilege is not held", "operation not permitted",
		"not authorized", "unauthorized", "eacces", "eperm",
		"read-only file system", "restricted-token sandbox",
		"refusing to run unsandboxed", "sandbox denied",
		"permission was denied", "security policy denied",
		"access to the path", "access to path",
		"отказано в доступе", "доступ запрещ", "недостаточно прав",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// Only structured failed tool events count as tool access refusals. A model
// quoting a Windows error in a successful source-review tool is NOT a denial.
func codexToolFailure(e codexEvent) (string, string) {
	if e.Item == nil {
		return "", ""
	}
	i := e.Item
	if i.Type != "command_execution" && i.Type != "mcp_tool_call" {
		return "", ""
	}
	toolFailed := (i.ExitCode != nil && *i.ExitCode != 0) ||
		strings.EqualFold(i.Status, "failed") ||
		(len(i.Error) > 0 && strings.TrimSpace(string(i.Error)) != "null")
	if !toolFailed {
		return "", ""
	}
	diagnostic := strings.Join([]string{i.Message, i.AggregatedOutput,
		string(i.Error), string(i.Result)}, "\n")
	if codexPermissionRefused(diagnostic) {
		return "tool_access_denied", "Codex tool returned an access/security denial; task NOT_PROVEN"
	}
	if strings.Contains(strings.ToLower(i.Command), "task.md") &&
		i.ExitCode != nil && *i.ExitCode != 0 {
		return "task_input_unavailable", "Codex tool failed reading TASK.md; task NOT_PROVEN"
	}
	return "", ""
}

func codexReportedUnreadableTask(detail []string) bool {
	for _, part := range detail {
		line := strings.ToLower(strings.TrimSpace(part))
		task := strings.Contains(line, "task.md") ||
			strings.Contains(line, "task file") || strings.Contains(line, "input file")
		if !task {
			continue
		}
		for _, phrase := range []string{
			"i couldn't read", "i could not read", "i couldn’t read",
			"i can't read", "i cannot read", "i didn't read", "i did not read",
			"i was unable to read", "unable to read", "unable to open",
			"cannot open", "could not open", "failed to read",
			"access is denied", "access denied", "permission denied",
			"not readable", "was not read",
		} {
			if strings.Contains(line, phrase) {
				return true
			}
		}
	}
	return false
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
			if reason, diagnostic := codexToolFailure(e); reason != "" {
				failed, subtype = true, reason
				detail = append(detail, diagnostic)
			}
			if e.Item.Type == "error" && strings.TrimSpace(e.Item.Message) != "" {
				if codexPermissionRefused(e.Item.Message) {
					failed, subtype = true, "tool_access_denied"
					detail = append(detail, "Codex error event denied access; task NOT_PROVEN")
				} else {
					detail = append(detail, "warning: "+strings.TrimSpace(e.Item.Message))
				}
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
			failed = true
			if subtype != "tool_access_denied" && subtype != "task_input_unavailable" {
				subtype = "runner_error"
			}
			if e.Error != nil && strings.TrimSpace(e.Error.Message) != "" {
				detail = append(detail, "turn.failed: "+strings.TrimSpace(e.Error.Message))
			}
		case "error":
			failed = true
			if subtype != "tool_access_denied" && subtype != "task_input_unavailable" {
				subtype = "runner_error"
			}
			if strings.TrimSpace(e.Message) != "" {
				detail = append(detail, "error: "+strings.TrimSpace(e.Message))
			}
		}
	}
	// Process error is fatal even if turn.completed was emitted.
	if runErr != nil {
		failed = true
		if subtype != "tool_access_denied" && subtype != "task_input_unavailable" {
			subtype = "runner_error"
		}
		detail = append(detail, "codex process: "+runErr.Error())
	}
	// Vera and Vega: Windows sandbox refused file access but Codex emitted
	// a final agent message and turn.completed. Neither proves task execution.
	if strings.Contains(strings.ToLower(raw), "windows unelevated restricted-token sandbox cannot enforce split writable root sets directly") {
		failed, subtype = true, "sandbox_unavailable"
		detail = append(detail, "Codex sandbox denied executable file access; TASK.md read NOT_PROVEN")
	}
	if !failed && completed && codexReportedUnreadableTask(detail) {
		failed, subtype = true, "task_input_unavailable"
		detail = append(detail, "Codex reported unreadable task input; task NOT_PROVEN")
	}
	if !completed && !failed {
		subtype = "unparsed"
		detail = append(detail, "codex stream ended without terminal event")
	}
	if failed && subtype == "runner_error" && isVendorLimit(raw+"\n"+strings.Join(detail, "\n")) {
		subtype = "vendor_limit"
	}
	return stepResult{
		Ok: completed && !failed, Cost: cost, Turns: &turns, Session: session,
		Subtype: subtype, Detail: strings.Join(detail, "\n"),
	}
}
