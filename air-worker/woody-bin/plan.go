package main

import (
	"os"
	"regexp"
	"strings"
)

// Грамматика плана — РОВНО ЧЕТЫРЕ КОЛОНКИ, закрытый шаг зачёркнутым номером:
//
//	| № | Шаг | Ступень | Судья |
//	| ~~1~~ | сделано | `script` | исполнитель: ведущая |
//	| 2 | предстоит | `sonnet` | исполнитель: субагент |
//	| 3 | выпуск | — | гейт: ЛПР |
//
// Пятая колонка не даёт ошибки — план молча даёт НОЛЬ шагов, и петля отвечает «план
// пуст» на живом плане. Этот отказ ловили трижды за сутки на трёх разных площадках.
//
// Образец принимает и зачёркнутый, и голый номер ОДНИМ выражением. Прежняя проверка
// ухода от плана искала голое число и потому считала допустимыми только ОТКРЫТЫЕ шаги:
// ход, отчитавшийся о шаге, который в нём же и закрыли, объявлялся уходом от плана.
var reStep = regexp.MustCompile(`^\s*\|\s*(~~)?\s*(\d+)\s*(~~)?\s*\|`)

type planStep struct {
	Num    string
	Closed bool
	Gate   bool // закрывается человеком, а не сессией
}

type planInfo struct {
	Found bool
	Steps []planStep
}

// OpenWork — сколько шагов сессия может закрыть работой.
//
// Гейты ЛПР исключены намеренно: они не закрываются работой, и считать их своим долгом
// значило бы держать расстояние до цели ненулевым вечно — то есть обвинять сессию в
// дрейфе за чужое бездействие. Тот же довод, что у гейтованных фактов.
func (p planInfo) OpenWork() int {
	n := 0
	for _, s := range p.Steps {
		if !s.Closed && !s.Gate {
			n++
		}
	}
	return n
}

func (p planInfo) Gates() int {
	n := 0
	for _, s := range p.Steps {
		if s.Gate {
			n++
		}
	}
	return n
}

func parsePlan(path string) planInfo {
	raw, err := os.ReadFile(path)
	if err != nil {
		return planInfo{Found: false}
	}
	info := planInfo{Found: true}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		m := reStep.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		info.Steps = append(info.Steps, planStep{
			Num:    m[2],
			Closed: m[1] != "",
			Gate:   strings.Contains(line, "гейт"),
		})
	}
	return info
}
