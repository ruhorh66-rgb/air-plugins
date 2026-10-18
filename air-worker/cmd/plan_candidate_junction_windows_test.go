//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// A Windows NTFS directory junction is a reparse point even where Go's
// Lstat/Readlink do not mark it as a symbolic link. Native PLAN/claim
// authority must reject a junction in any ancestor, including when the
// target leaf does not exist yet. Everything here is a disposable temp path.
func TestCandidateNoLinksRejectsWindowsJunction(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	contents := []byte("foreign immutable fixture\n")
	if err := os.WriteFile(filepath.Join(foreign, "sentinel.txt"), contents, 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "plan")
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, foreign).CombinedOutput()
	if err != nil {
		t.Skipf("NTFS junction construction unavailable: %v; %s", err, string(out))
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
			}
		})
	}
	// The foreign target is never modified by these read-only checks.
	raw, err := os.ReadFile(filepath.Join(foreign, "sentinel.txt"))
	if err != nil || string(raw) != string(contents) {
		t.Fatal("foreign fixture was modified")
	}
}
