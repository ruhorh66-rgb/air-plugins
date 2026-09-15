package main

import (
	"os"
	"path/filepath"
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
