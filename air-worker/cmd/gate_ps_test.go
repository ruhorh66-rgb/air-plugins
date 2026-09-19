package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoopGateParsingFailClosed(t *testing.T) {
	plan := writeLoopPlan(t, "| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n"+
		"| ~~1~~ | valid closed | — | гейт: ЛПР |\n"+
		"| ~~2~~ | malformed closure | LPR??? | awaiting approval |\n"+
		"| 3 | valid open | — | гейт: ЛПР |\n"+
		"| 4 | must not run | script | К1 |\n")
	steps := readPlanSteps(plan)
	if len(steps) != 4 {
		t.Fatalf("malformed numbered gate row disappeared: %+v", steps)
	}
	if !steps[0].Gate || !steps[0].Done {
		t.Fatalf("valid closed gate grammar changed: %+v", steps[0])
	}
	if !steps[1].Gate || steps[1].Done || steps[1].Num != "2" || !strings.Contains(steps[1].Title, "некорректный гейт") {
		t.Fatalf("malformed gate must become an open named barrier: %+v", steps[1])
	}
	if !steps[2].Gate || steps[2].Done {
		t.Fatalf("valid open gate grammar changed: %+v", steps[2])
	}
	if gate, state := firstOpenPlanStep(steps); state != openPlanGate || gate.Num != "2" {
		t.Fatalf("parser mutation let work cross malformed gate: %+v (%v)", gate, state)
	}
}

func captureLoopOutput(t *testing.T, run func() int) (int, string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	code := run()
	_ = w.Close()
	os.Stdout = old
	var out bytes.Buffer
	_, _ = out.ReadFrom(r)
	_ = r.Close()
	return code, out.String()
}

func TestLoopOpenGateExactStop(t *testing.T) {
	for _, command := range []struct {
		name string
		run  func([]string) int
	}{{"loop", cmdLoop}, {"orchestrate", cmdOrchestrate}} {
		t.Run(command.name, func(t *testing.T) {
			dir := t.TempDir()
			called := filepath.Join(dir, "CALLED")
			quotedCalled := strings.ReplaceAll(called, "'", "''")
			judge := "Set-Content -LiteralPath '" + quotedCalled + "' -Value judge; exit 1\n"
			if err := os.WriteFile(filepath.Join(dir, "judge.ps1"), []byte(judge), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg := `{"judge":{"path":"judge.ps1"},"ladder":["script"]}`
			if err := os.WriteFile(filepath.Join(dir, "run-config.json"), []byte(cfg), 0o644); err != nil {
				t.Fatal(err)
			}
			plan := "**Ц1.** release waits for approval\n\n" +
				"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
				"| К1 | Ц1 | approval recorded | факт `f01` |\n\n" +
				"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
				"| 7 | production release | — | гейт: ЛПР |\n" +
				"| 8 | forbidden call :: Set-Content -LiteralPath '" + quotedCalled + "' -Value script | script | К1 |\n"
			if err := os.WriteFile(filepath.Join(dir, "PLAN.md"), []byte(plan), 0o644); err != nil {
				t.Fatal(err)
			}
			code, out := captureLoopOutput(t, func() int { return command.run([]string{"-product", dir}) })
			want := "ЖДЁТ ЛПР: гейт 7 «7. production release»" + lineEnding
			if code != 3 || out != want {
				t.Fatalf("gate result = code %d, %q; want code 3, %q", code, out, want)
			}
			if _, err := os.Stat(called); !os.IsNotExist(err) {
				t.Fatalf("judge/model/script call occurred after open gate: %v", err)
			}
		})
	}
}

func TestPowerShellGuardMutationProofAndPwshUnchanged(t *testing.T) {
	input := []string{`Path=C:\bin`, `PSModulePath=C:\Program Files\PowerShell\7\Modules;C:\custom\Modules`}
	if runtime.GOOS == "windows" {
		guarded := configuredChildEnvironment(`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, input)
		if strings.Contains(strings.ToLower(strings.Join(guarded, "\n")), `\powershell\7`) {
			t.Fatalf("mutation proof: powershell.exe retained PowerShell 7 modules: %#v", guarded)
		}
	}
	untouched := configuredChildEnvironment("pwsh", input)
	if strings.Join(untouched, "\x00") != strings.Join(input, "\x00") {
		t.Fatalf("pwsh environment changed: %#v", untouched)
	}
}

func TestPowerShellGuardAllCallSites(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows PowerShell 5.1 guard")
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe unavailable")
	}
	t.Setenv("PSModulePath", `C:\Program Files\PowerShell\7\Modules;C:\Windows\System32\WindowsPowerShell\v1.0\Modules`)
	root := t.TempDir()
	probe := `if ($env:PSModulePath -like '*\PowerShell\7*') { exit 91 }; exit 0`
	if got := (&loopCtx{Root: root}).runScriptStep(workStep{Cmd: probe}); !got.Ok {
		t.Fatalf("script step bypassed PowerShell guard: %+v", got)
	}
	path := filepath.Join(root, "probe.ps1")
	if err := os.WriteFile(path, []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	var scriptResult judgeResult
	runScriptCheck(root, checkSpec{Script: "probe.ps1"}, "script", legacyScope(root), &scriptResult)
	if len(scriptResult.Passed) != 1 {
		t.Fatalf("judge script bypassed PowerShell guard: %+v", scriptResult)
	}
	var commandResult judgeResult
	runCommandCheck(root, checkSpec{
		Command: "powershell.exe",
		Args:    []string{"-NoProfile", "-Command", probe},
		Env:     map[string]string{"PSModulePath": `C:\Program Files\PowerShell\7\Modules`},
	}, "command", legacyScope(root), &commandResult)
	if len(commandResult.Passed) != 1 {
		t.Fatalf("configured command bypassed PowerShell guard: %+v", commandResult)
	}
	c := &loopCtx{Root: root, JudgePath: path}
	if code, _, _ := c.judgeDetailed(); code != 0 {
		t.Fatalf("configured judge path bypassed PowerShell guard: code %d", code)
	}
}

func TestWindowsPowerShellGetFileHash(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Get-FileHash integration requires Windows PowerShell")
	}
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skip("powershell.exe unavailable")
	}
	t.Setenv("PSModulePath", `C:\Program Files\PowerShell\7\Modules;C:\Windows\System32\WindowsPowerShell\v1.0\Modules`)
	cmd := newPowerShellCommand("-NoProfile", "-Command", "(Get-FileHash -LiteralPath $env:ComSpec -Algorithm SHA256).Hash")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Get-FileHash through guarded PowerShell 5.1 failed: %v: %s", err, out)
	}
	hash := strings.TrimSpace(string(out))
	if len(hash) != 64 {
		t.Fatalf("unexpected SHA-256 %q", hash)
	}
}
