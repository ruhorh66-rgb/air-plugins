//go:build windows

package main

import (
	"os"
	"path/filepath"
)

// sessionStateDir — то же место, что держал mode.ps1 (`%ProgramData%\AIR OS\State`), и то же,
// что уже читает трей (cmd/tray/main_windows.go, stateDir()): формат файлов и каталог — общий
// контракт с треем, менять их здесь означало бы разойтись со значком молча.
func sessionStateDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	dir := filepath.Join(pd, "AIR OS", "State")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
