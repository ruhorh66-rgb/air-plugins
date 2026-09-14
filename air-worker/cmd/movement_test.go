package main

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Суждение петли об итерации. Определения движения, регресса и уточнения — двигателя
// цели; петля сравнивает не с предыдущим замером, а с точкой последнего движения.

func TestПоломкаСборкиНеДвижение(t *testing.T) {
	// Живой случай 14.09.2026, ASW, шаг 8а: исполнитель сломал сборку, остаток по судье
	// вырос с 1 до 2. Петля по подписи текста вердикта напечатала «судья сдвинулся».
	move, why := judgeIteration(pp(1, 7, 29), pp(2, 7, 29), false)
	if move != moveRegress {
		t.Fatalf("поломка сборки: ожидался регресс, получено %d (%s)", move, why)
	}
}

func TestСломалПочинилНеДвижение(t *testing.T) {
	// Сравнение с точкой последнего движения, а не с предыдущим замером: иначе починка
	// своей же поломки засчитывалась бы движением, и колебание пряталось бы от застоя.
	ref := pp(1, 7, 29)
	if move, _ := judgeIteration(ref, pp(2, 7, 29), false); move != moveRegress {
		t.Fatalf("сломал: ожидался регресс, получено %d", move)
	}
	if move, why := judgeIteration(ref, pp(1, 7, 29), false); move != moveNone {
		t.Fatalf("починил до исходного: ожидалось «не сдвинулась», получено %d (%s)", move, why)
	}
}

func TestДвижениеПетли(t *testing.T) {
	ref := pp(1, 7, 29)
	if move, _ := judgeIteration(ref, pp(1, 8, 28), false); move != moveForward {
		t.Errorf("закрыт шаг: ожидалось движение, получено %d", move)
	}
	if move, _ := judgeIteration(ref, pp(0, 7, 29), false); move != moveForward {
		t.Errorf("остаток уменьшился: ожидалось движение, получено %d", move)
	}
}

func TestЗакрытиеПриРостеОстаткаНеДвижение(t *testing.T) {
	// Шаг зачёркнут, а судья стал хуже. Одна точка не может быть и движением, и регрессом.
	ref, cur := pp(1, 7, 29), pp(2, 8, 28)
	if moved(ref, cur) {
		t.Fatal("moved: закрытие шага при росте остатка засчитано движением")
	}
	if move, _ := judgeIteration(ref, cur, false); move != moveRegress {
		t.Fatalf("петля: ожидался регресс, получено %d", move)
	}
	// Двигатель по истории застой этим тоже не сбрасывает.
	if stall, _ := countStreaks([]driftPoint{ref, ref}, cur); stall != 2 {
		t.Fatalf("застой по истории: ожидалось 2, получено %d", stall)
	}
	got, reasons := evaluate(cur, d(1), d(28), []driftPoint{ref}, defaultLimits())
	if got != verdictThrottle || !hasRule(reasons, "WORKER-DRIFT-03") {
		t.Fatalf("двигатель: ожидался THROTTLE по WORKER-DRIFT-03, получено %q %v", got, reasons)
	}
}

func TestУточнениеПланаВПетлеОдинРазЗаЦикл(t *testing.T) {
	ref := pp(1, 7, 29)
	if move, _ := judgeIteration(ref, pp(1, 7, 31), false); move != moveRefined {
		t.Errorf("первое уточнение: ожидалось moveRefined, получено %d", move)
	}
	if move, _ := judgeIteration(ref, pp(1, 7, 32), true); move != moveNone {
		t.Errorf("второе уточнение в цикле: ожидалось «не сдвинулась», получено %d", move)
	}
}

func TestНеизмеримоеВПетле(t *testing.T) {
	if move, _ := judgeIteration(pp(1, 7, 29), driftPoint{Closed: d(7), Open: d(29)}, false); move != moveUnmeasured {
		t.Errorf("ожидалось moveUnmeasured, получено %d", move)
	}
}

func TestРасходБезСухихИНулевыхСтрок(t *testing.T) {
	dir := t.TempDir()
	rows := []string{
		`{"iteration":1,"total_cost_usd":null,"num_turns":null,"spent_usd":0,"whatif":true}`,
		`{"iteration":1,"total_cost_usd":1.0899,"num_turns":56,"spent_usd":1.0899,"whatif":false}`,
		`{"iteration":2,"total_cost_usd":0.5,"num_turns":20,"spent_usd":1.5899,"whatif":false}`,
		`{"iteration":0,"total_cost_usd":0,"num_turns":0,"spent_usd":1.5899,"whatif":false}`,
		`{"iteration":1,"total_cost_usd":null,"num_turns":null,"spent_usd":0,"whatif":true}`,
	}
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), []byte(strings.Join(rows, "\r\n")+"\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := readSpend(dir, 20)
	if s.Iterations != 2 {
		t.Errorf("итераций: ожидалось 2 (сухие и нулевая не считаются), получено %d", s.Iterations)
	}
	if s.DryRuns != 2 {
		t.Errorf("сухих строк: ожидалось 2, получено %d", s.DryRuns)
	}
	if s.LastCost == nil || math.Abs(*s.LastCost-0.5) > 1e-9 {
		t.Errorf("расход последней итерации: ожидалось 0.5, получено %v", s.LastCost)
	}
	if s.RunSpent == nil || math.Abs(*s.RunSpent-1.5899) > 1e-9 {
		t.Errorf("расход последнего прогона: ожидалось 1.5899, получено %v", s.RunSpent)
	}
	if math.Abs(s.Total-1.5899) > 1e-9 {
		t.Errorf("всего по журналу: ожидалось 1.5899, получено %v", s.Total)
	}
}

func usd(v float64) *float64 { return &v }

func TestСтрокаРасходаНазываетПотолокПрогона(t *testing.T) {
	r := productReport{Product: "X", Spend: reportSpend{
		Iterations: 2, LastCost: usd(0.5), RunSpent: usd(1.5899), Total: 7.25, Budget: 20}}
	txt := r.text()
	if !strings.Contains(txt, "последний прогон $1.59 из потолка $20 на прогон") {
		t.Errorf("нет расхода прогона против потолка:\n%s", txt)
	}
	if strings.Contains(txt, "из бюджета") {
		t.Errorf("сумма журнала снова сравнивается с потолком прогона:\n%s", txt)
	}
	dry := productReport{Product: "X", Spend: reportSpend{DryRuns: 2, Budget: 20}}.text()
	if !strings.Contains(dry, "петля не заводилась") || strings.Contains(dry, "потолк") {
		t.Errorf("журнал из одних сухих строк:\n%s", dry)
	}
}

func TestПетляБерётПервыйОткрытыйШагИзПланаНаДиске(t *testing.T) {
	// Живой случай 14.09.2026, ASW: шаг 8а закрыт на седьмой итерации, а восьмая пошла по
	// заданию того же шага на opus:medium. Шаг берётся из плана на диске, а не из списка,
	// разобранного при старте.
	dir := t.TempDir()
	plan := filepath.Join(dir, "PLAN.md")
	write := func(s string) {
		if err := os.WriteFile(plan, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	head := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 7 | Решение ЛПР | — | подпись |\n"
	write(head + "| 8а | Литералы mcpupdate | haiku | тест |\n| 8б | Литералы enginenotice | haiku | тест |\n")
	cur, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok || cur.Num != "8а" {
		t.Fatalf("до закрытия: ожидался шаг 8а, получено %+v (%v)", cur, ok)
	}
	write(head + "| ~~8а~~ | Литералы mcpupdate | haiku | тест |\n| 8б | Литералы enginenotice | haiku | тест |\n")
	next, ok := firstOpenWorkStep(readPlanSteps(plan))
	if !ok || next.Num != "8б" || sameStep(next, cur) {
		t.Fatalf("после закрытия 8а: ожидался другой шаг 8б, получено %+v (%v)", next, ok)
	}
	// Правка текста открытого шага — тот же шаг: лестница заново не начинается.
	write(head + "| 8а | Литералы mcpupdate (уточнено) | haiku | тест |\n| 8б | Литералы enginenotice | haiku | тест |\n")
	edited, _ := firstOpenWorkStep(readPlanSteps(plan))
	if !sameStep(edited, cur) {
		t.Fatalf("правка текста шага приняла его за другой шаг: %+v против %+v", edited, cur)
	}
}

func TestЗаданиеНазываетКритерийИПравилоЗакрытия(t *testing.T) {
	// Итерации 5–6 на ASW не сдвинули вердикт: код был, номер в плане не зачёркнут.
	// Сухой прогон пишет файл задания и выходит до вызова модели — его и читаем.
	dir := t.TempDir()
	c := &loopCtx{Root: dir, WhatIf: true, MaxTurns: 60}
	step := workStep{Index: 2, Num: "8а", Title: "8а. Литералы mcpupdate", Tier: "haiku",
		Judge: "пакет внесён в translatedPackages"}
	c.runModelStep(step, "haiku:medium", "", runnerSpec{Kind: "claude", Model: "haiku"})
	tasks, _ := filepath.Glob(filepath.Join(dir, ".woody", "TASK-2-*.md"))
	if len(tasks) != 1 {
		t.Fatalf("файл задания не найден: %v", tasks)
	}
	raw, err := os.ReadFile(tasks[0])
	if err != nil {
		t.Fatal(err)
	}
	task := string(raw)
	for _, want := range []string{"Критерий шага: пакет внесён в translatedPackages", "~~8а~~", "PLAN.md"} {
		if !strings.Contains(task, want) {
			t.Errorf("в задании нет «%s»:\n%s", want, task)
		}
	}
}
