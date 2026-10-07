//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func canonicalSharedLearningBootstrapRuntime(runtimeRoot string) (string, error) {
	clean := filepath.Clean(runtimeRoot)
	if st, err := os.Stat(clean); err == nil {
		if !st.IsDir() {
			return "", errors.New("runtime root exists and is not a directory")
		}
		resolved, err := filepath.EvalSymlinks(clean)
		if err != nil {
			return "", err
		}
		return filepath.Clean(resolved), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	parent := filepath.Dir(clean)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", err
	}
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		return "", err
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
	deadline := time.Now().Add(timeout)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
				_ = f.Close()
			}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = f.Close()
			return nil, err
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
