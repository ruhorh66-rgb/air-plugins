package main

import (
	"flag"
	"fmt"
	"path/filepath"
	"strings"
)

func semanticExecutorSpec(name string) (runnerSpec, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "claude", "anthropic":
		return runnerSpec{Kind: "claude"}, nil
	case "codex":
		return runnerSpec{Kind: "codex"}, nil
	case "chatgpt", "gpt":
		return runnerSpec{Kind: "chatgpt"}, nil
	default:
		return runnerSpec{}, fmt.Errorf("unknown executor %q; use claude, codex, or chatgpt", name)
	}
}

func workStepByNum(steps []workStep, num string) (workStep, bool) {
	for _, step := range steps {
		if step.Num == num {
			return step, true
		}
	}
	return workStep{}, false
}

func cmdSemantic(argv []string) int {
	fs := flag.NewFlagSet("semantic", flag.ContinueOnError)
	product := fs.String("product", ".", "корень продукта")
	configPath := fs.String("config", "", "путь к run-config.json")
	stepNum := fs.String("step", "", "номер шага PLAN")
	executorName := fs.String("executor", "claude", "кто исполнил шаг: claude|codex|chatgpt")
	claim := fs.String("claim", "standalone semantic review", "краткое заявление исполнителя")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if strings.TrimSpace(*stepNum) == "" {
		line("ОТКАЗ: semantic review требует -step <номер>")
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
	} else if !filepath.IsAbs(cfgPath) {
		cfgPath = filepath.Join(root, cfgPath)
	}
	var cfg runConfig
	if err := readJSON(cfgPath, &cfg); err != nil {
		line("ОТКАЗ: нет конфигурации " + cfgPath)
		return 2
	}
	req, code, ok := requirePlan(root, cfg, "semantic judge")
	if !ok {
		return code
	}
	step, ok := workStepByNum(req.Steps, strings.TrimSpace(*stepNum))
	if !ok {
		line("ОТКАЗ: шаг " + *stepNum + " не найден в " + req.Path)
		return 2
	}
	if step.Gate {
		line("ОТКАЗ: semantic judge не закрывает гейт ЛПР: " + step.Title)
		return 2
	}
	executor, err := semanticExecutorSpec(*executorName)
	if err != nil {
		line("ОТКАЗ: " + err.Error())
		return 2
	}
	result := runJudge(root, cfg, -1, legacyScope(root))
	globalCode, globalText := verdict(result)
	stepState := stepFactualVerdict(step, &result)
	factual := semanticFactualPacket{
		Scope: "step", Code: stepState.Code, Text: stepState.Text,
		Passed: stepState.Passed, Failed: stepState.Failed, Unknown: stepState.Unknown,
	}
	ctx := &loopCtx{Root: root, Cfg: cfg, CfgPath: cfgPath, goals: req.Goals}
	line(fmt.Sprintf("semantic factual: step %s · code %d · product code %d", step.Num, stepState.Code, globalCode))
	line("  " + stepState.Text)
	if globalCode != 0 {
		line("  product factual remains open: " + globalText)
	}
	run := ctx.semanticJudge(step, executor, factual, *claim)
	if run.Error != "" {
		line("semantic judge: NOT_PROVEN — " + run.Error)
		return 2
	}
	line(fmt.Sprintf("semantic judge: %s · step %s · drift %s · reviewer %s",
		run.Verdict.Verdict, run.Verdict.StepDone, run.Verdict.Drift, run.Reviewer.Kind))
	switch combinedAcceptance(stepState.Code, run.Verdict) {
	case "PASS":
		return 0
	case "DRIFT", "FAIL":
		return 1
	default:
		return 2
	}
}
