package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func withPonytailFixture(t *testing.T, body string) {
	t.Helper()
	old := loadPonytailSkill
	loadPonytailSkill = func() (ponytailSkill, error) {
		return ponytailSkill{Version: "4.10.0", Path: "fixture/SKILL.md", Body: body}, nil
	}
	t.Cleanup(func() { loadPonytailSkill = old })
}

func TestPonytailPromptUsesInstalledVendorSkillAndFullMode(t *testing.T) {
	withPonytailFixture(t, "# Ponytail\nVENDOR_RULE_7_UNIQUE")
	got := ponytailPrompt("TASK")
	for _, want := range []string{
		"@ponytail full",
		"installed plugin ponytail@ponytail v4.10.0",
		"ACTIVE MODE: full",
		"VENDOR_RULE_7_UNIQUE",
		"AIRWORKER TASK:\nTASK",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q: %s", want, got)
		}
	}
}

func TestReadInstalledPonytailSkillUsesEnabledVendorPluginAndNewestVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	config := "[plugins.\"ponytail@ponytail\"]\nenabled = true\n\n[marketplaces.ponytail]\nsource_type = \"git\"\nsource = \"https://github.com/DietrichGebert/ponytail.git\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	for version, body := range map[string]string{"4.9.0": "OLD", "4.10.0": "NEW_VENDOR_RULE"} {
		path := filepath.Join(home, "plugins", "cache", "ponytail", "ponytail", version, "skills", "ponytail", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	skill, err := readInstalledPonytailSkill()
	if err != nil {
		t.Fatal(err)
	}
	if skill.Version != "4.10.0" || skill.Body != "NEW_VENDOR_RULE" {
		t.Fatalf("skill=%+v", skill)
	}
}

func TestReadInstalledPonytailSkillRejectsUnapprovedSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	config := "[plugins.\"ponytail@ponytail\"]\nenabled = true\n\n[marketplaces.ponytail]\nsource = \"https://example.invalid/fork.git\"\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readInstalledPonytailSkill(); err == nil || !strings.Contains(err.Error(), "approved vendor source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCodexExecutorAndPlannerUsePonytail(t *testing.T) {
	withPonytailFixture(t, "VENDOR_CONTEXT")
	runner := runnerSpec{Kind: "codex", Model: "gpt-6-sol", Effort: "low"}
	execArgs := codexArgsForSandbox("X:\\product", "EXEC_TASK", runner, "workspace-write")
	if got := execArgs[len(execArgs)-1]; !strings.Contains(got, "VENDOR_CONTEXT") || !strings.Contains(got, "AIRWORKER TASK:\nEXEC_TASK") {
		t.Fatalf("executor prompt missing Ponytail vendor context: %q", got)
	}
	planArgs := plannerCodexArgs("X:\\product", "PLAN_TASK", runner)
	if got := planArgs[len(planArgs)-1]; !strings.Contains(got, "VENDOR_CONTEXT") || !strings.Contains(got, "AIRWORKER TASK:\nPLAN_TASK") {
		t.Fatalf("planner prompt missing Ponytail vendor context: %q", got)
	}
}

func TestSemanticCodexUsesPonytail(t *testing.T) {
	withPonytailFixture(t, "VENDOR_CONTEXT")
	cmd := semanticCodexCommand("codex", "X:\\product", "JUDGE_TASK", runnerSpec{Kind: "codex", Effort: "low"}, "X:\\schema.json")
	raw, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "VENDOR_CONTEXT") || !strings.Contains(string(raw), "AIRWORKER TASK:\nJUDGE_TASK") {
		t.Fatalf("semantic prompt missing Ponytail: %q", string(raw))
	}
}

func TestInvokeCodexFailsClosedWhenPonytailUnavailable(t *testing.T) {
	oldLoad := loadPonytailSkill
	loadPonytailSkill = func() (ponytailSkill, error) { return ponytailSkill{}, errors.New("missing") }
	t.Cleanup(func() { loadPonytailSkill = oldLoad })
	oldCommand := runnerCommand
	started := false
	runnerCommand = func(_ string, _ ...string) *exec.Cmd {
		started = true
		return exec.Command("cmd", "/c", "exit", "0")
	}
	t.Cleanup(func() { runnerCommand = oldCommand })
	res := (&loopCtx{Root: t.TempDir()}).invokeCodex("codex", "TASK", runnerSpec{Kind: "codex"}, "p")
	if started {
		t.Fatal("Codex started without a proven Ponytail skill")
	}
	if res.Subtype != "invalid_runner_config" || !strings.Contains(res.Detail, "ponytail") {
		t.Fatalf("result=%+v", res)
	}
}
