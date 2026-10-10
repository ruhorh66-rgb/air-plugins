//go:build windows

package main

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// The general AirWorker lock deliberately retains its old fail-open policy.
// Proof-bearing learning requires a DIFFERENT rule: if Windows did not return
// an owning kernel object handle, it cannot publish machine evidence.
func checkedNativeFeedbackMutex(name string, create func(*uint16) (uintptr, uintptr, error)) (*osLock, bool, error) {
	utf16Name, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, false, fmt.Errorf("invalid native feedback mutex identity: %w", err)
	}
	handle, _, callErr := create(utf16Name)
	if handle == 0 {
		if callErr == nil || errors.Is(callErr, syscall.Errno(0)) {
			return nil, false, errors.New("native feedback mutex handle was zero without an OS result")
		}
		return nil, false, fmt.Errorf("native feedback mutex could not be acquired: %w", callErr)
	}
	errno, hasErrno := callErr.(syscall.Errno)
	if hasErrno && uintptr(errno) == errAlreadyExistsLock {
		procCloseHandleLock.Call(handle)
		return nil, false, nil
	}
	if callErr != nil && (!hasErrno || errno != syscall.Errno(0)) {
		procCloseHandleLock.Call(handle)
		return nil, false, fmt.Errorf("native feedback mutex returned unexpected OS status: %w", callErr)
	}
	return &osLock{h: syscall.Handle(handle)}, true, nil
}

func acquireNativeFeedbackUseLock(name string) (*osLock, bool, error) {
	return checkedNativeFeedbackMutex(name, func(p *uint16) (uintptr, uintptr, error) {
		return procCreateMutexWLock.Call(0, 1, uintptr(unsafe.Pointer(p)))
	})
}
