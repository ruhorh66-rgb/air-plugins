//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestRouterProcessHelper is run only in a subprocess of these tests.
func TestRouterProcessHelper(t *testing.T) {
	if len(os.Args) < 5 || os.Args[1] != "-test.run=^TestRouterProcessHelper$" {
		return
	}
	mode, dir := os.Args[3], os.Args[4]
	if mode == "parent" {
		deadline := time.Now().Add(15 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(dir, "start")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(2)
			}
			time.Sleep(10 * time.Millisecond)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestRouterProcessHelper$", "--", "child", dir)
		if err := child.Start(); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
	if mode == "child" {
		if err := os.WriteFile(filepath.Join(dir, "pid"), []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
			os.Exit(4)
		}
		time.Sleep(30 * time.Second)
		os.Exit(0)
	}
	os.Exit(5)
}

func routerTestCommand(mode, dir string) *exec.Cmd {
	return exec.Command(os.Args[0], "-test.run=^TestRouterProcessHelper$", "--", mode, dir)
}

func routerTestWait(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("parent failed: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("parent did not exit")
	}
}

func routerTestChildHandle(t *testing.T, dir string) syscall.Handle {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(dir, "pid"))
		if err == nil {
			pid, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 32)
			if err == nil && pid != 0 {
				h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
				if err != nil {
					t.Fatalf("open child process: %v", err)
				}
				t.Cleanup(func() { syscall.CloseHandle(h) })
				return h
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("child did not report its PID")
	return 0
}

func routerTestWaitStatus(t *testing.T, h syscall.Handle, timeout uint32, want uint32) {
	t.Helper()
	got, err := syscall.WaitForSingleObject(h, timeout)
	if err != nil || got != want {
		t.Fatalf("process wait = %d, %v; want %d", got, err, want)
	}
}

func TestRouterOwnedTreeStopsChildAfterParentExit(t *testing.T) {
	dir := t.TempDir()
	cmd := routerTestCommand("parent", dir)
	tree, err := startRouterOwnedTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopRouterOwnedTree(tree) })
	if err := os.WriteFile(filepath.Join(dir, "start"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	child := routerTestChildHandle(t, dir)
	routerTestWait(t, cmd)
	routerTestWaitStatus(t, child, 0, syscall.WAIT_TIMEOUT)
	if err := stopRouterOwnedTree(tree); err != nil {
		t.Fatal(err)
	}
	routerTestWaitStatus(t, child, 5000, syscall.WAIT_OBJECT_0)
}

func TestRouterOwnedTreeStartStop(t *testing.T) {
	dir := t.TempDir()
	cmd := routerTestCommand("child", dir)
	tree, err := startRouterOwnedTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopRouterOwnedTree(tree) })
	child := routerTestChildHandle(t, dir)
	routerTestWaitStatus(t, child, 0, syscall.WAIT_TIMEOUT)
	if err := stopRouterOwnedTree(tree); err != nil {
		t.Fatal(err)
	}
	routerTestWaitStatus(t, child, 5000, syscall.WAIT_OBJECT_0)
	_ = cmd.Wait()
}

func TestRouterOwnedTreeInvalidExecutable(t *testing.T) {
	cmd := exec.Command(filepath.Join(t.TempDir(), "missing.exe"))
	tree, err := startRouterOwnedTree(cmd)
	if tree != nil {
		defer stopRouterOwnedTree(tree)
		t.Fatal("invalid executable returned a tree")
	}
	if err == nil || cmd.Process != nil {
		t.Fatalf("invalid executable: err = %v, process = %v", err, cmd.Process)
	}
}

func TestRouterOwnedTreeKillsOnLastHandleClose(t *testing.T) {
	dir := t.TempDir()
	cmd := routerTestCommand("child", dir)
	tree, err := startRouterOwnedTree(cmd)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stopRouterOwnedTree(tree); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	child := routerTestChildHandle(t, dir)
	routerTestWaitStatus(t, child, 0, syscall.WAIT_TIMEOUT)
	tree.mu.Lock()
	err = syscall.CloseHandle(tree.job)
	if err == nil {
		tree.job = 0
	}
	tree.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	routerTestWaitStatus(t, child, 5000, syscall.WAIT_OBJECT_0)
}
