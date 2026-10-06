package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type criterionMeasure struct {
	Kind     string
	Name     string
	Selector string
}

var (
	reCriterionCheck = regexp.MustCompile(`^проверка\s+` + "`([^`]+)`" + `(?:\s*·\s*тест\s+` + "`([^`]+)`" + `)?$`)
	reCriterionFact  = regexp.MustCompile(`^факт\s+` + "`([^`]+)`" + `$`)
)

func parseCriterionMeasures(text string) ([]criterionMeasure, error) {
	var out []criterionMeasure
	for _, part := range strings.Split(text, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if m := reCriterionCheck.FindStringSubmatch(part); m != nil {
			out = append(out, criterionMeasure{Kind: "check", Name: strings.TrimSpace(m[1]), Selector: strings.TrimSpace(m[2])})
			continue
		}
		if m := reCriterionFact.FindStringSubmatch(part); m != nil {
			out = append(out, criterionMeasure{Kind: "fact", Name: strings.TrimSpace(m[1])})
			continue
		}
		return nil, fmt.Errorf("мера должна быть только `проверка <имя> · тест <селектор>` или `факт <id>`: %s", part)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("мера пуста")
	}
	return out, nil
}
func checkByName(cfg runConfig, name string) (checkSpec, bool) {
	for _, c := range cfg.Judge.Checks {
		if strings.TrimSpace(c.Name) == strings.TrimSpace(name) {
			return c, true
		}
	}
	return checkSpec{}, false
}

func selectorArgs(chk checkSpec, selector string) ([]string, error) {
	selector = strings.TrimSpace(selector)
	if chk.Select == "" {
		if selector != "" {
			return nil, fmt.Errorf("проверка %q не объявляет шаблон select", chk.Name)
		}
		return nil, nil
	}
	if selector == "" {
		return nil, fmt.Errorf("проверка %q требует `тест <селектор>`", chk.Name)
	}
	if !strings.Contains(chk.Select, "{}") {
		return nil, fmt.Errorf("проверка %q: select не содержит {}", chk.Name)
	}
	parts := strings.Fields(chk.Select)
	for i := range parts {
		parts[i] = strings.ReplaceAll(parts[i], "{}", selector)
	}
	return parts, nil
}

type measureState int

const (
	measurePass measureState = iota
	measureFail
	// measureGated — критерий ждёт РЕШЕНИЯ ЛПР, а не работы и не измерения. Отдельно от
	// measureUnknown (К40): unknown значит «нечем измерить», gated значит «измерено, ждёт
	// человека». Смешение этих двух состояний в одно раньше читалось как «непонятно, чего
	// не хватает» там, где на деле не хватало только решения ЛПР — и наоборот, объявленный
	// гейт маскировал реальное «нечем измерить» у соседней меры того же критерия.
	measureGated
	measureUnknown
)

type measureResult struct {
	State  measureState
	Detail string
}

// step64 continuation
func checkResultState(r judgeResult, name string) measureResult {
	for _, p := range r.Passed {
		if p == name {
			return measureResult{State: measurePass, Detail: name}
		}
	}
	for _, f := range r.Failed {
		if f == name || strings.HasPrefix(f, name+" (") || strings.HasPrefix(f, name+" —") {
			return measureResult{State: measureFail, Detail: f}
		}
	}
	for _, u := range r.Unknown {
		if u == name || strings.HasPrefix(u, name+" —") || strings.HasPrefix(u, name+" (") {
			return measureResult{State: measureUnknown, Detail: u}
		}
	}
	return measureResult{State: measureUnknown, Detail: fmt.Sprintf("проверка %q не дала машинного результата", name)}
}
func goSelectorExists(root string, chk checkSpec, selector string) measureResult {
	resolved, err := exec.LookPath(chk.Command)
	if err != nil {
		return measureResult{State: measureUnknown, Detail: fmt.Sprintf("команда %q не резолвится", chk.Command)}
	}
	args := append([]string{}, chk.Args...)
	args = append(args, "-list", "^"+regexp.QuoteMeta(selector)+"$")
	c := newChildCommand(resolved, args...)
	c.Dir = root
	if len(chk.Env) > 0 {
		c.Env = os.Environ()
		for k, v := range chk.Env {
			c.Env = append(c.Env, k+"="+v)
		}
		c.Env, c.Err = configuredChildEnvironment(resolved, c.Env)
	}
	out, err := c.CombinedOutput()
	code := exitCode(c, err)
	text := decodeOutput(out)
	if code != 0 {
		return measureResult{State: measureUnknown, Detail: fmt.Sprintf("не удалось перечислить тест %q (код %d): %s", selector, code, lastNonEmpty(text))}
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == selector {
			return measureResult{State: measurePass, Detail: selector}
		}
	}
	return measureResult{State: measureFail, Detail: fmt.Sprintf("тест %q не написан", selector)}
}

var judgeGoListSelectors = goListSelectors

func normalizedCommandBase(command string) string {
	base := filepath.Base(strings.TrimSpace(command))
	ext := filepath.Ext(base)
	if ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.ToLower(base)
}

func isGoCommand(command string) bool {
	return normalizedCommandBase(command) == "go"
}

func goCheckRunsAllTests(chk checkSpec) bool {
	if !isGoCommand(chk.Command) {
		return false
	}
	for i := 0; i < len(chk.Args); i++ {
		arg := strings.ToLower(strings.TrimSpace(chk.Args[i]))
		key := arg
		if eq := strings.IndexByte(key, '='); eq >= 0 {
			key = key[:eq]
		}
		switch key {
		case "-run", "-test.run", "-skip", "-test.skip", "-list", "-test.list",
			"-bench", "-test.bench", "-fuzz", "-test.fuzz", "-args",
			"-short", "-test.short":
			return false
		}
	}
	return true
}

func buildJudgeSelectorInventory(root string, cfg runConfig, g planGoals) map[string]map[string]measureResult {
	needs, _ := criterionSelectorNeeds(cfg, g)
	inventory := map[string]map[string]measureResult{}
	for name, need := range needs {
		chk := need.Check
		if !isGoCommand(chk.Command) || len(need.Selectors) == 0 {
			continue
		}
		results := map[string]measureResult{}
		found, err := judgeGoListSelectors(root, chk, need.Selectors)
		if err != nil {
			for _, selector := range need.Selectors {
				results[selector] = measureResult{State: measureUnknown, Detail: fmt.Sprintf("selector inventory проверки %q недоступен: %v", name, err)}
			}
		} else {
			for _, selector := range need.Selectors {
				if found[selector] {
					results[selector] = measureResult{State: measurePass, Detail: selector}
				} else {
					results[selector] = measureResult{State: measureFail, Detail: fmt.Sprintf("тест %q не написан", selector)}
				}
			}
		}
		inventory[name] = results
	}
	return inventory
}

func runSelectedCheck(root string, chk checkSpec, selector string) measureResult {
	return runSelectedCheckWithInventory(root, chk, selector, judgeResult{})
}

func runSelectedCheckWithInventory(root string, chk checkSpec, selector string, base judgeResult) measureResult {
	extra, err := selectorArgs(chk, selector)
	if err != nil {
		return measureResult{State: measureUnknown, Detail: err.Error()}
	}
	if selector != "" && isGoCommand(chk.Command) {
		if inventory, ok := base.SelectorInventory[chk.Name]; ok {
			exists, known := inventory[selector]
			if !known {
				return measureResult{State: measureUnknown, Detail: fmt.Sprintf("selector %q отсутствует в inventory проверки %q", selector, chk.Name)}
			}
			if exists.State != measurePass {
				return exists
			}
			// Inventory proves existence only. Reuse of the full check's PASS is
			// valid only when that full Go invocation actually covered all tests.
			// Filtered/list/short/fuzz/bench invocations must execute the selected
			// criterion test explicitly instead of inheriting an unrelated PASS.
			full := checkResultState(base, chk.Name)
			if full.State == measurePass && goCheckRunsAllTests(chk) {
				return measureResult{State: measurePass, Detail: fmt.Sprintf("%s · selector %s найден в общем inventory и покрыт полным go test", chk.Name, selector)}
			}
		} else {
			exists := goSelectorExists(root, chk, selector)
			if exists.State != measurePass {
				return exists
			}
		}
	}
	clone := chk
	clone.Args = append(append([]string{}, chk.Args...), extra...)
	var r judgeResult
	if clone.Script != "" {
		var namedIndices []int
		for i, part := range strings.Fields(chk.Select) {
			if powerShellNamedArgument.MatchString(part) {
				namedIndices = append(namedIndices, len(chk.Args)+i)
			}
		}
		runScriptCheck(root, clone, chk.Name, legacyScope(root), &r, namedIndices...)
	} else if clone.Command != "" {
		runCommandCheck(root, clone, chk.Name, legacyScope(root), &r)
	} else {
		return measureResult{State: measureUnknown, Detail: fmt.Sprintf("проверка %q не имеет script/command", chk.Name)}
	}
	return checkResultState(r, chk.Name)
}
func factMeasureState(root string, cfg runConfig, id string) measureResult {
	if cfg.Judge.Checklist == "" {
		return measureResult{State: measureUnknown, Detail: "judge.checklist не объявлен"}
	}
	var cl checklistFile
	path := filepath.Join(root, cfg.Judge.Checklist)
	if err := readJSON(path, &cl); err != nil {
		return measureResult{State: measureUnknown, Detail: fmt.Sprintf("реестр фактов не разобран: %v", err)}
	}
	for _, it := range cl.Items {
		if it.ID != id {
			continue
		}
		switch it.Status {
		case "completed":
			return measureResult{State: measurePass, Detail: id}
		case "gated":
			return measureResult{State: measureGated, Detail: fmt.Sprintf("факт %s ждёт ЛПР: %s", id, it.Awaits)}
		default:
			return measureResult{State: measureFail, Detail: fmt.Sprintf("факт %s имеет статус %q", id, it.Status)}
		}
	}
	return measureResult{State: measureUnknown, Detail: fmt.Sprintf("факта %q в реестре нет", id)}
}

type criterionState struct {
	ID     string
	State  measureState
	Detail string
}

// measureRank — приоритет состояния при объединении нескольких мер одного критерия.
// Выше ранг — то состояние и побеждает: «нечем измерить» перевешивает объявленный гейт
// (недоказанный гейт недоказан), гейт перевешивает провал (ждём ЛПР, а не считаем работой),
// провал перевешивает успех.
func measureRank(s measureState) int {
	switch s {
	case measureUnknown:
		return 3
	case measureGated:
		return 2
	case measureFail:
		return 1
	default:
		return 0
	}
}

func evaluateCriterionObserved(root string, cfg runConfig, c planCriterion, base judgeResult) (criterionState, judgeCriterionObservation) {
	started := time.Now()
	obs := judgeCriterionObservation{
		ID: c.ID, Goal: c.Goal, Sign: c.Sign, Measure: c.Measure,
		Measures: []judgeMeasureObservation{},
	}
	measures, err := parseCriterionMeasures(c.Measure)
	if err != nil {
		cr := criterionState{ID: c.ID, State: measureUnknown, Detail: err.Error()}
		obs.State, obs.Reason = measureStateText(cr.State), cr.Detail
		obs.DurationMS = time.Since(started).Milliseconds()
		return cr, obs
	}
	state := measurePass
	var details []string
	for _, m := range measures {
		measureStarted := time.Now()
		var mr measureResult
		switch m.Kind {
		case "check":
			chk, ok := checkByName(cfg, m.Name)
			if !ok {
				mr = measureResult{State: measureUnknown, Detail: fmt.Sprintf("проверки %q в судье нет", m.Name)}
			} else if m.Selector == "" {
				mr = checkResultState(base, m.Name)
			} else {
				mr = runSelectedCheckWithInventory(root, chk, m.Selector, base)
			}
		case "fact":
			mr = factMeasureState(root, cfg, m.Name)
		default:
			mr = measureResult{State: measureUnknown, Detail: "неизвестный вид меры"}
		}
		obs.Measures = append(obs.Measures, judgeMeasureObservation{
			Kind: m.Kind, Name: m.Name, Selector: m.Selector,
			State: measureStateText(mr.State), Reason: mr.Detail,
			DurationMS: time.Since(measureStarted).Milliseconds(),
		})
		details = append(details, mr.Detail)
		if measureRank(mr.State) > measureRank(state) {
			state = mr.State
		}
	}
	cr := criterionState{ID: c.ID, State: state, Detail: strings.Join(details, "; ")}
	obs.State, obs.Reason = measureStateText(state), cr.Detail
	obs.DurationMS = time.Since(started).Milliseconds()
	return cr, obs
}

func evaluateCriterion(root string, cfg runConfig, c planCriterion, base judgeResult) criterionState {
	cr, _ := evaluateCriterionObserved(root, cfg, c, base)
	return cr
}

// evaluatePlanCriteria — К40: GATED отделён от UNKNOWN. Критерий, чья мера объявлена
// решением ЛПР (факт со статусом gated), попадает в gated, а не в unknown — иначе двум
// разным причинам «не считать работой» — «нечем измерить» и «ждём человека» — отвечало бы
// одно и то же число, и отчёт не смог бы сказать, что именно чинить.
func evaluatePlanCriteriaObserved(root string, cfg runConfig, g planGoals, base judgeResult) (passed, failed, gated, unknown []string, observations []judgeCriterionObservation) {
	for _, c := range g.Criteria {
		cr, obs := evaluateCriterionObserved(root, cfg, c, base)
		observations = append(observations, obs)
		switch cr.State {
		case measurePass:
			passed = append(passed, c.ID)
		case measureFail:
			failed = append(failed, fmt.Sprintf("%s — %s", c.ID, cr.Detail))
		case measureGated:
			gated = append(gated, fmt.Sprintf("%s — %s", c.ID, cr.Detail))
		default:
			unknown = append(unknown, fmt.Sprintf("%s — %s", c.ID, cr.Detail))
		}
	}
	return
}

func evaluatePlanCriteria(root string, cfg runConfig, g planGoals, base judgeResult) (passed, failed, gated, unknown []string) {
	passed, failed, gated, unknown, _ = evaluatePlanCriteriaObserved(root, cfg, g, base)
	return
}

func confirmedClosedSteps(planPath string, passed []string) int {
	ok := map[string]bool{}
	for _, id := range passed {
		ok[id] = true
	}
	n := 0
	for _, s := range readPlanSteps(planPath) {
		if !s.Done {
			continue
		}
		if s.Gate {
			n++
			continue
		}
		refs := stepCriteria(s)
		if len(refs) == 0 {
			n++
			continue
		} // legacy closed step before goal grammar
		confirmed := true
		for _, id := range refs {
			if !ok[id] {
				confirmed = false
				break
			}
		}
		if confirmed {
			n++
		}
	}
	return n
}
