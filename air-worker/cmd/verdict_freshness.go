package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const machineVerdictMaxAge = 6 * time.Hour
const gitOutputLimit = 4 << 20

type limitedGitOutput struct{ bytes.Buffer }

func (w *limitedGitOutput) Write(p []byte) (int, error) {
	if w.Len()+len(p) > gitOutputLimit {
		return 0, fmt.Errorf("git output exceeds %d bytes", gitOutputLimit)
	}
	return w.Buffer.Write(p)
}

func gitRead(root string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	var out, stderr limitedGitOutput
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, stderr.String())
	}
	return out.Bytes(), nil
}

func currentGitHead(root string) string {
	out, err := gitRead(root, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func judgeSourceIdentity(root string) (head string, clean bool, err error) {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", false, fmt.Errorf("invalid product root %q: %v", root, err)
	}
	inside, err := gitRead(root, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		for dir := root; ; dir = filepath.Dir(dir) {
			if _, statErr := os.Stat(filepath.Join(dir, ".git")); statErr == nil || !os.IsNotExist(statErr) {
				return "", false, err
			}
			if parent := filepath.Dir(dir); parent == dir {
				break
			}
		}
		if strings.Contains(err.Error(), "not a git repository") {
			return "", true, nil
		}
		return "", false, err
	}
	if string(inside) != "true\n" {
		return "", false, fmt.Errorf("product root is not inside a Git worktree")
	}
	out, err := gitRead(root, "rev-parse", "HEAD")
	if err != nil {
		return "", false, err
	}
	head = strings.TrimSpace(string(out))
	if len(head) != 40 && len(head) != 64 {
		return "", false, fmt.Errorf("invalid Git HEAD")
	}
	for _, c := range head {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return "", false, fmt.Errorf("invalid Git HEAD")
		}
	}
	tracked, err := gitRead(root, "diff", "--name-only", "-z", "HEAD", "--", ".")
	if err != nil {
		return head, false, err
	}
	if len(tracked) != 0 {
		if tracked[len(tracked)-1] != 0 {
			return head, false, fmt.Errorf("malformed tracked Git paths")
		}
		return head, false, nil
	}
	untracked, err := gitRead(root, "ls-files", "--others", "--exclude-standard", "-z", "--", ".")
	if err != nil {
		return head, false, err
	}
	if len(untracked) == 0 {
		return head, true, nil
	}
	if untracked[len(untracked)-1] != 0 {
		return head, false, fmt.Errorf("malformed untracked Git paths")
	}
	for _, raw := range bytes.Split(untracked[:len(untracked)-1], []byte{0}) {
		path := string(raw)
		if path == "" || filepath.IsAbs(path) || strings.Contains(path, "\\") || path == ".." || strings.HasPrefix(path, "../") || strings.Contains(path, "/../") || strings.HasPrefix(path, "./") {
			return head, false, fmt.Errorf("unsafe untracked Git path %q", path)
		}
		if path != ".goal-verdict" && path != ".goal-verdict.json" && !strings.HasPrefix(path, ".woody/") {
			return head, false, nil
		}
	}
	return head, true, nil
}

func parseMachineVerdictTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("cached machine verdict has no timestamp")
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if at, err := time.Parse(layout, raw); err == nil {
			return at, nil
		}
	}
	if at, err := time.ParseInLocation("2006-01-02T15:04:05", raw, time.Local); err == nil {
		return at, nil
	}
	return time.Time{}, fmt.Errorf("cached machine verdict has invalid timestamp %q", raw)
}

func machineVerdictFreshness(root string, mv machineVerdict, now time.Time) error {
	at, err := parseMachineVerdictTime(mv.At)
	if err != nil {
		return err
	}
	if now.Sub(at) > machineVerdictMaxAge {
		return fmt.Errorf("cached machine verdict is stale: age %s exceeds %s",
			now.Sub(at).Round(time.Second), machineVerdictMaxAge)
	}
	if at.Sub(now) > 5*time.Minute {
		return fmt.Errorf("cached machine verdict timestamp is in the future: %s", mv.At)
	}
	head, clean, err := judgeSourceIdentity(root)
	if err != nil {
		return fmt.Errorf("cached machine verdict source is unproven: %w", err)
	}
	if head != "" {
		if mv.GitHead == "" {
			return fmt.Errorf("cached machine verdict has no git_head for repository HEAD %s", head)
		}
		if mv.GitHead != head {
			return fmt.Errorf("cached machine verdict is stale: git_head %s != current HEAD %s", mv.GitHead, head)
		}
		if !mv.SourceClean || !clean {
			return fmt.Errorf("cached machine verdict source is dirty or unproven")
		}
	}
	return nil
}
