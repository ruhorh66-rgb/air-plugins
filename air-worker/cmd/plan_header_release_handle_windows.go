//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// planHeaderVerifyReleaseHandle refuses a Windows release sidecar backed by
// multiple hardlinks or a reparse point. os.SameFile cannot distinguish the
// canonical path from a foreign hardlink to the same physical file.
// This is read-only preflight, not a production generation/owner lock.
func planHeaderVerifyReleaseHandle(f *os.File) error {
	if f == nil {
		return errors.New("release sidecar file handle missing")
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &info); err != nil {
		return fmt.Errorf("release sidecar Windows handle identity unavailable: %w", err)
	}
	if info.FileAttributes&(syscall.FILE_ATTRIBUTE_DIRECTORY|syscall.FILE_ATTRIBUTE_REPARSE_POINT) != 0 ||
		info.NumberOfLinks != 1 {
		return errors.New("release sidecar has untrusted hardlink/reparse identity")
	}
	return nil
}
