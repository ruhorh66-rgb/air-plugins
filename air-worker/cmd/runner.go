package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// Исполнители: ступень говорит, СКОЛЬКО мы готовы заплатить, исполнитель — ЧЕМ именно
// исполняем. Разведены потому, что механизм обязан работать не только с Claude.

var reNonWord = regexp.MustCompile(`[^\p{L}\p{Nd}]+`)

func runnerCapabilityLines(kind string) []string {
	if kind == "codex" {
		return []string{"- files may be read, created and edited;", "- Codex runs with workspace-write inside the product root and may run local tests allowed by the sandbox;", "- do not request broader permissions, commit/push, or rewrite the judge;", "- the external judge independently verifies the result after the turn."}
	}
	return []string{"- files may be read, created and edited;", "- local commands are not available to this detached runner;", "- the external loop runs build, tests and judge after the turn."}
}

// Ответ исполнителя, которому не дали прав. Образец широкий намеренно: формулировка
// клиента меняется от версии к версии и от языка, а последствие всегда одно — работы не
// было. Узкий образец промолчал бы на новой формулировке, и мы вернулись бы к тому, что
// платим за пустые прогоны.
var reNeedsPermission = regexp.MustCompile(`(?i)(разреш|подтверд|permission|approve|allow this)`)

// runModelStep — шаг, исполняемый моделью.
//
// ЗАДАНИЕ ПЕРЕДАЁТСЯ ПУТЁМ К ФАЙЛУ, А НЕ ТЕКСТОМ. Требование 3 нормы AUTO-080, купленное
// там дважды: «длинный текст в командной строке до исполнителя не доезжает: он отвечает
// "Понял, что нужно сделать?" и делает ноль работы». 12.09.2026 куплено в третий раз — на
// отцепленных ветвях задание, переданное аргументом, порвалось на пробелах, и ветвь
// отработала вслепую, потратив доллар.
//
// Это же закрывает роль ЦЕЛЬ: по норме цель — «файл задания, который исполнитель читает
// первым действием», а не текст в промпте.
func (c *loopCtx) runModelStep(step workStep, tier, judgeText string, runner runnerSpec) stepResult {
	var b strings.Builder
	w := func(s string) { b.WriteString(s + "\r\n") }
	w("Задача: " + step.Title)
	// КАК ЗАКРЫВАЕТСЯ ШАГ — ИСПОЛНИТЕЛЮ ПРЯМО. Живой случай 14.09.2026, ASW, шаг 8а: итерации
	// 5–6 на sonnet ($2.59) не сдвинули вердикт — код шага уже лежал в дереве, а номер в
	// плане никто не зачеркнул. Зачеркнула итерация 7 на opus и сама написала, что именно
	// этого не хватало. Правило закрытия жило в плане и в судье, но не в задании.
	planName := c.Cfg.Plan
	if planName == "" {
		planName = "PLAN.md"
	}
	if step.Judge != "" {
		w("Критерий шага: " + step.Judge)
	}
	// Метка критерия исполнителю ничего не говорит: «К2» без текста — шифр. Поэтому
	// критерий уходит в задание целиком — что должно стать правдой и чем это меряется.
	for _, id := range stepCriteria(step) {
		if cr, ok := c.goals.criterion(id); ok {
			w(fmt.Sprintf("%s — %s; меряется: %s", cr.ID, cr.Sign, cr.Measure))
		}
	}
	if strings.HasPrefix(step.Title, step.Num+". ") {
		w(fmt.Sprintf("Шаг закрывается зачёркнутым номером в %s: «| ~~%s~~ |». Зачеркни номер в том же ходе, что и работу,",
			planName, step.Num))
	} else {
		w(fmt.Sprintf("Шаг закрывается отметкой «- [x]» в %s. Отметь его в том же ходе, что и работу,", planName))
	}
	w("и только когда критерий выполнен. Без закрытия сделанная работа не видна судье: вердикт не")
	w("сдвинется, и петля поднимет ступень — оплатит то же дороже.")
	if c.repairNote != "" {
		w("")
		w("ПРОШЛАЯ ИТЕРАЦИЯ СДЕЛАЛА ХУЖЕ: " + c.repairNote + ".")
		w("Сначала почини сломанное — что именно, называет вердикт судьи ниже. Если номер шага уже")
		w("зачёркнут, а критерий не выполнен, сними зачёркивание: закрытие при красном судье не засчитывается.")
	}
	w("")
	w("Цель проверяется судьёй, а не тобой. Переписывать судью запрещено.")
	if c.JudgePath != "" {
		w("Судья: " + c.JudgePath + " " + strings.Join(c.JudgeArgs, " "))
	} else {
		w("Судья: " + appName + " judge -product " + c.Root)
	}
	if judgeText != "" {
		w("")
		w("Последний вердикт судьи:")
		w(judgeText)
	}
	w("")
	w("EXECUTOR CAPABILITIES:")
	for _, msg := range runnerCapabilityLines(runner.Kind) {
		w(msg)
	}
	w("Write code and explain the change. Final acceptance belongs to the external judge.")
	w("")
	if c.Orchestrate {
		w("")
		w(fmt.Sprintf("Режим оркестрации: раздай работу %d субагентам и сведи результат.", c.Subagents))
		w("Оркестрация оправдана только на независимых шагах: на связанных она")
		w("умножает ходы, а ходы и есть цена.")
	}

	taskDir := filepath.Join(c.Root, ".woody")
	_ = os.MkdirAll(taskDir, 0o755)
	safe := strings.Trim(reNonWord.ReplaceAllString(step.Title, "-"), "-")
	if len([]rune(safe)) > 40 {
		safe = string([]rune(safe)[:40])
	}
	taskFile := filepath.Join(taskDir, fmt.Sprintf("TASK-%d-%s.md", step.Index, safe))
	_ = os.WriteFile(taskFile, append(utf8BOM, []byte(b.String())...), 0o644)

	// В командную строку уходит КОРОТКАЯ директива с путём: она умещается в аргумент
	// целиком и не зависит от кавычек, пробелов и кодировки консоли.
	prompt := "Прочитай ПЕРВЫМ ДЕЙСТВИЕМ файл с заданием: " + taskFile + "\n" +
		"В нём задача, судья и ограничения итерации. Работай по нему.\n" +
		"Судью не переписывай: решение «готово» принимает его код возврата, а не твоё мнение."

	eff := runner.Effort
	if eff == "" {
		eff = "по умолчанию"
	}
	line(fmt.Sprintf("  исполнитель: %s · модель %s · усилие %s · потолок ходов %d",
		runner.Kind, runner.Model, eff, c.MaxTurns))

	// Расход, которого поток НЕ ПРИНЁС, пишется как nil, а не как ноль: ноль означает
	// «работа была и стоила нисколько», и бюджет считается в сторону «можно ещё».
	if c.WhatIf {
		return stepResult{Ok: true, Session: "whatif", Subtype: "whatif"}
	}

	tool := "claude"
	if runner.Kind == "codex" {
		tool = "codex"
	}
	// Исполнитель ищется ОБЩЕЙ функцией: явное перекрытие, PATH, известные расположения.
	// Здесь стоял голый exec.LookPath — и на машине, где клиент лежит в профиле и на PATH
	// его нет, петля объявила бы «исполнителя нет» при наличном исполнителе. Тот же
	// дефект, что дважды чинен в этом продукте до меня; я внесла его третьим.
	exePath, err := resolveRunnerTool(tool)
	if err != nil {
		// Инструмента нет — это «нечем исполнить», а не «модель не справилась».
		// Различать обязательно: иначе петля поднимет ступень и заплатит за то же.
		line("  ОТКАЗ: " + err.Error())
		return stepResult{Subtype: "no_runner"}
	}
	if runner.Kind == "codex" {
		return c.invokeCodex(exePath, prompt, runner)
	}
	return c.invokeClaude(exePath, prompt, runner)
}

// Detail несёт текст отказа исполнителя. Без него петля могла бы только сказать «не
// получилось», и человек шёл бы искать причину сам — а она уже пришла в ответе.
type claudeResult struct {
	IsError      bool     `json:"is_error"`
	TotalCostUSD *float64 `json:"total_cost_usd"`
	NumTurns     *int     `json:"num_turns"`
	SessionID    string   `json:"session_id"`
	Subtype      string   `json:"subtype"`
	DurationAPI  *int     `json:"duration_api_ms"`
	Type         string   `json:"type"`
	Result       string   `json:"result"`
}

func (c *loopCtx) invokeClaude(exePath, prompt string, runner runnerSpec) stepResult {
	args := []string{"-p", prompt, "--model", runner.Model, "--output-format", "json",
		"--max-turns", fmt.Sprintf("%d", c.MaxTurns)}
	// Без этого отцеплённый исполнитель не может писать файлы вовсе: он спрашивает
	// подтверждение, которого некому дать, и возвращает «успех», ничего не сделав.
	if c.Permission != "" {
		args = append(args, "--permission-mode", c.Permission)
	}
	// ОДНОЙ СТРОКОЙ ЧЕРЕЗ ЗАПЯТУЮ. Флаг переменной арности: раздельные аргументы он
	// разбирает иначе, и проба с ними заставила меня заключить, что исполнитель писать не
	// может вовсе. Заключение было неверным — проверенная ветвь того же дня писала файлы
	// именно со списком через запятую.
	if c.Tools != "" {
		args = append(args, "--allowed-tools", c.Tools)
	}
	// Уровни усилия: low, medium, high, xhigh, max. Неизвестное значение CLI не отвергает,
	// а МОЛЧА берёт умолчание — печатает предупреждение и идёт дальше. Поэтому опечатка в
	// лестнице обошлась бы дороже ошибки: прогон состоялся бы не на том усилии, и замер
	// приписали бы не той ступени.
	if runner.Effort != "" {
		args = append(args, "--effort", runner.Effort)
	}
	// Бюджет отдаётся самому CLI, а не считается постфактум: так он останавливается ДО
	// перерасхода, а не после.
	if left := c.MaxUSD - c.spent; left > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%.2f", left))
	}
	cmd := exec.Command(exePath, args...)
	cmd.Dir = c.Root
	if env, took := runnerEnv(); took {
		cmd.Env = env
		line("  токен взят из окружения пользователя (в процессе его не было)")
	}
	out, _ := cmd.CombinedOutput()
	raw := decodeOutput(out)

	// JSON вынимается ПОСТРОЧНО, а не разбором всего вывода: claude печатает
	// предупреждения в тот же поток ПЕРЕД результатом, и разбор всего вывода падает.
	// Прогон при этом состоялся и деньги потрачены — то есть «не смог разобрать»
	// выглядело бы как отказ модели.
	var res *claudeResult
	for _, l := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if !strings.HasPrefix(t, "{") {
			continue
		}
		var o claudeResult
		if json.Unmarshal([]byte(t), &o) != nil {
			continue
		}
		if o.Type == "result" || o.TotalCostUSD != nil {
			cp := o
			res = &cp
		}
	}
	if res == nil {
		line("  claude не вернул разбираемый результат; первые строки ответа:")
		for i, l := range strings.Split(raw, "\n") {
			if i >= 3 {
				break
			}
			line("    " + strings.TrimRight(l, "\r"))
		}
		return stepResult{Subtype: "unparsed"}
	}
	// ПОЛЕ subtype ВРЁТ. Живой ответ claude на «Not logged in» приходит с is_error=true и
	// subtype="success" одновременно: клиент считает успехом сам факт ответа, а не работу.
	// Петля смотрела только на subtype, отказа не замечала и шла поднимать ступень —
	// оплачивая дороже вызов, который вообще не состоялся. Решает is_error, а не subtype.
	sub := res.Subtype
	if res.IsError {
		sub = "runner_error"
	}
	// ИСПОЛНИТЕЛЬ ПРОСИТ РАЗРЕШЕНИЯ — ЭТО НЕ ОТКАЗ МОДЕЛИ И НЕ РАБОТА.
	//
	// Найдено первым настоящим прогоном 13.09.2026 ценой $7.33 за две итерации и НОЛЬ
	// изменённых файлов. Отцеплённый claude -p не может писать: он просит подтверждение,
	// которого в отцепленном прогоне дать некому, — и возвращает is_error=false. То есть
	// для петли это выглядело как удавшийся прогон, просто ничего не сделавший, и она
	// честно поднимала ступень, оплачивая дороже то, что не могло исполниться в принципе.
	//
	// Ни --permission-mode acceptEdits, ни --allowedTools Write Edit этого не снимают:
	// проверено прогоном, оба раза ответ тот же. Снимает только полное отключение
	// проверок, а это решение ЛПР, а не механизма.
	//
	// Различать обязательно: «нечем исполнить» ведёт к человеку, «модель не справилась» —
	// к подъёму ступени. Спутать их значит платить дороже за то, что не исполнится.
	// РЕШАЕТ ФАКТ, А НЕ ТЕКСТ. Первая редакция объявляла «нет прав» по одному лишь
	// совпадению слова в ответе — и немедленно дала ЛОЖНОЕ срабатывание на живом прогоне
	// 13.09.2026: исполнитель СДЕЛАЛ работу (два новых пакета, семь тестов, шаг плана
	// закрыт, вердикт судьи сдвинулся с 24 на 23) и в конце законно попросил прав на
	// go build и git commit. Петля остановилась со словами «работы не было» — прямо
	// противоположными тому, что произошло.
	//
	// Это тот же класс, который вычищался весь день: суждение по ТЕКСТУ вместо ЗАМЕРА.
	// Теперь «нет прав» объявляется, только если исполнитель просил прав И рабочее дерево
	// не изменилось: просьба без работы — это отказ, просьба после работы — это просьба.
	if !res.IsError && reNeedsPermission.MatchString(res.Result) && !c.treeChanged() {
		sub = "needs_permission"
	}
	return stepResult{
		Ok: !res.IsError, Cost: res.TotalCostUSD, Turns: res.NumTurns,
		Session: res.SessionID, Subtype: sub, ApiMs: res.DurationAPI,
		Detail: strings.TrimSpace(res.Result),
	}
}

type codexEvent struct {
	Type     string `json:"type"`
	ThreadID string `json:"thread_id"`
	Message  string `json:"message"`
	Item     *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Text    string `json:"text"`
	} `json:"item"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Usage *struct {
		CostUSD *float64 `json:"cost_usd"`
	} `json:"usage"`
}
