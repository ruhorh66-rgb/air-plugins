package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeReadyToolsFixture(t *testing.T, includeSection bool, homemade bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "goal"), 0o755); err != nil {
		t.Fatal(err)
	}
	step := "Интегрировать RAGFlow через официальный ragflow-sdk"
	if homemade {
		step = "Написать свой HTTP-клиент RAGFlow через curl"
	}
	plan := "**Ц1.** external integration is controlled\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | integration ready | факт `f01` |\n\n"
	if includeSection {
		plan += "## Готовые средства\n\n" +
			"| Инструмент | Штатное средство | Версия | Покрытие |\n|---|---|---|---|\n" +
			"| RAGFlow | официальный ragflow-sdk API | 0.27.2 | dataset/document/chunks/retrieve |\n\n"
	}
	plan += "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | " + step + " | `terra:medium` | К1 |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "goal", "checklist.json"), []byte(`{"items":[{"id":"f01","status":"completed"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte(`{"plan":"PLAN.md","judge":{"checklist":"goal/checklist.json","min_facts":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestValidateRequiresReadyToolsForExternalIntegration(t *testing.T) {
	root := writeReadyToolsFixture(t, false, false)
	report, code := validateProduct(root)
	if code != 1 || !strings.Contains(strings.Join(report.Problems, "\n"), "Готовые средства") {
		t.Fatalf("missing ready-tools section was not rejected: code=%d problems=%v", code, report.Problems)
	}
	if code := cmdGoals([]string{"-product", root, "-json"}); code != 1 {
		t.Fatalf("goals diverged from validate on missing ready tools: %d", code)
	}

	root = writeReadyToolsFixture(t, true, false)
	report, code = validateProduct(root)
	if code != 0 || len(report.Problems) != 0 {
		t.Fatalf("valid ready-tools section rejected: code=%d problems=%v", code, report.Problems)
	}
}

func TestValidateRejectsMalformedReadyToolsSection(t *testing.T) {
	root := writeReadyToolsFixture(t, true, false)
	path := filepath.Join(root, "PLAN.md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bad := strings.Replace(string(raw),
		"| RAGFlow | официальный ragflow-sdk API | 0.27.2 | dataset/document/chunks/retrieve |",
		"| RAGFlow | официальный ragflow-sdk API | | dataset/document/chunks/retrieve |", 1)
	if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	report, code := validateProduct(root)
	if code != 1 || !strings.Contains(strings.Join(report.Problems, "\n"), "неполную строку") {
		t.Fatalf("malformed ready-tools row passed: code=%d problems=%v", code, report.Problems)
	}
}

func TestPlanLintWarnsOnHomemadeControlWhenReadyToolExists(t *testing.T) {
	root := writeReadyToolsFixture(t, true, true)
	report, err := planLint(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, warning := range report.Warnings {
		if warning.Rule == "AW-PLAN-LINT-02" && warning.Step == "1" {
			found = true
		}
	}
	if !found {
		t.Fatalf("homemade client was not named by lint: %#v", report.Warnings)
	}
}
