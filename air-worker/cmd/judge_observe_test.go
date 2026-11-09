package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCanonicalFullGoCheck(t *testing.T) {
	var cfg runConfig
	if err := readJSON(filepath.Join("..", "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	var checks []checkSpec
	for _, check := range cfg.Judge.Checks {
		if check.Name == "тесты бинарника" {
			checks = append(checks, check)
		}
	}
	if len(checks) != 1 {
		t.Fatalf("canonical Go checks = %d, want 1", len(checks))
	}
	check := checks[0]
	if !isGoCommand(check.Command) || check.Script != "" {
		t.Fatalf("canonical check executable: command=%q script=%q", check.Command, check.Script)
	}
	args := check.Args
	if len(args) < 6 || !slices.Equal(args[:5], []string{"-C", "cmd", "test", "./...", "-count=1"}) {
		t.Fatalf("canonical Go arguments = %q", args)
	}
	timeout := ""
	if len(args) == 6 {
		timeout = strings.TrimPrefix(args[5], "-timeout=")
		if timeout == args[5] {
			timeout = ""
		}
	} else if len(args) == 7 && args[5] == "-timeout" {
		timeout = args[6]
	}
	duration, err := time.ParseDuration(timeout)
	if err != nil || duration != 25*time.Minute {
		t.Fatalf("explicit Go timeout = %q, want 25m", timeout)
	}
	bound, err := n123GoJudgeDuration(args)
	if err != nil || bound != 25*time.Minute+30*time.Second {
		t.Fatalf("native bound = %v, err = %v, want 25m30s", bound, err)
	}
}

func TestN123GoJudgeDuration(t *testing.T) {
	if n123GoJudgeMaxDuration != 4*time.Minute {
		t.Fatalf("default duration = %v, want 4m", n123GoJudgeMaxDuration)
	}
	for _, tc := range []struct {
		name string
		args []string
		want time.Duration
		bad  bool
	}{
		{"no timeout", []string{"test", "./..."}, 0, false},
		{"equals", []string{"-C", "cmd", "test", "./...", "-timeout=12m"}, 12*time.Minute + 30*time.Second, false},
		{"separate", []string{"test", "./...", "-timeout", "12m"}, 12*time.Minute + 30*time.Second, false},
		{"build", []string{"build", "-timeout=12m", "./..."}, 0, false},
		{"unrelated argument", []string{"test", "-run", "timeout=12m", "./..."}, 0, false},
		{"malformed", []string{"test", "-timeout=invalid"}, 0, true},
		{"missing", []string{"test", "-timeout"}, 0, true},
		{"zero", []string{"test", "-timeout=0"}, 0, true},
		{"negative", []string{"test", "-timeout=-1m"}, 0, true},
		{"25 minutes", []string{"test", "-timeout=25m"}, 25*time.Minute + 30*time.Second, false},
		{"boundary", []string{"test", "-timeout=29m30s"}, 30 * time.Minute, false},
		{"over limit", []string{"test", "-timeout=29m30s1ns"}, 0, true},
		{"ambiguous", []string{"test", "-unknown", "-timeout=12m"}, 0, true},
		{"duplicate", []string{"test", "-timeout=12m", "-timeout", "12m"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := n123GoJudgeDuration(tc.args)
			if tc.bad {
				if err == nil {
					t.Fatalf("duration = %v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := tc.want
			if want == 0 {
				want = n123GoJudgeMaxDuration
			}
			if got != want {
				t.Fatalf("duration = %v, want %v", got, want)
			}
		})
	}
}

func TestN123GoJudgeTimeoutHelper(t *testing.T) {
	if os.Getenv("AW_N123_TIMEOUT_HELPER") != "1" {
		t.Skip("helper process only")
	}
	f, err := os.OpenFile(os.Getenv("AW_N123_STARTS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("started\n")
	_ = f.Close()
	fmt.Println("owned Go judge fixture started")
	time.Sleep(5 * time.Second)
}

func TestN123GoJudgeTimeoutSealsOwnedProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows process-tree termination")
	}
	root := t.TempDir()
	initJudgeRefreshGitRepo(t, root)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	goExe := filepath.Join(t.TempDir(), "go.exe")
	if err := os.WriteFile(goExe, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	starts := filepath.Join(root, "starts.txt")
	t.Setenv("AW_N123_TIMEOUT_HELPER", "1")
	t.Setenv("AW_N123_STARTS", starts)
	old := n123GoJudgeMaxDuration
	n123GoJudgeMaxDuration = 500 * time.Millisecond
	t.Cleanup(func() { n123GoJudgeMaxDuration = old })
	args := []string{"-test.run=^TestN123GoJudgeTimeoutHelper$"}
	cmd := newChildCommand(goExe, args...)
	cmd.Dir = root
	operation := n123GoJudgeOperation(root, "unit", cmd)
	if operation == "unit" {
		t.Fatal("Go judge operation was not bound to source")
	}
	scope := legacyScope(root)
	result := make(chan judgeResult, 1)
	before := time.Now()
	go func() {
		var r judgeResult
		runCommandCheck(root, checkSpec{Command: goExe, Args: args}, "unit", scope, &r)
		result <- r
	}()
	var verdict judgeResult
	select {
	case verdict = <-result:
	case <-time.After(2 * time.Second):
		receipt, output := jobReceiptPaths(scope, "judge", operation)
		data, _ := os.ReadFile(output)
		t.Fatalf("NOT_PROVEN: owned Go judge did not terminate; receipt=%s output=%s log=%q", receipt, output, data)
	}
	if len(verdict.Passed) != 0 || len(verdict.Failed)+len(verdict.Unknown) == 0 {
		t.Fatalf("timeout verdict must not pass: %+v", verdict)
	}
	// Transport timeout may return while the owned child is still exiting.
	// Wait for the native Wait/finalize goroutine, not for another model run.
	pendingReceipt, _ := jobReceiptPaths(scope, "judge", operation)
	_ = pollReceiptStatus(t, pendingReceipt, jobStatusFailed, 5*time.Second)
	view, err := inspectOwnedJob(scope, "judge", operation)
	if err != nil {
		t.Fatalf("NOT_PROVEN: inspect receipt: %v", err)
	}
	deadline, err := time.Parse(time.RFC3339Nano, view.DeadlineUTC)
	if err != nil || deadline.Before(before) || deadline.After(before.Add(2*time.Second)) {
		t.Fatalf("invalid deadline %q: %v", view.DeadlineUTC, err)
	}
	if !view.Terminal || view.Status != jobStatusFailed || view.ExitCode == nil || *view.ExitCode == 0 || view.OutputSHA256 == "" || view.OutputBytes == 0 {
		t.Fatalf("NOT_PROVEN: terminal receipt not sealed: %+v", view)
	}
	receiptPath, outputPath := jobReceiptPaths(scope, "judge", operation)
	receipt, err := readJobReceipt(receiptPath)
	if err != nil || !receipt.ProcessStarted || receipt.PID <= 0 {
		t.Fatalf("NOT_PROVEN: owned process was not recorded: receipt=%+v err=%v", receipt, err)
	}
	listed, err := exec.Command("tasklist.exe", "/FI", "PID eq "+strconv.Itoa(receipt.PID), "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil || strings.Contains(string(listed), `"`+strconv.Itoa(receipt.PID)+`"`) {
		t.Fatalf("NOT_PROVEN: owned process termination: pid=%d err=%v tasklist=%s", receipt.PID, err, listed)
	}
	receiptBefore, _ := os.ReadFile(receiptPath)
	outputBefore, _ := os.ReadFile(outputPath)
	var again judgeResult
	runCommandCheck(root, checkSpec{Command: goExe, Args: args}, "unit", scope, &again)
	receiptAfter, _ := os.ReadFile(receiptPath)
	outputAfter, _ := os.ReadFile(outputPath)
	started, err := os.ReadFile(starts)
	if err != nil || string(started) != "started\n" || string(receiptBefore) != string(receiptAfter) || string(outputBefore) != string(outputAfter) || len(again.Passed) != 0 {
		t.Fatalf("duplicate start or changed evidence: starts=%q err=%v verdict=%+v", started, err, again)
	}
}

func TestN123GoJudgeOperationBoundToSourceAndArguments(t *testing.T) {
	root := t.TempDir()
	initJudgeRefreshGitRepo(t, root)
	const name = "unit"
	command := func(arg string) *exec.Cmd { return exec.Command("go", "test", arg) }
	operation := n123GoJudgeOperation(root, name, command("./secret-package"))
	if operation == name || len(operation) > 128 || !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(operation) || strings.Contains(operation, "secret-package") {
		t.Fatalf("unsafe or unbound operation: %q", operation)
	}
	scope := legacyScope(root)
	receipt, output := jobReceiptPaths(scope, "N123", operation)
	same := n123GoJudgeOperation(root, name, command("./secret-package"))
	sameReceipt, sameOutput := jobReceiptPaths(scope, "N123", same)
	if same != operation || sameReceipt != receipt || sameOutput != output {
		t.Fatalf("identical inputs changed identity: %q vs %q", operation, same)
	}
	changedArgs := n123GoJudgeOperation(root, name, command("./other-package"))
	argsReceipt, argsOutput := jobReceiptPaths(scope, "N123", changedArgs)
	if changedArgs == operation || argsReceipt == receipt || argsOutput == output {
		t.Fatal("changed arguments did not change identity and paths")
	}
	if err := os.MkdirAll(filepath.Dir(receipt), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receipt, []byte("original receipt"), 0o644); err != nil {
		t.Fatal(err)
	}
	commitJudgeRefreshFixture(t, root, "v2\n")
	changedHead := n123GoJudgeOperation(root, name, command("./secret-package"))
	headReceipt, headOutput := jobReceiptPaths(scope, "N123", changedHead)
	if changedHead == operation || headReceipt == receipt || headOutput == output {
		t.Fatal("changed HEAD did not change identity and paths")
	}
	oldReceipt, _ := jobReceiptPaths(scope, "N123", operation)
	if data, err := os.ReadFile(oldReceipt); err != nil || string(data) != "original receipt" {
		t.Fatalf("original receipt no longer addressable: %q, %v", data, err)
	}
	if got := n123GoJudgeOperation(root, name, exec.Command("echo", "test")); got != name {
		t.Fatalf("non-Go operation = %q, want %q", got, name)
	}
	if got := n123GoJudgeOperation(t.TempDir(), name, command("./secret-package")); got != name {
		t.Fatalf("Gitless operation = %q, want %q", got, name)
	}
}

func writeObservabilityFixture(t *testing.T, exitCode int) (root, counterPath string) {
	t.Helper()
	root = t.TempDir()
	counterPath = filepath.Join(root, "counter.txt")
	script := "param([string]$Counter,[int]$Code)\n" +
		"$n=0; if(Test-Path -LiteralPath $Counter){$n=[int](Get-Content -LiteralPath $Counter -Raw)}; " +
		"$n++; [IO.File]::WriteAllText($Counter,[string]$n,[Text.UTF8Encoding]::new($false)); " +
		"if($Code -ne 0){Write-Output 'fixture failure'}; exit $Code\n"
	if err := os.WriteFile(filepath.Join(root, "check.ps1"), append(utf8BOM, []byte(script)...), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "**Ц1.** observable fixture\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | base check | проверка `probe` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | work | `script` | К1 |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := runConfig{
		Plan: "PLAN.md",
		Judge: judgeSpec{Checks: []checkSpec{{
			Name: "probe", Script: "check.ps1",
			Args: []string{counterPath, strconv.Itoa(exitCode)},
		}}},
	}
	raw, _ := json.Marshal(cfg)
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return root, counterPath
}

func readCounter(t *testing.T, path string) int {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	var n int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(raw)), "%d", &n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestJudgeSessionScope(t *testing.T) {
	root, counter := writeObservabilityFixture(t, 0)
	state := t.TempDir()
	id := sessionIdentity{Principal: "chatgpt", SessionKey: "judge-fixture"}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	base := []string{"-product", root, "-state-dir", state}
	run := func(args ...string) int {
		_, code := captureStdout(t, func() int { return cmdJudge(append(append([]string{}, base...), args...)) })
		return code
	}
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing session key", []string{"-principal", id.Principal}},
		{"missing principal", []string{"-session-key", id.SessionKey}},
		{"unknown session", []string{"-principal", id.Principal, "-session-key", "unknown"}},
		{"foreign principal", []string{"-principal", "claude", "-session-key", id.SessionKey}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := run(tc.args...); code != 2 || readCounter(t, counter) != 0 {
				t.Fatalf("code=%d, check count=%d", code, readCounter(t, counter))
			}
		})
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: t.TempDir(), Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	identity := []string{"-principal", id.Principal, "-session-key", id.SessionKey}
	if code := run(identity...); code != 2 || readCounter(t, counter) != 0 {
		t.Fatalf("foreign product: code=%d, check count=%d", code, readCounter(t, counter))
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if code := run(identity...); code != 2 || readCounter(t, counter) != 0 {
		t.Fatalf("inactive session: code=%d, check count=%d", code, readCounter(t, counter))
	}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(root, "PLAN.md")
	plan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planPath, []byte("invalid plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run(identity...); code != 2 || readCounter(t, counter) != 0 {
		t.Fatalf("invalid plan: code=%d, check count=%d", code, readCounter(t, counter))
	}
	if err := os.WriteFile(planPath, plan, 0o644); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		t.Skip("fixture check requires Windows PowerShell")
	}
	if code := run(identity...); code != 0 || readCounter(t, counter) != 1 {
		t.Fatalf("scoped judge: code=%d, check count=%d", code, readCounter(t, counter))
	}
	receipt, output := jobReceiptPaths(newScope(root, id), "judge", "probe")
	beforeReceipt, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	beforeOutput, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	if code := run(identity...); code == 0 || readCounter(t, counter) != 1 {
		t.Fatalf("duplicate judge: code=%d, check count=%d", code, readCounter(t, counter))
	}
	afterReceipt, receiptErr := os.ReadFile(receipt)
	afterOutput, outputErr := os.ReadFile(output)
	if receiptErr != nil || outputErr != nil || string(beforeReceipt) != string(afterReceipt) || string(beforeOutput) != string(afterOutput) {
		t.Fatal("duplicate judge changed receipt or output")
	}
	_, code := captureStdout(t, func() int { return cmdJudge([]string{"-product", root}) })
	if code != 0 || readCounter(t, counter) != 2 {
		t.Fatalf("legacy judge: code=%d, check count=%d", code, readCounter(t, counter))
	}
	legacyReceipt, _ := jobReceiptPaths(legacyScope(root), "judge", "probe")
	if _, err := os.Stat(legacyReceipt); err != nil {
		t.Fatalf("legacy receipt: %v", err)
	}
}

func TestJudgeJSONReportDoesNotRerunChecks(t *testing.T) {
	root, counter := writeObservabilityFixture(t, 0)
	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	res := runJudge(root, cfg, -1, legacyScope(root))
	res.InputFingerprint = judgeInputFingerprint(root, cfg, filepath.Join(root, "run-config.json"), filepath.Join(root, "PLAN.md"))
	code, text := verdict(res)
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("regular factual run executed check %d times", got)
	}
	mv := machineVerdictFromResult(root, code, text, res)
	report := buildJudgeJSONReport(root, mv, res)
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("building JSON reran check: %d", got)
	}
	if len(report.Checks) != 1 || report.Checks[0].Name != "probe" || report.Checks[0].State != "PASS" {
		t.Fatalf("check observation=%#v", report.Checks)
	}
	if len(report.Criteria) != 1 || report.Criteria[0].ID != "К1" || report.Criteria[0].State != "PASS" {
		t.Fatalf("criterion observation=%#v", report.Criteria)
	}
	if report.InputFingerprint == "" {
		t.Fatal("JSON report lacks input fingerprint")
	}
}

func TestJudgeInputFingerprintChangesOnProtectedInputs(t *testing.T) {
	root, _ := writeObservabilityFixture(t, 0)
	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "run-config.json")
	planPath := filepath.Join(root, "PLAN.md")
	base := judgeInputFingerprint(root, cfg, configPath, planPath)
	if base == "" {
		t.Fatal("empty fingerprint")
	}
	for _, name := range []string{"PLAN.md", "run-config.json", "check.ps1"} {
		path := filepath.Join(root, name)
		before, _ := os.ReadFile(path)
		if err := os.WriteFile(path, append(before, []byte("\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		got := judgeInputFingerprint(root, cfg, configPath, planPath)
		if got == base {
			t.Fatalf("%s mutation did not change fingerprint", name)
		}
		if err := os.WriteFile(path, before, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCachedReportDoesNotRunJudgeAndRejectsStaleFingerprint(t *testing.T) {
	root, counter := writeObservabilityFixture(t, 1)
	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	res := runJudge(root, cfg, -1, legacyScope(root))
	res.InputFingerprint = judgeInputFingerprint(root, cfg, filepath.Join(root, "run-config.json"), filepath.Join(root, "PLAN.md"))
	code, text := verdict(res)
	if code != 1 {
		t.Fatalf("fixture factual code=%d text=%s", code, text)
	}
	publishVerdict(root, code, text, res)
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("initial judge count=%d", got)
	}

	report := buildReportMode(root, true)
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("cached report reran judge: %d", got)
	}
	if !report.JudgeCached || report.JudgeStale || report.JudgeCode != 1 {
		t.Fatalf("cached report=%#v", report)
	}

	scriptPath := filepath.Join(root, "check.ps1")
	raw, _ := os.ReadFile(scriptPath)
	if err := os.WriteFile(scriptPath, append(raw, []byte("# input changed\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	stale := buildReportMode(root, true)
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("stale cached report ran judge: %d", got)
	}
	if stale.JudgeCode != 2 || !stale.JudgeStale || stale.Measure.Distance != nil || stale.Measure.Verdict != "NOT_PROVEN" {
		t.Fatalf("stale report did not fail closed: %#v", stale)
	}
	if !strings.Contains(strings.Join(stale.Limits, "\n"), "stale") {
		t.Fatalf("stale reason not surfaced: %v", stale.Limits)
	}
}

func TestActualJudgeCachedReportTracksSourceIdentity(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fixture check requires Windows PowerShell")
	}
	for _, nested := range []bool{false, true} {
		for _, dirtyAtJudge := range []bool{false, true} {
			name := fmt.Sprintf("nested=%t/dirty-at-judge=%t", nested, dirtyAtJudge)
			t.Run(name, func(t *testing.T) {
				outer := t.TempDir()
				product := outer
				if nested {
					product = filepath.Join(outer, "product")
					if err := os.Mkdir(product, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				fixture, _ := writeObservabilityFixture(t, 0)
				for _, name := range []string{"PLAN.md", "run-config.json", "check.ps1"} {
					body, err := os.ReadFile(filepath.Join(fixture, name))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(product, name), body, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				counter := filepath.Join(t.TempDir(), "counter.txt")
				var cfg runConfig
				configPath := filepath.Join(product, "run-config.json")
				if err := readJSON(configPath, &cfg); err != nil {
					t.Fatal(err)
				}
				cfg.Judge.Checks[0].Args[0] = counter
				body, err := json.Marshal(cfg)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(configPath, body, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(outer, ".gitignore"), []byte(".woody/\n.goal-verdict\n.goal-verdict.json\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				git := func(args ...string) string {
					t.Helper()
					cmd := exec.Command("git", append([]string{"-C", outer}, args...)...)
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("git %v: %v: %s", args, err, out)
					}
					return strings.TrimSpace(string(out))
				}
				git("init")
				git("config", "user.email", "air-worker-test@example.invalid")
				git("config", "user.name", "AirWorker Test")
				git("add", ".")
				git("commit", "-m", "fixture")
				top := exec.Command("git", "-C", product, "rev-parse", "--show-toplevel")
				out, err := top.CombinedOutput()
				if err != nil || !strings.EqualFold(filepath.Clean(strings.TrimSpace(string(out))), filepath.Clean(outer)) {
					t.Fatalf("product Git root = %q, want %q: %v", out, outer, err)
				}
				if _, err := os.Stat(filepath.Join(product, ".git")); !os.IsNotExist(err) && nested {
					t.Fatalf("nested product has its own .git: %v", err)
				}
				source := filepath.Join(product, "check.ps1")
				original, err := os.ReadFile(source)
				if err != nil {
					t.Fatal(err)
				}
				mutated := append(append([]byte(nil), original...), []byte("# changed\n")...)
				if dirtyAtJudge {
					if err := os.WriteFile(source, mutated, 0o644); err != nil {
						t.Fatal(err)
					}
				}
				_, code := captureStdout(t, func() int { return cmdJudge([]string{"-product", product}) })
				if readCounter(t, counter) != 1 {
					t.Fatalf("judge code=%d, check count=%d", code, readCounter(t, counter))
				}
				if err := os.WriteFile(source, original, 0o644); err != nil {
					t.Fatal(err)
				}
				if status := git("status", "--porcelain"); status != "" {
					t.Fatalf("restored source is not clean: %s", status)
				}
				cached := func(want int) {
					t.Helper()
					report := buildReportMode(product, true)
					if !report.JudgeCached || report.JudgeCode != want || report.JudgeStale != (want == 2) || readCounter(t, counter) != 1 {
						t.Fatalf("cached report=%#v; check count=%d, want code=%d", report, readCounter(t, counter), want)
					}
				}
				if dirtyAtJudge {
					cached(2)
					return
				}
				if code != 0 {
					t.Fatalf("clean judge code=%d, want PASS", code)
				}
				cached(0)
				if err := os.WriteFile(source, mutated, 0o644); err != nil {
					t.Fatal(err)
				}
				cached(2)
				if err := os.WriteFile(source, original, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(product, "untracked.go"), []byte("package fixture\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				cached(2)
			})
		}
	}
}

func TestReportInformationalChangesOnlyExitCode(t *testing.T) {
	root, counter := writeObservabilityFixture(t, 1)
	var cfg runConfig
	if err := readJSON(filepath.Join(root, "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	res := runJudge(root, cfg, -1, legacyScope(root))
	res.InputFingerprint = judgeInputFingerprint(root, cfg, filepath.Join(root, "run-config.json"), filepath.Join(root, "PLAN.md"))
	code, text := verdict(res)
	publishVerdict(root, code, text, res)
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("initial judge count=%d", got)
	}

	if rc := cmdReport([]string{"-product", root, "-cached", "-informational", "-json"}); rc != 0 {
		t.Fatalf("informational cached report exit=%d", rc)
	}
	if got := readCounter(t, counter); got != 1 {
		t.Fatalf("informational cached report reran judge: %d", got)
	}
	report := buildReportMode(root, true)
	if report.JudgeCode != 1 {
		t.Fatalf("informational mode changed factual code: %#v", report)
	}
}

func TestJudgeJSONIncludesSelectorObservation(t *testing.T) {
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOENV", "off")
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module obsfixture\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixture_test.go"), []byte("package obsfixture\nimport \"testing\"\nfunc TestKnown(t *testing.T) {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "**Ц1.** selector observable\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | selected test | проверка `unit` · тест `TestKnown` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n| 1 | work | `script` | К1 |\n"
	_ = os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644)
	cfg := runConfig{
		Plan: "PLAN.md",
		Judge: judgeSpec{Checks: []checkSpec{{
			Name: "unit", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$",
		}}},
	}
	raw, _ := json.Marshal(cfg)
	_ = os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0o644)

	res := runJudge(root, cfg, -1, legacyScope(root))
	res.InputFingerprint = judgeInputFingerprint(root, cfg, filepath.Join(root, "run-config.json"), filepath.Join(root, "PLAN.md"))
	code, text := verdict(res)
	report := buildJudgeJSONReport(root, machineVerdictFromResult(root, code, text, res), res)
	if len(report.Criteria) != 1 || len(report.Criteria[0].Measures) != 1 {
		t.Fatalf("criterion observation=%#v", report.Criteria)
	}
	c := report.Criteria[0]
	m := c.Measures[0]
	if c.ID != "К1" || c.Goal != "Ц1" || c.Measure == "" || c.State != "PASS" || c.Reason == "" || c.DurationMS < 0 {
		t.Fatalf("criterion structured observation incomplete: %#v", c)
	}
	if m.Kind != "check" || m.Name != "unit" || m.Selector != "TestKnown" || m.State != "PASS" || m.Reason == "" || m.DurationMS < 0 {
		t.Fatalf("selector observation=%#v", m)
	}
	if len(report.Checks) != 1 || report.Checks[0].State != "PASS" || report.Checks[0].Reason == "" || report.Checks[0].DurationMS < 0 {
		t.Fatalf("base check timing/reason missing: %#v", report.Checks)
	}
	if report.InputFingerprint == "" {
		t.Fatal("structured judge report lacks input fingerprint")
	}
}

func TestJudgeInputFingerprintCoversChecklistAndCustomJudgeFile(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"PLAN.md":          "**Ц1.** x\n",
		"run-config.json":  "{}\n",
		"checklist.json":   "{\"items\":[]}\n",
		"custom-judge.ps1": "exit 0\n",
		"probe.ps1":        "exit 0\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cfg := runConfig{
		Plan: "PLAN.md",
		Judge: judgeSpec{
			Checklist: "checklist.json",
			Path:      "custom-judge.ps1",
			Checks:    []checkSpec{{Name: "probe", Script: "probe.ps1"}},
		},
	}
	configPath := filepath.Join(root, "run-config.json")
	planPath := filepath.Join(root, "PLAN.md")
	base := judgeInputFingerprint(root, cfg, configPath, planPath)
	for _, name := range []string{"checklist.json", "custom-judge.ps1", "probe.ps1"} {
		path := filepath.Join(root, name)
		raw, _ := os.ReadFile(path)
		if err := os.WriteFile(path, append(raw, []byte("#mutated\n")...), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := judgeInputFingerprint(root, cfg, configPath, planPath); got == base {
			t.Fatalf("%s mutation did not change fingerprint", name)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestN123NonGoCheckIdentity(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fixture requires Windows PowerShell")
	}
	for _, kind := range []string{"script", "command"} {
		t.Run(kind, func(t *testing.T) {
			root, _ := writeObservabilityFixture(t, 0)
			counter := filepath.Join(t.TempDir(), "counter.txt")
			initJudgeRefreshGitRepo(t, root)
			scope := legacyScope(root)
			run := func(arg string) judgeResult {
				var result judgeResult
				if kind == "script" {
					runScriptCheck(root, checkSpec{Script: "check.ps1", Args: []string{counter, arg}}, "probe", scope, &result)
				} else {
					runCommandCheck(root, checkSpec{Command: "powershell.exe", Args: []string{"-NoProfile", "-File", filepath.Join(root, "check.ps1"), counter, arg}}, "probe", scope, &result)
				}
				return result
			}
			assert := func(result judgeResult, count int, pass bool) {
				t.Helper()
				if got := readCounter(t, counter); got != count || (len(result.Passed) == 1) != pass || (!pass && len(result.Unknown) != 1) {
					t.Fatalf("count=%d want=%d result=%+v", got, count, result)
				}
			}
			assert(run("0"), 1, true)
			assert(run("0"), 1, false)
			assert(run("00"), 2, true)
			assert(run("00"), 2, false)
			commitJudgeRefreshFixture(t, root, "v2\n")
			assert(run("0"), 3, true)
			assert(run("0"), 3, false)
		})
	}
}

func TestN123LegacyScriptEvidenceRecovery(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fixture requires Windows PowerShell")
	}
	for _, tc := range []struct {
		name                                      string
		advance, unknown, running, tamper, report bool
		pass                                      bool
	}{
		{name: "same source"},
		{name: "changed source sealed", advance: true, pass: true},
		{name: "changed source unknown", advance: true, unknown: true},
		{name: "changed source running", advance: true, running: true},
		{name: "changed source tampered", advance: true, tamper: true},
		{name: "opposite report unknown", advance: true, unknown: true, report: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _ := writeObservabilityFixture(t, 0)
			counter := filepath.Join(t.TempDir(), "counter.txt")
			initJudgeRefreshGitRepo(t, root)
			scope := legacyScope(root)
			step := "judge"
			if tc.report {
				step = "report-judge"
			}
			cmd := newPowerShellCommand("-NoProfile", "-File", filepath.Join(root, "check.ps1"), counter, "0")
			cmd.Dir = root
			if _, err := runJudgeCheckReceipted(context.Background(), scope, "probe", cmd, tc.report); err != nil {
				t.Fatal(err)
			}
			receiptPath, outputPath := jobReceiptPaths(scope, step, "probe")
			receipt, err := readJobReceipt(receiptPath)
			if err != nil || receipt.SourceHead == "" {
				t.Fatalf("legacy receipt=%+v err=%v", receipt, err)
			}
			if tc.unknown {
				receipt.SourceHead = ""
			}
			if tc.running {
				receipt.Status = jobStatusRunning
			}
			if tc.unknown || tc.running {
				if err := writeJobReceipt(receiptPath, receipt); err != nil {
					t.Fatal(err)
				}
			}
			if tc.tamper {
				if err := os.WriteFile(outputPath, []byte("tampered"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			beforeReceipt, _ := os.ReadFile(receiptPath)
			beforeOutput, _ := os.ReadFile(outputPath)
			if tc.advance {
				commitJudgeRefreshFixture(t, root, "v2\n")
			}
			for i := 0; i < 2; i++ {
				var result judgeResult
				runScriptCheck(root, checkSpec{Script: "check.ps1", Args: []string{counter, "0"}}, "probe", scope, &result)
				wantPass := tc.pass && i == 0
				wantCount := 1
				if tc.pass {
					wantCount = 2
				}
				if got := readCounter(t, counter); got != wantCount || (len(result.Passed) == 1) != wantPass || (!wantPass && len(result.Unknown) != 1) {
					t.Fatalf("attempt=%d count=%d result=%+v", i, got, result)
				}
			}
			afterReceipt, _ := os.ReadFile(receiptPath)
			afterOutput, _ := os.ReadFile(outputPath)
			if string(beforeReceipt) != string(afterReceipt) || string(beforeOutput) != string(afterOutput) {
				t.Fatal("legacy evidence changed")
			}
		})
	}
}

func TestProvenanceFixtureChild(t *testing.T) {
	mode := os.Getenv("AW_PROVENANCE_MODE")
	if mode == "" {
		t.Skip("helper process only")
	}
	f, err := os.OpenFile(os.Getenv("AW_PROVENANCE_COUNTER"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("started\n"); err != nil {
		t.Fatal(err)
	}
	if mode == "held" {
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(os.Getenv("AW_PROVENANCE_RELEASE")); err == nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("release signal timed out")
	}
	if mode == "running" {
		time.Sleep(time.Second)
		return
	}
	if mode == "dirty" || mode == "head" {
		if err := os.WriteFile("tracked.txt", []byte("changed\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if mode == "head" {
		for _, args := range [][]string{{"add", "tracked.txt"}, {"commit", "-m", "advance"}} {
			if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
				t.Fatalf("git %v: %v: %s", args, err, out)
			}
		}
	}
}

func TestJudgeGoProvenanceTerminal(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fixture uses a copied go.exe")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	goExe := filepath.Join(t.TempDir(), "go.exe")
	if err := os.WriteFile(goExe, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"clean", "dirty-before", "dirty", "head", "missing", "unsealed", "mismatched"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".woody/\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			initJudgeRefreshGitRepo(t, root)
			counter := filepath.Join(t.TempDir(), "started")
			t.Setenv("AW_PROVENANCE_MODE", mode)
			t.Setenv("AW_PROVENANCE_COUNTER", counter)
			if mode == "dirty-before" {
				if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("dirty\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			oldWriter := writeJobReceiptFunc
			t.Cleanup(func() { writeJobReceiptFunc = oldWriter })
			writeJobReceiptFunc = func(path string, receipt *jobReceipt) error {
				if receipt.Status == jobStatusDone {
					switch mode {
					case "missing":
						return os.Remove(path)
					case "unsealed":
						receipt.OutputSHA256 = ""
					case "mismatched":
						receipt.SourceHead = strings.Repeat("0", 40)
					}
				}
				return oldWriter(path, receipt)
			}
			args := []string{"-test.run=^TestProvenanceFixtureChild$"}
			cmd := newChildCommand(goExe, args...)
			cmd.Dir = root
			operation := n123GoJudgeOperation(root, "unit", cmd)
			scope := legacyScope(root)
			receiptPath, outputPath := jobReceiptPaths(scope, "judge", operation)
			var result judgeResult
			runCommandCheck(root, checkSpec{Command: goExe, Args: args}, "unit", scope, &result)
			if mode == "clean" {
				if len(result.Passed) != 1 || len(result.Unknown) != 0 {
					t.Fatalf("clean result: %+v", result)
				}
			} else if len(result.Passed) != 0 || len(result.Failed) != 0 || len(result.Unknown) != 1 || strings.Contains(strings.ToUpper(strings.Join(result.Unknown, " ")), "RUNNING") {
				t.Errorf("terminal provenance result: %+v", result)
			}
			if mode == "dirty-before" {
				if _, err := os.Stat(counter); !os.IsNotExist(err) {
					t.Errorf("dirty source started process: %v", err)
				}
				return
			}
			if _, err := os.Stat(counter); err != nil {
				t.Fatalf("fixture did not start: %v", err)
			}
			beforeReceipt, receiptErr := os.ReadFile(receiptPath)
			beforeOutput, outputErr := os.ReadFile(outputPath)
			var again judgeResult
			runCommandCheck(root, checkSpec{Command: goExe, Args: args}, "unit", scope, &again)
			afterReceipt, receiptErr2 := os.ReadFile(receiptPath)
			afterOutput, outputErr2 := os.ReadFile(outputPath)
			if mode != "head" {
				if starts, _ := os.ReadFile(counter); string(starts) != "started\n" {
					t.Errorf("duplicate start: %q", starts)
				}
			}
			if (receiptErr == nil) != (receiptErr2 == nil) || (outputErr == nil) != (outputErr2 == nil) || string(beforeReceipt) != string(afterReceipt) || string(beforeOutput) != string(afterOutput) {
				t.Error("repeated check replaced evidence")
			}
		})
	}
}

func TestJudgeProvenanceLiveRunning(t *testing.T) {
	root := t.TempDir()
	initJudgeRefreshGitRepo(t, root)
	t.Setenv("AW_PROVENANCE_MODE", "running")
	t.Setenv("AW_PROVENANCE_COUNTER", filepath.Join(t.TempDir(), "started"))
	cmd := newChildCommand(os.Args[0], "-test.run=^TestProvenanceFixtureChild$")
	cmd.Dir = root
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := runJudgeCheckReceipted(ctx, legacyScope(root), "live", cmd, false)
	if !errors.Is(err, errJobStillRunning) {
		t.Fatalf("live result: %v", err)
	}
	var result judgeResult
	if !judgeCheckUnconfirmed("live", cmd, err, &result) || len(result.Unknown) != 1 || !strings.Contains(strings.ToUpper(result.Unknown[0]), "RUNNING") {
		t.Fatalf("live classification: %+v", result)
	}
	receipt, _ := jobReceiptPaths(legacyScope(root), "judge", "live")
	pollReceiptStatus(t, receipt, jobStatusDone, 5*time.Second)
}

func TestN123LegacyGoEvidenceRecovery(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fixture uses a copied go.exe")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	goExe := filepath.Join(t.TempDir(), "go.exe")
	if err := os.WriteFile(goExe, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name                                                       string
		advance, unknown, running, unsealed, partial, tamper, pass bool
	}{
		{name: "same source"},
		{name: "different source sealed", advance: true, pass: true},
		{name: "unknown source", advance: true, unknown: true},
		{name: "running", advance: true, running: true},
		{name: "unsealed", advance: true, unsealed: true},
		{name: "partial", advance: true, partial: true},
		{name: "tampered", advance: true, tamper: true},
	} {
		for _, legacyReport := range []bool{false, true} {
			for _, newReport := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/legacy-report=%t/new-report=%t", tc.name, legacyReport, newReport), func(t *testing.T) {
					root := t.TempDir()
					initJudgeRefreshGitRepo(t, root)
					counter := filepath.Join(t.TempDir(), "started")
					t.Setenv("AW_PROVENANCE_MODE", "clean")
					t.Setenv("AW_PROVENANCE_COUNTER", counter)
					args := []string{"-test.run=^TestProvenanceFixtureChild$"}
					cmd := newChildCommand(goExe, args...)
					cmd.Dir = root
					scope := legacyScope(root)
					if _, err := runJudgeCheckReceipted(context.Background(), scope, "unit", cmd, legacyReport); err != nil {
						t.Fatal(err)
					}
					step := "judge"
					if legacyReport {
						step = "report-judge"
					}
					receiptPath, outputPath := jobReceiptPaths(scope, step, "unit")
					receipt, err := readJobReceipt(receiptPath)
					if err != nil || receipt.SourceHead == "" {
						t.Fatalf("legacy receipt=%+v err=%v", receipt, err)
					}
					if tc.unknown {
						receipt.SourceHead = ""
					}
					if tc.running {
						receipt.Status = jobStatusRunning
					}
					if tc.unsealed {
						receipt.OutputSHA256 = ""
					}
					if tc.unknown || tc.running || tc.unsealed {
						if err := writeJobReceipt(receiptPath, receipt); err != nil {
							t.Fatal(err)
						}
					}
					if tc.partial {
						if err := os.Remove(outputPath); err != nil {
							t.Fatal(err)
						}
					}
					if tc.tamper {
						if err := os.WriteFile(outputPath, []byte("tampered"), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					beforeReceipt, _ := os.ReadFile(receiptPath)
					beforeOutput, beforeOutputErr := os.ReadFile(outputPath)
					if tc.advance {
						commitJudgeRefreshFixture(t, root, "v2\n")
					}
					for i := 0; i < 2; i++ {
						result := judgeResult{forReport: newReport}
						runCommandCheck(root, checkSpec{Command: goExe, Args: args}, "unit", scope, &result)
						wantPass := tc.pass && i == 0
						wantStarts := "started\n"
						if tc.pass {
							wantStarts += "started\n"
						}
						starts, err := os.ReadFile(counter)
						if err != nil || string(starts) != wantStarts || (len(result.Passed) == 1) != wantPass || (!wantPass && len(result.Unknown) != 1) {
							t.Fatalf("attempt=%d starts=%q err=%v result=%+v", i, starts, err, result)
						}
					}
					afterReceipt, receiptErr := os.ReadFile(receiptPath)
					afterOutput, outputErr := os.ReadFile(outputPath)
					if receiptErr != nil || string(beforeReceipt) != string(afterReceipt) || (beforeOutputErr == nil) != (outputErr == nil) || string(beforeOutput) != string(afterOutput) {
						t.Fatal("legacy evidence changed")
					}
				})
			}
		}
	}
}

func TestN123LegacyGoLiveReplayGuard(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("fixture uses a copied go.exe")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	goExe := filepath.Join(t.TempDir(), "go.exe")
	if err := os.WriteFile(goExe, binary, 0o755); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	initJudgeRefreshGitRepo(t, root)
	counter := filepath.Join(t.TempDir(), "started")
	release := filepath.Join(t.TempDir(), "release")
	t.Setenv("AW_PROVENANCE_MODE", "held")
	t.Setenv("AW_PROVENANCE_COUNTER", counter)
	t.Setenv("AW_PROVENANCE_RELEASE", release)
	args := []string{"-test.run=^TestProvenanceFixtureChild$"}
	cmd := newChildCommand(goExe, args...)
	cmd.Dir = root
	scope := legacyScope(root)
	done := make(chan error, 1)
	go func() {
		_, err := runJudgeCheckReceipted(context.Background(), scope, "unit", cmd, false)
		done <- err
	}()
	t.Cleanup(func() {
		_ = os.WriteFile(release, []byte("release"), 0o600)
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("legacy process did not finish")
		}
	})
	receiptPath, outputPath := jobReceiptPaths(scope, "judge", "unit")
	deadline := time.Now().Add(10 * time.Second)
	for {
		starts, _ := os.ReadFile(counter)
		receipt, err := readJobReceipt(receiptPath)
		if string(starts) == "started\n" && err == nil && receipt.Status == jobStatusRunning && receipt.ProcessStarted && receipt.PID > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("legacy process did not start: starts=%q receipt=%+v err=%v", starts, receipt, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	beforeReceipt, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeOutput, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, report := range []bool{false, true} {
		result := judgeResult{forReport: report}
		runCommandCheck(root, checkSpec{Command: goExe, Args: args}, "unit", scope, &result)
		if len(result.Unknown) != 1 || len(result.Passed) != 0 || len(result.Failed) != 0 {
			t.Fatalf("report=%t result=%+v", report, result)
		}
	}
	afterReceipt, receiptErr := os.ReadFile(receiptPath)
	afterOutput, outputErr := os.ReadFile(outputPath)
	starts, startsErr := os.ReadFile(counter)
	if receiptErr != nil || outputErr != nil || startsErr != nil || string(beforeReceipt) != string(afterReceipt) || string(beforeOutput) != string(afterOutput) || string(starts) != "started\n" {
		t.Fatalf("legacy evidence or starts changed: receiptErr=%v outputErr=%v starts=%q startsErr=%v", receiptErr, outputErr, starts, startsErr)
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("legacy process: %v", err)
		}
		done <- nil
	case <-time.After(10 * time.Second):
		t.Fatal("legacy process did not finish")
	}
	pollReceiptStatus(t, receiptPath, jobStatusDone, 5*time.Second)
}

func TestJudgeInvalidWindowsMutex(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows named mutex contract")
	}
	for _, report := range []bool{false, true} {
		t.Run(fmt.Sprintf("report=%t", report), func(t *testing.T) {
			root := t.TempDir()
			initJudgeRefreshGitRepo(t, root)
			scope := legacyScope(root)
			cmd := newChildCommand(os.Args[0], "-test.run=^TestProvenanceFixtureChild$")
			cmd.Dir = root
			operation := "invalid\x00mutex"
			var err error
			if report {
				_, err = runJudgeCheckReceipted(context.Background(), scope, operation, cmd, true)
			} else {
				_, err = runSourceAwareJudgeCheck(context.Background(), root, scope, operation, cmd, false)
			}
			if err == nil || !strings.Contains(err.Error(), "mutex") || cmd.Process != nil {
				t.Fatalf("err=%v process=%v", err, cmd.Process)
			}
			var result judgeResult
			if !judgeCheckUnconfirmed(operation, cmd, err, &result) || len(result.Unknown) != 1 || len(result.Passed) != 0 || len(result.Failed) != 0 {
				t.Fatalf("result=%+v", result)
			}
			if _, err := os.Stat(filepath.Join(root, ".woody", "jobs")); !os.IsNotExist(err) {
				t.Fatalf("judge evidence directory exists or cannot be inspected: %v", err)
			}
		})
	}
}
