//go:build windows

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A hard link to a foreign release-slices file has the same physical identity
// as its target. os.SameFile alone does not prove exclusive native ownership.
// DEV-136's read-only authority preflight must fail closed on multiple links.
func TestPlanHeaderReleaseSidecarRejectsForeignHardlink(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "plan")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(t.TempDir(), "foreign-release-slices.json")
	content := []byte(`{"active_release":"1.9.7","releases":[{"version":"1.9.7"}]}`)
	if err := os.WriteFile(foreign, content, 0600); err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(dir, "release-slices.json")
	if err := os.Link(foreign, local); err != nil {
		t.Fatalf("cannot construct NTFS hardlink negative fixture: %v", err)
	}
	if _, err := planHeaderActiveRelease(root); err == nil {
		t.Fatal("foreign hardlink to release authority was accepted")
	}
	got, err := os.ReadFile(foreign)
	if err != nil || string(got) != string(content) {
		t.Fatalf("foreign release-sidecar changed: %v", err)
	}
	if err := os.Remove(local); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, content, 0600); err != nil {
		t.Fatal(err)
	}
	release, err := planHeaderActiveRelease(root)
	if err != nil || release != "1.9.7" {
		t.Fatalf("normal single-link local release did not recover: %q %v", release, err)
	}
}
