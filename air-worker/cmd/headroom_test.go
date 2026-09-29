package main

import (
	"os/exec"
	"strings"
	"testing"
)

func envValue(env []string, key string) string {
	prefix := strings.ToUpper(key) + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.ToUpper(env[i]), prefix) {
			return env[i][len(key)+1:]
		}
	}
	return ""
}

func TestClaudeRunnerEnvForcesHeadroom(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fixture-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://bypass.invalid")
	env, took := runnerEnv()
	if took {
		t.Fatal("process token is already present; runnerEnv must not report user-env injection")
	}
	if got := envValue(env, "ANTHROPIC_BASE_URL"); got != headroomBaseURL {
		t.Fatalf("ANTHROPIC_BASE_URL=%q want=%q", got, headroomBaseURL)
	}
	if got := envValue(env, "CLAUDE_CODE_OAUTH_TOKEN"); got != "fixture-token" {
		t.Fatalf("process OAuth token was not preserved")
	}
}

func TestApplyClaudeRunnerEnvRejectsDirectBaseURLBypass(t *testing.T) {
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "fixture-token")
	t.Setenv("ANTHROPIC_BASE_URL", "https://bypass.invalid")
	cmd := exec.Command("claude", "--version")
	_ = applyClaudeRunnerEnv(cmd)
	if got := envValue(cmd.Env, "ANTHROPIC_BASE_URL"); got != headroomBaseURL {
		t.Fatalf("Claude command bypassed Headroom: %q", got)
	}
}
