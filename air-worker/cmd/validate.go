package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const productValidationSchema = "air-worker.validate/v1"

type productValidation struct {
	Schema          string   `json:"schema"`
	Product         string   `json:"product"`
	Config          string   `json:"config"`
	Plan            string   `json:"plan"`
	Goals           int      `json:"goals"`
	Criteria        int      `json:"criteria"`
	OpenGates       []string `json:"open_gates"`
	JudgeFiles      []string `json:"judge_files"`
	Problems        []string `json:"problems"`
	SelectorBatches int      `json:"selector_batches"`
}

func hasNUL(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	return bytes.IndexByte(b, 0) >= 0, nil
}

func appendUniqueString(dst []string, value string) []string {
	for _, existing := range dst {
		if existing == value {
			return dst
		}
	}
	return append(dst, value)
}

func validationPath(root, raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("empty path")
	}
	path := raw
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Abs(path)
}

func validateReadableFile(label, path string, nul bool, problems *[]string) {
	st, err := os.Stat(path)
	if err != nil {
		*problems = append(*problems, fmt.Sprintf("%s отсутствует: %s", label, path))
		return
	}
	if st.IsDir() {
		*problems = append(*problems, fmt.Sprintf("%s указывает на каталог: %s", label, path))
		return
	}
	if nul {
		has, err := hasNUL(path)
		if err != nil {
			*problems = append(*problems, fmt.Sprintf("%s не прочитан: %s (%v)", label, path, err))
		} else if has {
			*problems = append(*problems, fmt.Sprintf("%s содержит NUL-байт: %s", label, path))
		}
	}
}

func validateJudgeFiles(root, configPath string, cfg runConfig) []string {
	var problems []string
	validateReadableFile("run-config.json", configPath, true, &problems)

	if strings.TrimSpace(cfg.Judge.Checklist) != "" {
		if path, err := validationPath(root, cfg.Judge.Checklist); err != nil {
			problems = append(problems, "judge.checklist: "+err.Error())
		} else {
			validateReadableFile("judge.checklist", path, true, &problems)
		}
	}
	if strings.TrimSpace(cfg.Judge.Path) != "" {
		if path, err := validationPath(root, cfg.Judge.Path); err != nil {
			problems = append(problems, "judge.path: "+err.Error())
		} else {
			validateReadableFile("judge.path", path, true, &problems)
		}
	}

	seenNames := map[string]bool{}
	for i, chk := range cfg.Judge.Checks {
		name := strings.TrimSpace(chk.Name)
		if name == "" {
			problems = append(problems, fmt.Sprintf("judge.checks[%d]: name пуст", i))
			name = fmt.Sprintf("#%d", i)
		} else if seenNames[name] {
			problems = append(problems, fmt.Sprintf("проверка %q объявлена дважды", name))
		}
		seenNames[name] = true

		hasScript := strings.TrimSpace(chk.Script) != ""
		hasCommand := strings.TrimSpace(chk.Command) != ""
		if hasScript == hasCommand {
			problems = append(problems, fmt.Sprintf("проверка %q должна объявлять ровно одно из script/command", name))
			continue
		}
		if hasScript {
			path, err := validationPath(root, chk.Script)
			if err != nil {
				problems = append(problems, fmt.Sprintf("проверка %q script: %v", name, err))
				continue
			}
			if !pathWithinRoot(root, path) {
				problems = append(problems, fmt.Sprintf("проверка %q script вне корня продукта: %s", name, path))
				continue
			}
			validateReadableFile("проверка "+name, path, true, &problems)
		} else {
			resolved, err := exec.LookPath(chk.Command)
			if err != nil {
				problems = append(problems, fmt.Sprintf("проверка %q: команда %q не резолвится", name, chk.Command))
				continue
			}
			if strings.Contains(strings.ToLower(resolved), `\windowsapps\`) {
				if fi, err := os.Lstat(resolved); err == nil && (fi.Size() == 0 || fi.Mode()&os.ModeSymlink != 0) {
					problems = append(problems, fmt.Sprintf("проверка %q: команда %q ведёт на алиас-заглушку магазина", name, chk.Command))
				}
			}
		}

		if chk.Select != "" && !strings.Contains(chk.Select, "{}") {
			problems = append(problems, fmt.Sprintf("проверка %q: select не содержит {}", name))
		}
	}
	return problems
}

type selectorNeed struct {
	Check            checkSpec
	Selectors        []string
	SelectorCriteria map[string][]string
}

func criterionSelectorNeeds(cfg runConfig, g planGoals) (map[string]*selectorNeed, []string) {
	needs := map[string]*selectorNeed{}
	var problems []string
	for _, c := range g.Criteria {
		measures, err := parseCriterionMeasures(c.Measure)
		if err != nil {
			continue // goalProblems reports the grammar error.
		}
		for _, m := range measures {
			if m.Kind != "check" || strings.TrimSpace(m.Selector) == "" {
				continue
			}
			chk, ok := checkByName(cfg, m.Name)
			if !ok {
				continue // criteriaBinding reports missing check name.
			}
			if _, err := selectorArgs(chk, m.Selector); err != nil {
				problems = append(problems, fmt.Sprintf("критерий %s: %v", c.ID, err))
				continue
			}
			need := needs[m.Name]
			if need == nil {
				copyCheck := chk
				need = &selectorNeed{Check: copyCheck, SelectorCriteria: map[string][]string{}}
				needs[m.Name] = need
			}
			need.Selectors = appendUniqueString(need.Selectors, m.Selector)
			need.SelectorCriteria[m.Selector] = appendUniqueString(need.SelectorCriteria[m.Selector], c.ID)
		}
	}
	return needs, problems
}

func goListSelectors(root string, chk checkSpec, selectors []string) (map[string]bool, error) {
	resolved, err := exec.LookPath(chk.Command)
	if err != nil {
		return nil, err
	}
	quoted := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		quoted = append(quoted, regexp.QuoteMeta(selector))
	}
	sort.Strings(quoted)
	pattern := "^(?:" + strings.Join(quoted, "|") + ")$"
	args := append([]string{}, chk.Args...)
	args = append(args, "-list", pattern)
	cmd := newChildCommand(resolved, args...)
	cmd.Dir = root
	if len(chk.Env) > 0 {
		cmd.Env = os.Environ()
		for k, v := range chk.Env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		cmd.Env = configuredChildEnvironment(resolved, cmd.Env)
	}
	out, err := cmd.CombinedOutput()
	code := exitCode(cmd, err)
	if code != 0 {
		return nil, fmt.Errorf("go test -list завершился кодом %d: %s", code, lastNonEmpty(decodeOutput(out)))
	}
	found := map[string]bool{}
	wanted := map[string]bool{}
	for _, selector := range selectors {
		wanted[selector] = true
	}
	for _, line := range strings.Split(strings.ReplaceAll(decodeOutput(out), "\r\n", "\n"), "\n") {
		name := strings.TrimSpace(line)
		if wanted[name] {
			found[name] = true
		}
	}
	return found, nil
}

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[strings.TrimSpace(value)] = true
	}
	return out
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func selectorPromisedByOpenStep(steps []workStep, criterionID, selector string) bool {
	for _, step := range steps {
		if step.Done || step.Gate {
			continue
		}
		if !containsString(stepCriteria(step), criterionID) {
			continue
		}
		haystack := step.Title + " " + step.Judge + " " + step.Cmd
		if strings.Contains(haystack, selector) {
			return true
		}
	}
	return false
}

func selectorPromisedForAllCriteria(steps []workStep, need *selectorNeed, selector string) bool {
	criteria := need.SelectorCriteria[selector]
	if len(criteria) == 0 {
		return false
	}
	for _, criterionID := range criteria {
		if !selectorPromisedByOpenStep(steps, criterionID, selector) {
			return false
		}
	}
	return true
}

func validateSelectorNeeds(root string, cfg runConfig, g planGoals, steps []workStep) (problems []string, batches int) {
	needs, contractProblems := criterionSelectorNeeds(cfg, g)
	problems = append(problems, contractProblems...)
	names := make([]string, 0, len(needs))
	for name := range needs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		need := needs[name]
		if len(need.Selectors) == 0 {
			continue
		}
		sort.Strings(need.Selectors)
		chk := need.Check
		if strings.EqualFold(strings.TrimSuffix(filepath.Base(chk.Command), filepath.Ext(chk.Command)), "go") {
			batches++
			found, err := goListSelectors(root, chk, need.Selectors)
			if err != nil {
				problems = append(problems, fmt.Sprintf("проверка %q: selectors не перечислены: %v", name, err))
				continue
			}
			for _, selector := range need.Selectors {
				if found[selector] {
					continue
				}
				if selectorPromisedForAllCriteria(steps, need, selector) {
					continue
				}
				problems = append(problems, fmt.Sprintf("проверка %q: тест %q не существует и не обещан открытым шагом связанного критерия", name, selector))
			}
			continue
		}

		catalog := stringSet(chk.Selectors)
		var scriptText string
		if strings.TrimSpace(chk.Script) != "" {
			if path, err := validationPath(root, chk.Script); err == nil {
				if b, readErr := os.ReadFile(path); readErr == nil {
					scriptText = string(b)
				}
			}
		}
		for _, selector := range need.Selectors {
			if catalog[selector] {
				continue
			}
			if scriptText != "" && strings.Contains(scriptText, selector) {
				continue
			}
			if selectorPromisedForAllCriteria(steps, need, selector) {
				continue
			}
			problems = append(problems, fmt.Sprintf(
				"проверка %q: selector %q не доказан — добавь его в selectors[]/script либо явно назови в открытом шаге связанного критерия",
				name, selector))
		}
	}
	return problems, batches
}

func validateProduct(root string) (productValidation, int) {
	report := productValidation{
		Schema:     productValidationSchema,
		OpenGates:  []string{},
		JudgeFiles: []string{},
		Problems:   []string{},
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		report.Product = root
		report.Problems = append(report.Problems, "не разобран путь продукта: "+err.Error())
		return report, 2
	}
	report.Product = abs
	if st, err := os.Stat(abs); err != nil || !st.IsDir() {
		report.Problems = append(report.Problems, "нет каталога продукта: "+abs)
		return report, 2
	}

	configPath := filepath.Join(abs, "run-config.json")
	report.Config = configPath
	if has, err := hasNUL(configPath); err == nil && has {
		report.Problems = append(report.Problems, "run-config.json содержит NUL-байт: "+configPath)
		return report, 1
	}
	var cfg runConfig
	if err := readJSON(configPath, &cfg); err != nil {
		report.Problems = append(report.Problems, "нет или не разобран run-config.json: "+err.Error())
		return report, 2
	}

	planPath := nativePlanPath(abs, cfg)
	report.Plan = planPath
	if has, err := hasNUL(planPath); err == nil && has {
		report.Problems = append(report.Problems, "PLAN содержит NUL-байт: "+planPath)
		return report, 1
	}
	steps := readPlanSteps(planPath)
	if len(steps) == 0 {
		report.Problems = append(report.Problems, "план пуст, бит или не найден: "+planPath)
		return report, 2
	}
	for _, step := range steps {
		if step.Gate && !step.Done {
			report.OpenGates = append(report.OpenGates, step.Num)
		}
	}

	g := readPlanGoals(planPath)
	report.Goals, report.Criteria = len(g.Goals), len(g.Criteria)
	report.Problems = append(report.Problems, goalProblems(g, steps)...)
	report.Problems = append(report.Problems, validateReadyTools(planPath, steps)...)
	report.Problems = append(report.Problems, validateJudgeFiles(abs, configPath, cfg)...)
	if len(g.Criteria) > 0 {
		report.Problems = append(report.Problems, criteriaBinding(abs, cfg, g)...)
		selectorProblems, batches := validateSelectorNeeds(abs, cfg, g, steps)
		report.Problems = append(report.Problems, selectorProblems...)
		report.SelectorBatches = batches
	}
	report.JudgeFiles = judgeFiles(abs, cfg, true)

	// Keep deterministic order while preserving the first occurrence of each problem.
	seen := map[string]bool{}
	unique := make([]string, 0, len(report.Problems))
	for _, problem := range report.Problems {
		if strings.TrimSpace(problem) == "" || seen[problem] {
			continue
		}
		seen[problem] = true
		unique = append(unique, problem)
	}
	report.Problems = unique
	if len(report.Problems) > 0 {
		return report, 1
	}
	return report, 0
}

func cmdValidate(argv []string) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	asJSON := fs.Bool("json", false, "машинный вывод")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	report, code := validateProduct(*product)
	if *asJSON {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Print(string(b) + lineEnding)
		return code
	}
	fmt.Printf("product  : %s%s", report.Product, lineEnding)
	fmt.Printf("plan     : %s%s", report.Plan, lineEnding)
	fmt.Printf("goals    : %d, criteria %d, selector batches %d%s", report.Goals, report.Criteria, report.SelectorBatches, lineEnding)
	if code == 0 {
		fmt.Print("validate : PASS — план и измерители доказуемо существуют" + lineEnding)
		return 0
	}
	fmt.Print("validate : FAIL" + lineEnding)
	for _, problem := range report.Problems {
		fmt.Print("  - " + problem + lineEnding)
	}
	return code
}
