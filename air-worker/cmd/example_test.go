package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// ОБРАЗЕЦ ПЛАНА РАЗБИРАЕТСЯ ТЕМ ЖЕ КОДОМ, ЧТО И НАСТОЯЩИЙ ПЛАН.
//
// За один день, 14.09.2026, документация дважды разошлась с разборщиком молча, и оба
// раза это нашла AIR-ENV-002, а не проверки. Сперва команда `::`: она была описана только
// для списка, а скил предписывал таблицу. Затем сам PLAN.example.md: он показывал таблицу
// из пяти колонок, пока разборщик и SKILL.md знали четыре, — и человек, писавший строго
// по образцу, получал ноль шагов без единой ошибки.
//
// Отказ этого класса не кричит: план «пуст», петля честно говорит «шагов нет», и никто не
// связывает это с образцом. Поэтому проверяется не наличие образца, а то, что КАЖДАЯ
// строка, похожая на шаг, разобрана как шаг. Строка, выпавшая из разбора, и есть дефект.
func TestОбразецПланаРазбирается(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "skills", "woody", "PLAN.example.md"))
	if err != nil {
		t.Fatalf("образец плана не прочитан: %v", err)
	}
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")

	fence := regexp.MustCompile("(?s)```text\n(.*?)```")
	tableRow := regexp.MustCompile(`^\s*\|\s*(~~)?\s*[0-9]`)
	listRow := regexp.MustCompile(`^\s*-\s*\[[ xX]\]`)
	placeholder := regexp.MustCompile(`<[^>]+>`)

	blocks := fence.FindAllStringSubmatch(text, -1)
	if len(blocks) == 0 {
		t.Fatal("в образце не найдено ни одного блока с примером — проверять нечем")
	}

	tables, lists, gates := 0, 0, 0
	for i, b := range blocks {
		var rows []string
		isTable := false
		for _, ln := range strings.Split(b[1], "\n") {
			switch {
			case placeholder.MatchString(ln):
				// Шаблон вида «<ступень> <заголовок>» — описание формата, а не шаг.
				continue
			case tableRow.MatchString(ln):
				rows = append(rows, ln)
				isTable = true
			case listRow.MatchString(ln):
				rows = append(rows, ln)
			}
		}
		if len(rows) == 0 {
			continue
		}
		dir := t.TempDir()
		p := filepath.Join(dir, "PLAN.md")
		body := strings.Join(rows, "\n") + "\n"
		if isTable {
			body = "| № | Шаг | Ступень | Судья |\n|---|-----|---------|-------|\n" + body
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		plan := parsePlan(p)
		if len(plan.Steps) != len(rows) {
			t.Errorf("блок %d образца: строк, похожих на шаг, %d, а разобрано шагов %d — "+
				"образец расходится с разборщиком, и план по нему окажется неполным или пустым",
				i+1, len(rows), len(plan.Steps))
		}
		if isTable {
			tables++
			gates += plan.Gates()
		} else {
			lists++
		}
	}

	if tables == 0 {
		t.Error("в образце нет таблицы — а гейт выражается только таблицей")
	}
	if lists == 0 {
		t.Error("в образце нет списка с флажками — второй формат не показан")
	}
	if gates == 0 {
		t.Error("табличный пример образца не содержит ни одного гейта — главный довод за таблицу не показан")
	}
}
