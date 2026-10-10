package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func loadReleaseModelPolicy(t *testing.T) runConfig {
	t.Helper()
	var cfg runConfig
	if err := readJSON(filepath.Join("..", "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestReleaseModelPolicy0113(t *testing.T) {
	cfg := loadReleaseModelPolicy(t)
	if err := validateModelPolicy(cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"script",
		"gpt6-luna:low", "gpt6-luna:medium", "gpt6-luna:high",
		"gpt6-sol:low", "gpt6-sol:medium", "gpt6-sol:high",
		"gpt6-astra:low", "gpt6-astra:medium", "gpt6-astra:high",
	}
	if !reflect.DeepEqual(cfg.Ladder, want) {
		t.Fatalf("ladder=%v want=%v", cfg.Ladder, want)
	}
	for _, tier := range cfg.Ladder {
		if strings.Contains(tier, ":max") || strings.Contains(tier, ":ultra") || strings.Contains(tier, ":xhigh") {
			t.Fatalf("forbidden effort in release ladder: %s", tier)
		}
	}
	models := map[string]string{
		"gpt55":       "gpt-5.5",
		"gpt56-luna":  "gpt-5.6-luna",
		"gpt56-terra": "gpt-5.6-terra",
		"gpt56-sol":   "gpt-5.6-sol",
		"gpt6-luna":   "gpt-6-luna",
		"gpt6-sol":    "gpt-6-sol",
		"gpt6-astra":  "gpt-6-astra",
	}
	for alias, model := range models {
		r := cfg.Runners[alias]
		if r.Kind != "codex" || r.Model != model {
			t.Fatalf("%s runner=%+v", alias, r)
		}
	}
	if got := executorFallbackTiers(cfg, "gpt6-sol:medium"); !reflect.DeepEqual(got, []string{"gpt56-terra:medium", "gpt56-sol:medium", "gpt55:medium"}) {
		t.Fatalf("sol fallbacks=%v", got)
	}
}

func TestReleaseJudgePolicy0113(t *testing.T) {
	cfg := loadReleaseModelPolicy(t)
	want := map[string]string{
		"default":     "anthropic/claude-sonnet-5.5",
		"complex":     "anthropic/claude-opus-5.5",
		"arbitration": "anthropic/claude-opus-5.5",
	}
	for class, model := range want {
		lane := cfg.ModelPolicy.Judges[class]
		if lane.Model != model || lane.Kind != "hermes" || lane.Provider != "nous" || !reflect.DeepEqual(lane.Efforts, []string{"medium"}) || len(lane.Fallbacks) != 0 {
			t.Fatalf("%s lane=%+v", class, lane)
		}
	}
}

func TestReleaseJudgeRoutesSimpleToNousSonnetAndComplexToNousOpus(t *testing.T) {
	cfg := loadReleaseModelPolicy(t)
	want := map[string]runnerSpec{
		"default":     {Kind: "hermes", Provider: "nous", Model: "anthropic/claude-sonnet-5.5", Effort: "medium"},
		"complex":     {Kind: "hermes", Provider: "nous", Model: "anthropic/claude-opus-5.5", Effort: "medium"},
		"arbitration": {Kind: "hermes", Provider: "nous", Model: "anthropic/claude-opus-5.5", Effort: "medium"},
	}
	for class, expected := range want {
		got, err := judgeLaneRunner(cfg, class)
		if err != nil || got.Kind != expected.Kind || got.Model != expected.Model || got.Effort != expected.Effort {
			t.Fatalf("judge lane %q = %+v, %v; want %+v", class, got, err, expected)
		}
	}
}

func TestSemanticJudgeClassRoutesAstraStepsToComplexLane(t *testing.T) {
	cases := []struct {
		name     string
		step     workStep
		executor runnerSpec
		want     string
	}{
		{name: "ordinary tier", step: workStep{Tier: "gpt6-luna:low"}, executor: runnerSpec{Kind: "codex", Model: "gpt-6-luna"}, want: "default"},
		{name: "declared complex tier", step: workStep{Tier: "gpt6-astra:high"}, executor: runnerSpec{Kind: "codex", Model: "gpt-6-astra"}, want: "complex"},
		{name: "complex execution after fallback", step: workStep{Tier: "gpt6-sol:medium"}, executor: runnerSpec{Kind: "codex", Model: "gpt-6-astra"}, want: "complex"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := semanticJudgeClass(tc.step, tc.executor); got != tc.want {
				t.Fatalf("semanticJudgeClass()=%q want %q", got, tc.want)
			}
		})
	}
}

func TestLegacyExecutorTierAliasesMapToOpenAI(t *testing.T) {
	cfg := loadReleaseModelPolicy(t)
	cases := map[string]string{
		"haiku":         "gpt6-luna:low",
		"sonnet":        "gpt6-sol:low",
		"sonnet:medium": "gpt6-sol:medium",
		"opus:high":     "gpt6-astra:high",
	}
	for legacy, want := range cases {
		m := resolveLadderTier(cfg.Ladder, legacy)
		if !m.Found || cfg.Ladder[m.Index] != want {
			t.Fatalf("%s -> match=%+v tier=%q want=%q", legacy, m, func() string {
				if m.Index >= 0 && m.Index < len(cfg.Ladder) {
					return cfg.Ladder[m.Index]
				}
				return ""
			}(), want)
		}
	}
	if resolveLadderTier(cfg.Ladder, "sonnet:max").Found {
		t.Fatal("legacy max must remain forbidden")
	}
}
