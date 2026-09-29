package main

import (
	"io"
	"strings"
	"testing"
)

func TestPonytailPromptUsesFullMode(t *testing.T) {
	got := ponytailPrompt("TASK")
	if got != "@ponytail full\n\nTASK" {
		t.Fatalf("prompt=%q", got)
	}
	if ponytailPrompt(got) != got {
		t.Fatal("Ponytail invocation must be idempotent")
	}
}

func TestCodexExecutorAndPlannerUsePonytail(t *testing.T) {
	runner := runnerSpec{Kind: "codex", Model: "gpt-6-sol", Effort: "low"}
	execArgs := codexArgsForSandbox("X:\\product", "EXEC_TASK", runner, "workspace-write")
	if got := execArgs[len(execArgs)-1]; !strings.HasPrefix(got, ponytailSkillInvocation+"\n\nEXEC_TASK") {
		t.Fatalf("executor prompt missing Ponytail: %q", got)
	}
	planArgs := plannerCodexArgs("X:\\product", "PLAN_TASK", runner)
	if got := planArgs[len(planArgs)-1]; !strings.HasPrefix(got, ponytailSkillInvocation+"\n\nPLAN_TASK") {
		t.Fatalf("planner prompt missing Ponytail: %q", got)
	}
}

func TestSemanticCodexUsesPonytail(t *testing.T) {
	cmd := semanticCodexCommand("codex", "X:\\product", "JUDGE_TASK", runnerSpec{Kind: "codex", Effort: "low"}, "X:\\schema.json")
	raw, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(raw), ponytailSkillInvocation+"\n\nJUDGE_TASK") {
		t.Fatalf("semantic prompt missing Ponytail: %q", string(raw))
	}
}
