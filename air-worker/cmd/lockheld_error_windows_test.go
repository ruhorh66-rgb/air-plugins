//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestLockHeldOpenErrorsFailClosed(t *testing.T) {
	validName := fmt.Sprintf(`Local\air-worker-lockheld-%d-%s`, syscall.Getpid(), strings.ReplaceAll(t.Name(), "/", "-"))
	if lockHeld(validName) {
		t.Fatal("a nonexistent mutex is reported held")
	}

	// This valid name has a missing namespace component, so OpenMutexW must fail
	// with ERROR_PATH_NOT_FOUND rather than ERROR_FILE_NOT_FOUND.
	invalidName := validName + `\child`
	n, err := syscall.UTF16PtrFromString(invalidName)
	if err != nil {
		t.Fatal(err)
	}
	h, _, callErr := procOpenMutexW.Call(mutexAllAccess, 0, uintptr(unsafe.Pointer(n)))
	if h != 0 {
		procCloseHandleLock.Call(h)
		t.Fatal("OpenMutexW unexpectedly opened a mutex with an invalid subpath")
	}
	if errno, ok := callErr.(syscall.Errno); !ok || errno != syscall.Errno(3) {
		t.Fatalf("OpenMutexW error = %v, want ERROR_PATH_NOT_FOUND (3)", callErr)
	}
	if !lockHeld(invalidName) {
		t.Error("OpenMutexW API failure is reported free")
	}

	lockedName := validName + "-live"
	lock, ok := acquireLock(lockedName)
	if !ok {
		t.Fatal("could not create test mutex")
	}
	t.Cleanup(lock.release)
	if !lockHeld(lockedName) {
		t.Error("an existing mutex is reported free")
	}

	if !lockHeld(validName + "\x00invalid") {
		t.Error("UTF-16 conversion failure is reported free")
	}
}
