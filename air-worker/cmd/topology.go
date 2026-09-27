package main

import "strings"

const (
	roleOrchestrator = "ChatGPT"
	roleEngine       = appName
	rolePlanner      = appName
	roleGuard        = appName
	roleFactualJudge = appName
)

type reportTopology struct {
	Orchestrator    string `json:"orchestrator"`
	Executor        string `json:"executor"`
	FactualJudge    string `json:"factual_judge"`
	SemanticJudge   string `json:"semantic_judge"`
	Engine          string `json:"engine"`
	Planner         string `json:"planner"`
	Guard           string `json:"guard"`
	CurrentTier     string `json:"current_tier,omitempty"`
	CurrentRunner   string `json:"current_runner,omitempty"`
	CurrentProvider string `json:"current_provider,omitempty"`
	CurrentModel    string `json:"current_model,omitempty"`
	CurrentEffort   string `json:"current_effort,omitempty"`
}

func buildReportTopology(cfg runConfig, next reportStep) reportTopology {
	out := reportTopology{
		Orchestrator: roleOrchestrator,
		Executor:     "none", FactualJudge: roleFactualJudge, SemanticJudge: "none",
		Engine: roleEngine, Planner: rolePlanner, Guard: roleGuard,
	}
	if !next.Found {
		return out
	}
	out.CurrentTier = strings.TrimSpace(next.Tier)
	if tierName(next.Tier) == "script" {
		out.Executor = "script"
		out.CurrentRunner = "script"
		return out
	}
	rung := next.Tier
	if match := resolveLadderTier(cfg.Ladder, next.Tier); match.Found {
		rung = cfg.Ladder[match.Index]
	}
	runner := resolveRunner(cfg, rung)
	out.Executor = runner.Kind
	out.CurrentRunner = runner.Kind
	out.CurrentProvider = runnerProvider(runner.Kind)
	out.CurrentModel = runner.Model
	out.CurrentEffort = runner.Effort
	if reviewer, err := oppositeSemanticReviewer(runner); err == nil {
		out.SemanticJudge = reviewer.Kind
	} else {
		out.SemanticJudge = "NOT_PROVEN"
	}
	return out
}
