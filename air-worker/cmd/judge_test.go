package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestТриКодаВердикта(t *testing.T) {
	// Третий код — главное отличие от прежних судей: «нечем проверить» не равно
	// «не прошло», и отсутствие доказательства нулевой оценкой не является.
	if code, _ := verdict(judgeResult{Passed: []string{"а"}}); code != 0 {
		t.Errorf("всё пройдено -> ожидался 0, получен %d", code)
	}
	if code, _ := verdict(judgeResult{Failed: []string{"а"}}); code != 1 {
		t.Errorf("не пройдено -> ожидался 1, получен %d", code)
	}
	if code, _ := verdict(judgeResult{Unknown: []string{"а"}}); code != 2 {
		t.Errorf("нечем проверить -> ожидался 2, получен %d", code)
	}
	// Неизвестное перевешивает непройденное: работать дальше бессмысленно, пока нечем
	// мерить, даже если что-то уже красное.
	code, text := verdict(judgeResult{Failed: []string{"б"}, Unknown: []string{"а"}})
	if code != 2 {
		t.Errorf("неизвестное вместе с непройденным -> ожидался 2, получен %d", code)
	}
	if !strings.Contains(text, "отдельно не пройдено") {
		t.Error("непройденное не должно пропадать из текста при коде 2")
	}
}

func TestГейтыВычтеныИзРасстоянияНоВидныВТексте(t *testing.T) {
	// Случай AIR-ENV-002: 4 закрыто, 3 ждут ЛПР, требуется 7. Работой закрывать нечего,
	// но цель не достигнута.
	r := judgeResult{
		FactsClosed:   intPtr(4),
		FactsGated:    3,
		FactsRequired: 7,
		FactsLine:     "фактов закрыто 4 из 7, из них 3 ждут ЛПР",
	}
	r.Failed = append(r.Failed, r.FactsLine)

	code, text := verdict(r)
	if code != 1 {
		t.Fatalf("гейты цель не закрывают: ожидался код 1, получен %d", code)
	}
	if !strings.Contains(text, "ждут ЛПР") {
		t.Error("гейты обязаны остаться видимы в тексте — иначе метка прячет работу")
	}
	dist := distanceOf(code, r)
	if dist == nil || *dist != 0 {
		t.Fatalf("расстояние обязано быть 0 (гейты вычтены), получено %v", dist)
	}
}

func TestРасстояниеНеизвестноПриКоде2(t *testing.T) {
	// «Ноль здесь читался бы как всё в порядке».
	r := judgeResult{Unknown: []string{"нечем"}, FactsRequired: 5}
	if distanceOf(2, r) != nil {
		t.Fatal("при коде 2 расстояние обязано быть неизвестным, а не нулевым")
	}
}

func TestГейтБезПричиныЭтоНечемПроверить(t *testing.T) {
	dir := t.TempDir()
	cl := `{"items":[
		{"id":"f01","status":"completed"},
		{"id":"f02","status":"gated","awaits":"повышение прав"},
		{"id":"f03","status":"gated"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, "checklist.json"), []byte(cl), 0o644); err != nil {
		t.Fatal(err)
	}
	var r judgeResult
	countFacts(dir, "checklist.json", 3, &r)
	if len(r.Unknown) == 0 {
		t.Fatal("gated без awaits обязан давать «нечем проверить»: иначе метка станет местом, куда складывают неудобное")
	}
	if !strings.Contains(r.Unknown[0], "f03") {
		t.Errorf("причина обязана называть виновный факт, получено: %s", r.Unknown[0])
	}
}

func TestПропажаРеестраЭтоНеЗакрытоНоль(t *testing.T) {
	var r judgeResult
	countFacts(t.TempDir(), "нет-такого.json", 9, &r)
	if len(r.Unknown) != 1 {
		t.Fatalf("пропажа реестра обязана давать «нечем», получено unknown=%d failed=%d", len(r.Unknown), len(r.Failed))
	}
	if r.FactsClosed != nil {
		t.Error("при пропаже реестра число закрытых не определено, а не равно нулю")
	}
}

func TestBOMНеЛомаетРазбор(t *testing.T) {
	// Файлы, написанные PowerShell 5.1, несут BOM. Это свойство того, кто писал.
	dir := t.TempDir()
	p := filepath.Join(dir, "c.json")
	if err := os.WriteFile(p, append(utf8BOM, []byte(`{"items":[{"id":"f01","status":"completed"}]}`)...), 0o644); err != nil {
		t.Fatal(err)
	}
	var cl checklistFile
	if err := readJSON(p, &cl); err != nil {
		t.Fatalf("BOM не должен ломать разбор: %v", err)
	}
	if len(cl.Items) != 1 {
		t.Fatalf("ожидался 1 факт, получено %d", len(cl.Items))
	}
}
