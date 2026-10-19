//go:build !windows

package main

import (
	"errors"
	"os"
	"syscall"
)

func n093RejectHardlinkedEvidence(f *os.File) error {
	if f == nil {
		return errors.New("N093: missing evidence handle")
	}
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		return errors.New("N093: runtime evidence has more than one hardlink")
	}
	return nil
}
