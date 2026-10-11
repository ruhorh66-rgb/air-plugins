//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type candidateOriginLock struct {
	paths      []string
	handles    []syscall.Handle
	identities []nativeInputIdentity
}

func lockCandidateOrigin(root string) (*candidateOriginLock, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.HasPrefix(root, `\\`) {
		return nil, fmt.Errorf("origin fence requires a local absolute path")
	}
	volume, err := syscall.UTF16PtrFromString(filepath.VolumeName(root) + `\`)
	if err != nil {
		return nil, err
	}
	driveType, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW").Call(uintptr(unsafe.Pointer(volume)))
	if driveType != 3 {
		return nil, fmt.Errorf("origin fence requires a fixed local volume")
	}
	l := &candidateOriginLock{}
	for p := root; ; p = filepath.Dir(p) {
		l.paths = append(l.paths, p)
		if filepath.Dir(p) == p {
			break
		}
	}
	// Pin ancestors first so no path component can be exchanged while opening a child.
	for i, j := 0, len(l.paths)-1; i < j; i, j = i+1, j-1 {
		l.paths[i], l.paths[j] = l.paths[j], l.paths[i]
	}
	fail := func(err error) (*candidateOriginLock, error) { l.Close(); return nil, err }
	for _, p := range l.paths {
		name, err := syscall.UTF16PtrFromString(p)
		if err != nil {
			return fail(err)
		}
		const fileListDirectory = 0x0001
		const fileReadAttributes = 0x0080
		// FILE_READ_ATTRIBUTES alone is exempt from share checks; listing access makes the no-delete share fence effective.
		h, err := syscall.CreateFile(name, fileListDirectory|fileReadAttributes, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return fail(err)
		}
		l.handles = append(l.handles, h)
		id, err := nativeInputIdentityOfHandle(h)
		if err != nil {
			return fail(err)
		}
		if id.High == 0 && id.Low == 0 {
			return fail(fmt.Errorf("filesystem has no reliable file identity"))
		}
		l.identities = append(l.identities, id)
	}
	var fsName [32]uint16
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetVolumeInformationByHandleW")
	if err := proc.Find(); err != nil {
		return fail(err)
	}
	ok, _, err := proc.Call(uintptr(l.handles[len(l.handles)-1]), 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)))
	if ok == 0 {
		return fail(err)
	}
	if fs := syscall.UTF16ToString(fsName[:]); fs != "NTFS" && fs != "ReFS" {
		return fail(fmt.Errorf("unsupported origin fence filesystem: %s", fs))
	}
	if err := l.Verify(); err != nil {
		return fail(err)
	}
	return l, nil
}

func (l *candidateOriginLock) Verify() error {
	for i, p := range l.paths {
		if err := candidateNoLinks(p); err != nil {
			return err
		}
		id, err := nativeInputPathIdentity(p)
		if err != nil {
			return err
		}
		held, err := nativeInputIdentityOfHandle(l.handles[i])
		if err != nil {
			return err
		}
		if id != l.identities[i] || held != id {
			return fmt.Errorf("origin fence identity changed")
		}
	}
	return nil
}

func (l *candidateOriginLock) Close() {
	for i := len(l.handles) - 1; i >= 0; i-- {
		syscall.CloseHandle(l.handles[i])
	}
	l.handles = nil
}
