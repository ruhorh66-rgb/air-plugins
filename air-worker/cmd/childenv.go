package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func newChildCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	var err error
	cmd.Env, err = childEnvironment(name, os.Environ(), "")
	if err != nil {
		cmd.Err = err
	}
	return cmd
}

// childEnvironment pins every descendant to the selected Claude profile. An
// explicit profile is used by update when it has already selected a profile.
func childEnvironment(name string, base []string, profile string) ([]string, error) {
	if profile == "" {
		var err error
		profile, err = activeClaudeConfigDir()
		if err != nil {
			return nil, err
		}
	}
	if profile == "" {
		return nil, fmt.Errorf("active Claude profile is unknown; set CLAUDE_CONFIG_DIR")
	}
	env := upsertEnv(base, "CLAUDE_CONFIG_DIR", profile)
	if isWindowsPowerShell(name) {
		env = sanitizePSModulePath(env)
	}
	return env, nil
}

func activeClaudeConfigDir() (string, error) {
	process := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR"))
	if process != "" {
		return filepath.Abs(process)
	}
	user := strings.TrimSpace(userEnvVar("CLAUDE_CONFIG_DIR"))
	if user != "" {
		return filepath.Abs(user)
	}
	if strings.TrimSpace(os.Getenv("USERPROFILE")) == "" && strings.TrimSpace(os.Getenv("HOME")) == "" {
		return "", fmt.Errorf("active Claude profile is unknown; set CLAUDE_CONFIG_DIR")
	}
	return filepath.Abs(claudeDefaultConfigDir())
}

func newPowerShellCommand(args ...string) *exec.Cmd {
	shell := "pwsh"
	if runtime.GOOS == "windows" {
		shell = "powershell.exe"
	}
	return newChildCommand(shell, args...)
}

func isWindowsPowerShell(name string) bool {
	return runtime.GOOS == "windows" && strings.EqualFold(filepath.Base(name), "powershell.exe")
}

// configuredChildEnvironment preserves the old explicit-environment behaviour for
// configured checks while applying the same PowerShell 5.1 guard after overrides.
func configuredChildEnvironment(name string, env []string) ([]string, error) {
	return childEnvironment(name, env, "")
}

func sanitizePSModulePath(env []string) []string {
	out := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(strings.ToLower(entry), "psmodulepath=") {
			out = append(out, entry)
			continue
		}
		key, value, _ := strings.Cut(entry, "=")
		parts := strings.Split(value, ";")
		kept := make([]string, 0, len(parts))
		for _, part := range parts {
			normalized := strings.ToLower(strings.ReplaceAll(part, "/", "\\"))
			if strings.Contains(normalized, `\powershell\7`) || strings.Contains(normalized, `\pwsh\7`) {
				continue
			}
			kept = append(kept, part)
		}
		out = append(out, key+"="+strings.Join(kept, ";"))
	}
	return out
}
