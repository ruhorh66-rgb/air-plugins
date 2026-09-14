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
	// RunSpent — расход последнего прогона: `spent_usd` его последней строки. Потолок
	// бюджета действует на прогон, и сравнивать с ним сумму всего журнала неверно.
	RunSpent *float64 `json:"run_spent"`
	// DryRuns — строк сухого прогона. Итерациями они не считаются: работы не было.
	DryRuns int     `json:"dry_runs"`
	Budget  float64 `json:"budget"`
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
	// LoopRunning — по продукту идёт петля в другом процессе: дерево и вердикт меняются
	// под её работой, и судья отчётом не прогоняется (см. buildReport).
	LoopRunning bool `json:"loop_running"`
}

// measureTree считает изменённые и новые файлы объявленного продукта.
// Не дерево git — не отказ: продукт может лежать вне репозитория, и тогда строка
// честно скажет «не под git», а не соврёт нулём.
// ДЕРЕВО МЕРЯЕТСЯ ТОЛЬКО ПОДКАТАЛОГОМ ПРОДУКТА, а не всем репозиторием.
//
// `git status --porcelain` без ограничения отвечает про ВЕСЬ репозиторий, и в
// моно-репозитории это чужие числа. Замер 14.09.2026 на F:\-7-: по всему репозиторию 10
// изменённых файлов, по каталогу air-worker — ОДИН. Девять принадлежали соседним
// продуктам, и отчёт приписывал их этому.
//
// Это ровно та дыра, ради которой строка «Дерево» и заводилась, только вывернутая:
// она должна была ловить «денег потрачено, а в продукте ноль», а показывала чужую
// работу как свою. Ограничение `-- .` вместе с рабочим каталогом продукта и есть
// ответ: пути считаются относительно него.
func measureTree(root string) reportTree {
	cmd := exec.Command("git", "status", "--porcelain", "--", ".")
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
			Cost      *float64 `json:"total_cost_usd"`
			Turns     *int     `json:"num_turns"`
			Iteration *int     `json:"iteration"`
			Spent     *float64 `json:"spent_usd"`
			WhatIf    bool     `json:"whatif"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(ln, string(utf8BOM))), &row) != nil {
			continue
		}
		// СУХОЙ ПРОГОН — НЕ ИТЕРАЦИЯ. Его строки помечены, и до 0.9.5 отчёт всё равно
		// считал их: журнал из строк сухого прогона давал «итераций N» и «расход последней
		// итерации НЕ ПРИШЁЛ» — как будто работа шла, а замер потерялся.
		if row.WhatIf {
			s.DryRuns++
			continue
		}
		s.RunSpent = row.Spent
		// Нулевая строка — запись «план пройден, делать нечего», а не работа. Расход
		// прогона она несёт, итерацией не считается.
		if row.Iteration != nil && *row.Iteration == 0 {
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

	// ПЕТЛЯ ИДЁТ — СУДЬЯ ОТЧЁТОМ НЕ ПРОГОНЯЕТСЯ. Единственное исключение из правила шапки,
	// и причина в устройстве петли: после каждой итерации она прогоняет судью, пишет
	// вердикт и тут же меряет по нему расстояние. Второй судья, запущенный отчётом рядом,
	// гонял бы тесты по дереву, которое исполнитель правит прямо сейчас, и мог бы записать
	// свой вердикт между её записью и её замером — петля судила бы итерацию чужими числами.
	// Поэтому при идущей петле отчёт берёт ЕЁ последний вердикт и прямо говорит, чей он и
	// когда записан.
	r.LoopRunning = lockHeld(lockName("loop", root))

	switch {
	case cfgErr != nil:
		r.JudgeCode = 2
		r.JudgeText = "НЕ ПРОВЕРЕНО: нет конфигурации run-config.json"
	case r.LoopRunning:
		var mv machineVerdict
		if err := readJSON(filepath.Join(root, ".goal-verdict.json"), &mv); err != nil {
			r.JudgeCode = 2
			r.JudgeText = "НЕ ПРОВЕРЕНО: петля идёт, а машинного вердикта ещё нет"
		} else {
			r.JudgeCode = mv.Code
			r.JudgeText = fmt.Sprintf("%s [вердикт петли от %s; отчёт судью не прогонял — петля идёт]",
				firstLine(mv.VerdictText), mv.At)
		}
	default:
		// Судья — прогоном. См. шапку о том, почему не из файла.
		res := runJudge(root, cfg, -1)
		r.JudgeCode, r.JudgeText = verdict(res)
		// Запись вердикта нужна следующему замеру расстояния: без неё drift скажет
		// «машинного вердикта нет» на продукте, судью которого только что прогнали.
		publishVerdict(root, r.JudgeCode, r.JudgeText, res)
	}

	r.Measure, r.Reasons, r.Limits = measureDrift(root, "")
	r.Tree = measureTree(root)
	budget := 0.0
	if cfgErr == nil {
		budget = orFloat(cfg.Budget.USD, 20) // тот же потолок, что возьмёт петля
	}
	r.Spend = readSpend(root, budget)
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
	if r.LoopRunning {
		w("Петля      : идёт в другом процессе — дерево и вердикт меняются под её работой")
	}
	w("Судья      : %s (код %d)", firstLine(r.JudgeText), r.JudgeCode)
	w("Расстояние : %s · застой %d · вердикт %s",
		intOrDash(r.Measure.Distance), r.Measure.StallMoves, r.Measure.Verdict)
	// ПЛАН — СВОЕЙ СТРОКОЙ, А НЕ СЛАГАЕМЫМ. Расстояние выше — остаток по судье; план
	// показывает, сколько работы объявлено и сколько закрыто. Подробный план не дальше
	// от цели, чем крупноблочный, — почему перестали складывать, см. measureDrift.
	if r.Measure.PlanOpenSteps != nil && r.Measure.PlanClosedSteps != nil {
		planLine := fmt.Sprintf("План       : открытых исполняемых шагов %d, закрытых %d",
			*r.Measure.PlanOpenSteps, *r.Measure.PlanClosedSteps)
		if r.Measure.PlanGates > 0 {
			planLine += fmt.Sprintf(", гейтов ЛПР %d", r.Measure.PlanGates)
		}
		w("%s", planLine)
	}

	spend := "Потрачено  : "
	switch {
	case r.Spend.Iterations == 0:
		spend += "петля не заводилась"
		if r.Spend.DryRuns > 0 {
			spend += fmt.Sprintf(" (сухих итераций %d — работы в них не было)", r.Spend.DryRuns)
		}
	case r.Spend.LastCost == nil:
		spend += "расход последней итерации НЕ ПРИШЁЛ"
	default:
		spend += fmt.Sprintf("$%.4f за последнюю итерацию", *r.Spend.LastCost)
	}
	// ПОТОЛОК БЮДЖЕТА — НА ПРОГОН, И СРАВНИВАЕТСЯ С РАСХОДОМ ПРОГОНА. До 0.9.5 здесь стояло
	// «всего по журналу $X из бюджета $20»: сумма всех прогонов против потолка одного.
	// Журнал в $40 читался бы перерасходом вдвое, хотя ни один прогон своего потолка не
	// достиг, а «$9 из $20» не говорило, сколько осталось у идущего прогона. Найдено
	// 14.09.2026 при разборе петли на ASW.
	if r.Spend.Iterations > 0 {
		if r.Spend.RunSpent != nil {
			spend += fmt.Sprintf(" · последний прогон $%.2f", *r.Spend.RunSpent)
			if r.Spend.Budget > 0 {
				spend += fmt.Sprintf(" из потолка $%.0f на прогон", r.Spend.Budget)
			}
		} else if r.Spend.Budget > 0 {
			spend += fmt.Sprintf(" · потолок $%.0f на прогон", r.Spend.Budget)
		}
		spend += fmt.Sprintf(" · всего по журналу $%.2f · итераций %d", r.Spend.Total, r.Spend.Iterations)
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

	// ПЛАН КАК ФАЙЛ ОБЯЗАТЕЛЕН И ЗДЕСЬ (этап 0.10, К32). До этой правки отчёт на продукте
	// без плана всё равно печатал числа — расстояние по одному судье, план как «плана
	// нет» строкой в ограничениях, — а страж хода вставлял их в ход как замер, которым
	// нечего доказывать: план не годен, а отчёт выглядел полным.
	var cfg runConfig
	_ = readJSON(filepath.Join(root, "run-config.json"), &cfg)
	if _, code, ok := requirePlan(root, cfg, "отчёт"); !ok {
		return code
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
