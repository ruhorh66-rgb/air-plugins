package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSemanticVerdictStrict(t *testing.T) {
	good := `{"verdict":"PASS","step_done":"yes","drift":"none","evidence":["tests pass"],"next":"close step"}`
	v, err := parseSemanticVerdict(good)
	if err != nil {
		t.Fatalf("valid verdict rejected: %v", err)
	}
	if v.Verdict != "PASS" || v.StepDone != "yes" || v.Drift != "none" {
		t.Fatalf("unexpected verdict: %+v", v)
	}

	bad := []string{
		"```json\n" + good + "\n```",
		good + " trailing",
		`{"verdict":"PASS","step_done":"yes","drift":"none","evidence":[],"next":"x","extra":1}`,
		`{"verdict":"PASS","step_done":"partial","drift":"none","evidence":[],"next":"x"}`,
		`{"verdict":"DRIFT","step_done":"no","drift":"none","evidence":[],"next":"x"}`,
	}
	for _, raw := range bad {
		if _, err := parseSemanticVerdict(raw); err == nil {
			t.Fatalf("invalid contract accepted: %q", raw)
		}
	}
}
func TestSemanticReviewerOppositeVendor(t *testing.T) {
	r, err := oppositeSemanticReviewer(runnerSpec{Kind: "claude"})
	if err != nil || r.Kind != "codex" {
		t.Fatalf("claude reviewer = %+v, %v", r, err)
	}
	r, err = oppositeSemanticReviewer(runnerSpec{Kind: "codex"})
	if err != nil || r.Kind != "claude" {
		t.Fatalf("codex reviewer = %+v, %v", r, err)
	}
	if _, err := oppositeSemanticReviewer(runnerSpec{Kind: "script"}); err == nil {
		t.Fatal("script must not get semantic model reviewer")
	}
}

func TestSemanticCodexIsReadOnly(t *testing.T) {
	args := semanticCodexArgs(`C:\product`, runnerSpec{Kind: "codex", Effort: "high"}, `C:\tmp\schema.json`)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-s read-only") {
		t.Fatalf("codex semantic sandbox is not read-only: %v", args)
	}
	if strings.Contains(joined, "workspace-write") {
		t.Fatalf("semantic reviewer received write sandbox: %v", args)
	}
	if !strings.Contains(joined, `--output-schema C:\tmp\schema.json`) {
		t.Fatalf("codex semantic output schema missing: %v", args)
	}
}

func TestCriterion56SemanticCodexUsesStdin(t *testing.T) {
	prompt := strings.Repeat("semantic-packet-", 4096)
	cmd := semanticCodexCommand("codex.cmd", `C:\product`, prompt, runnerSpec{Kind: "codex", Effort: "high"}, `C:\tmp\schema.json`)
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "semantic-packet-") {
		t.Fatalf("semantic packet leaked into argv: %d chars", len(joined))
	}
	if !strings.HasSuffix(joined, " -") {
		t.Fatalf("codex stdin sentinel missing: %v", cmd.Args)
	}
	raw, err := io.ReadAll(cmd.Stdin)
	if err != nil || string(raw) != prompt {
		t.Fatalf("semantic stdin mismatch: len=%d err=%v", len(raw), err)
	}
}

func TestSemanticClaudeUsesStdin(t *testing.T) {
	prompt := strings.Repeat("semantic-packet-", 4096)
	cmd := semanticClaudeCommand("claude.cmd", `C:\product`, prompt, runnerSpec{Kind: "claude", Model: "sonnet"})
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "semantic-packet-") {
		t.Fatalf("semantic packet leaked into argv: %d chars", len(joined))
	}
	raw, err := io.ReadAll(cmd.Stdin)
	if err != nil || string(raw) != prompt {
		t.Fatalf("semantic stdin mismatch: len=%d err=%v", len(raw), err)
	}
}

func TestCombinedAcceptanceMatrix(t *testing.T) {
	pass := semanticVerdict{Verdict: "PASS", StepDone: "yes", Drift: "none"}
	drift := semanticVerdict{Verdict: "DRIFT", StepDone: "partial", Drift: "major"}
	np := semanticVerdict{Verdict: "NOT_PROVEN", StepDone: "partial", Drift: "none"}
	if got := combinedAcceptance(1, pass); got != "FAIL" {
		t.Fatalf("factual fail overridden: %s", got)
	}
	if got := combinedAcceptance(2, pass); got != "NOT_PROVEN" {
		t.Fatalf("factual unknown became failure/pass: %s", got)
	}
	if got := combinedAcceptance(0, pass); got != "PASS" {
		t.Fatalf("pass matrix: %s", got)
	}
	if got := combinedAcceptance(0, drift); got != "DRIFT" {
		t.Fatalf("drift matrix: %s", got)
	}
	if got := combinedAcceptance(0, np); got != "NOT_PROVEN" {
		t.Fatalf("not-proven matrix: %s", got)
	}
}
func TestPublishSemanticWritesLatestAndHistory(t *testing.T) {
	root := t.TempDir()
	v := semanticVerdict{Verdict: "PASS", StepDone: "yes", Drift: "none", Next: "done"}
	rec := semanticRecord{At: "now", Subject: "step", Step: "65", ReviewerVendor: "codex", Verdict: &v}
	publishSemantic(root, rec)
	publishSemantic(root, rec)

	latest := filepath.Join(root, ".woody", "semantic-verdict.json")
	if _, err := os.Stat(latest); err != nil {
		t.Fatalf("latest verdict missing: %v", err)
	}
	history := filepath.Join(root, ".woody", "semantic-history.jsonl")
	raw, err := os.ReadFile(history)
	if err != nil {
		t.Fatalf("history missing: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("history is not append-only: %d lines", len(lines))
	}
}

func TestRouterFirstReleaseProfileKeepsCodexJudge(t *testing.T) {
	var cfg runConfig
	if err := readJSON(filepath.Join("..", "run-config.json"), &cfg); err != nil {
		t.Fatalf("read release run-config: %v", err)
	}
	if len(cfg.Ladder) < 2 || cfg.Ladder[0] != "script" || cfg.Ladder[1] != "haiku:medium" {
		t.Fatalf("release ladder must start script -> haiku:medium: %v", cfg.Ladder)
	}
	haiku := resolveRunner(cfg, cfg.Ladder[1])
	if haiku.Kind != "router" {
		t.Fatalf("haiku tier must route through AirLLMRouter: %+v", haiku)
	}
	for _, tier := range cfg.Ladder {
		r := resolveRunner(cfg, tier)
		if r.Kind == "script" {
			continue
		}
		if r.Kind != "router" && r.Kind != "claude" && r.Kind != "codex" && r.Kind != "openai" {
			t.Fatalf("release has unsupported executor at %s: %+v", tier, r)
		}
		reviewer, err := oppositeSemanticReviewer(r)
		wantReviewer := "codex"
		if r.Kind == "codex" || r.Kind == "openai" {
			wantReviewer = "claude"
		}
		if err != nil || reviewer.Kind != wantReviewer {
			t.Fatalf("executor at %s is not judged by the opposite provider: %+v, %v", tier, reviewer, err)
		}
	}
}

// The inverse Codex->Claude path remains available for a future executor profile,
// but the 0.10.5 resilience release keeps Codex out of the executor ladder.
