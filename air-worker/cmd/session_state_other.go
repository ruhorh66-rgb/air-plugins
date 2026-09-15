//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

// sessionStateDir — host-neutral (К44): на не-Windows состояние сессии не привязано к
// ProgramData, которого там нет. XDG_STATE_HOME — стандартное место пользовательского
// изменяемого состояния; без него — домашний каталог.
func sessionStateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		base = filepath.Join(home, ".local", "state")
	}
	dir := filepath.Join(base, "air-worker")
	_ = os.MkdirAll(dir, 0o755)
	return dir
}
