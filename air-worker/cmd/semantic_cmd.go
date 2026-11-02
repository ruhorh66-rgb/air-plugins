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
	case "router":
		return runnerSpec{Kind: "router"}, nil
	default:
		return runnerSpec{}, fmt.Errorf("unknown executor %q; use claude, codex, chatgpt, or router", name)
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
	nodeID := fs.String("node", "", "exact native plan/N-ID (distinct from legacy -step)")
	reviewScope := fs.String("scope", "", "native-node review mode: source-only; never runs factual QA")
	sourceCommit := fs.String("source-commit", "", "exact current 40-hex Git commit")
	artifact := fs.String("artifact", "", "one tracked UTF-8 source artifact relative to product")
	artifactSHA := fs.String("artifact-sha256", "", "exact SHA256 of tracked source artifact")
	expectedReviewer := fs.String("reviewer", "", "explicit expected Anthropic model (no model switching)")
	expectedEffort := fs.String("effort", "", "explicit expected reviewer effort")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if strings.HasPrefix(strings.TrimSpace(*stepNum), "N-") {
		line("ОТКАЗ до модели: -step разрешает только legacy numeric PLAN step. Для штатного plan/N-ID используй -node <exact N-ID> -scope source-only; этот режим не исполняет QA.")
		return 2
	}
	if strings.TrimSpace(*nodeID) != "" {
		if strings.TrimSpace(*stepNum) != "" || *reviewScope != "source-only" {
			line("ОТКАЗ до модели: native node требует -node <exact N-ID> -scope source-only БЕЗ -step; QA не запускается")
			return 2
		}
		root, err := normalizePlanNodeProduct(*product)
		if err != nil {
			line("ОТКАЗ до модели: " + err.Error())
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
			line("ОТКАЗ до модели: нет run-config: " + err.Error())
			return 2
		}
		return cmdSemanticNativeNode(root, cfg, semanticNodeSourceRequest{
			NodeID: strings.TrimSpace(*nodeID), SourceCommit: strings.TrimSpace(*sourceCommit),
			ArtifactPath: *artifact, ArtifactSHA256: strings.TrimSpace(*artifactSHA),
			Executor:         strings.TrimSpace(*executorName),
			ExpectedReviewer: strings.TrimSpace(*expectedReviewer),
			ExpectedEffort:   strings.TrimSpace(*expectedEffort),
		})
	}
	if strings.TrimSpace(*stepNum) == "" || *reviewScope != "" || *sourceCommit != "" || *artifact != "" ||
		*artifactSHA != "" || *expectedReviewer != "" || *expectedEffort != "" {
		line("ОТКАЗ до модели: -step <legacy numeric PLAN step> запускает configured factual QA; -node -scope source-only обязателен для native static review. Нельзя смешивать аргументы.")
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
	line("semantic legacy -step: FACTUAL QA WILL_EXECUTE before the model; for native source-only review use -node -scope source-only")
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
