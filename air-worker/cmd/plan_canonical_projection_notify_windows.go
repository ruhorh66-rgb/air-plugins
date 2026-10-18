//go:build windows

package main

import (
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// FindFirstChangeNotificationW creates a kernel change-notification handle.
// Unlike a final os.ReadDir snapshot, the notification remains signaled after
// an ABA create+delete until FindNextChangeNotification is called. We NEVER
// call FindNext: even one native change invalidates this source-only hold.
//
// Windows API: https://learn.microsoft.com/windows/win32/api/fileapi/nf-fileapi-findfirstchangenotificationw
// and https://learn.microsoft.com/windows/win32/api/fileapi/nf-fileapi-findclosechangenotification
// This is NOT a production authority, a durable cross-process publisher
// generation, or permission to write native PLAN/N/LEARN.
var (
	n124ChangeKernel32        = syscall.NewLazyDLL("kernel32.dll")
	n124FindFirstChangeNotify = n124ChangeKernel32.NewProc("FindFirstChangeNotificationW")
	n124FindCloseChangeNotify = n124ChangeKernel32.NewProc("FindCloseChangeNotification")
)

const (
	n124NotifyFileName = 0x00000001
	n124NotifyDirName  = 0x00000002

	// Existing native file handles already deny writes and renames. Watch
	// NAMES only: metadata/size notification can spuriously fire on read-only
	// enumeration, obscuring the create/delete (ABA) contract. A transient
	// new leaf changes at least FILE_NAME even if it is later removed.
	// Subtree mode covers plan/event-outbox before its own watch is armed.
	n124NativeNotifyFilter = n124NotifyFileName | n124NotifyDirName
)

type n124NativeDirectoryWatch struct {
	path   string
	handle syscall.Handle
}

func n124OpenNativeDirectoryWatch(dir string) (*n124NativeDirectoryWatch, error) {
	ptr, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return nil, err
	}
	// watchSubtree=true provides coverage for a nested directory while its
	// own handle/watch is being acquired. The parent native directory handle
	// is already pinned against delete/rename by the caller.
	h, _, callErr := n124FindFirstChangeNotify.Call(
		uintptr(unsafe.Pointer(ptr)), uintptr(1), uintptr(n124NativeNotifyFilter))
	runtime.KeepAlive(ptr)
	if h == uintptr(syscall.InvalidHandle) || h == 0 {
		return nil, fmt.Errorf("native directory change notification unavailable for %s: %w", dir, callErr)
	}
	return &n124NativeDirectoryWatch{path: dir, handle: syscall.Handle(h)}, nil
}

func (w *n124NativeDirectoryWatch) Verify() error {
	if w == nil || w.handle == 0 || w.handle == syscall.InvalidHandle {
		return errors.New("native directory change notification handle missing")
	}
	state, err := syscall.WaitForSingleObject(w.handle, 0)
	if err != nil {
		return fmt.Errorf("native directory change notification readback failed: %w", err)
	}
	switch state {
	case syscall.WAIT_TIMEOUT:
		return nil
	case syscall.WAIT_OBJECT_0:
		// Do not call FindNextChangeNotification: clearing a signal would
		// erase causal evidence of even a short-lived file-name change.
		return fmt.Errorf("native directory change notification observed: %s", w.path)
	default:
		return fmt.Errorf("native directory change notification untrusted state %d: %s", state, w.path)
	}
}

func (w *n124NativeDirectoryWatch) Close() {
	if w == nil || w.handle == 0 || w.handle == syscall.InvalidHandle {
		return
	}
	h := w.handle
	w.handle = 0
	_, _, _ = n124FindCloseChangeNotify.Call(uintptr(h))
}
