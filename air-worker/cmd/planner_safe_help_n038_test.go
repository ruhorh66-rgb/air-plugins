package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// N-038 is intentionally a zero-model regression. This tests the Go parser,
// never dispatching a real planner, model, command runner or learning reviewer.
func TestN038UnknownPlanSubcommandsFailBeforePlanner(t *testing.T) {
	root := t.TempDir()
	// Deliberately leave runner credentials absent: a regression must never
	// invoke a real provider during an automated parser test.
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte(`{"ladder":["script"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		args []string
		want int
	}{
		{"unknown help", []string{"release", "-help"}, 2},
		{"unknown verb", []string{"release"}, 2},
		{"unknown nested node verb", []string{"node", "release"}, 2},
		{"missing nested node verb", []string{"node"}, 2},
		{"positional after product", []string{"-product", root, "release", "-help"}, 2},
		{"positional without help", []string{"-product", root, "release"}, 2},
		{"invalid flag", []string{"-product", root, "-unknown"}, 2},
		{"explicit help", []string{"help"}, 0},
		{"flag help", []string{"-help"}, 0},
		{"product then help", []string{"-product", root, "-help"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if !planHelpOrUnsupportedSyntax(tc.args) {
				t.Fatal("unsafe help/unsupported argv was not classified before execution")
			}
			if rc := cmdPlan(tc.args); rc != tc.want {
				t.Fatalf("unexpected usage code: %d (want %d)", rc, tc.want)
			}
			for _, name := range []string{".woody", "PLAN.proposed.md", "PLAN.md"} {
				if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
					t.Fatalf("help/unsupported path touched product %q: %v", name, err)
				}
			}
		})
	}
}

func TestN038PlannerSyntaxClassificationPreservesExecutableCommands(t *testing.T) {
	root := filepath.Join(t.TempDir(), "product")
	for _, args := range [][]string{
		{"node", "list", "-product", root, "-json"},
		{"spine", "-product", root, "-json"},
		{"migrate", "-product", root},
		{"-product", root, "-dry-run"},
		{"-product", root, "-model", "gpt6-sol:low", "-dry-run"},
	} {
		if planHelpOrUnsupportedSyntax(args) {
			t.Fatalf("supported planner/native route incorrectly filtered: %q", strings.Join(args, " "))
		}
	}
	for _, tc := range []struct {
		args []string
		want string
		ok   bool
	}{
		{[]string{"plan", "node", "list", "-product", root, "-json"}, "plan node list", true},
		{[]string{"plan", "node", "release"}, "", false},
		{[]string{"plan", "node"}, "", false},
		{[]string{"plan", "-product", root, "-dry-run"}, "plan", true},
		{[]string{"plan", "release", "-help"}, "", false},
		{[]string{"plan", "release"}, "", false},
		{[]string{"plan", "-product", root, "release", "-help"}, "", false},
		{[]string{"plan", "-product", root, "release"}, "", false},
		{[]string{"plan", "--mystery-flag"}, "", false},
		{[]string{"plan", "-help"}, "", false},
		{[]string{"plan", "node", "list", "-help"}, "", false},
	} {
		got, ok := ownCLIFailureAction(tc.args)
		if got != tc.want || ok != tc.ok {
			t.Fatalf("unexpected auto-review routing %q: action=%q enabled=%v (want %q/%v)",
				strings.Join(tc.args, " "), got, ok, tc.want, tc.ok)
		}
	}
}
