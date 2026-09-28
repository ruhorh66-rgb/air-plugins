//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func artifactForFile(t *testing.T, path, name string) updateArtifact {
	t.Helper()
	sum, err := sha256File(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return updateArtifact{Name: name, SHA256: sum, Size: st.Size()}
}

func TestUpdatePairRollsBackOnPostInstallFailure(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(home, "bin")
	stage := filepath.Join(root, "stage")
	snapshot := filepath.Join(root, "snapshot")
	for _, dir := range []string{bin, stage, snapshot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	oldCLI := []byte("old-cli")
	oldTray := []byte("old-tray")
	newCLI := []byte("new-cli")
	newTray := []byte("new-tray")
	dstCLI := filepath.Join(bin, "air-worker.exe")
	dstTray := filepath.Join(bin, "air-worker-tray.exe")
	stagedCLI := filepath.Join(stage, "air-worker.exe")
	stagedTray := filepath.Join(stage, "air-worker-tray.exe")
	if err := os.WriteFile(dstCLI, oldCLI, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dstTray, oldTray, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedCLI, newCLI, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stagedTray, newTray, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyUpdateSnapshot(dstCLI, filepath.Join(snapshot, "air-worker.exe")); err != nil {
		t.Fatal(err)
	}
	if err := copyUpdateSnapshot(dstTray, filepath.Join(snapshot, "air-worker-tray.exe")); err != nil {
		t.Fatal(err)
	}

	m := updateManifest{
		CLI:  artifactForFile(t, stagedCLI, "air-worker.exe"),
		Tray: artifactForFile(t, stagedTray, "air-worker-tray.exe"),
	}
	err := replaceUpdatePairTransactional(snapshot, home, stagedCLI, stagedTray, m, func(string) error {
		return errors.New("forced post-install failure")
	})
	if err == nil || !strings.Contains(err.Error(), "forced post-install failure") {
		t.Fatalf("forced failure was not returned: %v", err)
	}
	gotCLI, _ := os.ReadFile(dstCLI)
	gotTray, _ := os.ReadFile(dstTray)
	if string(gotCLI) != string(oldCLI) || string(gotTray) != string(oldTray) {
		t.Fatalf("rollback failed: cli=%q tray=%q", gotCLI, gotTray)
	}
}

func TestUpdatePairKeepsNewFilesAfterAcceptance(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	bin := filepath.Join(home, "bin")
	stage := filepath.Join(root, "stage")
	snapshot := filepath.Join(root, "snapshot")
	for _, dir := range []string{bin, stage, snapshot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	dstCLI := filepath.Join(bin, "air-worker.exe")
	dstTray := filepath.Join(bin, "air-worker-tray.exe")
	stagedCLI := filepath.Join(stage, "air-worker.exe")
	stagedTray := filepath.Join(stage, "air-worker-tray.exe")
	for path, body := range map[string][]byte{
		dstCLI: []byte("old-cli"), dstTray: []byte("old-tray"),
		stagedCLI: []byte("new-cli"), stagedTray: []byte("new-tray"),
	} {
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := copyUpdateSnapshot(dstCLI, filepath.Join(snapshot, "air-worker.exe")); err != nil {
		t.Fatal(err)
	}
	if err := copyUpdateSnapshot(dstTray, filepath.Join(snapshot, "air-worker-tray.exe")); err != nil {
		t.Fatal(err)
	}

	m := updateManifest{
		CLI:  artifactForFile(t, stagedCLI, "air-worker.exe"),
		Tray: artifactForFile(t, stagedTray, "air-worker-tray.exe"),
	}
	if err := replaceUpdatePairTransactional(snapshot, home, stagedCLI, stagedTray, m, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	gotCLI, _ := os.ReadFile(dstCLI)
	gotTray, _ := os.ReadFile(dstTray)
	if string(gotCLI) != "new-cli" || string(gotTray) != "new-tray" {
		t.Fatalf("accepted update did not remain installed: cli=%q tray=%q", gotCLI, gotTray)
	}
}
