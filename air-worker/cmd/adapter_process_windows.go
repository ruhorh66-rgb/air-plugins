//go:build windows

package main

import (
	"syscall"
	"time"
	"unsafe"
)

var (
	adapterKernel32            = syscall.NewLazyDLL("kernel32.dll")
	adapterOpenProcess         = adapterKernel32.NewProc("OpenProcess")
	adapterWaitForSingleObject = adapterKernel32.NewProc("WaitForSingleObject")
	adapterGetProcessTimes     = adapterKernel32.NewProc("GetProcessTimes")
	adapterCloseHandle         = adapterKernel32.NewProc("CloseHandle")
)

func adapterProcessMatches(pid int, receiptStartedAt time.Time) bool {
	if pid <= 0 || receiptStartedAt.IsZero() || receiptStartedAt.After(time.Now().Add(5*time.Second)) {
		return false
	}
	const (
		synchronize                    = 0x00100000
		processQueryLimitedInformation = 0x00001000
		waitTimeout                    = 258
	)
	handle, _, _ := adapterOpenProcess.Call(synchronize|processQueryLimitedInformation, 0, uintptr(uint32(pid)))
	if handle == 0 {
		return false
	}
	defer adapterCloseHandle.Call(handle)
	result, _, _ := adapterWaitForSingleObject.Call(handle, 0)
	if result != waitTimeout {
		return false
	}
	var creation, exit, kernel, user syscall.Filetime
	ok, _, _ := adapterGetProcessTimes.Call(
		handle,
		uintptr(unsafe.Pointer(&creation)),
		uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)),
		uintptr(unsafe.Pointer(&user)),
	)
	if ok == 0 {
		return false
	}
	createdAt := time.Unix(0, creation.Nanoseconds()).UTC()
	// The receipt may be written a few seconds after process creation. A reused
	// PID points to a process created after the old receipt and is rejected.
	return !createdAt.After(receiptStartedAt.UTC().Add(5 * time.Second))
}
