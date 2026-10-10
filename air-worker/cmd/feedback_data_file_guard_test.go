package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestN108NativeFeedbackInputOpenedHandleKeepsIdentity(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "safe.json")
	first := []byte(`{"kind":"error","run_id":"n108","text":"true source"}`)
	if err := os.WriteFile(file, first, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readNativeFeedbackDataSafe(file, 64*1024)
	if err != nil || string(got) != string(first) {
		t.Fatalf("untampered opened JSON was rejected or changed: %v %s", err, got)
	}
	alt := []byte(`{"kind":"error","run_id":"n108","text":"other source"}`)
	_, err = readNativeFeedbackDataSafeWithOpen(file, 64*1024, func(path string) (*os.File, error) {
		if err := os.Rename(path, path+".old"); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, alt, 0o600); err != nil {
			return nil, err
		}
		return os.Open(path)
	})
	if err == nil {
		t.Fatal("leaf replaced after Lstat was accepted as the original source handle")
	}
}

func TestN108NativeFeedbackInputRejectsParentJunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific junction repro")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	alias := filepath.Join(dir, "alias")
	if err := os.MkdirAll(real, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(real, "request.json")
	if err := os.WriteFile(path, []byte(`{"kind":"error","run_id":"n108","text":"real"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("cmd", "/c", "mklink", "/J", alias, real).CombinedOutput()
	if err != nil {
		t.Skipf("junction creation unavailable in isolated test directory: %v %s", err, raw)
	}
	if _, err := readNativeFeedbackDataSafe(filepath.Join(alias, "request.json"), 64*1024); err == nil {
		t.Fatal("parent directory reparse/junction was used as a feedback source")
	}
}

func TestN108NativeFeedbackInputSameSizeSwapFailsClosed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "same-size.json")
	original := []byte(`{"kind":"error","text":"TRUE"}`)
	changed := []byte(`{"kind":"error","text":"FAKE"}`)
	if len(original) != len(changed) {
		t.Fatal("fixture length mismatch")
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := readNativeFeedbackDataSafeWithOpen(path, 64*1024, func(name string) (*os.File, error) {
		if err := os.Rename(name, name+".old"); err != nil {
			return nil, err
		}
		if err := os.WriteFile(name, changed, 0o600); err != nil {
			return nil, err
		}
		return os.Open(name)
	})
	if err == nil {
		t.Fatal("same-size replacement evaded opened-file and path identity verification")
	}
}

func TestN108NativeFeedbackSwapSameLengthAndRestoredTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real-input.json")
	original := []byte(`{"kind":"error","text":"TRUE"}`)
	impostor := []byte(`{"kind":"error","text":"FAKE"}`)
	if len(original) != len(impostor) {
		t.Fatal("fixture length drift")
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = readNativeFeedbackDataSafeWithOpen(path, 64*1024, func(name string) (*os.File, error) {
		if err := os.Rename(name, name+".saved"); err != nil {
			return nil, err
		}
		if err := os.WriteFile(name, impostor, 0o600); err != nil {
			return nil, err
		}
		// P2: metadata validation alone accepts this forged replacement.
		if err := os.Chtimes(name, before.ModTime(), before.ModTime()); err != nil {
			return nil, err
		}
		return os.Open(name)
	})
	if err == nil {
		t.Fatal("changed file ID with identical size and mtime was accepted")
	}
}
