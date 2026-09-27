package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePlanLintFixture(t *testing.T, tier, title string) string {
	t.Helper()
	root := t.TempDir()
	plan := "**Ц1.** lint fixture\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | state known | проверка `probe` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | " + title + " | `" + tier + "` | К1: probe |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := `{"plan":"PLAN.md","judge":{"checks":[{"name":"probe","command":"go","args":["version"]}]}}`
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestPlanLintWarnsOnlyOnDeterministicReconModelStep(t *testing.T) {
	root := writePlanLintFixture(t, "terra:medium", "Найди текущее состояние")
	report, err := planLint(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 1 || report.Warnings[0].Step != "1" || report.Warnings[0].Rule != "AW-PLAN-LINT-01" {
		t.Fatalf("expected one recon warning: %#v", report.Warnings)
	}

	for name, fixture := range map[string]struct{ tier, title string }{
		"script":      {"script", "Найди текущее состояние"},
		"coding":      {"terra:medium", "Реализуй обработчик"},
		"file-target": {"terra:medium", "Прочитай config.json"},
	} {
		t.Run(name, func(t *testing.T) {
			root := writePlanLintFixture(t, fixture.tier, fixture.title)
			report, err := planLint(root)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Warnings) != 0 {
				t.Fatalf("false positive: %#v", report.Warnings)
			}
		})
	}
}

func TestPlanLintRequiresExistingCheckMeasure(t *testing.T) {
	root := writePlanLintFixture(t, "terra:medium", "Сверь текущее состояние")
	raw, _ := os.ReadFile(filepath.Join(root, "run-config.json"))
	var cfg map[string]any
	_ = json.Unmarshal(raw, &cfg)
	cfg["judge"].(map[string]any)["checks"] = []any{}
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(root, "run-config.json"), b, 0o644)

	report, err := planLint(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 0 {
		t.Fatalf("missing check must not be presented as deterministic recon: %#v", report.Warnings)
	}
}

func TestPostToolUsePlanLintReturnsNonBlockingContextOnlyForPlan(t *testing.T) {
	root := writePlanLintFixture(t, "terra:medium", "Прочитай текущее состояние")
	stateDir := t.TempDir()
	session := "plan-lint-session"
	seedActiveLearningSession(t, root, stateDir, session)

	planInput, _ := json.Marshal(map[string]any{"file_path": filepath.Join(root, "PLAN.md")})
	res, err := handlePostToolUsePlanLint(hookInput{
		SessionID: session, HookEventName: "PostToolUse", ToolName: "Edit",
		ToolInput: planInput, Cwd: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Block || !strings.Contains(res.Context, "AW-PLAN-LINT-01") || !strings.Contains(res.Context, "non-blocking") {
		t.Fatalf("lint context missing/nonblocking contract broken: %#v", res)
	}

	otherInput, _ := json.Marshal(map[string]any{"file_path": filepath.Join(root, "README.md")})
	res, err = handlePostToolUsePlanLint(hookInput{
		SessionID: session, HookEventName: "PostToolUse", ToolName: "Edit",
		ToolInput: otherInput, Cwd: root,
	})
	if err != nil || res.Context != "" || res.Block {
		t.Fatalf("non-plan edit must be no-op: res=%#v err=%v", res, err)
	}
}

func TestPostToolUsePlanLintFailureIsLifecycleFailOpen(t *testing.T) {
	root := writePlanLintFixture(t, "terra:medium", "Прочитай текущее состояние")
	stateDir := t.TempDir()
	session := "plan-lint-broken-session"
	seedActiveLearningSession(t, root, stateDir, session)
	if err := os.Remove(filepath.Join(root, "PLAN.md")); err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(map[string]any{"file_path": filepath.Join(root, "PLAN.md")})
	_, err := handlePostToolUsePlanLint(hookInput{
		SessionID: session, HookEventName: "PostToolUse", ToolName: "Write",
		ToolInput: input, Cwd: root,
	})
	if err == nil {
		t.Fatal("broken plan must make handler report an error")
	}
	if code := finishHook(classLifecycle, "PostToolUse", session, err.Error()); code != 0 {
		t.Fatalf("lifecycle lint error blocked host: code=%d", code)
	}
}
