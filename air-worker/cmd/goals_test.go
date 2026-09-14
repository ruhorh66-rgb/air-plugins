package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Этап 0.10: без блока целей air-worker не работает (решение ЛПР 14.09.2026).

const planWithGoals = "**Ц1.** механизм не работает без целей\n\n" +
	"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
	"| К1 | Ц1 | план без целей не исполняется | проверка `тесты` |\n" +
	"| К2 | Ц1 | шаг без критерия не исполняется | факт `f01` |\n\n" +
	"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
	"| ~~1~~ | сделано до целей | `script` | исполнитель: ведущая |\n" +
	"| 2 | отказ петли | `sonnet` | К1: тест петли |\n" +
	"| 3 | решение ЛПР | — | гейт: ЛПР |\n"

func writePlanFile(t *testing.T, dir, text string) string {
	t.Helper()
	p := filepath.Join(dir, "PLAN.md")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestБлокЦелейРазбирается(t *testing.T) {
	path := writePlanFile(t, t.TempDir(), planWithGoals)
	g := readPlanGoals(path)
	if len(g.Goals) != 1 || g.Goals[0].ID != "Ц1" {
		t.Fatalf("цели разобраны неверно: %+v", g.Goals)
	}
	if len(g.Criteria) != 2 {
		t.Fatalf("критериев ожидалось 2, получено %+v", g.Criteria)
	}
	if c := g.Criteria[0]; c.ID != "К1" || c.Goal != "Ц1" || len(c.Checks) != 1 || c.Checks[0] != "тесты" {
		t.Errorf("К1 разобран неверно: %+v", c)
	}
	if c := g.Criteria[1]; len(c.Facts) != 1 || c.Facts[0] != "f01" {
		t.Errorf("К2 разобран неверно: %+v", c)
	}
	if problems := goalProblems(g, readPlanSteps(path)); len(problems) != 0 {
		t.Errorf("годный план получил замечания: %v", problems)
	}
	// Шаги таблицы по-прежнему разбираются: строки критериев шагами не считаются.
	if steps := readPlanSteps(path); len(steps) != 3 {
		t.Errorf("шагов ожидалось 3, разобрано %d — таблица критериев попала в шаги", len(steps))
	}
}

func TestПланБезЦелейНеГоден(t *testing.T) {
	path := writePlanFile(t, t.TempDir(), "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | работа | `sonnet` | тест |\n")
	joined := strings.Join(goalProblems(readPlanGoals(path), readPlanSteps(path)), "\n")
	for _, want := range []string{"нет ни одной цели", "нет ни одного критерия", "без ссылки на критерий"} {
		if !strings.Contains(joined, want) {
			t.Errorf("нет замечания «%s»:\n%s", want, joined)
		}
	}
}

func TestШагБезКритерияНазванПоНомеру(t *testing.T) {
	text := strings.Replace(planWithGoals, "| 2 | отказ петли | `sonnet` | К1: тест петли |",
		"| 2 | отказ петли | `sonnet` | тест петли |\n| 4 | ещё работа | `haiku` | К9: такого критерия нет |", 1)
	path := writePlanFile(t, t.TempDir(), text)
	joined := strings.Join(goalProblems(readPlanGoals(path), readPlanSteps(path)), "\n")
	if !strings.Contains(joined, "цель не требует: 2") {
		t.Errorf("шаг 2 без критерия не назван:\n%s", joined)
	}
	if !strings.Contains(joined, "шаг 4 ссылается на критерий К9") {
		t.Errorf("ссылка на несуществующий критерий не названа:\n%s", joined)
	}
}

func TestПетляНеИсполняетПланБезЦелей(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"judge":{"checks":[{"name":"тесты","command":"cmd","args":["/c","exit","0"]}]},"ladder":["script","sonnet"]}`
	if err := os.WriteFile(filepath.Join(dir, "run-config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	writePlanFile(t, dir, "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | работа | `sonnet` | тест |\n")
	if code := cmdLoop([]string{"-product", dir}); code != 2 {
		t.Fatalf("петля на плане без целей вернула %d, ожидался отказ 2", code)
	}
}

func TestКритерийБезПроверкиНечемПроверить(t *testing.T) {
	dir := t.TempDir()
	cfg := runConfig{Judge: judgeSpec{Checks: []checkSpec{{Name: "тесты", Command: "cmd", Args: []string{"/c", "exit", "0"}}}}}
	g := planGoals{Criteria: []planCriterion{
		{ID: "К1", Goal: "Ц1", Checks: []string{"тесты"}},
		{ID: "К2", Goal: "Ц1", Checks: []string{"нет такой"}},
		{ID: "К3", Goal: "Ц1"},
	}}
	out := strings.Join(criteriaBinding(dir, cfg, g), "\n")
	if strings.Contains(out, "К1") {
		t.Errorf("привязанный критерий назван непривязанным:\n%s", out)
	}
	if !strings.Contains(out, "критерий К2: проверки «нет такой» в судье нет") {
		t.Errorf("мёртвая ссылка на проверку не названа:\n%s", out)
	}
	if !strings.Contains(out, "критерий К3 не привязан") {
		t.Errorf("критерий без ссылки не назван:\n%s", out)
	}

	// И судья выносит «нечем проверить»: у К2 из плана ссылка на факт, которого нет в реестре.
	writePlanFile(t, dir, planWithGoals)
	code, text := verdict(runJudge(dir, cfg, -1))
	if code != 2 || !strings.Contains(text, "К2") {
		t.Errorf("судья на критерии без меры: код %d, «%s»; ожидался код 2 с названным К2", code, text)
	}
}

func TestПланировщикБезПостановкиОтказывает(t *testing.T) {
	dir := t.TempDir()
	cfg := `{"judge":{"checks":[{"name":"x","command":"cmd","args":["/c","exit","0"]}]},"ladder":["script","opus"]}`
	if err := os.WriteFile(filepath.Join(dir, "run-config.json"), []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "goal"), 0o755); err != nil {
		t.Fatal(err)
	}
	goal := filepath.Join(dir, "goal", "goal.json")
	if err := os.WriteFile(goal, []byte(`{"goal":"цель без постановки"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := cmdPlan([]string{"-product", dir, "-dry-run"}); code != 2 {
		t.Errorf("планировщик без постановки вернул %d, ожидался отказ 2", code)
	}
	if err := os.WriteFile(goal, []byte(`{"goal":"цель","objective":"нет-такого-файла.md"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := cmdPlan([]string{"-product", dir, "-dry-run"}); code != 2 {
		t.Errorf("планировщик с постановкой без файла вернул %d, ожидался отказ 2", code)
	}
}
