package main

import (
	"fmt"
	"strings"
)

const modelPolicySchemaV1 = "air-worker.model-policy/v1"

type modelJudgeLane struct {
	Kind      string   `json:"kind,omitempty"`
	Provider  string   `json:"provider,omitempty"`
	Model     string   `json:"model"`
	Efforts   []string `json:"efforts"`
	Fallbacks []string `json:"fallbacks,omitempty"`
}

type modelPolicySpec struct {
	Schema            string                    `json:"schema"`
	ExecutorFallbacks map[string][]string       `json:"executor_fallbacks,omitempty"`
	Judges            map[string]modelJudgeLane `json:"judges,omitempty"`
}

func allowedModelEffort(effort string) bool {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "low", "medium", "high":
		return true
	default:
		return false
	}
}

func legacyExecutorAlias(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "haiku", "luna":
		return "gpt6-luna"
	case "sonnet", "terra", "sol":
		return "gpt6-sol"
	case "opus":
		return "gpt6-astra"
	default:
		return name
	}
}

func policyTierAlias(tier string) string {
	name := tierName(tier)
	alias := legacyExecutorAlias(name)
	if alias == name {
		return tier
	}
	if effort := tierEffort(tier); effort != "" {
		return alias + ":" + effort
	}
	return alias
}

func executorFallbackTiers(cfg runConfig, primaryTier string) []string {
	name := tierName(primaryTier)
	fallbacks := cfg.ModelPolicy.ExecutorFallbacks[name]
	if len(fallbacks) == 0 {
		return nil
	}
	effort := tierEffort(primaryTier)
	out := make([]string, 0, len(fallbacks))
	for _, fallback := range fallbacks {
		fallback = strings.TrimSpace(fallback)
		if fallback == "" {
			continue
		}
		if effort != "" {
			fallback += ":" + effort
		}
		out = append(out, fallback)
	}
	return out
}

func judgeLaneRunner(cfg runConfig, class string) (runnerSpec, error) {
	lane, ok := cfg.ModelPolicy.Judges[class]
	if !ok {
		return runnerSpec{}, fmt.Errorf("judge class %q is not configured", class)
	}
	if strings.TrimSpace(lane.Model) == "" || len(lane.Efforts) == 0 {
		return runnerSpec{}, fmt.Errorf("judge class %q is incomplete", class)
	}
	kind := strings.TrimSpace(lane.Kind)
	if kind == "" {
		kind = "claude"
	}
	return runnerSpec{Kind: kind, Provider: strings.TrimSpace(lane.Provider), Model: lane.Model, Effort: lane.Efforts[0]}, nil
}

func validateModelPolicy(cfg runConfig) error {
	p := cfg.ModelPolicy
	if p.Schema == "" {
		return nil // backward-compatible products without the 0.11.3 policy
	}
	if p.Schema != modelPolicySchemaV1 {
		return fmt.Errorf("unsupported model policy schema %q", p.Schema)
	}
	if len(cfg.Ladder) < 2 || cfg.Ladder[0] != "script" {
		return fmt.Errorf("model policy ladder must start with script")
	}
	for _, tier := range cfg.Ladder[1:] {
		effort := tierEffort(tier)
		if !allowedModelEffort(effort) {
			return fmt.Errorf("tier %q uses forbidden effort %q; allowed: low, medium, high", tier, effort)
		}
		runner := resolveRunner(cfg, tier)
		if runner.Kind != "codex" {
			return fmt.Errorf("executor tier %q must use OpenAI/Codex, got %q", tier, runner.Kind)
		}
	}
	for primary, aliases := range p.ExecutorFallbacks {
		if _, ok := cfg.Runners[primary]; !ok {
			return fmt.Errorf("fallback primary %q has no runner", primary)
		}
		for _, alias := range aliases {
			r, ok := cfg.Runners[alias]
			if !ok {
				return fmt.Errorf("fallback %q for %q has no runner", alias, primary)
			}
			if r.Kind != "codex" {
				return fmt.Errorf("fallback %q must use OpenAI/Codex, got %q", alias, r.Kind)
			}
		}
	}
	for _, class := range []string{"default", "complex", "arbitration"} {
		lane, ok := p.Judges[class]
		if !ok {
			return fmt.Errorf("judge class %q is missing", class)
		}
		if strings.TrimSpace(lane.Model) == "" {
			return fmt.Errorf("judge class %q has no model", class)
		}
		if lane.Kind == "hermes" {
			wantModel := "anthropic/claude-opus-5.5"
			if class == "default" {
				wantModel = "anthropic/claude-sonnet-5.5"
			}
			if lane.Provider != "nous" || lane.Model != wantModel || len(lane.Efforts) != 1 || lane.Efforts[0] != "medium" || len(lane.Fallbacks) != 0 {
				return fmt.Errorf("Hermes judge class %q must use Nous %s with medium effort and no fallbacks", class, wantModel)
			}
			continue
		}
		if lane.Kind != "" && lane.Kind != "claude" {
			return fmt.Errorf("judge class %q has unsupported runner kind %q", class, lane.Kind)
		}
		if lane.Provider != "" {
			return fmt.Errorf("judge class %q declares provider without Hermes runner", class)
		}
		if len(lane.Efforts) == 0 || lane.Efforts[0] != "low" || lane.Efforts[len(lane.Efforts)-1] != "high" {
			return fmt.Errorf("judge class %q must start low and end high", class)
		}
		for _, effort := range lane.Efforts {
			if !allowedModelEffort(effort) {
				return fmt.Errorf("judge class %q uses forbidden effort %q", class, effort)
			}
		}
	}
	return nil
}
