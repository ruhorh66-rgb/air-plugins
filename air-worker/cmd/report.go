package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ОТЧЁТ — ЭТО ЧИСЛА, КОТОРЫЕ ПЕЧАТАЕТ ПРОГРАММА, А НЕ МОДЕЛЬ.
//
// Написано 13.09.2026 по указанию ЛПР после разбора: «который день пытаемся настроить
// инструкцию взаимодействия, и только всё усложняем».
//
// Разбор дал число. Механизм к тому дню — 9 400 строк, и делился он надвое: 5 159 строк
// меряли СОСТОЯНИЕ ПРОДУКТА, 4 264 надзирали за ФОРМОЙ СООБЩЕНИЯ модели. За сутки первая
// половина нашла расхождение плана и вердикта, целый пласт необъявленной работы, три
// файла с неверной кодировкой и невидимый frontmatter. Вторая половина за те же сутки не
// улучшила ни одного продукта: она около десяти раз остановила ход, и лечением каждый раз
// была переписанная модель абзаца.
//
// Вывод, из которого растёт эта команда: НАДЗИРАТЬ ЗА РЕЧЬЮ НЕЛЬЗЯ. Речь подстроится под
// любой надзор, и надзор придётся удлинять — это и был «который день». Поэтому поля,
// которые прежде требовались от модели словами (фаза, ступень, кто исполняет, замер),
// становятся выводом программы: их нельзя ни забыть, ни сочинить.
//
// СУДЬЯ ЗДЕСЬ ПРОГОНЯЕТСЯ, А НЕ ЧИТАЕТСЯ ИЗ ФАЙЛА, и это не мелочь реализации.
// `drift` читает .goal-verdict.json намеренно: он меряет ТРЕНД между прогонами, и ему
// нужна записанная точка. Отчёту нужна ТЕКУЩАЯ, и записанный вердикт для этого врёт —
// у AirSmeta в файле лежало «8 из 10» от предыдущих суток, а прогон того же судьи дал
// «0 из 10», потому что зачистка забрала базу. Если бы отчёт читал файл, модель могла бы
// напечатать вчерашний успех, а страж, сверяющий отчёт с тем же файлом, согласился бы
// сам с собой. Замер, который сверяется с собственной записью, не замер.
//
// ЧЕГО ЭТА КОМАНДА НЕ ЧИНИТ, И ЭТО НАЗВАНО ЧЕСТНО. Она не мешает работать не над тем
// продуктом: печатается ОБЪЯВЛЕННЫЙ корень, и работа в чужом дереве даст чужой ноль.
// Ровно эта дыра скрыла полдня работы 13.09.2026. Поэтому в отчёте есть строка «Дерево»:
// ноль изменённых файлов при потраченных деньгах виден сразу и без надзора за речью.

type reportTree struct {
	Changed int `json:"changed"`
	New     int `json:"new"`
	// Signature — то же `git status --porcelain`, которым петля отличает «исполнитель
	// ничего не сделал» от «сделал и просит разрешения». Здесь она не печатается, но
	// входит в -json: страж сверяет её, а не пересказ.
	Signature string `json:"signature"`
	Known     bool   `json:"known"`
}

type reportSpend struct {
	LastCost   *float64 `json:"last_cost"`
	LastTurns  *int     `json:"last_turns"`
	Iterations int      `json:"iterations"`
	Total      float64  `json:"total"`
	Budget     float64  `json:"budget"`
}

type reportStep struct {
	Num   string `json:"num"`
	Title string `json:"title"`
	Tier  string `json:"tier"`
	Found bool   `json:"found"`
}

type productReport struct {
	Product   string        `json:"product"`
	JudgeCode int           `json:"judge_code"`
	JudgeText string        `json:"judge_text"`
	Measure   driftMeasure  `json:"measure"`
	Reasons   []driftReason `json:"reasons"`
	Limits    []string      `json:"limits"`
	Tree      reportTree    `json:"tree"`
	Spend     reportSpend   `json:"spend"`
	Next      reportStep    `json:"next"`
}

// measureTree считает изменённые и новые файлы объявленного продукта.
// Не дерево git — не отказ: продукт может лежать вне репозитория, и тогда строка
// честно скажет «не под git», а не соврёт нулём.
func measureTree(root string) reportTree {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return reportTree{Known: false}
	}
	t := reportTree{Known: true, Signature: string(out)}
	for _, ln := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		t.Changed++
		// Первый столбец — индекс, второй — дерево. Новым считается и добавленное в
		// индекс, и вовсе неизвестное git: для отчёта важно, что файла раньше не было.
		if len(ln) >= 2 && (ln[0] == 'A' || ln[1] == '?') {
			t.New++
		}
	}
	return t
}

// readSpend складывает журнал прогонов. Расход, которого поток не принёс, в журнале
// лежит как null и здесь НЕ СЧИТАЕТСЯ НУЛЁМ: ноль означал бы «бесплатно», и бюджет
// считался бы в сторону «можно ещё».
func readSpend(root string, budget float64) reportSpend {
	s := reportSpend{Budget: budget}
	f, err := os.Open(filepath.Join(root, "steps.jsonl"))
	if err != nil {
		return s
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		ln := strings.TrimSpace(sc.Text())
		if ln == "" {
			continue
		}
		var row struct {
			Cost  *float64 `json:"total_cost_usd"`
			Turns *int     `json:"num_turns"`
		}
		if json.Unmarshal([]byte(ln), &row) != nil {
			continue
		}
		s.Iterations++
		if row.Cost != nil {
			s.Total += *row.Cost
			s.LastCost = row.Cost
		} else {
			s.LastCost = nil
		}
		s.LastTurns = row.Turns
	}
	return s
}

// nextOpenStep — первый незакрытый исполняемый шаг плана. Гейт шагом не считается:
// его исполняет человек, и называть его «следующим шагом модели» значило бы обещать
// работу, которой модель сделать не может.
func nextOpenStep(root, planPath string) reportStep {
	if planPath == "" {
		planPath = filepath.Join(root, "PLAN.md")
	} else if !filepath.IsAbs(planPath) {
		planPath = filepath.Join(root, planPath)
	}
	for _, st := range readPlanSteps(planPath) {
		if st.Done || st.Gate {
			continue
		}
		return reportStep{Num: st.Num, Title: st.Title, Tier: st.Tier, Found: true}
	}
	return reportStep{}
}

func buildReport(root string) productReport {
	r := productReport{Product: root}

	var cfg runConfig
	cfgErr := readJSON(filepath.Join(root, "run-config.json"), &cfg)

	// Судья — прогоном. См. шапку о том, почему не из файла.
	if cfgErr != nil {
		r.JudgeCode = 2
		r.JudgeText = "НЕ ПРОВЕРЕНО: нет конфигурации run-config.json"
	} else {
		res := runJudge(root, cfg, -1)
		r.JudgeCode, r.JudgeText = verdict(res)
		// Запись вердикта нужна следующему замеру расстояния: без неё drift скажет
		// «машинного вердикта нет» на продукте, судью которого только что прогнали.
		publishVerdict(root, r.JudgeCode, r.JudgeText, res)
	}

	r.Measure, r.Reasons, r.Limits = measureDrift(root, "")
	r.Tree = measureTree(root)
	r.Spend = readSpend(root, cfg.Budget.USD)
	r.Next = nextOpenStep(root, cfg.Plan)
	return r
}

func intOrDash(v *int) string {
	if v == nil {
		return "нечем измерить"
	}
	return fmt.Sprintf("%d", *v)
}

func (r productReport) text() string {
	var b strings.Builder
	w := func(f string, a ...any) { fmt.Fprintf(&b, f+lineEnding, a...) }

	w("Продукт    : %s", r.Product)
	w("Судья      : %s (код %d)", firstLine(r.JudgeText), r.JudgeCode)
	w("Расстояние : %s · застой %d · вердикт %s",
		intOrDash(r.Measure.Distance), r.Measure.StallMoves, r.Measure.Verdict)

	spend := "Потрачено  : "
	switch {
	case r.Spend.Iterations == 0:
		spend += "петля не заводилась"
	case r.Spend.LastCost == nil:
		spend += fmt.Sprintf("расход последней итерации НЕ ПРИШЁЛ · всего по журналу $%.2f", r.Spend.Total)
	default:
		spend += fmt.Sprintf("$%.4f за последнюю итерацию · всего по журналу $%.2f", *r.Spend.LastCost, r.Spend.Total)
	}
	if r.Spend.Budget > 0 {
		spend += fmt.Sprintf(" из бюджета $%.0f", r.Spend.Budget)
	}
	if r.Spend.Iterations > 0 {
		spend += fmt.Sprintf(" · итераций %d", r.Spend.Iterations)
	}
	w("%s", spend)

	if r.Tree.Known {
		w("Дерево     : изменено файлов %d, из них новых %d", r.Tree.Changed, r.Tree.New)
	} else {
		w("Дерево     : не под git — изменения не измеряются")
	}

	if r.Next.Found {
		w("Шаг        : %s · %s · ступень %s", r.Next.Num, r.Next.Title, r.Next.Tier)
	} else {
		w("Шаг        : открытых исполняемых шагов в плане нет")
	}

	// Пределы замера печатаются ВСЕГДА, когда они есть. Отчёт, умолчавший о том, чего
	// он не смог измерить, хуже отсутствующего: он выглядит полным.
	for _, l := range r.Limits {
		w("НЕЧЕМ МЕРИТЬ: %s", l)
	}
	return b.String()
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func cmdReport(argv []string) int {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	asJSON := fs.Bool("json", false, "машинный вывод для сверки стражем")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Print("НЕЧЕМ МЕРИТЬ: не разобран путь продукта" + lineEnding)
		return 2
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		fmt.Printf("НЕЧЕМ МЕРИТЬ: нет каталога продукта %s"+lineEnding, root)
		return 2
	}

	r := buildReport(root)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
	} else {
		fmt.Print(r.text())
	}

	// Код возврата — код судьи. Отчёт не заводит своего мнения о готовности.
	return r.JudgeCode
}
