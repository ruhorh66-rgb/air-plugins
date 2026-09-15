package main

import (
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
	args := semanticCodexArgs(`C:\product`, "prompt", runnerSpec{Kind: "codex", Effort: "high"}, `C:\tmp\schema.json`)
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

func TestCombinedAcceptanceMatrix(t *testing.T) {
	pass := semanticVerdict{Verdict: "PASS", StepDone: "yes", Drift: "none"}
	drift := semanticVerdict{Verdict: "DRIFT", StepDone: "partial", Drift: "major"}
	np := semanticVerdict{Verdict: "NOT_PROVEN", StepDone: "partial", Drift: "none"}
	if got := combinedAcceptance(1, pass); got != "FAIL" {
		t.Fatalf("factual fail overridden: %s", got)
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

func TestIntermediateReleaseProfileAnthropicExecutorCodexJudge(t *testing.T) {
	var cfg runConfig
	if err := readJSON(filepath.Join("..", "run-config.json"), &cfg); err != nil {
		t.Fatalf("read release run-config: %v", err)
	}
	for _, tier := range cfg.Ladder {
		r := resolveRunner(cfg, tier)
		if r.Kind == "script" {
			continue
		}
		if r.Kind != "claude" {
			t.Fatalf("intermediate release has non-Anthropic executor at %s: %+v", tier, r)
		}
		reviewer, err := oppositeSemanticReviewer(r)
		if err != nil || reviewer.Kind != "codex" {
			t.Fatalf("Anthropic executor at %s is not judged by Codex: %+v, %v", tier, reviewer, err)
		}
	}
}

// The inverse Codex->Claude path remains for the later cross-vendor release,
// but the current release profile above proves it is not active.
