package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const portfolioSchemaVersion = "air-worker.products/v1"
const portfolioReportSchemaVersion = "air-worker.report-all/v1"

type portfolioProductSpec struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Root string `json:"root"`
	Plan string `json:"plan"`
}

type portfolioRegistry struct {
	Schema   string                 `json:"schema"`
	Products []portfolioProductSpec `json:"products"`
}

type portfolioRow struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Root                string     `json:"root"`
	Plan                string     `json:"plan"`
	PlanContract        string     `json:"plan_contract"`
	OpenSteps           *int       `json:"open_steps,omitempty"`
	ClosedSteps         *int       `json:"closed_steps,omitempty"`
	OpenGates           *int       `json:"open_gates,omitempty"`
	DistanceToMilestone *int       `json:"distance_to_milestone,omitempty"`
	DistanceToGoal      *int       `json:"distance_to_goal,omitempty"`
	Next                reportStep `json:"next"`
	NextGate            reportStep `json:"next_gate"`
	DriftVerdict        string     `json:"drift_verdict,omitempty"`
	Limits              []string   `json:"limits,omitempty"`
}

type portfolioReport struct {
	Schema       string         `json:"schema"`
	Registry     string         `json:"registry"`
	ProductCount int            `json:"product_count"`
	Rows         []portfolioRow `json:"rows"`
}

func defaultPortfolioRegistryPath(explicit string) string {
	if v := strings.TrimSpace(explicit); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("AIR_WORKER_PRODUCTS_FILE")); v != "" {
		return v
	}
	state := filepath.Join(sessionStateDir(), "air-worker-products.json")
	if st, err := os.Stat(state); err == nil && !st.IsDir() {
		return state
	}
	if cwd, err := os.Getwd(); err == nil {
		local := filepath.Join(cwd, ".air-worker", "products.json")
		if st, err := os.Stat(local); err == nil && !st.IsDir() {
			return local
		}
	}
	return state
}

func expandPortfolioPath(raw, base string) (string, error) {
	value := strings.TrimSpace(os.ExpandEnv(raw))
	if value == "" {
		return "", errors.New("empty path")
	}
	if !filepath.IsAbs(value) {
		value = filepath.Join(base, value)
	}
	return filepath.Abs(value)
}

func readPortfolioRegistry(path string) (portfolioRegistry, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return portfolioRegistry{}, "", err
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return portfolioRegistry{}, abs, err
	}
	var registry portfolioRegistry
	if err := json.Unmarshal(raw, &registry); err != nil {
		return portfolioRegistry{}, abs, err
	}
	if registry.Schema != portfolioSchemaVersion {
		return portfolioRegistry{}, abs, fmt.Errorf("registry schema %q, expected %q", registry.Schema, portfolioSchemaVersion)
	}
	if len(registry.Products) == 0 {
		return portfolioRegistry{}, abs, errors.New("registry has no products")
	}
	seen := map[string]bool{}
	for i := range registry.Products {
		p := &registry.Products[i]
		p.ID = strings.TrimSpace(p.ID)
		p.Name = strings.TrimSpace(p.Name)
		if p.ID == "" {
			return portfolioRegistry{}, abs, fmt.Errorf("products[%d].id is required", i)
		}
		if seen[p.ID] {
			return portfolioRegistry{}, abs, fmt.Errorf("duplicate product id %q", p.ID)
		}
		seen[p.ID] = true
		if p.Name == "" {
			p.Name = p.ID
		}
	}
	return registry, abs, nil
}

func portfolioPlanStats(planPath string) (open, closed, gates int, next, nextGate reportStep, ok bool) {
	steps := readPlanSteps(planPath)
	if len(steps) == 0 {
		return 0, 0, 0, reportStep{}, reportStep{}, false
	}
	for _, step := range steps {
		view := reportStep{Num: step.Num, Title: step.Title, Tier: step.Tier, Found: true}
		if step.Done {
			if !step.Gate {
				closed++
			}
			continue
		}
		if step.Gate {
			gates++
			if !nextGate.Found {
				nextGate = view
			}
			continue
		}
		open++
		if !next.Found {
			next = view
		}
	}
	return open, closed, gates, next, nextGate, true
}

func portfolioMilestoneDistance(planPath string) (*int, bool) {
	steps := readPlanSteps(planPath)
	if len(steps) == 0 {
		return nil, false
	}
	n := 0
	foundOpen := false
	for _, step := range steps {
		if step.Done {
			continue
		}
		foundOpen = true
		if step.Gate {
			return &n, true
		}
		n++
	}
	if !foundOpen {
		zero := 0
		return &zero, true
	}
	return &n, true
}

func nativePlanPath(root string, cfg runConfig) string {
	name := strings.TrimSpace(cfg.Plan)
	if name == "" {
		name = "PLAN.md"
	}
	if filepath.IsAbs(name) {
		return filepath.Clean(name)
	}
	return filepath.Clean(filepath.Join(root, name))
}

func buildPortfolioRow(spec portfolioProductSpec, registryDir string) portfolioRow {
	row := portfolioRow{ID: spec.ID, Name: spec.Name, PlanContract: "air-worker-plan/v1"}
	root, rootErr := expandPortfolioPath(spec.Root, registryDir)
	if rootErr != nil {
		row.Root = strings.TrimSpace(spec.Root)
		row.Limits = append(row.Limits, "root_invalid: "+rootErr.Error())
		return row
	}
	row.Root = root
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		row.Limits = append(row.Limits, "root_unavailable")
	}

	planBase := root
	plan, planErr := expandPortfolioPath(spec.Plan, planBase)
	if planErr != nil {
		row.Plan = strings.TrimSpace(spec.Plan)
		row.Limits = append(row.Limits, "plan_invalid: "+planErr.Error())
		return row
	}
	row.Plan = plan
	if st, err := os.Stat(plan); err != nil || st.IsDir() {
		row.Limits = append(row.Limits, "plan_unavailable")
		return row
	}

	open, closed, gates, next, nextGate, parsed := portfolioPlanStats(plan)
	if !parsed {
		row.PlanContract = "NOT_PROVEN"
		row.Limits = append(row.Limits, "plan_not_air-worker-machine-readable")
		return row
	}
	row.OpenSteps, row.ClosedSteps, row.OpenGates = &open, &closed, &gates
	row.Next, row.NextGate = next, nextGate
	if d, ok := portfolioMilestoneDistance(plan); ok {
		row.DistanceToMilestone = d
	}

	var cfg runConfig
	cfgPath := filepath.Join(root, "run-config.json")
	if err := readJSON(cfgPath, &cfg); err != nil {
		row.Limits = append(row.Limits, "goal_distance_unavailable:no_run_config")
		return row
	}
	if !portfolioSamePath(nativePlanPath(root, cfg), plan) {
		row.Limits = append(row.Limits, "goal_distance_unavailable:registry_plan_differs_from_run_config")
		return row
	}
	measure, _, limits := measureDrift(root, "")
	row.DistanceToGoal = measure.Distance
	row.DriftVerdict = measure.Verdict
	row.Limits = append(row.Limits, limits...)
	return row
}

func portfolioSamePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func buildPortfolioReport(registryPath string) (portfolioReport, error) {
	registry, abs, err := readPortfolioRegistry(registryPath)
	if err != nil {
		return portfolioReport{}, err
	}
	base := filepath.Dir(abs)
	rows := make([]portfolioRow, 0, len(registry.Products))
	for _, spec := range registry.Products {
		rows = append(rows, buildPortfolioRow(spec, base))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	return portfolioReport{
		Schema: portfolioReportSchemaVersion, Registry: abs, ProductCount: len(rows), Rows: rows,
	}, nil
}

func portfolioInt(v *int) string {
	if v == nil {
		return "?"
	}
	return fmt.Sprintf("%d", *v)
}

func printPortfolioReport(report portfolioReport) {
	fmt.Printf("AIR portfolio: %d products%s", report.ProductCount, lineEnding)
	fmt.Printf("%-14s | %-9s | %-9s | %-6s | %-12s | %s%s",
		"Product", "milestone", "goal", "gates", "next", "limits", lineEnding)
	fmt.Printf("%s%s", strings.Repeat("-", 100), lineEnding)
	for _, row := range report.Rows {
		next := "-"
		if row.Next.Found {
			next = row.Next.Num
		} else if row.NextGate.Found {
			next = "gate:" + row.NextGate.Num
		}
		limits := strings.Join(row.Limits, ";")
		if limits == "" {
			limits = "-"
		}
		fmt.Printf("%-14s | %-9s | %-9s | %-6s | %-12s | %s%s",
			row.Name, portfolioInt(row.DistanceToMilestone), portfolioInt(row.DistanceToGoal),
			portfolioInt(row.OpenGates), next, limits, lineEnding)
	}
}

func cmdReportAll(registryPath string, asJSON bool) int {
	path := defaultPortfolioRegistryPath(registryPath)
	report, err := buildPortfolioReport(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "report -all: %v; registry=%s%s", err, path, lineEnding)
		return 2
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printPortfolioReport(report)
	}
	// Portfolio report is a measurement, not a readiness verdict. Individual
	// rows carry NOT_PROVEN/limits instead of collapsing the whole ecosystem.
	return 0
}
