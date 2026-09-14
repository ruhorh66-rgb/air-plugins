package main

import (
	"fmt"
	"path/filepath"
)

// ПЛАН КАК ФАЙЛ ОБЯЗАТЕЛЕН НА КАЖДОМ ВХОДЕ БИНАРНИКА (этап 0.10, К32): петля, судья,
// двигатель, отчёт, объявление продукта. Решение ЛПР 14.09.2026: «без плана и без целей
// air-worker не работает» — до этой правки оно держалось только петлёй (cmd/loop.go), а
// остальные четыре входа расходились с ней порознь:
//
//   - судья (cmdJudge) без плана выносил «цель достигнута» — criteriaBinding молчит,
//     когда критериев ноль, а критериев ноль, когда план читать нечем;
//   - двигатель (cmdDrift) и отчёт (cmdReport) считали расстояние без плана и вдобавок
//     брали ПРИБИТЫЙ filepath.Join(root, "PLAN.md") мимо run-config.json — на продукте
//     с именем плана, отличным от умолчания, это расходилось с судьёй и петлёй, которые
//     путь брали верно;
//   - объявление продукта (skills/woody/hooks/mode.ps1 -Product) проверяло только маркер
//     заготовки: план без блока целей, но без маркера, продуктом становился беспрепятственно.
//
// ПУТЬ ПЛАНА ОДИН НА ВЕСЬ МЕХАНИЗМ — planFilePath ниже, и его же берут cmdGoals и cmdLoop:
// поле `plan` из run-config.json продукта, по умолчанию PLAN.md в его корне.
//
// ПРОВЕРКА ГОДНОСТИ — ТА ЖЕ, ЧТО У `air-worker goals`, И БУКВАЛЬНО ТЕ ЖЕ ФУНКЦИИ
// (readPlanSteps из planfile.go, readPlanGoals/goalProblems/criteriaBinding из goals.go),
// а не второе мнение рядом с первым: два места, судящие одно и то же порознь, в этом же
// механизме уже расходились молча дважды (parsePlan против readPlanSteps, сумма расстояния
// против отдельных признаков движения) — ровно тот класс дефекта, который эта правка и
// закрывает, а не плодит третий раз.
//
// КОД ЗДЕСЬ ОДИН — 2, А НЕ ДВА, КАК У cmdGoals. cmdGoals различает «плана нет» (2) и
// «план есть, но не годен» (1): это диагностика для стража работы, который правит файлы
// один за другим и обязан отличать «начинать с нуля» от «почти готово». У judge/drift/
// report такой разницы нет — им либо есть с чем работать, либо продолжать без человека
// некуда в обоих случаях, и К32 прямо требует код 2 на оба: «нет файла плана с годным
// блоком целей — отказ кодом 2», без разбора, чего именно не хватает.

// planFilePath — путь плана продукта: поле `plan` из run-config.json, по умолчанию
// PLAN.md в корне. Здесь вызывающему не важно, прочитан ли run-config.json целиком: при
// его отсутствии cfg остаётся нулевым значением, cfg.Plan пуст, и путь равен умолчанию —
// то же самое поведение, что было у прибитой строки, которую эта функция заменяет.
func planFilePath(root string, cfg runConfig) string {
	name := cfg.Plan
	if name == "" {
		name = "PLAN.md"
	}
	return filepath.Join(root, name)
}

// planRequirement — итог проверки годности плана: путь, разобранные шаги и цели (для
// вызывающего, которому они нужны дальше), и Problems — пусто значит годен.
type planRequirement struct {
	Path     string
	Steps    []workStep
	Goals    planGoals
	Problems []string
}

// OK — план годен к работе: файл есть, шаги разбираются, блок целей годен по той же
// логике, что и у `air-worker goals`.
func (r planRequirement) OK() bool { return len(r.Problems) == 0 }

// checkPlanRequirement — план как файл обязателен: нет файла с годным блоком целей —
// Problems непустой, а не молчаливая работа без плана (К32). Логика ровно та, что у
// cmdGoals: readPlanSteps -> readPlanGoals -> goalProblems -> criteriaBinding, теми же
// функциями, а не их пересказом.
func checkPlanRequirement(root string, cfg runConfig) planRequirement {
	path := planFilePath(root, cfg)
	res := planRequirement{Path: path}
	steps := readPlanSteps(path)
	if len(steps) == 0 {
		res.Problems = append(res.Problems, "план пуст или не найден: "+path)
		return res
	}
	res.Steps = steps
	res.Goals = readPlanGoals(path)
	res.Problems = append(res.Problems, goalProblems(res.Goals, steps)...)
	if len(res.Goals.Criteria) > 0 {
		res.Problems = append(res.Problems, criteriaBinding(root, cfg, res.Goals)...)
	}
	return res
}

// requirePlan — как checkPlanRequirement, но при негодном плане сама печатает отказ с
// путём плана и причинами и возвращает код 2 (К32: «нет файла плана с годным блоком
// целей — отказ кодом 2 с путём плана в сообщении, а не работа без плана»). caller
// называет вход («судья», «двигатель цели», «отчёт»), чтобы по одному только выводу было
// видно, что именно отказало и почему — не только числом кода.
func requirePlan(root string, cfg runConfig, caller string) (planRequirement, int, bool) {
	res := checkPlanRequirement(root, cfg)
	if res.OK() {
		return res, 0, true
	}
	fmt.Print("ОТКАЗ: план не годен к работе — " + res.Path + lineEnding)
	fmt.Print("  без годного плана " + caller + " не работает (этап 0.10, К32)" + lineEnding)
	for _, p := range res.Problems {
		fmt.Print("  - " + p + lineEnding)
	}
	return res, 2, false
}
