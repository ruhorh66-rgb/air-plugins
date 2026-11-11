package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCodingWorkspaceLockProcessExit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows named mutex")
	}
	if name := os.Getenv("AW_CODING_LOCK_CHILD"); name != "" {
		lock, ok := acquireLock(name)
		if !ok || !validAutoSelfDeliveryLock(lock) {
			t.Fatal("child did not acquire mutex")
		}
		if err := os.WriteFile(os.Getenv("AW_CODING_LOCK_READY"), []byte("ready"), 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(30 * time.Second)
		lock.release()
		return
	}
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	name := lockName("coding-reserve", dir)
	cmd := exec.Command(os.Args[0], "-test.run=^TestCodingWorkspaceLockProcessExit$")
	cmd.Env = append(os.Environ(), "AW_CODING_LOCK_CHILD="+name, "AW_CODING_LOCK_READY="+ready)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child mutex not ready")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if lock, ok := acquireLock(name); ok {
		if lock != nil {
			lock.release()
		}
		t.Fatal("live child mutex was not held")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	lock, ok := acquireLock(name)
	if !ok || !validAutoSelfDeliveryLock(lock) {
		t.Fatal("mutex not released after process exit")
	}
	lock.release()
}

func TestCodingWorkspaceReservationCapacityAndIdentity(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "test-session"}}
	for i := 0; i < codingWorkspaceLimit; i++ {
		step := strings.Repeat("a", i+1)
		base, err := reserveCodingWorkspace(root, "air-worker", scope, step, "base-sha", "task-contents")
		if err != nil {
			t.Fatal(err)
		}
		raw, err := os.ReadFile(filepath.Join(base, "reservation.json"))
		if err != nil {
			t.Fatal(err)
		}
		var got codingWorkspaceReservation
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got.Step != step || got.Principal != "codex" || got.SessionKey != "test-session" || got.SourceHead != "base-sha" || got.TaskSHA256 != shaCodingBytes([]byte("task-contents")) || got.WorkerPaths[0] != filepath.Join(base, "worker-1", "air-worker") {
			t.Fatalf("incorrect reservation: %+v", got)
		}
		task, err := os.ReadFile(filepath.Join(base, "task.json"))
		if err != nil || string(task) != "task-contents" {
			t.Fatal("task was not preserved")
		}
	}
	if _, err := reserveCodingWorkspace(root, "air-worker", scope, "overflow", "base-sha", "task"); err == nil || !strings.Contains(err.Error(), "retained attempts:") {
		t.Fatalf("missing capacity rejection: %v", err)
	}
}

func TestCodingWorkspaceReservationRequiresExactlyTwoPaths(t *testing.T) {
	base := map[string]any{"schema": "air-worker.coding-workspace-reservation/v1", "step": "coord-task", "principal": "codex", "session_key": "session", "source_head": "sha", "task_sha256": "hash"}
	for _, paths := range [][]string{{}, {"one"}, {"one", "two", "three"}} {
		base["worker_paths"] = paths
		raw, err := json.Marshal(base)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeCodingWorkspaceReservation(raw); err == nil {
			t.Fatalf("accepted %d worker paths", len(paths))
		}
	}
	base["worker_paths"] = []string{"one", "two"}
	raw, _ := json.Marshal(base)
	if _, err := decodeCodingWorkspaceReservation(raw); err != nil {
		t.Fatalf("two paths rejected: %v", err)
	}
}

func TestCodingWorkspaceReservationConcurrentLastSlot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repo")
	scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "test-session"}}
	for i := 0; i < codingWorkspaceLimit-1; i++ {
		if _, err := reserveCodingWorkspace(root, ".", scope, strings.Repeat("a", i+1), "base-sha", "task"); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, step := range []string{"parallel-1", "parallel-2"} {
		wg.Add(1)
		go func(step string) {
			defer wg.Done()
			<-start
			_, err := reserveCodingWorkspace(root, ".", scope, step, "base-sha", "task")
			results <- err
		}(step)
	}
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected one last-slot reservation, got %d", successes)
	}
}

func TestCodingWorkspaceReservationConcurrentProcesses(t *testing.T) {
	if root := os.Getenv("AW_RESERVATION_CHILD_ROOT"); root != "" {
		gate := os.Getenv("AW_RESERVATION_GATE")
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := os.Stat(gate); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("gate timeout")
			}
			time.Sleep(10 * time.Millisecond)
		}
		scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "test-session"}}
		_, err := reserveCodingWorkspace(root, ".", scope, os.Getenv("AW_RESERVATION_STEP"), "base-sha", "task")
		result := "rejected"
		if err == nil {
			result = "accepted"
		}
		if err := os.WriteFile(os.Getenv("AW_RESERVATION_RESULT"), []byte(result), 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	work := t.TempDir()
	root := filepath.Join(work, "repo")
	scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "test-session"}}
	for i := 0; i < codingWorkspaceLimit-1; i++ {
		if _, err := reserveCodingWorkspace(root, ".", scope, strings.Repeat("a", i+1), "base-sha", "task"); err != nil {
			t.Fatal(err)
		}
	}
	gate := filepath.Join(work, "gate")
	cmds := make([]*exec.Cmd, 2)
	for i, step := range []string{"process-one", "process-two"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCodingWorkspaceReservationConcurrentProcesses$")
		cmd.Env = append(os.Environ(), "AW_RESERVATION_CHILD_ROOT="+root, "AW_RESERVATION_GATE="+gate, "AW_RESERVATION_STEP="+step, "AW_RESERVATION_RESULT="+filepath.Join(work, step))
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		cmds[i] = cmd
	}
	if err := os.WriteFile(gate, []byte("go"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	accepted := 0
	for _, step := range []string{"process-one", "process-two"} {
		raw, err := os.ReadFile(filepath.Join(work, step))
		if err != nil {
			t.Fatal(err)
		}
		if string(raw) == "accepted" {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("expected one accepted cross-process reservation, got %d", accepted)
	}
}
func TestCodingWorkspaceReservationInterruptedAndDuplicate(t *testing.T) {
	work := t.TempDir()
	root := filepath.Join(work, "repo")
	scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "session"}}
	parent := filepath.Join(work, ".airworker-coding-workspaces", "repo")
	if err := os.MkdirAll(filepath.Join(parent, ".reserve-lock"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := reserveCodingWorkspace(root, ".", scope, "step", "base", "first"); err == nil || !strings.Contains(err.Error(), "busy or interrupted") {
		t.Fatalf("interrupted lock was not held: %v", err)
	}
	if err := os.Remove(filepath.Join(parent, ".reserve-lock")); err != nil {
		t.Fatal(err)
	}
	base, err := reserveCodingWorkspace(root, ".", scope, "step", "base", "first")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reserveCodingWorkspace(root, ".", scope, "step", "base", "different"); err == nil {
		t.Fatal("duplicate reservation accepted")
	}
	raw, err := os.ReadFile(filepath.Join(base, "task.json"))
	if err != nil || string(raw) != "first" {
		t.Fatal("original task was overwritten")
	}
}

func TestCodingWorkspaceReservationRejectsForeignNamespace(t *testing.T) {
	work := t.TempDir()
	root := filepath.Join(work, "repo")
	foreign := filepath.Join(work, ".airworker-coding-workspaces")
	if err := os.WriteFile(foreign, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "session"}}
	if _, err := reserveCodingWorkspace(root, ".", scope, "step", "base", "task"); err == nil {
		t.Fatal("foreign namespace accepted")
	}
	raw, err := os.ReadFile(foreign)
	if err != nil || string(raw) != "foreign" {
		t.Fatal("foreign data changed")
	}
}
func TestCodingWorkspaceReservationRejectsForeignLink(t *testing.T) {
	work := t.TempDir()
	root := filepath.Join(work, "repo")
	foreign := filepath.Join(t.TempDir(), "outside")
	if err := os.Mkdir(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(foreign, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(foreign, filepath.Join(work, ".airworker-coding-workspaces")); err != nil {
		t.Skipf("environment cannot create a test symlink without privilege: %v", err)
	}
	scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "session"}}
	if _, err := reserveCodingWorkspace(root, ".", scope, "step", "base", "task"); err == nil {
		t.Fatal("foreign link accepted")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "preserve" {
		t.Fatal("foreign data changed")
	}
	if _, err := os.Stat(filepath.Join(foreign, "repo")); !os.IsNotExist(err) {
		t.Fatalf("created data in foreign directory: %v", err)
	}
}

func TestCodingWorkspaceReservationRejectsForeignJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction")
	}
	work, foreign := t.TempDir(), t.TempDir()
	sentinel := filepath.Join(foreign, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(work, ".airworker-coding-workspaces")
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; New-Item -ItemType Junction -Path $env:AW_TEST_LINK -Target $env:AW_TEST_TARGET | Out-Null")
	cmd.Env = append(os.Environ(), "AW_TEST_LINK="+link, "AW_TEST_TARGET="+foreign)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cannot create junction in test directory: %v %s", err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "junction-test"}}
	if _, err := reserveCodingWorkspace(filepath.Join(work, "repo"), ".", scope, "step", "base", "task"); err == nil {
		t.Fatal("foreign junction accepted")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "preserve" {
		t.Fatal("foreign junction target changed")
	}
}
