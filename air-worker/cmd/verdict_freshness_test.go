package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initFreshnessGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
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
	run("add", "tracked.txt")
	run("commit", "-m", "fixture")
	return root
}

func TestMachineVerdictFreshnessRejectsOlderThanSixHours(t *testing.T) {
	now := time.Now()
	mv := machineVerdict{At: now.Add(-machineVerdictMaxAge - time.Second).Format(time.RFC3339Nano)}
	err := machineVerdictFreshness(t.TempDir(), mv, now)
	if err == nil || !strings.Contains(err.Error(), "age") {
		t.Fatalf("verdict older than six hours must be stale: %v", err)
	}
}

func TestMachineVerdictFreshnessRequiresCurrentGitHead(t *testing.T) {
	root := initFreshnessGitRepo(t)
	now := time.Now()
	head := currentGitHead(root)
	if head == "" {
		t.Fatal("fixture has no git HEAD")
	}

	fresh := machineVerdict{At: now.Format(time.RFC3339Nano), GitHead: head}
	if err := machineVerdictFreshness(root, fresh, now); err != nil {
		t.Fatalf("current HEAD verdict rejected: %v", err)
	}

	missing := fresh
	missing.GitHead = ""
	if err := machineVerdictFreshness(root, missing, now); err == nil || !strings.Contains(err.Error(), "no git_head") {
		t.Fatalf("git verdict without head must be stale: %v", err)
	}

	wrong := fresh
	wrong.GitHead = strings.Repeat("a", 40)
	if err := machineVerdictFreshness(root, wrong, now); err == nil || !strings.Contains(err.Error(), "current HEAD") {
		t.Fatalf("mismatched git head must be stale: %v", err)
	}
}

func TestMachineVerdictFreshnessAcceptsLegacyLocalTimestamp(t *testing.T) {
	now := time.Now()
	mv := machineVerdict{At: now.Add(-time.Hour).Format("2006-01-02T15:04:05")}
	if err := machineVerdictFreshness(t.TempDir(), mv, now); err != nil {
		t.Fatalf("legacy local timestamp within six hours must remain readable: %v", err)
	}
}
