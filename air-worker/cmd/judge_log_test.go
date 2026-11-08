//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestJudgeCheckLogWriteFailure(t *testing.T) {
	cases := []struct {
		name string
		run  func(string, string, sessionScope, *judgeResult)
	}{
		{
			name: "script",
			run: func(root, log string, scope sessionScope, result *judgeResult) {
				script := filepath.Join(root, "check.ps1")
				if err := os.WriteFile(script, []byte("Write-Output 'ok'; exit 0"), 0o600); err != nil {
					t.Fatal(err)
				}
				runScriptCheck(root, checkSpec{Script: "check.ps1", Log: log}, "check", scope, result)
			},
		},
		{
			name: "command",
			run: func(root, log string, scope sessionScope, result *judgeResult) {
				runCommandCheck(root, checkSpec{Command: "cmd.exe", Args: []string{"/c", "echo ok"}, Log: log}, "check", scope, result)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+" no configured log", func(t *testing.T) {
			root := t.TempDir()
			var result judgeResult
			tc.run(root, "", legacyScope(root), &result)
			if code, _ := verdict(result); code != 0 || len(result.Passed) != 1 {
				t.Fatalf("no-log check did not pass: %+v", result)
			}
		})
		t.Run(tc.name+" successful child", func(t *testing.T) {
			root := t.TempDir()
			log := filepath.Join("logs", "check.log")
			if err := os.MkdirAll(filepath.Join(root, "logs", "check.log"), 0o700); err != nil {
				t.Fatal(err)
			}
			var result judgeResult
			tc.run(root, log, legacyScope(root), &result)
			if len(result.Passed) != 0 || len(result.Unknown) != 1 || len(result.Failed) != 0 {
				t.Fatalf("successful child with failed log write: passed=%v unknown=%v failed=%v", result.Passed, result.Unknown, result.Failed)
			}
			if code, _ := verdict(result); code != 2 {
				t.Fatalf("log failure verdict = %d, want UNKNOWN", code)
			}
		})
	}

	t.Run("command failing child remains failed", func(t *testing.T) {
		root := t.TempDir()
		log := filepath.Join("logs", "check.log")
		if err := os.MkdirAll(filepath.Join(root, "logs", "check.log"), 0o700); err != nil {
			t.Fatal(err)
		}
		var result judgeResult
		runCommandCheck(root, checkSpec{Command: "cmd.exe", Args: []string{"/c", "exit 7"}, Log: log}, "check", legacyScope(root), &result)
		if len(result.Passed) != 0 || len(result.Unknown) != 0 || len(result.Failed) != 1 {
			t.Fatalf("nonzero child classification changed: passed=%v unknown=%v failed=%v", result.Passed, result.Unknown, result.Failed)
		}
		if code, _ := verdict(result); code != 1 {
			t.Fatalf("failing child verdict = %d, want FAIL", code)
		}
		if !strings.Contains(result.Failed[0], "лог") {
			t.Fatalf("log failure omitted: %v", result.Failed)
		}
	})

	t.Run("exit 9009 stays unknown with log error", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "blocked"), 0700); err != nil {
			t.Fatal(err)
		}
		var result judgeResult
		runCommandCheck(root, checkSpec{Command: "cmd.exe", Args: []string{"/c", "exit 9009"}, Log: "blocked"}, "check", legacyScope(root), &result)
		if len(result.Passed) != 0 || len(result.Failed) != 0 || len(result.Unknown) != 1 {
			t.Fatalf("exit 9009 classification: %+v", result)
		}
		if code, _ := verdict(result); code != 2 {
			t.Fatalf("exit 9009 verdict = %d, want UNKNOWN", code)
		}
		if !strings.Contains(result.Unknown[0], "9009") || !strings.Contains(result.Unknown[0], "лог") {
			t.Fatalf("exit 9009 or log diagnostic omitted: %v", result.Unknown)
		}
	})

	t.Run("script failing child remains failed", func(t *testing.T) {
		root := t.TempDir()
		if err := os.Mkdir(filepath.Join(root, "blocked"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "fail.ps1"), []byte("Write-Output 'bad'; exit 7"), 0600); err != nil {
			t.Fatal(err)
		}
		var result judgeResult
		runScriptCheck(root, checkSpec{Script: "fail.ps1", Log: "blocked"}, "check", legacyScope(root), &result)
		if len(result.Passed) != 0 || len(result.Unknown) != 0 || len(result.Failed) != 1 {
			t.Fatalf("script failure classification: %+v", result)
		}
		if code, _ := verdict(result); code != 1 {
			t.Fatalf("failing script verdict = %d, want FAIL", code)
		}
		if !strings.Contains(result.Failed[0], "лог") {
			t.Fatalf("log failure omitted: %v", result.Failed)
		}
	})

	for _, tc := range cases {
		t.Run(tc.name+" writable log", func(t *testing.T) {
			root := t.TempDir()
			log := filepath.Join("logs", "check.log")
			if err := os.MkdirAll(filepath.Join(root, "logs"), 0o700); err != nil {
				t.Fatal(err)
			}
			var result judgeResult
			tc.run(root, log, legacyScope(root), &result)
			contents, err := os.ReadFile(filepath.Join(root, log))
			if err != nil {
				t.Fatalf("read configured log: %v", err)
			}
			if len(contents) == 0 {
				t.Fatal("configured log is empty")
			}
			if len(result.Passed) != 1 || len(result.Unknown) != 0 || len(result.Failed) != 0 {
				t.Fatalf("successful child with writable log: passed=%v unknown=%v failed=%v", result.Passed, result.Unknown, result.Failed)
			}
			if code, _ := verdict(result); code != 0 {
				t.Fatalf("writable log verdict = %d, want PASS", code)
			}
		})
	}
}
