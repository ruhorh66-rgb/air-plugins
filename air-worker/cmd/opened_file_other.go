//go:build !windows

package main

import (
	"os"
	"path/filepath"
)

func openedFileFinalPath(f *os.File) (string, error) {
	return filepath.EvalSymlinks(f.Name())
}
