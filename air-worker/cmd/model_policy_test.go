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
		"gpt61-terra:low", "gpt61-terra:medium", "gpt61-terra:high",
		"gpt6-sol:low", "gpt6-sol:medium", "gpt6-sol:high",
		"gpt61-sol:low", "gpt61-sol:medium", "gpt61-sol:high",
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
		"gpt61-terra": "gpt-6.1-terra",
		"gpt61-sol":   "gpt-6.1-sol",
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
	if got := executorFallbackTiers(cfg, "gpt61-sol:medium"); !reflect.DeepEqual(got, []string{"gpt6-sol:medium", "gpt56-sol:medium", "gpt55:medium"}) {
		t.Fatalf("gpt61-sol fallbacks=%v", got)
	}
}

func TestReleaseJudgePolicy0113(t *testing.T) {
	cfg := loadReleaseModelPolicy(t)
	want := map[string]string{
		"triage":      "claude-haiku-5-5",
		"default":     "claude-sonnet-5-5",
		"complex":     "claude-opus-5-5",
		"arbitration": "claude-fable-5-1",
	}
	for class, model := range want {
		lane := cfg.ModelPolicy.Judges[class]
		if lane.Model != model || !reflect.DeepEqual(lane.Efforts, []string{"low", "medium", "high"}) {
			t.Fatalf("%s lane=%+v", class, lane)
		}
	}
	for _, lane := range cfg.ModelPolicy.Judges {
		for _, effort := range lane.Efforts {
			if effort == "max" || effort == "ultra" || effort == "xhigh" {
				t.Fatalf("forbidden judge effort %q", effort)
			}
		}
	}
	if got := cfg.ModelPolicy.Judges["default"].Fallbacks; !reflect.DeepEqual(got, []string{"claude-sonnet-5", "claude-sonnet-4-6"}) {
		t.Fatalf("default judge fallbacks=%v", got)
	}
}

// N135 retains the accepted Codex coding / independent Anthropic boundary.
// LPR 07.11.2026 additionally requires exactly two native Codex patch-author
// subagents plus one shared-tree writing leader, not an optional analysis mode.
func TestN135RestoreAcceptedCodingLadderAndAnthropicJudges(t *testing.T) {
	cfg := loadReleaseModelPolicy(t)
	if !cfg.Orchestration.Enabled || cfg.Orchestration.Subagents != 2 ||
		!cfg.Orchestration.RequireNativeSubagents || !cfg.Orchestration.RequireAnthropicReviewer {
		t.Fatalf("two Codex coding subagents and independent Anthropic must be mandatory: %+v", cfg.Orchestration)
	}
	if err := validateModelPolicy(cfg); err != nil {
		t.Fatal(err)
	}
	classes := map[string]string{
		"bulk":     "gpt6-luna:low",
		"standard": "gpt61-terra:low",
		"complex":  "gpt61-sol:low",
		"hardest":  "gpt6-astra:low",
	}
	for class, want := range classes {
		got, err := startTierForClass(cfg, class)
		if err != nil || got != want {
			t.Fatalf("class %s: got %q want %q err=%v", class, got, want, err)
		}
		runner := resolveRunner(cfg, got)
		if runner.Kind != "codex" || runner.Model == "" {
			t.Fatalf("coding tier %s has no native Codex writer: %+v", got, runner)
		}
	}
	for _, item := range []struct {
		executor runnerSpec
		model    string
	}{
		{runnerSpec{Kind: "codex", Model: "gpt-6-sol"}, "claude-sonnet-5-5"},
		{runnerSpec{Kind: "codex", Model: "gpt-6-astra"}, "claude-opus-5-5"},
	} {
		reviewer, override, err := cfg.SemanticJudge.Resolve(item.executor, "")
		if err != nil || override || reviewer.Model != item.model {
			t.Fatalf("no independent default judge for %q: %+v override=%v err=%v", item.executor.Model, reviewer, override, err)
		}
		if err := enforceIndependentAnthropicReviewer(cfg, item.executor, reviewer); err != nil {
			t.Fatal(err)
		}
	}
	if err := requireNativeLoopInvocation(cfg, false, 0); err != nil {
		t.Fatalf("native default must use canonical two-subagent orchestration: %v", err)
	}
	if err := requireNativeLoopInvocation(cfg, false, 1); err == nil {
		t.Fatal("one subagent bypassed exact two-subagent gate")
	}
	broken := cfg
	broken.Orchestration.Subagents = 1
	if err := validateModelPolicy(broken); err == nil {
		t.Fatal("native product accepted a single Codex subagent")
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
