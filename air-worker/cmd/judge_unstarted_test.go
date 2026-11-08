//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestJudgeRealLaunchFailurePreservesLog(t *testing.T) {
	root := t.TempDir()
	bad := filepath.Join(root, "invalid.exe")
	if err := os.WriteFile(bad, []byte("not a valid executable"), 0600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(root, "check.log")
	if err := os.WriteFile(logPath, []byte("previous log"), 0600); err != nil {
		t.Fatal(err)
	}
	scope := legacyScope(root)
	var result judgeResult
	runCommandCheck(root, checkSpec{Command: bad, Log: "check.log"}, "launch-failure", scope, &result)
	if code, _ := verdict(result); code != 2 || len(result.Unknown) != 1 || len(result.Failed) != 0 {
		t.Fatalf("launch failure classification: %+v", result)
	}
	if !strings.Contains(result.Unknown[0], "не запущена") {
		t.Fatalf("missing launch-failure reason: %v", result.Unknown)
	}
	after, err := os.ReadFile(logPath)
	if err != nil || string(after) != "previous log" {
		t.Fatalf("previous log changed: %q, %v", after, err)
	}
	path, _ := jobReceiptPaths(scope, "judge", "launch-failure")
	receipt, err := readJobReceipt(path)
	if err != nil || receipt.ProcessStarted {
		t.Fatalf("unexpected process start: %+v, %v", receipt, err)
	}
}

func TestJudgeStillRunningDoesNotClaimNeverStarted(t *testing.T) {
	cmd := exec.Command("unused")
	var result judgeResult
	if !judgeCheckUnconfirmed("check", cmd, fmt.Errorf("transport: %w", errJobStillRunning), &result) {
		t.Fatal("running job not classified")
	}
	if len(result.Unknown) != 1 || strings.Contains(result.Unknown[0], "не запущена") || !strings.Contains(result.Unknown[0], "квитанцию") {
		t.Fatalf("misleading running-job diagnostic: %+v", result)
	}
}

func TestJudgeDuplicateRefusalIsUnknownWithoutRerun(t *testing.T) {
	cases := []struct {
		name  string
		setup func(string, string) (checkSpec, func(string, checkSpec, sessionScope, *judgeResult))
	}{
		{
			name: "script",
			setup: func(root, marker string) (checkSpec, func(string, checkSpec, sessionScope, *judgeResult)) {
				script := "$marker = '" + strings.ReplaceAll(marker, "'", "''") + "'; Add-Content -LiteralPath $marker -Value 'ran'; Write-Output 'script output'; exit 0"
				if err := os.WriteFile(filepath.Join(root, "check.ps1"), []byte(script), 0o600); err != nil {
					t.Fatal(err)
				}
				return checkSpec{Script: "check.ps1", Log: "check.log"}, func(root string, chk checkSpec, scope sessionScope, result *judgeResult) {
					runScriptCheck(root, chk, "duplicate-check", scope, result)
				}
			},
		},
		{
			name: "command",
			setup: func(root, marker string) (checkSpec, func(string, checkSpec, sessionScope, *judgeResult)) {
				args := []string{"/c", "echo ran>>runs.txt & echo command output"}
				return checkSpec{Command: "cmd.exe", Args: args, Log: "check.log"}, func(root string, chk checkSpec, scope sessionScope, result *judgeResult) {
					runCommandCheck(root, chk, "duplicate-check", scope, result)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			marker := filepath.Join(root, "runs.txt")
			chk, run := tc.setup(root, marker)
			scope := legacyScope(root)

			first := judgeResult{}
			run(root, chk, scope, &first)
			if code, _ := verdict(first); code != 0 || len(first.Passed) != 1 || len(first.Failed) != 0 || len(first.Unknown) != 0 {
				t.Fatalf("first execution did not pass: code=%d passed=%v failed=%v unknown=%v", code, first.Passed, first.Failed, first.Unknown)
			}
			logPath := filepath.Join(root, chk.Log)
			before, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read first execution log: %v", err)
			}
			if len(before) == 0 {
				t.Fatal("first execution wrote an empty log")
			}

			second := judgeResult{}
			run(root, chk, scope, &second)
			if code, _ := verdict(second); code != 2 || len(second.Unknown) != 1 || len(second.Passed) != 0 || len(second.Failed) != 0 {
				t.Fatalf("duplicate refusal was not UNKNOWN: code=%d passed=%v failed=%v unknown=%v", code, second.Passed, second.Failed, second.Unknown)
			}
			if !strings.Contains(second.Unknown[0], "existing job evidence") {
				t.Fatalf("refusal reason omitted existing job evidence: %v", second.Unknown)
			}
			after, err := os.ReadFile(logPath)
			if err != nil {
				t.Fatalf("read log after duplicate refusal: %v", err)
			}
			if string(after) != string(before) {
				t.Fatalf("duplicate refusal changed log: before=%q after=%q", before, after)
			}
			runs, err := os.ReadFile(marker)
			if err != nil {
				t.Fatalf("read execution marker: %v", err)
			}
			if lines := strings.Fields(string(runs)); len(lines) != 1 || lines[0] != "ran" {
				t.Fatalf("child execution count = %q, want exactly one run", runs)
			}
		})
	}
}
