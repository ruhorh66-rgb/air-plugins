//go:build !windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Замки и значок описаны в терминах Windows: именованные объекты ядра и область
// уведомлений. На других системах продукт не работает, и подделывать замок нельзя —
// заглушка, которая всегда отвечает «взял», превратила бы защиту от одновременной
// работы в её видимость.

type osLock struct{}

func lockName(kind, path string) string { return kind + ":" + path }

func acquireLock(name string) (*osLock, bool) { return &osLock{}, true }

func (l *osLock) release() {}

func lockHeld(name string) bool { return false }

func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// writeFileAtomicDurable is reserved for recovery intents whose bytes must be
// flushed before publication. POSIX additionally syncs the containing directory
// so the rename itself is durable across a power-loss boundary.
func writeFileAtomicDurable(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("sync durable intent directory: %w", err)
	}
	syncErr := d.Sync()
	closeErr := d.Close()
	if syncErr != nil {
		return fmt.Errorf("sync durable intent directory: %w", syncErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func cmdTray(argv []string) int {
	fmt.Fprint(os.Stderr, "значок в области уведомлений сделан для Windows"+lineEnding)
	return 2
}
