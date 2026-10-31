//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestUniqueConfigRootsKeepsOnePhysicalProfileAcrossWindowsJunction(t *testing.T) {
	base := t.TempDir()
	physical := filepath.Join(base, "codex-physical")
	alias := filepath.Join(base, "codex-alias")
	if err := os.MkdirAll(physical, 0o700); err != nil {
		t.Fatal(err)
	}
	// An NTFS directory junction is available to standard Windows users and
	// models the observed USERPROFILE/.codex -> CODEX_HOME alias on SRVLM01.
	out, err := exec.Command("cmd", "/d", "/c", "mklink", "/J", alias, physical).CombinedOutput()
	if err != nil {
		t.Fatalf("create isolated NTFS junction: %v: %s", err, out)
	}
	active, err := os.Stat(physical)
	if err != nil {
		t.Fatal(err)
	}
	fallback, err := os.Stat(alias)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(active, fallback) {
		t.Fatal("test fixture is not a physical directory alias")
	}
	roots := uniqueConfigRoots(physical, alias)
	if len(roots) != 1 || roots[0] != physical {
		t.Fatalf("one physical Codex profile must be inspected once: %v", roots)
	}
	unrelated := filepath.Join(base, "codex-independent")
	if err := os.MkdirAll(unrelated, 0o700); err != nil {
		t.Fatal(err)
	}
	roots = uniqueConfigRoots(physical, unrelated)
	if len(roots) != 2 || roots[0] != physical || roots[1] != unrelated {
		t.Fatalf("independent Codex profile must remain inspected: %v", roots)
	}
	unavailable := filepath.Join(base, "not-yet-created")
	roots = uniqueConfigRoots(physical, unavailable)
	if len(roots) != 2 || roots[1] != unavailable {
		t.Fatalf("stat failure cannot conceal an unverified profile: %v", roots)
	}
}
