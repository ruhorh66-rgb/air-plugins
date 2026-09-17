package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCodexAdditionalWritableDirs(t *testing.T) {
	first := t.TempDir()
	second := filepath.Join(t.TempDir(), "vault")
	if err := os.Mkdir(second, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := runnerSpec{Kind: "codex", Model: "gpt-test", AddDirs: []string{first, second}}
	if err := validateCodexAddDirs(runner.AddDirs); err != nil {
		t.Fatalf("valid add_dirs rejected: %v", err)
	}
	args := codexArgs(`F:\product`, "TASK", runner)
	var got []string
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--add-dir" {
			got = append(got, args[i+1])
		}
	}
	want := []string{filepath.Clean(first), filepath.Clean(second)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("add-dir arguments = %#v, want %#v", got, want)
	}
	cfg := runConfig{Runners: map[string]runnerSpec{"luna": runner}}
	if resolved := resolveRunner(cfg, "luna:low"); !reflect.DeepEqual(resolved.AddDirs, runner.AddDirs) {
		t.Fatalf("resolveRunner lost add_dirs: %#v", resolved.AddDirs)
	}
}

func TestCodexVolumeRootClassification(t *testing.T) {
	for _, path := range []string{`C:\`, `\\server\share`, `\\server\share\`} {
		if !isCodexVolumeRoot(path) {
			t.Errorf("volume root not detected: %q", path)
		}
	}
	for _, path := range []string{`C:\work`, `\\server\share\work`} {
		if isCodexVolumeRoot(path) {
			t.Errorf("ordinary directory classified as root: %q", path)
		}
	}
}

func TestCodexAdditionalWritableDirsRejectUnsafe(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"empty": " ", "relative": "vault",
		"root":    filepath.VolumeName(t.TempDir()) + string(filepath.Separator),
		"missing": filepath.Join(t.TempDir(), "missing"), "file": file,
	}
	for name, path := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateCodexAddDirs([]string{path}); err == nil {
				t.Fatalf("unsafe add_dir accepted: %q", path)
			}
		})
	}
}

func TestInvokeCodexRejectsAddDirsBeforeProcessStart(t *testing.T) {
	old := runnerCommand
	t.Cleanup(func() { runnerCommand = old })
	started := false
	runnerCommand = func(_ string, _ ...string) *exec.Cmd { started = true; return nil }
	res := (&loopCtx{Root: t.TempDir()}).invokeCodex("codex", "TASK", runnerSpec{Kind: "codex", AddDirs: []string{"relative"}}, "unsafe")
	if started {
		t.Fatal("Codex process started for invalid add_dirs")
	}
	if res.Ok || res.Subtype != "invalid_runner_config" || !strings.Contains(res.Detail, "must be absolute") {
		t.Fatalf("unexpected result: %+v", res)
	}
}
