//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// NTFS can create and delete a new child while a read-only directory handle
// is open. The final directory listing then matches the original, but the
// native generation is NOT unchanged. This is a disposable OS-temp fixture:
// no production PLAN, N-node, LEARN or Git ref is touched.
func TestN124ProjectionHoldRejectsTransientABA(t *testing.T) {
	for _, rel := range []string{
		"plan/N-120_transient.md",
		"plan/event-outbox/transient.json",
		"learn/transient.jsonl",
	} {
		t.Run(strings.ReplaceAll(rel, "/", "_"), func(t *testing.T) {
			root, _, foreign := n124ProjectionHoldFixture(t)
			hold, err := holdN124ReplayProjection(root)
			if err != nil {
				t.Fatalf("cannot open baseline fixture hold: %v", err)
			}
			defer hold.Close()
			if err := hold.Verify(); err != nil {
				t.Fatalf("baseline generation cannot be verified: %v", err)
			}

			path := filepath.Join(root, filepath.FromSlash(rel))
			if err := os.WriteFile(path, []byte("transient native mutation\n"), 0600); err != nil {
				t.Fatalf("cannot reproduce transient create: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatalf("cannot reproduce transient delete: %v", err)
			}
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("transient path still exists, not an ABA fixture: %s: %v", path, err)
			}
			// A generation guard must latch notifications, not merely compare
			// directory contents at the end of the hold.
			if err := hold.Verify(); err == nil {
				t.Fatalf("ABA create/delete was mistaken for unchanged native generation: %s", rel)
			} else if !strings.Contains(err.Error(), "change notification") {
				t.Fatalf("ABA rejection was for an unrelated reason: %v", err)
			}

			// A native-path mutation must never rewrite unrelated content.
			if raw, err := os.ReadFile(foreign); err != nil ||
				string(raw) != "independent foreign WIP\n" {
				t.Fatalf("ABA detection mutated foreign WIP: %v", err)
			}
		})
	}
}

// This is an actual second OS process, not a goroutine. It creates and
// removes a native N-node while the parent owns all pinned read handles.
// The directory notification must remain latched on parent readback.
func TestN124ProjectionHoldDetectsCrossProcessABA(t *testing.T) {
	if target := os.Getenv("N124_ABA_CHILD_PATH"); target != "" {
		if err := os.WriteFile(target, []byte("cross-process ephemeral N-node\n"), 0600); err != nil {
			t.Fatalf("child cannot create N-node: %v", err)
		}
		if err := os.Remove(target); err != nil {
			t.Fatalf("child cannot delete N-node: %v", err)
		}
		t.Log("N124_CROSS_PROCESS_ABA_COMPLETE")
		return
	}

	root, _, foreign := n124ProjectionHoldFixture(t)
	hold, err := holdN124ReplayProjection(root)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	if err := hold.Verify(); err != nil {
		t.Fatalf("baseline cannot be verified before child ABA: %v", err)
	}
	target := filepath.Join(root, "plan", "N-120_cross_process.md")
	child := exec.Command(os.Args[0], "-test.run=^TestN124ProjectionHoldDetectsCrossProcessABA$", "-test.v")
	child.Env = append(os.Environ(), "N124_ABA_CHILD_PATH="+target)
	log, err := child.CombinedOutput()
	if err != nil || !strings.Contains(string(log), "N124_CROSS_PROCESS_ABA_COMPLETE") {
		t.Fatalf("independent child failed the create/delete fixture: %v: %s", err, log)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatalf("independent child left an N-node, not ABA: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := hold.Verify(); err == nil {
			t.Fatal("independent OS process ABA was accepted as an unchanged generation")
		} else if !strings.Contains(err.Error(), "change notification") {
			t.Fatalf("ABA denied for unrelated reason: %v", err)
		}
	}
	raw, err := os.ReadFile(foreign)
	if err != nil || string(raw) != "independent foreign WIP\n" {
		t.Fatalf("unrelated foreign WIP changed: %v", err)
	}

	// A new hold on the now-clean fixture must start with a new generation.
	hold.Close()
	newHold, err := holdN124ReplayProjection(root)
	if err != nil {
		t.Fatalf("clean fixture could not start a fresh hold: %v", err)
	}
	defer newHold.Close()
	if err := newHold.Verify(); err != nil {
		t.Fatalf("new hold unexpectedly inherited old notification: %v", err)
	}
}
