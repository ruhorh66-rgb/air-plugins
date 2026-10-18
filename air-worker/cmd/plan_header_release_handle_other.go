//go:build !windows

package main

import (
	"errors"
	"os"
)

// Non-Windows preflight remains read-only; platform-specific hardlink tests
// and stronger native file-handle policy are Windows-only for this release.
// Do not treat this fallback as an installed/prod CAS authorization.
func planHeaderVerifyReleaseHandle(f *os.File) error {
	if f == nil {
		return errors.New("release sidecar handle missing")
	}
	return nil
}
