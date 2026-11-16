package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A bare exit 2 without a typed native cause carries no lesson: the
// observation would be the same template for every failure, so it is counted
// and not learned from. A judge FAIL (exit 1) is a failed check and still
// creates an event: AIR_VIBECODING invariant 15 requires review on a failed
// check; giving that event the failed check names is a later step. A help
// request (-h) makes Go's flag parser return ErrHelp, which every captured
// action turns into exit 2 without a typed cause, so it is covered by the
// same rule (only `learn` maps ErrHelp to 0, and `learn` is never captured);
// re-deriving help from argv would mean re-implementing each command's flags.
func ownCLISignalSkipReason(rc int, cause string) string {
	if rc == 2 && cause == "" {
		return "untyped-exit-2"
	}
	return ""
}

// Recorded as a plain fact, never used to exclude a call from any number:
// a help token may also be a flag value, and guessing which would let a
// misclassification move calls out of the denominator.
func ownCLIHelpTokenPresent(argv []string) bool {
	for _, a := range argv {
		switch a {
		case "--":
			return false
		case "-h", "-help", "--h", "--help":
			return true
		}
	}
	return false
}

const ownCLIInvocationSchema = "air-worker.self-learning.cli-invocation/v1"

// Only the command class is kept; arguments, paths and secrets never are.
// Host hooks, reviewer adapters and internal replays are machine traffic and
// would dilute the denominator of operator-facing calls.
func ownCLIInvocationCommand(argv []string) (string, bool) {
	// The R02 release slice runs a disposable validate child on purpose to
	// produce exit 2; it is a machine check, not an operator call.
	if os.Getenv(releaseSliceReviewDeferEnv) == "1" {
		return "", false
	}
	if len(argv) == 0 {
		return "usage", true
	}
	switch argv[0] {
	case "hook", "learning-adapter", "internal-self-cli-replay", "tray":
		return "", false
	case "-v", "--version":
		return "version", true
	case "learn":
		if len(argv) > 1 && argv[1] == "review-batch" {
			return "", false
		}
	}
	if action, ok := ownCLIFailureAction(argv); ok {
		return action, true
	}
	if knownCLICommand(argv[0]) {
		return argv[0], true
	}
	return "unknown", true
}

func knownCLICommand(name string) bool {
	switch name {
	case "drift", "loop", "operational-goal", "orchestrate", "job", "semantic",
		"review", "plan-review", "adapter", "encoding", "report", "goals",
		"learn", "curator", "update", "install", "session", "version":
		return true
	}
	return false
}

type ownCLIInvocation struct {
	Schema     string `json:"schema"`
	RunID      string `json:"run_id"`
	At         string `json:"at"`
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	HelpToken  bool   `json:"help_token,omitempty"`
	Host       string `json:"host"`
	HostSource string `json:"host_source"`
	Version    string `json:"version"`
}

// The calling host is declared by its plugin adapter through AIR_WORKER_HOST,
// as session identity is declared rather than guessed. CODEX_HOME and
// HERMES_HOME are machine-wide settings, not proof of the caller, so they are
// never used. CLAUDECODE=1 is set by Claude Code for its own child processes
// and is accepted only as a marked fallback.
func ownCLIInvocationHost() (host, source string) {
	switch h := strings.ToLower(strings.TrimSpace(os.Getenv("AIR_WORKER_HOST"))); h {
	case "codex", "claude", "hermes":
		return h, "declared"
	case "":
	default:
		return "other", "declared"
	}
	if os.Getenv("CLAUDECODE") == "1" {
		return "claude", "marker"
	}
	return "unknown", "none"
}

func ownCLIInvocationPath(runtimeRoot string, at time.Time) string {
	return filepath.Join(runtimeRoot, "cli-invocations", at.UTC().Format("2006-01-02")+".jsonl")
}

// One short O_APPEND line per call. It runs after the command and never
// changes its exit code. The selector's runtime root is used whether learning
// is enabled or disabled, so `learn self disable` and calls made while
// learning is off still enter the denominator; with no selector at all there
// is nowhere to count. run_id is the one also given to a failure receipt of
// the same process (AIR_VIBECODING invariant 15: run_id and outcome per run).
func recordOwnCLIInvocation(argv []string, rc int, runID string) error {
	command, ok := ownCLIInvocationCommand(argv)
	if !ok {
		return nil
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found || !filepath.IsAbs(sel.RuntimeRoot) {
		return err
	}
	now := time.Now().UTC()
	host, hostSource := ownCLIInvocationHost()
	line, err := json.Marshal(ownCLIInvocation{
		Schema: ownCLIInvocationSchema, RunID: runID, At: now.Format(time.RFC3339), Command: command,
		ExitCode: rc, HelpToken: ownCLIHelpTokenPresent(argv), Host: host, HostSource: hostSource, Version: version,
	})
	if err != nil {
		return err
	}
	path := ownCLIInvocationPath(sel.RuntimeRoot, now)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Every recorded call is in the denominator. help_token is shown beside the
// rate as a fact; it is never subtracted, so no classification can move a
// call out of the number.
type ownCLIStatsRow struct {
	Calls     int     `json:"calls"`
	Exit2     int     `json:"exit_2"`
	Nonzero   int     `json:"nonzero"`
	HelpToken int     `json:"help_token"`
	Exit2Rate float64 `json:"exit_2_rate"`
}

func (r *ownCLIStatsRow) add(inv ownCLIInvocation) {
	if inv.HelpToken {
		r.HelpToken++
	}
	r.Calls++
	if inv.ExitCode == 2 {
		r.Exit2++
	}
	if inv.ExitCode != 0 {
		r.Nonzero++
	}
	r.Exit2Rate = float64(r.Exit2) / float64(r.Calls)
}

// The B1 pilot target counts only classes where exit 2 means a wrong call,
// not a correct fail-closed gate refusal (curator decision 15.11.2026):
// lowering the overall number by weakening a gate would game the metric.
var ownCLIPilotClasses = map[string]bool{
	"plan": true, "plan node": true, "plan node new": true, "plan node close": true,
	"plan node reparent": true, "plan node list": true, "plan node invalid-action": true,
	"plan-lint": true, "unknown": true,
}

type ownCLIStats struct {
	Schema string `json:"schema"`
	From   string `json:"from"`
	To     string `json:"to"`
	ownCLIStatsRow
	Pilot     ownCLIStatsRow            `json:"pilot"`
	Skipped   int                       `json:"unreadable_lines"`
	ByHost    map[string]ownCLIStatsRow `json:"by_host"`
	ByCommand map[string]ownCLIStatsRow `json:"by_command"`
}

func readOwnCLIStats(runtimeRoot string, from, to time.Time) (ownCLIStats, error) {
	st := ownCLIStats{Schema: "air-worker.self-learning.cli-stats/v1",
		From: from.UTC().Format("2006-01-02"), To: to.UTC().Format("2006-01-02"),
		ByHost: map[string]ownCLIStatsRow{}, ByCommand: map[string]ownCLIStatsRow{}}
	for d := from.UTC().Truncate(24 * time.Hour); !d.After(to.UTC()); d = d.Add(24 * time.Hour) {
		raw, err := readLearningBounded(ownCLIInvocationPath(runtimeRoot, d), 64*1024*1024)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return st, err
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var inv ownCLIInvocation
			if json.Unmarshal([]byte(line), &inv) != nil || inv.Schema != ownCLIInvocationSchema {
				st.Skipped++
				continue
			}
			st.add(inv)
			if ownCLIPilotClasses[inv.Command] {
				st.Pilot.add(inv)
			}
			host := inv.Host
			if host == "" {
				host = "unknown"
			}
			h := st.ByHost[host]
			h.add(inv)
			st.ByHost[host] = h
			c := st.ByCommand[inv.Command]
			c.add(inv)
			st.ByCommand[inv.Command] = c
		}
	}
	return st, nil
}

func cmdLearnSelfCLIStats(argv []string) int {
	fs := flag.NewFlagSet("learn self cli-stats", flag.ContinueOnError)
	days := fs.Int("days", 7, "UTC days back from today, inclusive")
	asJSON := fs.Bool("json", true, "machine-readable JSON")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 || *days < 1 || *days > 366 {
		fmt.Fprintln(os.Stderr, "usage: air-worker learn self cli-stats [-days 1..366]")
		return 2
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err == nil && (!found || !filepath.IsAbs(sel.RuntimeRoot)) {
		err = errors.New("self-learning selector is not configured")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self cli-stats:", err)
		return 2
	}
	to := time.Now().UTC()
	st, err := readOwnCLIStats(sel.RuntimeRoot, to.AddDate(0, 0, -(*days-1)), to)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn self cli-stats:", err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(st)
		return 0
	}
	fmt.Printf("%s..%s calls=%d exit_2=%d rate=%.4f nonzero=%d help_token=%d\n", st.From, st.To, st.Calls, st.Exit2, st.Exit2Rate, st.Nonzero, st.HelpToken)
	fmt.Printf("  pilot    wrong-call classes           calls=%d exit_2=%d rate=%.4f\n", st.Pilot.Calls, st.Pilot.Exit2, st.Pilot.Exit2Rate)
	for _, group := range []struct {
		title string
		rows  map[string]ownCLIStatsRow
	}{{"host", st.ByHost}, {"command", st.ByCommand}} {
		names := make([]string, 0, len(group.rows))
		for k := range group.rows {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			r := group.rows[k]
			fmt.Printf("  %-8s %-28s calls=%d exit_2=%d rate=%.4f nonzero=%d help_token=%d\n", group.title, k, r.Calls, r.Exit2, r.Exit2Rate, r.Nonzero, r.HelpToken)
		}
	}
	return 0
}
