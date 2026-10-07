//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLearningProcessTreeHelper(t *testing.T) {
	mode := os.Getenv("AW_LEARNING_TREE_HELPER")
	if mode == "" {
		t.Skip("helper process only")
	}
	switch mode {
	case "child":
		time.Sleep(30 * time.Second)
		if done := os.Getenv("AW_LEARNING_CHILD_DONE"); done != "" {
			_ = os.WriteFile(done, []byte("unexpected-completion"), 0o600)
		}
	case "parent":
		exe, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		child := exec.Command(exe, "-test.run=^TestLearningProcessTreeHelper$")
		child.Env = append(os.Environ(),
			"AW_LEARNING_TREE_HELPER=child",
			"AW_LEARNING_CHILD_DONE="+os.Getenv("AW_LEARNING_CHILD_DONE"),
		)
		if err := child.Start(); err != nil {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Getenv("AW_LEARNING_CHILD_PID"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
			os.Exit(2)
		}
		time.Sleep(30 * time.Second)
	default:
		os.Exit(2)
	}
}

func TestTerminateLearningProcessTreeKillsDescendants(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	pidPath := filepath.Join(dir, "child.pid")
	donePath := filepath.Join(dir, "child.done")
	cmd := exec.Command(exe, "-test.run=^TestLearningProcessTreeHelper$")
	cmd.Env = append(os.Environ(),
		"AW_LEARNING_TREE_HELPER=parent",
		"AW_LEARNING_CHILD_PID="+pidPath,
		"AW_LEARNING_CHILD_DONE="+donePath,
	)
	prepareLearningProcessTree(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var childPID string
	for time.Now().Before(deadline) {
		b, readErr := os.ReadFile(pidPath)
		if readErr == nil && strings.TrimSpace(string(b)) != "" {
			childPID = strings.TrimSpace(string(b))
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if childPID == "" {
		_ = terminateLearningProcessTree(cmd)
		t.Fatal("helper child PID was not published")
	}
	if err := terminateLearningProcessTree(cmd); err != nil {
		t.Fatal(err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	select {
	case <-waitCh:
	case <-time.After(5 * time.Second):
		t.Fatal("learning adapter parent survived process-tree termination")
	}
	time.Sleep(150 * time.Millisecond)
	out, err := exec.Command("tasklist.exe", "/FI", "PID eq "+childPID, "/FO", "CSV", "/NH").CombinedOutput()
	if err != nil {
		t.Fatalf("tasklist child pid %s: %v %s", childPID, err, out)
	}
	if strings.Contains(string(out), "\""+childPID+"\"") {
		t.Fatalf("learning adapter descendant survived timeout cleanup: pid=%s output=%s", childPID, out)
	}
	if _, err := os.Stat(donePath); !os.IsNotExist(err) {
		t.Fatalf("killed descendant completed after cleanup: %v", err)
	}
}
