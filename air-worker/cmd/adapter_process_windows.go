//go:build windows

package main

import "syscall"

var (
	adapterKernel32            = syscall.NewLazyDLL("kernel32.dll")
	adapterOpenProcess         = adapterKernel32.NewProc("OpenProcess")
	adapterWaitForSingleObject = adapterKernel32.NewProc("WaitForSingleObject")
	adapterCloseHandle         = adapterKernel32.NewProc("CloseHandle")
)

func adapterProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	const (
		synchronize = 0x00100000
		waitTimeout = 258
	)
	handle, _, _ := adapterOpenProcess.Call(synchronize, 0, uintptr(uint32(pid)))
	if handle == 0 {
		return false
	}
	defer adapterCloseHandle.Call(handle)
	result, _, _ := adapterWaitForSingleObject.Call(handle, 0)
	return result == waitTimeout
}
