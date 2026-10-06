//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var sharedPathKernel32 = syscall.NewLazyDLL("kernel32.dll")
var procSharedGetFinalPathNameByHandleW = sharedPathKernel32.NewProc("GetFinalPathNameByHandleW")

func openedFileFinalPath(f *os.File) (string, error) {
	if f == nil {
		return "", errors.New("nil file handle")
	}
	size := 512
	for size <= 32768 {
		buf := make([]uint16, size)
		n, _, callErr := procSharedGetFinalPathNameByHandleW.Call(
			f.Fd(),
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(len(buf)),
			0,
		)
		if n == 0 {
			if callErr != nil && callErr != syscall.Errno(0) {
				return "", callErr
			}
			return "", errors.New("GetFinalPathNameByHandleW failed")
		}
		if int(n) < len(buf) {
			path := syscall.UTF16ToString(buf[:n])
			switch {
			case strings.HasPrefix(path, `\\?\UNC\`):
				path = `\\` + strings.TrimPrefix(path, `\\?\UNC\`)
			case strings.HasPrefix(path, `\\?\`):
				path = strings.TrimPrefix(path, `\\?\`)
			}
			return filepath.Clean(path), nil
		}
		size = int(n) + 1
	}
	return "", errors.New("resolved file path exceeds supported length")
}
