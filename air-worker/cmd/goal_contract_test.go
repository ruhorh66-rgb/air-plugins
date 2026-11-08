//go:build windows

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoalContractReleaseVersion(t *testing.T) {
	checker, err := os.ReadFile(filepath.Join("..", "tools", "check-goal-file.ps1"))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		field   string
		value   any
		wantErr bool
	}{
		{name: "valid release", field: "release_version", value: "0.11.9"},
		{name: "absent release"},
		{name: "numeric release", field: "release_version", value: 119, wantErr: true},
		{name: "malformed release", field: "release_version", value: "not-a-version", wantErr: true},
		{name: "unicode digits", field: "release_version", value: "٠.١١.٩", wantErr: true},
		{name: "trailing newline", field: "release_version", value: "0.11.9\n", wantErr: true},
		{name: "null release", field: "release_version", value: nil, wantErr: true},
		{name: "unknown field", field: "surpriseField", value: true, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for _, dir := range []string{"tools", "goal"} {
				if err := os.Mkdir(filepath.Join(root, dir), 0755); err != nil {
					t.Fatal(err)
				}
			}
			goal := map[string]any{
				"goal": "test", "objective": "test", "enabled": true,
				"workdir": ".", "taskFile": "PLAN.md", "judge": "dummy.exe", "engine": "dummy.exe",
				"maxIterations": 1, "maxMinutes": 1, "maxRunsPerDay": 1,
			}
			if tc.field != "" {
				goal[tc.field] = tc.value
			}
			data, err := json.Marshal(goal)
			if err != nil {
				t.Fatal(err)
			}
			files := map[string][]byte{
				filepath.Join("tools", "check-goal-file.ps1"): checker,
				filepath.Join("goal", "goal.json"):            data,
				"PLAN.md":                                     []byte("test"),
				"dummy.exe":                                   []byte("test"),
				"woody.ps1":                                   []byte("$taskFile = Join-Path\n[System.IO.File]::WriteAllText($taskFile\nInvoke-Claude $prompt\nInvoke-Codex $prompt\n"),
			}
			for name, contents := range files {
				if err := os.WriteFile(filepath.Join(root, name), contents, 0644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "tools", "check-goal-file.ps1"))
			cmd.Env = append(os.Environ(), "AIR_WOODY_SCRIPT="+filepath.Join(root, "woody.ps1"))
			output, err := cmd.CombinedOutput()
			if !tc.wantErr {
				if err != nil {
					t.Fatalf("checker failed: %v\n%s", err, output)
				}
				return
			}
			exitErr, ok := err.(*exec.ExitError)
			if !ok || exitErr.ExitCode() != 1 {
				t.Fatalf("expected exit 1, got %v\n%s", err, output)
			}
			found := false
			for _, line := range strings.Split(string(output), "\n") {
				if strings.Contains(line, "[FAIL]") && strings.Contains(line, tc.field) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no failure attributed to %s:\n%s", tc.field, output)
			}
		})
	}
}
