//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPlanHeaderBoundedPayloadRejectsForeignHardlink(t *testing.T) {
	root := t.TempDir()
	foreignDir := t.TempDir()
	foreign := filepath.Join(foreignDir, "outside.txt")
	content := []byte("unrelated immutable cache owner\n")
	if err := os.WriteFile(foreign, content, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "alias.txt")
	if err := os.Link(foreign, link); err != nil {
		t.Fatalf("cannot construct same-volume hardlink fixture: %v", err)
	}
	if _, _, err := planHeaderBoundedPayloadSnapshot(root); err == nil {
		t.Fatal("untrusted foreign hardlink accepted as canonical plugin payload")
	}
	got, err := os.ReadFile(foreign)
	if err != nil || string(got) != string(content) {
		t.Fatalf("foreign payload changed: %v", err)
	}
}

func TestPlanHeaderBoundedPayloadRejectsNTFSJunction(t *testing.T) {
	root := t.TempDir()
	foreign := t.TempDir()
	if err := os.WriteFile(filepath.Join(foreign, "foreign.txt"), []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "redirected-cache-directory")
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, foreign).CombinedOutput()
	if err != nil {
		t.Fatalf("cannot construct junction negative fixture: %v, %s", err, output)
	}
	if _, err := os.Stat(filepath.Join(junction, "foreign.txt")); err != nil {
		t.Fatalf("invalid junction fixture: %v", err)
	}
	if _, _, err := planHeaderBoundedPayloadSnapshot(root); err == nil {
		t.Fatal("foreign directory junction accepted as plugin payload")
	}
}
