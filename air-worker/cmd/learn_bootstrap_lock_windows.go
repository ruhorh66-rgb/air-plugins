//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var bootstrapKernel = syscall.NewLazyDLL("kernel32.dll")
var bootstrapLockFile = bootstrapKernel.NewProc("LockFileEx")
var bootstrapUnlockFile = bootstrapKernel.NewProc("UnlockFileEx")

func acquireBootstrapFileLock(path string, timeout time.Duration) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	ov := new(syscall.Overlapped)
	deadline := time.Now().Add(timeout)
	for {
		ok, _, callErr := bootstrapLockFile.Call(f.Fd(), 0x3, 0, 1, 0, uintptr(unsafe.Pointer(ov)))
		if ok != 0 {
			return func() {
				bootstrapUnlockFile.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(ov)))
				_ = f.Close()
			}, nil
		}
		if !errors.Is(callErr, syscall.Errno(33)) {
			_ = f.Close()
			return nil, callErr
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, errors.New("shared-learning bootstrap lock timeout")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func acquireSharedLearningBootstrapLock(product, runtimeRoot string, timeout time.Duration) (func(), error) {
	productUnlock, err := acquireBootstrapFileLock(filepath.Join(product, ".air-worker", "learn", "bootstrap.lock"), timeout)
	if err != nil {
		return nil, err
	}
	runtimeUnlock, err := acquireBootstrapFileLock(filepath.Clean(runtimeRoot)+".air-worker-bootstrap.lock", timeout)
	if err != nil {
		productUnlock()
		return nil, err
	}
	return func() {
		runtimeUnlock()
		productUnlock()
	}, nil
}
