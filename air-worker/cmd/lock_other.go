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

func cmdTray(argv []string) int {
	fmt.Fprint(os.Stderr, "значок в области уведомлений сделан для Windows"+lineEnding)
	return 2
}
