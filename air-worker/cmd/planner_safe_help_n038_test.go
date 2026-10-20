package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n038ValidPlannerProduct(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"plan", "goal"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg := runConfig{
		Ladder: []string{"script", "sol:low"},
		Runners: map[string]runnerSpec{
			"sol": {Kind: "codex", Model: "gpt-6-sol", Effort: "low"},
		},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string][]byte{
		"run-config.json":   raw,
		"goal/goal.json":    []byte(`{"goal":"An isolated valid product fixture","objective":"goal/objective.md"}`),
		"goal/objective.md": []byte("# Test objective\n\n## Acceptance\nThe source stays unchanged.\n"),
		"PLAN.md":           []byte("# Existing canonical plan\n\n<!-- air-worker-plan-nodes -->\n"),
	} {
		if err := os.WriteFile(filepath.Join(root, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestN038ActualParserDoesNotDispatchPaidModelOrReviewForSyntax(t *testing.T) {
	root := n038ValidPlannerProduct(t)
	before, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	original := dispatchPlannerModel
	modelCalls := 0
	dispatchPlannerModel = func(_ string, _ string, _ runnerSpec, _ string, _ sessionScope) int {
		modelCalls++
		return 77 // Fail safely. Do not invoke any provider even if routing regresses.
	}
	defer func() {
		dispatchPlannerModel = original
		planSyntaxOnly.Store(false)
	}()
	tests := []struct {
		name         string
		argv         []string
		wantRC       int
		syntaxOnly   bool
		captureClass string
	}{
		{"unknown help", []string{"plan", "release", "-help"}, 2, true, ""},
		{"unknown verb", []string{"plan", "release"}, 2, true, ""},
		{"unknown nested node", []string{"plan", "node", "release"}, 2, true, ""},
		{"missing node verb", []string{"plan", "node"}, 2, true, ""},
		{"unsupported topic", []string{"plan", "help", "release"}, 2, true, ""},
		{"positional after flag", []string{"plan", "-product", root, "release", "-help"}, 2, true, ""},
		{"positional without help", []string{"plan", "-product", root, "release"}, 2, true, ""},
		{"invalid top flag", []string{"plan", "-product", root, "-unknown"}, 2, true, ""},
		{"invalid bool value", []string{"plan", "-product", root, "-dry-run=bad"}, 2, true, ""},
		{"invalid node flag", []string{"plan", "node", "list", "-bogus"}, 2, true, ""},
		{"invalid node flag after product", []string{"plan", "node", "list", "-product", root, "-bogus"}, 2, true, ""},
		{"node flag help", []string{"plan", "node", "list", "-help"}, 2, true, ""},
		// Go flag.Parse consumes "-help" as the VALUE of the -receipt string.
		// This is a genuine supported-command execution failure, NOT help.
		{"help as a receipt value", []string{"plan", "node", "close", "N-1", "-receipt", "-help", "-product", root}, 2, false, "plan node close"},
		{"normal local help", []string{"plan", "help"}, 0, false, ""},
		{"normal flag help", []string{"plan", "-help"}, 0, false, ""},
		{"normal product flag help", []string{"plan", "-product", root, "-help"}, 0, false, ""},
		{"valid node list", []string{"plan", "node", "list", "-product", root, "-json"}, 0, false, ""},
		{"valid node spine", []string{"plan", "spine", "-product", root, "-json"}, 0, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := run(tc.argv)
			if rc != tc.wantRC {
				t.Fatalf("unexpected exit: got %d, want %d", rc, tc.wantRC)
			}
			if got := planSyntaxOnly.Load(); got != tc.syntaxOnly {
				t.Fatalf("parser classification: syntaxOnly=%t, want %t", got, tc.syntaxOnly)
			}
			class, eligible := ownCLIFailureAction(tc.argv)
			if rc != 0 {
				if eligible != (tc.captureClass != "") || class != tc.captureClass {
					t.Fatalf("wrong paid review eligibility: class=%q enabled=%t", class, eligible)
				}
			}
			if modelCalls != 0 {
				t.Fatalf("paid planner was reached %d times", modelCalls)
			}
			for _, name := range []string{".woody", "PLAN.proposed.md"} {
				if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Fatalf("help/unsupported path touched product %q: %v", name, err)
				}
			}
			after, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
			if err != nil || string(after) != string(before) {
				t.Fatalf("existing canonical plan changed: %v", err)
			}
		})
	}
	// Prove the product configuration really reaches the normal planner path.
	// Dry run is explicitly safe and returns before any external model call.
	if code := run([]string{"plan", "-product", root, "-dry-run"}); code != 0 {
		t.Fatalf("valid planner fixture not executable by safe dry-run: RC=%d", code)
	}
	if _, err := os.Stat(filepath.Join(root, ".woody", "planner-prompt.txt")); err != nil {
		t.Fatalf("valid fixture did not reach planner preflight: %v", err)
	}
	if modelCalls != 0 {
		t.Fatalf("even a valid dry-run called model %d times", modelCalls)
	}
}

func TestN038ParsedSyntaxDoesNotCarryOverBetweenCalls(t *testing.T) {
	root := n038ValidPlannerProduct(t)
	if rc := run([]string{"plan", "-product", root, "-dry-run=bad"}); rc != 2 || !planSyntaxOnly.Load() {
		t.Fatalf("invalid boolean did not mark syntax, RC=%d", rc)
	}
	if rc := run([]string{"plan", "node", "list", "-product", root, "-json"}); rc != 0 || planSyntaxOnly.Load() {
		t.Fatalf("syntax state leaked to supported node list: RC=%d", rc)
	}
	if action, eligible := ownCLIFailureAction([]string{"plan", "node", "list", "-product", root, "-json"}); !eligible || action != "plan node list" {
		t.Fatalf("supported command no longer eligible after previous syntax error: %s %t", action, eligible)
	}
}

func TestN038NoRawHelpTokenHeuristic(t *testing.T) {
	for _, args := range [][]string{
		{"plan", "node", "close", "N-1", "-receipt", "-help"},
		{"plan", "node", "close", "N-1", "-receipt=-help"},
	} {
		planSyntaxOnly.Store(false)
		got, valid := ownCLIFailureAction(args)
		if !valid || got != "plan node close" {
			t.Fatalf("flag value misclassified as help: %q -> %q %t", strings.Join(args, " "), got, valid)
		}
	}
}
