package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const machineVerdictMaxAge = 6 * time.Hour

func currentGitHead(root string) string {
	cmd := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
	if head := currentGitHead(root); head != "" {
		if strings.TrimSpace(mv.GitHead) == "" {
			return fmt.Errorf("cached machine verdict has no git_head for repository HEAD %s", head)
		}
		if !strings.EqualFold(strings.TrimSpace(mv.GitHead), head) {
			return fmt.Errorf("cached machine verdict is stale: git_head %s != current HEAD %s", mv.GitHead, head)
		}
	}
	return nil
}
