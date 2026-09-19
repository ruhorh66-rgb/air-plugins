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
	// rePlanRowNum — ТОЛЬКО номер табличной строки, той же формой, что головы rePlanTable и
	// rePlanGate. Нужен closeStepInPlan: строку закрывает петля сама (К31), а не исполнитель,
	// и трогать при этом можно ровно номер — ни отступы, ни остальные колонки.
	rePlanRowNum            = regexp.MustCompile(`^\s*\|\s*(~~)?\s*([0-9]+[A-Za-zА-Яа-я]?)\s*(~~)?\s*\|`)
	reNumberedFourColumnRow = regexp.MustCompile(`^\s*\|\s*(~~)?\s*([0-9]+[A-Za-zА-Яа-я]?)\s*(~~)?\s*\|\s*([^|]*)\|\s*([^|]*)\|\s*([^|]*)\|\s*$`)
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
			// A numbered four-column row that advertises an LPR gate must never vanish
			// because its punctuation or tier cell is malformed. Its closure cannot be
			// trusted either, so represent it as an open barrier until a human repairs it.
			if malformed := reNumberedFourColumnRow.FindStringSubmatch(line); malformed != nil {
				gateText := strings.ToLower(malformed[5] + " " + malformed[6])
				if strings.Contains(gateText, "гейт") || strings.Contains(gateText, "лпр") ||
					strings.Contains(gateText, "lpr") {
					i++
					steps = append(steps, workStep{
						Index: i, Num: malformed[2], Tier: "gate", Gate: true,
						Title: malformed[2] + ". " + strings.TrimSpace(malformed[4]) + " [некорректный гейт]",
						Judge: strings.TrimSpace(malformed[6]),
					})
				}
			}
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

// closeStepInPlan — закрывает В ФАЙЛЕ ПЛАНА ровно ту строку, что readPlanSteps отдала под
// step.Index: зачёркивает номер в табличной форме («| N |» -> «| ~~N~~ |») либо ставит «x»
// в строке-галочке («- [ ]» -> «- [x]»). Остальной текст строки и все прочие строки файла
// копируются побайтово — правится только отметка закрытия.
//
// К31: шаг СО СВОЕЙ КОМАНДОЙ (ступень script) закрывается кодом ЭТОЙ команды, а не работой
// исполнителя — у script исполнителя нет вовсе, править план в её ходе некому, кроме петли
// самой. Нашла AIR-ENV-002 14.09.2026: команда шага отработала кодом 0, план не тронулся
// (ей и нечем было его трогать — то была только проверка), и петля читала неподвижность
// плана как «цель не сдвинулась» — поднимала ступень до haiku на первом же прогоне.
//
// Строка ищется ТЕМ ЖЕ СЧЁТОМ, что и readPlanSteps: те же образцы, в том же порядке, номер
// по счёту совпадений, а не по тексту заголовка. Две реализации счёта, расходящиеся молча, —
// ровно тот класс дефекта, что уже чинили в этом продукте (см. parsePlan): поэтому счёт не
// заводится заново, а зеркалится буквально.
func closeStepInPlan(path string, step workStep) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	hadCR := strings.Contains(string(raw), "\r\n")
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")

	i := 0
	found := false
	for li, ln := range lines {
		switch {
		case reCheckLine.MatchString(ln):
			i++
			if i == step.Index {
				lines[li] = closeCheckLine(ln)
				found = true
			}
		case rePlanGate.MatchString(ln):
			// Гейты петля не закрывает, но счёт обязан идти той же строкой, что у
			// readPlanSteps, — иначе номер по счёту у всех шагов ПОСЛЕ гейта разойдётся.
			i++
		case rePlanTable.MatchString(ln):
			i++
			if i == step.Index {
				lines[li] = closeTableRow(ln)
				found = true
			}
		default:
			continue
		}
		if found {
			break
		}
	}
	if !found {
		return fmt.Errorf("шаг %d («%s») не нашёлся построчно в %s", step.Index, step.Title, path)
	}

	out := strings.Join(lines, "\n")
	if hadCR {
		out = strings.ReplaceAll(out, "\n", "\r\n")
	}
	// PLAN.md — BOM ЗАПРЕЩЁН (правило в encoding.go): харнесс не распознаёт frontmatter под
	// BOM. Файл читается уже без BOM (живые планы его не несут) и пишется таким же.
	return os.WriteFile(path, []byte(out), 0o644)
}

// closeTableRow — оборачивает номер табличной строки ~~страйком~~, не трогая ни отступы,
// ни остальные колонки: правится только диапазон байт самого номера.
func closeTableRow(ln string) string {
	loc := rePlanRowNum.FindStringSubmatchIndex(ln)
	if loc == nil {
		return ln
	}
	numStart, numEnd := loc[4], loc[5] // группа 2 — сам номер
	return ln[:numStart] + "~~" + ln[numStart:numEnd] + "~~" + ln[numEnd:]
}

// closeCheckLine — ставит «x» между квадратных скобок строки-галочки, не трогая остальное.
func closeCheckLine(ln string) string {
	loc := reCheckLine.FindStringSubmatchIndex(ln)
	if loc == nil {
		return ln
	}
	markStart, markEnd := loc[2], loc[3] // группа 1 — символ внутри [ ]
	return ln[:markStart] + "x" + ln[markEnd:]
}
