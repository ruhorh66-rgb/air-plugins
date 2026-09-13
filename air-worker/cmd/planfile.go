package main

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Разбор плана для ПЕТЛИ. Отличается от parsePlan (plan.go) назначением: тому нужны
// только номера и признаки закрытости для расстояния до цели, этому — ступень, заголовок,
// команда и судья, чтобы шаг можно было исполнить.

// Имена ступеней: свои и вендорские. Суффикс усилия НЕОБЯЗАТЕЛЕН, но принимается —
// решение ЛПР 11.09.2026 о двух ступеньках на каждую модель ('haiku:medium', 'haiku:max')
// было записано в лестницу конфигурации, а разборщик плана его не принимал: согласованное
// решение нельзя было выразить в плане ВООБЩЕ. Чтением не находилось, потому что оба
// файла по отдельности выглядели верными.
const tierPattern = `script|haiku|sonnet|opus|luna|terra|sol`

// ДВЕ ФОРМЫ ЗАПИСИ ШАГА, и это не украшение. Строка с галочкой — короткая, для планов без
// приёмки на каждый шаг. Строка таблицы — там, где у шага есть СВОЙ судья: в галочку его
// вписать физически некуда. Поэтому таблица не альтернативный стиль, а единственная форма,
// в которой требуемое содержимое помещается; второго ДОКУМЕНТА при этом не заводится.
var (
	reCheckLine = regexp.MustCompile(`^\s*-\s*\[([ xX])\]\s+(` + tierPattern + `)(:[A-Za-z]+)?\s+(.+)$`)
	rePlanTable = regexp.MustCompile(`^\s*\|\s*(~~)?\s*([0-9]+[A-Za-zА-Яа-я]?)\s*(?:~~)?\s*\|` +
		`\s*([^|]+?)\s*\|` +
		"\\s*`?(" + tierPattern + `)(:[A-Za-z]+)?` + "`?\\s*\\|" +
		`\s*([^|]*?)\s*\|\s*$`)
	// Шаг-ГЕЙТ: в колонке ступени не ступень, а прочерк или слово «гейт». Такой шаг
	// закрывается решением ЛПР, а не работой; петля его не исполняет, но и НЕ ГЛОТАЕТ.
	// Первый прогон 12.09.2026 дал «шагов в плане: 30» при 31 строке: разница пропадала
	// молча. Пропуск был верным, молчание — нет.
	rePlanGate = regexp.MustCompile(`^\s*\|\s*(~~)?\s*([0-9]+[A-Za-zА-Яа-я]?)\s*(?:~~)?\s*\|\s*([^|]+?)\s*\|\s*(?:—|-{1,2}|гейт[^|]*)\s*\|\s*([^|]*?)\s*\|\s*$`)
)

type workStep struct {
	Index int
	Num   string
	Done  bool
	Tier  string
	Title string
	Cmd   string
	Judge string
	Gate  bool
}

func splitCmd(rest string) (title, cmd string) {
	if i := strings.Index(rest, "::"); i >= 0 {
		return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i+2:])
	}
	return strings.TrimSpace(rest), ""
}

func readPlanSteps(path string) []workStep {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var steps []workStep
	i := 0
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if m := reCheckLine.FindStringSubmatch(line); m != nil {
			i++
			title, cmd := splitCmd(m[4])
			steps = append(steps, workStep{
				Index: i, Num: fmt.Sprintf("%d", i), Done: m[1] != " ", Tier: m[2] + m[3], Title: title, Cmd: cmd,
			})
			continue
		}
		if m := rePlanGate.FindStringSubmatch(line); m != nil {
			i++
			steps = append(steps, workStep{
				Index: i, Num: m[2], Done: m[1] != "", Tier: "gate", Gate: true,
				Title: m[2] + ". " + strings.TrimSpace(m[3]), Judge: strings.TrimSpace(m[4]),
			})
			continue
		}
		m := rePlanTable.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		// Заголовок таблицы сюда не попадает: слово «Ступень» не входит в перечень имён
		// ступеней, и строка просто не совпадает.
		i++
		title, cmd := splitCmd(strings.TrimSpace(m[3]))
		steps = append(steps, workStep{
			Index: i, Num: m[2], Done: m[1] != "", Tier: m[4] + m[5],
			Title: m[2] + ". " + title, Cmd: cmd, Judge: strings.TrimSpace(m[6]),
		})
	}
	return steps
}
