//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Hold every non-volume ancestor of the signed disposable target without
// FILE_SHARE_DELETE until the child has exited and its signed outcome can be
// read back. Windows rename, junction replacement and removal require DELETE
// sharing; unlike a transient os.Root, these pinned handles prevent the
// check-to-child gap from redirecting the CLI's original signed text path.
func releaseR04HoldSignedScratchUntilChild(target string) (func(), error) {
	if target == "" || !filepath.IsAbs(target) || filepath.Clean(target) != target {
		return nil, errors.New("R04: scratch hold target is not canonical absolute")
	}
	// Keep os.File objects alive for the entire child lifetime. Creating an
	// os.File and retaining only its raw handle risks its finalizer closing
	// the handle while the signed-path child is still running.
	var pins []*os.File
	release := func() {
		for i := len(pins) - 1; i >= 0; i-- {
			_ = pins[i].Close()
		}
		pins = nil
	}
	for current := target; ; current = filepath.Dir(current) {
		if filepath.Dir(current) == current {
			break
		} // volume root cannot be renamed
		if err := n093CheckOrdinaryDir(current); err != nil {
			release()
			return nil, fmt.Errorf("R04: unsafe signed scratch ancestor: %w", err)
		}
		name, err := syscall.UTF16PtrFromString(current)
		if err != nil {
			release()
			return nil, err
		}
		h, err := syscall.CreateFile(name, 0,
			syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE,
			nil, syscall.OPEN_EXISTING,
			syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			release()
			return nil, fmt.Errorf("R04: cannot pin scratch ancestor against rename: %w", err)
		}
		var info syscall.ByHandleFileInformation
		if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
			_ = syscall.CloseHandle(h)
			release()
			return nil, err
		}
		if info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY == 0 ||
			info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			_ = syscall.CloseHandle(h)
			release()
			return nil, errors.New("R04: signed scratch ancestor handle is not plain directory")
		}
		f := os.NewFile(uintptr(h), current)
		physical, err := openedFileFinalPath(f)
		if err != nil || !sameLearningPath(physical, current) {
			_ = f.Close()
			release()
			return nil, errors.New("R04: signed scratch ancestor changed physical identity")
		}
		// The file reference owns the handle until release() is called after
		// subprocess exit. No garbage collector finalizer may close it early.
		pins = append(pins, f)
	}
	return release, nil
}
