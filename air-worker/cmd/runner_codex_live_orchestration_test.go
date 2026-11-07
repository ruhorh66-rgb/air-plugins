package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Opt-in live acceptance of the LPR-approved two-Codex coding-subagent route.
// Never runs in normal QA. No shared source tree is written by the models.
// Native receipts remain in the owned D: fixture for independent readback.
func TestDevWork30LiveTwoCodexPatchAuthors(t *testing.T) {
	if os.Getenv("AIR_WORKER_LIVE_TWO_CODEX") != "1" {
		t.Skip("live two-Codex verification requires exact opt-in")
	}
	root := strings.TrimSpace(os.Getenv("AIR_WORKER_LIVE_FIXTURE_ROOT"))
	if root == "" || !filepath.IsAbs(root) {
		t.Fatal("exact absolute OWN D: fixture root required")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("live fixture root already exists or unreadable; no overwrite: %v", err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"go.mod":    "module example.com/airworker-orch-live\n\ngo 1.22\n",
		"demo.go":   "package demo\n\nfunc Answer() string { return \"OLD\" }\n",
		"README.md": "Synthetic two-Codex coding-subagent fixture. Only change demo.go. Preserve all job receipts.\n",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ctx := loopCtx{Root: root, Orchestrate: true, Subagents: 2,
		Principal: "gpt", SessionKey: "devwork30-native-two-codex-20261107"}
	runner := runnerSpec{Kind: "codex", Model: "gpt-6.1-terra", Effort: "low"}
	task := "Read demo.go and README.md first. Task: change only demo.go so Answer() returns NEW rather than OLD. " +
		"Exactly TWO Codex coding subagents must independently author explicit unified diff candidates for demo.go with diff --git / --- a/ / +++ b/ / @@. " +
		"They must not directly write shared files. The Codex leader must compare both patch candidates, apply the bounded implementation in demo.go, and verify file content. Do not alter any other files."
	out := ctx.invokeCodexOrchestrated("codex", task, runner, "devwork30-native-two-codex")
	if !out.Ok || out.AgentRequested != 2 || out.AgentStarted != 2 || out.AgentCompleted != 2 ||
		out.AgentAborted != 0 || out.AgentOrphan != 0 || out.AgentLive != 0 || out.AgentIssue != "" {
		t.Fatalf("two coding patch authors not accepted: %+v", out)
	}
	after, err := os.ReadFile(filepath.Join(root, "demo.go"))
	if err != nil || !strings.Contains(string(after), "return \"NEW\"") {
		t.Fatalf("single leader shared-tree code edit not proven: content=%q err=%v", after, err)
	}
	t.Logf("LIVE_TWO_CODEX_PASS: requested=%d started=%d completed=%d leader_edited=true own_receipts=%s",
		out.AgentRequested, out.AgentStarted, out.AgentCompleted, filepath.Join(root, ".woody", "jobs"))
}
