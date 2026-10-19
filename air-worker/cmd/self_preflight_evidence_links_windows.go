//go:build windows

package main

import (
	"errors"
	"os"
	"syscall"
)

// Even an NTFS hardlink has a regular file mode. A second file name outside
// the module runtime must not become independent module-owned evidence.
func n093RejectHardlinkedEvidence(f *os.File) error {
	if f == nil {
		return errors.New("N093: missing evidence handle")
	}
	var data syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &data); err != nil {
		return err
	}
	if data.NumberOfLinks != 1 {
		return errors.New("N093: runtime evidence has more than one NTFS hardlink")
	}
	return nil
}
