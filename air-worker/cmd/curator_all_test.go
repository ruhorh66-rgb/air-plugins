package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCuratorPortfolioDigestAggregatesPendingAndEffects(t *testing.T) {
	base := t.TempDir()
	p1 := filepath.Join(base, "p1")
	p2 := filepath.Join(base, "p2")
	for _, root := range []string{p1, p2} {
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		writePortfolioPlan(t, filepath.Join(root, "PLAN.md"))
	}

	seedLearnProposal(t, p1, "LP-waiting", "plan-first", "Read the plan before acting.")

	seedLearnProposal(t, p2, "LP-applied", "delivery", "Verify delivery before reporting success.")
	grantLearnProposal(t, p2, "LP-applied")
	if code := cmdLearnApply([]string{"-product", p2, "-id", "LP-applied"}); code != 0 {
		t.Fatalf("apply failed: %d", code)
	}
	proposals, err := readLearnProposals(learnPaths(p2).Proposals)
	if err != nil {
		t.Fatal(err)
	}
	appliedAt, err := time.Parse(time.RFC3339Nano, proposals[0].AppliedAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := appendLearnJSON(learnPaths(p2).Journal, learnJournalRecord{
		Schema: learnSchemaVersion, ID: "LR-repeat",
		CreatedAt: appliedAt.Add(time.Second).Format(time.RFC3339Nano),
		Class:     "delivery", Observed: "repeated", Kind: "lesson", Source: "worker",
	}); err != nil {
		t.Fatal(err)
	}

	registryPath := filepath.Join(base, "products.json")
	registry := portfolioRegistry{
		Schema: portfolioSchemaVersion,
		Products: []portfolioProductSpec{
			{ID: "p1", Name: "Product One", Root: p1, Plan: "PLAN.md"},
			{ID: "p2", Name: "Product Two", Root: p2, Plan: "PLAN.md"},
		},
	}
	raw, _ := json.Marshal(registry)
	if err := os.WriteFile(registryPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, err := buildCuratorPortfolioSnapshot(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Schema != "air-worker.curator-portfolio/v1" || snapshot.Portfolio.ProductCount != 2 {
		t.Fatalf("bad snapshot header: %#v", snapshot)
	}
	if len(snapshot.PendingLPR) != 1 || snapshot.PendingLPR[0].Proposal.ID != "LP-waiting" || snapshot.PendingLPR[0].ProductID != "p1" {
		t.Fatalf("pending aggregation=%#v", snapshot.PendingLPR)
	}
	if len(snapshot.Learning) != 1 {
		t.Fatalf("learning aggregation=%#v", snapshot.Learning)
	}
	effect := snapshot.Learning[0]
	if effect.ProductID != "p2" || effect.Effect.ProposalID != "LP-applied" || effect.Effect.RepeatsAfter != 1 || effect.Effect.Verdict != "не сработало" {
		t.Fatalf("wrong effect=%#v", effect)
	}
}
