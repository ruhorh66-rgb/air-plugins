//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

type nativeInputIdentity struct {
	Volume uint32
	High   uint32
	Low    uint32
}

func nativeInputIdentityOfHandle(handle syscall.Handle) (nativeInputIdentity, error) {
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(handle, &info); err != nil {
		return nativeInputIdentity{}, err
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return nativeInputIdentity{}, fmt.Errorf("reparse-point feedback input is prohibited")
	}
	return nativeInputIdentity{Volume: info.VolumeSerialNumber, High: info.FileIndexHigh, Low: info.FileIndexLow}, nil
}

func nativeInputPathIdentity(path string) (nativeInputIdentity, error) {
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nativeInputIdentity{}, err
	}
	handle, err := syscall.CreateFile(
		name, 0,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0,
	)
	if err != nil {
		return nativeInputIdentity{}, err
	}
	defer syscall.CloseHandle(handle)
	return nativeInputIdentityOfHandle(handle)
}

func nativeInputOpenedIdentity(file *os.File) (nativeInputIdentity, error) {
	return nativeInputIdentityOfHandle(syscall.Handle(file.Fd()))
}

func nativeInputSameIdentity(a, b nativeInputIdentity) bool { return a == b }
