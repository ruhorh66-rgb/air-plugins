package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const modelPolicySchemaV1 = "air-worker.model-policy/v1"

type modelJudgeLane struct {
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
	return runnerSpec{Kind: "claude", Model: lane.Model, Effort: lane.Efforts[0]}, nil
}

// A managed product cannot silently drop its role/subagent contract by
// passing a different -config path to an otherwise native CLI command.
// Other products (without this canonical opt-in) retain their config override.
func enforceCanonicalNativeRouteConfig(root, selected string) error {
	canonical := filepath.Join(root, "run-config.json")
	var pinned runConfig
	if err := readJSON(canonical, &pinned); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("canonical native route policy unreadable: %w", err)
	}
	if !pinned.Orchestration.RequireNativeSubagents && !pinned.Orchestration.RequireAnthropicReviewer {
		return nil
	}
	chosen := strings.TrimSpace(selected)
	if chosen == "" {
		chosen = canonical
	}
	full, err := filepath.Abs(chosen)
	if err != nil {
		return err
	}
	if !strings.EqualFold(filepath.Clean(full), filepath.Clean(canonical)) {
		return fmt.Errorf("managed AirWorker route cannot replace canonical run-config.json with -config; native subagent/reviewer contract is pinned")
	}
	return nil
}

// These guarantees are opt-in per product, not a global rewrite of other
// products' accepted single-executor workflows.
func requireNativeOrchestration(cfg runConfig) error {
	if !cfg.Orchestration.RequireNativeSubagents {
		return nil
	}
	if !cfg.Orchestration.Enabled || cfg.Orchestration.Subagents != 2 {
		return fmt.Errorf("native orchestration contract requires enabled=true and exactly two subagents")
	}
	return nil
}

// A product which mandates native subagents must never launch an ordinary
// single-executor loop under the appearance of orchestrated work. The real
// release-slice guard still runs afterward, unchanged.
func requireNativeLoopInvocation(cfg runConfig, orchestrate bool, requested int) error {
	if !cfg.Orchestration.RequireNativeSubagents {
		return nil
	}
	if requested != 0 && requested != cfg.Orchestration.Subagents {
		return fmt.Errorf("native orchestration requires exactly %d parallel subagents; requested %d", cfg.Orchestration.Subagents, requested)
	}
	if !orchestrate && requested == 0 {
		return fmt.Errorf("native orchestration required: use air-worker orchestrate -subagents %d, not a single-executor loop", cfg.Orchestration.Subagents)
	}
	return nil
}

func enforceIndependentAnthropicReviewer(cfg runConfig, executor, reviewer runnerSpec) error {
	if !cfg.Orchestration.RequireAnthropicReviewer {
		return nil
	}
	if executor.Kind != "codex" {
		return fmt.Errorf("AirWorker reviewer contract requires a Codex executor, got %q", executor.Kind)
	}
	if reviewer.Kind != "claude" || !strings.HasPrefix(strings.ToLower(reviewer.Model), "claude-") {
		return fmt.Errorf("AirWorker reviewer contract requires an independent Anthropic Claude model, got %q/%q", reviewer.Kind, reviewer.Model)
	}
	if !allowedModelEffort(reviewer.Effort) {
		return fmt.Errorf("AirWorker reviewer effort must be low, medium or high, got %q", reviewer.Effort)
	}
	return nil
}

func validateModelPolicy(cfg runConfig) error {
	if err := cfg.SemanticJudge.Validate(); err != nil {
		return err
	}
	if err := requireNativeOrchestration(cfg); err != nil {
		return err
	}
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
