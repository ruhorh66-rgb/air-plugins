package main

import (
	"strings"
	"testing"
)

func TestCodexArgsEnvAndCapabilities(t *testing.T) {
	args := strings.Join(codexArgs(`X:\\product`, "task", runnerSpec{Model: "gpt-test", Effort: "medium"}), " ")
	if !strings.Contains(args, "workspace-write") || strings.Contains(args, "read-only") {
		t.Fatalf("wrong sandbox args: %s", args)
	}
	if !strings.Contains(args, "gpt-test") || !strings.Contains(args, "model_reasoning_effort=medium") {
		t.Fatalf("model/effort lost: %s", args)
	}
	env := codexEnv([]string{"PATH=x", "CLAUDE_X=value", "ANTHROPIC_X=value", "CODEX_HOME=y"})
	joined := strings.Join(env, "|")
	if strings.Contains(joined, "CLAUDE_") || strings.Contains(joined, "ANTHROPIC_") {
		t.Fatalf("foreign vendor env leaked: %s", joined)
	}
	if !strings.Contains(joined, "PATH=x") || !strings.Contains(joined, "CODEX_HOME=y") {
		t.Fatalf("required env lost: %s", joined)
	}
	caps := strings.Join(runnerCapabilityLines("codex"), " ")
	if !strings.Contains(caps, "workspace-write") || !strings.Contains(caps, "local tests") {
		t.Fatalf("Codex capabilities not declared: %s", caps)
	}
}
