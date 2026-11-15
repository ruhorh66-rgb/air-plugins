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
		// Help needs no rule of its own: flag.ErrHelp ends as untyped exit 2,
		// and the trusted-cause parser rejects -help, so no typed cause exists.
		{"help request", []string{"plan", "node", "new", "-h"}, 2},
		{"help on validate with missing run-config", []string{"validate", "-product", product, "-help"}, 2},
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
	// Operator calls: list, new, validate, judge, new -h, unknown = 6; every
	// one stays in the denominator. exit 2: new, new -h, unknown = 3.
	if first.Calls != 6 || first.Exit2 != 3 || first.Nonzero != 4 || first.HelpToken != 1 || first.Exit2Rate != 0.5 {
		t.Fatalf("unexpected counters: %+v", first)
	}
	if _, ok := first.ByCommand["hook"]; ok {
		t.Fatal("host hook traffic entered the operator denominator")
	}
	if first.ByCommand["unknown"].Exit2 != 1 || first.ByCommand["plan node new"].Calls != 2 || first.ByCommand["plan node new"].HelpToken != 1 {
		t.Fatalf("per-command breakdown wrong: %+v", first.ByCommand)
	}
	// Pilot classes: plan node list (0), plan node new (2, 2), unknown (2);
	// validate and judge are gate classes and stay out of the pilot target.
	if p := first.Pilot; p.Calls != 4 || p.Exit2 != 3 || p.HelpToken != 1 {
		t.Fatalf("pilot row wrong: %+v", p)
	}
	if c := first.ByHost["codex"]; c.Calls != 2 || c.Exit2 != 1 || c.Exit2Rate != 0.5 {
		t.Fatalf("codex host row wrong: %+v", first.ByHost)
	}
	if first.ByHost["claude"].Calls != 2 || first.ByHost["claude"].HelpToken != 1 ||
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

// Codex review P1: a typed first capture whose precondition is later restored
// must still go through receipt authentication on replay, not the filter.
func TestA1ReplayAfterPreconditionChangeStaysAuthenticated(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	config := filepath.Join(product, "run-config.json")
	saved, readErr := os.ReadFile(config)
	if err := os.Remove(config); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	argv := []string{"validate", "-product", product}
	run := "AW-CLI-A1-replay-001"
	if err := autoCaptureOwnCLIExitWithRunID(argv, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	if readErr != nil {
		saved = []byte("{}\n")
	}
	if err := os.WriteFile(config, saved, 0600); err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(argv, 2, run, nil); err != nil {
		t.Fatalf("exact replay after restored precondition failed: %v", err)
	}
	if n, _ := n110CountFailureEvents(t, settings.RuntimeRoot, run); n != 1 {
		t.Fatalf("replay changed event count: %d", n)
	}
	path := filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), "Trusted read-only precondition", "Tampered precondition", 1)
	if tampered == string(raw) {
		t.Fatal("fixture receipt has no typed observation to tamper")
	}
	if err := os.WriteFile(path, []byte(tampered), 0600); err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(argv, 2, run, nil); err == nil {
		t.Fatal("tampered receipt accepted once the precondition no longer holds")
	}
}

// Codex review rounds 1-3: help is never inferred, so a help token used as a
// flag value cannot move a call out of the denominator.
func TestA1HelpTokenNeverExcludesACall(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	for _, argv := range [][]string{
		{"validate", "-product", "--help"},
		{"plan", "-product", "-foo", "-h"},
		{"plan", "-all", "-h"},
		{"tool", "--", "-h"},
	} {
		if err := recordOwnCLIInvocation(argv, 2); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	st, err := readOwnCLIStats(settings.RuntimeRoot, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.Calls != 4 || st.Exit2 != 4 || st.HelpToken != 3 || st.Pilot.Calls != 2 || st.Pilot.Exit2 != 2 {
		t.Fatalf("help tokens changed the numbers: %+v", st)
	}
}

// Codex review P2: calls made while learning is disabled still count.
func TestA1CounterRecordsWhileLearningDisabled(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	t.Setenv("AIR_WORKER_HOST", "claude")
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("fixture selector: found=%v err=%v", found, err)
	}
	sel.Enabled = false
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	if err := recordOwnCLIInvocation([]string{"learn", "self", "disable"}, 0); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	st, err := readOwnCLIStats(settings.RuntimeRoot, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if st.Calls != 1 || st.ByCommand["learn"].Calls != 1 {
		t.Fatalf("disabled-owner call not counted: %+v", st)
	}
}

// Codex review P2: the R02 disposable validate child is machine traffic.
func TestA1ReleaseSliceChildNotCounted(t *testing.T) {
	_, settings, _ := selfLearningFixture(t)
	t.Setenv(releaseSliceReviewDeferEnv, "1")
	if err := recordOwnCLIInvocation([]string{"validate", "-product", "x", "-json"}, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownCLIInvocationPath(settings.RuntimeRoot, time.Now())); !os.IsNotExist(err) {
		t.Fatalf("R02 child entered the operator denominator: %v", err)
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
