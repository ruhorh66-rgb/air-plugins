package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writePlan(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "PLAN.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestГрамматикаПланаЧетыреКолонки(t *testing.T) {
	plan := parsePlan(writePlan(t, "| № | Шаг | Ступень | Судья |\n"+
		"|---|-----|---------|-------|\n"+
		"| ~~1~~ | сделано | `script` | исполнитель: ведущая |\n"+
		"| 2 | предстоит | `sonnet` | исполнитель: субагент |\n"+
		"| 3 | выпуск | — | гейт: ЛПР |\n"))
	if len(plan.Steps) != 3 {
		t.Fatalf("ожидалось 3 шага, разобрано %d", len(plan.Steps))
	}
	if plan.OpenWork() != 1 {
		t.Errorf("работой закрывается ровно один шаг, получено %d", plan.OpenWork())
	}
	if plan.Gates() != 1 {
		t.Errorf("гейтов ожидался 1, получено %d", plan.Gates())
	}
}

func TestЗакрытыйШагТожеРазбирается(t *testing.T) {
	// Прежний образец искал голое число, и закрытый шаг под него не подходил: проверка
	// ухода от плана считала допустимыми ТОЛЬКО открытые шаги, и ход, отчитавшийся о
	// шаге, который в нём же и закрыли, объявлялся уходом.
	plan := parsePlan(writePlan(t, "| ~~11~~ | сделано | `script` | ведущая |\n"))
	if len(plan.Steps) != 1 || plan.Steps[0].Num != "11" || !plan.Steps[0].Closed {
		t.Fatalf("зачёркнутый номер обязан разбираться как закрытый шаг 11, получено %+v", plan.Steps)
	}
}

func TestГейтыНеСчитаютсяДолгомСессии(t *testing.T) {
	// Гейт не закрывается работой. Считать его своим долгом значило бы держать расстояние
	// до цели ненулевым вечно — то есть обвинять сессию в дрейфе за чужое бездействие.
	plan := parsePlan(writePlan(t,
		"| 1 | выпуск | — | гейт: ЛПР |\n"+
			"| 2 | бинарник | — | гейт: ЛПР |\n"))
	if plan.OpenWork() != 0 {
		t.Fatalf("план из одних гейтов не даёт работы, получено %d", plan.OpenWork())
	}
}

func TestОтсутствующийПланЭтоНеПустойПлан(t *testing.T) {
	plan := parsePlan(filepath.Join(t.TempDir(), "нет.md"))
	if plan.Found {
		t.Fatal("отсутствующий план обязан отличаться от пустого: первое «нечем мерить», второе «мерить нечего»")
	}
}
