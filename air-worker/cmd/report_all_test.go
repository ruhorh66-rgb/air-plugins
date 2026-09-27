package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

func TestReportAllUsesExplicitRootPlanContract(t *testing.T) {
	base := t.TempDir()
	rootNative := filepath.Join(base, "native")
	rootExternal := filepath.Join(base, "external-root")
	if err := os.MkdirAll(rootNative, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(rootExternal, 0o755); err != nil {
		t.Fatal(err)
	}
	nativePlan := filepath.Join(rootNative, "PLAN.md")
	writePortfolioPlan(t, nativePlan)
	if err := os.WriteFile(filepath.Join(rootNative, "run-config.json"), []byte("{\"plan\":\"PLAN.md\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	externalPlan := filepath.Join(base, "wiki", "PLAN-CURRENT.md")
	writePortfolioPlan(t, externalPlan)

	registryPath := filepath.Join(base, "products.json")
	registry := portfolioRegistry{
		Schema: portfolioSchemaVersion,
		Products: []portfolioProductSpec{
			{ID: "native", Name: "Native", Root: rootNative, Plan: "PLAN.md"},
			{ID: "external", Name: "External", Root: rootExternal, Plan: externalPlan},
		},
	}
	raw, _ := json.Marshal(registry)
	if err := os.WriteFile(registryPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := buildPortfolioReport(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Schema != portfolioReportSchemaVersion || report.ProductCount != 2 {
		t.Fatalf("bad report header: %#v", report)
	}
	byID := map[string]portfolioRow{}
	for _, row := range report.Rows {
		byID[row.ID] = row
	}
	for _, id := range []string{"native", "external"} {
		row := byID[id]
		if row.PlanContract != "air-worker-plan/v1" {
			t.Fatalf("%s plan contract=%q limits=%v", id, row.PlanContract, row.Limits)
		}
		if row.OpenSteps == nil || *row.OpenSteps != 2 {
			t.Fatalf("%s open steps=%v", id, row.OpenSteps)
		}
		if row.ClosedSteps == nil || *row.ClosedSteps != 1 {
			t.Fatalf("%s closed steps=%v", id, row.ClosedSteps)
		}
		if row.OpenGates == nil || *row.OpenGates != 1 {
			t.Fatalf("%s gates=%v", id, row.OpenGates)
		}
		if row.DistanceToMilestone == nil || *row.DistanceToMilestone != 1 {
			t.Fatalf("%s milestone distance=%v", id, row.DistanceToMilestone)
		}
		if !row.Next.Found || row.Next.Num != "2" || !row.NextGate.Found || row.NextGate.Num != "3" {
			t.Fatalf("%s next=%#v gate=%#v", id, row.Next, row.NextGate)
		}
	}
	if !strings.Contains(strings.Join(byID["external"].Limits, ";"), "registry_plan_differs_from_run_config") &&
		!strings.Contains(strings.Join(byID["external"].Limits, ";"), "no_run_config") {
		t.Fatalf("external plan must disclose why native goal distance is unavailable: %v", byID["external"].Limits)
	}
}

func TestReportAllMarksNonCanonicalPlanNotProven(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "product")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	plan := filepath.Join(root, "PLAN.md")
	if err := os.WriteFile(plan, []byte("# prose only\nNo machine-readable steps.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(base, "products.json")
	raw := []byte(`{"schema":"air-worker.products/v1","products":[{"id":"p","name":"P","root":"` + strings.ReplaceAll(root, "\\", "\\\\") + `","plan":"PLAN.md"}]}`)
	if err := os.WriteFile(registryPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := buildPortfolioReport(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	row := report.Rows[0]
	if row.PlanContract != "NOT_PROVEN" || !strings.Contains(strings.Join(row.Limits, ";"), "plan_not_air-worker-machine-readable") {
		t.Fatalf("noncanonical plan was presented as measured: %#v", row)
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
