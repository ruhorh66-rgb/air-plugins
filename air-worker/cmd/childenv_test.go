package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChildReceivesClaudeProfileWithEmptyBaseEnv(t *testing.T) {
	profile := filepath.Join(t.TempDir(), "active-claude")
	t.Setenv("CLAUDE_CONFIG_DIR", profile)
	env, err := childEnvironment("claude", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestClaudeProfileChildHelper$")
	cmd.Env = append(env, "AIR_CHILD_ENV_TEST=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != profile {
		t.Fatalf("child CLAUDE_CONFIG_DIR=%q want %q", got, profile)
	}
}

func TestClaudeProfileChildHelper(t *testing.T) {
	if os.Getenv("AIR_CHILD_ENV_TEST") == "" {
		return
	}
	_, _ = os.Stdout.WriteString(os.Getenv("CLAUDE_CONFIG_DIR"))
	os.Exit(0)
}

func TestChildEnvironmentSanitizesPSModulePath(t *testing.T) {
	input := []string{
		`Path=C:\bin`,
		`PSMODULEPATH=C:\Windows\System32\WindowsPowerShell\v1.0\Modules;C:\Program Files\PowerShell\7\Modules;D:/tools/pwsh/7/Modules;C:\custom\Modules`,
		`Unrelated=value;with=equals`,
	}
	got := sanitizePSModulePath(input)
	if len(got) != len(input) || got[0] != input[0] || got[2] != input[2] {
		t.Fatalf("unrelated environment changed: %#v", got)
	}
	if !strings.Contains(got[1], `v1.0\Modules`) || !strings.Contains(got[1], `C:\custom\Modules`) {
		t.Fatalf("Windows PowerShell and custom modules must remain: %q", got[1])
	}
	if strings.Contains(strings.ToLower(got[1]), `\powershell\7`) || strings.Contains(strings.ToLower(got[1]), `\pwsh\7`) {
		t.Fatalf("PowerShell 7 module roots remain: %q", got[1])
	}
}
