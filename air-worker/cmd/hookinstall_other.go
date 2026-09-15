//go:build !windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

// hookInstallHome — каталог установки air-worker для стража обхода на не-Windows: install
// (cmdInstall, install_other.go) на этой ОС не ставит продукт вовсе, поэтому здесь только
// то же имя переменной окружения, что читает install_windows.go, без ветки автозапуска.
func hookInstallHome() string {
	if v := strings.TrimSpace(os.Getenv("AIR_WORKER_HOME")); v != "" {
		return v
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".local", "share", "air-worker")
}
