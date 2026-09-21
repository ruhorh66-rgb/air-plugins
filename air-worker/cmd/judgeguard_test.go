package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJudgeGuardFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func judgeGuardConfig(root string) runConfig {
	return runConfig{Judge: judgeSpec{
		Path:      "judge/judge.ps1",
		Checklist: "judge/checklist.json",
		Checks:    []checkSpec{{Name: "suite", Script: "judge/check.ps1"}},
	}}
}

func TestJudgeFilesRestoredAfterExecutorTurn(t *testing.T) {
	root := t.TempDir()
	cfg := judgeGuardConfig(root)
	configPath := filepath.Join(root, "custom-run-config.json")
	paths := []string{configPath, filepath.Join(root, "judge/judge.ps1"), filepath.Join(root, "judge/check.ps1"), filepath.Join(root, "judge/checklist.json")}
	for i, path := range paths {
		writeJudgeGuardFile(t, path, "before-"+string(rune('a'+i)))
	}
	snap, err := snapshotJudgeFiles(root, cfg, configPath)
	if err != nil {
		t.Fatal(err)
	}
	writeJudgeGuardFile(t, paths[0], "changed")
	writeJudgeGuardFile(t, paths[1], "changed")
	writeJudgeGuardFile(t, paths[2], "changed")
	writeJudgeGuardFile(t, paths[3], "changed")
	if err := os.Remove(paths[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreJudgeFiles(snap); err != nil {
		t.Fatal(err)
	}
	for i, path := range paths {
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := "before-" + string(rune('a'+i))
		if string(got) != want {
			t.Errorf("%s: got %q, want %q", path, got, want)
		}
	}
}

func TestProtectedJudgeSetFollowsConfig(t *testing.T) {
	root := t.TempDir()
	cfg := judgeGuardConfig(root)
	configPath := filepath.Join(root, "custom-run-config.json")
	want := []string{configPath, filepath.Join(root, "judge/judge.ps1"), filepath.Join(root, "judge/checklist.json"), filepath.Join(root, "judge/check.ps1")}
	for _, path := range want {
		writeJudgeGuardFile(t, path, "x")
	}
	writeJudgeGuardFile(t, filepath.Join(root, "cmd/product.go"), "package product")
	writeJudgeGuardFile(t, filepath.Join(root, "PLAN.md"), "plan")
	got := protectedJudgeFiles(root, cfg, configPath)
	if len(got) != len(want) {
		t.Fatalf("protected set = %#v, want %#v", got, want)
	}
	for _, path := range want {
		found := false
		for _, gotPath := range got {
			if gotPath == path {
				found = true
			}
		}
		if !found {
			t.Errorf("protected set misses %s", path)
		}
	}
}

func TestNewFileNextToJudgeScriptIsReportedNotDeleted(t *testing.T) {
	root := t.TempDir()
	cfg := judgeGuardConfig(root)
	configPath := filepath.Join(root, "run-config.json")
	writeJudgeGuardFile(t, configPath, "config")
	writeJudgeGuardFile(t, filepath.Join(root, "judge/judge.ps1"), "judge")
	writeJudgeGuardFile(t, filepath.Join(root, "judge/check.ps1"), "check")
	writeJudgeGuardFile(t, filepath.Join(root, "judge/checklist.json"), "facts")
	snap, err := snapshotJudgeFiles(root, cfg, configPath)
	if err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(root, "judge/new-evidence.txt")
	writeJudgeGuardFile(t, newPath, "new")
	if _, err := restoreJudgeFiles(snap); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("new file was deleted: %v", err)
	}
	warnings, err := newJudgeFiles(snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || warnings[0] != newPath {
		t.Fatalf("warnings = %#v, want %q", warnings, newPath)
	}
}

func TestExecutorPromptNamesProtectedJudgeFiles(t *testing.T) {
	root := t.TempDir()
	cfg := judgeGuardConfig(root)
	configPath := filepath.Join(root, "run-config.json")
	writeJudgeGuardFile(t, configPath, "config")
	writeJudgeGuardFile(t, filepath.Join(root, "judge/judge.ps1"), "judge")
	writeJudgeGuardFile(t, filepath.Join(root, "judge/check.ps1"), "check")
	writeJudgeGuardFile(t, filepath.Join(root, "judge/checklist.json"), "facts")
	c := &loopCtx{Root: root, Cfg: cfg, CfgPath: configPath, WhatIf: true, MaxTurns: 1}
	c.runModelStep(workStep{Index: 1, Num: "8b", Title: "8b. judge guard", Tier: "haiku"}, "haiku:medium", "", runnerSpec{Kind: "claude", Model: "haiku"})
	files, err := filepath.Glob(filepath.Join(root, ".woody", "TASK-1-*.md"))
	if err != nil || len(files) != 1 {
		t.Fatalf("task file not found: %v", err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	prompt := string(b)
	if !strings.Contains(prompt, "Не правь файлы судьи") {
		t.Fatal("prompt does not name protected judge files")
	}
	for _, path := range protectedJudgeFiles(root, cfg, configPath) {
		rel, _ := filepath.Rel(root, path)
		if !strings.Contains(prompt, filepath.ToSlash(rel)) {
			t.Errorf("prompt misses protected path %s", rel)
		}
	}
}
