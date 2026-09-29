package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const executorSchemaV1 = "air-worker.executor/v1"

type executorTierDoc struct {
	Tier   string `json:"tier"`
	Model  string `json:"model"`
	Effort string `json:"effort"`
}

type executorClassDoc struct {
	Class     string `json:"class"`
	StartTier string `json:"start_tier"`
	MaxTier   string `json:"max_tier"`
}

type executorListDoc struct {
	Schema    string                       `json:"schema"`
	Primary   []executorTierDoc            `json:"primary"`
	Fallbacks map[string][]executorTierDoc `json:"fallbacks,omitempty"`
	Classes   []executorClassDoc           `json:"classes"`
}

type executorAttemptDoc struct {
	Tier    string `json:"tier"`
	Model   string `json:"model"`
	Effort  string `json:"effort"`
	OK      bool   `json:"ok"`
	Subtype string `json:"subtype"`
	Detail  string `json:"detail,omitempty"`
	Session string `json:"session_id,omitempty"`
	Receipt string `json:"receipt,omitempty"`
}

type executorDispatchDoc struct {
	Schema        string               `json:"schema"`
	Product       string               `json:"product"`
	Principal     string               `json:"principal"`
	SessionKey    string               `json:"session_key"`
	Class         string               `json:"class,omitempty"`
	RequestedTier string               `json:"requested_tier"`
	SelectedTier  string               `json:"selected_tier"`
	TaskFile      string               `json:"task_file"`
	Attempts      []executorAttemptDoc `json:"attempts"`
	OK            bool                 `json:"ok"`
}

func cmdExecutor(argv []string) int {
	if len(argv) < 1 {
		fmt.Fprint(os.Stderr, "air-worker executor list|dispatch ..."+lineEnding)
		return 2
	}
	switch argv[0] {
	case "list":
		return cmdExecutorList(argv[1:])
	case "dispatch":
		return cmdExecutorDispatch(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown executor action %q"+lineEnding, argv[0])
		return 2
	}
}

func loadExecutorConfig(product, configPath string) (string, runConfig, error) {
	root, err := filepath.Abs(strings.TrimSpace(product))
	if err != nil {
		return "", runConfig{}, err
	}
	if strings.TrimSpace(configPath) == "" {
		configPath = filepath.Join(root, "run-config.json")
	}
	var cfg runConfig
	if err := readJSON(configPath, &cfg); err != nil {
		return "", runConfig{}, fmt.Errorf("read config: %w", err)
	}
	if err := validateModelPolicy(cfg); err != nil {
		return "", runConfig{}, err
	}
	return filepath.Clean(root), cfg, nil
}

func executorTier(cfg runConfig, tier string) (executorTierDoc, error) {
	r := resolveRunner(cfg, tier)
	if r.Kind != "codex" || strings.TrimSpace(r.Model) == "" || !allowedModelEffort(r.Effort) {
		return executorTierDoc{}, fmt.Errorf("tier %q is not an approved Codex executor tier", tier)
	}
	return executorTierDoc{Tier: tier, Model: r.Model, Effort: r.Effort}, nil
}

func fallbackNames(cfg runConfig) map[string]bool {
	out := map[string]bool{}
	for _, names := range cfg.ModelPolicy.ExecutorFallbacks {
		for _, name := range names {
			out[strings.TrimSpace(name)] = true
		}
	}
	return out
}

func approvedExecutorTier(cfg runConfig, tier string) (executorTierDoc, error) {
	for _, primary := range cfg.Ladder[1:] {
		if primary == tier {
			return executorTier(cfg, tier)
		}
	}
	name := tierName(tier)
	if !fallbackNames(cfg)[name] {
		return executorTierDoc{}, fmt.Errorf("tier %q is not registered in executor policy", tier)
	}
	if !allowedModelEffort(tierEffort(tier)) {
		return executorTierDoc{}, fmt.Errorf("tier %q uses forbidden effort; allowed: low, medium, high", tier)
	}
	return executorTier(cfg, tier)
}

func firstPolicyTier(cfg runConfig, alias, effort string) (string, error) {
	want := alias + ":" + effort
	for _, tier := range cfg.Ladder[1:] {
		if tier == want {
			return tier, nil
		}
	}
	return "", fmt.Errorf("required executor tier %q is absent from ladder", want)
}

func executorClasses(cfg runConfig) ([]executorClassDoc, error) {
	specs := []struct {
		Class string
		Alias string
	}{
		{"bulk", "gpt6-luna"},
		{"standard", "gpt6-sol"},
		{"complex", "gpt6-astra"},
		{"hardest", "gpt6-astra"},
	}
	out := make([]executorClassDoc, 0, len(specs))
	for _, spec := range specs {
		start, err := firstPolicyTier(cfg, spec.Alias, "low")
		if err != nil {
			return nil, err
		}
		max, err := firstPolicyTier(cfg, spec.Alias, "high")
		if err != nil {
			return nil, err
		}
		out = append(out, executorClassDoc{Class: spec.Class, StartTier: start, MaxTier: max})
	}
	return out, nil
}

func executorListForConfig(cfg runConfig) (executorListDoc, error) {
	doc := executorListDoc{Schema: executorSchemaV1, Fallbacks: map[string][]executorTierDoc{}}
	for _, tier := range cfg.Ladder[1:] {
		item, err := executorTier(cfg, tier)
		if err != nil {
			return doc, err
		}
		doc.Primary = append(doc.Primary, item)
	}
	for primary, names := range cfg.ModelPolicy.ExecutorFallbacks {
		for _, name := range names {
			for _, effort := range []string{"low", "medium", "high"} {
				item, err := executorTier(cfg, name+":"+effort)
				if err != nil {
					return doc, err
				}
				doc.Fallbacks[primary] = append(doc.Fallbacks[primary], item)
			}
		}
	}
	classes, err := executorClasses(cfg)
	if err != nil {
		return doc, err
	}
	doc.Classes = classes
	return doc, nil
}

func cmdExecutorList(argv []string) int {
	fs := flag.NewFlagSet("executor list", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	configPath := fs.String("config", "", "run-config.json path")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	_, cfg, err := loadExecutorConfig(*product, *configPath)
	if err != nil {
		fmt.Fprint(os.Stderr, "ОТКАЗ: "+err.Error()+lineEnding)
		return 1
	}
	doc, err := executorListForConfig(cfg)
	if err != nil {
		fmt.Fprint(os.Stderr, "ОТКАЗ: "+err.Error()+lineEnding)
		return 1
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(doc)
		return 0
	}
	for _, item := range doc.Primary {
		fmt.Printf("%s -> %s effort=%s"+lineEnding, item.Tier, item.Model, item.Effort)
	}
	return 0
}

func executorScopeForSession(root, principal, sessionKey, stateDir string) (sessionScope, error) {
	id, err := parseIdentity(principal, sessionKey)
	if err != nil {
		return sessionScope{}, err
	}
	dir := resolveStateDir(stateDir)
	if _, err := readSessionOff(dir, id); err == nil {
		return sessionScope{}, fmt.Errorf("session %s is disabled by LPR", id.namespace())
	}
	var mode sessionModeState
	if err := readJSON(sessionModePath(dir, id), &mode); err != nil || !mode.Enabled {
		return sessionScope{}, fmt.Errorf("session %s is not active", id.namespace())
	}
	var prod sessionProductState
	if err := readJSON(sessionProductPath(dir, id), &prod); err != nil {
		return sessionScope{}, fmt.Errorf("session %s has no declared product", id.namespace())
	}
	declared, err := filepath.Abs(prod.Path)
	if err != nil || !strings.EqualFold(filepath.Clean(declared), filepath.Clean(root)) {
		return sessionScope{}, fmt.Errorf("session %s is declared for %q, not %q", id.namespace(), prod.Path, root)
	}
	return newScope(root, id), nil
}

func taskFileInsideProduct(root, taskFile string) (string, error) {
	path, err := filepath.Abs(strings.TrimSpace(taskFile))
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("task file: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("task file is a directory: %s", path)
	}
	rootEval, err := filepath.EvalSymlinks(root)
	if err != nil {
		rootEval = root
	}
	pathEval, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootEval, pathEval)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("task file must be inside product root")
	}
	return filepath.Clean(path), nil
}

func startTierForClass(cfg runConfig, class string) (string, error) {
	items, err := executorClasses(cfg)
	if err != nil {
		return "", err
	}
	for _, item := range items {
		if item.Class == class {
			return item.StartTier, nil
		}
	}
	return "", fmt.Errorf("unknown executor class %q; expected bulk|standard|complex|hardest", class)
}

func runExecutorDispatch(root string, cfg runConfig, scope sessionScope, class, tier, taskFile string) executorDispatchDoc {
	doc := executorDispatchDoc{
		Schema: executorSchemaV1, Product: root, Principal: scope.ID.Principal, SessionKey: scope.ID.SessionKey,
		Class: class, RequestedTier: tier, TaskFile: taskFile,
	}
	ctx := loopCtx{Root: root, Cfg: cfg, Principal: scope.ID.Principal, SessionKey: scope.ID.SessionKey}
	stepID := "executor-" + filepath.Base(taskFile)
	queue := []string{tier}
	queue = append(queue, executorFallbackTiers(cfg, tier)...)
	for _, attemptTier := range queue {
		item, err := approvedExecutorTier(cfg, attemptTier)
		if err != nil {
			doc.Attempts = append(doc.Attempts, executorAttemptDoc{Tier: attemptTier, Subtype: "invalid_runner_config", Detail: err.Error()})
			return doc
		}
		runner := resolveRunner(cfg, attemptTier)
		prompt := "Read this task file first: " + taskFile + "\nExecute exactly that bounded task in the product root. Do not change AirWorker policy, judges, or PLAN unless the task file explicitly requires it."
		result := ctx.invokeCodex("codex", prompt, runner, stepID)
		operation := runnerReceiptOperation("executor-codex", runner, ctx.iter)
		receipt, _ := jobReceiptPaths(scope, stepID, operation)
		doc.Attempts = append(doc.Attempts, executorAttemptDoc{
			Tier: attemptTier, Model: item.Model, Effort: item.Effort, OK: result.Ok,
			Subtype: result.Subtype, Detail: result.Detail, Session: result.Session, Receipt: receipt,
		})
		doc.SelectedTier = attemptTier
		if result.Ok {
			doc.OK = true
			return doc
		}
		if result.Subtype != "vendor_limit" {
			return doc
		}
	}
	return doc
}

func cmdExecutorDispatch(argv []string) int {
	fs := flag.NewFlagSet("executor dispatch", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	configPath := fs.String("config", "", "run-config.json path")
	class := fs.String("class", "standard", "bulk|standard|complex|hardest")
	tier := fs.String("tier", "", "explicit approved tier override")
	taskFile := fs.String("task-file", "", "task file inside product root")
	principal := fs.String("principal", "", "session principal")
	sessionKey := fs.String("session-key", "", "session key")
	stateDir := fs.String("state-dir", "", "session state dir override")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	root, cfg, err := loadExecutorConfig(*product, *configPath)
	if err != nil {
		fmt.Fprint(os.Stderr, "ОТКАЗ: "+err.Error()+lineEnding)
		return 1
	}
	scope, err := executorScopeForSession(root, *principal, *sessionKey, *stateDir)
	if err != nil {
		fmt.Fprint(os.Stderr, "ОТКАЗ: "+err.Error()+lineEnding)
		return 1
	}
	task, err := taskFileInsideProduct(root, *taskFile)
	if err != nil {
		fmt.Fprint(os.Stderr, "ОТКАЗ: "+err.Error()+lineEnding)
		return 1
	}
	selected := strings.TrimSpace(*tier)
	if selected == "" {
		selected, err = startTierForClass(cfg, strings.ToLower(strings.TrimSpace(*class)))
		if err != nil {
			fmt.Fprint(os.Stderr, "ОТКАЗ: "+err.Error()+lineEnding)
			return 1
		}
	}
	if _, err := approvedExecutorTier(cfg, selected); err != nil {
		fmt.Fprint(os.Stderr, "ОТКАЗ: "+err.Error()+lineEnding)
		return 1
	}
	doc := runExecutorDispatch(root, cfg, scope, strings.ToLower(strings.TrimSpace(*class)), selected, task)
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(doc)
	} else {
		for _, a := range doc.Attempts {
			fmt.Printf("%s model=%s effort=%s ok=%t subtype=%s receipt=%s"+lineEnding, a.Tier, a.Model, a.Effort, a.OK, a.Subtype, a.Receipt)
		}
	}
	if doc.OK {
		return 0
	}
	return 1
}
