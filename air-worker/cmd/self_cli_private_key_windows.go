//go:build windows

package main

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"
)

type cliDataBlob struct {
	cbData uint32
	pbData *byte
}

var ownCLICrypt32 = syscall.NewLazyDLL("crypt32.dll")
var ownCLIProtect = ownCLICrypt32.NewProc("CryptProtectData")
var ownCLIUnprotect = ownCLICrypt32.NewProc("CryptUnprotectData")
var ownCLILocalFree = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalFree")

const cliDPAPIUIForbidden = uintptr(0x1)

func cliDPAPI(input []byte, encrypt bool) ([]byte, error) {
	if len(input) == 0 || len(input) > 16*1024 {
		return nil, errors.New("invalid own CLI DPAPI input")
	}
	in := cliDataBlob{cbData: uint32(len(input)), pbData: &input[0]}
	var out cliDataBlob
	proc := ownCLIProtect
	if !encrypt {
		proc = ownCLIUnprotect
	}
	ret, _, callErr := proc.Call(
		uintptr(unsafe.Pointer(&in)), 0, 0, 0, 0,
		cliDPAPIUIForbidden, uintptr(unsafe.Pointer(&out)),
	)
	runtime.KeepAlive(input)
	if ret == 0 {
		return nil, callErr
	}
	if out.pbData == nil || out.cbData == 0 || out.cbData > 16*1024 {
		if out.pbData != nil {
			ownCLILocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
		}
		return nil, errors.New("invalid own CLI DPAPI output")
	}
	defer ownCLILocalFree.Call(uintptr(unsafe.Pointer(out.pbData)))
	return append([]byte(nil), unsafe.Slice(out.pbData, int(out.cbData))...), nil
}

// DPAPI defaults to the current Windows user scope (never machine-wide).
// An unrelated account reading the on-disk envelope cannot brute-force
// low-entropy argv values without the protected per-user key.
func sealOwnCLIKey(key []byte) ([]byte, error)    { return cliDPAPI(key, true) }
func unsealOwnCLIKey(data []byte) ([]byte, error) { return cliDPAPI(data, false) }

func validatePrivateCLIKeyPath(path string) error {
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	attrs, err := syscall.GetFileAttributes(ptr)
	if err != nil {
		return err
	}
	if attrs&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("own CLI private key is a reparse point")
	}
	return nil
}
