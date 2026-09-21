package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planCloseFixture(t *testing.T) (string, []byte, workStep) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "PLAN.md")
	raw := "| № | Шаг | Ступень | Судья |\n| 8a | модель | haiku | К13 |\n| 9 | чужой | haiku | К14 |\n"
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	steps := readPlanSteps(path)
	if len(steps) != 2 {
		t.Fatalf("разбор плана: %#v", steps)
	}
	return path, []byte(raw), steps[0]
}

func TestPlanRowReturnedByteForByteOnNotPass(t *testing.T) {
	for _, tc := range []struct {
		name string
		sem  *semanticRun
		fact bool
	}{
		{"not-proven", &semanticRun{Verdict: semanticVerdict{Verdict: "NOT_PROVEN"}}, true},
		{"drift", &semanticRun{Verdict: semanticVerdict{Verdict: "DRIFT"}}, true},
		{"judge-error", &semanticRun{Error: "timeout"}, true},
		{"factual-fail", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, before, step := planCloseFixture(t)
			if err := os.WriteFile(path, []byte("| ~~8a~~ | испорчено | haiku | К13 |\n| ~~9~~ | чужая правка | haiku | К14 |\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, _, err := reconcileModelStepPlan(path, before, step, tc.fact, tc.sem); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != string(before) {
				t.Fatalf("план изменён при не-PASS: %q", got)
			}
		})
	}
}

func TestExecutorCannotModifyPlan(t *testing.T) {
	path, before, step := planCloseFixture(t)
	if err := os.WriteFile(path, []byte("| ~~8a~~ | испорчено | haiku | К13 |\n| ~~9~~ | чужая правка | haiku | К14 |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	closed, tampered, err := reconcileModelStepPlan(path, before, step, true, &semanticRun{Verdict: semanticVerdict{Verdict: "PASS", StepDone: "yes"}})
	if err != nil || !closed || !tampered {
		t.Fatalf("closed=%v tampered=%v err=%v", closed, tampered, err)
	}
	got, _ := os.ReadFile(path)
	want := strings.Replace(string(before), "| 8a |", "| ~~8a~~ |", 1)
	if string(got) != want {
		t.Fatalf("чужая правка пережила откат: %q", got)
	}
}

func TestEngineClosesModelStepOnPass(t *testing.T) {
	path, before, step := planCloseFixture(t)
	closed, tampered, err := reconcileModelStepPlan(path, before, step, true, &semanticRun{Verdict: semanticVerdict{Verdict: "PASS", StepDone: "yes"}})
	if err != nil || !closed || tampered {
		t.Fatalf("closed=%v tampered=%v err=%v", closed, tampered, err)
	}
	got, _ := os.ReadFile(path)
	want := strings.Replace(string(before), "| 8a |", "| ~~8a~~ |", 1)
	if string(got) != want {
		t.Fatalf("ядро не закрыло строку: %q", got)
	}
}

func TestExecutorPromptDoesNotAskToCloseTheRow(t *testing.T) {
	c := &loopCtx{Root: t.TempDir(), WhatIf: true, MaxTurns: 1}
	step := workStep{Index: 1, Num: "8a", Title: "8a. модель", Tier: "haiku"}
	c.runModelStep(step, "haiku:medium", "", runnerSpec{Kind: "claude", Model: "haiku"})
	files, _ := filepath.Glob(filepath.Join(c.Root, ".woody", "TASK-1-*.md"))
	if len(files) != 1 {
		t.Fatalf("задание не создано")
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(b)
	for _, forbidden := range []string{"Зачеркни номер", "сними зачёркивание"} {
		if strings.Contains(prompt, forbidden) {
			t.Fatalf("задание содержит запрещённую инструкцию %q", forbidden)
		}
	}
	if !strings.Contains(prompt, "Не правь PLAN.md") {
		t.Fatalf("в задании нет запрета править PLAN.md")
	}
}

// Закрытие строки решают критерии самого шага: красные критерии будущих шагов закрытию не мешают.
func TestModelStepFactualPassUsesStepCriteriaNotWholeGoal(t *testing.T) {
	step := workStep{Num: "8a", Judge: "К13: критерий этого шага"}
	whole := &judgeResult{CriteriaPassed: []string{"К13"}, CriteriaFailed: []string{"К14 — будущий шаг красный"}}
	if !modelStepFactualPass(step, true, whole) {
		t.Fatal("критерий шага пройден, красный критерий другого шага не должен мешать закрытию")
	}
	if modelStepFactualPass(step, false, whole) {
		t.Fatal("исполнитель не завершил ход — закрывать нельзя")
	}
	red := &judgeResult{CriteriaFailed: []string{"К13 — критерий этого шага красный"}}
	if modelStepFactualPass(step, true, red) {
		t.Fatal("красный критерий самого шага закрывать не даёт")
	}
	if modelStepFactualPass(step, true, nil) {
		t.Fatal("нет структурированного результата судьи — закрывать нельзя")
	}
}
