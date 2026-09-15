package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// БЛОК ЦЕЛЕЙ ПЛАНА — без него air-worker не работает.
//
// Решение ЛПР 14.09.2026, переданное AIR-ENV-002: «без целей план — бумажка, не видно,
// пришли к цели или нет»; «без плана и без целей air-worker не работает», и держаться это
// должно механизмом, а не памятью. Повод живой: ЛПР не принял план AIR-ENV-002 — цель
// лежала отдельно, в goal.json и пакете изменений, в плане была только таблица шагов, и
// механизм это пропустил. У плана самого air-worker было так же: цели этапов жили в
// постановке, а не в плане.
//
// Грамматика:
//
//	**Ц1.** что должно стать правдой
//
//	| Критерий | Цель | Признак достижения | Чем меряется |
//	|---|---|---|---|
//	| К1 | Ц1 | наблюдаемое состояние | проверка `имя проверки судьи` |
//
// «Чем меряется» ссылается на проверку судьи (проверка `имя`) или факт реестра (факт `id`)
// — иначе критерий нечем проверить. Исполняемый шаг таблицы начинает колонку «Судья» со
// ссылок на критерии («К1, К2: …»); у шага списком колонки «Судья» нет, и ссылки пишутся в
// заголовке («… [К2]»).
//
// Кириллица и латиница в метках равноправны (К и K, Ц и C): глазом их не различить, а
// отказ из-за невидимой разницы был бы тем самым молчаливым расхождением, которое механизм
// вычищает.
var (
	reGoalLine     = regexp.MustCompile(`^\s*\*\*[ЦC](\d+)\.\*\*\s*(\S.*)$`)
	reCriterionRow = regexp.MustCompile(`^\s*\|\s*[КK](\d+)\s*\|\s*[ЦC](\d+)\s*\|\s*([^|]*?)\s*\|\s*([^|]*?)\s*\|\s*$`)
	reCriterionRef = regexp.MustCompile(`(?:^|[^\p{L}\p{N}])[КK](\d+)`)
	reMeasureRef   = regexp.MustCompile("(проверка|факт)\\s+`([^`]+)`")
)

type planGoal struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type planCriterion struct {
	ID      string   `json:"id"`
	Goal    string   `json:"goal"`
	Sign    string   `json:"sign"`
	Measure string   `json:"measure"`
	Checks  []string `json:"checks,omitempty"`
	Facts   []string `json:"facts,omitempty"`
}

type planGoals struct {
	Goals    []planGoal      `json:"goals"`
	Criteria []planCriterion `json:"criteria"`
}

// readPlanGoals — блок целей плана. Нет файла — пустой блок: отказ называет вызывающий.
func readPlanGoals(path string) planGoals {
	raw, err := os.ReadFile(path)
	if err != nil {
		return planGoals{}
	}
	var g planGoals
	for _, ln := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if m := reGoalLine.FindStringSubmatch(ln); m != nil {
			g.Goals = append(g.Goals, planGoal{ID: "Ц" + m[1], Text: strings.TrimSpace(m[2])})
			continue
		}
		if m := reCriterionRow.FindStringSubmatch(ln); m != nil {
			c := planCriterion{ID: "К" + m[1], Goal: "Ц" + m[2], Sign: m[3], Measure: m[4]}
			for _, r := range reMeasureRef.FindAllStringSubmatch(c.Measure, -1) {
				if r[1] == "проверка" {
					c.Checks = append(c.Checks, strings.TrimSpace(r[2]))
				} else {
					c.Facts = append(c.Facts, strings.TrimSpace(r[2]))
				}
			}
			g.Criteria = append(g.Criteria, c)
		}
	}
	return g
}

func (g planGoals) criterion(id string) (planCriterion, bool) {
	for _, c := range g.Criteria {
		if c.ID == id {
			return c, true
		}
	}
	return planCriterion{}, false
}

// stepCriteria — на какие критерии ссылается шаг. Табличный шаг — колонкой «Судья», шаг
// списком — заголовком: колонки «Судья» у него нет.
func stepCriteria(s workStep) []string {
	src := s.Judge
	if strings.TrimSpace(src) == "" {
		src = s.Title
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range reCriterionRef.FindAllStringSubmatch(src, -1) {
		id := "К" + m[1]
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// goalProblems — чем план не годится к исполнению; пустой список — годится.
//
// Ссылки на критерии требуются только от ОТКРЫТЫХ исполняемых шагов. Закрытые шаги,
// написанные до блока целей, задним числом не переписываются — так решено в плане самого
// air-worker (этап 0.10); гейт закрывается решением ЛПР, а не работой.
func goalProblems(g planGoals, steps []workStep) []string {
	var p []string
	if len(g.Goals) == 0 {
		p = append(p, "нет ни одной цели — строки вида **Ц1.** <что должно стать правдой>")
	}
	if len(g.Criteria) == 0 {
		p = append(p, "нет ни одного критерия — таблицы | Критерий | Цель | Признак достижения | Чем меряется |")
	}
	goals := map[string]bool{}
	for _, x := range g.Goals {
		if goals[x.ID] {
			p = append(p, "цель "+x.ID+" объявлена дважды")
		}
		goals[x.ID] = true
	}
	crit := map[string]bool{}
	for _, c := range g.Criteria {
		if crit[c.ID] {
			p = append(p, "критерий "+c.ID+" объявлен дважды")
		}
		crit[c.ID] = true
		if len(g.Goals) > 0 && !goals[c.Goal] {
			p = append(p, fmt.Sprintf("критерий %s ссылается на цель %s, которой в плане нет", c.ID, c.Goal))
		}
		if c.Sign == "" || c.Measure == "" {
			p = append(p, fmt.Sprintf("у критерия %s пуст признак достижения или «чем меряется»", c.ID))
		} else if _, err := parseCriterionMeasures(c.Measure); err != nil {
			p = append(p, fmt.Sprintf("критерий %s: %v", c.ID, err))
		}
	}
	var bare []string
	for _, s := range steps {
		if s.Done || s.Gate {
			continue
		}
		refs := stepCriteria(s)
		if len(refs) == 0 {
			bare = append(bare, s.Num)
			continue
		}
		for _, r := range refs {
			if !crit[r] {
				p = append(p, fmt.Sprintf("шаг %s ссылается на критерий %s, которого в плане нет", s.Num, r))
			}
		}
	}
	if len(bare) > 0 {
		p = append(p, "исполняемые шаги без ссылки на критерий — работа, которой цель не требует: "+strings.Join(bare, ", "))
	}
	return p
}

// criteriaBinding — есть ли каждому критерию чем меряться у судьи продукта. Возвращает
// строки «нечем проверить»: критерий без ссылки, ссылку на проверку, которой нет в судье,
// и ссылку на факт, которого нет в реестре.
//
// Опечатка в одной ссылке — тоже отказ, даже если другая ссылка того же критерия жива:
// мёртвая ссылка в плане читается человеком как проверка, которой на деле нет.
func criteriaBinding(root string, cfg runConfig, g planGoals) []string {
	checks := map[string]bool{}
	for _, c := range cfg.Judge.Checks {
		checks[strings.TrimSpace(c.Name)] = true
	}
	facts := map[string]bool{}
	if cfg.Judge.Checklist != "" {
		var cl checklistFile
		if readJSON(filepath.Join(root, cfg.Judge.Checklist), &cl) == nil {
			for _, it := range cl.Items {
				facts[it.ID] = true
			}
		}
	}
	var out []string
	for _, c := range g.Criteria {
		if len(c.Checks) == 0 && len(c.Facts) == 0 {
			out = append(out, fmt.Sprintf("критерий %s не привязан ни к проверке судьи, ни к факту реестра — нечем проверить", c.ID))
			continue
		}
		for _, name := range c.Checks {
			if !checks[name] {
				out = append(out, fmt.Sprintf("критерий %s: проверки «%s» в судье нет", c.ID, name))
			}
		}
		for _, id := range c.Facts {
			if !facts[id] {
				out = append(out, fmt.Sprintf("критерий %s: факта «%s» в реестре нет", c.ID, id))
			}
		}
	}
	return out
}

// judgeFiles — из чего состоит судья продукта: конфигурация (какие проверки идут) и свой
// судья, если назван. Их стережёт страж работы (этап 0.10, К10): судья определяет, что
// значит «готово», и сессия, которая его правит, сама себе ставит оценку. ЛПР 14.09.2026:
// «пир полез в код судьи, зачем нам такое поведение?»
//
// Скрипт проверки в список НЕ входит, и намеренно: это тест продукта, его пишет работа по
// шагам плана. Стеречь его значило бы звать ЛПР на каждый новый тест — ровно те лишние
// вопросы, от которых лечит К9. Ослабленный тест этим не ловится; ловится подключение и
// отключение проверок и подмена судьи — то, что и сделала сессия 14.09.
func judgeFiles(root string, cfg runConfig, cfgLoaded bool) []string {
	out := []string{filepath.Join(root, "run-config.json")}
	if !cfgLoaded {
		return out
	}
	if cfg.Judge.Path != "" {
		out = append(out, filepath.Clean(filepath.Join(root, cfg.Judge.Path)))
	}
	return out
}

// cmdGoals — годен ли план продукта к работе: блок целей, критерии, ссылки шагов, привязка
// критериев к судье. Ничего не запускает и судью не гоняет: зовётся стражем работы на
// каждой правке файла и обязан отвечать за миллисекунды.
//
// Коды: 0 — годен; 1 — план есть, но не годен (причины названы); 2 — продукта или плана нет.
func cmdGoals(argv []string) int {
	fs := flag.NewFlagSet("goals", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	asJSON := fs.Bool("json", false, "машинный вывод")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Print("НЕЧЕМ ПРОВЕРИТЬ: не разобран путь продукта: " + err.Error() + lineEnding)
		return 2
	}
	var cfg runConfig
	cfgErr := readJSON(filepath.Join(root, "run-config.json"), &cfg)
	planName := cfg.Plan
	if planName == "" {
		planName = "PLAN.md"
	}
	planPath := filepath.Join(root, planName)

	res := struct {
		Product    string   `json:"product"`
		Plan       string   `json:"plan"`
		Goals      int      `json:"goals"`
		Criteria   int      `json:"criteria"`
		OpenGates  []string `json:"open_gates"`
		JudgeFiles []string `json:"judge_files"`
		// LoopRunning — по продукту идёт петля. Страж работы пропускает по нему исполнителя
		// петли: отцеплённый `claude -p` в корне продукта получает хуки плагина, а продукт у
		// него не объявлен, — без этого страж остановил бы саму петлю.
		LoopRunning bool     `json:"loop_running"`
		Problems    []string `json:"problems"`
	}{Product: root, Plan: planPath, OpenGates: []string{}, JudgeFiles: judgeFiles(root, cfg, cfgErr == nil),
		LoopRunning: lockHeld(lockName("loop", root)), Problems: []string{}}

	code := 0
	steps := readPlanSteps(planPath)
	// Открытые гейты печатаются всегда: по ним страж хода проверяет, законно ли ход ждёт ЛПР
	// (этап 0.10, К9). Ожидание ЛПР без открытого гейта — вопрос, ответ на который план
	// обязан давать сам.
	for _, s := range steps {
		if s.Gate && !s.Done {
			res.OpenGates = append(res.OpenGates, s.Num)
		}
	}
	switch {
	case cfgErr != nil:
		res.Problems = append(res.Problems, "нет run-config.json — это не продукт air-worker")
		code = 2
	case len(steps) == 0:
		res.Problems = append(res.Problems, "план пуст или не найден: "+planPath)
		code = 2
	default:
		g := readPlanGoals(planPath)
		res.Goals, res.Criteria = len(g.Goals), len(g.Criteria)
		res.Problems = append(res.Problems, goalProblems(g, steps)...)
		if len(g.Criteria) > 0 {
			res.Problems = append(res.Problems, criteriaBinding(root, cfg, g)...)
		}
		if len(res.Problems) > 0 {
			code = 1
		}
	}

	if *asJSON {
		b, _ := json.MarshalIndent(res, "", "  ")
		fmt.Print(string(b) + lineEnding)
		return code
	}
	fmt.Print("план     : " + planPath + lineEnding)
	fmt.Printf("цели     : %d, критериев %d"+lineEnding, res.Goals, res.Criteria)
	if len(res.OpenGates) > 0 {
		fmt.Print("гейты    : открыты " + strings.Join(res.OpenGates, ", ") + lineEnding)
	} else {
		fmt.Print("гейты    : открытых нет" + lineEnding)
	}
	if code == 0 {
		fmt.Print("годен    : да — цели объявлены, шаги ссылаются на критерии, критерии меряются судьёй" + lineEnding)
		return 0
	}
	fmt.Print("годен    : НЕТ" + lineEnding)
	for _, p := range res.Problems {
		fmt.Print("  - " + p + lineEnding)
	}
	return code
}
