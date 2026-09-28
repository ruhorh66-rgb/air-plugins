package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initJudgeRefreshGitRepo(t *testing.T, root string) {
	t.Helper()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	run("init")
	run("config", "user.email", "air-worker-test@example.invalid")
	run("config", "user.name", "AirWorker Test")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "fixture")
}

func commitJudgeRefreshFixture(t *testing.T, root, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "add", "tracked.txt")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	cmd = exec.Command("git", "commit", "-m", "advance")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %v: %s", err, out)
	}
}

func TestEnsureFreshMachineVerdictRefreshesMissingReusesFreshAndTracksHead(t *testing.T) {
	root := seedPlanNodeProduct(t)
	initJudgeRefreshGitRepo(t, root)

	first, err := ensureFreshMachineVerdict(root)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Refreshed || first.Code == 2 || first.GitHead != currentGitHead(root) {
		t.Fatalf("first refresh=%#v", first)
	}
	second, err := ensureFreshMachineVerdict(root)
	if err != nil {
		t.Fatal(err)
	}
	if second.Refreshed || second.Reason != "fresh" || second.Code != first.Code {
		t.Fatalf("fresh verdict reran: %#v", second)
	}

	commitJudgeRefreshFixture(t, root, "v2\n")
	third, err := ensureFreshMachineVerdict(root)
	if err != nil {
		t.Fatal(err)
	}
	if !third.Refreshed || !strings.Contains(third.Reason, "current HEAD") || third.GitHead != currentGitHead(root) {
		t.Fatalf("HEAD change did not refresh: %#v", third)
	}
}

func TestPostToolUseGitCommitRefreshesJudge(t *testing.T) {
	root := seedPlanNodeProduct(t)
	session := "post-commit-refresh"
	hookTestStateForPrincipal(t, "chatgpt", session, root)
	body, _ := json.Marshal(map[string]string{"command": "git commit -m done"})
	res, err := handlePostToolUse(hookInput{
		Principal: "chatgpt", SessionID: session, HookEventName: "PostToolUse",
		ToolName: "PowerShell", ToolInput: body,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Context, "AIRWORKER FACTUAL JUDGE: refreshed=true code=") || strings.Contains(res.Context, "code=2") {
		t.Fatalf("commit did not refresh factual judge: %#v", res)
	}
	if _, err := os.Stat(filepath.Join(root, ".goal-verdict.json")); err != nil {
		t.Fatalf("commit refresh did not publish verdict: %v", err)
	}
}

func TestCuratorTickRefreshesJudgeAndFailsClosedWhenUnmeasurable(t *testing.T) {
	root := seedPlanNodeProduct(t)
	code, first := captureLoopOutput(t, func() int {
		return cmdCuratorTick([]string{"-product", root, "-json"})
	})
	if code != 0 || !strings.Contains(first, `"judge":{"schema":"air-worker.judge.refresh/v1","refreshed":true`) {
		t.Fatalf("measurable tick did not refresh: code=%d out=%s", code, first)
	}
	code, second := captureLoopOutput(t, func() int {
		return cmdCuratorTick([]string{"-product", root, "-json"})
	})
	if code != 0 || !strings.Contains(second, `"refreshed":false,"reason":"fresh","code":1`) {
		t.Fatalf("fresh tick reran judge: code=%d out=%s", code, second)
	}

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "PLAN.md"), []byte("# plan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out := captureLoopOutput(t, func() int {
		return cmdCuratorTick([]string{"-product", bad, "-json"})
	})
	if code != 3 || !strings.Contains(out, `"code":2`) || !strings.Contains(out, "run-config unavailable") {
		t.Fatalf("unmeasurable tick not fail-closed: code=%d out=%s", code, out)
	}
}
