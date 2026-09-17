package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Планировщик: разбивка цели на шаги с назначением ступеней ОДНИМ дорогим вызовом.
//
// Пятая роль, названная ЛПР сверх четырёх ролей AUTO-080 и прямо отделённая им от
// планировщика ОС: «не нужен тебе планировщик вообще от слова совсем» — это про задачу
// Windows, а не про разбивку цели.
//
// ЗАЧЕМ ОН ЭКОНОМИЧЕСКИ. Разбивка — единственное место, где суждение окупается. Ошибка
// здесь тиражируется на все последующие прогоны: шаг, назначенный дорогой модели по
// недосмотру, платится столько раз, сколько раз петля его возьмёт. Один дорогой вызов на
// разбивку — и дальше механическое исполняется скриптами. Обратный порядок, разведка боем
// на каждом шаге, выглядит работой и стоит вдесятеро.
//
// ПОЧЕМУ ОН ПРЕДЛАГАЕТ, А НЕ ПИШЕТ. План правится руками: машина исполняет, человек
// владеет разбивкой. Планировщик, молча переписывающий PLAN.md, отнимает единственное
// место, где решает человек, — и отнимает тихо, потому что новый план выглядит как старый.

type goalFile struct {
	Goal      string `json:"goal"`
	Objective string `json:"objective"`
}

func cmdPlan(argv []string) int {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	apply := fs.Bool("apply", false, "записать PLAN.md, если его НЕТ; существующий не трогается никогда")
	model := fs.String("model", "", "модель разбивки; по умолчанию верх лестницы продукта")
	dryRun := fs.Bool("dry-run", false, "собрать задание и показать, модель не звать")
	useAnswer := fs.String("use-answer", "", "разобрать уже полученный ответ, не платя снова")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		line("ОТКАЗ: не разобран путь продукта: " + err.Error())
		return 2
	}

	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		line("ОТКАЗ: нет run-config.json. Разбивка без лестницы назначала бы ступени наугад.")
		return 2
	}
	if len(cfg.Ladder) == 0 {
		line("ОТКАЗ: лестница не объявлена в run-config.json")
		return 2
	}

	// Цель. Без неё разбивать нечего, и выдумывать её планировщик не станет.
	goalPath := filepath.Join(root, "goal", "goal.json")
	var goal goalFile
	if err := readJSON(goalPath, &goal); err != nil {
		line("ОТКАЗ: нет " + goalPath + ". Цель как файл — признак 1 постановки; разбивать пересказ из промпта запрещено.")
		return 2
	}
	var rawGoal map[string]any
	_ = readJSON(goalPath, &rawGoal)

	// ПОСТАНОВКА ОБЯЗАТЕЛЬНА (этап 0.10, К4). До 0.10 её отсутствие было не отказом, а
	// примечанием: «разбивка идёт по одной цели-файлу» либо «без признаков достижения».
	// ЛПР назвал план без целей бумажкой (14.09.2026): разбить цель без признаков
	// достижения — значит получить шаги, про которые не видно, пришли к цели или нет.
	if goal.Objective == "" {
		line("ОТКАЗ: в " + goalPath + " не объявлена постановка (поле objective).")
		line("Разбивка без признаков достижения запрещена решением ЛПР 14.09.2026: «без целей план — бумажка».")
		return 2
	}
	objPath := goal.Objective
	if !filepath.IsAbs(objPath) {
		objPath = filepath.Join(root, objPath)
	}
	objBytes, objErr := os.ReadFile(objPath)
	if objErr != nil {
		line("ОТКАЗ: постановка объявлена (" + goal.Objective + "), но файла нет — " + objPath)
		line("Разбивка без признаков достижения запрещена решением ЛПР 14.09.2026: «без целей план — бумажка».")
		return 2
	}
	objText, objNote := string(objBytes), "постановка прочитана: "+objPath

	tier := *model
	if tier == "" {
		tier = cfg.Ladder[len(cfg.Ladder)-1]
	}

	goalJSON, _ := json.MarshalIndent(rawGoal, "", "  ")
	var checkNames []string
	for _, chk := range cfg.Judge.Checks {
		if strings.TrimSpace(chk.Name) != "" {
			checkNames = append(checkNames, chk.Name)
		}
	}
	prompt := buildPlannerPrompt(goal.Goal, string(goalJSON), objNote, objText, cfg.Ladder, checkNames)

	outDir := filepath.Join(root, ".woody")
	_ = os.MkdirAll(outDir, 0o755)
	_ = os.WriteFile(filepath.Join(outDir, "planner-prompt.txt"), []byte(prompt), 0o644)

	line("продукт   : " + root)
	line("цель      : " + goal.Goal)
	line("разбивка  : модель " + tier + " (верх лестницы), ОДИН вызов")
	line(objNote)

	if *dryRun {
		line("сухой прогон: задание собрано, модель не зовётся")
		fmt.Print(lineEnding + prompt + lineEnding)
		return 0
	}

	answerPath := filepath.Join(outDir, "planner-answer.json")
	if *useAnswer != "" {
		// Разбор уже оплаченного ответа. Заведено после того, как ошибка в проверке
		// уронила разбор ПОСЛЕ состоявшегося вызова ценой почти доллара: дефект проверки
		// не должен стоить второго дорогого вызова — это ровно та расточительность, от
		// которой шаг и защищает.
		b, err := os.ReadFile(*useAnswer)
		if err != nil {
			line("ОТКАЗ: не прочитан сохранённый ответ: " + err.Error())
			return 2
		}
		_ = os.WriteFile(answerPath, b, 0o644)
		line("разбор сохранённого ответа, вызова нет")
	} else {
		planner, err := plannerRunner(cfg, tier)
		if err != nil {
			line("ОТКАЗ: " + err.Error())
			return 2
		}
		if code := callPlannerRunner(root, prompt, planner, answerPath, legacyScope(root)); code != 0 {
			return code
		}
	}

	var res claudeResult
	if err := readJSON(answerPath, &res); err != nil {
		line("ОТКАЗ: ответ разбивщика не разобран: " + err.Error())
		return 1
	}
	var payload struct {
		Result string `json:"result"`
	}
	_ = readJSON(answerPath, &payload)
	table := payload.Result

	costText, turnsText := "не измерено", "не измерено"
	if res.TotalCostUSD != nil {
		costText = fmt.Sprintf("$%.4f", *res.TotalCostUSD)
	}
	if res.NumTurns != nil {
		turnsText = fmt.Sprintf("%d", *res.NumTurns)
	}
	fmt.Printf("Замер: разбивка · вызовов 1 · %s · ходов %s"+lineEnding, costText, turnsText)

	// ОТВЕТ ПРОВЕРЯЕТСЯ ДО ЗАПИСИ. Разбивщик мог ответить прозой, пятью колонками или
	// пустотой; записать такое значит отдать человеку заготовку под видом плана.
	rows := plannerRows(table)
	if len(rows) == 0 {
		line("ОТКАЗ: в ответе нет ни одной строки шага — это не разбивка.")
		return 1
	}
	if cols := len(strings.Split(strings.Trim(strings.TrimSpace(rows[0]), "|"), "|")); cols != 4 {
		line(fmt.Sprintf("ОТКАЗ: в таблице %d колонок вместо четырёх. Такой план петля разберёт в НОЛЬ шагов и скажет «план пуст» — молча.", cols))
		return 1
	}
	var bad []string
	for _, r := range rows {
		parts := strings.Split(strings.Trim(strings.TrimSpace(r), "|"), "|")
		if len(parts) < 3 {
			continue
		}
		t := strings.Trim(strings.TrimSpace(parts[2]), "`")
		t = strings.TrimSpace(t)
		if t == "—" || t == "-" || strings.Contains(r, "гейт") {
			continue
		}
		if !resolveLadderTier(cfg.Ladder, t).Found {
			bad = append(bad, t)
		}
	}
	if len(bad) > 0 {
		line("ОТКАЗ: назначены ступени вне лестницы продукта — " + strings.Join(uniq(bad), ", "))
		return 1
	}

	// БЛОК ЦЕЛЕЙ ПРОВЕРЯЕТСЯ ДО ЗАПИСИ ТАК ЖЕ, КАК ТАБЛИЦА (этап 0.10, К4). Разбивка без
	// целей — та самая бумажка, которую петля не возьмёт; отдать её человеку под видом плана
	// значило бы отложить отказ до прогона. Ответ сохраняется: поправить его можно руками, не
	// платя за второй вызов.
	draft := filepath.Join(outDir, "planner-plan.md")
	_ = os.WriteFile(draft, []byte(table), 0o644)
	if problems := goalProblems(readPlanGoals(draft), readPlanSteps(draft)); len(problems) > 0 {
		line("ОТКАЗ: в разбивке нет годного блока целей — петля такой план не возьмёт. Ответ сохранён: " + draft)
		for _, p := range problems {
			line("  - " + p)
		}
		return 1
	}

	header := fmt.Sprintf(`# PLAN — предложен Планировщиком %s

Постановка: %s
Разбивка сделана ОДНИМ вызовом модели %s. Замер: %s, ходов %s.

ЭТО ПРЕДЛОЖЕНИЕ, А НЕ ПЛАН. План правится руками: машина исполняет, человек владеет
разбивкой. Перенеси в PLAN.md то, с чем согласен, — целиком или частями.

`, time.Now().Format("02.01.2006 15:04"), goal.Objective, tier, costText, turnsText)

	proposed := filepath.Join(root, "PLAN.proposed.md")
	_ = os.WriteFile(proposed, []byte(header+table+lineEnding), 0o644)
	line(fmt.Sprintf("шагов предложено: %d; записано: %s", len(rows), proposed))

	planPath := filepath.Join(root, "PLAN.md")
	if *apply {
		if _, err := os.Stat(planPath); err == nil {
			line("PLAN.md существует — НЕ ТРОГАЮ. У написанного плана есть владелец, и это не я.")
		} else {
			_ = os.WriteFile(planPath, []byte(header+table+lineEnding), 0o644)
			line("PLAN.md создан (его не было): " + planPath)
		}
	}

	fmt.Print(lineEnding)
	fmt.Print("Дальше: перенести согласованные шаги из PLAN.proposed.md в PLAN.md" + lineEnding)
	fmt.Print("От тебя жду: сверить разбивку и ступени — это единственное место, где решение твоё" + lineEnding)
	return 0
}

func plannerRows(table string) []string {
	var rows []string
	for _, l := range strings.Split(strings.ReplaceAll(table, "\r\n", "\n"), "\n") {
		if reStep.MatchString(l) {
			rows = append(rows, l)
		}
	}
	return rows
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// callPlanner — ОДИН вызов. Не «немного», не «сколько понадобится»: разбивка, требующая
// пяти заходов, — это разведка боем, от которой шаг и защищает.
func callClaudePlanner(root, prompt, model, answerPath string, scope sessionScope) int {
	// Своя копия поиска УБРАНА: она была верной, но вторая верная копия того же правила —
	// это и есть будущее расхождение. Ровно так петля и осталась без запасного пути.
	exe, err := resolveRunnerTool("claude")
	if err != nil {
		line("ОТКАЗ: " + err.Error() + ". Это «нечем исполнить», а не «модель не справилась».")
		return 2
	}
	line("зову разбивщика (один вызов)...")
	cmd := exec.Command(exe, "-p", "--model", model, "--output-format", "json")
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Dir = root
	// ТОКЕН — ТЕМ ЖЕ ПУТЁМ, ЧТО У ИСПОЛНИТЕЛЯ ПЕТЛИ. Исполнитель брал его из окружения
	// пользователя, а разбивщик — нет, и 14.09.2026 сессия Vera получила «Not logged in» при
	// том, что петля в тот же день на той же машине работала. Урок, живший в одном вызове, не
	// сработал в соседнем.
	if env, took := runnerEnv(); took {
		cmd.Env = env
		line("  токен взят из окружения пользователя (в процессе его не было)")
	}
	// К42 — durable job receipt пишется RUNNING ДО запуска разбивщика (один дорогой вызов).
	out, err := runReceipted(context.Background(), scope, "plan", "planner-claude", cmd)
	if code := exitCode(cmd, err); code != 0 {
		line(fmt.Sprintf("ОТКАЗ: разбивщик вернул код %d", code))
		line(strings.TrimSpace(decodeOutput(out)))
		return 1
	}
	_ = os.WriteFile(answerPath, out, 0o644)
	return 0
}

// buildPlannerPrompt — правило назначения ступени дано ЯВНО и с основанием: без него
// модель назначает по ожидаемой трудности, а надо — по природе шага. Это тот самый
// дефект, ради которого механизм и написан: модель на ступени script — дефект, а не выбор.
func buildPlannerPrompt(goalText, goalJSON, objNote, objText string, ladder, checks []string) string {
	checkList := "(в run-config.json не объявлено ни одной — сначала заведи судью)"
	if len(checks) > 0 {
		checkList = strings.Join(checks, "; ")
	}
	return `Ты размечаешь работу для петли Дятла Вуди. Тебя зовут ОДИН раз: разбивка делается
дорогим вызовом, чтобы дальше механическое исполнялось скриптами, а не моделью.

ЦЕЛЬ ПРОДУКТА
` + goalText + `

ОПИСАНИЕ ЦЕЛИ (goal.json)
` + goalJSON + `

ПОСТАНОВКА (` + objNote + `)
` + objText + `

ЛЕСТНИЦА ПРОДУКТА (других ступеней не существует)
` + strings.Join(ladder, ", ") + `

КАК НАЗНАЧАТЬ СТУПЕНЬ — ПО ПРИРОДЕ ШАГА, А НЕ ПО ОЖИДАЕМОЙ ТРУДНОСТИ:
- СНАЧАЛА проверь, можно ли получить и проверить точный результат скриптом.
  Если да — назначь script; вызов любой модели для такого шага запрещён. В таком шаге
  обязательно запиши точную команду после :: — бинарник отклоняет эту команду на model tier.
- детерминированное с проверяемым точным выходом -> script. Модель здесь ДЕФЕКТ:
  переименования, правка путей, генерация манифеста, сверка хэшей, счётчик, замена по
  образцу. Отдать такое модели значит заплатить за бесплатное.
- механическое, но текстовой формы -> haiku: применить известный образец ко многим
  файлам, заполнить шаблон, разложить список в структуру.
- обычный кодинг с названной причиной -> sonnet: функция, тест, починка.
- только там, где нужно СУЖДЕНИЕ -> opus: архитектура, неоднозначный отказ,
  противоречащие свидетельства.
- шаг, который закрывает не сессия, а человек (выпуск, покупка, доступ, перезагрузка,
  вход в учётную запись вендора), ступени не имеет: ставь тире и слово «гейт: ЛПР».

ПОРЯДОК ШАГОВ НЕ ПРОИЗВОЛЕН: то, что проверяется чем-то, идёт ПОСЛЕ того, чем
проверяется. Судья раньше тех, кого он судит.

ОТВЕТ — БЛОК ЦЕЛЕЙ, ЗАТЕМ ТАБЛИЦА ШАГОВ, без текста до и после.

БЛОК ЦЕЛЕЙ ОБЯЗАТЕЛЕН: без него петля план не возьмёт. Решение ЛПР 14.09.2026 — «без
целей план — бумажка, не видно, пришли к цели или нет». Цели и признаки достижения бери
из постановки, не выдумывай.

**Ц1.** что должно стать правдой, одной фразой

| Критерий | Цель | Признак достижения | Чем меряется |
|---|---|---|---|
| К1 | Ц1 | наблюдаемое состояние | проверка ` + "`имя проверки судьи`" + ` |

«Чем меряется» — ТОЛЬКО машинная грамматика без пояснительной прозы: проверка ` + "`имя`" + `;
если критерий закрывает конкретный тест — проверка ` + "`имя`" + ` · тест ` + "`точный селектор`" + `;
либо факт ` + "`id`" + `. Проверка с тестом обязана иметь шаблон select с {} в run-config.json.
Критерий, которому нечем меряться, судья объявит «нечем проверить». Проверки судьи продукта: ` + checkList + `

ТАБЛИЦА ШАГОВ — ровно четыре колонки. Лишняя колонка ломает разбор молча: план даёт ноль
шагов, и петля отвечает «план пуст» на живом плане. Каждый исполняемый шаг начинает колонку
«Судья» ссылками на критерии; шаг без ссылки — работа, которой цель не требует, и петля его
не исполнит. Закрытых шагов не помечай — всё предстоит.

| № | Шаг | Ступень | Судья |
|---|-----|---------|-------|
| 1 | что сделать, одной строкой, проверяемо | ` + "`script`" + ` | К1: чем проверяется |
| 2 | следующее | ` + "`sonnet`" + ` | К1: чем проверяется |
| 3 | выпуск версии | — | гейт: ЛПР |
`
}
