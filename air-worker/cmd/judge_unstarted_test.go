//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestJudgeReportCrossOperationEvidence(t *testing.T) {
	for _, firstReport := range []bool{false, true} {
		for _, evidence := range []string{"running", "output-only", "receipt-only", "tampered-output", "invalid-receipt", "sealed"} {
			name := fmt.Sprintf("report-first=%t/%s", firstReport, evidence)
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				scope := newScope(root, sessionIdentity{"gpt", "cross-operation"})
				operation := "Проверка Иванова Ивана, отчёт с пробелами"
				firstStep, secondStep := "judge", "report-judge"
				if firstReport {
					firstStep, secondStep = secondStep, firstStep
				}
				firstReceipt, firstOutput := jobReceiptPaths(scope, firstStep, operation)
				secondReceipt, secondOutput := jobReceiptPaths(scope, secondStep, operation)
				if firstReceipt == secondReceipt || firstOutput == secondOutput {
					t.Fatal("opposite operations share evidence paths")
				}

				firstMarker := filepath.Join(root, "first-child")
				t.Setenv("AW_N123_CHILD_MARKER", firstMarker)
				firstCmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
				if evidence == "running" {
					firstCmd = slowHelperCmd(t, 500)
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
					defer cancel()
					if _, err := runJudgeCheckReceipted(ctx, scope, operation, firstCmd, firstReport); err != errJobStillRunning {
						t.Fatalf("interrupted first operation: %v", err)
					}
					pollReceiptStatus(t, firstReceipt, jobStatusRunning, time.Second)
				} else if _, err := runJudgeCheckReceipted(context.Background(), scope, operation, firstCmd, firstReport); err != nil {
					t.Fatalf("first operation: %v", err)
				}

				switch evidence {
				case "output-only":
					if err := os.Remove(firstReceipt); err != nil {
						t.Fatal(err)
					}
				case "receipt-only":
					if err := os.Remove(firstOutput); err != nil {
						t.Fatal(err)
					}
				case "tampered-output":
					if err := os.WriteFile(firstOutput, []byte("tampered"), 0600); err != nil {
						t.Fatal(err)
					}
				case "invalid-receipt":
					if err := os.WriteFile(firstReceipt, []byte("{invalid"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				beforeReceipt, receiptErr := os.ReadFile(firstReceipt)
				beforeOutput, outputErr := os.ReadFile(firstOutput)
				secondMarker := filepath.Join(root, "second-child")
				t.Setenv("AW_N123_CHILD_MARKER", secondMarker)
				secondCmd := func() *exec.Cmd { return exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$") }
				_, err := runJudgeCheckReceipted(context.Background(), scope, operation, secondCmd(), !firstReport)
				if evidence == "sealed" {
					if err != nil {
						t.Fatalf("sealed opposite refused: %v", err)
					}
					first, err := readJobReceipt(firstReceipt)
					if err != nil {
						t.Fatal(err)
					}
					second, err := readJobReceipt(secondReceipt)
					if err != nil {
						t.Fatal(err)
					}
					if first.JobID == second.JobID || first.Step != firstStep || second.Step != secondStep || first.Status != jobStatusDone || second.Status != jobStatusDone || first.SourceHead != second.SourceHead || first.ArgsSHA256 == "" || second.ArgsSHA256 == "" || first.OutputSHA256 == "" || second.OutputSHA256 == "" {
						t.Fatalf("distinct sealed identities missing: first=%+v second=%+v", first, second)
					}
					sealedReceipt, err := os.ReadFile(secondReceipt)
					if err != nil {
						t.Fatal(err)
					}
					sealedOutput, err := os.ReadFile(secondOutput)
					if err != nil {
						t.Fatal(err)
					}
					if _, err := runJudgeCheckReceipted(context.Background(), scope, operation, secondCmd(), !firstReport); err == nil {
						t.Fatal("same operation replay accepted")
					}
					if data, err := os.ReadFile(secondReceipt); err != nil || string(data) != string(sealedReceipt) {
						t.Fatalf("replay changed receipt: %v", err)
					}
					if data, err := os.ReadFile(secondOutput); err != nil || string(data) != string(sealedOutput) {
						t.Fatalf("replay changed output: %v", err)
					}
					if data, err := os.ReadFile(secondMarker); err != nil || string(data) != "started" {
						t.Fatalf("second child: %q, %v", data, err)
					}
					return
				}
				if err == nil {
					t.Fatal("unsafe opposite evidence accepted")
				}
				if _, err := os.Stat(secondMarker); !os.IsNotExist(err) {
					t.Fatalf("refused child started: %v", err)
				}
				if _, err := os.Stat(secondReceipt); !os.IsNotExist(err) {
					t.Fatalf("refusal created receipt: %v", err)
				}
				if _, err := os.Stat(secondOutput); !os.IsNotExist(err) {
					t.Fatalf("refusal created output: %v", err)
				}
				if evidence == "running" {
					original, err := readJobReceipt(firstReceipt)
					if err != nil {
						t.Fatal(err)
					}
					pollReceiptStatus(t, firstReceipt, jobStatusDone, 3*time.Second)
					finished, err := readJobReceipt(firstReceipt)
					if err != nil || finished.JobID != original.JobID || finished.Step != firstStep || finished.OutputSHA256 == "" {
						t.Fatalf("original child did not seal same job: %+v, %v", finished, err)
					}
					if _, err := inspectOwnedJob(scope, firstStep, operation); err != nil {
						t.Fatalf("original seal invalid: %v", err)
					}
					return
				}
				afterReceipt, afterReceiptErr := os.ReadFile(firstReceipt)
				afterOutput, afterOutputErr := os.ReadFile(firstOutput)
				if string(beforeReceipt) != string(afterReceipt) || string(beforeOutput) != string(afterOutput) || (receiptErr == nil) != (afterReceiptErr == nil) || (outputErr == nil) != (afterOutputErr == nil) {
					t.Fatal("refusal changed opposite evidence")
				}
			})
		}
	}
}

func TestLegacyNonGoConcurrentCrossModeRefusal(t *testing.T) {
	for _, git := range []bool{false, true} {
		for _, firstReport := range []bool{false, true} {
			t.Run(fmt.Sprintf("git=%t/report-first=%t", git, firstReport), func(t *testing.T) {
				root := t.TempDir()
				if git {
					initJudgeRefreshGitRepo(t, root)
				}
				scope := legacyScope(root)
				const operation = "non-go-concurrent"
				firstStep, secondStep := "judge", "report-judge"
				if firstReport {
					firstStep, secondStep = secondStep, firstStep
				}
				firstReceipt, firstOutput := jobReceiptPaths(scope, firstStep, operation)
				secondReceipt, secondOutput := jobReceiptPaths(scope, secondStep, operation)
				started := filepath.Join(t.TempDir(), "started")
				release := filepath.Join(t.TempDir(), "release")
				cmd := exec.Command(os.Args[0], "-test.run=^TestJudgeCrossFenceChild$")
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "AW_JUDGE_STARTED="+started, "AW_JUDGE_RELEASE="+release)
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				result := make(chan error, 1)
				go func() {
					_, err := runJudgeCheckReceipted(ctx, scope, operation, cmd, firstReport)
					result <- err
				}()
				t.Cleanup(func() {
					_ = os.WriteFile(release, nil, 0o600)
					select {
					case <-result:
					case <-time.After(10 * time.Second):
						t.Error("first check did not finish")
					}
				})
				pollJudgeMarker(t, started, 5*time.Second)
				initial := pollReceiptStatus(t, firstReceipt, jobStatusRunning, 5*time.Second)
				beforeReceipt, err := os.ReadFile(firstReceipt)
				if err != nil {
					t.Fatal(err)
				}
				beforeOutput, err := os.ReadFile(firstOutput)
				if err != nil {
					t.Fatal(err)
				}
				secondMarker := filepath.Join(t.TempDir(), "second-started")
				second := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
				second.Dir = root
				second.Env = append(os.Environ(), "AW_N123_CHILD_MARKER="+secondMarker)
				if _, err := runSourceAwareJudgeCheck(ctx, root, scope, operation, second, !firstReport); err == nil {
					t.Fatal("concurrent opposite check was accepted")
				}
				for _, path := range []string{secondReceipt, secondOutput, secondMarker} {
					if _, err := os.Lstat(path); !os.IsNotExist(err) {
						t.Fatalf("refused check created %s: %v", path, err)
					}
				}
				for path, before := range map[string][]byte{firstReceipt: beforeReceipt, firstOutput: beforeOutput} {
					if after, err := os.ReadFile(path); err != nil || string(after) != string(before) {
						t.Fatalf("refusal changed %s: %v", path, err)
					}
				}
				if err := os.WriteFile(release, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				finished := pollReceiptStatus(t, firstReceipt, jobStatusDone, 5*time.Second)
				if finished.JobID != initial.JobID || finished.Step != firstStep || finished.OutputSHA256 == "" || (finished.SourceHead != "") != git {
					t.Fatalf("original receipt changed identity: initial=%+v finished=%+v", initial, finished)
				}
				if data, err := os.ReadFile(firstOutput); err != nil || string(data) != jobReceiptStdoutMarker {
					t.Fatalf("original output: %q, %v", data, err)
				}
			})
		}
	}
}
