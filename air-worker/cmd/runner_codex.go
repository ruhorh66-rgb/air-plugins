package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func codexArgs(root, prompt string, runner runnerSpec) []string {
	args := []string{"exec", "--json", "--skip-git-repo-check", "-s", "workspace-write", "-C", root}
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

func (c *loopCtx) invokeCodex(exePath, prompt string, runner runnerSpec, stepID string) stepResult {
	if err := validateCodexAddDirs(runner.AddDirs); err != nil {
		return stepResult{Subtype: "invalid_runner_config", Detail: "codex add_dirs: " + err.Error()}
	}
	cmd := runnerCommand(exePath, codexArgs(c.Root, prompt, runner)...)
	cmd.Dir = c.Root
	cmd.Env = codexEnv(nil)
	cmd.Stdin = nil
	// К42 — durable job receipt пишется RUNNING ДО запуска исполнителя Codex.
	out, runErr := runReceipted(context.Background(), c.scope(), stepID, "executor-codex", cmd)
	return parseCodexResult(decodeOutput(out), runErr)
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
