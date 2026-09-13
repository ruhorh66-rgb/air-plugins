package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
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
	Log     string            `json:"log"`
	Env     map[string]string `json:"env"`
}

type judgeSpec struct {
	Checks    []checkSpec `json:"checks"`
	Checklist string      `json:"checklist"`
	MinFacts  int         `json:"min_facts"`
}

type driftThresholds struct {
	StallThrottle    *int `json:"stall_moves_throttle"`
	StallEscalate    *int `json:"stall_moves_escalate"`
	UnverifiableStop *int `json:"unverifiable_streak_escalate"`
}

type runConfig struct {
	Judge     judgeSpec       `json:"judge"`
	Plan      string          `json:"plan"`
	GoalDrift driftThresholds `json:"goal_drift"`
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
	At            string `json:"at"`
	Code          int    `json:"code"`
	Distance      *int   `json:"distance"`
	ChecksPassed  int    `json:"checks_passed"`
	ChecksFailed  int    `json:"checks_failed"`
	ChecksUnknown int    `json:"checks_unknown"`
	FactsClosed   *int   `json:"facts_closed"`
	FactsGated    int    `json:"facts_gated"`
	FactsRequired int    `json:"facts_required"`
	VerdictText   string `json:"verdict_text"`
	By            string `json:"by"` // чем посчитано: две реализации живут рядом
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

	FactsClosed   *int
	FactsGated    int
	FactsRequired int
	FactsLine     string
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
	res := runJudge(root, cfg, *minFacts)
	code, text := verdict(res)
	publishVerdict(root, code, text, res)
	fmt.Print(text + lineEnding)
	return code
}

func runJudge(root string, cfg runConfig, minFactsOverride int) judgeResult {
	var r judgeResult

	for _, chk := range cfg.Judge.Checks {
		name := chk.Name
		if name == "" {
			name = "проверка без имени"
		}
		switch {
		case chk.Script != "":
			runScriptCheck(root, chk, name, &r)
		case chk.Command != "":
			runCommandCheck(root, chk, name, &r)
		default:
			r.Unknown = append(r.Unknown, name+" — нечем: в проверке не задан ни script, ни command")
		}
	}

	want := cfg.Judge.MinFacts
	if minFactsOverride >= 0 {
		want = minFactsOverride
	}
	r.FactsRequired = want
	if want > 0 {
		countFacts(root, cfg.Judge.Checklist, want, &r)
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
func runScriptCheck(root string, chk checkSpec, name string, r *judgeResult) {
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
	tail := ""
	for _, a := range chk.Args {
		tail += " '" + strings.ReplaceAll(a, "'", "''") + "'"
	}
	shell := "powershell.exe"
	if runtime.GOOS != "windows" {
		shell = "pwsh"
	}
	cmd := exec.Command(shell, "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command",
		preamble+"& "+quoted+tail+"; exit $LASTEXITCODE")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
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
func runCommandCheck(root string, chk checkSpec, name string, r *judgeResult) {
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
	cmd := exec.Command(resolved, chk.Args...)
	cmd.Dir = root
	if len(chk.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range chk.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	out, err := cmd.CombinedOutput()
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
func countFacts(root, checklistRel string, want int, r *judgeResult) {
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
	for _, it := range cl.Items {
		switch it.Status {
		case "completed":
			closed++
		case "gated":
			gated++
			if strings.TrimSpace(it.Awaits) == "" {
				noReason = append(noReason, it.ID)
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
	line := fmt.Sprintf("фактов закрыто %d из %d", closed, want)
	if gated > 0 {
		line += fmt.Sprintf(", из них %d ждут ЛПР", gated)
	}
	r.FactsLine = line
	if closed < want {
		r.Failed = append(r.Failed, line)
	} else {
		r.Passed = append(r.Passed, line)
	}
}

func verdict(r judgeResult) (int, string) {
	if len(r.Unknown) > 0 {
		text := "НЕ ПРОВЕРЕНО: " + strings.Join(r.Unknown, "; ")
		if len(r.Failed) > 0 {
			text += "; отдельно не пройдено: " + strings.Join(r.Failed, "; ")
		}
		return 2, text
	}
	if len(r.Failed) > 0 {
		return 1, "ЦЕЛЬ НЕ ДОСТИГНУТА: " + strings.Join(r.Failed, "; ")
	}
	summary := fmt.Sprintf("пройдено проверок %d", len(r.Passed))
	if r.FactsLine != "" {
		summary += ", " + r.FactsLine
	}
	return 0, "ЦЕЛЬ ДОСТИГНУТА: " + summary
}

// distanceOf — расстояние до цели по судье: сколько ещё закрывается РАБОТОЙ.
//
// Гейты вычитаются: работой они не лечатся. При коде 2 расстояние неизвестно, а не ноль —
// «ноль здесь читался бы как всё в порядке».
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
		short = r.FactsRequired - *r.FactsClosed - r.FactsGated
		if short < 0 {
			short = 0
		}
	}
	d := checksFailed + short
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
	_ = os.WriteFile(filepath.Join(root, ".goal-verdict"), []byte(text+lineEnding), 0o644)
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
		At:            time.Now().Format("2006-01-02T15:04:05"),
		Code:          code,
		Distance:      distanceOf(code, r),
		ChecksPassed:  passed,
		ChecksFailed:  failed,
		ChecksUnknown: len(r.Unknown),
		FactsClosed:   r.FactsClosed,
		FactsGated:    r.FactsGated,
		FactsRequired: r.FactsRequired,
		VerdictText:   text,
		By:            appName + " " + version,
	}
	if b, err := json.MarshalIndent(mv, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(root, ".goal-verdict.json"), b, 0o644)
	}
}
