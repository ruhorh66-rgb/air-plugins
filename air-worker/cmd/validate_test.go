package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeValidationGoProduct(t *testing.T, missingSelector bool) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := "package fixture\n\nimport \"testing\"\n\nfunc TestKnown(t *testing.T) {}\n"
	if err := os.WriteFile(filepath.Join(root, "fixture_test.go"), []byte(tests), 0o644); err != nil {
		t.Fatal(err)
	}
	selector := "TestKnown"
	if missingSelector {
		selector = "TestUnknownFromFeedbackA2405264"
	}
	plan := "**Ц1.** selector is measurable\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | selected test exists | проверка `unit` · тест `" + selector + "` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | implement | `script` | К1: selector |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := `{"plan":"PLAN.md","judge":{"checks":[{"name":"unit","command":"go","args":["test","."],"select":"-run ^{}$"}]}}`
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestValidateRejectsUnknownGoSelectorBeforeJudge(t *testing.T) {
	root := writeValidationGoProduct(t, true)
	report, code := validateProduct(root)
	if code != 1 {
		t.Fatalf("validate code=%d report=%#v", code, report)
	}
	joined := strings.Join(report.Problems, "\n")
	if !strings.Contains(joined, `тест "TestUnknownFromFeedbackA2405264" не существует`) {
		t.Fatalf("feedback a2405264 not reproduced: %s", joined)
	}
	if report.SelectorBatches != 1 {
		t.Fatalf("selectors were not batched by check: %d", report.SelectorBatches)
	}
	if code := cmdGoals([]string{"-product", root, "-json"}); code != 1 {
		t.Fatalf("goals diverged from validate for missing selector: %d", code)
	}
}

func TestValidateAcceptsKnownGoSelector(t *testing.T) {
	root := writeValidationGoProduct(t, false)
	report, code := validateProduct(root)
	if code != 0 || len(report.Problems) != 0 {
		t.Fatalf("known selector rejected: code=%d problems=%v", code, report.Problems)
	}
	if report.SelectorBatches != 1 {
		t.Fatalf("expected one selector batch, got %d", report.SelectorBatches)
	}
	if code := cmdGoals([]string{"-product", root, "-json"}); code != 0 {
		t.Fatalf("goals rejected same valid product: %d", code)
	}
}

func TestValidateScriptSelectorRequiresStaticProof(t *testing.T) {
	root := t.TempDir()
	script := "param([string]$Select)\nif ($Select -eq 'KnownCase') { exit 0 }\nexit 1\n"
	if err := os.WriteFile(filepath.Join(root, "check.ps1"), append(utf8BOM, []byte(script)...), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "**Ц1.** script selector\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | known | проверка `ps` · тест `KnownCase` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | work | `script` | К1 |\n"
	_ = os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644)
	cfg := `{"plan":"PLAN.md","judge":{"checks":[{"name":"ps","script":"check.ps1","select":"-Select {}"}]}}`
	_ = os.WriteFile(filepath.Join(root, "run-config.json"), []byte(cfg), 0o644)

	if report, code := validateProduct(root); code != 0 {
		t.Fatalf("literal selector proof rejected: code=%d problems=%v", code, report.Problems)
	}

	plan = strings.Replace(plan, "KnownCase", "NotInScript", 1)
	_ = os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644)
	report, code := validateProduct(root)
	if code != 1 || !strings.Contains(strings.Join(report.Problems, "\n"), "selector") {
		t.Fatalf("missing script selector not rejected: code=%d problems=%v", code, report.Problems)
	}

	var raw map[string]any
	if err := json.Unmarshal([]byte(cfg), &raw); err != nil {
		t.Fatal(err)
	}
	raw["judge"].(map[string]any)["checks"].([]any)[0].(map[string]any)["selectors"] = []any{"NotInScript"}
	b, _ := json.Marshal(raw)
	_ = os.WriteFile(filepath.Join(root, "run-config.json"), b, 0o644)
	if report, code := validateProduct(root); code != 0 {
		t.Fatalf("explicit selector catalog rejected: code=%d problems=%v", code, report.Problems)
	}
}

func TestValidateRejectsNULAndMissingJudgeFile(t *testing.T) {
	root := t.TempDir()
	plan := "**Ц1.** valid\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | fact | факт `f01` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | work | `script` | К1 |\n"
	_ = os.WriteFile(filepath.Join(root, "PLAN.md"), append([]byte(plan), 0), 0o644)
	_ = os.WriteFile(filepath.Join(root, "run-config.json"), []byte(`{"plan":"PLAN.md","judge":{"checklist":"missing.json","min_facts":1}}`), 0o644)
	report, code := validateProduct(root)
	if code != 1 || !strings.Contains(strings.Join(report.Problems, "\n"), "NUL") {
		t.Fatalf("NUL plan not rejected: code=%d problems=%v", code, report.Problems)
	}

	_ = os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644)
	report, code = validateProduct(root)
	joined := strings.Join(report.Problems, "\n")
	if code != 1 || !strings.Contains(joined, "judge.checklist отсутствует") {
		t.Fatalf("missing checklist not rejected: code=%d problems=%v", code, report.Problems)
	}
}

func TestValidateRejectsInvalidSelectorTemplate(t *testing.T) {
	root := writeValidationGoProduct(t, false)
	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.Judge.Checks[0].Select = "-run literal"
	b, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(root, "run-config.json"), b, 0o644)
	report, code := validateProduct(root)
	if code != 1 || !strings.Contains(strings.Join(report.Problems, "\n"), "select не содержит {}") {
		t.Fatalf("invalid select template not rejected: code=%d problems=%v", code, report.Problems)
	}
}

func TestValidateAllowsSelectorPromisedByNamedOpenStep(t *testing.T) {
	root := writeValidationGoProduct(t, true)
	planPath := filepath.Join(root, "PLAN.md")
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Replace(
		string(raw),
		"| 1 | implement | `script` | К1: selector |",
		"| 1 | implement TestUnknownFromFeedbackA2405264 | `script` | К1: selector TestUnknownFromFeedbackA2405264 |",
		1,
	)
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	report, code := validateProduct(root)
	if code != 0 {
		t.Fatalf("open step promise was not accepted: code=%d problems=%v", code, report.Problems)
	}

	plan = strings.Replace(plan, "| 1 | implement TestUnknownFromFeedbackA2405264 |", "| ~~1~~ | implement TestUnknownFromFeedbackA2405264 |", 1)
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	report, code = validateProduct(root)
	if code != 1 || !strings.Contains(strings.Join(report.Problems, "\n"), "не существует") {
		t.Fatalf("closed step incorrectly excused missing selector: code=%d problems=%v", code, report.Problems)
	}
}

func TestValidateFutureSelectorPromiseMustMatchCriterion(t *testing.T) {
	root := writeValidationGoProduct(t, true)
	planPath := filepath.Join(root, "PLAN.md")
	raw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Replace(
		string(raw),
		"| 1 | implement | `script` | К1: selector |",
		"| 1 | implement TestUnknownFromFeedbackA2405264 | `script` | К2: selector TestUnknownFromFeedbackA2405264 |",
		1,
	)
	if err := os.WriteFile(planPath, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	report, code := validateProduct(root)
	if code != 1 || !strings.Contains(strings.Join(report.Problems, "\n"), "не существует") {
		t.Fatalf("unrelated criterion incorrectly excused missing selector: code=%d problems=%v", code, report.Problems)
	}
}
