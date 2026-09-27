package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const portfolioSchemaVersion = "air-worker.products/v1"
const portfolioReportSchemaVersion = "air-worker.report-all/v2"

type portfolioPlanAdapterSpec struct {
	Kind            string   `json:"kind,omitempty"`
	SectionContains string   `json:"section_contains,omitempty"`
	IDPrefix        string   `json:"id_prefix,omitempty"`
	IDColumn        int      `json:"id_column,omitempty"`
	TitleColumn     int      `json:"title_column,omitempty"`
	StatusColumn    int      `json:"status_column,omitempty"`
	DoneStatuses    []string `json:"done_statuses,omitempty"`
	GateStatuses    []string `json:"gate_statuses,omitempty"`
}

type portfolioMetricSpec struct {
	Kind      string  `json:"kind,omitempty"`
	Path      string  `json:"path,omitempty"`
	Field     string  `json:"field,omitempty"`
	Target    float64 `json:"target,omitempty"`
	Direction string  `json:"direction,omitempty"`
	Scale     float64 `json:"scale,omitempty"`
}

type portfolioProductSpec struct {
	ID           string                   `json:"id"`
	Name         string                   `json:"name"`
	Root         string                   `json:"root"`
	ControlRoot  string                   `json:"control_root,omitempty"`
	Plan         string                   `json:"plan"`
	Config       string                   `json:"config,omitempty"`
	PlanAdapter  portfolioPlanAdapterSpec `json:"plan_adapter,omitempty"`
	Metric       *portfolioMetricSpec     `json:"metric,omitempty"`
	RefreshJudge bool                     `json:"refresh_judge,omitempty"`
}

type portfolioRegistry struct {
	Schema   string                 `json:"schema"`
	Products []portfolioProductSpec `json:"products"`
}

type portfolioRow struct {
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Root                string     `json:"root"`
	ControlRoot         string     `json:"control_root"`
	Plan                string     `json:"plan"`
	Config              string     `json:"config,omitempty"`
	PlanContract        string     `json:"plan_contract"`
	OpenSteps           *int       `json:"open_steps,omitempty"`
	ClosedSteps         *int       `json:"closed_steps,omitempty"`
	OpenGates           *int       `json:"open_gates,omitempty"`
	DistanceToMilestone *int       `json:"distance_to_milestone,omitempty"`
	DistanceToGoal      *int       `json:"distance_to_goal,omitempty"`
	EffectiveDistance   *int       `json:"effective_distance,omitempty"`
	DistanceSource      string     `json:"distance_source,omitempty"`
	ExternalMetricValue *float64   `json:"external_metric_value,omitempty"`
	JudgeRefreshed      bool       `json:"judge_refreshed,omitempty"`
	JudgeCode           *int       `json:"judge_code,omitempty"`
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
		kind := strings.TrimSpace(p.PlanAdapter.Kind)
		if kind != "" && kind != "air-worker" && kind != "status-table" {
			return portfolioRegistry{}, abs, fmt.Errorf("product %s: unsupported plan_adapter.kind %q", p.ID, kind)
		}
		if p.Metric != nil {
			if p.Metric.Kind == "" {
				p.Metric.Kind = "json-number"
			}
			if p.Metric.Kind != "json-number" {
				return portfolioRegistry{}, abs, fmt.Errorf("product %s: unsupported metric.kind %q", p.ID, p.Metric.Kind)
			}
			if strings.TrimSpace(p.Metric.Path) == "" || strings.TrimSpace(p.Metric.Field) == "" {
				return portfolioRegistry{}, abs, fmt.Errorf("product %s: metric path and field are required", p.ID)
			}
			switch p.Metric.Direction {
			case "higher_better", "lower_better":
			default:
				return portfolioRegistry{}, abs, fmt.Errorf("product %s: metric.direction must be higher_better or lower_better", p.ID)
			}
		}
	}
	return registry, abs, nil
}

func planStatusSet(values []string, defaults []string) map[string]bool {
	if len(values) == 0 {
		values = defaults
	}
	out := map[string]bool{}
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = true
	}
	return out
}

func markdownTableColumns(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	raw := strings.Split(strings.Trim(line, "|"), "|")
	cols := make([]string, len(raw))
	for i := range raw {
		cols[i] = strings.TrimSpace(raw[i])
	}
	return cols
}

func markdownSectionText(raw []byte, contains string) (string, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	needle := strings.ToLower(strings.TrimSpace(contains))
	if needle == "" {
		return text, nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") {
			continue
		}
		level := 0
		for level < len(trimmed) && trimmed[level] == '#' {
			level++
		}
		if level == 0 || level == len(trimmed) || trimmed[level] != ' ' {
			continue
		}
		if !strings.Contains(strings.ToLower(strings.TrimSpace(trimmed[level+1:])), needle) {
			continue
		}
		end := len(lines)
		for j := i + 1; j < len(lines); j++ {
			next := strings.TrimSpace(lines[j])
			if !strings.HasPrefix(next, "#") {
				continue
			}
			nextLevel := 0
			for nextLevel < len(next) && next[nextLevel] == '#' {
				nextLevel++
			}
			if nextLevel > 0 && nextLevel < len(next) && next[nextLevel] == ' ' && nextLevel <= level {
				end = j
				break
			}
		}
		return strings.Join(lines[i+1:end], "\n"), nil
	}
	return "", fmt.Errorf("section containing %q not found", contains)
}

func statusTablePlanSteps(path string, adapter portfolioPlanAdapterSpec) ([]workStep, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	scoped, err := markdownSectionText(raw, adapter.SectionContains)
	if err != nil {
		return nil, err
	}
	idCol := adapter.IDColumn
	titleCol := adapter.TitleColumn
	statusCol := adapter.StatusColumn
	if titleCol == 0 {
		titleCol = 2
	}
	if statusCol == 0 {
		statusCol = 3
	}
	maxCol := idCol
	if titleCol > maxCol {
		maxCol = titleCol
	}
	if statusCol > maxCol {
		maxCol = statusCol
	}
	done := planStatusSet(adapter.DoneStatuses, []string{"done", "closed", "completed", "released", "pass"})
	gated := planStatusSet(adapter.GateStatuses, nil)
	var steps []workStep
	for _, line := range strings.Split(scoped, "\n") {
		cols := markdownTableColumns(line)
		if len(cols) <= maxCol {
			continue
		}
		id := strings.TrimSpace(cols[idCol])
		if adapter.IDPrefix != "" && !strings.HasPrefix(id, adapter.IDPrefix) {
			continue
		}
		if id == "" || strings.HasPrefix(id, "---") {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(cols[statusCol]))
		title := strings.TrimSpace(cols[titleCol])
		if title == "" {
			title = id
		}
		step := workStep{
			Index: len(steps) + 1,
			Num:   id,
			Title: id + ". " + title,
			Tier:  "status:" + status,
			Done:  done[status],
			Gate:  gated[status],
		}
		steps = append(steps, step)
	}
	if len(steps) == 0 {
		return nil, errors.New("no status-table rows matched adapter")
	}
	return steps, nil
}

func portfolioPlanSteps(spec portfolioProductSpec, planPath string) ([]workStep, string, error) {
	kind := strings.TrimSpace(spec.PlanAdapter.Kind)
	if kind == "" || kind == "air-worker" {
		steps := readPlanSteps(planPath)
		if len(steps) == 0 {
			return nil, "NOT_PROVEN", errors.New("plan is not air-worker machine-readable")
		}
		return steps, "air-worker-plan/v1", nil
	}
	steps, err := statusTablePlanSteps(planPath, spec.PlanAdapter)
	if err != nil {
		return nil, "NOT_PROVEN", err
	}
	return steps, "status-table/v1", nil
}

func portfolioPlanStats(steps []workStep) (open, closed, gates int, next, nextGate reportStep) {
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
	return
}

func portfolioMilestoneDistanceFromSteps(steps []workStep) *int {
	n := 0
	foundOpen := false
	for _, step := range steps {
		if step.Done {
			continue
		}
		foundOpen = true
		if step.Gate {
			return intPtr(n)
		}
		n++
	}
	if !foundOpen {
		return intPtr(0)
	}
	return intPtr(n)
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

func jsonNumberAtField(value any, field string) (float64, error) {
	cur := value
	for _, part := range strings.Split(field, ".") {
		obj, ok := cur.(map[string]any)
		if !ok {
			return 0, fmt.Errorf("%s: parent is not an object", part)
		}
		next, ok := obj[part]
		if !ok {
			return 0, fmt.Errorf("%s: field not found", part)
		}
		cur = next
	}
	switch n := cur.(type) {
	case float64:
		return n, nil
	case json.Number:
		return n.Float64()
	case string:
		return strconv.ParseFloat(strings.TrimSpace(n), 64)
	default:
		return 0, fmt.Errorf("field %s is %T, not numeric", field, cur)
	}
}

func readPortfolioMetric(spec portfolioMetricSpec, base string) (value float64, distance int, source string, err error) {
	path, err := expandPortfolioPath(spec.Path, base)
	if err != nil {
		return 0, 0, "", err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, "", err
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var payload any
	if err := dec.Decode(&payload); err != nil {
		return 0, 0, "", err
	}
	value, err = jsonNumberAtField(payload, spec.Field)
	if err != nil {
		return 0, 0, "", err
	}
	scale := spec.Scale
	if scale <= 0 {
		scale = 1
	}
	var remaining float64
	switch spec.Direction {
	case "higher_better":
		remaining = math.Max(0, spec.Target-value)
	case "lower_better":
		remaining = math.Max(0, value-spec.Target)
	default:
		return 0, 0, "", fmt.Errorf("unsupported direction %q", spec.Direction)
	}
	distance = int(math.Ceil(remaining*scale - 1e-12))
	source = fmt.Sprintf("metric:%s:%s:target=%g:scale=%g", spec.Field, spec.Direction, spec.Target, scale)
	return value, distance, source, nil
}

func portfolioSamePath(a, b string) bool {
	aa, errA := filepath.Abs(a)
	bb, errB := filepath.Abs(b)
	if errA != nil || errB != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

func refreshPortfolioJudge(root string, cfg runConfig) (int, error) {
	if lockHeld(lockName("loop", root)) {
		return 2, errors.New("product loop is running; judge refresh skipped to avoid racing its verdict")
	}
	planPath := nativePlanPath(root, cfg)
	if st, err := os.Stat(planPath); err != nil || st.IsDir() {
		return 2, fmt.Errorf("judge plan unavailable: %s", planPath)
	}
	res := runJudge(root, cfg, -1, legacyScope(root))
	code, text := verdict(res)
	publishVerdict(root, code, text, res)
	return code, nil
}

func buildPortfolioRow(spec portfolioProductSpec, registryDir string) portfolioRow {
	row := portfolioRow{ID: spec.ID, Name: spec.Name}
	root, rootErr := expandPortfolioPath(spec.Root, registryDir)
	if rootErr != nil {
		row.Root = strings.TrimSpace(spec.Root)
		row.Limits = append(row.Limits, "root_invalid:"+rootErr.Error())
		return row
	}
	row.Root = root
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		row.Limits = append(row.Limits, "root_unavailable")
	}

	controlRoot := root
	if strings.TrimSpace(spec.ControlRoot) != "" {
		if resolved, err := expandPortfolioPath(spec.ControlRoot, registryDir); err == nil {
			controlRoot = resolved
		} else {
			row.ControlRoot = spec.ControlRoot
			row.Limits = append(row.Limits, "control_root_invalid:"+err.Error())
			return row
		}
	}
	row.ControlRoot = controlRoot
	if st, err := os.Stat(controlRoot); err != nil || !st.IsDir() {
		row.Limits = append(row.Limits, "control_root_unavailable")
	}

	plan, planErr := expandPortfolioPath(spec.Plan, controlRoot)
	if planErr != nil {
		row.Plan = strings.TrimSpace(spec.Plan)
		row.Limits = append(row.Limits, "plan_invalid:"+planErr.Error())
		return row
	}
	row.Plan = plan
	if st, err := os.Stat(plan); err != nil || st.IsDir() {
		row.Limits = append(row.Limits, "plan_unavailable")
		return row
	}

	steps, contract, planParseErr := portfolioPlanSteps(spec, plan)
	row.PlanContract = contract
	if planParseErr == nil {
		open, closed, gates, next, nextGate := portfolioPlanStats(steps)
		row.OpenSteps, row.ClosedSteps, row.OpenGates = intPtr(open), intPtr(closed), intPtr(gates)
		row.Next, row.NextGate = next, nextGate
		row.DistanceToMilestone = portfolioMilestoneDistanceFromSteps(steps)
	}

	configPath := filepath.Join(controlRoot, "run-config.json")
	if strings.TrimSpace(spec.Config) != "" {
		if resolved, err := expandPortfolioPath(spec.Config, controlRoot); err == nil {
			configPath = resolved
		} else {
			row.Limits = append(row.Limits, "config_invalid:"+err.Error())
		}
	}
	row.Config = configPath

	var cfg runConfig
	cfgOK := readJSON(configPath, &cfg) == nil
	nativeJudgeEligible := cfgOK && portfolioSamePath(nativePlanPath(controlRoot, cfg), plan)
	if nativeJudgeEligible {
		if spec.RefreshJudge {
			code, err := refreshPortfolioJudge(controlRoot, cfg)
			if err != nil {
				row.Limits = append(row.Limits, "judge_refresh_failed:"+err.Error())
			} else {
				row.JudgeRefreshed = true
				row.JudgeCode = intPtr(code)
			}
		}
		measure, _, _ := measureDrift(controlRoot, "")
		if measure.Distance != nil {
			row.DistanceToGoal = intPtr(*measure.Distance)
			row.EffectiveDistance = intPtr(*measure.Distance)
			row.DistanceSource = "judge:air-worker/v1"
			row.DriftVerdict = measure.Verdict
			if measure.JudgeCode != nil {
				row.JudgeCode = intPtr(*measure.JudgeCode)
			}
		}
	}

	if row.EffectiveDistance == nil && spec.Metric != nil {
		value, distance, source, err := readPortfolioMetric(*spec.Metric, controlRoot)
		if err != nil {
			row.Limits = append(row.Limits, "external_metric_unavailable:"+err.Error())
		} else {
			row.ExternalMetricValue = &value
			row.EffectiveDistance = intPtr(distance)
			row.DistanceSource = source
		}
	}

	if row.EffectiveDistance == nil && row.DistanceToMilestone != nil {
		row.EffectiveDistance = intPtr(*row.DistanceToMilestone)
		row.DistanceSource = "milestone:" + row.PlanContract
	}

	if row.EffectiveDistance == nil {
		switch {
		case planParseErr != nil:
			row.Limits = append(row.Limits, "plan_not_machine_readable")
		case !cfgOK && spec.Metric == nil:
			row.Limits = append(row.Limits, "no_distance_source")
		case cfgOK && !nativeJudgeEligible && spec.Metric == nil:
			row.Limits = append(row.Limits, "native_plan_mismatch_no_fallback")
		}
	}
	return row
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
	fmt.Printf("%-14s | %-9s | %-9s | %-9s | %-18s | %-12s | %s%s",
		"Product", "effective", "milestone", "goal", "source", "next", "limits", lineEnding)
	fmt.Printf("%s%s", strings.Repeat("-", 132), lineEnding)
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
		source := row.DistanceSource
		if source == "" {
			source = "-"
		}
		fmt.Printf("%-14s | %-9s | %-9s | %-9s | %-18s | %-12s | %s%s",
			row.Name, portfolioInt(row.EffectiveDistance), portfolioInt(row.DistanceToMilestone),
			portfolioInt(row.DistanceToGoal), source, next, limits, lineEnding)
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
	return 0
}
