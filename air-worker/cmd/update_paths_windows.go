//go:build windows

package main

import "path/filepath"

func updateRootDir() string {
	return filepath.Join(installHome(), "update")
}

func updateInstallHome() string {
	return installHome()
}
