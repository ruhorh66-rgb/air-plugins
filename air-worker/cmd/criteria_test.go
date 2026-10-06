package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCriterionK33SelectorGrammar(t *testing.T) {
	m, err := parseCriterionMeasures("проверка `тесты бинарника` · тест `TestX`")
	if err != nil || len(m) != 1 || m[0].Selector != "TestX" {
		t.Fatalf("selector grammar: %+v, %v", m, err)
	}
	if _, err := parseCriterionMeasures("проверка `тесты бинарника` — пояснение прозой"); err == nil {
		t.Fatal("prose in machine measure must be rejected")
	}
}

func TestCriterionK33MissingSelectorFails(t *testing.T) {
	chk := checkSpec{Name: "go", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$"}
	r := runSelectedCheck(".", chk, "TestDefinitelyNotWrittenForAirWorker")
	if r.State != measureFail {
		t.Fatalf("missing test must be factual fail: %+v", r)
	}
}

func TestPowerShellSelectorParameterReachesScript(t *testing.T) {
	shell := "powershell.exe"
	if runtime.GOOS != "windows" {
		shell = "pwsh"
	}
	if _, err := exec.LookPath(shell); err != nil {
		t.Skip(shell + " is unavailable")
	}

	root := t.TempDir()
	script := "param([string]$Select)\nif ($Select -eq 'RunnerWrites' -or $Select -eq '-RunnerWrites') { exit 0 }\nexit 9\n"
	if err := os.WriteFile(filepath.Join(root, "selector.ps1"), append(utf8BOM, []byte(script)...), 0o644); err != nil {
		t.Fatal(err)
	}
	chk := checkSpec{Name: "probe", Script: "selector.ps1", Select: "-Select {}"}
	for _, selector := range []string{"RunnerWrites", "-RunnerWrites"} {
		result := runSelectedCheck(root, chk, selector)
		if result.State != measurePass {
			t.Fatalf("PowerShell selector %q was corrupted: %+v", selector, result)
		}
	}
}
func TestCriterionK19ConfirmedClosure(t *testing.T) {
	root := t.TempDir()
	plan := filepath.Join(root, "PLAN.md")
	text := "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| ~~1~~ | done | `sonnet` | К1: factual |\n" +
		"| ~~2~~ | gate | — | гейт: ЛПР |\n"
	if err := os.WriteFile(plan, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := confirmedClosedSteps(plan, nil); got != 1 {
		t.Fatalf("unconfirmed work step counted closed: %d", got)
	}
	if got := confirmedClosedSteps(plan, []string{"К1"}); got != 2 {
		t.Fatalf("confirmed closure not counted: %d", got)
	}
}

func TestCriterionJudgeAffectsVerdictAndDistance(t *testing.T) {
	r := judgeResult{Passed: []string{"base"}, CriteriaFailed: []string{"К1 — missing"}}
	if code, _ := verdict(r); code != 1 {
		t.Fatalf("criterion fail code=%d", code)
	}
	if d := distanceOf(1, r); d == nil || *d != 1 {
		t.Fatalf("criterion distance=%v", d)
	}
	r = judgeResult{CriteriaUnknown: []string{"К1 — unknown"}}
	if code, _ := verdict(r); code != 2 {
		t.Fatalf("criterion unknown code=%d", code)
	}
}

func TestJudgeSelectorInventoryBatchesOneGoListPerCheck(t *testing.T) {
	old := judgeGoListSelectors
	defer func() { judgeGoListSelectors = old }()
	calls := 0
	judgeGoListSelectors = func(root string, chk checkSpec, selectors []string) (map[string]bool, error) {
		calls++
		if chk.Name != "unit" || len(selectors) != 2 {
			t.Fatalf("inventory request=%s %#v", chk.Name, selectors)
		}
		return map[string]bool{"TestOne": true, "TestTwo": true}, nil
	}
	cfg := runConfig{Judge: judgeSpec{Checks: []checkSpec{{
		Name: "unit", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$",
	}}}}
	g := planGoals{Criteria: []planCriterion{
		{ID: "К1", Goal: "Ц1", Measure: "проверка `unit` · тест `TestOne`"},
		{ID: "К2", Goal: "Ц1", Measure: "проверка `unit` · тест `TestTwo`"},
	}}
	inventory := buildJudgeSelectorInventory(".", cfg, g)
	if calls != 1 {
		t.Fatalf("go selector inventory calls=%d want 1", calls)
	}
	base := judgeResult{Passed: []string{"unit"}, SelectorInventory: inventory}
	passed, failed, gated, unknown, _ := evaluatePlanCriteriaObserved(".", cfg, g, base)
	if len(passed) != 2 || len(failed) != 0 || len(gated) != 0 || len(unknown) != 0 {
		t.Fatalf("criteria pass=%v fail=%v gated=%v unknown=%v", passed, failed, gated, unknown)
	}
}

func TestJudgeSelectorInventoryMarksMissingWithoutPerSelectorProbe(t *testing.T) {
	old := judgeGoListSelectors
	defer func() { judgeGoListSelectors = old }()
	calls := 0
	judgeGoListSelectors = func(root string, chk checkSpec, selectors []string) (map[string]bool, error) {
		calls++
		return map[string]bool{"TestOne": true}, nil
	}
	cfg := runConfig{Judge: judgeSpec{Checks: []checkSpec{{
		Name: "unit", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$",
	}}}}
	g := planGoals{Criteria: []planCriterion{
		{ID: "К1", Goal: "Ц1", Measure: "проверка `unit` · тест `TestOne`"},
		{ID: "К2", Goal: "Ц1", Measure: "проверка `unit` · тест `TestMissing`"},
	}}
	inventory := buildJudgeSelectorInventory(".", cfg, g)
	base := judgeResult{Passed: []string{"unit"}, SelectorInventory: inventory}
	passed, failed, _, unknown, _ := evaluatePlanCriteriaObserved(".", cfg, g, base)
	if calls != 1 || len(passed) != 1 || len(failed) != 1 || len(unknown) != 0 {
		t.Fatalf("calls=%d pass=%v fail=%v unknown=%v", calls, passed, failed, unknown)
	}
}

func TestFilteredGoCheckCannotLendPassToExcludedSelector(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module filtered\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testSrc := "package filtered\nimport \"testing\"\nfunc TestSafe(t *testing.T) {}\nfunc TestBad(t *testing.T) { t.Fatal(\"must fail when selected\") }\n"
	if err := os.WriteFile(filepath.Join(root, "filtered_test.go"), []byte(testSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	chk := checkSpec{
		Name: "unit", Command: "go",
		Args:   []string{"test", ".", "-run", "^TestSafe$"},
		Select: "-run ^{}$",
	}
	base := judgeResult{
		Passed: []string{"unit"},
		SelectorInventory: map[string]map[string]measureResult{
			"unit": {"TestBad": {State: measurePass, Detail: "TestBad"}},
		},
	}
	got := runSelectedCheckWithInventory(root, chk, "TestBad", base)
	if got.State != measureFail {
		t.Fatalf("filtered base check must execute excluded selector instead of inheriting PASS: %+v", got)
	}
}

func TestGoExeUsesSameSelectorInventoryAsGo(t *testing.T) {
	chk := checkSpec{Name: "unit", Command: `C:\\tools\\go.exe`, Args: []string{"test", "."}, Select: "-run ^{}$"}
	if !isGoCommand(chk.Command) {
		t.Fatalf("go.exe path was not normalized as Go command: %q", chk.Command)
	}
	base := judgeResult{
		Passed: []string{"unit"},
		SelectorInventory: map[string]map[string]measureResult{
			"unit": {"TestMissing": {State: measureFail, Detail: "missing"}},
		},
	}
	got := runSelectedCheckWithInventory(".", chk, "TestMissing", base)
	if got.State != measureFail {
		t.Fatalf("go.exe missing selector must fail from shared inventory: %+v", got)
	}
}

func TestGoCheckRunsAllTestsRejectsSelectionFlags(t *testing.T) {
	full := checkSpec{Command: "go", Args: []string{"-C", "cmd", "test", "./...", "-count=1"}}
	if !goCheckRunsAllTests(full) {
		t.Fatal("canonical full go test must be reusable")
	}
	for _, args := range [][]string{
		{"test", ".", "-run", "^TestSafe$"},
		{"test", ".", "-run=^TestSafe$"},
		{"test", ".", "-skip", "Slow"},
		{"test", ".", "-list", "Test"},
		{"test", ".", "-short"},
		{"test", ".", "-args", "-test.run=TestSafe"},
	} {
		if goCheckRunsAllTests(checkSpec{Command: "go.exe", Args: args}) {
			t.Fatalf("filtered/suppressed Go check incorrectly treated as full: %#v", args)
		}
	}
}

func writeSuppressedGoSelectorFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module suppressed\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := "package suppressed\nimport \"testing\"\nfunc TestSafe(t *testing.T) {}\nfunc TestBad(t *testing.T) { if testing.Short() { return }; t.Fatal(\"selected TestBad executed\") }\n"
	if err := os.WriteFile(filepath.Join(root, "suppressed_test.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func selectorInventoryPass(name string) judgeResult {
	return judgeResult{
		Passed: []string{"unit"},
		SelectorInventory: map[string]map[string]measureResult{
			"unit": {name: {State: measurePass, Detail: name}},
		},
	}
}

func TestSelectedGoExecutionRemovesSuppressingFlags(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"double-dash-run", []string{"test", ".", "--run=^TestSafe$"}},
		{"list", []string{"test", ".", "-list", "Test"}},
		{"test-list", []string{"test", ".", "-test.list", "Test"}},
		{"skip", []string{"test", ".", "-skip", "TestBad"}},
		{"test-skip", []string{"test", ".", "-test.skip", "TestBad"}},
		{"short", []string{"test", ".", "-short"}},
		{"count-zero", []string{"test", ".", "-count=0"}},
		{"count-zero-octal-spelling", []string{"test", ".", "-count=00"}},
		{"count-zero-hex-spelling", []string{"test", ".", "-count=0x0"}},
		{"test-count-zero", []string{"test", ".", "-test.count", "0"}},
		{"test-count-zero-octal-spelling", []string{"test", ".", "-test.count=00"}},
		{"test-count-zero-hex-spelling", []string{"test", ".", "-test.count=0x0"}},
		{"test-run", []string{"test", ".", "-test.run", "^TestSafe$"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeSuppressedGoSelectorFixture(t)
			chk := checkSpec{Name: "unit", Command: "go", Args: tc.args, Select: "-run ^{}$"}
			got := runSelectedCheckWithInventory(root, chk, "TestBad", selectorInventoryPass("TestBad"))
			if got.State != measureFail {
				t.Fatalf("suppressing args %#v hid selected failing test: %+v", tc.args, got)
			}
		})
	}
}

func TestGoFlagsCannotLendOrSuppressSelectedPass(t *testing.T) {
	root := writeSuppressedGoSelectorFixture(t)
	t.Setenv("GOFLAGS", "-run=^TestSafe$")
	chk := checkSpec{Name: "unit", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$"}
	if goCheckRunsAllTests(chk) {
		t.Fatal("inherited GOFLAGS made filtered check look unrestricted")
	}
	got := runSelectedCheckWithInventory(root, chk, "TestBad", selectorInventoryPass("TestBad"))
	if got.State != measureUnknown || !strings.Contains(got.Detail, "GOFLAGS") {
		t.Fatalf("inherited GOFLAGS must fail selected execution closed: %+v", got)
	}

	chk.Env = map[string]string{"GOFLAGS": ""}
	chk.Args = []string{"test", ".", "-run", "^TestSafe$"}
	got = runSelectedCheckWithInventory(root, chk, "TestBad", selectorInventoryPass("TestBad"))
	if got.State != measureFail {
		t.Fatalf("explicit empty GOFLAGS plus filtered base must run selected failing test: %+v", got)
	}
}

func TestGoCheckRunsAllTestsRejectsDoubleDashAndZeroCount(t *testing.T) {
	for _, args := range [][]string{
		{"test", ".", "--run=^TestSafe$"},
		{"test", ".", "--skip=TestBad"},
		{"test", ".", "--list=Test"},
		{"test", ".", "-count=0"},
		{"test", ".", "-count=00"},
		{"test", ".", "-count=0x0"},
		{"test", ".", "-count", "0"},
		{"test", ".", "-test.count=0"},
		{"test", ".", "-test.count=00"},
		{"test", ".", "-test.count=0x0"},
	} {
		if goCheckRunsAllTests(checkSpec{Command: "go.exe", Args: args, Env: map[string]string{"GOFLAGS": ""}}) {
			t.Fatalf("suppressed Go invocation incorrectly reusable: %#v", args)
		}
	}
}

func TestGoEnvConfigFlagsCannotLendOrSuppressSelectedPass(t *testing.T) {
	root := writeSuppressedGoSelectorFixture(t)
	envFile := filepath.Join(t.TempDir(), "goenv")
	if err := os.WriteFile(envFile, []byte("GOFLAGS=-skip=TestBad\\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldFlags, hadFlags := os.LookupEnv("GOFLAGS")
	if err := os.Unsetenv("GOFLAGS"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadFlags {
			_ = os.Setenv("GOFLAGS", oldFlags)
		} else {
			_ = os.Unsetenv("GOFLAGS")
		}
	})
	t.Setenv("GOENV", envFile)

	chk := checkSpec{Name: "unit", Command: "go", Args: []string{"test", "."}, Select: "-run ^{}$"}
	flags, err := effectiveGoFlags(root, chk)
	if err != nil {
		t.Fatalf("effective GOFLAGS: %v", err)
	}
	if !strings.Contains(flags, "-skip=TestBad") {
		t.Fatalf("GOENV-configured GOFLAGS not observed: %q", flags)
	}
	if covered, err := goCheckRunsAllTestsAt(root, chk); err != nil || covered {
		t.Fatalf("GOENV-filtered base check must not be reusable: covered=%v err=%v flags=%q", covered, err, flags)
	}
	got := runSelectedCheckWithInventory(root, chk, "TestBad", selectorInventoryPass("TestBad"))
	if got.State != measureUnknown || !strings.Contains(got.Detail, "GOFLAGS") {
		t.Fatalf("GOENV-filtered selected execution must fail closed: %+v", got)
	}
}

func TestSelectedGoArgsRejectArgsTerminator(t *testing.T) {
	chk := checkSpec{Name: "unit", Command: "go", Args: []string{"test", ".", "-args", "-test.run=TestSafe"}, Select: "-run ^{}$", Env: map[string]string{"GOFLAGS": ""}}
	if _, err := selectedGoCheck(chk, "TestBad"); err == nil || !strings.Contains(err.Error(), "-args") {
		t.Fatalf("-args selected execution must be UNKNOWN, err=%v", err)
	}
}
