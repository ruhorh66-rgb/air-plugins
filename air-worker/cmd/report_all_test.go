package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writePortfolioPlan(t *testing.T, path string) {
	t.Helper()
	plan := "**Ц1.** release\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| ~~1~~ | done | `script` | K1 |\n" +
		"| 2 | build | `terra:medium` | K2 |\n" +
		"| 3 | release approval | гейт | ЛПР |\n" +
		"| 4 | smoke | `script` | K3 |\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeNativeJudgeFixture(t *testing.T, root string, distance int) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writePortfolioPlan(t, filepath.Join(root, "PLAN.md"))
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte("{\"plan\":\"PLAN.md\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mv := machineVerdict{
		At:   time.Now().Format("2006-01-02T15:04:05"),
		Code: 1, Distance: intPtr(distance), VerdictText: "fixture", By: "test",
	}
	raw, _ := json.Marshal(mv)
	if err := os.WriteFile(filepath.Join(root, ".goal-verdict.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReportAllControlRootEnablesExternalControlPlane(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "runtime")
	control := filepath.Join(base, "wiki")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	writeNativeJudgeFixture(t, control, 4)

	registryPath := filepath.Join(base, "products.json")
	registry := portfolioRegistry{
		Schema: portfolioSchemaVersion,
		Products: []portfolioProductSpec{{
			ID: "split", Name: "Split", Root: work, ControlRoot: control, Plan: "PLAN.md",
		}},
	}
	raw, _ := json.Marshal(registry)
	if err := os.WriteFile(registryPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := buildPortfolioReport(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	row := report.Rows[0]
	if row.Root != work || row.ControlRoot != control {
		t.Fatalf("root/control mismatch: %#v", row)
	}
	if row.DistanceToGoal == nil || *row.DistanceToGoal != 4 {
		t.Fatalf("goal distance=%v limits=%v", row.DistanceToGoal, row.Limits)
	}
	if row.EffectiveDistance == nil || *row.EffectiveDistance != 4 || row.DistanceSource != "judge:air-worker/v1" {
		t.Fatalf("effective distance=%v source=%q", row.EffectiveDistance, row.DistanceSource)
	}
}

func TestReportAllFallsBackToMilestoneWithoutJudge(t *testing.T) {
	root := t.TempDir()
	writePortfolioPlan(t, filepath.Join(root, "PLAN.md"))
	registryPath := filepath.Join(root, "products.json")
	raw := []byte(`{"schema":"air-worker.products/v1","products":[{"id":"p","root":".","plan":"PLAN.md"}]}`)
	if err := os.WriteFile(registryPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := buildPortfolioReport(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	row := report.Rows[0]
	if row.DistanceToMilestone == nil || *row.DistanceToMilestone != 1 {
		t.Fatalf("milestone=%v", row.DistanceToMilestone)
	}
	if row.EffectiveDistance == nil || *row.EffectiveDistance != 1 || row.DistanceSource != "milestone:air-worker-plan/v1" {
		t.Fatalf("fallback=%v source=%q limits=%v", row.EffectiveDistance, row.DistanceSource, row.Limits)
	}
}

func TestStatusTableAdapterMeasuresLegacyBacklog(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN-CURRENT.md")
	text := "| ID | Priority | Work | Status | Acceptance |\n" +
		"|---|---|---|---|---|\n" +
		"| DEV-AL-001 | P0 | first | done | pass |\n" +
		"| DEV-AL-002 | P0 | second | implementing | pass |\n" +
		"| DEV-AL-003 | P1 | third | waiting_human | pass |\n" +
		"| BIZ-001 | P0 | ignored | open | pass |\n"
	if err := os.WriteFile(plan, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := portfolioProductSpec{
		ID: "legal", Name: "Legal", Root: root, Plan: "PLAN-CURRENT.md",
		PlanAdapter: portfolioPlanAdapterSpec{
			Kind: "status-table", IDPrefix: "DEV-AL-", TitleColumn: 2, StatusColumn: 3,
			DoneStatuses: []string{"done"}, GateStatuses: []string{"waiting_human"},
		},
	}
	row := buildPortfolioRow(spec, root)
	if row.PlanContract != "status-table/v1" {
		t.Fatalf("contract=%q limits=%v", row.PlanContract, row.Limits)
	}
	if row.OpenSteps == nil || *row.OpenSteps != 1 || row.ClosedSteps == nil || *row.ClosedSteps != 1 || row.OpenGates == nil || *row.OpenGates != 1 {
		t.Fatalf("counts open=%v closed=%v gates=%v", row.OpenSteps, row.ClosedSteps, row.OpenGates)
	}
	if !row.Next.Found || row.Next.Num != "DEV-AL-002" || !row.NextGate.Found || row.NextGate.Num != "DEV-AL-003" {
		t.Fatalf("next=%#v gate=%#v", row.Next, row.NextGate)
	}
	if row.EffectiveDistance == nil || *row.EffectiveDistance != 1 || row.DistanceSource != "milestone:status-table/v1" {
		t.Fatalf("distance=%v source=%q", row.EffectiveDistance, row.DistanceSource)
	}
}

func TestExternalMetricProvidesDeterministicDistance(t *testing.T) {
	root := t.TempDir()
	writePortfolioPlan(t, filepath.Join(root, "PLAN.md"))
	if err := os.WriteFile(filepath.Join(root, "metrics.json"), []byte("{\"overall_score\":52.17}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := portfolioProductSpec{
		ID: "metric", Root: root, Plan: "PLAN.md",
		Metric: &portfolioMetricSpec{
			Kind: "json-number", Path: "metrics.json", Field: "overall_score",
			Target: 100, Direction: "higher_better", Scale: 100,
		},
	}
	row := buildPortfolioRow(spec, root)
	if row.ExternalMetricValue == nil || *row.ExternalMetricValue != 52.17 {
		t.Fatalf("metric value=%v limits=%v", row.ExternalMetricValue, row.Limits)
	}
	if row.EffectiveDistance == nil || *row.EffectiveDistance != 4783 {
		t.Fatalf("distance=%v", row.EffectiveDistance)
	}
	if !strings.HasPrefix(row.DistanceSource, "metric:overall_score:higher_better") {
		t.Fatalf("source=%q", row.DistanceSource)
	}
}

func TestRefreshJudgePublishesFreshMachineVerdict(t *testing.T) {
	root := t.TempDir()
	plan := "**Ц1.** fixture reached\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | fact complete | факт `f01` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| ~~1~~ | done | `script` | К1 |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "goal"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "goal", "checklist.json"), []byte(`{"items":[{"id":"f01","status":"completed"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte(`{"plan":"PLAN.md","judge":{"checklist":"goal/checklist.json","min_facts":1}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	row := buildPortfolioRow(portfolioProductSpec{
		ID: "refresh", Root: root, Plan: "PLAN.md", RefreshJudge: true,
	}, root)
	if !row.JudgeRefreshed {
		t.Fatalf("judge did not refresh: %#v", row)
	}
	if _, err := os.Stat(filepath.Join(root, ".goal-verdict.json")); err != nil {
		t.Fatalf("machine verdict not published: %v", err)
	}
	if row.DistanceToGoal == nil || *row.DistanceToGoal != 0 || row.EffectiveDistance == nil || *row.EffectiveDistance != 0 {
		t.Fatalf("distances goal=%v effective=%v limits=%v", row.DistanceToGoal, row.EffectiveDistance, row.Limits)
	}
}

func TestPortfolioRegistryRejectsDuplicateIDs(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "products.json")
	if err := os.WriteFile(path, []byte(`{"schema":"air-worker.products/v1","products":[
		{"id":"x","root":".","plan":"PLAN.md"},
		{"id":"x","root":".","plan":"PLAN.md"}
	]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readPortfolioRegistry(path); err == nil || !strings.Contains(err.Error(), "duplicate product id") {
		t.Fatalf("duplicate registry ids must fail closed, err=%v", err)
	}
}

func TestJudgeDoesNotRefreshWithoutExplicitRegistryFlag(t *testing.T) {
	root := t.TempDir()
	plan := "**Ц1.** fixture reached\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | fact complete | факт `f01` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | still open | `script` | К1 |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "goal"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(root, "goal", "checklist.json"), []byte(`{"items":[{"id":"f01","status":"completed"}]}`), 0o644)
	_ = os.WriteFile(filepath.Join(root, "run-config.json"), []byte(`{"plan":"PLAN.md","judge":{"checklist":"goal/checklist.json","min_facts":1}}`), 0o644)

	row := buildPortfolioRow(portfolioProductSpec{ID: "p", Root: root, Plan: "PLAN.md"}, root)
	if row.JudgeRefreshed {
		t.Fatal("judge refreshed without explicit refresh_judge")
	}
	if _, err := os.Stat(filepath.Join(root, ".goal-verdict.json")); !os.IsNotExist(err) {
		t.Fatalf("machine verdict created without opt-in: %v", err)
	}
	if row.DistanceToGoal != nil {
		t.Fatalf("goal distance must stay unavailable without existing/fresh verdict: %v", row.DistanceToGoal)
	}
	if row.EffectiveDistance == nil || row.DistanceSource != "milestone:air-worker-plan/v1" {
		t.Fatalf("milestone fallback missing: distance=%v source=%q", row.EffectiveDistance, row.DistanceSource)
	}
}

func TestRefreshJudgeSkipsWhileProductLoopOwnsLock(t *testing.T) {
	root := t.TempDir()
	writeNativeJudgeFixture(t, root, 3)
	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	lock, ok := acquireLock(lockName("loop", root))
	if !ok {
		t.Fatal("could not acquire fixture loop lock")
	}
	defer lock.release()

	if _, err := refreshPortfolioJudge(root, cfg); err == nil || !strings.Contains(err.Error(), "loop is running") {
		t.Fatalf("refresh did not fail closed while loop lock held: %v", err)
	}
}

func TestStatusTableAdapterScopesToNamedSection(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN.md")
	text := "# Old release\n\n" +
		"| Step | Work | Judge | Acceptance | Status |\n|---|---|---|---|---|\n" +
		"| PD0 | stale old work | factual | old | open |\n\n" +
		"### PORTFOLIO-DRIFT-1 — current release\n\n" +
		"| Step | Work | Judge | Acceptance | Status |\n|---|---|---|---|---|\n" +
		"| PD0 | branch | factual | clean | done |\n" +
		"| PD1 | registry | unit | measured | implementing |\n" +
		"| PD2 | release approval | LPR | approved | waiting_lpr |\n\n" +
		"### Next section\n\n" +
		"| Step | Work | Judge | Acceptance | Status |\n|---|---|---|---|---|\n" +
		"| PD9 | must not leak | factual | no | open |\n"
	if err := os.WriteFile(plan, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	row := buildPortfolioRow(portfolioProductSpec{
		ID: "aw", Root: root, Plan: "PLAN.md",
		PlanAdapter: portfolioPlanAdapterSpec{
			Kind: "status-table", SectionContains: "PORTFOLIO-DRIFT-1",
			IDPrefix: "PD", IDColumn: 0, TitleColumn: 1, StatusColumn: 4,
			DoneStatuses: []string{"done"}, GateStatuses: []string{"waiting_lpr"},
		},
	}, root)
	if row.ClosedSteps == nil || *row.ClosedSteps != 1 || row.OpenSteps == nil || *row.OpenSteps != 1 || row.OpenGates == nil || *row.OpenGates != 1 {
		t.Fatalf("scoped counts closed=%v open=%v gates=%v limits=%v", row.ClosedSteps, row.OpenSteps, row.OpenGates, row.Limits)
	}
	if !row.Next.Found || row.Next.Num != "PD1" || !row.NextGate.Found || row.NextGate.Num != "PD2" {
		t.Fatalf("scoped next=%#v gate=%#v", row.Next, row.NextGate)
	}
	if row.EffectiveDistance == nil || *row.EffectiveDistance != 1 || row.DistanceSource != "milestone:status-table/v1" {
		t.Fatalf("scoped distance=%v source=%q", row.EffectiveDistance, row.DistanceSource)
	}
}
