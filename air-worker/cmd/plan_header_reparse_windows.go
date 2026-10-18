//go:build windows

package main

import (
	"errors"
	"syscall"
)

// Windows directory junctions are reparse points even when Go's Lstat
// reports them as regular directories and EvalSymlinks preserves the name.
// Use the OS attribute directly; unknown paths fail closed. This is a
// preflight, not a substitute for a stable handle/generation lock on writes.
func planHeaderRejectOSReparse(name string) error {
	p, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	attributes, err := syscall.GetFileAttributes(p)
	if err != nil {
		return err
	}
	if attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("Windows reparse point in PLAN authority path")
	}
	return nil
}
