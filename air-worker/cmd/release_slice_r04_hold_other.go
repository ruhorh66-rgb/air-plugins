//go:build !windows

package main

import "errors"

// The only accepted R04 production target is SRVLM01 Windows. Non-Windows
// source consumers fail closed until a corresponding directory-handle guard
// is independently implemented and accepted.
func releaseR04HoldSignedScratchUntilChild(string) (func(), error) {
	return nil, errors.New("R04: native scratch child hold is Windows-only")
}
