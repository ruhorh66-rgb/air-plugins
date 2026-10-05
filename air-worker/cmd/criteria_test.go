package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCriterionK33SelectorGrammar(t *testing.T) {
	m, err := parseCriterionMeasures("проверка `тесты бинарника` · тест `TestX`")
	if err != nil || len(m) != 1 || m[0].Selector != "TestX" {
		t.Fatalf("selector grammar: %+v, %v", m, err)
	}
	if _, err := parseCriterionMeasures("проверка `тесты бинарника` — пояснение прозой"); err == nil {
		t.Fatal("prose in machine measure must be rejected")
	}
}

func TestCriterionK33MissingSelectorFails(t *testing.T) {
	chk := checkSpec{Name: "go", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$"}
	r := runSelectedCheck(".", chk, "TestDefinitelyNotWrittenForAirWorker")
	if r.State != measureFail {
		t.Fatalf("missing test must be factual fail: %+v", r)
	}
}

func TestPowerShellSelectorParameterReachesScript(t *testing.T) {
	shell := "powershell.exe"
	if runtime.GOOS != "windows" {
		shell = "pwsh"
	}
	if _, err := exec.LookPath(shell); err != nil {
		t.Skip(shell + " is unavailable")
	}

	root := t.TempDir()
	script := "param([string]$Select)\nif ($Select -eq 'RunnerWrites' -or $Select -eq '-RunnerWrites') { exit 0 }\nexit 9\n"
	if err := os.WriteFile(filepath.Join(root, "selector.ps1"), append(utf8BOM, []byte(script)...), 0o644); err != nil {
		t.Fatal(err)
	}
	chk := checkSpec{Name: "probe", Script: "selector.ps1", Select: "-Select {}"}
	for _, selector := range []string{"RunnerWrites", "-RunnerWrites"} {
		result := runSelectedCheck(root, chk, selector)
		if result.State != measurePass {
			t.Fatalf("PowerShell selector %q was corrupted: %+v", selector, result)
		}
	}
}
func TestCriterionK19ConfirmedClosure(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN.md")
	text := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| ~~1~~ | done | `sonnet` | К1: factual |\n" +
		"| ~~2~~ | gate | — | гейт: ЛПР |\n"
	if err := os.WriteFile(plan, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := confirmedClosedSteps(plan, nil); got != 1 {
		t.Fatalf("unconfirmed work step counted closed: %d", got)
	}
	if got := confirmedClosedSteps(plan, []string{"К1"}); got != 2 {
		t.Fatalf("confirmed closure not counted: %d", got)
	}
}

func TestCriterionJudgeAffectsVerdictAndDistance(t *testing.T) {
	r := judgeResult{Passed: []string{"base"}, CriteriaFailed: []string{"К1 — missing"}}
	if code, _ := verdict(r); code != 1 {
		t.Fatalf("criterion fail code=%d", code)
	}
	if d := distanceOf(1, r); d == nil || *d != 1 {
		t.Fatalf("criterion distance=%v", d)
	}
	r = judgeResult{CriteriaUnknown: []string{"К1 — unknown"}}
	if code, _ := verdict(r); code != 2 {
		t.Fatalf("criterion unknown code=%d", code)
	}
}

func TestJudgeSelectorInventoryBatchesOneGoListPerCheck(t *testing.T) {
	old := judgeGoListSelectors
	defer func() { judgeGoListSelectors = old }()
	calls := 0
	judgeGoListSelectors = func(root string, chk checkSpec, selectors []string) (map[string]bool, error) {
		calls++
		if chk.Name != "unit" || len(selectors) != 2 {
			t.Fatalf("inventory request=%s %#v", chk.Name, selectors)
		}
		return map[string]bool{"TestOne": true, "TestTwo": true}, nil
	}
	cfg := runConfig{Judge: judgeSpec{Checks: []checkSpec{{
		Name: "unit", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$",
	}}}}
	g := planGoals{Criteria: []planCriterion{
		{ID: "К1", Goal: "Ц1", Measure: "проверка `unit` · тест `TestOne`"},
		{ID: "К2", Goal: "Ц1", Measure: "проверка `unit` · тест `TestTwo`"},
	}}
	inventory := buildJudgeSelectorInventory(".", cfg, g)
	if calls != 1 {
		t.Fatalf("go selector inventory calls=%d want 1", calls)
	}
	base := judgeResult{Passed: []string{"unit"}, SelectorInventory: inventory}
	passed, failed, gated, unknown, _ := evaluatePlanCriteriaObserved(".", cfg, g, base)
	if len(passed) != 2 || len(failed) != 0 || len(gated) != 0 || len(unknown) != 0 {
		t.Fatalf("criteria pass=%v fail=%v gated=%v unknown=%v", passed, failed, gated, unknown)
	}
}

func TestJudgeSelectorInventoryMarksMissingWithoutPerSelectorProbe(t *testing.T) {
	old := judgeGoListSelectors
	defer func() { judgeGoListSelectors = old }()
	calls := 0
	judgeGoListSelectors = func(root string, chk checkSpec, selectors []string) (map[string]bool, error) {
		calls++
		return map[string]bool{"TestOne": true}, nil
	}
	cfg := runConfig{Judge: judgeSpec{Checks: []checkSpec{{
		Name: "unit", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$",
	}}}}
	g := planGoals{Criteria: []planCriterion{
		{ID: "К1", Goal: "Ц1", Measure: "проверка `unit` · тест `TestOne`"},
		{ID: "К2", Goal: "Ц1", Measure: "проверка `unit` · тест `TestMissing`"},
	}}
	inventory := buildJudgeSelectorInventory(".", cfg, g)
	base := judgeResult{Passed: []string{"unit"}, SelectorInventory: inventory}
	passed, failed, _, unknown, _ := evaluatePlanCriteriaObserved(".", cfg, g, base)
	if calls != 1 || len(passed) != 1 || len(failed) != 1 || len(unknown) != 0 {
		t.Fatalf("calls=%d pass=%v fail=%v unknown=%v", calls, passed, failed, unknown)
	}
}
