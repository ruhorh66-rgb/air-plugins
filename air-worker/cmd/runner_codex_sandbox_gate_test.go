package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodexWindowsSandboxExcludesUnexpectedSlashTmp(t *testing.T) {
	args := strings.Join(codexArgsForSandbox("D:\\task", runnerSpec{Model: "gpt-6-sol", Effort: "low"}, "workspace-write"), " ")
	got := strings.Contains(args, "sandbox_workspace_write.exclude_slash_tmp=true")
	if runtime.GOOS == "windows" && !got {
		t.Fatalf("Windows workspace-write must restrict unexpected /tmp root: %s", args)
	}
	if runtime.GOOS != "windows" && got {
		t.Fatalf("non-Windows workspace sandbox changed: %s", args)
	}
	if !strings.Contains(args, "workspace-write") || strings.Contains(args, "danger-full-access") {
		t.Fatalf("sandbox bypass or mode changed: %s", args)
	}
	ro := strings.Join(codexArgsForSandbox("D:\\task", runnerSpec{Model: "gpt-6-sol"}, "read-only"), " ")
	if strings.Contains(ro, "exclude_slash_tmp") {
		t.Fatalf("read-only config unexpectedly modified: %s", ro)
	}
}

func TestCodexResultRejectsSandboxDeniedWithTurnCompleted(t *testing.T) {
	raw := strings.Join([]string{
		"2026-11-01 ERROR codex_core::tools::router: error=exec_command failed: CreateProcess { message: \"UnsupportedOperation(\\\"windows unelevated restricted-token sandbox cannot enforce split writable root sets directly; refusing to run unsandboxed\\\")\" }",
		"{\"type\":\"thread.started\",\"thread_id\":\"vera-repro\"}",
		"{\"type\":\"turn.started\"}",
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"I couldn't read TASK.md, so I made no product changes. Skipped: all implementation and checks.\"}}",
		"{\"type\":\"turn.completed\",\"usage\":{\"input_tokens\":10,\"output_tokens\":10}}",
	}, "\n")
	r := parseCodexResult(raw, nil)
	if r.Ok || r.Subtype != "sandbox_unavailable" || !strings.Contains(r.Detail, "NOT_PROVEN") {
		t.Fatalf("sandbox refusal became completed task: %+v", r)
	}
}

func TestCodexResultRejectsUnreadableTaskWithoutSandboxLine(t *testing.T) {
	raw := strings.Join([]string{
		"{\"type\":\"turn.started\"}",
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"I couldn't read TASK.md; task not completed.\"}}",
		"{\"type\":\"turn.completed\"}",
	}, "\n")
	r := parseCodexResult(raw, nil)
	if r.Ok || r.Subtype != "task_input_unavailable" {
		t.Fatalf("denied task accepted: %+v", r)
	}
}

func TestCodexResultRejectsNonzeroExitDespiteTurnCompleted(t *testing.T) {
	raw := "{\"type\":\"turn.completed\"}"
	r := parseCodexResult(raw, errors.New("exit status 1"))
	if r.Ok || r.Subtype != "runner_error" {
		t.Fatalf("exit status swallowed: %+v", r)
	}
}

func TestCodexResultNormalCompletedTurnStillNotTaskProof(t *testing.T) {
	raw := strings.Join([]string{
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"Output completed.\"}}",
		"{\"type\":\"turn.completed\"}",
	}, "\n")
	r := parseCodexResult(raw, nil)
	if !r.Ok || r.Subtype != "success" {
		t.Fatalf("model protocol success blocked: %+v", r)
	}
	receiptPath := filepath.Join(t.TempDir(), "job.receipt.json")
	receipt := &jobReceipt{Status: jobStatusDone, TaskOutcome: "NOT_VERIFIED"}
	if err := writeJobReceipt(receiptPath, receipt); err != nil {
		t.Fatal(err)
	}
	if err := recordCodexModelOutcome(receiptPath, r); err != nil {
		t.Fatal(err)
	}
	got, err := readJobReceipt(receiptPath)
	if err != nil || got.Status != jobStatusDone || got.TaskOutcome != "MODEL_TURN_COMPLETED_TASK_NOT_VERIFIED" {
		t.Fatalf("model turn silently accepted as factual task completion: %+v %v", got, err)
	}
}

func TestCodexReceiptMarksDeniedTaskFailedEvenWithProcessDone(t *testing.T) {
	receiptPath := filepath.Join(t.TempDir(), "job.receipt.json")
	code := 0
	r := &jobReceipt{Status: jobStatusDone, ProcessExitCode: &code, TaskOutcome: "NOT_VERIFIED"}
	if err := writeJobReceipt(receiptPath, r); err != nil {
		t.Fatal(err)
	}
	if err := recordCodexModelOutcome(receiptPath, stepResult{Subtype: "sandbox_unavailable"}); err != nil {
		t.Fatal(err)
	}
	got, err := readJobReceipt(receiptPath)
	if err != nil || got.Status != jobStatusFailed || got.TaskOutcome != "EXECUTOR_FAILED" || got.ProcessExitCode == nil || *got.ProcessExitCode != 0 {
		t.Fatalf("process exit0 / task denial conflated: %+v %v", got, err)
	}
}

func TestCodexRealDeniedOutputFiles(t *testing.T) {
	path := os.Getenv("AW_CODEX_REPRO_OUT")
	if path == "" {
		t.Skip("set AW_CODEX_REPRO_OUT to preserved Vera or Vega exact output for read-only native replay")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	r := parseCodexResult(string(raw), nil)
	if r.Ok || r.Subtype != "sandbox_unavailable" {
		t.Fatalf("real denied Codex output falsely accepted: %+v", r)
	}
}
