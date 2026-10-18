//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A Windows NTFS directory junction is a reparse point that os.Lstat may
// not classify as a symbolic link. The candidateNoLinks guard must reject
// a junction in any ancestor, including a missing future leaf. Verify
// this on a real disposable junction, without UAC or production mutation.
func TestCandidateNoLinksRejectsWindowsJunction(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	contents := []byte("foreign immutable fixture\n")
	if err := os.WriteFile(filepath.Join(foreign, "sentinel.txt"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	// A normal sibling and future leaf under the SAME root must be accepted.
	// Otherwise a linked temp-root ancestor can produce a false-positive PASS.
	ordinary := filepath.Join(root, "ordinary")
	if err := os.Mkdir(ordinary, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{root, foreign, ordinary, filepath.Join(ordinary, "future", "N-120_new.md")} {
		if err := candidateNoLinks(path); err != nil {
			t.Fatalf("ordinary path rejected, test fixture unsuitable: %s: %v", path, err)
		}
	}
	junction := filepath.Join(root, "plan")
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, foreign).CombinedOutput()
	if err != nil {
		t.Fatalf("cannot verify Windows junction safety when fixture creation fails: %v; %s", err, string(out))
	}
	via, err := os.ReadFile(filepath.Join(junction, "sentinel.txt"))
	if err != nil || string(via) != string(contents) {
		t.Fatalf("junction fixture is not an actual redirect: %v", err)
	}
	for _, leaf := range []string{
		junction,
		filepath.Join(junction, "sentinel.txt"),
		filepath.Join(junction, "future", "N-120_new.md"),
	} {
		t.Run(filepath.Base(leaf), func(t *testing.T) {
			if err := candidateNoLinks(leaf); err == nil {
				t.Fatalf("untrusted NTFS junction path accepted: %s", leaf)
			} else if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(junction)) {
				t.Fatalf("NTFS junction was not identified as the denial reason: %s: %v", leaf, err)
			}
		})
	}
	// The foreign target is never modified by these read-only checks.
	raw, err := os.ReadFile(filepath.Join(foreign, "sentinel.txt"))
	if err != nil || string(raw) != string(contents) {
		t.Fatal("foreign fixture was modified")
	}
}
