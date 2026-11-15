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

// A bare nonzero exit without a typed native cause carries no lesson: the
// observation would be the same template for every failure. Such exits, help
// requests and ordinary judge FAIL verdicts are counted, never learned from.
func ownCLISignalSkipReason(argv []string, action string, rc int, cause string) string {
	switch {
	case ownCLIHelpRequested(argv):
		return "help"
	case action == "judge" && rc == 1:
		return "judge-verdict"
	case rc == 2 && cause == "":
		return "untyped-exit-2"
	default:
		return ""
	}
}

// A help token counts only where Go's flag parser would read it as a flag:
// not after "--" and not as the value of a preceding "-name" flag. Boolean
// flags never take a separate value, so a help token after them still counts.
func ownCLIHelpRequested(argv []string) bool {
	for i, a := range argv {
		if a == "--" {
			return false
		}
		switch a {
		case "-h", "-help", "--h", "--help":
			if i == 0 || !ownCLIFlagTakesNextValue(argv[i-1]) {
				return true
			}
		}
	}
	return false
}

func ownCLIFlagTakesNextValue(prev string) bool {
	if !strings.HasPrefix(prev, "-") || prev == "-" || strings.Contains(prev, "=") {
		return false
	}
	switch strings.TrimLeft(prev, "-") {
	case "json", "open", "no-owner", "h", "help", "v", "version":
		return false
	}
	return true
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
	At         string `json:"at"`
	Command    string `json:"command"`
	ExitCode   int    `json:"exit_code"`
	Help       bool   `json:"help,omitempty"`
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
// is nowhere to count.
func recordOwnCLIInvocation(argv []string, rc int) error {
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
		Schema: ownCLIInvocationSchema, At: now.Format(time.RFC3339), Command: command,
		ExitCode: rc, Help: ownCLIHelpRequested(argv), Host: host, HostSource: hostSource, Version: version,
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

// Calls exclude help requests in every row: asking for usage is not a call
// that can fail, so it enters neither numerator nor denominator.
type ownCLIStatsRow struct {
	Calls     int     `json:"calls"`
	Exit2     int     `json:"exit_2"`
	Nonzero   int     `json:"nonzero"`
	Help      int     `json:"help"`
	Exit2Rate float64 `json:"exit_2_rate"`
}

func (r *ownCLIStatsRow) add(inv ownCLIInvocation) {
	if inv.Help {
		r.Help++
		return
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

type ownCLIStats struct {
	Schema string `json:"schema"`
	From   string `json:"from"`
	To     string `json:"to"`
	ownCLIStatsRow
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
	fmt.Printf("%s..%s calls=%d exit_2=%d rate=%.4f nonzero=%d help=%d\n", st.From, st.To, st.Calls, st.Exit2, st.Exit2Rate, st.Nonzero, st.Help)
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
			fmt.Printf("  %-8s %-28s calls=%d exit_2=%d rate=%.4f nonzero=%d help=%d\n", group.title, k, r.Calls, r.Exit2, r.Exit2Rate, r.Nonzero, r.Help)
		}
	}
	return 0
}
