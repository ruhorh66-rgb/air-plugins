package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func codexStreamWithTool(tool string, finalText string) string {
	return strings.Join([]string{
		`{"type":"thread.started","thread_id":"access-negative"}`,
		`{"type":"turn.started"}`,
		tool,
		fmt.Sprintf(`{"type":"item.completed","item":{"type":"agent_message","text":%q}}`, finalText),
		`{"type":"turn.completed","usage":{"input_tokens":1,"output_tokens":1}}`,
	}, "\n")
}

func TestCodexStructuredDenialsNeverBecomeModelSuccess(t *testing.T) {
	cases := []struct {
		name, item, final, subtype string
	}{
		{
			name:  "command_access_denied",
			item:  `{"type":"item.completed","item":{"type":"command_execution","command":"Get-Content TASK.md","exit_code":1,"aggregated_output":"Get-Content : Access to the path TASK.md is denied"}}`,
			final: "Done", subtype: "tool_access_denied",
		},
		{
			name:  "command_unread_task_missing",
			item:  `{"type":"item.completed","item":{"type":"command_execution","command":"Get-Content TASK.md","exit_code":1,"aggregated_output":"Cannot find path TASK.md because it does not exist"}}`,
			final: "Completed", subtype: "task_input_unavailable",
		},
		{
			name:  "mcp_permission_denied_object",
			item:  `{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"read","status":"failed","error":{"message":"Permission denied for TASK.md"}}}`,
			final: "Task done", subtype: "tool_access_denied",
		},
		{
			name:  "mcp_permission_denied_string",
			item:  `{"type":"item.completed","item":{"type":"mcp_tool_call","tool":"read","status":"failed","error":"Unauthorized: access denied"}}`,
			final: "Task done", subtype: "tool_access_denied",
		},
		{
			name:  "error_item_access_denied",
			item:  `{"type":"item.completed","item":{"type":"error","message":"Access is denied to TASK.md"}}`,
			final: "Task done", subtype: "tool_access_denied",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := parseCodexResult(codexStreamWithTool(tc.item, tc.final), nil)
			if result.Ok || result.Subtype != tc.subtype || !strings.Contains(result.Detail, "NOT_PROVEN") {
				t.Fatalf("tool denial falsely accepted: %+v", result)
			}
		})
	}
}

func TestCodexTaskUnreadableAlternativeWording(t *testing.T) {
	for _, phrase := range []string{
		"I was unable to read TASK.md. The task is not complete.",
		"Could not open the task file because access is denied.",
		"I did not read the input file; no changes made.",
		"I cannot open TASK.md: permission denied.",
	} {
		t.Run(strings.ReplaceAll(strings.ReplaceAll(phrase, " ", "_"), ".", "_"), func(t *testing.T) {
			got := parseCodexResult(codexStreamWithTool(`{"type":"item.completed","item":{"type":"reasoning"}}`, phrase), nil)
			if got.Ok || got.Subtype != "task_input_unavailable" {
				t.Fatalf("failed to detect unread task %q: %+v", phrase, got)
			}
		})
	}
}

func TestCodexSourceErrorLiteralDoesNotBlockSuccessfulModelTurn(t *testing.T) {
	raw := codexStreamWithTool(
		`{"type":"item.completed","item":{"type":"command_execution","command":"rg Access code.go","exit_code":0,"aggregated_output":"source line mentions Access is denied; this is not a failed tool call"}}`,
		"Successfully analyzed code")
	got := parseCodexResult(raw, nil)
	if !got.Ok || got.Subtype != "success" {
		t.Fatalf("source quoted an error but the tool did not fail: %+v", got)
	}
}

func TestCodexNonZeroProcessWithTurnCompletedStillFails(t *testing.T) {
	raw := codexStreamWithTool(`{"type":"item.completed","item":{"type":"agent_message","text":"Done"}}`, "Done")
	got := parseCodexResult(raw, errors.New("exit status 1"))
	if got.Ok || got.Subtype != "runner_error" {
		t.Fatalf("codex returned exit1 but was accepted: %+v", got)
	}
}

func TestWindowsCodexMultiAddDirsSameVolumeStrictSandbox(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows unelevated sandbox-specific")
	}
	root, first, second := t.TempDir(), t.TempDir(), t.TempDir()
	if err := validateCodexWorkspaceRoots(root, []string{first, second}); err != nil {
		t.Fatalf("two distinct same-volume writable roots rejected: %v", err)
	}
	args := codexArgsForSandbox(root, runnerSpec{
		Kind: "codex", Model: "gpt-6-sol", Effort: "low", AddDirs: []string{first, second},
	}, "workspace-write")
	if count := strings.Count(strings.Join(args, " "), "--add-dir"); count != 2 {
		t.Fatalf("expected exactly 2 extra roots, got args %#v", args)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "sandbox_workspace_write.exclude_slash_tmp=true") ||
		!strings.Contains(joined, "workspace-write") || strings.Contains(joined, "danger-full-access") {
		t.Fatalf("Windows safe sandbox policy lost: %#v", args)
	}
}

func TestWindowsCodexAddDirsRejectDuplicateAndParentScope(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows writable-roots scope")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "child-product")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	first := t.TempDir()
	if err := validateCodexWorkspaceRoots(root, []string{first, first}); err == nil ||
		!strings.Contains(err.Error(), "repeats") {
		t.Fatalf("duplicate writable roots should fail closed, got %v", err)
	}
	if err := validateCodexWorkspaceRoots(root, []string{parent}); err == nil ||
		!strings.Contains(err.Error(), "broaden") {
		t.Fatalf("writable ancestor would broaden product root, got %v", err)
	}
	if err := validateCodexWorkspaceRoots(root, []string{root}); err == nil ||
		!strings.Contains(err.Error(), "duplicates the workspace root") {
		t.Fatalf("redundant root must fail closed before launch, got %v", err)
	}
}

func TestWindowsCodexCrossVolumeRefusesBeforeProcessStart(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows unelevated sandbox-specific")
	}
	root := t.TempDir()
	var foreign string
	for _, candidate := range []string{`C:\Windows`, `D:\Devbox`, `R:\-4-`} {
		if _, err := os.Stat(candidate); err == nil &&
			!strings.EqualFold(filepath.VolumeName(candidate), filepath.VolumeName(root)) {
			foreign = candidate
			break
		}
	}
	if foreign == "" {
		t.Skip("No existing foreign-volume directory suitable for negative test")
	}
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	started := false
	runnerCommand = func(_ string, args ...string) *exec.Cmd {
		started = true
		return exec.Command(os.Args[0], "-test.run=TestBogus")
	}
	ctx := &loopCtx{Root: root}
	got := ctx.invokeCodex("codex", "read TASK", runnerSpec{
		Kind: "codex", Model: "gpt-6-sol", AddDirs: []string{foreign},
	}, "prelaunch")
	if started || got.Ok || got.Subtype != "invalid_runner_config" ||
		!strings.Contains(got.Detail, "different volume") {
		t.Fatalf("unsafe multi-volume launch: started=%t result=%+v", started, got)
	}
}

func TestExecutorDispatchExitCodeIsNeverModelTurnSuccess(t *testing.T) {
	turn := executorDispatchDoc{
		Schema: executorDispatchSchemaV2, ModelTurnCompleted: true,
		TaskOutcome: "MODEL_TURN_COMPLETED_TASK_NOT_VERIFIED",
	}
	if got := executorDispatchExitCode(turn); got != 2 {
		t.Fatalf("model turn without factual check must be RC2, got %d", got)
	}
	if got := executorDispatchExitCode(executorDispatchDoc{TaskOutcome: "EXECUTOR_FAILED"}); got != 1 {
		t.Fatalf("failed tool/process must be RC1, got %d", got)
	}
	// Even a forged or stale client OK cannot turn a mere model turn into
	// an unqualified task acceptance.
	turn.OK = true
	if got := executorDispatchExitCode(turn); got == 0 {
		t.Fatalf("stale OK flag was treated as factual proof")
	}
}

func TestCodexAccessDenialNeverTriggersVendorFallbackEvenWith429(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"thread.started","thread_id":"no-fallback-on-access-denial"}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"type":"command_execution","command":"Get-Content TASK.md","aggregated_output":"Access is denied to TASK.md","exit_code":1}}`,
		`{"type":"turn.failed","error":{"message":"HTTP 429 appeared in a subsequent diagnostic, not an authorized model fallback"}}`,
	}, "\n")
	got := parseCodexResult(raw, errors.New("exit status 1"))
	if got.Ok || got.Subtype != "tool_access_denied" {
		t.Fatalf("access failure must stay fail-closed; no vendor fallback: %+v", got)
	}
}
