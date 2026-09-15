package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Двигатель цели: ход, не уменьшивший расстояние до цели, продвижением не считается.
//
// Восстановлено из Goal/Drift Loop продукта ВЕРА. Правило, записанное там в трёх местах
// независимо: рост промежуточного числа продвижением не считается, если расстояние до
// цели не уменьшается.
//
// Отсюда главное: УХОД В СТОРОНУ И ПРОСТОЙ — ОДНО ЯВЛЕНИЕ, а не два. Работа кипит,
// коммиты идут, формат хода безупречен — а расстояние стоит; молчащее бездействие даёт
// ту же картину. Мерить надо расстояние, а не признаки усердия: тогда обе беды ловятся
// одним числом и без догадок о намерении.
//
// ЭТО ЕДИНСТВЕННАЯ РЕАЛИЗАЦИЯ. До 14.09.2026 расстояние считалось в трёх местах:
// measureDrift, его построчная копия внутри cmdDrift и скрипт goal-drift.ps1, — и все
// три писали в один журнал истории. Поправить формулу в одном месте значило получить
// двигатель, который судит один журнал двумя правилами. Теперь cmdDrift зовёт
// measureDrift, а скрипт только доставляет вызов до бинарника.

const (
	verdictAllow    = "ALLOW"
	verdictThrottle = "THROTTLE"
	verdictEscalate = "ESCALATE"
	verdictBlocked  = "ЖДЁТ ЛПР"
)

// distanceRule — ПРАВИЛО, ПО КОТОРОМУ ЗАПИСАН ЗАМЕР. Замеры разных правил не сравниваются.
//
// 14.09.2026 расстояние перестало быть суммой «остаток по судье + открытые шаги плана»
// (почему — у measureDrift). Прежние строки истории записаны суммой, новые — остатком по
// судье, и сравнивать их — всё равно что сравнивать метры с шагами.
//
// Замер перед правкой показал, во что это обошлось бы: в истории ASW 14 строк, и остаток
// по судье в них всё время 1, пока сумма честно убывала. Перечитай старую историю по
// новому правилу — первый же замер после обновления насчитал бы застой 13 и вынес
// ESCALATE на продукте, который работает.
//
// Поэтому смена правила — ГРАНИЦА ИСТОРИИ: строки без этой метки при подсчёте застоя не
// читаются. Цена названа честно: один раз, на первом замере после обновления, застой
// начинается с нуля. Альтернатива хуже — перенести застой, насчитанный дефектным правилом:
// у AIR-ENV-002 эскалация была наработана именно им.
const distanceRule = "judge+closed"

// severity — строгость только растёт. Поздний вердикт не затирает раннего, иначе порядок
// правил в файле менял бы результат.
var severity = map[string]int{
	verdictAllow:    0,
	verdictBlocked:  1,
	verdictThrottle: 1,
	verdictEscalate: 2,
}

func harden(cur, cand string) string {
	if severity[cand] > severity[cur] {
		return cand
	}
	return cur
}

type driftMeasure struct {
	At                 string `json:"at"`
	Distance           *int   `json:"distance"`
	DistanceRule       string `json:"distance_rule"`
	JudgeDistance      *int   `json:"judge_distance"`
	JudgeCode          *int   `json:"judge_code"`
	PlanOpenSteps      *int   `json:"plan_open_steps"`
	PlanClosedSteps    *int   `json:"plan_closed_steps"`
	PlanGates          int    `json:"plan_gates"`
	// LPRGates/CriteriaGated — К40: то же PlanState, что и у судьи, взятое из ОДНОГО и того
	// же machineVerdict (.goal-verdict.json), а не пересчитанное здесь заново. GATED здесь
	// не входит ни в JudgeDistance (судья уже исключил гейты из distance), ни в PlanOpenSteps.
	LPRGates           int      `json:"lpr_gates"`
	CriteriaGated      []string `json:"criteria_gated,omitempty"`
	// CriteriaUnknown/FactsOverlap — К59: technical unknowns и предупреждение о legacy
	// overlap, тем же machineVerdict, что и LPRGates выше.
	CriteriaUnknown    []string `json:"criteria_unknown,omitempty"`
	FactsOverlap       []string `json:"facts_overlap,omitempty"`
	StallMoves         int    `json:"stall_moves"`
	UnverifiableStreak int    `json:"unverifiable_streak"`
	Verdict            string `json:"verdict"`
	Note               string `json:"note"`
	By                 string `json:"by"`
}

// driftPoint — один замер в том виде, в каком по нему судится движение.
//
// Judge — остаток по судье; nil значит «неизмеримо» (судья с кодом 2), и это не ноль.
// Closed и Open — шаги плана; nil значит, что плана не было.
type driftPoint struct {
	Judge  *int
	Closed *int
	Open   *int
}

type driftReason struct {
	Rule    string `json:"rule"`
	Verdict string `json:"verdict"`
	Why     string `json:"why"`
}

type driftOutcome struct {
	Measure driftMeasure
	Reasons []driftReason
	Limits  []string
}

type driftLimits struct {
	StallThrottle    int
	StallEscalate    int
	UnverifiableStop int
}

// defaultLimits — пороги ЗАШИТЫ в механизм. Решение ЛПР 12.09.2026 по лестнице и порогам.
// Перекрываются разделом goal_drift в run-config.json, если продукту нужно иное.
func defaultLimits() driftLimits {
	return driftLimits{StallThrottle: 3, StallEscalate: 6, UnverifiableStop: 2}
}

func (l driftLimits) withConfig(c driftThresholds) driftLimits {
	if c.StallThrottle != nil {
		l.StallThrottle = *c.StallThrottle
	}
	if c.StallEscalate != nil {
		l.StallEscalate = *c.StallEscalate
	}
	if c.UnverifiableStop != nil {
		l.UnverifiableStop = *c.UnverifiableStop
	}
	return l
}

// moved — сдвинулась ли работа к цели между двумя измеримыми замерами.
//
// Признаков два. Остаток по судье уменьшился — это движение всегда. Число ЗАКРЫТЫХ шагов
// плана выросло — тоже движение, и этот второй признак нужен продуктам, чей судья видит
// план одной проверкой: у ASW остаток стоит на единице до последнего шага, и закрытие шага
// — единственное, что двигается.
//
// Считаются именно закрытые, а не открытые шаги: число открытых растёт и от уточнения
// плана, а закрыть шаг можно только работой.
//
// ДВИЖЕНИЕ НЕ БЫВАЕТ ОДНОВРЕМЕННО РЕГРЕССОМ. До 0.9.5 замер, где шаг закрылся, а остаток
// по судье вырос, судился дважды и противоположно: движением (застой в ноль) и регрессом
// (WORKER-DRIFT-03). Найдено чтением 14.09.2026, при разборе живого дефекта петли на ASW:
// исполнитель внёс пакет в список проверки и сломал сборку. Зачеркни он тем же ходом номер
// шага — сломанная сборка сбросила бы застой. Теперь движение — улучшение одного признака
// при том, что второй не хуже.
func moved(prev, cur driftPoint) bool {
	if regression(prev, cur) != "" {
		return false
	}
	if *cur.Judge < *prev.Judge {
		return true
	}
	return prev.Closed != nil && cur.Closed != nil && *cur.Closed > *prev.Closed
}

// regression — чем cur хуже prev, словами; пустая строка — не хуже. Регресс — рост
// остатка по судье либо меньшее число закрытых шагов плана. Новые строки плана регрессом
// не являются: уточнение пути откатом не является. Определение одно — для двигателя по
// истории и для петли по итерации.
func regression(prev, cur driftPoint) string {
	if prev.Judge == nil || cur.Judge == nil {
		return ""
	}
	switch {
	case *cur.Judge > *prev.Judge:
		return fmt.Sprintf("остаток по судье вырос с %d до %d: закрытое ранее перестало быть закрытым",
			*prev.Judge, *cur.Judge)
	case cur.Closed != nil && prev.Closed != nil && *cur.Closed < *prev.Closed:
		return fmt.Sprintf("закрытых шагов плана стало меньше — было %d, стало %d: закрытый шаг снова открыт",
			*prev.Closed, *cur.Closed)
	}
	return ""
}

// refinedPlan — план уточнён, а не откачен: открытых шагов стало больше, закрытых не
// меньше, остаток по судье не вырос. Это разбиение шага или добавление недостающего, то
// есть ровно то, что предписывает THROTTLE.
func refinedPlan(prev, cur driftPoint) bool {
	if prev.Open == nil || cur.Open == nil || *cur.Open <= *prev.Open {
		return false
	}
	if *cur.Judge > *prev.Judge {
		return false
	}
	if prev.Closed != nil && cur.Closed != nil && *cur.Closed < *prev.Closed {
		return false
	}
	return true
}

// countStreaks — застой и слепота считаются ПО ИСТОРИИ, то есть по факту записанных
// чисел, а не по тому, что сессия о себе сообщила.
//
// Текущий замер учитывается наравне с историей: иначе о последнем ходе судили бы только
// на следующем, и ровно он оставался бы безнаказанным.
//
// УТОЧНЕНИЕ ПЛАНА БЕСПЛАТНО ОДИН РАЗ ЗА ЦИКЛ — от движения до движения. Найдено
// AIR-ENV-002: THROTTLE предписывает сузить шаг, она сузила, и замер после сужения
// засчитался застоем — предписанное лекарство приближало эскалацию. Механизм не вправе
// брать плату за то, что сам велел сделать. Но второе уточнение подряд без движения
// засчитывается застоем: иначе описание пути можно было бы наращивать вместо того, чтобы
// идти по нему, — ровно «рост промежуточного числа», против которого двигатель заведён.
func countStreaks(history []driftPoint, current driftPoint) (stall, unverifiable int) {
	var prev *driftPoint
	refined := false
	step := func(p driftPoint) {
		if p.Judge == nil {
			unverifiable++
			return
		}
		unverifiable = 0
		if prev != nil {
			switch {
			case moved(*prev, p):
				stall, refined = 0, false
			case refinedPlan(*prev, p) && !refined:
				refined = true
			default:
				stall++
			}
		}
		pp := p
		prev = &pp
	}
	for _, p := range history {
		step(p)
	}
	step(current)
	return
}

// lastKnown — последний измеримый замер истории либо nil.
func lastKnown(history []driftPoint) *driftPoint {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Judge != nil {
			p := history[i]
			return &p
		}
	}
	return nil
}

// evaluate — правила. Чистая функция: всё, что она знает, приходит аргументами, поэтому
// её поведение проверяется тестом, а не прогоном на живом продукте.
func evaluate(cur driftPoint, judgeCode *int, planOpen *int, history []driftPoint, lim driftLimits) (string, []driftReason) {
	v := verdictAllow
	var reasons []driftReason
	fire := func(rule, cand, why string) {
		v = harden(v, cand)
		reasons = append(reasons, driftReason{Rule: rule, Verdict: cand, Why: why})
	}

	stall, unverifiable := countStreaks(history, cur)

	// НУЛЕВОЕ РАССТОЯНИЕ НЕ МОЖЕТ ЗАСТАИВАТЬСЯ: двигать уже нечего.
	//
	// Найдено AIR-ENV-002 и названо ею мелочью оформления. Мелочью это не было:
	// расстояние ноль не уменьшается никогда, а счётчик застоя растёт при каждом «не
	// меньше предыдущего», — значит продукт с исчерпанной работой получал бы эскалацию за
	// то, что работа кончилась.
	workExhausted := cur.Judge != nil && *cur.Judge == 0
	if workExhausted {
		switch {
		case judgeCode != nil && *judgeCode == 0 && planOpen != nil && *planOpen > 0:
			// WORKER-DRIFT-04 — СУДЬЯ ДОВОЛЕН, А ПЛАН ГОВОРИТ ИНОЕ. Пока шаги плана
			// складывались в расстояние, этот случай давал ненулевое расстояние сам собой.
			// Сумма ушла — защита обязана остаться и остаётся явным правилом. У ASW было
			// ровно так: судья «16 из 16» при 24 открытых шагах.
			// СОВЕТ «РАСШИРИТЬ СУДЬЮ» АДРЕСОВАН ЛПР, А НЕ СЕССИИ (этап 0.10, К10). Прежняя
			// формулировка «Расширить судью на эти шаги» читалась как задание — и 14.09.2026
			// сессия Vera по ней сама дописала проверку в судью своего продукта. ЛПР: «пир полез
			// в код судьи, зачем нам такое поведение?»
			fire("WORKER-DRIFT-04", verdictBlocked, fmt.Sprintf(
				"судья доволен (остаток ноль, код 0), а в плане открытых исполняемых шагов %d: судья "+
					"не видит этой работы. Работой это не лечится — её закрытия судья не заметит. "+
					"Расширять судью — решение ЛПР, а не работа сессии: шаг-гейт в плане и его слово "+
					"(mode.ps1 -JudgeGate); либо шаги закрываются в плане", *planOpen))
		case judgeCode != nil && *judgeCode == 0:
			reasons = append(reasons, driftReason{
				Rule: "WORKER-DRIFT-00", Verdict: verdictAllow,
				Why: "цель достигнута: остаток по судье ноль, судья согласен, открытых исполняемых шагов плана нет"})
		default:
			code := "неизвестен"
			if judgeCode != nil {
				code = fmt.Sprintf("%d", *judgeCode)
			}
			fire("WORKER-DRIFT-00", verdictBlocked, fmt.Sprintf(
				"работой закрывать нечего — расстояние ноль, — но судья цель не подтвердил (код %s). "+
					"Значит остаток держат гейты ЛПР либо реестр не покрывает того, что судья требует. "+
					"Ни то, ни другое не лечится следующей итерацией.", code))
		}
	}

	// WORKER-DRIFT-01 — ходы идут, а работа к цели не движется. Это и уход в сторону, и простой.
	if !workExhausted {
		if stall >= lim.StallEscalate {
			fire("WORKER-DRIFT-01", verdictEscalate, fmt.Sprintf(
				"замеров подряд без движения к цели — ни остаток по судье не уменьшился, ни шаг плана "+
					"не закрылся — %d при пороге %d: работа идёт мимо цели, и продолжать её тем же "+
					"способом значит платить за то же ещё раз", stall, lim.StallEscalate))
		} else if stall >= lim.StallThrottle {
			fire("WORKER-DRIFT-01", verdictThrottle, fmt.Sprintf(
				"замеров подряд без движения к цели — %d при пороге %d: следующий ход обязан "+
					"сузить шаг или назвать, почему он не двигает работу. Одно уточнение плана "+
					"застоем не считается", stall, lim.StallThrottle))
		}
	}

	// WORKER-DRIFT-02 — «проверить не могу» подряд. Это не «чисто».
	if unverifiable >= lim.UnverifiableStop {
		fire("WORKER-DRIFT-02", verdictEscalate, fmt.Sprintf(
			"расстояние неизмеримо подряд %d раз при пороге %d: у механизма нет достижимого "+
				"состояния «проверено», и работа идёт вслепую", unverifiable, lim.UnverifiableStop))
	}

	// WORKER-DRIFT-03 — РЕГРЕСС. Отдельно от застоя: застой — это ноль движения, а регресс
	// означает, что сделанное разломало уже закрытое.
	//
	// Регресс — рост остатка по судье либо меньшее число закрытых шагов плана. НЕ регресс —
	// появление новых строк плана: уточнение пути откатом не является. Прежде этим правилом
	// наказывалось разбиение шага, потому что оно увеличивало сумму.
	if last := lastKnown(history); last != nil {
		if why := regression(*last, cur); why != "" {
			fire("WORKER-DRIFT-03", verdictThrottle, why)
		}
	}

	return v, reasons
}

// readHistory — замеры ТЕКУЩЕГО правила. Строки прежнего правила пропускаются: см.
// distanceRule о том, почему смена правила — граница истории.
func readHistory(path string) []driftPoint {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []driftPoint
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var row struct {
			Rule   string `json:"distance_rule"`
			Judge  *int   `json:"judge_distance"`
			Open   *int   `json:"plan_open_steps"`
			Closed *int   `json:"plan_closed_steps"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, string(utf8BOM))), &row) != nil {
			continue
		}
		if row.Rule != distanceRule {
			continue
		}
		out = append(out, driftPoint{Judge: row.Judge, Closed: row.Closed, Open: row.Open})
	}
	return out
}

// driftOnce — один замер двигателя цели ВНУТРИ процесса, без подпроцесса.
//
// Заведено переносом петли в бинарник: прежде петля звала goal-drift.ps1 отдельным
// процессом на каждой итерации. Подпроцесс здесь был бы платой за то, что уже лежит под
// рукой, — а ходы и есть цена.
//
// Возвращает замер и код: 0 ALLOW, 1 THROTTLE, 2 ESCALATE, 3 ЖДЁТ ЛПР. Замер нужен петле:
// по нему, а не по тексту вердикта, она судит, сдвинула ли итерация цель.
func driftOnce(root string, record bool, note string) (driftMeasure, int) {
	m, _, _ := measureDrift(root, note)
	if record {
		recordMeasure(root, m)
	}
	return m, verdictExitCode(m.Verdict)
}

// pointOf — точка для сравнения замеров: те же три числа, что двигатель пишет в историю.
func pointOf(m driftMeasure) driftPoint {
	return driftPoint{Judge: m.JudgeDistance, Closed: m.PlanClosedSteps, Open: m.PlanOpenSteps}
}

// verdictExitCode — код 3 у «ждёт ЛПР» отдельный от торможения намеренно. Механизм,
// читающий один код, обязан различать «сузь шаг» и «работы не осталось»: первое лечится
// следующим ходом, второе — только человеком.
func verdictExitCode(v string) int {
	switch v {
	case verdictEscalate:
		return 2
	case verdictBlocked:
		return 3
	case verdictThrottle:
		return 1
	}
	return 0
}

func recordMeasure(root string, m driftMeasure) {
	// История ДОПИСЫВАЕТСЯ и не переписывается: по ней видно не только что стояли, но и
	// НА ЧТО ушли ходы, простоявшие мимо цели.
	histDir := filepath.Join(root, ".woody")
	_ = os.MkdirAll(histDir, 0o755)
	b, err := json.Marshal(m)
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(histDir, "goal-drift.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\r', '\n'))
}

// measureDrift — сбор фактов и применение правил. Отделено от вывода намеренно: то, что
// считает, и то, что печатает, — разные обязанности, и смешение их мешает проверить
// первое тестом.
func measureDrift(root, note string) (driftMeasure, []driftReason, []string) {
	lim := defaultLimits()
	var cfg runConfig
	if readJSON(filepath.Join(root, "run-config.json"), &cfg) == nil {
		lim = lim.withConfig(cfg.GoalDrift)
	}

	var limits []string
	var judgeDistance, judgeCode *int
	var mv machineVerdict
	vPath := filepath.Join(root, ".goal-verdict.json")
	if err := readJSON(vPath, &mv); err != nil {
		limits = append(limits, fmt.Sprintf(
			"машинного вердикта нет (%s): расстояние по судье не измеряется. Судья должен быть прогнан хотя бы раз.", vPath))
	} else {
		judgeCode = intPtr(mv.Code)
		if mv.Distance != nil {
			judgeDistance = intPtr(*mv.Distance)
		} else {
			limits = append(limits, "судья вернул «нечем проверить»: расстояние неизвестно, а не ноль")
		}
	}

	// ПУТЬ ПЛАНА — ИЗ run-config.json, А НЕ ПРИБИТ (этап 0.10, К32). Было
	// filepath.Join(root, "PLAN.md") мимо cfg.Plan: на продукте с именем плана, отличным
	// от умолчания, двигатель мерил не тот файл, что судья и петля. planFilePath —
	// planrequire.go, та же функция, что берёт путь плана cmdJudge, cmdDrift и cmdReport.
	planPath := planFilePath(root, cfg)
	plan := parsePlan(planPath)
	var planOpen, planClosed *int
	if !plan.Found {
		limits = append(limits, fmt.Sprintf(
			"плана нет (%s): шаги плана не измеряются, движение судится только остатком по судье", planPath))
	} else {
		planOpen = intPtr(plan.OpenWork())
		if mv.CriteriaTotal > 0 {
			planClosed = intPtr(confirmedClosedSteps(planPath, mv.CriteriaPassed))
		} else {
			planClosed = intPtr(plan.ClosedSteps())
		}
	}

	// РАССТОЯНИЕ — ОСТАТОК ПО СУДЬЕ. ШАГИ ПЛАНА — ОТДЕЛЬНЫЙ ПОКАЗАТЕЛЬ И ВТОРОЙ ПРИЗНАК
	// ДВИЖЕНИЯ, А НЕ СЛАГАЕМОЕ.
	//
	// До 14.09.2026 здесь складывалось: остаток по судье + открытые шаги плана. Сложение
	// было ошибкой, и нашли её с двух сторон.
	//
	// AIR-ENV-002, на живом продукте: расстояние 6, застой 3, THROTTLE. THROTTLE по нашей
	// же таблице означает «сузить шаг» — она сузила, разбив слипшийся шаг на два, и
	// получила расстояние 7, застой 6, ESCALATE. Новая строка плана дала +1 к сумме, и
	// единственное действие, которое THROTTLE называет верным, приблизило эскалацию.
	// Складывались разнородные числа: остаток до ЦЕЛИ и длина ОПИСАНИЯ пути, — подробный
	// план всегда выходил «дальше», чем крупноблочный, при одинаковой работе.
	//
	// Замер на ASW перед правкой закрыл обратную дверь: 24 = 1 + 23. Судья ASW видит план
	// одной проверкой («план пройден»), и если просто выкинуть план из расстояния, остаток
	// стоит на единице все двадцать три шага — каждый честно закрытый шаг копил бы застой.
	// Зеркальный дефект тому, что нашла AIR-ENV-002.
	//
	// Отсюда устройство:
	//   - расстояние, которое печатается и сверяется стражем, — остаток по судье;
	//   - движение — уменьшение остатка по судье ИЛИ рост числа ЗАКРЫТЫХ шагов плана;
	//   - уточнение плана — не движение и не застой, один раз за цикл;
	//   - регресс — рост остатка по судье или повторно открытый шаг, но не новые строки.
	//
	// Защита «судья доволен, а план говорит иное», ради которой сумма когда-то вводилась,
	// не теряется: при нулевом остатке и открытых исполняемых шагах двигатель выносит
	// «ЖДЁТ ЛПР» (WORKER-DRIFT-04), а петля отказывается работать отдельной проверкой.
	var distance *int
	if judgeDistance != nil {
		distance = intPtr(*judgeDistance)
	}
	cur := driftPoint{Judge: judgeDistance, Closed: planClosed, Open: planOpen}

	history := readHistory(filepath.Join(root, ".woody", "goal-drift.jsonl"))
	v, reasons := evaluate(cur, judgeCode, planOpen, history, lim)
	stall, unverifiable := countStreaks(history, cur)

	return driftMeasure{
		At:                 time.Now().Format("2006-01-02T15:04:05"),
		Distance:           distance,
		DistanceRule:       distanceRule,
		JudgeDistance:      judgeDistance,
		JudgeCode:          judgeCode,
		PlanOpenSteps:      planOpen,
		PlanClosedSteps:    planClosed,
		PlanGates:          plan.Gates(),
		LPRGates:           mv.LPRGates,
		CriteriaGated:      mv.CriteriaGated,
		CriteriaUnknown:    mv.CriteriaUnknown,
		FactsOverlap:       mv.FactsOverlap,
		StallMoves:         stall,
		UnverifiableStreak: unverifiable,
		Verdict:            v,
		Note:               note,
		By:                 appName + " " + version,
	}, reasons, limits
}

func cmdDrift(argv []string) int {
	fs := flag.NewFlagSet("drift", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	record := fs.Bool("record", false, "дописать замер в историю")
	note := fs.String("note", "", "чем был ход")
	asJSON := fs.Bool("json", false, "машинный вывод")
	quiet := fs.Bool("quiet", false, "молча, только код возврата")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Printf("НЕЧЕМ МЕРИТЬ: не разобран путь продукта: %v\n", err)
		return 2
	}
	if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
		fmt.Printf("НЕЧЕМ МЕРИТЬ: нет каталога продукта %s\n", root)
		return 2
	}

	// ПЛАН КАК ФАЙЛ ОБЯЗАТЕЛЕН И ЗДЕСЬ (этап 0.10, К32). До этой правки двигатель без
	// плана всё равно считал расстояние и называл отсутствие плана лишь в «ограничениях»
	// замера — как будто это меньше, чем отсутствие пары строк вывода. Конфигурация
	// читается здесь, а не молча внутри measureDrift: путь плана нужен ДО замера, нет
	// смысла мерить то, что сам механизм не считает годным к работе.
	var cfg runConfig
	_ = readJSON(filepath.Join(root, "run-config.json"), &cfg)
	if _, code, ok := requirePlan(root, cfg, "двигатель цели"); !ok {
		return code
	}

	// ЗАМЕР — ТОТ ЖЕ, ЧТО У ПЕТЛИ И ОТЧЁТА. Здесь была построчная копия measureDrift, и
	// правка расстояния 14.09.2026 упёрлась ровно в неё: поправить одно место из двух
	// значило развести `drift -json` с петлёй, то есть сделать хуже, чем было.
	m, reasons, limits := measureDrift(root, *note)
	if *record {
		recordMeasure(root, m)
	}
	// ИДЁТ ЛИ ПЕТЛЯ ПО ПРОДУКТУ — только в выводе, не в истории: это состояние машины в
	// миг замера, а не свойство продукта. Страж хода узнаёт по нему, что дерево и вердикт
	// меняются под работой петли и сверять ход не с чем.
	loopRunning := lockHeld(lockName("loop", root))

	switch {
	case *asJSON:
		out := struct {
			driftMeasure
			LoopRunning bool          `json:"loop_running"`
			Reasons     []driftReason `json:"reasons"`
			Limits      []string      `json:"limits"`
		}{m, loopRunning, reasons, limits}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	case !*quiet:
		dText := "НЕИЗВЕСТНО"
		if m.Distance != nil {
			dText = fmt.Sprintf("%d", *m.Distance)
		}
		pText := "плана нет"
		if m.PlanOpenSteps != nil && m.PlanClosedSteps != nil {
			pText = fmt.Sprintf("открытых исполняемых шагов %d, закрытых %d, гейтов ЛПР %d",
				*m.PlanOpenSteps, *m.PlanClosedSteps, m.PlanGates)
		}
		fmt.Printf("вердикт двигателя цели: %s\n", m.Verdict)
		fmt.Printf("  расстояние до цели (остаток по судье): %s\n", dText)
		fmt.Printf("  план: %s\n", pText)
		fmt.Printf("  замеров подряд без движения: %d; неизмеримо подряд: %d\n", m.StallMoves, m.UnverifiableStreak)
		if loopRunning {
			fmt.Println("  петля по продукту идёт в другом процессе: дерево и вердикт меняются под её работой")
		}
		for _, r := range reasons {
			fmt.Printf("  [%s] %s: %s\n", r.Rule, r.Verdict, r.Why)
		}
		for _, l := range limits {
			fmt.Printf("  ограничение: %s\n", l)
		}
		if len(reasons) == 0 {
			fmt.Println("  правил не сработало")
		}
	}

	return verdictExitCode(m.Verdict)
}
