package main

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func newPowerShellCommand(args ...string) *exec.Cmd {
	shell := "pwsh"
	if runtime.GOOS == "windows" {
		shell = "powershell.exe"
	}
	cmd := exec.Command(shell, args...)
	if runtime.GOOS == "windows" {
		cmd.Env = sanitizePSModulePath(os.Environ())
	}
	return cmd
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
