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

const (
	verdictAllow    = "ALLOW"
	verdictThrottle = "THROTTLE"
	verdictEscalate = "ESCALATE"
	verdictBlocked  = "ЖДЁТ ЛПР"
)

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
	JudgeDistance      *int   `json:"judge_distance"`
	JudgeCode          *int   `json:"judge_code"`
	PlanOpenSteps      *int   `json:"plan_open_steps"`
	PlanGates          int    `json:"plan_gates"`
	StallMoves         int    `json:"stall_moves"`
	UnverifiableStreak int    `json:"unverifiable_streak"`
	Verdict            string `json:"verdict"`
	Note               string `json:"note"`
	By                 string `json:"by"`
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

// countStreaks — застой и слепота считаются ПО ИСТОРИИ, то есть по факту записанных
// чисел, а не по тому, что сессия о себе сообщила.
//
// Текущий замер учитывается наравне с историей: иначе о последнем ходе судили бы только
// на следующем, и ровно он оставался бы безнаказанным.
func countStreaks(history []*int, current *int) (stall, unverifiable int) {
	var prev *int
	for _, d := range history {
		if d == nil {
			unverifiable++
			continue
		}
		unverifiable = 0
		if prev != nil {
			if *d < *prev {
				stall = 0
			} else {
				stall++
			}
		}
		v := *d
		prev = &v
	}
	if current == nil {
		unverifiable++
		return
	}
	unverifiable = 0
	if prev != nil {
		if *current < *prev {
			stall = 0
		} else {
			stall++
		}
	}
	return
}

// evaluate — правила. Чистая функция: всё, что она знает, приходит аргументами, поэтому
// её поведение проверяется тестом, а не прогоном на живом продукте.
func evaluate(distance *int, judgeCode *int, history []*int, lim driftLimits) (string, []driftReason) {
	v := verdictAllow
	var reasons []driftReason
	fire := func(rule, cand, why string) {
		v = harden(v, cand)
		reasons = append(reasons, driftReason{Rule: rule, Verdict: cand, Why: why})
	}

	stall, unverifiable := countStreaks(history, distance)

	// НУЛЕВОЕ РАССТОЯНИЕ НЕ МОЖЕТ ЗАСТАИВАТЬСЯ: двигать уже нечего.
	//
	// Найдено AIR-ENV-002 и названо ею мелочью оформления. Мелочью это не было:
	// расстояние ноль не уменьшается никогда, а счётчик застоя растёт при каждом «не
	// меньше предыдущего», — значит продукт с исчерпанной работой получал бы эскалацию за
	// то, что работа кончилась.
	workExhausted := distance != nil && *distance == 0
	if workExhausted {
		if judgeCode != nil && *judgeCode == 0 {
			reasons = append(reasons, driftReason{
				Rule: "WORKER-DRIFT-00", Verdict: verdictAllow,
				Why: "цель достигнута: расстояние ноль и судья согласен"})
		} else {
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

	// WORKER-DRIFT-01 — ходы идут, расстояние стоит. Это и уход в сторону, и простой.
	if !workExhausted {
		if stall >= lim.StallEscalate {
			fire("WORKER-DRIFT-01", verdictEscalate, fmt.Sprintf(
				"замеров подряд без уменьшения расстояния — %d при пороге %d: работа идёт мимо цели, "+
					"и продолжать её тем же способом значит платить за то же ещё раз", stall, lim.StallEscalate))
		} else if stall >= lim.StallThrottle {
			fire("WORKER-DRIFT-01", verdictThrottle, fmt.Sprintf(
				"замеров подряд без уменьшения расстояния — %d при пороге %d: следующий ход обязан "+
					"сузить шаг или назвать, почему он не двигает расстояние", stall, lim.StallThrottle))
		}
	}

	// WORKER-DRIFT-02 — «проверить не могу» подряд. Это не «чисто».
	if unverifiable >= lim.UnverifiableStop {
		fire("WORKER-DRIFT-02", verdictEscalate, fmt.Sprintf(
			"расстояние неизмеримо подряд %d раз при пороге %d: у механизма нет достижимого "+
				"состояния «проверено», и работа идёт вслепую", unverifiable, lim.UnverifiableStop))
	}

	// WORKER-DRIFT-03 — расстояние ВЫРОСЛО. Отдельно от застоя: застой это ноль движения,
	// а рост означает, что сделанное разломало уже закрытое.
	if distance != nil {
		var prev *int
		for _, d := range history {
			if d != nil {
				prev = d
			}
		}
		if prev != nil && *distance > *prev {
			fire("WORKER-DRIFT-03", verdictThrottle, fmt.Sprintf(
				"расстояние выросло с %d до %d: закрытое ранее перестало быть закрытым", *prev, *distance))
		}
	}

	return v, reasons
}

func readHistory(path string) []*int {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []*int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var m driftMeasure
		if json.Unmarshal([]byte(strings.TrimPrefix(line, string(utf8BOM))), &m) != nil {
			continue
		}
		out = append(out, m.Distance)
	}
	return out
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

	lim := defaultLimits()
	var cfg runConfig
	if readJSON(filepath.Join(root, "run-config.json"), &cfg) == nil {
		lim = lim.withConfig(cfg.GoalDrift)
	}

	var limits []string

	// Расстояние по судье берётся ФАКТОМ из машинного вердикта, а не разбором его текста.
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

	planPath := filepath.Join(root, "PLAN.md")
	plan := parsePlan(planPath)
	var planOpen *int
	if !plan.Found {
		limits = append(limits, fmt.Sprintf("плана нет (%s): расстояние по шагам не измеряется", planPath))
	} else {
		planOpen = intPtr(plan.OpenWork())
	}

	// Непроверенное НЕ СВОРАЧИВАЕТСЯ В НОЛЬ и не складывается: если хоть одна часть
	// неизвестна, неизвестно и целое. Иначе пропажа судьи выглядела бы как приближение.
	var distance *int
	if judgeDistance != nil && planOpen != nil {
		distance = intPtr(*judgeDistance + *planOpen)
	}

	histDir := filepath.Join(root, ".woody")
	histPath := filepath.Join(histDir, "goal-drift.jsonl")
	history := readHistory(histPath)

	v, reasons := evaluate(distance, judgeCode, history, lim)
	stall, unverifiable := countStreaks(history, distance)

	m := driftMeasure{
		At:                 time.Now().Format("2006-01-02T15:04:05"),
		Distance:           distance,
		JudgeDistance:      judgeDistance,
		JudgeCode:          judgeCode,
		PlanOpenSteps:      planOpen,
		PlanGates:          plan.Gates(),
		StallMoves:         stall,
		UnverifiableStreak: unverifiable,
		Verdict:            v,
		Note:               *note,
		By:                 appName + " " + version,
	}

	if *record {
		// История ДОПИСЫВАЕТСЯ и не переписывается: по ней видно не только что стояли,
		// но и НА ЧТО ушли ходы, простоявшие мимо цели.
		_ = os.MkdirAll(histDir, 0o755)
		if b, err := json.Marshal(m); err == nil {
			if f, err := os.OpenFile(histPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
				_, _ = f.Write(append(b, '\r', '\n'))
				_ = f.Close()
			}
		}
	}

	switch {
	case *asJSON:
		out := struct {
			driftMeasure
			Reasons []driftReason `json:"reasons"`
			Limits  []string      `json:"limits"`
		}{m, reasons, limits}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
	case !*quiet:
		dText := "НЕИЗВЕСТНО"
		if distance != nil {
			dText = fmt.Sprintf("%d", *distance)
		}
		jText, pText := "?", "?"
		if judgeDistance != nil {
			jText = fmt.Sprintf("%d", *judgeDistance)
		}
		if planOpen != nil {
			pText = fmt.Sprintf("%d", *planOpen)
		}
		fmt.Printf("вердикт двигателя цели: %s\n", v)
		fmt.Printf("  расстояние до цели: %s (судья %s + открытых шагов плана %s)\n", dText, jText, pText)
		fmt.Printf("  замеров подряд без движения: %d; неизмеримо подряд: %d\n", stall, unverifiable)
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

	// Код 3 у «ждёт ЛПР» отдельный от торможения намеренно. Механизм, читающий один код,
	// обязан различать «сузь шаг» и «работы не осталось»: первое лечится следующим ходом,
	// второе — только человеком.
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
