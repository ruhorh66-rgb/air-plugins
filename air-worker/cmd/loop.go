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
	Tools       string

	treeBefore  string
	spent       float64
	iter        int
	stalledRuns int
	// regressStreak — регрессов подряд. Первый чинится на той же ступени, второй — нет.
	regressStreak int
	// verdictFresh — последний прогон судьи оставил машинный вердикт, по которому
	// двигатель меряет расстояние. Встроенный судья пишет его всегда. Свой судья продукта —
	// не обязательно, и файл от прошлого прогона дал бы расстояние, которое не двигается
	// ни от какой работы.
	verdictFresh bool
}

// treeChanged — изменилось ли рабочее дерево продукта с начала шага.
//
// Замер, а не догадка: подпись состояния дерева снимается перед вызовом исполнителя и
// сравнивается после. Читается git status --porcelain; если git недоступен, функция
// отвечает true — «считать, что работа была». Осторожность здесь несимметрична: принять
// сделанную работу за несделанную дороже, чем наоборот, потому что первое ОСТАНАВЛИВАЕТ
// петлю ложным вердиктом.
// Подпись дерева берётся ТОЛЬКО по каталогу продукта — см. measureTree в report.go:
// без ограничения `-- .` в моно-репозитории считаются чужие изменения, и петля примет
// работу соседнего продукта за работу своего исполнителя.
func (c *loopCtx) treeSignature() string {
	cmd := exec.Command("git", "status", "--porcelain", "--", ".")
	cmd.Dir = c.Root
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func (c *loopCtx) treeChanged() bool {
	if c.treeBefore == "" {
		return true
	}
	return c.treeSignature() != c.treeBefore
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

	// ОДНА ПЕТЛЯ НА ПРОДУКТ, И ЭТО САМЫЙ ВАЖНЫЙ ИЗ ЗАМКОВ.
	//
	// Вводная ЛПР 14.09.2026: на машине работают несколько сессий одновременно. Две
	// петли на ОДНОМ продукте — худшее, что может случиться с механизмом: каждый
	// исполнитель видит чужие изменения дерева как свои, судья меряет смесь, застой и
	// расстояние перестают что-либо значить. Механизм, который врёт про расстояние,
	// хуже отсутствующего — на него ещё и полагаются.
	//
	// Замок именной по ПУТИ ПРОДУКТА, а не общий: две петли по РАЗНЫМ продуктам
	// мешать друг другу не должны, это обычная параллельная работа.
	//
	// БЕЗ ОЖИДАНИЯ. Простоять час в очереди и начать, когда обстановка уже другая,
	// хуже честного отказа: человек узнает сразу и решит сам.
	loopLock, free := acquireLock(lockName("loop", root))
	if !free {
		line("ОТКАЗ: петля по этому продукту УЖЕ ИДЁТ в другом процессе.")
		line("  Две петли на одном дереве меряют смесь чужих изменений, и расстояние до цели")
		line("  перестаёт что-либо значить. Дождись окончания либо останови ту петлю.")
		line("  Состояние продукта: air-worker report -product \"" + root + "\"")
		return 2
	}
	defer loopLock.release()

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
		Tools:       cfg.Runner.AllowedTools,
	}
	if c.Permission == "" {
		c.Permission = "acceptEdits"
	}
	if c.Tools == "" {
		// Список проверенной ветви 13.09.2026 — той, что реально писала файлы.
		c.Tools = "Read,Write,Edit,Glob,Grep,Bash"
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
	line("права исполн: " + c.Permission + " · инструменты: " + c.Tools)
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

	// ТОЧКА ПОСЛЕДНЕГО ДВИЖЕНИЯ — с ней сравнивается каждая итерация. До первой итерации
	// это состояние до работы. Замер без записи в историю: хода ещё не было.
	m0, _, _ := measureDrift(root, "")
	ref := pointOf(m0)
	if !c.verdictFresh {
		ref.Judge = nil
	}
	refinedInCycle := false

	// ШАГ БЕРЁТСЯ ИЗ ПЛАНА НА ДИСКЕ, А НЕ ИЗ СПИСКА, РАЗОБРАННОГО ПРИ СТАРТЕ.
	//
	// До 0.9.5 петля разбирала план один раз и шла по этому списку, а внутри шага крутилась
	// до вердикта или потолка. Шаг, закрытый исполнителем, петля не замечала: следующая
	// итерация получала задание уже закрытого шага. Живой случай 14.09.2026, ASW: шаг 8а
	// закрыт на седьмой итерации ступенью opus:medium, и восьмая пошла на opus:medium по
	// тому же заданию. Исполнителю делать нечего — «не сдвинулась» — подъём до opus:max:
	// самые дорогие ступени лестницы оплачивали бы работу, которой нет.
	//
	// Теперь перед каждой итерацией план перечитывается, и работа идёт над ПЕРВЫМ ОТКРЫТЫМ
	// исполняемым шагом. Сменился он — новый шаг начинает со своей ступени из плана.
	for {
		step, found := firstOpenWorkStep(steps)
		if !found {
			break
		}
		m := resolveLadderTier(c.Ladder, step.Tier)
		if !m.Found {
			closeWoody("ничего: ступень шага не найдена в лестнице продукта",
				fmt.Sprintf("поправь либо PLAN.md — шаг %d «%s» называет ступень '%s', которой нет в лестнице "+
					"(ни точно, ни по имени модели), либо ladder в run-config.json. Доступные ступени: %s",
					step.Index, step.Title, step.Tier, strings.Join(c.Ladder, ", ")), 1)
		}
		tierIndex := m.Index
		c.regressStreak = 0

		for pass := 0; ; pass++ {
			if pass > 0 {
				steps = readPlanSteps(planPath)
				if len(steps) == 0 {
					closeWoody("ничего: план перестал разбираться",
						fmt.Sprintf("посмотреть %s — после итерации %d в нём не разбирается ни одного шага; "+
							"скорее всего исполнитель повредил таблицу", planPath, c.iter), 2)
				}
				if next, ok := firstOpenWorkStep(steps); !ok || !sameStep(next, step) {
					if ok {
						line("  шаг «" + step.Title + "» закрыт в плане — дальше «" + next.Title +
							"» со своей ступени " + next.Tier + ".")
					}
					break
				}
			}
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

			// Подпись дерева снимается ДО работы: по ней потом видно, была работа или нет,
			// и это факт, а не пересказ исполнителя о себе.
			c.treeBefore = c.treeSignature()
			var r stepResult
			if runner.Kind == "script" {
				r = c.runScriptStep(step)
			} else {
				r = c.runModelStep(step, tier, text, runner)
			}
			if r.Cost != nil {
				c.spent += *r.Cost
			}
			// СУХОЙ ПРОГОН СУДЬЮ НЕ ПЕРЕСПРАШИВАЕТ: работы не было, вердикт тот же, что до
			// неё. У ASW один прогон судьи — полминуты с лишним.
			if !c.WhatIf {
				code, text = c.judge()
			}

			c.addStep(map[string]any{
				"step": step.Index, "title": step.Title, "tier": tier,
				"iteration": c.iter, "code": code,
				"total_cost_usd": r.Cost, "num_turns": r.Turns,
				"duration_api_ms": r.ApiMs, "session_id": nullIfEmpty(r.Session),
				"subtype": nullIfEmpty(r.Subtype), "spent_usd": round4(c.spent),
				// ОТВЕТ ИСПОЛНИТЕЛЯ ЛОЖИТСЯ В ЖУРНАЛ. Без него прогон, потративший
				// $7.33 и не изменивший ни одного файла, не оставлял следа о причине:
				// цена и ходы были, а что сказал исполнитель — нигде.
				"runner_said":   nullIfEmpty(r.Detail),
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
			dm, driftCode := c.recordDrift(fmt.Sprintf("итерация %d · шаг %d", c.iter, step.Index))
			switch driftCode {
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
				// Текст правится 13.09.2026: прежний утверждал, что исполнитель не может ПИСАТЬ
				// файлы — это оказалось неверно и было снято замерами. Верное утверждение уже:
				// он не может ЗАПУСКАТЬ КОМАНДЫ, а файлы пишет.
				closeWoody("ничего: исполнитель попросил прав и ничего не сделал",
					"разобрать, чего он просил. Отцеплённый исполнитель НЕ ЗАПУСКАЕТ команды (go, git, "+
						"судью) ни при каких правах — это проверено, и задание ему об этом прямо говорит. "+
						"Файлы он писать МОЖЕТ. Если он просит прав вместо работы, значит задание шага "+
						"требует запуска, а не кода: перепиши шаг так, чтобы результатом был код, а "+
						"проверку оставь судье петли. Ответ исполнителя: "+r.Detail, 2)
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
			if r.Subtype == "no_command" {
				closeWoody(fmt.Sprintf("ничего: у шага %d ступень script, но команды нет", step.Index),
					fmt.Sprintf("дописать команду шагу %d «%s». В табличной форме плана она пишется в "+
						"колонку «Шаг» после «::», и для ступени script она ОБЯЗАТЕЛЬНА. Это «нечем "+
						"исполнить», а не «модель не справилась»: подъём ступени здесь означал бы плату "+
						"моделью за детерминированный шаг — ровно то, что скил объявляет дефектом",
						step.Index, step.Title), 2)
			}

			// СУХОЙ ПРОГОН ЛЕСТНИЦУ НЕ ИМИТИРУЕТ. Модель не звалась, значит сдвинуть цель было
			// нечем, и любое суждение о движении здесь заранее ложно. Прежде петля шла дальше:
			// «не сдвинулся» — подъём ступени — ещё подъём — «снять цель с вращения». Человек,
			// посмотревший план, получал совет бросить работающую цель. Найдено 14.09.2026
			// сухим прогоном перед петлёй на ASW.
			if c.WhatIf {
				line("  сухой прогон: модель не звалась, судить движение нечем — лестницу дальше не имитирую.")
				closeWoody(fmt.Sprintf("боевой прогон начнёт с шага «%s» на ступени %s; следующую ступень "+
					"выберет замер после итерации, а не сухой прогон", step.Title, tier), "не жду ничего", 0)
			}

			// ДВИЖЕНИЕ СУДИТСЯ РАССТОЯНИЕМ ДВИГАТЕЛЯ, А НЕ ТЕКСТОМ ВЕРДИКТА.
			//
			// Прежде здесь сравнивалась подпись: код плюс текст вердикта. Подпись заводилась
			// против невидимого прогресса — «закрыто 13 из 16» и «14 из 16» дают один код, и
			// петля лезла вверх по лестнице, не видя сдвига. Но подпись меняется от ЛЮБОГО
			// нового текста, в том числе от поломки. Живой случай 14.09.2026, ASW, шаг 8а:
			// исполнитель haiku сломал сборку internal/mcpupdate, в вердикте появилась строка
			// компилятора — и петля напечатала «судья сдвинулся», сбросила застой и пошла на
			// ту же ступень как за успехом. Это второй источник правды о движении рядом с
			// двигателем цели — ровно то, что выпуск 0.9.4 убирал из drift.
			//
			// Теперь итерация сравнивается с ТОЧКОЙ ПОСЛЕДНЕГО ДВИЖЕНИЯ теми же определениями,
			// что у двигателя (moved, regression, refinedPlan). Не с предыдущим замером: иначе
			// «сломал — починил — сломал» засчитывался бы движением на каждой починке, и
			// колебание пряталось бы от застоя навсегда. Двигатель по истории сравнивает с
			// предыдущим замером и прав по-своему: между сессиями судью расширяют, и остаток
			// законно растёт. Внутри одного прогона мерка неизменна.
			cur := pointOf(dm)
			if !c.verdictFresh {
				cur.Judge = nil
			}
			move, why := judgeIteration(ref, cur, refinedInCycle)
			sig := verdictSignature(code, text)
			lastCode = code
			if move == moveUnmeasured {
				// Свой судья продукта не оставил машинного вердикта — расстояние мерить нечем.
				// Остаётся прежнее средство, и оно называется вслух: текст вердикта.
				if sig != lastSig {
					move, why = moveForward, why+": сужу по тексту вердикта, а он изменился"
				} else {
					move, why = moveNone, why+", и текст вердикта не изменился"
				}
			}
			lastSig = sig
			if ref.Judge == nil && cur.Judge != nil {
				ref = cur
			}

			switch move {
			case moveForward:
				if cur.Judge != nil {
					ref = cur
				}
				refinedInCycle = false
				c.stalledRuns, c.regressStreak = 0, 0
				line("  цель сдвинулась (" + why + ") — остаюсь на той же ступени.")
				continue
			case moveRefined:
				refinedInCycle = true
				c.regressStreak = 0
				line("  план уточнён (" + why + ") — один раз за цикл застоем не считается; ступень та же.")
				continue
			case moveRegress:
				// РЕГРЕСС — НЕ ДВИЖЕНИЕ И НЕ ЗАСТОЙ. Застой — это итерация без действия,
				// регресс — действие не туда. Считать его застоем значило бы останавливать
				// петлю на поломке: на ASW 14.09.2026 при пороге 3 петля встала бы на третьей
				// итерации со сломанной сборкой, а починила её ступень sonnet на четвёртой.
				//
				// ПЕРВЫЙ РЕГРЕСС ЧИНИТСЯ НА ТОЙ ЖЕ СТУПЕНИ. Исполнитель не запускает сборку и
				// свою поломку видит только в вердикте следующей итерации; поднимать модель за
				// опечатку, которую дешёвая ступень починит сама, — платить дороже за то же.
				// Второй регресс подряд уже не опечатка: ступень поднимается. Бесконечно это не
				// длится — лестница конечна, и верхняя ступень закрывает петлю.
				c.regressStreak++
				if c.regressStreak == 1 {
					line("  РЕГРЕСС (" + why + ") — движением не считается. Ступень не поднимаю: " +
						"следующая итерация получит вердикт и чинит сломанное.")
					continue
				}
				why = fmt.Sprintf("регресс %d раза подряд: %s", c.regressStreak, why)
			default:
				c.regressStreak = 0
				// ЗАСТОЙ ПО ЦЕЛИ, А НЕ ПО ШАГУ. Счётчик общий и сбрасывается только движением,
				// в том числе на другом шаге: цель одна, и двигают её сообща.
				c.stalledRuns++
				if c.stalledRuns >= c.StallLimit {
					closeWoody(fmt.Sprintf("ничего: цель не сдвинулась %d прогонов подряд", c.stalledRuns),
						fmt.Sprintf("снять цель с вращения либо изменить постановку — с точки последнего движения "+
							"цель не приблизилась за %d прогонов (последний: %s). Подъём ступени это не лечит, "+
							"он оплачивает ту же неподвижность дороже", c.stalledRuns, why), 1)
				}
			}
			if tierIndex >= len(c.Ladder)-1 {
				closeWoody("ничего: лестница пройдена до верха",
					fmt.Sprintf("решение по шагу «%s» — цель не сдвинулась и на верхней ступени (%s)",
						step.Title, why), 1)
			}
			tierIndex++
			line("  цель не сдвинулась (" + why + ") — поднимаю ступень до " + c.Ladder[tierIndex] + ".")
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

// verdictSignature — код ПЛЮС текст вердикта. С 0.9.5 это ЗАПАСНОЕ средство: движение
// судится расстоянием двигателя, а подпись берётся, только когда свой судья продукта не
// оставил машинного вердикта. Главным средством подпись быть не может: она меняется от
// любого нового текста, в том числе от строки компилятора о поломке.
func verdictSignature(code int, text string) string {
	return fmt.Sprintf("%d|%s", code, strings.TrimSpace(reSpaces.ReplaceAllString(text, " ")))
}

// iterationMove — что итерация сделала с целью по замеру двигателя.
type iterationMove int

const (
	moveNone       iterationMove = iota // с точки последнего движения ничего не улучшилось
	moveForward                         // остаток по судье меньше либо закрыт шаг плана, и ничего не хуже
	moveRegress                         // остаток вырос либо закрытый шаг снова открыт
	moveRefined                         // план уточнён: открытых шагов больше, остальное не хуже
	moveUnmeasured                      // расстояние по судье не измерилось
)

// judgeIteration — суждение об итерации: замер после неё против точки последнего
// движения. Чистая функция: правило проверяется тестом, а не прогоном на живом продукте.
// Определения движения, регресса и уточнения — двигателя цели; своих здесь не заводится.
func judgeIteration(ref, cur driftPoint, refinedInCycle bool) (iterationMove, string) {
	if ref.Judge == nil || cur.Judge == nil {
		return moveUnmeasured, "расстояние по судье не измерилось"
	}
	if why := regression(ref, cur); why != "" {
		return moveRegress, why
	}
	if moved(ref, cur) {
		if *cur.Judge < *ref.Judge {
			return moveForward, fmt.Sprintf("остаток по судье %d → %d", *ref.Judge, *cur.Judge)
		}
		return moveForward, fmt.Sprintf("закрытых шагов плана %d → %d", *ref.Closed, *cur.Closed)
	}
	if refinedPlan(ref, cur) && !refinedInCycle {
		return moveRefined, fmt.Sprintf("открытых шагов плана %d → %d, остаток и закрытые не хуже",
			*ref.Open, *cur.Open)
	}
	closed := ""
	if cur.Closed != nil {
		closed = fmt.Sprintf(", закрытых шагов %d", *cur.Closed)
	}
	return moveNone, fmt.Sprintf("остаток по судье %d%s — как в точке последнего движения", *cur.Judge, closed)
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
		c.verdictFresh = true
		return code, text
	}
	started := time.Now()
	args := append([]string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", c.JudgePath}, c.JudgeArgs...)
	cmd := exec.Command("powershell.exe", args...)
	cmd.Dir = c.Root
	out, err := cmd.CombinedOutput()
	c.verdictFresh = verdictWrittenSince(c.Root, started)
	return exitCode(cmd, err), strings.TrimSpace(decodeOutput(out))
}

// firstOpenWorkStep — первый незакрытый шаг, исполняемый работой. Гейт шагом работы не
// считается: его закрывает решение ЛПР.
func firstOpenWorkStep(steps []workStep) (workStep, bool) {
	for _, s := range steps {
		if !s.Done && !s.Gate {
			return s, true
		}
	}
	return workStep{}, false
}

// sameStep — тот же ли это шаг плана. Заголовок переживает вставку строк выше; номер
// вместе с позицией переживают правку текста шага. Иначе исполнитель, уточнивший
// формулировку открытого шага, отправил бы петлю на его первую ступень заново.
func sameStep(a, b workStep) bool {
	return a.Title == b.Title || (a.Num == b.Num && a.Index == b.Index)
}

// verdictWrittenSince — машинный вердикт записан не раньше t. Допуск в две секунды — на
// файловые системы, где время изменения хранится с таким шагом.
func verdictWrittenSince(root string, t time.Time) bool {
	fi, err := os.Stat(filepath.Join(root, ".goal-verdict.json"))
	return err == nil && !fi.ModTime().Before(t.Add(-2*time.Second))
}

// recordDrift — двигатель цели внутри процесса. Возвращает замер и код: 0 ALLOW,
// 1 THROTTLE, 2 ESCALATE, 3 ЖДЁТ ЛПР.
//
// СУХОЙ ПРОГОН В ИСТОРИЮ НЕ ПИШЕТ, и это не осторожность, а починка дефекта.
//
// Нашла AIR-ENV-002 14.09.2026 на живом продукте: `loop -whatif` дописал в
// .woody/goal-drift.jsonl три НАСТОЯЩИЕ записи, и застой ушёл с 3 на 5. Ещё два сухих
// прогона — шесть, то есть ESCALATE: петля встанет, ход не закроется.
//
// Дефект злой по устройству. Сухой прогон НЕ ЗОВЁТ МОДЕЛЬ — значит он по определению
// не может сдвинуть судью, значит гарантированно производит «неподвижность» и
// приписывает её продукту. Механизм наказывал за то, что человек посмотрел, не трогая.
//
// В steps.jsonl такие строки помечались честно («whatif»: true), а в истории дрейфа
// пометки не было вовсе — там они неотличимы от боевых. Пометка и не помогла бы:
// застой считается по ИСТОРИИ, и запись, которую нельзя было сдвинуть, не должна в
// неё попадать вовсе.
func (c *loopCtx) recordDrift(note string) (driftMeasure, int) {
	if c.WhatIf {
		// Замер делается, чтобы показать человеку текущее состояние, но НЕ ЗАПИСЫВАЕТСЯ:
		// запись — это утверждение «был ход», а хода не было.
		return driftOnce(c.Root, false, note)
	}
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
		// «НЕЧЕМ ИСПОЛНИТЬ» — НЕ «МОДЕЛЬ НЕ СПРАВИЛАСЬ». Это правило у петли уже есть
		// для неавторизованного исполнителя (no_runner) и для его отказа (runner_error);
		// здесь оно не действовало, и цена оказалась прямой.
		//
		// Нашла AIR-ENV-002 14.09.2026 прогоном: шаг ступени script без команды молча
		// пропускался, пропуск давал «судья не сдвинулся», и петля ПОДНИМАЛА СТУПЕНЬ —
		// уходила на платную модель. То есть механизм сам делал ровно то, что скил
		// объявляет дефектом: платил моделью за детерминированный шаг.
		//
		// Пропуск сам по себе был верен. Неверен был ВЫВОД ИЗ НЕГО: неподвижность,
		// вызванная отсутствием команды, приписывалась продукту.
		//
		// Причина, по которой шаг остался без команды, — не небрежность человека, а
		// расхождение документации с кодом: `::` был описан только для чекбоксовой формы
		// плана, а SKILL.md предписывает табличную и про команду молчал. Человек,
		// написавший план ровно по скилу, гарантированно получал этот дефект. Исправлено
		// и в SKILL.md тем же выпуском.
		return stepResult{Subtype: "no_command"}
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
