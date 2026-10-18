//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// This is a disposable native PLAN projection. The primitive is source-only:
// no actual product PLAN/N/LEARN, GitHub ref or installed binary is touched.
func n124ProjectionHoldFixture(t *testing.T) (string, []string, string) {
	t.Helper()
	root, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"plan", "plan/event-outbox", "learn"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{
		"PLAN.md",
		"plan/N-119_base.md",
		"plan/event-outbox/operation.json",
		"learn/events.jsonl",
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("fixed original "+name+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	declared, err := json.Marshal(map[string]string{
		"schema": "air-worker.plan.authority/v1",
		"mode":   "isolated-fixture",
		"root":   root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan-authority.json"), declared, 0600); err != nil {
		t.Fatal(err)
	}
	names = append(names, "plan-authority.json")
	foreign := filepath.Join(root, "foreign.txt")
	if err := os.WriteFile(foreign, []byte("independent foreign WIP\n"), 0600); err != nil {
		t.Fatal(err)
	}
	return root, names, foreign
}

func TestN124ProjectionHoldRejectsNativeConcurrentWrites(t *testing.T) {
	root, names, foreign := n124ProjectionHoldFixture(t)
	original := make(map[string][]byte, len(names))
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		original[name] = raw
	}
	hold, err := holdN124ReplayProjection(root)
	if err != nil {
		t.Fatalf("cannot pin native fixture projection: %v", err)
	}
	defer hold.Close()
	if err := hold.Verify(); err != nil {
		t.Fatalf("native file generation could not be verified: %v", err)
	}
	for _, name := range names {
		t.Run("write-"+filepath.Base(name), func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(name))
			if err := os.WriteFile(path, []byte("malicious writer\n"), 0600); err == nil {
				t.Fatalf("write to held native projection succeeded: %s", name)
			} else if !errors.Is(err, syscall.Errno(32)) {
				t.Fatalf("write denied for wrong reason (expected Windows ERROR_SHARING_VIOLATION): %s: %v", name, err)
			}
			if err := os.Rename(path, path+".moved"); err == nil {
				t.Fatalf("rename of held native projection succeeded: %s", name)
			}
			if err := os.Remove(path); err == nil {
				t.Fatalf("delete of held native projection succeeded: %s", name)
			}
			raw, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(raw, original[name]) {
				t.Fatalf("native projection changed despite held file: %s: %v", name, err)
			}
		})
	}
	// An open directory handle alone does NOT prevent a separate Windows
	// creator from introducing a new N-ID. Detect membership drift and
	// reject the operation instead of returning an unsafe success.
	t.Run("new-N-ID-fails-verification", func(t *testing.T) {
		path := filepath.Join(root, "plan", "N-120_intruder.md")
		createErr := os.WriteFile(path, []byte("foreign allocation\n"), 0600)
		if createErr == nil {
			if err := hold.Verify(); err == nil {
				t.Fatalf("unapproved N-ID was created but generation verification accepted it: %s", path)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("cannot clean disposable intruder fixture: %v", err)
			}
			// ABA is never a safe recovery for the SAME held generation.
			// The directory watch must stay signaled even after deletion.
			if err := hold.Verify(); err == nil {
				t.Fatal("transient N-ID creation was forgotten by the native hold")
			} else if !strings.Contains(err.Error(), "change notification") {
				t.Fatalf("native ABA denied for unrelated reason: %v", err)
			}
			hold.Close()
			hold, err = holdN124ReplayProjection(root)
			if err != nil {
				t.Fatalf("fresh hold after cleanup should be possible: %v", err)
			}
		} else {
			t.Logf("OS independently denied new child: %v", createErr)
			if err := hold.Verify(); err != nil {
				t.Fatalf("denied new child changed native generation: %v", err)
			}
		}
	})
	// Foreign WIP outside the native projection remains independently writable.
	if err := os.WriteFile(foreign, []byte("preserved foreign WIP updated\n"), 0600); err != nil {
		t.Fatalf("native projection lock blocked unrelated WIP: %v", err)
	}
	if err := hold.Verify(); err != nil {
		t.Fatalf("pinned native projection failed generation check: %v", err)
	}
	hold.Close()
	// No handle leak: the pinned native file becomes writable after release.
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("released\n"), 0600); err != nil {
		t.Fatalf("native generation hold leaked a Windows file handle: %v", err)
	}
}

// A second OS process, not just a goroutine or same-process writer, must
// observe the Windows share fence while the native handles are pinned.
func TestN124ProjectionHoldDeniesSecondProcessWriter(t *testing.T) {
	if target := os.Getenv("N124_PROJECTION_CHILD_WRITER"); target != "" {
		if err := os.WriteFile(target, []byte("unexpected independent write\n"), 0600); err == nil {
			t.Fatal("separate process modified held native source")
		} else if !errors.Is(err, syscall.Errno(32)) {
			t.Fatalf("separate writer failed for wrong reason (not Windows sharing violation): %v", err)
		}
		println("N124_CHILD_SHARING_VIOLATION_CONFIRMED")
		return
	}
	root, _, _ := n124ProjectionHoldFixture(t)
	hold, err := holdN124ReplayProjection(root)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	path := filepath.Join(root, "PLAN.md")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestN124ProjectionHoldDeniesSecondProcessWriter$", "-test.v")
	cmd.Env = append(os.Environ(), "N124_PROJECTION_CHILD_WRITER="+path)
	output, childErr := cmd.CombinedOutput()
	if childErr != nil || !bytes.Contains(output, []byte("N124_CHILD_SHARING_VIOLATION_CONFIRMED")) {
		t.Fatalf("separate writer not held safely: err=%v output=%s", childErr, output)
	}
	current, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(original, current) {
		t.Fatal("native PLAN changed after independent child writer")
	}
	if err := hold.Verify(); err != nil {
		t.Fatalf("native projection changed after child denial: %v", err)
	}
}

func TestN124ProjectionHoldRejectsExistingWriter(t *testing.T) {
	root, _, _ := n124ProjectionHoldFixture(t)
	path := filepath.Join(root, "learn", "events.jsonl")
	writer, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	hold, err := holdN124ReplayProjection(root)
	if err == nil {
		hold.Close()
		t.Fatal("native generation hold trusted a concurrently opened writer")
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	hold, err = holdN124ReplayProjection(root)
	if err != nil {
		t.Fatalf("failed to reacquire native generation after competing writer closed: %v", err)
	}
	if err := hold.Verify(); err != nil {
		t.Fatal(err)
	}
	hold.Close()
}

func TestN124ProjectionHoldRejectsHardlinkAndRecovers(t *testing.T) {
	root, _, _ := n124ProjectionHoldFixture(t)
	leaf := filepath.Join(root, "plan", "N-119_base.md")
	alias := filepath.Join(root, "outside-native-hardlink.md")
	if err := os.Link(leaf, alias); err != nil {
		t.Fatalf("NTFS hardlink negative fixture unavailable: %v", err)
	}
	hold, err := holdN124ReplayProjection(root)
	if err == nil {
		hold.Close()
		t.Fatal("held native file with second hardlink accepted")
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	hold, err = holdN124ReplayProjection(root)
	if err != nil {
		t.Fatalf("failed to recover after removing disposable hardlink: %v", err)
	}
	if err := hold.Verify(); err != nil {
		t.Fatal(err)
	}
	hold.Close()
}

func TestN124ProjectionHoldRejectsOversizedNativeFile(t *testing.T) {
	root, _, _ := n124ProjectionHoldFixture(t)
	leaf := filepath.Join(root, "learn", "events.jsonl")
	if err := os.WriteFile(leaf, bytes.Repeat([]byte("x"), n124ProjectionFileLimit+1), 0600); err != nil {
		t.Fatal(err)
	}
	hold, err := holdN124ReplayProjection(root)
	if err == nil {
		hold.Close()
		t.Fatal("oversized LEARN file accepted under native generation hold")
	}
	if err := os.WriteFile(leaf, []byte("recovered original fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	hold, err = holdN124ReplayProjection(root)
	if err != nil {
		t.Fatalf("failed to recover after native file size corrected: %v", err)
	}
	if err := hold.Verify(); err != nil {
		t.Fatal(err)
	}
	hold.Close()
}

func TestN124ProjectionHoldRejectsWindowsJunction(t *testing.T) {
	root, _, _ := n124ProjectionHoldFixture(t)
	foreign := t.TempDir()
	leaf := filepath.Join(foreign, "N-119_base.md")
	content := []byte("unchanged foreign fixture\n")
	if err := os.WriteFile(leaf, content, 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "plan")
	if err := os.RemoveAll(junction); err != nil {
		t.Fatal(err)
	}
	result, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, foreign).CombinedOutput()
	if err != nil {
		t.Fatalf("NTFS junction fixture unavailable: %v output=%s", err, result)
	}
	via, err := os.ReadFile(filepath.Join(junction, "N-119_base.md"))
	if err != nil || !bytes.Equal(via, content) {
		t.Fatalf("junction did not actually redirect into foreign fixture: %v", err)
	}
	hold, err := holdN124ReplayProjection(root)
	if err == nil {
		hold.Close()
		t.Fatal("native projection hold trusted a Windows junction")
	}
	actual, err := os.ReadFile(leaf)
	if err != nil || !bytes.Equal(actual, content) {
		t.Fatal("junction rejection mutated the foreign file")
	}
}

func TestN124ProjectionHoldRequiresIsolatedFixtureAuthority(t *testing.T) {
	root, _, _ := n124ProjectionHoldFixture(t)
	path := filepath.Join(root, "plan-authority.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if hold, err := holdN124ReplayProjection(root); err == nil {
		hold.Close()
		t.Fatal("native hold accepted an undeclared authority root")
	}
	bad := bytes.Replace(original, []byte("isolated-fixture"), []byte("production"), 1)
	if bytes.Equal(original, bad) {
		t.Fatal("test cannot construct invalid fixture authority")
	}
	if err := os.WriteFile(path, bad, 0600); err != nil {
		t.Fatal(err)
	}
	if hold, err := holdN124ReplayProjection(root); err == nil {
		hold.Close()
		t.Fatal("native hold accepted a production authority declaration")
	}
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	hold, err := holdN124ReplayProjection(root)
	if err != nil {
		t.Fatalf("valid disposable declaration was not recoverable: %v", err)
	}
	if err := hold.Verify(); err != nil {
		t.Fatal(err)
	}
	hold.Close()
}
