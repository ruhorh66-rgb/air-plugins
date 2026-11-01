package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCmdExecutorDispatchReturnsRC2AfterCompletedUnverifiedModelTurn(t *testing.T) {
	oldCommand := runnerCommand
	oldPonytail := loadPonytailSkill
	t.Cleanup(func() {
		runnerCommand = oldCommand
		loadPonytailSkill = oldPonytail
	})
	t.Setenv("AW_EXECUTOR_HELPER", "1")
	loadPonytailSkill = func() (ponytailSkill, error) {
		return ponytailSkill{Version: "4.10.0", Path: "fixture/SKILL.md", Body: "VENDOR_SKILL_BODY"}, nil
	}
	runnerCommand = func(_ string, args ...string) *exec.Cmd {
		helper := append([]string{"-test.run=TestExecutorHelperProcess", "--"}, args...)
		return exec.Command(os.Args[0], helper...)
	}
	root := t.TempDir()
	stateDir := t.TempDir()
	task := filepath.Join(root, "TASK.md")
	if err := os.WriteFile(task, []byte("Return EXECUTOR_OK after reading TASK.md"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := executorTestConfig()
	bytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), bytes, 0o644); err != nil {
		t.Fatal(err)
	}
	id := sessionIdentity{Principal: "chatgpt", SessionKey: "rc2-task-not-proven"}
	if err := writeStateJSON(sessionModePath(stateDir, id), sessionModeState{
		Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(stateDir, id), sessionProductState{
		Path: root, Principal: id.Principal, SessionKey: id.SessionKey,
	}); err != nil {
		t.Fatal(err)
	}
	code := cmdExecutorDispatch([]string{
		"-product", root, "-principal", id.Principal, "-session-key", id.SessionKey,
		"-state-dir", stateDir, "-task-file", task, "-tier", "gpt6-sol:low", "-json",
	})
	if code != 2 {
		t.Fatalf("completed model turn without independent factual task proof must return RC2, got %d", code)
	}
	operation := runnerReceiptOperation("executor-codex", runnerSpec{
		Kind: "codex", Model: "gpt-fallback-2", Effort: "low",
	}, 0)
	receiptPath, _ := jobReceiptPaths(newScope(root, id), "executor-TASK.md", operation)
	receipt, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != jobStatusDone || receipt.TaskOutcome != "MODEL_TURN_COMPLETED_TASK_NOT_VERIFIED" ||
		receipt.ProcessExitCode == nil || *receipt.ProcessExitCode != 0 {
		t.Fatalf("process outcome should be distinct from unverified task: %+v", receipt)
	}
}
