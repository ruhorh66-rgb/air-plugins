package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

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
