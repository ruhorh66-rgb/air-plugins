//go:build !windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func acquireSharedLearningBootstrapLock(product string, timeout time.Duration) (func(), error) {
	dir := filepath.Join(product, ".air-worker", "learn")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "bootstrap.lock"), os.O_CREATE|os.O_RDWR, 0o600)
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
