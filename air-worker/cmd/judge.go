package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Судья цели. Возвращает 0 ТОЛЬКО когда цель достигнута.
//
// Судьёй не может быть модель: она объявляет победу рано. Решение «готово» принимает код
// возврата, а не мнение.
//
// Три кода, и третий — главное:
//
//	0  ЦЕЛЬ ДОСТИГНУТА    — всё проверено и пройдено
//	1  ЦЕЛЬ НЕ ДОСТИГНУТА — проверено и не пройдено, работать есть над чем
//	2  НЕ ПРОВЕРЕНО       — проверять нечем; работой не лечится, нужен человек
//
// Отсутствие доказательства нулевой оценкой НЕ является. Прежний судья считал пропавший
// реестр фактов за «закрыто 0 из 16», и «не доказано» становилось неотличимо от
// «доказательство уничтожено».

type checkSpec struct {
	Name    string            `json:"name"`
	Script  string            `json:"script"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Select  string            `json:"select"`
	Log     string            `json:"log"`
	Env     map[string]string `json:"env"`
}

type judgeSpec struct {
	Checks    []checkSpec `json:"checks"`
	Checklist string      `json:"checklist"`
	MinFacts  int         `json:"min_facts"`
	// Свой судья продукта берётся, ТОЛЬКО если он назван. Умолчание — универсальный:
	// продукт объявляет, ЧТО проверять, а не КАК. Раньше каждый продукт писал своего, и
	// частности одного протекали в образец для всех.
	Path string   `json:"path"`
	Args []string `json:"args"`
}

type budgetSpec struct {
	// Три потолка, любой останавливает петлю. Защита от разорения, а не настройка вкуса.
	Iterations        int     `json:"iterations"`
	USD               float64 `json:"usd"`
	TurnsPerIteration int     `json:"turns_per_iteration"`
	// Четвёртый предохранитель: прогонов без сдвига вердикта. Требование 7 нормы
	// AUTO-080 — механизм, молча жгущий квоту на работе, которая не идёт, хуже
	// остановленного: он ВЫГЛЯДИТ работающим.
	StallRuns int `json:"stall_runs"`
}

// PermissionMode — ПРАВА ОТЦЕПЛЁННОГО ИСПОЛНИТЕЛЯ.
//
// Найдено первым настоящим прогоном 13.09.2026, и это самый глубокий дефект продукта за
// всё время: механизм НЕ МОГ ВЫПОЛНИТЬ НИ ОДНОЙ РАБОТЫ. claude -p без прав на запись
// спрашивает подтверждение, которого в отцепленном прогоне дать некому, отвечает
// «Требуется ваше разрешение на запись файла» — и возвращает is_error=false. Петля видела
// успешный прогон, просто ничего не сделавший, и честно поднимала ступень.
//
// Цена измерена: $7.33 за две итерации, ноль изменённых файлов.
//
// Умолчание acceptEdits выбрано сознательно. Обратный вариант — требовать объявления в
// каждом продукте — уже проверен на этом же скиле 12.09.2026: порог входа оказался выше
// цены обойти инструмент, и обе сессии его обошли. Механизм, который нельзя запустить без
// ритуала, не запускают вовсе.
//
// Права НЕ МОЛЧАЛИВЫ: петля печатает их в заголовке прогона рядом с бюджетом. Человек
// обязан видеть, что отцеплённая модель сейчас получит право править его продукт, — и
// видеть это ДО первой итерации, а не по следам в git.
type runnerAuth struct {
	PermissionMode string `json:"permission_mode"`
	// AllowedTools — список инструментов исполнителя ОДНОЙ строкой через запятую.
	//
	// Форма существенна: флаг переменной арности, и раздельные аргументы
	// («--allowed-tools Write Edit») он разбирает не так, как ожидается. На этом я
	// потеряла ход 13.09.2026, заключив по такой пробе, что исполнитель писать не может
	// вовсе. Вывод был неверен: проверенная ветвь того же дня работала со списком через
	// запятую и файлы писала.
	AllowedTools string `json:"allowed_tools"`
}

type orchestrationSpec struct {
	Enabled   bool `json:"enabled"`
	Subagents int  `json:"subagents"`
}

type driftThresholds struct {
	StallThrottle    *int `json:"stall_moves_throttle"`
	StallEscalate    *int `json:"stall_moves_escalate"`
	UnverifiableStop *int `json:"unverifiable_streak_escalate"`
}

type runConfig struct {
	Judge         judgeSpec             `json:"judge"`
	Plan          string                `json:"plan"`
	GoalDrift     driftThresholds       `json:"goal_drift"`
	Ladder        []string              `json:"ladder"`
	Runners       map[string]runnerSpec `json:"runners"`
	Orchestration orchestrationSpec     `json:"orchestration"`
	Budget        budgetSpec            `json:"budget"`
	Runner        runnerAuth            `json:"runner"`
	OpenAI        openAIConfig          `json:"openai"`
}

type openAIConfig struct {
	BaseURL   string `json:"base_url"`
	APIKeyEnv string `json:"api_key_env"`
}

type factItem struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Awaits string `json:"awaits"`
}

type checklistFile struct {
	Items []factItem `json:"items"`
}

// machineVerdict — числа рядом с человеческим текстом.
//
// Заведено потому, что счётчик, считающий поиском подстроки в тексте причины, молча
// перестаёт расти, когда причину переписывают: этот отказ был найден у Goal/Drift Loop
// ВЕРЫ и описан там дословно. Текст остаётся человеку, числа — машине.
type machineVerdict struct {
	At             string   `json:"at"`
	Code           int      `json:"code"`
	Distance       *int     `json:"distance"`
	ChecksPassed   int      `json:"checks_passed"`
	ChecksFailed   int      `json:"checks_failed"`
	ChecksUnknown  int      `json:"checks_unknown"`
	CriteriaPassed []string `json:"criteria_passed,omitempty"`
	CriteriaFailed []string `json:"criteria_failed,omitempty"`
	// CriteriaGated — К40: критерии, ждущие решения ЛПР, отдельно от CriteriaUnknown
	// («нечем измерить»). Закрытые gate-факты сюда не попадают: они уже в CriteriaPassed.
	CriteriaGated   []string `json:"criteria_gated,omitempty"`
	CriteriaUnknown []string `json:"criteria_unknown,omitempty"`
	CriteriaTotal   int      `json:"criteria_total"`
	// LPRGates — К40: ВСЕ гейты ЛПР (гейты плана + CriteriaGated), одним числом, отдельно
	// от executable-остатка. Закрытые gate-шаги плана в это число не входят.
	LPRGates      int  `json:"lpr_gates"`
	FactsClosed   *int `json:"facts_closed"`
	FactsGated    int  `json:"facts_gated"`
	FactsRequired int  `json:"facts_required"`
	// FactsOverlap — К59: факты, которые legacy `min_facts` считал бы «не хватает», хотя они
	// уже являются мерой критерия и учтены в CriteriaFailed/CriteriaGated. Названы здесь,
	// чтобы отчёт мог предупредить о дублирующем подсчёте, а не просто занизить число молча.
	FactsOverlap []string `json:"facts_overlap,omitempty"`
	VerdictText  string   `json:"verdict_text"`
	By           string   `json:"by"` // чем посчитано: две реализации живут рядом
}

var reFailLine = regexp.MustCompile(`\[FAIL\]|ОТКАЗ|НЕЧЕМ|FAIL|Exception|ошибка`)
var reCmdFailLine = regexp.MustCompile(`^(FAIL|---\s+FAIL|# |panic:|Error|ОШИБКА|.*:\d+:)`)

func firstMatch(text string, re *regexp.Regexp) string {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if re.MatchString(line) {
			return strings.TrimSpace(line)
		}
	}
	return ""
}

func lastNonEmpty(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if s := strings.TrimSpace(lines[i]); s != "" {
			return s
		}
	}
	return ""
}

type judgeResult struct {
	Passed  []string
	Failed  []string
	Unknown []string

	CriteriaPassed  []string
	CriteriaFailed  []string
	CriteriaGated   []string
	CriteriaUnknown []string
	// PlanGates — открытые гейты самого плана (см. PlanState); заполняется runJudge через
	// buildPlanState, тем же кодом, что и остальные потребители PlanState.
	PlanGates int

	FactsClosed   *int
	FactsGated    int
	FactsRequired int
	FactsLine     string
	// FactsOverlap — К59: id фактов, которые legacy min_facts посчитал бы «недостающими», но
	// которые уже являются мерой критерия и не считаются дважды в distanceOf.
	FactsOverlap []string
}

func cmdJudge(argv []string) int {
	fs := flag.NewFlagSet("judge", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	configPath := fs.String("config", "", "путь к run-config.json")
	minFacts := fs.Int("min-facts", -1, "перекрыть требуемое число фактов")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Printf("НЕ ПРОВЕРЕНО: не разобран путь продукта: %v\n", err)
		return 2
	}
	cfgPath := *configPath
	if cfgPath == "" {
		cfgPath = filepath.Join(root, "run-config.json")
	}
	var cfg runConfig
	if err := readJSON(cfgPath, &cfg); err != nil {
		fmt.Printf("НЕ ПРОВЕРЕНО: нет конфигурации %s\n", cfgPath)
		return 2
	}
	// ПЛАН КАК ФАЙЛ ОБЯЗАТЕЛЕН И ЗДЕСЬ (этап 0.10, К32). До этой правки судья без плана
	// выносил «цель достигнута»: criteriaBinding молчит, когда критериев ноль, а
	// критериев ноль, когда план читать нечем, — отсутствие плана читалось как согласие.
	if _, code, ok := requirePlan(root, cfg, "судья"); !ok {
		return code
	}
	res := runJudge(root, cfg, *minFacts, legacyScope(root))
	code, text := verdict(res)
	publishVerdict(root, code, text, res)
	fmt.Print(text + lineEnding)
	return code
}

func runJudge(root string, cfg runConfig, minFactsOverride int, scope sessionScope) judgeResult {
	var r judgeResult

	for _, chk := range cfg.Judge.Checks {
		name := chk.Name
		if name == "" {
			name = "проверка без имени"
		}
		switch {
		case chk.Script != "":
			runScriptCheck(root, chk, name, scope, &r)
		case chk.Command != "":
			runCommandCheck(root, chk, name, scope, &r)
		default:
			r.Unknown = append(r.Unknown, name+" — нечем: в проверке не задан ни script, ни command")
		}
	}

	// КРИТЕРИЙ ПЛАНА БЕЗ МЕРЫ — «НЕЧЕМ ПРОВЕРИТЬ» (этап 0.10, К3). Судья отвечает не только
	// за проверки из конфигурации, но и за то, что каждому критерию цели в плане есть чем
	// меряться: критерий, на который никто не смотрит, закрывается словами, а слова механизм
	// не принимает. План без блока целей здесь не судится — его не возьмёт петля, и отказ
	// назван там.
	//
	// К40 — план читается ОДИН РАЗ здесь, через buildPlanState (run-config.json -> plan —
	// единственный активный PLAN): goals/report/drift читают тот же путь той же функцией.
	planName := cfg.Plan
	if planName == "" {
		planName = "PLAN.md"
	}
	planPath := filepath.Join(root, planName)
	g := readPlanGoals(planPath)

	// К59 — факты, уже являющиеся мерой критерия, размечаются ДО подсчёта legacy min_facts,
	// чтобы недостача по ним не вошла в distance дважды: один раз через CriteriaFailed/Gated,
	// второй раз через устаревший короткий подсчёт «фактов не хватает».
	criterionFacts := map[string]bool{}
	for _, c := range g.Criteria {
		for _, id := range c.Facts {
			criterionFacts[id] = true
		}
	}

	want := cfg.Judge.MinFacts
	if minFactsOverride >= 0 {
		want = minFactsOverride
	}
	r.FactsRequired = want
	if want > 0 {
		countFacts(root, cfg.Judge.Checklist, want, criterionFacts, &r)
	}

	if len(g.Criteria) > 0 {
		bindings := criteriaBinding(root, cfg, g)
		if len(bindings) > 0 {
			r.CriteriaUnknown = append(r.CriteriaUnknown, bindings...)
		} else {
			ps := buildPlanState(root, cfg, planPath, r)
			r.CriteriaPassed, r.CriteriaFailed, r.CriteriaGated, r.CriteriaUnknown = ps.CriteriaPassed, ps.CriteriaFailed, ps.CriteriaGated, ps.CriteriaUnknown
			r.PlanGates = ps.PlanGates
		}
	} else {
		r.PlanGates = parsePlan(planPath).Gates()
	}
	return r
}

// runScriptCheck — проверка-скрипт. Отдельным процессом и с рабочим каталогом продукта.
//
// Отдельным — потому что вызванная внутри своего процесса проверка своим `exit 1`
// завершила бы самого судью: он отдал бы чужой код вместо вердикта, а остальные условия
// остались бы непроверенными.
//
// С рабочим каталогом — потому что ветви были несимметричны, и это порождало дефект в
// каждом продукте: проверка запускалась ниоткуда и должна была угадать, где продукт.
var powerShellNamedArgument = regexp.MustCompile("^-[A-Za-z][A-Za-z0-9_-]*$")

func powerShellScriptTail(args []string, namedIndices []int) string {
	var tail strings.Builder
	for i, arg := range args {
		tail.WriteByte(32)
		if slices.Contains(namedIndices, i) && powerShellNamedArgument.MatchString(arg) {
			tail.WriteString(arg)
			continue
		}
		tail.WriteByte(39)
		tail.WriteString(strings.ReplaceAll(arg, "'", "''"))
		tail.WriteByte(39)
	}
	return tail.String()
}
func runScriptCheck(root string, chk checkSpec, name string, scope sessionScope, r *judgeResult, namedIndices ...int) {
	scriptPath := filepath.Join(root, chk.Script)
	if _, err := os.Stat(scriptPath); err != nil {
		r.Unknown = append(r.Unknown, fmt.Sprintf("%s — нечем: нет %s", name, scriptPath))
		return
	}
	// Преамбула ставит дочернему процессу UTF-8 ДО первой строки вывода. Без неё
	// PowerShell пишет перенаправленный вывод в своей консольной кодировке и подставляет
	// вопросительные знаки ПРИ ЗАПИСИ — потеря происходит у источника, и чтением её не
	// вернуть.
	preamble := `[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); $OutputEncoding=[System.Text.UTF8Encoding]::new($false); `
	quoted := "'" + strings.ReplaceAll(scriptPath, "'", "''") + "'"
	tail := powerShellScriptTail(chk.Args, namedIndices)
	cmd := newPowerShellCommand("-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
		preamble+"& "+quoted+tail+"; exit $LASTEXITCODE")
	cmd.Dir = root
	// К42 — durable job receipt пишется RUNNING ДО запуска этой проверки; transport/RDC
	// timeout здесь не наступает (ctx без дедлайна), поэтому вызов, как и раньше, ждёт
	// завершения синхронно — только теперь ещё и оставляет receipt/output_path на диске.
	out, err := runReceipted(context.Background(), scope, "judge", name, cmd)
	code := exitCode(cmd, err)
	text := decodeOutput(out)
	if chk.Log != "" {
		_ = os.WriteFile(filepath.Join(root, chk.Log), []byte(text), 0o644)
	}
	if code == 0 {
		r.Passed = append(r.Passed, name)
		return
	}
	// Вывод проверки не выбрасывается: вердикт «не проходит» без причины заставляет
	// строить гипотезы вместо работы.
	why := firstMatch(text, reFailLine)
	if why == "" {
		why = lastNonEmpty(text)
	}
	if why != "" {
		why = " — " + why
	}
	r.Failed = append(r.Failed, fmt.Sprintf("%s (код %d)%s", name, code, why))
}

// runCommandCheck — проверка произвольной командой.
//
// РЕЗОЛВ НЕ ДОКАЗЫВАЕТ НАЛИЧИЯ ИНСТРУМЕНТА, ДОКАЗЫВАЕТ ТОЛЬКО ОТВЕТ. В WindowsApps лежат
// алиасы-заглушки магазина: поиск их находит, вызов отвечает «не найдено» с кодом 9009 —
// и отсутствующий инструмент превращался в претензию к продукту, то есть третий код
// обходился любой заглушкой на PATH.
func runCommandCheck(root string, chk checkSpec, name string, scope sessionScope, r *judgeResult) {
	resolved, err := exec.LookPath(chk.Command)
	if err != nil {
		r.Unknown = append(r.Unknown, fmt.Sprintf("%s — нечем: команда '%s' не резолвится", name, chk.Command))
		return
	}
	if strings.Contains(strings.ToLower(resolved), `\windowsapps\`) {
		if fi, err := os.Lstat(resolved); err == nil {
			if fi.Size() == 0 || fi.Mode()&os.ModeSymlink != 0 {
				r.Unknown = append(r.Unknown, fmt.Sprintf(
					"%s — нечем: '%s' ведёт на алиас-заглушку магазина (%s), а не на инструмент",
					name, chk.Command, resolved))
				return
			}
		}
	}
	cmd := newChildCommand(resolved, chk.Args...)
	cmd.Dir = root
	if len(chk.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range chk.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Env = configuredChildEnvironment(resolved, cmd.Env)
	}
	out, err := runReceipted(context.Background(), scope, "judge", name, cmd)
	code := exitCode(cmd, err)
	text := decodeOutput(out)
	if chk.Log != "" {
		_ = os.WriteFile(filepath.Join(root, chk.Log), []byte(text), 0o644)
	}
	if code == 0 {
		r.Passed = append(r.Passed, name)
		return
	}
	// 9009 — «команда не найдена» у интерпретатора: процесс НЕ исполнился. Второй рубеж
	// после проверки заглушки — он ловит и те обманки, о которых мы ещё не знаем.
	if code == 9009 {
		r.Unknown = append(r.Unknown, fmt.Sprintf(
			"%s — нечем: команда '%s' не исполнилась (код 9009: не найдена, хотя и резолвилась)",
			name, chk.Command))
		return
	}
	why := firstMatch(text, reCmdFailLine)
	if why != "" {
		why = " — " + why
	}
	r.Failed = append(r.Failed, fmt.Sprintf("%s (код %d)%s", name, code, why))
}

// countFacts — реестр фактов. Живёт С ПРОДУКТОМ и под git.
//
// ГЛАВНОЕ здесь: пропажа реестра — это «не проверено», а не «закрыто 0».
//
// Факт со статусом gated закрывает человек, а не сессия, и он ОБЯЗАН нести awaits с
// названным действием. Без этого требования метка стала бы местом, куда складывают
// неудобное. Гейты видны в тексте вердикта и вычтены из расстояния — прятать за меткой
// работу нельзя, а обвинять сессию в дрейфе за чужое бездействие нечестно.
func countFacts(root, checklistRel string, want int, criterionFacts map[string]bool, r *judgeResult) {
	if checklistRel == "" {
		r.Unknown = append(r.Unknown, fmt.Sprintf(
			"реестр фактов — нечем: judge.checklist не назван, а требуется %d фактов", want))
		return
	}
	clPath := filepath.Join(root, checklistRel)
	if _, err := os.Stat(clPath); err != nil {
		r.Unknown = append(r.Unknown, "реестр фактов — нечем: нет "+clPath)
		return
	}
	var cl checklistFile
	if err := readJSON(clPath, &cl); err != nil {
		r.Unknown = append(r.Unknown, fmt.Sprintf("реестр фактов — нечем: %s не разобран (%v)", clPath, err))
		return
	}
	closed, gated := 0, 0
	var noReason []string
	var overlap []string
	for _, it := range cl.Items {
		switch it.Status {
		case "completed":
			closed++
		case "gated":
			gated++
			if strings.TrimSpace(it.Awaits) == "" {
				noReason = append(noReason, it.ID)
			}
		default:
			// К59 — факт не закрыт и уже является мерой критерия: недостача по нему считана
			// критерием (CriteriaFailed/CriteriaGated), а не legacy-счётом ниже.
			if criterionFacts[it.ID] {
				overlap = append(overlap, it.ID)
			}
		}
	}
	if len(noReason) > 0 {
		r.Unknown = append(r.Unknown, fmt.Sprintf(
			"факт помечен гейтом без названной причины (%s): поле awaits обязано называть, чего ждёт человек — иначе метка прячет работу",
			strings.Join(noReason, ", ")))
	}
	r.FactsClosed = &closed
	r.FactsGated = gated
	r.FactsOverlap = overlap
	line := fmt.Sprintf("фактов закрыто %d из %d", closed, want)
	if gated > 0 {
		line += fmt.Sprintf(", из них %d ждут ЛПР", gated)
	}
	if len(overlap) > 0 {
		line += fmt.Sprintf("; из недостающих %d уже считаны критерием (не дублируются): %s", len(overlap), strings.Join(overlap, ", "))
	}
	r.FactsLine = line
	if closed < want {
		r.Failed = append(r.Failed, line)
	} else {
		r.Passed = append(r.Passed, line)
	}
}

// verdict — К40: GATED критериев НЕ хватает в unknown (это не «нечем измерить», а «измерено,
// ждёт ЛПР») и не превращают код 2 в код «непонятно, что чинить». Пока не осталось ни
// unknown, ни failed, а gated-критерии ещё висят, цель честно НЕ ДОСТИГНУТА кодом 1, с
// текстом, отдельным от provalившихся проверок — работой это не закрыть, надо звать ЛПР.
func verdict(r judgeResult) (int, string) {
	unknown := append(append([]string{}, r.Unknown...), r.CriteriaUnknown...)
	failed := append(append([]string{}, r.Failed...), r.CriteriaFailed...)
	if len(unknown) > 0 {
		text := "НЕ ПРОВЕРЕНО: " + strings.Join(unknown, "; ")
		if len(failed) > 0 {
			text += "; отдельно не пройдено: " + strings.Join(failed, "; ")
		}
		return 2, text
	}
	if len(failed) > 0 {
		return 1, "ЦЕЛЬ НЕ ДОСТИГНУТА: " + strings.Join(failed, "; ")
	}
	if len(r.CriteriaGated) > 0 {
		return 1, "ЦЕЛЬ НЕ ДОСТИГНУТА: ждёт ЛПР (гейт, не работа): " + strings.Join(r.CriteriaGated, "; ")
	}
	summary := fmt.Sprintf("пройдено проверок %d", len(r.Passed))
	if len(r.CriteriaPassed) > 0 {
		summary += fmt.Sprintf(", критериев %d", len(r.CriteriaPassed))
	}
	if r.FactsLine != "" {
		summary += ", " + r.FactsLine
	}
	return 0, "ЦЕЛЬ ДОСТИГНУТА: " + summary
}

// distanceOf — расстояние до цели по судье: сколько ещё закрывается РАБОТОЙ.
//
// Гейты вычитаются: работой они не лечатся — ни гейты плана, ни CriteriaGated. При коде 2
// расстояние неизвестно, а не ноль — «ноль здесь читался бы как всё в порядке».
//
// К59 — len(r.FactsOverlap) вычитается из legacy-недостачи: факт, уже считанный как мера
// критерия (через CriteriaFailed/CriteriaGated), не добавляет вторую единицу расстояния
// только потому, что тот же факт входит и в устаревший min_facts.
func distanceOf(code int, r judgeResult) *int {
	if code == 2 {
		return nil
	}
	checksFailed := len(r.Failed)
	if r.FactsLine != "" {
		for _, f := range r.Failed {
			if f == r.FactsLine {
				checksFailed--
				break
			}
		}
	}
	short := 0
	if r.FactsRequired > 0 && r.FactsClosed != nil {
		short = r.FactsRequired - *r.FactsClosed - r.FactsGated - len(r.FactsOverlap)
		if short < 0 {
			short = 0
		}
	}
	d := checksFailed + len(r.CriteriaFailed) + short
	return &d
}

func publishVerdict(root string, code int, text string, r judgeResult) {
	// ЗАВЕРШАЮЩИЙ ПЕРЕВОД СТРОКИ — КАК У СКРИПТА (CRLF на Windows).
	// Найдено AIR-ENV-002 13.09.2026 побайтовой сверкой: 105 байт против 104, diff
	// расхождение видит, глаз нет. Её же довод и решил вопрос: всё, что сравнивает файл
	// вердикта БАЙТАМИ — хеш, diff, «изменился ли вердикт с прошлого прогона», — при
	// чередовании двух реализаций увидело бы изменение на каждом прогоне, хотя не
	// изменилось ничего. Ложное движение вместо ложного застоя, зеркало той беды, что
	// лечит двигатель цели.
	// АТОМАРНО, потому что читателей у вердикта много и они в разных процессах: страж
	// хода, двигатель цели, значок в трее, соседняя сессия. os.WriteFile НЕ атомарен —
	// читатель может застать файл усечённым, и «вердикта нет» станет неотличимо от
	// «вердикт пуст». Замок здесь не нужен: атомарная запись дешевле и надёжнее.
	_ = writeFileAtomic(filepath.Join(root, ".goal-verdict"), []byte(text+lineEnding))
	passed := len(r.Passed)
	failed := len(r.Failed)
	if r.FactsLine != "" {
		for _, p := range r.Passed {
			if p == r.FactsLine {
				passed--
				break
			}
		}
		for _, f := range r.Failed {
			if f == r.FactsLine {
				failed--
				break
			}
		}
	}
	mv := machineVerdict{
		At:              time.Now().Format("2006-01-02T15:04:05"),
		Code:            code,
		Distance:        distanceOf(code, r),
		ChecksPassed:    passed,
		ChecksFailed:    failed,
		ChecksUnknown:   len(r.Unknown),
		CriteriaPassed:  r.CriteriaPassed,
		CriteriaFailed:  r.CriteriaFailed,
		CriteriaGated:   r.CriteriaGated,
		CriteriaUnknown: r.CriteriaUnknown,
		CriteriaTotal:   len(r.CriteriaPassed) + len(r.CriteriaFailed) + len(r.CriteriaGated) + len(r.CriteriaUnknown),
		LPRGates:        r.PlanGates + len(r.CriteriaGated),
		FactsClosed:     r.FactsClosed,
		FactsGated:      r.FactsGated,
		FactsRequired:   r.FactsRequired,
		FactsOverlap:    r.FactsOverlap,
		VerdictText:     text,
		By:              appName + " " + version,
	}
	if b, err := json.MarshalIndent(mv, "", "  "); err == nil {
		_ = writeFileAtomic(filepath.Join(root, ".goal-verdict.json"), b)
	}
}
