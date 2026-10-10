//go:build windows

package main

import (
	"syscall"
	"testing"
)

func TestN108NativeMutexDeniedHandleNeverPassesAsLocked(t *testing.T) {
	lock, ok, err := checkedNativeFeedbackMutex(
		nativeFeedbackUseLockName("p", "r"),
		func(_ *uint16) (uintptr, uintptr, error) { return 0, 0, syscall.Errno(5) },
	)
	if ok || lock != nil || err == nil {
		t.Fatalf("CreateMutexW access denied was mistaken for a protected operation: lock=%v ok=%v err=%v", lock, ok, err)
	}
}

func TestN108NativeMutexZeroWithoutWindowsErrorFailsClosed(t *testing.T) {
	lock, ok, err := checkedNativeFeedbackMutex(
		nativeFeedbackUseLockName("p2", "r2"),
		func(_ *uint16) (uintptr, uintptr, error) { return 0, 0, syscall.Errno(0) },
	)
	if ok || lock != nil || err == nil {
		t.Fatalf("zero handle with errno0 must deny use: lock=%v ok=%v err=%v", lock, ok, err)
	}
}

func TestN108NativeMutexInvalidNameCannotFailOpen(t *testing.T) {
	name := "Local" + string(byte(0)) + "invalid"
	lock, ok, err := checkedNativeFeedbackMutex(name,
		func(_ *uint16) (uintptr, uintptr, error) {
			t.Fatal("Windows must reject name before syscall")
			return 0, 0, nil
		})
	if ok || lock != nil || err == nil {
		t.Fatalf("invalid named mutex must not grant use: lock=%v ok=%v err=%v", lock, ok, err)
	}
}
