//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var bootstrapKernel = syscall.NewLazyDLL("kernel32.dll")
var bootstrapLockFile = bootstrapKernel.NewProc("LockFileEx")
var bootstrapUnlockFile = bootstrapKernel.NewProc("UnlockFileEx")

func validateWindowsBootstrapRuntimeSpelling(path string) error {
	clean := filepath.Clean(strings.TrimSpace(path))
	lower := strings.ToLower(clean)
	if strings.HasPrefix(lower, `\\?\`) || strings.HasPrefix(lower, `\\.\`) {
		return errors.New("runtime root uses unsupported Windows device-path spelling")
	}
	volume := filepath.VolumeName(clean)
	rest := strings.TrimPrefix(clean, volume)
	for _, part := range strings.FieldsFunc(rest, func(r rune) bool { return r == '\\' || r == '/' }) {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return fmt.Errorf("runtime root uses unsupported Windows trailing-dot/space alias component %q", part)
		}
	}
	return nil
}

func canonicalSharedLearningBootstrapRuntime(runtimeRoot string) (string, error) {
	clean := filepath.Clean(strings.TrimSpace(runtimeRoot))
	if err := validateWindowsBootstrapRuntimeSpelling(clean); err != nil {
		return "", err
	}
	if st, err := os.Stat(clean); err == nil {
		if !st.IsDir() {
			return "", errors.New("runtime root exists and is not a directory")
		}
		f, err := os.Open(clean)
		if err != nil {
			return "", err
		}
		defer f.Close()
		resolved, err := openedFileFinalPath(f)
		if err != nil {
			return "", fmt.Errorf("resolve existing runtime root identity: %w", err)
		}
		return filepath.Clean(resolved), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	parent := filepath.Dir(clean)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	f, err := os.Open(parent)
	if err != nil {
		return "", err
	}
	defer f.Close()
	resolvedParent, err := openedFileFinalPath(f)
	if err != nil {
		return "", fmt.Errorf("resolve runtime parent identity: %w", err)
	}
	return filepath.Join(resolvedParent, filepath.Base(clean)), nil
}

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
