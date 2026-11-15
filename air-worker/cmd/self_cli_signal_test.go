package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestA1NoLearningEventForContentlessExits(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	cases := []struct {
		name string
		argv []string
		rc   int
	}{
		{"untyped exit 2", []string{"plan", "node", "new", "-parent", "N-999_missing"}, 2},
		{"invalid plan action exit 2", []string{"plan", "node", "bogus"}, 2},
		{"judge FAIL verdict", []string{"judge", "-product", product}, 1},
		{"judge untyped exit 2", []string{"judge", "-product", product}, 2},
		{"help request", []string{"plan", "node", "new", "-h"}, 2},
		{"help on validate with trusted cause", []string{"validate", "-product", product, "-help"}, 2},
	}
	for i, tc := range cases {
		run := "AW-CLI-A1-skip-00" + string(rune('0'+i))
		called := 0
		if err := autoCaptureOwnCLIExitWithRunID(tc.argv, tc.rc, run, func([]sharedDeferredReviewJob) error { called++; return nil }); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if called != 0 {
			t.Fatalf("%s: reviewer dispatched for a contentless exit", tc.name)
		}
		if n, _ := n110CountFailureEvents(t, settings.RuntimeRoot, run); n != 0 {
			t.Fatalf("%s: learning event recorded", tc.name)
		}
		if _, err := os.Stat(filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")); !os.IsNotExist(err) {
			t.Fatalf("%s: failure receipt written: %v", tc.name, err)
		}
	}
}

func TestA1TypedCauseAndOtherFailuresStillLearn(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	if err := os.Remove(filepath.Join(product, "run-config.json")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cases := []struct {
		name, run, class string
		argv             []string
		rc               int
	}{
		{"validate run-config missing", "AW-CLI-A1-keep-001", "air-worker-cli-preflight", []string{"validate", "-product", product}, 2},
		{"judge internal error", "AW-CLI-A1-keep-002", "air-worker-cli-failure", []string{"judge", "-product", product}, 3},
		{"plan node new exit 1", "AW-CLI-A1-keep-003", "air-worker-cli-failure", []string{"plan", "node", "new"}, 1},
	}
	for _, tc := range cases {
		if err := autoCaptureOwnCLIExitWithRunID(tc.argv, tc.rc, tc.run, nil); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		n, event := n110CountFailureEvents(t, settings.RuntimeRoot, tc.run)
		if n != 1 || event["class"] != tc.class {
			t.Fatalf("%s: want one %s event, got %d %v", tc.name, tc.class, n, event)
		}
	}
}

func TestA1InvocationCounterGivesStableExit2Rate(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	t.Setenv("CLAUDECODE", "")
	calls := []struct {
		host string
		argv []string
		rc   int
	}{
		{"codex", []string{"plan", "node", "list"}, 0},
		{"codex", []string{"plan", "node", "new"}, 2},
		{"claude", []string{"validate"}, 0},
		{"hermes", []string{"judge"}, 1},
		{"claude", []string{"plan", "node", "new", "-h"}, 2},
		{"claude", []string{"hook", "Stop"}, 0},
		{"codex", []string{"learn", "review-batch"}, 2},
		{"codex", []string{"internal-self-cli-replay"}, 2},
		{"", []string{"no-such-command"}, 2},
	}
	for _, c := range calls {
		t.Setenv("AIR_WORKER_HOST", c.host)
		if err := recordOwnCLIInvocation(c.argv, c.rc); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	first, err := readOwnCLIStats(settings.RuntimeRoot, now, now)
	if err != nil {
		t.Fatal(err)
	}
	// Operator calls without help: list, new, validate, judge, unknown = 5;
	// exit 2: new, unknown = 2; the help call is counted apart.
	if first.Calls != 5 || first.Exit2 != 2 || first.Nonzero != 3 || first.Help != 1 || first.Exit2Rate != 0.4 {
		t.Fatalf("unexpected counters: %+v", first)
	}
	if _, ok := first.ByCommand["hook"]; ok {
		t.Fatal("host hook traffic entered the operator denominator")
	}
	if first.ByCommand["unknown"].Exit2 != 1 || first.ByCommand["plan node new"].Help != 1 {
		t.Fatalf("per-command breakdown wrong: %+v", first.ByCommand)
	}
	if c := first.ByHost["codex"]; c.Calls != 2 || c.Exit2 != 1 || c.Exit2Rate != 0.5 {
		t.Fatalf("codex host row wrong: %+v", first.ByHost)
	}
	if first.ByHost["claude"].Calls != 1 || first.ByHost["claude"].Help != 1 ||
		first.ByHost["hermes"].Nonzero != 1 || first.ByHost["unknown"].Exit2 != 1 {
		t.Fatalf("per-host breakdown wrong: %+v", first.ByHost)
	}
	second, err := readOwnCLIStats(settings.RuntimeRoot, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if second.Calls != first.Calls || second.Exit2 != first.Exit2 || second.Exit2Rate != first.Exit2Rate {
		t.Fatalf("same data gave different numbers: %+v vs %+v", first, second)
	}
}

func TestA1HostIsDeclaredNotGuessed(t *testing.T) {
	cases := []struct{ declared, claudeMarker, host, source string }{
		{"codex", "", "codex", "declared"},
		{" Hermes ", "1", "hermes", "declared"},
		{"vscode", "", "other", "declared"},
		{"", "1", "claude", "marker"},
		{"", "", "unknown", "none"},
	}
	t.Setenv("CODEX_HOME", `C:\codex-home`)
	t.Setenv("HERMES_HOME", `C:\hermes-home`)
	for _, c := range cases {
		t.Setenv("AIR_WORKER_HOST", c.declared)
		t.Setenv("CLAUDECODE", c.claudeMarker)
		if host, source := ownCLIInvocationHost(); host != c.host || source != c.source {
			t.Fatalf("declared=%q marker=%q: got %s/%s", c.declared, c.claudeMarker, host, source)
		}
	}
}

func TestA1CounterKeepsNoArguments(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	if err := recordOwnCLIInvocation(n110CLIArgs(), 2); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(ownCLIInvocationPath(settings.RuntimeRoot, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"WORKER-SECRET", "N-999_missing", "-parent"} {
		if strings.Contains(string(raw), leak) {
			t.Fatalf("counter stored argument %q: %s", leak, raw)
		}
	}
}
