package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Петля: берёт следующий незакрытый шаг плана, исполняет его на назначенной ступени,
// спрашивает судью и пишет замер. Поднимает ступень ТОЛЬКО когда судья не сдвинулся.
//
// Порядок рычагов обратному не подлежит: не звать модель -> резать ходы -> и только потом
// поднимать ступень. Основание — замер 11.08.2026: цена определяется числом ходов, а не
// выбором модели, 23 137 токенов перечитывания кэша на каждый токен выхода.

type stepResult struct {
	Ok      bool
	Cost    *float64 // nil — «не измерено». Ноль означал бы «бесплатно», и бюджет считался бы в сторону «можно ещё»
	Turns   *int
	Session string
	Subtype string
	ApiMs   *int
	Detail  string // текст отказа исполнителя, если он был
}

type loopCtx struct {
	Root        string
	Cfg         runConfig
	CfgPath     string
	Ladder      []string
	MaxIter     int
	MaxUSD      float64
	MaxTurns    int
	StallLimit  int
	Orchestrate bool
	Subagents   int
	StepsPath   string
	WhatIf      bool
	JudgePath   string
	JudgeArgs   []string
	Permission  string

	spent       float64
	iter        int
	stalledRuns int
}

func line(text string) { fmt.Print(time.Now().Format("15:04:05") + "  " + text + lineEnding) }

// status — формат согласован ЛПР (AIR_VIBECODING v1.49) и не меняется под предлогом
// экономии токенов или удобства чтения.
func status(phase int, title, tier, who, state string) {
	fmt.Printf("Фаза %d · %s · ступень %s · %s · %s"+lineEnding, phase, title, tier, who, state)
}

// measure — «не измерено» и «ноль» показываются РАЗНЫМИ словами. Прежняя версия
// подставляла ноль вместо отсутствующего значения, и строка замера утверждала, что работа
// была и стоила нисколько: семь строк сухого прогона читались как семь бесплатных итераций.
func measure(iter int, cost *float64, turns *int, total, budget float64) {
	c := "не измерено"
	if cost != nil {
		c = fmt.Sprintf("$%.4f", *cost)
	}
	t := "не измерено"
	if turns != nil {
		t = fmt.Sprintf("%d", *turns)
	}
	fmt.Printf("Замер: итерация %d · %s · ходов %s · всего $%.2f из бюджета $%.0f"+lineEnding,
		iter, c, t, total, budget)
}

// closeWoody — каждое завершение заканчивается двумя строками. «Не жду ничего» пишется
// прямо: молчание в конце читается как незаданный вопрос.
func closeWoody(next, await string, code int) {
	fmt.Print(lineEnding)
	fmt.Print("Дальше: " + next + lineEnding)
	fmt.Print("От тебя жду: " + await + lineEnding)
	os.Exit(code)
}

func cmdLoop(argv []string) int {
	fs := flag.NewFlagSet("loop", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	configPath := fs.String("config", "", "путь к run-config.json")
	planOnly := fs.Bool("plan-only", false, "разобрать план и судью, дальше не идти")
	whatIf := fs.Bool("whatif", false, "сухой прогон: модель не зовётся, расход не считается")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		line("ОТКАЗ: не разобран путь продукта: " + err.Error())
		return 2
	}
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(root, "run-config.json")
	}
	var cfg runConfig
	if err := readJSON(cfgPath, &cfg); err != nil {
		line("ОТКАЗ: нет " + cfgPath + ". Образец — run-config.example.json в скиле.")
		return 2
	}
	if len(cfg.Judge.Checks) == 0 && cfg.Judge.Checklist == "" && cfg.Judge.Path == "" {
		line("ОТКАЗ: в конфигурации нет раздела judge. Без судьи Дятел не работает.")
		return 3
	}

	c := &loopCtx{
		Root: root, Cfg: cfg, CfgPath: cfgPath, WhatIf: *whatIf,
		Ladder:      cfg.Ladder,
		MaxIter:     orInt(cfg.Budget.Iterations, 12),
		MaxUSD:      orFloat(cfg.Budget.USD, 20),
		MaxTurns:    orInt(cfg.Budget.TurnsPerIteration, 60),
		StallLimit:  orInt(cfg.Budget.StallRuns, 3),
		Orchestrate: cfg.Orchestration.Enabled,
		Subagents:   orInt(cfg.Orchestration.Subagents, 2),
		StepsPath:   filepath.Join(root, "steps.jsonl"),
		Permission:  cfg.Runner.PermissionMode,
	}
	if c.Permission == "" {
		c.Permission = "acceptEdits"
	}
	if len(c.Ladder) == 0 {
		c.Ladder = []string{"script", "haiku", "sonnet", "opus"}
	}
	if cfg.Judge.Path != "" {
		c.JudgePath = filepath.Join(root, cfg.Judge.Path)
		c.JudgeArgs = cfg.Judge.Args
		if _, err := os.Stat(c.JudgePath); err != nil {
			line("ОТКАЗ: судья не найден — " + c.JudgePath)
			return 3
		}
	}

	planName := cfg.Plan
	if planName == "" {
		planName = "PLAN.md"
	}
	planPath := filepath.Join(root, planName)

	// ЗАГОТОВКА — НЕ ПЛАН. Сразу после -Init на пустом продукте петля печатала «шагов в
	// плане: 7, из них закрыто 1» — это разобранный ОБРАЗЕЦ. Новый продукт показывал
	// прогресс, которого нет. Маркер снимается человеком вместе с примерами, и это и есть
	// признак, что план заполняли, а не скопировали.
	if raw, err := os.ReadFile(planPath); err == nil {
		if strings.Contains(string(raw), "ЗАГОТОВКА-НЕ-ЗАПОЛНЕНА") {
			line("ОТКАЗ: план не заполнен — " + planPath + " всё ещё содержит маркер заготовки.")
			line("Замени примеры своими шагами и удали строку с маркером. Пока она на месте,")
			line("петля считала бы прогрессом разобранные примеры шаблона.")
			return 2
		}
	}

	steps := readPlanSteps(planPath)
	if len(steps) == 0 {
		line("ОТКАЗ: план пуст или не найден — " + planPath)
		line("Без плана Дятел не начинает: разбивка на ступени и есть экономия.")
		return 4
	}

	line("продукт     : " + root)
	if c.JudgePath != "" {
		line("судья       : " + c.JudgePath)
	} else {
		line("судья       : встроенный (" + appName + " judge)")
	}
	// Счёт разложен так, чтобы он СХОДИЛСЯ с планом на глаз. Гейты названы отдельной
	// величиной: петля их не исполняет, но и не прячет.
	var gates, workable, done int
	for _, s := range steps {
		if s.Gate {
			gates++
		} else {
			workable++
		}
		if s.Done {
			done++
		}
	}
	line(fmt.Sprintf("шагов в плане: %d, из них закрыто %d; исполняемых %d, гейтов ЛПР %d",
		len(steps), done, workable, gates))
	for _, s := range steps {
		if s.Gate {
			line("  гейт ЛПР, петлёй не закрывается: " + s.Title)
		}
	}
	line(fmt.Sprintf("бюджет      : %d итераций, $%.0f, %d ходов на итерацию", c.MaxIter, c.MaxUSD, c.MaxTurns))
	// ПРАВА ИСПОЛНИТЕЛЯ НАЗЫВАЮТСЯ ВСЛУХ, до первой итерации. Отцеплённая модель сейчас
	// получит право править этот продукт, и человек обязан увидеть это заранее, а не по
	// следам в git.
	// Права называются вслух ДО первой итерации, и называются честно: объявленный режим
	// сам по себе НЕ даёт отцеплённому исполнителю писать файлы — это проверено прогоном.
	line("права исполн: " + c.Permission + " (полного отключения проверок нет — исполнитель писать не сможет, см. шаг плана)")
	if c.Orchestrate {
		line(fmt.Sprintf("оркестрация : включена, субагентов %d", c.Subagents))
	} else {
		line("оркестрация : выключена")
	}

	code, text := c.judge()
	line(fmt.Sprintf("судья до работы: код %d", code))
	line("  " + text)
	if code == 0 {
		// СУДЬЯ ОДИН НЕ РЕШАЕТ, ДОСТИГНУТА ЛИ ЦЕЛЬ. Найдено первым же настоящим вызовом
		// по чужому продукту 13.09.2026: у ASW судья отвечал «цель достигнута, фактов 16
		// из 16», а в плане стояло пятнадцать открытых исполняемых шагов. Петля выходила
		// со словами «работать не над чем» и не делала ничего.
		//
		// Это тот же класс, что мы вычищали весь день: два источника правды об одном, и
		// механизм берёт слабейший. Двигатель цели при этом считал верно — расстояние 15,
		// — но петля его до начала работы не спрашивала.
		//
		// Работать такие шаги петля тоже не может: судья их закрытия НЕ ВИДИТ, значит
		// вердикт не сдвинется ни от какой работы, и петля пойдёт вверх по лестнице,
		// оплачивая неподвижность всё дороже. Поэтому не победа и не работа, а НАЗВАННОЕ
		// РАСХОЖДЕНИЕ: судью надо расширить либо шаги закрыть.
		openWork := 0
		for _, s := range steps {
			if !s.Done && !s.Gate {
				openWork++
			}
		}
		if openWork > 0 {
			line("СУДЬЯ ДОВОЛЕН, А ПЛАН ГОВОРИТ ИНОЕ.")
			line(fmt.Sprintf("  судья: %s", text))
			line(fmt.Sprintf("  план : открытых исполняемых шагов %d", openWork))
			closeWoody("ничего: работой это не лечится — судья не видит того, что план считает несделанным",
				fmt.Sprintf("расширить судью на открытые шаги (их %d) либо закрыть их в плане. "+
					"Работать сейчас бессмысленно: закрытия этих шагов судья НЕ УВИДИТ, вердикт не "+
					"сдвинется, и петля полезет вверх по лестнице, оплачивая неподвижность дороже", openWork), 1)
		}
		line("ЦЕЛЬ УЖЕ ДОСТИГНУТА, работать не над чем.")
		return 0
	}
	// Код 2 — «проверять нечем». Работа здесь не помогает: сколько ни долби, судья не
	// сможет вынести вердикт. Это к человеку, а не к следующей итерации.
	if code == 2 {
		line("СТОП: судья не может вынести вердикт. Чинить условия проверки, а не код.")
		return 2
	}
	if *planOnly {
		line("PlanOnly: дальше не иду.")
		return 0
	}

	lastCode := code
	lastSig := verdictSignature(code, text)

	for _, step := range steps {
		if step.Done || step.Gate {
			continue
		}
		m := resolveLadderTier(c.Ladder, step.Tier)
		if !m.Found {
			closeWoody("ничего: ступень шага не найдена в лестнице продукта",
				fmt.Sprintf("поправь либо PLAN.md — шаг %d «%s» называет ступень '%s', которой нет в лестнице "+
					"(ни точно, ни по имени модели), либо ladder в run-config.json. Доступные ступени: %s",
					step.Index, step.Title, step.Tier, strings.Join(c.Ladder, ", ")), 1)
		}
		tierIndex := m.Index

		for {
			if c.iter >= c.MaxIter {
				closeWoody("ничего: потолок итераций исчерпан",
					fmt.Sprintf("решить, поднимать ли потолок (%d) или переписать план", c.MaxIter), 1)
			}
			if c.spent >= c.MaxUSD {
				closeWoody("ничего: бюджет исчерпан",
					fmt.Sprintf("решить, поднимать ли бюджет — потрачено $%.2f из $%.0f", c.spent, c.MaxUSD), 1)
			}
			c.iter++
			tier := c.Ladder[tierIndex]
			runner := resolveRunner(cfg, tier)
			who := runner.Kind
			if runner.Kind == "script" {
				who = "скрипт"
			} else if c.Orchestrate {
				who = fmt.Sprintf("ведущая · субагентов %d", c.Subagents)
			}
			status(step.Index, step.Title, tier, who, "прогон идёт")

			var r stepResult
			if runner.Kind == "script" {
				r = c.runScriptStep(step)
			} else {
				r = c.runModelStep(step, tier, text, runner)
			}
			if r.Cost != nil {
				c.spent += *r.Cost
			}
			code, text = c.judge()

			c.addStep(map[string]any{
				"step": step.Index, "title": step.Title, "tier": tier,
				"iteration": c.iter, "code": code,
				"total_cost_usd": r.Cost, "num_turns": r.Turns,
				"duration_api_ms": r.ApiMs, "session_id": nullIfEmpty(r.Session),
				"subtype": nullIfEmpty(r.Subtype), "spent_usd": round4(c.spent),
				// ОТВЕТ ИСПОЛНИТЕЛЯ ЛОЖИТСЯ В ЖУРНАЛ. Без него прогон, потративший
				// $7.33 и не изменивший ни одного файла, не оставлял следа о причине:
				// цена и ходы были, а что сказал исполнитель — нигде.
				"runner_said": nullIfEmpty(r.Detail),
				"orchestration": c.Orchestrate, "subagents": c.subagentsInLog(),
			})

			state := "судья не пропустил"
			switch code {
			case 0:
				state = "цель закрыта"
			case 2:
				state = "не проверено"
			}
			status(step.Index, step.Title, tier, who, state)
			measure(c.iter, r.Cost, r.Turns, c.spent, c.MaxUSD)

			// ДВИГАТЕЛЬ ЦЕЛИ. Замер идёт ПОСЛЕ итерации: пара «до/после» и отвечает на
			// вопрос, двинул ли ход расстояние. Зовётся ВНУТРИ процесса — отдельный
			// подпроцесс здесь был бы платой за то, что уже есть под рукой.
			switch c.recordDrift(fmt.Sprintf("итерация %d · шаг %d", c.iter, step.Index)) {
			case 3:
				closeWoody("ничего: работой закрывать нечего, расстояние до цели ноль",
					"закрыть остаток — он держится гейтами ЛПР либо реестр не покрывает того, что требует судья", 0)
			case 2:
				// Эскалация ОСТАНАВЛИВАЕТ цикл. У ВЕРЫ вердикт сперва влиял только на
				// отдельных исполнителей, цикл продолжал брать постороннюю работу и
				// кончался PASS. Гейт без действия — не гейт.
				closeWoody("ничего: двигатель цели объявил эскалацию",
					"решить, что делать с работой, которая не уменьшает расстояние до цели. "+
						"Подъём ступени этого не лечит: дорогая модель так же точно будет двигаться мимо", 1)
			}

			if code == 0 {
				line(text)
				closeWoody("закрыть работу вместе с замером: сколько стоило, сколько ходов, какими моделями",
					"не жду ничего", 0)
			}
			if code == 2 {
				line(text)
				closeWoody("ничего: работой это не лечится",
					"починить условия проверки — судья не может вынести вердикт", 2)
			}
			// Упор в потолок ходов подъёмом ступени не лечится: это признак слишком
			// широкого шага. Поднимать модель здесь значит платить дороже за ту же ошибку.
			if r.Subtype == "error_max_turns" {
				closeWoody("ничего: подъём ступени эту ошибку не лечит, он оплачивает её дороже",
					fmt.Sprintf("разбить шаг %d в плане на более узкие — он упёрся в потолок ходов", step.Index), 1)
			}
			if r.Subtype == "error_max_budget_usd" {
				closeWoody("ничего: прогон остановлен потолком бюджета внутри CLI",
					fmt.Sprintf("решить, поднимать ли бюджет — потрачено $%.4f из $%.0f", c.spent, c.MaxUSD), 1)
			}
			// ОТКАЗ ИСПОЛНИТЕЛЯ — НЕ РАБОТА. Прежде петля проверяла только известные
			// подтипы ошибок и пропускала общий случай: вызов не состоялся, судья
			// естественно не сдвинулся, и петля лезла вверх по лестнице, оплачивая
			// дороже то, что не исполнилось ни разу.
			if r.Subtype == "needs_permission" {
				closeWoody("ничего: исполнителю не дали прав, работы не было",
					"РЕШЕНИЕ ЛПР: отцеплённый исполнитель не может писать файлы без полного отключения "+
						"проверок прав. Ни permission-mode acceptEdits, ни перечень allowedTools этого не "+
						"снимают — проверено прогоном. Пока права не даны, петля тратит деньги и не может "+
						"сделать НИЧЕГО. Ответ исполнителя: "+r.Detail, 2)
			}
			if r.Subtype == "runner_error" {
				closeWoody("ничего: исполнитель отказал, работы не было",
					"разобрать отказ исполнителя — это «нечем исполнить», а не «модель не справилась»: "+
						r.Detail, 2)
			}
			if r.Subtype == "no_runner" {
				closeWoody("ничего: исполнителя нет на машине",
					"поставить или авторизовать исполнителя — это «нечем исполнить», а не отказ модели", 2)
			}

			// ДВИЖЕНИЕ СУДЬИ ЛОВИТСЯ ПОДПИСЬЮ, А НЕ КОДОМ. Кодов три, а состояний работы
			// сколько угодно: «закрыто 13 из 16» и «14 из 16» оба дают код 1, значит
			// прогресс был НЕВИДИМ, и петля лезла вверх по лестнице независимо от него —
			// прямо обратно замыслу. Наблюдалось вживую: шаг прошёл всю лестницу за шесть
			// подъёмов, ни один из которых не был вызван отсутствием прогресса.
			sig := verdictSignature(code, text)
			if sig != lastSig {
				lastSig = sig
				lastCode = code
				c.stalledRuns = 0
				line("  судья сдвинулся — остаюсь на той же ступени. Вердикт: " + text)
				continue
			}

			// ЗАСТОЙ ПО ЦЕЛИ, А НЕ ПО ШАГУ. Счётчик общий и сбрасывается любым сдвигом
			// вердикта, в том числе на другом шаге: цель одна, и двигают её сообща.
			c.stalledRuns++
			if c.stalledRuns >= c.StallLimit {
				closeWoody(fmt.Sprintf("ничего: цель не сдвинулась %d прогонов подряд", c.stalledRuns),
					fmt.Sprintf("снять цель с вращения либо изменить постановку — вердикт судьи не меняется "+
						"с %d прогонов: «%s». Подъём ступени это не лечит, он оплачивает ту же неподвижность дороже",
						c.stalledRuns, text), 1)
			}
			if tierIndex >= len(c.Ladder)-1 {
				closeWoody("ничего: лестница пройдена до верха",
					fmt.Sprintf("решение по шагу %d «%s» — судья не сдвинулся и на верхней ступени",
						step.Index, step.Title), 1)
			}
			tierIndex++
			line("  судья не сдвинулся — поднимаю ступень до " + c.Ladder[tierIndex] + ".")
		}
	}

	// Нулевая итерация тоже итерация: она должна быть в журнале, а не молчать. Иначе
	// «петля не заводилась» (нет steps.jsonl) неотличимо от «завелась и честно нашла, что
	// делать нечего» — тот же класс путаницы, ради которого журнал и заведён.
	c.addStep(map[string]any{
		"step": nil, "title": "план пройден до конца", "tier": nil,
		"iteration": 0, "code": lastCode,
		"total_cost_usd": 0, "num_turns": 0, "duration_api_ms": 0,
		"session_id": nil, "subtype": nil, "spent_usd": round4(c.spent),
		"orchestration": c.Orchestrate, "subagents": c.subagentsInLog(),
		"reason": "все исполняемые шаги плана уже закрыты; открытых non-gate шагов нет",
	})
	closeWoody("ничего: план пройден до конца",
		"дополнить план — все шаги закрыты, а судья цель не подтвердил", 1)
	return 1
}

var reSpaces = regexp.MustCompile(`\s+`)

// verdictSignature — код ПЛЮС текст вердикта. Судья печатает «фактов закрыто N из M»;
// смена N меняет подпись, и петля остаётся на дешёвой ступени, пока работа сдвигается
// хоть на один факт.
func verdictSignature(code int, text string) string {
	return fmt.Sprintf("%d|%s", code, strings.TrimSpace(reSpaces.ReplaceAllString(text, " ")))
}

func (c *loopCtx) subagentsInLog() int {
	if c.Orchestrate {
		return c.Subagents
	}
	return 0
}

// judge — свой судья продукта, если назван; иначе встроенный, БЕЗ подпроцесса.
func (c *loopCtx) judge() (int, string) {
	if c.JudgePath == "" {
		res := runJudge(c.Root, c.Cfg, -1)
		code, text := verdict(res)
		publishVerdict(c.Root, code, text, res)
		return code, text
	}
	args := append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", c.JudgePath}, c.JudgeArgs...)
	cmd := exec.Command("powershell.exe", args...)
	cmd.Dir = c.Root
	out, err := cmd.CombinedOutput()
	return exitCode(cmd, err), strings.TrimSpace(decodeOutput(out))
}

// recordDrift — двигатель цели внутри процесса. Возвращает его код: 0 ALLOW, 1 THROTTLE,
// 2 ESCALATE, 3 ЖДЁТ ЛПР.
func (c *loopCtx) recordDrift(note string) int {
	return driftOnce(c.Root, true, note)
}

func (c *loopCtx) addStep(row map[string]any) {
	row["at"] = time.Now().Format("2006-01-02T15:04:05")
	// Сухой прогон помечается В КАЖДОЙ строке журнала: без пометки его итерации
	// неотличимы от настоящих — расход ноль, ходов ноль, — и читатель решит, что работа
	// шла и не стоила ничего.
	row["whatif"] = c.WhatIf
	b, err := json.Marshal(row)
	if err != nil {
		return
	}
	f, err := os.OpenFile(c.StepsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\r', '\n'))
}

// runScriptStep — ступень script модель не зовёт вовсе. Это не оптимизация, а правило:
// шаг с проверяемым точным выходом, отданный модели, — оплаченное бесплатное.
func (c *loopCtx) runScriptStep(step workStep) stepResult {
	if step.Cmd == "" {
		// Работы не было — значит и замера нет. nil, а не ноль.
		line(fmt.Sprintf("  шаг %d: ступень script, но команда не задана (нет '::'). Пропуск.", step.Index))
		return stepResult{}
	}
	line("  выполняю скриптом: " + step.Cmd)
	if c.WhatIf {
		return stepResult{Ok: true, Session: "whatif"}
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", step.Cmd)
	cmd.Dir = c.Root
	out, err := cmd.CombinedOutput()
	code := exitCode(cmd, err)
	if s := strings.TrimSpace(decodeOutput(out)); s != "" {
		for _, l := range strings.Split(s, "\n") {
			line("    " + strings.TrimRight(l, "\r"))
		}
	}
	// Здесь ноль ЗАКОННЫЙ и отличается от nil по смыслу: скрипт отработал, и расхода у
	// него нет по определению. Пустое значение означало бы «не мерили», а мы мерили.
	zeroCost, zeroTurns := 0.0, 0
	return stepResult{Ok: code == 0, Cost: &zeroCost, Turns: &zeroTurns}
}

func orInt(v, def int) int {
	if v > 0 {
		return v
	}
	return def
}

func orFloat(v, def float64) float64 {
	if v > 0 {
		return v
	}
	return def
}

func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
