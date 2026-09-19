package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func newChildCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	if isWindowsPowerShell(name) {
		cmd.Env = sanitizePSModulePath(os.Environ())
	}
	return cmd
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
func configuredChildEnvironment(name string, env []string) []string {
	if isWindowsPowerShell(name) {
		return sanitizePSModulePath(env)
	}
	return env
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
