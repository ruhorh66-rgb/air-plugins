//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// openedFileFinalPath must describe the object behind the already-open handle, not
// resolve f.Name() again. Re-resolving the pathname is racy: an attacker can swap a
// parent link for os.Open and restore it before validation while the handle still
// points outside the product root.
//
// Linux exposes the kernel-bound file description through /proc/self/fd. Platforms
// without that facility fail closed; adding a platform-specific handle API is safer
// than silently weakening path confinement.
func openedFileFinalPath(f *os.File) (string, error) {
	if f == nil {
		return "", errors.New("nil file handle")
	}
	procPath := fmt.Sprintf("/proc/self/fd/%d", f.Fd())
	resolved, err := os.Readlink(procPath)
	if err != nil {
		return "", fmt.Errorf("handle-derived final path unavailable on this platform: %w", err)
	}
	if strings.HasSuffix(resolved, " (deleted)") {
		return "", errors.New("opened file was deleted before confinement verification")
	}
	if !filepath.IsAbs(resolved) {
		return "", fmt.Errorf("handle-derived final path is not absolute: %q", resolved)
	}
	return filepath.Clean(resolved), nil
}
