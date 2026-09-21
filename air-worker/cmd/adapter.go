package main

import (
	"encoding/json"
	"flag"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	adapterSchemaVersion = "air-worker.tool/v1"
	adapterReceiptLimit  = 8
)

type adapterProgress struct {
	Closed int `json:"closed"`
	Total  int `json:"total"`
}

type adapterReceipt struct {
	Path       string    `json:"path"`
	JobID      string    `json:"job_id"`
	Status     string    `json:"status"`
	Step       string    `json:"step"`
	Operation  string    `json:"operation"`
	OutputPath string    `json:"output_path"`
	Principal  string    `json:"principal,omitempty"`
	SessionKey string    `json:"session_key,omitempty"`
	StartedAt  time.Time `json:"started_at"`
}

type adapterWorker struct {
	JobID      string `json:"job_id"`
	PID        int    `json:"pid"`
	Step       string `json:"step"`
	Operation  string `json:"operation"`
	Principal  string `json:"principal,omitempty"`
	SessionKey string `json:"session_key,omitempty"`
	Runner     string `json:"runner,omitempty"`
	Provider   string `json:"provider,omitempty"`
	Model      string `json:"model,omitempty"`
	Effort     string `json:"effort,omitempty"`
	Role       string `json:"role,omitempty"`
	Sandbox    string `json:"sandbox,omitempty"`
}

type adapterEnvelope struct {
	SchemaVersion   string           `json:"schema_version"`
	Action          string           `json:"action"`
	Outcome         string           `json:"outcome"`
	ExitCode        int              `json:"exit_code"`
	Progress        adapterProgress  `json:"progress"`
	CurrentStep     string           `json:"current_step"`
	NextAction      string           `json:"next_action"`
	StopReason      string           `json:"stop_reason"`
	Receipts        []adapterReceipt `json:"receipts"`
	ReceiptsOmitted int              `json:"receipts_omitted,omitempty"`
	Workers         []adapterWorker  `json:"workers"`
	DetailPath      string           `json:"detail_path"`
}

func newAdapterEnvelope(action string) adapterEnvelope {
	return adapterEnvelope{
		SchemaVersion: adapterSchemaVersion,
		Action:        action,
		Outcome:       "error",
		ExitCode:      2,
		Receipts:      []adapterReceipt{},
		Workers:       []adapterWorker{},
	}
}

func writeAdapterEnvelope(w io.Writer, result adapterEnvelope) int {
	if err := json.NewEncoder(w).Encode(result); err != nil {
		return 2
	}
	return result.ExitCode
}

func adapterError(action, reason, detailPath string) adapterEnvelope {
	result := newAdapterEnvelope(action)
	result.StopReason = reason
	result.DetailPath = detailPath
	return result
}

func resolveAdapterConfig(root, configPath string) string {
	if strings.TrimSpace(configPath) == "" {
		return filepath.Join(root, "run-config.json")
	}
	if filepath.IsAbs(configPath) {
		return filepath.Clean(configPath)
	}
	return filepath.Join(root, configPath)
}

func resolveAdapterPlan(root, planName string) string {
	if strings.TrimSpace(planName) == "" {
		planName = "PLAN.md"
	}
	if filepath.IsAbs(planName) {
		return filepath.Clean(planName)
	}
	return filepath.Join(root, planName)
}

func canonicalAdapterPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = filepath.Clean(resolved)
	}
	return abs, nil
}

func sameAdapterProduct(root, receiptProduct string) bool {
	if strings.TrimSpace(receiptProduct) == "" {
		return false
	}
	want, err := canonicalAdapterPath(root)
	if err != nil {
		return false
	}
	got, err := canonicalAdapterPath(receiptProduct)
	if err != nil {
		return false
	}
	wantInfo, wantErr := os.Stat(want)
	gotInfo, gotErr := os.Stat(got)
	if wantErr == nil && gotErr == nil && os.SameFile(wantInfo, gotInfo) {
		return true
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(want, got)
	}
	return want == got
}

type adapterReceiptItem struct {
	path string
	r    *jobReceipt
}

func receiptIdentityMatches(r *jobReceipt, principal, sessionKey string) bool {
	return (principal == "" || r.Principal == principal) && (sessionKey == "" || r.Session == sessionKey)
}

func readAdapterReceipts(root, currentStep, principal, sessionKey string) ([]adapterReceipt, []adapterWorker, string, int, bool) {
	paths, err := filepath.Glob(filepath.Join(root, ".woody", "jobs", "*.receipt.json"))
	if err != nil {
		return []adapterReceipt{}, []adapterWorker{}, "", 0, false
	}
	items := make([]adapterReceiptItem, 0, len(paths))
	workers := make([]adapterWorker, 0)
	for _, path := range paths {
		r, err := readJobReceipt(path)
		if err != nil {
			continue
		}
		if !sameAdapterProduct(root, r.Product) || (currentStep != "" && r.Step != currentStep) || !receiptIdentityMatches(r, principal, sessionKey) {
			continue
		}
		items = append(items, adapterReceiptItem{path: path, r: r})
		if r.Status == jobStatusRunning && r.ProcessStarted && r.PID > 0 && adapterProcessMatches(r.PID, r.StartedAt) {
			workers = append(workers, adapterWorker{
				JobID: r.JobID, PID: r.PID, Step: r.Step, Operation: r.Operation,
				Principal: r.Principal, SessionKey: r.Session, Runner: r.Runner,
				Provider: r.Provider, Model: r.Model, Effort: r.Effort,
				Role: r.Role, Sandbox: r.Sandbox,
			})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].r.StartedAt.Equal(items[j].r.StartedAt) {
			return items[i].path < items[j].path
		}
		return items[i].r.StartedAt.After(items[j].r.StartedAt)
	})
	sort.Slice(workers, func(i, j int) bool { return workers[i].JobID < workers[j].JobID })
	hasLiveWorker := len(workers) > 0
	if len(workers) > adapterReceiptLimit {
		workers = workers[:adapterReceiptLimit]
	}

	selected := items
	if len(selected) > adapterReceiptLimit {
		selected = selected[:adapterReceiptLimit]
	}
	receipts := make([]adapterReceipt, 0, len(selected))
	detailPath := ""
	for _, item := range selected {
		r := item.r
		receipts = append(receipts, adapterReceipt{
			Path: item.path, JobID: r.JobID, Status: r.Status, Step: r.Step,
			Operation: r.Operation, OutputPath: r.OutputPath, Principal: r.Principal,
			SessionKey: r.Session, StartedAt: r.StartedAt,
		})
		if detailPath == "" {
			if r.OutputPath != "" {
				detailPath = r.OutputPath
			} else {
				detailPath = item.path
			}
		}
	}
	return receipts, workers, detailPath, len(items) - len(selected), hasLiveWorker
}

func buildAdapterStatus(root, configPath, principal, sessionKey string) adapterEnvelope {
	result := newAdapterEnvelope("status")
	result.DetailPath = root

	fi, err := os.Stat(root)
	if err != nil || !fi.IsDir() {
		result.StopReason = "product_not_found"
		return result
	}

	cfgPath := resolveAdapterConfig(root, configPath)
	var cfg runConfig
	if err := readJSON(cfgPath, &cfg); err != nil {
		result.StopReason = "config_unreadable"
		result.DetailPath = cfgPath
		return result
	}

	planPath := resolveAdapterPlan(root, cfg.Plan)
	if fi, err := os.Stat(planPath); err != nil || fi.IsDir() {
		result.StopReason = "plan_unreadable"
		result.DetailPath = planPath
		return result
	}
	steps := readPlanSteps(planPath)
	if len(steps) == 0 {
		result.StopReason = "plan_empty"
		result.DetailPath = planPath
		return result
	}

	result.ExitCode = 0
	// Pending executable work with no live worker is not a successful stop.
	// Keep it non-terminal so every harness must advance it with `loop` or
	// report a continuity failure instead of presenting an idle campaign as done.
	result.Outcome = "needs_action"
	result.DetailPath = planPath
	var current *workStep
	for i := range steps {
		if steps[i].Done {
			result.Progress.Closed++
			continue
		}
		if current == nil {
			current = &steps[i]
		}
	}
	result.Progress.Total = len(steps)
	currentStep := ""
	if current != nil {
		currentStep = current.Num
	}
	var hasLiveWorker bool
	result.Receipts, result.Workers, result.DetailPath, result.ReceiptsOmitted, hasLiveWorker = readAdapterReceipts(root, currentStep, principal, sessionKey)
	if result.DetailPath == "" {
		result.DetailPath = planPath
	}

	switch {
	case current == nil:
		result.Outcome = "completed"
		result.NextAction = "none"
		result.StopReason = "plan_complete"
	case current.Gate:
		result.Outcome = "waiting"
		result.CurrentStep = current.Num
		result.NextAction = "approve_lpr"
		result.StopReason = "lpr_gate"
	case hasLiveWorker:
		result.Outcome = "running"
		result.CurrentStep = current.Num
		result.NextAction = "wait"
		result.StopReason = ""
	default:
		result.CurrentStep = current.Num
		result.NextAction = "run_loop"
		result.StopReason = "no_live_worker"
	}
	return result
}

func cmdAdapterTo(argv []string, stdout io.Writer) int {
	fs := flag.NewFlagSet("adapter", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	action := fs.String("action", "", "adapter action")
	product := fs.String("product", "", "product root")
	configPath := fs.String("config", "", "path to run-config.json")
	principal := fs.String("principal", "", "receipt principal filter")
	sessionKey := fs.String("session-key", "", "receipt session-key filter")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 {
		return writeAdapterEnvelope(stdout, adapterError(*action, "invalid_arguments", ""))
	}
	if *action != "status" {
		return writeAdapterEnvelope(stdout, adapterError(*action, "unsupported_action", ""))
	}
	if strings.TrimSpace(*product) == "" {
		return writeAdapterEnvelope(stdout, adapterError(*action, "product_required", ""))
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		return writeAdapterEnvelope(stdout, adapterError(*action, "invalid_product", *product))
	}
	return writeAdapterEnvelope(stdout, buildAdapterStatus(root, *configPath, *principal, *sessionKey))
}

func cmdAdapter(argv []string) int {
	return cmdAdapterTo(argv, os.Stdout)
}
