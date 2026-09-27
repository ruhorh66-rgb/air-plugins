package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPortfolioDriftMovementAndSourceBoundary(t *testing.T) {
	history := []portfolioHistoryRecord{
		{Schema: portfolioDriftSchemaVersion, Product: "p", Distance: intPtr(5), Source: "judge:air-worker/v1"},
	}
	row := portfolioRow{ID: "p", Name: "P", EffectiveDistance: intPtr(4), DistanceSource: "judge:air-worker/v1"}
	got := evaluatePortfolioDrift(row, history)
	if got.Movement != "FORWARD" || got.PreviousDistance == nil || *got.PreviousDistance != 5 {
		t.Fatalf("forward=%#v", got)
	}

	row.EffectiveDistance = intPtr(6)
	got = evaluatePortfolioDrift(row, history)
	if got.Movement != "REGRESS" {
		t.Fatalf("regress=%#v", got)
	}

	row.EffectiveDistance = intPtr(5)
	got = evaluatePortfolioDrift(row, history)
	if got.Movement != "STALL" || got.StallMoves != 1 {
		t.Fatalf("stall=%#v", got)
	}

	row.DistanceSource = "milestone:air-worker-plan/v1"
	got = evaluatePortfolioDrift(row, history)
	if got.Movement != "BASELINE_SOURCE_CHANGE" || got.PreviousSource != "judge:air-worker/v1" {
		t.Fatalf("source boundary=%#v", got)
	}
}

func TestPortfolioDriftUnknownIsNotZero(t *testing.T) {
	history := []portfolioHistoryRecord{
		{Schema: portfolioDriftSchemaVersion, Product: "p", Distance: intPtr(0), Source: "judge:air-worker/v1"},
	}
	got := evaluatePortfolioDrift(portfolioRow{ID: "p", Name: "P"}, history)
	if got.Movement != "UNKNOWN" || got.Distance != nil {
		t.Fatalf("unknown was collapsed: %#v", got)
	}
}

func TestBuildPortfolioDriftRecordsHistory(t *testing.T) {
	root := t.TempDir()
	writePortfolioPlan(t, filepath.Join(root, "PLAN.md"))
	registryPath := filepath.Join(root, "products.json")
	if err := os.WriteFile(registryPath, []byte(`{"schema":"air-worker.products/v1","products":[{"id":"p","name":"P","root":".","plan":"PLAN.md"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	historyPath := filepath.Join(root, "history.jsonl")
	first, err := buildPortfolioDrift(registryPath, historyPath, "first", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Rows) != 1 || first.Rows[0].Movement != "BASELINE" {
		t.Fatalf("first=%#v", first)
	}
	second, err := buildPortfolioDrift(registryPath, historyPath, "second", true)
	if err != nil {
		t.Fatal(err)
	}
	if second.Rows[0].Movement != "STALL" || second.Rows[0].StallMoves != 1 {
		t.Fatalf("second=%#v", second.Rows[0])
	}
	rows, err := readPortfolioHistory(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Note != "first" || rows[1].Note != "second" {
		t.Fatalf("history=%#v", rows)
	}
}

func TestPortfolioDriftUsesClosedStepsWhenJudgeDistanceStaysFlat(t *testing.T) {
	history := []portfolioHistoryRecord{{
		Schema: portfolioDriftSchemaVersion, Product: "p",
		Distance: intPtr(0), Source: "judge:air-worker/v1",
		ClosedSteps: intPtr(10), PlanContract: "air-worker-plan/v1",
	}}
	row := portfolioRow{
		ID: "p", Name: "P", EffectiveDistance: intPtr(0), DistanceSource: "judge:air-worker/v1",
		ClosedSteps: intPtr(11), PlanContract: "air-worker-plan/v1",
	}
	got := evaluatePortfolioDrift(row, history)
	if got.Movement != "FORWARD" {
		t.Fatalf("closed-step progress was not movement: %#v", got)
	}

	row.ClosedSteps = intPtr(9)
	got = evaluatePortfolioDrift(row, history)
	if got.Movement != "REGRESS" {
		t.Fatalf("reopened step was not regression: %#v", got)
	}
}
