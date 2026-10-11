//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"syscall"
)

func lockCandidateConfig(repo string) (*candidateOriginLock, error) {
	l, err := lockCandidateOrigin(filepath.Join(repo, ".git"))
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*candidateOriginLock, error) { l.Close(); return nil, err }
	path := filepath.Join(repo, ".git", "config")
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return fail(err)
	}
	// Data read access enforces sharing: Git may read, but writers cannot edit or replace config.
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
	if err != nil {
		return fail(err)
	}
	l.handles = append(l.handles, h)
	id, err := nativeInputIdentityOfHandle(h)
	if err != nil {
		return fail(err)
	}
	if id.High == 0 && id.Low == 0 {
		return fail(fmt.Errorf("config filesystem has no reliable file identity"))
	}
	l.paths = append(l.paths, path)
	l.identities = append(l.identities, id)
	if err := l.Verify(); err != nil {
		return fail(err)
	}
	var info syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(h, &info); err != nil {
		return fail(err)
	}
	if info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 || info.NumberOfLinks != 1 {
		return fail(fmt.Errorf("regular unlinked config required"))
	}
	return l, nil
}
