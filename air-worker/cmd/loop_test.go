package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Шаг 62, критерий К31: шаг СО СВОЕЙ КОМАНДОЙ (ступень script) закрывается КОДОМ ЭТОЙ
// КОМАНДЫ, а не двигателем цели.
//
// Дефект нашла AIR-ENV-002 14.09.2026: команда шага script отработала кодом 0, напечатала
// «НОРМА» — а петля всё равно подняла ступень до haiku, потому что решение о шаге
// принималось расстоянием ПРОДУКТОВОГО судьи, которому команда шага ничем не обязана: он
// мог не увидеть в её работе вообще ничего. Haiku получил уже пройденный шаг, ничего не
// запустил и сочинил отчёт «готово».
//
// Мера К31 — go test: успешный шаг script закрыт в плане без подъёма ступени, неуспешный
// поднимает ступень (движение по-прежнему меряет двигатель цели — путь уже проверен
// TestДвижениеПетли и соседями в movement_test.go).

func TestLoopStopsAtOpenGate(t *testing.T) {
	steps := []workStep{
		{Index: 1, Num: "1", Title: "completed", Done: true},
		{Index: 2, Num: "2", Title: "release approval", Gate: true},
		{Index: 3, Num: "3", Title: "must not run"},
	}

	step, state := firstOpenPlanStep(steps)
	if state != openPlanGate || step.Num != "2" {
		t.Fatalf("first open plan item = %+v (%v), want LPR gate 2", step, state)
	}
	if work, ok := firstOpenWorkStep(steps); ok {
		t.Fatalf("selected work after an open LPR gate: %+v", work)
	}

	steps[1].Done = true
	step, state = firstOpenPlanStep(steps)
	if state != openPlanWork || step.Num != "3" {
		t.Fatalf("after gate closure first open item = %+v (%v), want work 3", step, state)
	}
}

func TestScriptStepCloses(t *testing.T) {
	cases := []struct {
		name string
		kind string
		ok   bool
		want bool
	}{
		{"script и код 0 — закрывает шаг сам", "script", true, true},
		{"script и код не 0 — прежний путь двигателя", "script", false, false},
		{"не script, код 0 — закрытие не его дело", "claude", true, false},
		{"не script, код не 0 — закрытие не его дело", "codex", false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := scriptStepCloses(c.kind, c.ok); got != c.want {
				t.Errorf("scriptStepCloses(%q, %v) = %v, ожидалось %v", c.kind, c.ok, got, c.want)
			}
		})
	}
}

func writeLoopPlan(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "PLAN.md")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestЗакрытиеШагаScriptВТаблицеНеТрогаетОстальное(t *testing.T) {
	head := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n"
	plan := writeLoopPlan(t, head+
		"| 1 | Проверка окружения :: exit 0 | script | автоматика |\n"+
		"| 2 | Следующий шаг | haiku | тест |\n")

	step, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok || step.Num != "1" || step.Cmd != "exit 0" {
		t.Fatalf("до закрытия: ожидался шаг 1 с командой «exit 0», получено %+v (%v)", step, ok)
	}

	if err := closeStepInPlan(plan, step); err != nil {
		t.Fatalf("closeStepInPlan: %v", err)
	}

	raw, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := head +
		"| ~~1~~ | Проверка окружения :: exit 0 | script | автоматика |\n" +
		"| 2 | Следующий шаг | haiku | тест |\n"
	if string(raw) != want {
		t.Fatalf("план после закрытия расходится с ожиданием:\nполучено:\n%s\nождалось:\n%s", raw, want)
	}

	steps := readPlanSteps(plan)
	if !steps[0].Done {
		t.Error("шаг 1 не отмечен закрытым")
	}
	next, ok := firstOpenWorkStep(steps)
	if !ok || next.Num != "2" || next.Tier != "haiku" {
		t.Fatalf("после закрытия шага 1 ожидался открытый шаг 2 на haiku, получено %+v (%v)", next, ok)
	}
}

func TestЗакрытиеШагаScriptВСпискеНеТрогаетОстальное(t *testing.T) {
	plan := writeLoopPlan(t, "- [ ] script Проверка окружения :: exit 0\n- [ ] haiku Следующий шаг\n")

	step, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok || step.Cmd != "exit 0" {
		t.Fatalf("до закрытия: неверный разбор шага 1, получено %+v (%v)", step, ok)
	}
	if err := closeStepInPlan(plan, step); err != nil {
		t.Fatalf("closeStepInPlan: %v", err)
	}

	raw, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := "- [x] script Проверка окружения :: exit 0\n- [ ] haiku Следующий шаг\n"
	if string(raw) != want {
		t.Fatalf("план после закрытия расходится с ожиданием:\nполучено: %q\nождалось: %q", string(raw), want)
	}

	next, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok || next.Tier != "haiku" {
		t.Fatalf("ожидался открытый шаг haiku, получено %+v (%v)", next, ok)
	}
}

func TestЗакрытиеШагаScriptСчётСовпадаетСReadPlanStepsПриГейте(t *testing.T) {
	// Гейт занимает свой номер по счёту и блокирует следующие шаги до решения ЛПР.
	head := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n"
	plan := writeLoopPlan(t, head+
		"| 1 | Релиз | — | гейт: ЛПР |\n"+
		"| 2 | Проверка окружения :: exit 0 | script | автоматика |\n"+
		"| 3 | Следующий шаг | haiku | тест |\n")

	gate, state := firstOpenPlanStep(readPlanSteps(plan))
	if state != openPlanGate || gate.Num != "1" {
		t.Fatalf("ожидался открытый гейт 1, получено %+v (%v)", gate, state)
	}
	if step, ok := firstOpenWorkStep(readPlanSteps(plan)); ok {
		t.Fatalf("рабочий шаг выбран до закрытия гейта: %+v", step)
	}

	// Имитируем отдельное решение ЛПР, а не закрытие петлёй.
	if err := os.WriteFile(plan, []byte(head+
		"| ~~1~~ | Релиз | — | гейт: ЛПР |\n"+
		"| 2 | Проверка окружения :: exit 0 | script | автоматика |\n"+
		"| 3 | Следующий шаг | haiku | тест |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	step, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok || step.Index != 2 || step.Num != "2" {
		t.Fatalf("после решения ЛПР ожидался рабочий шаг 2, получено %+v (%v)", step, ok)
	}
	if err := closeStepInPlan(plan, step); err != nil {
		t.Fatalf("closeStepInPlan: %v", err)
	}

	steps := readPlanSteps(plan)
	if !steps[0].Done || !steps[1].Done {
		t.Fatalf("гейт и шаг 2 должны оставаться закрыты: %+v", steps)
	}
	next, ok := firstOpenWorkStep(steps)
	if !ok || next.Num != "3" {
		t.Fatalf("ожидался открытый шаг 3, получено %+v (%v)", next, ok)
	}
}

func TestЗакрытиеШагаScriptСохраняетCRLF(t *testing.T) {
	head := "| № | Шаг | Ступень | Судья |\r\n|---|---|---|---|\r\n"
	dir := t.TempDir()
	plan := filepath.Join(dir, "PLAN.md")
	body := head + "| 1 | Проверка :: exit 0 | script | автоматика |\r\n"
	if err := os.WriteFile(plan, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	step, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok {
		t.Fatal("шаг 1 не найден")
	}
	if err := closeStepInPlan(plan, step); err != nil {
		t.Fatalf("closeStepInPlan: %v", err)
	}

	raw, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := head + "| ~~1~~ | Проверка :: exit 0 | script | автоматика |\r\n"
	if string(raw) != want {
		t.Fatalf("план после закрытия (CRLF) расходится с ожиданием:\nполучено: %q\nождалось: %q", string(raw), want)
	}
	if strings.Contains(string(raw), "\n") && !strings.Contains(string(raw), "\r\n") {
		t.Fatal("перевод строки файла обязан остаться CRLF")
	}
}

func TestЗакрытиеПоследнегоШагаScriptНеОставляетОткрытых(t *testing.T) {
	head := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n"
	plan := writeLoopPlan(t, head+"| 1 | Проверка окружения :: exit 0 | script | автоматика |\n")

	step, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok {
		t.Fatal("шаг 1 не найден")
	}
	if err := closeStepInPlan(plan, step); err != nil {
		t.Fatalf("closeStepInPlan: %v", err)
	}
	if _, ok := firstOpenWorkStep(readPlanSteps(plan)); ok {
		t.Fatal("после закрытия единственного шага открытых остаться не должно")
	}
}

func TestЗакрытиеШагаОтказываетНаЧужомИндексе(t *testing.T) {
	head := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n"
	plan := writeLoopPlan(t, head+"| 1 | Проверка окружения :: exit 0 | script | автоматика |\n")
	fake := workStep{Index: 5, Num: "5", Title: "5. Чужой шаг"}
	if err := closeStepInPlan(plan, fake); err == nil {
		t.Fatal("несуществующий по счёту индекс шага обязан быть отказом, а не тихим пропуском")
	}
}

// TestRunScriptStepOkПоКодуКоманды — «код этой команды» буквально: без реальных вызовов
// моделей и без сети, локальный powershell.exe с известным кодом возврата.
func TestRunScriptStepOkПоКодуКоманды(t *testing.T) {
	c := &loopCtx{Root: t.TempDir()}

	ok := c.runScriptStep(workStep{Index: 1, Title: "норма", Tier: "script", Cmd: "exit 0"})
	if !ok.Ok {
		t.Fatalf("команда «exit 0» обязана дать Ok=true, получено %+v", ok)
	}
	if ok.Cost == nil || *ok.Cost != 0 || ok.Turns == nil || *ok.Turns != 0 {
		t.Errorf("у script расход и ходы известны и равны нулю, получено cost=%v turns=%v", ok.Cost, ok.Turns)
	}

	bad := c.runScriptStep(workStep{Index: 2, Title: "не норма", Tier: "script", Cmd: "exit 1"})
	if bad.Ok {
		t.Fatalf("команда «exit 1» обязана дать Ok=false, получено %+v", bad)
	}

	noCmd := c.runScriptStep(workStep{Index: 3, Title: "без команды", Tier: "script"})
	if noCmd.Subtype != "no_command" || noCmd.Ok {
		t.Fatalf("шаг script без команды обязан отвечать no_command и Ok=false, получено %+v", noCmd)
	}
}

// TestШагScriptКодНольЗакрываетБезПодъёмаСтупени — К31, положительный случай, той же
// последовательностью действий, что и cmdLoop: команда шага, решение scriptStepCloses,
// closeStepInPlan, следующий шаг из плана на диске.
func TestШагScriptКодНольЗакрываетБезПодъёмаСтупени(t *testing.T) {
	head := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n"
	plan := writeLoopPlan(t, head+
		"| 1 | Проверка окружения :: exit 0 | script | автоматика |\n"+
		"| 2 | Следующий шаг | haiku | тест |\n")

	c := &loopCtx{Root: filepath.Dir(plan)}
	step, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok {
		t.Fatal("шаг 1 не найден")
	}

	r := c.runScriptStep(step)
	if !r.Ok {
		t.Fatalf("команда «exit 0» обязана дать Ok=true, получено %+v", r)
	}
	if !scriptStepCloses("script", r.Ok) {
		t.Fatal("К31: код 0 обязан закрывать шаг без подъёма ступени")
	}
	if err := closeStepInPlan(plan, step); err != nil {
		t.Fatalf("closeStepInPlan: %v", err)
	}

	steps := readPlanSteps(plan)
	if !steps[0].Done {
		t.Error("шаг 1 обязан быть закрыт в плане")
	}
	next, ok := firstOpenWorkStep(steps)
	if !ok || next.Num != "2" || next.Tier != "haiku" {
		t.Fatalf("после кода 0 ожидался открытый шаг 2 на его собственной ступени haiku, получено %+v (%v)",
			next, ok)
	}
}

// TestШагScriptКодНеНольОставляетШагОткрытым — К31, отрицательный случай: команда провалена,
// closeStepInPlan не зовётся вовсе (как в cmdLoop, где scriptStepCloses стоит перед ним
// стражем), план не меняется, шаг остаётся первым открытым — движение и подъём ступени
// дальше решает двигатель цели прежним путём (movement_test.go).
func TestШагScriptКодНеНольОставляетШагОткрытым(t *testing.T) {
	head := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n"
	plan := writeLoopPlan(t, head+
		"| 1 | Проверка окружения :: exit 1 | script | автоматика |\n"+
		"| 2 | Следующий шаг | haiku | тест |\n")

	c := &loopCtx{Root: filepath.Dir(plan)}
	step, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok {
		t.Fatal("шаг 1 не найден")
	}
	before, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}

	r := c.runScriptStep(step)
	if r.Ok {
		t.Fatalf("команда «exit 1» обязана дать Ok=false, получено %+v", r)
	}
	if scriptStepCloses("script", r.Ok) {
		t.Fatal("К31: код не 0 не должен закрывать шаг сам — решает двигатель цели")
	}
	// cmdLoop зовёт closeStepInPlan только когда scriptStepCloses вернул true — здесь этого
	// не происходит, план обязан остаться нетронутым.
	after, err := os.ReadFile(plan)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("план не должен меняться от неуспешной команды шага")
	}

	still, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok || !sameStep(still, step) {
		t.Fatalf("шаг обязан остаться первым открытым до движения двигателя цели, получено %+v (%v)", still, ok)
	}
}
