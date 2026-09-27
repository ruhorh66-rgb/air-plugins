package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type curatorPortfolioPending struct {
	Product   string        `json:"product"`
	ProductID string        `json:"product_id"`
	Proposal  learnProposal `json:"proposal"`
}

type curatorPortfolioEffect struct {
	Product   string            `json:"product"`
	ProductID string            `json:"product_id"`
	Effect    curatorEffectView `json:"effect"`
}

type curatorPortfolioSnapshot struct {
	Schema     string                    `json:"schema"`
	Registry   string                    `json:"registry"`
	Portfolio  portfolioReport           `json:"portfolio"`
	PendingLPR []curatorPortfolioPending `json:"pending_lpr,omitempty"`
	Learning   []curatorPortfolioEffect  `json:"learning,omitempty"`
}

func buildCuratorPortfolioSnapshot(registryPath string) (curatorPortfolioSnapshot, error) {
	report, err := buildPortfolioReport(registryPath)
	if err != nil {
		return curatorPortfolioSnapshot{}, err
	}
	out := curatorPortfolioSnapshot{
		Schema:    "air-worker.curator-portfolio/v1",
		Registry:  report.Registry,
		Portfolio: report,
	}
	for _, row := range report.Rows {
		if row.Root == "" {
			continue
		}
		paths := learnPaths(row.Root)
		proposals, err := readLearnProposals(paths.Proposals)
		if err != nil {
			return curatorPortfolioSnapshot{}, fmt.Errorf("%s proposals: %w", row.ID, err)
		}
		for _, proposal := range proposals {
			if proposal.Status == learnPending {
				out.PendingLPR = append(out.PendingLPR, curatorPortfolioPending{
					Product: row.Name, ProductID: row.ID, Proposal: proposal,
				})
			}
		}
		effects, err := curatorLearningEffects(paths, proposals)
		if err != nil {
			return curatorPortfolioSnapshot{}, fmt.Errorf("%s effects: %w", row.ID, err)
		}
		for _, effect := range effects {
			out.Learning = append(out.Learning, curatorPortfolioEffect{
				Product: row.Name, ProductID: row.ID, Effect: effect,
			})
		}
	}
	return out, nil
}

func printCuratorPortfolioDigest(s curatorPortfolioSnapshot) {
	fmt.Println("# AirCurator ecosystem digest")
	fmt.Println()
	fmt.Printf("- registry: %s\n", s.Registry)
	fmt.Printf("- products: %d\n", s.Portfolio.ProductCount)
	fmt.Println()
	fmt.Println("## Вехи")
	fmt.Println()
	fmt.Println("| продукт | до вехи | до цели | гейты | следующий | ограничения |")
	fmt.Println("|---|---:|---:|---:|---|---|")
	for _, row := range s.Portfolio.Rows {
		next := "-"
		if row.Next.Found {
			next = row.Next.Num + " " + row.Next.Title
		} else if row.NextGate.Found {
			next = "гейт " + row.NextGate.Num + " " + row.NextGate.Title
		}
		limits := strings.Join(row.Limits, "; ")
		if limits == "" {
			limits = "-"
		}
		fmt.Printf("| %s | %s | %s | %s | %s | %s |\n",
			row.Name, portfolioInt(row.DistanceToMilestone), portfolioInt(row.DistanceToGoal),
			portfolioInt(row.OpenGates), markdownCell(next), markdownCell(limits))
	}
	fmt.Println()
	fmt.Println("## Ждёт да")
	fmt.Println()
	if len(s.PendingLPR) == 0 {
		fmt.Println("Очередь пуста.")
	} else {
		fmt.Println("| продукт | id | класс | предложение | как ответить |")
		fmt.Println("|---|---|---|---|---|")
		for _, pending := range s.PendingLPR {
			p := pending.Proposal
			fmt.Printf("| %s | %s | %s | %s | да %s |\n",
				markdownCell(pending.Product), p.ID, markdownCell(p.Class), markdownCell(p.Rule), p.ID)
		}
	}
	fmt.Println()
	fmt.Println("## Самообучение")
	fmt.Println()
	if len(s.Learning) == 0 {
		fmt.Println("Применённых правил пока нет.")
	} else {
		fmt.Println("| продукт | proposal | класс | повторов после | вердикт |")
		fmt.Println("|---|---|---|---:|---|")
		for _, item := range s.Learning {
			e := item.Effect
			fmt.Printf("| %s | %s | %s | %d | %s |\n",
				markdownCell(item.Product), e.ProposalID, markdownCell(e.Class), e.RepeatsAfter, e.Verdict)
		}
	}
}

func markdownCell(s string) string {
	s = strings.ReplaceAll(s, "|", "/")
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func cmdCuratorPortfolio(registryPath string, asJSON bool) int {
	path := defaultPortfolioRegistryPath(registryPath)
	snapshot, err := buildCuratorPortfolioSnapshot(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "curator -all: %v; registry=%s%s", err, path, lineEnding)
		return 2
	}
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(snapshot)
		return 0
	}
	printCuratorPortfolioDigest(snapshot)
	return 0
}

// portfolioPlanPath is used by fixture builders that want the canonical path
// without changing a product's run-config.
func portfolioPlanPath(root, plan string) string {
	if filepath.IsAbs(plan) {
		return filepath.Clean(plan)
	}
	return filepath.Join(root, plan)
}
