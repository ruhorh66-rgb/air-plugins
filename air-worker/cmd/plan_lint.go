package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const planLintSchema = "air-worker.plan-lint/v1"

var (
	reReconVerb    = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(найди|собери|сверь|прочитай|find|collect|compare|read)(?:[^\p{L}]|$)`)
	reMutationVerb = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(создай|добавь|измени|исправь|напиши|реализуй|удали|обнови|create|add|change|edit|fix|write|implement|delete|update)(?:[^\p{L}]|$)`)
	reFileTarget   = regexp.MustCompile(`(?i)(?:[A-Z]:\\|/|\\|\b[\w.-]+\.(?:go|ps1|py|json|md|yaml|yml|toml|sql|exe|dll)\b)`)
)

type planLintWarning struct {
	Step   string `json:"step"`
	Tier   string `json:"tier"`
	Rule   string `json:"rule"`
	Detail string `json:"detail"`
}

type planLintReport struct {
	Schema   string            `json:"schema"`
	Product  string            `json:"product"`
	Plan     string            `json:"plan"`
	Warnings []planLintWarning `json:"warnings"`
}

func criterionHasExistingCheck(cfg runConfig, criterion planCriterion) bool {
	measures, err := parseCriterionMeasures(criterion.Measure)
	if err != nil {
		return false
	}
	for _, measure := range measures {
		if measure.Kind != "check" {
			continue
		}
		if _, ok := checkByName(cfg, measure.Name); ok {
			return true
		}
	}
	return false
}

func planLint(root string) (planLintReport, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return planLintReport{}, err
	}
	var cfg runConfig
	configPath := filepath.Join(abs, "run-config.json")
	if err := readJSON(configPath, &cfg); err != nil {
		return planLintReport{}, fmt.Errorf("run-config: %w", err)
	}
	planPath := nativePlanPath(abs, cfg)
	steps := readPlanSteps(planPath)
	if len(steps) == 0 {
		return planLintReport{}, errors.New("PLAN has no machine-readable steps")
	}
	goals := readPlanGoals(planPath)
	report := planLintReport{
		Schema: planLintSchema, Product: abs, Plan: planPath, Warnings: []planLintWarning{},
	}
	for _, step := range steps {
		if step.Done || step.Gate || tierName(step.Tier) == "script" || strings.TrimSpace(step.Cmd) != "" {
			continue
		}
		title := strings.TrimSpace(step.Title)
		if !reReconVerb.MatchString(title) {
			continue
		}
		if reMutationVerb.MatchString(title) || reFileTarget.MatchString(title) {
			continue
		}
		refs := stepCriteria(step)
		if len(refs) == 0 {
			continue
		}
		measured := false
		for _, ref := range refs {
			if criterion, ok := goals.criterion(ref); ok && criterionHasExistingCheck(cfg, criterion) {
				measured = true
				break
			}
		}
		if !measured {
			continue
		}
		report.Warnings = append(report.Warnings, planLintWarning{
			Step: step.Num, Tier: step.Tier, Rule: "AW-PLAN-LINT-01",
			Detail: "разведочный шаг уже меряется существующей проверкой и не называет mutation/file target; проверь, не должен ли он быть script",
		})
	}
	if ready, err := readReadyTools(planPath); err == nil {
		report.Warnings = append(report.Warnings, homemadeToolingWarnings(steps, ready)...)
	}
	return report, nil
}

func cmdPlanLint(argv []string) int {
	fs := flag.NewFlagSet("plan-lint", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	asJSON := fs.Bool("json", false, "машинный вывод")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	report, err := planLint(*product)
	if err != nil {
		if *asJSON {
			b, _ := json.Marshal(map[string]any{"schema": planLintSchema, "error": err.Error()})
			fmt.Print(string(b) + lineEnding)
		} else {
			fmt.Print("plan-lint: НЕ ПРОВЕРЕНО — " + err.Error() + lineEnding)
		}
		return 2
	}
	if *asJSON {
		b, _ := json.MarshalIndent(report, "", "  ")
		fmt.Print(string(b) + lineEnding)
		return 0
	}
	if len(report.Warnings) == 0 {
		fmt.Print("plan-lint: замечаний нет" + lineEnding)
		return 0
	}
	fmt.Printf("plan-lint: %d предупреждений%s", len(report.Warnings), lineEnding)
	for _, warning := range report.Warnings {
		fmt.Printf("  [%s] шаг %s (%s): %s%s", warning.Rule, warning.Step, warning.Tier, warning.Detail, lineEnding)
	}
	return 0
}

func hookTargetPath(in hookInput) string {
	var params map[string]any
	if len(in.ToolInput) == 0 || json.Unmarshal(in.ToolInput, &params) != nil {
		return ""
	}
	for _, key := range []string{"file_path", "path", "target_path", "output_path", "notebook_path"} {
		value, ok := params[key].(string)
		if !ok || strings.TrimSpace(value) == "" {
			continue
		}
		path := strings.TrimSpace(value)
		if !filepath.IsAbs(path) {
			base := strings.TrimSpace(in.Cwd)
			if base == "" {
				base = "."
			}
			path = filepath.Join(base, path)
		}
		if abs, err := filepath.Abs(path); err == nil {
			return filepath.Clean(abs)
		}
	}
	return ""
}

func handlePostToolUsePlanLint(in hookInput) (hookResult, error) {
	tool := strings.ToLower(strings.TrimSpace(in.ToolName))
	if tool != "write" && tool != "edit" && tool != "notebookedit" {
		return hookResult{}, nil
	}
	product, ok := productForLearningHookInput(in)
	if !ok {
		return hookResult{}, nil
	}
	var cfg runConfig
	if readJSON(filepath.Join(product, "run-config.json"), &cfg) != nil {
		return hookResult{}, nil
	}
	target := hookTargetPath(in)
	if target == "" || !portfolioSamePath(target, nativePlanPath(product, cfg)) {
		return hookResult{}, nil
	}
	report, err := planLint(product)
	if err != nil {
		return hookResult{}, err
	}
	if len(report.Warnings) == 0 {
		return hookResult{}, nil
	}
	var lines []string
	for _, warning := range report.Warnings {
		lines = append(lines, fmt.Sprintf("%s step=%s tier=%s: %s", warning.Rule, warning.Step, warning.Tier, warning.Detail))
	}
	return hookResult{Context: "AIRWORKER PLAN-LINT WARNING (non-blocking):\n" + strings.Join(lines, "\n")}, nil
}

func _planLintReadOnlyGuard(path string) error {
	_, err := os.Stat(path)
	return err
}
