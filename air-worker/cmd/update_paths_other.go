//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func updateRootDir() string {
	if base, err := os.UserCacheDir(); err == nil && base != "" {
		return filepath.Join(base, "air-worker", "update")
	}
	return filepath.Join(os.TempDir(), "air-worker-update")
}

func updateInstallHome() string {
	return ""
}
