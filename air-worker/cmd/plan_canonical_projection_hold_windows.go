//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// n124ReplayProjectionHold is a Windows source-only prototype, not an
// installed product capability or a GitHub canonical generation grant.
// Caller retains all handles until Close (including across the final read).
type n124ReplayProjectionHold struct {
	ancestors        []*candidateOriginLock
	directoryHandles []syscall.Handle
	fileHandles      []syscall.Handle
	paths            []string
	ids              []nativeInputIdentity
	snapshots        [][]byte
	directories      []string
}

const (
	n124ProjectionFileLimit      = 4 * 1024 * 1024
	n124ProjectionEntryLimit     = 4096
	n124ProjectionTotalLimit     = 32 * 1024 * 1024
	n124ProjectionDirectoryLimit = 256
)

func (h *n124ReplayProjectionHold) Close() {
	if h == nil {
		return
	}
	for i := len(h.fileHandles) - 1; i >= 0; i-- {
		_ = syscall.CloseHandle(h.fileHandles[i])
	}
	h.fileHandles = nil
	for i := len(h.directoryHandles) - 1; i >= 0; i-- {
		_ = syscall.CloseHandle(h.directoryHandles[i])
	}
	h.directoryHandles = nil
	for i := len(h.ancestors) - 1; i >= 0; i-- {
		h.ancestors[i].Close()
	}
	h.ancestors = nil
}

// holdN124ReplayProjection holds read-only Windows handles to every native
// PLAN/node/LEARN file and its ancestor directories in a disposable fixture.
// GENERIC_READ + FILE_SHARE_READ prevents concurrent writers and renames.
// The existing candidateOriginLock verifies fixed-local NTFS/ReFS identity.
// This DOES NOT authorize mutation or defeat changes made after Close.
func holdN124ReplayProjection(root string) (*n124ReplayProjectionHold, error) {
	h := &n124ReplayProjectionHold{}
	fail := func(e error) (*n124ReplayProjectionHold, error) {
		h.Close()
		return nil, e
	}
	if root == "" || !filepath.IsAbs(root) || filepath.Clean(root) != root || strings.HasPrefix(root, "\\\\") {
		return fail(errors.New("native projection requires local absolute root"))
	}
	// This source-only prototype must NEVER lock a production vault or
	// masquerade as the new native publication authority.
	if err := candidatePublishFixture(root); err != nil {
		return fail(fmt.Errorf("unapproved native hold outside disposable fixture: %w", err))
	}
	anchor, err := lockCandidateOrigin(root)
	if err != nil {
		return fail(fmt.Errorf("native projection root fence: %w", err))
	}
	h.ancestors = append(h.ancestors, anchor)
	seen := map[string]bool{}
	entries, total := 0, 0
	pinDirectory := func(dir string) error {
		if len(h.directories) >= n124ProjectionDirectoryLimit {
			return errors.New("native projection exceeds directory count limit")
		}
		if err := candidateNoLinks(dir); err != nil {
			return err
		}
		lock, err := lockCandidateOrigin(dir)
		if err != nil {
			return err
		}
		h.ancestors = append(h.ancestors, lock)
		p, err := syscall.UTF16PtrFromString(dir)
		if err != nil {
			return err
		}
		const fileListDirectory = 0x0001
		const fileReadAttributes = 0x0080
		// A live FILE_LIST_DIRECTORY handle enforces share-access checks;
		// no share-write and no share-delete are granted to other openers.
		handle, err := syscall.CreateFile(p, fileListDirectory|fileReadAttributes,
			syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING,
			syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return fmt.Errorf("native projection directory share fence unavailable: %w", err)
		}
		h.directoryHandles = append(h.directoryHandles, handle)
		id, err := nativeInputIdentityOfHandle(handle)
		if err != nil || id.High == 0 && id.Low == 0 {
			return errors.New("native directory physical identity unavailable")
		}
		if err := lock.Verify(); err != nil {
			return err
		}
		h.directories = append(h.directories, dir)
		return nil
	}
	pinFile := func(name string) error {
		entries++
		if entries > n124ProjectionEntryLimit {
			return errors.New("native projection file-count limit exceeded")
		}
		if err := candidateNoLinks(name); err != nil {
			return err
		}
		before, err := os.Lstat(name)
		if err != nil || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > n124ProjectionFileLimit {
			return fmt.Errorf("native file absent, unsafe or oversized: %s", name)
		}
		p, err := syscall.UTF16PtrFromString(name)
		if err != nil {
			return err
		}
		handle, err := syscall.CreateFile(p, syscall.GENERIC_READ,
			syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING,
			syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return fmt.Errorf("native file cannot be locked against writers: %w", err)
		}
		h.fileHandles = append(h.fileHandles, handle)
		id, err := nativeInputIdentityOfHandle(handle)
		if err != nil || id.High == 0 && id.Low == 0 {
			return errors.New("native file physical identity unavailable")
		}
		var attributes syscall.ByHandleFileInformation
		if err := syscall.GetFileInformationByHandle(handle, &attributes); err != nil ||
			attributes.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0 ||
			attributes.NumberOfLinks != 1 {
			return errors.New("native file reparse/hardlink/identity untrusted")
		}
		if pathID, err := nativeInputPathIdentity(name); err != nil || pathID != id {
			return errors.New("native source path changed during handle acquisition")
		}
		f, err := os.Open(name)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(f, n124ProjectionFileLimit+1))
		closeErr := f.Close()
		if readErr != nil || closeErr != nil || len(data) > n124ProjectionFileLimit {
			return errors.New("native file bounded content unavailable")
		}
		total += len(data)
		if total > n124ProjectionTotalLimit {
			return errors.New("native projection aggregate byte limit exceeded")
		}
		key := strings.ToLower(filepath.Clean(name))
		if seen[key] {
			return errors.New("duplicate native file path")
		}
		seen[key] = true
		h.paths = append(h.paths, name)
		h.ids = append(h.ids, id)
		h.snapshots = append(h.snapshots, data)
		return nil
	}
	// Pin the authority declaration itself so it cannot change after
	// validation and before returning source-only readback.
	if err := pinFile(filepath.Join(root, "plan-authority.json")); err != nil {
		return fail(err)
	}
	if err := candidatePublishFixture(root); err != nil {
		return fail(fmt.Errorf("fixture authority changed while pinning: %w", err))
	}
	if err := pinFile(filepath.Join(root, "PLAN.md")); err != nil {
		return fail(err)
	}
	var walk func(string) error
	walk = func(dir string) error {
		if err := pinDirectory(dir); err != nil {
			return err
		}
		fd, err := os.Open(dir)
		if err != nil {
			return err
		}
		items, readErr := fd.ReadDir(n124ProjectionEntryLimit + 1)
		closeErr := fd.Close()
		if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil || len(items) > n124ProjectionEntryLimit {
			return errors.New("native directory cannot be enumerated within bounds")
		}
		for _, item := range items {
			path := filepath.Join(dir, item.Name())
			if err := candidateNoLinks(path); err != nil {
				return err
			}
			if item.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
			} else if err := pinFile(path); err != nil {
				return err
			}
		}
		return nil
	}
	// Missing plan or LEARN is NOT evidence of an unchanged native
	// generation. A live allocation must prove both before promotion.
	for _, dir := range []string{"plan", "learn"} {
		if err := walk(filepath.Join(root, dir)); err != nil {
			return fail(err)
		}
	}
	if err := h.Verify(); err != nil {
		return fail(err)
	}
	return h, nil
}

func (h *n124ReplayProjectionHold) Verify() error {
	if h == nil || len(h.paths) == 0 || len(h.fileHandles) != len(h.paths) ||
		len(h.ids) != len(h.paths) || len(h.snapshots) != len(h.paths) {
		return errors.New("native generation hold incomplete")
	}
	for _, lock := range h.ancestors {
		if err := lock.Verify(); err != nil {
			return err
		}
	}
	for i, path := range h.paths {
		if err := candidateNoLinks(path); err != nil {
			return err
		}
		id, err := nativeInputIdentityOfHandle(h.fileHandles[i])
		if err != nil || id != h.ids[i] {
			return errors.New("native held file identity changed")
		}
		pathID, err := nativeInputPathIdentity(path)
		if err != nil || pathID != id {
			return errors.New("native file path identity changed")
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		actual, readErr := io.ReadAll(io.LimitReader(file, n124ProjectionFileLimit+1))
		closeErr := file.Close()
		if readErr != nil || closeErr != nil || !bytes.Equal(actual, h.snapshots[i]) {
			return errors.New("native held file content changed")
		}
	}
	// Inspect directory membership as an extra fail-closed check. A
	// future production protocol must additionally maintain a durable
	// cross-process generation/owner lease through the entire mutation.
	all := make(map[string]bool, len(h.paths))
	for _, p := range h.paths {
		all[p] = true
	}
	for _, dir := range h.directories {
		fd, err := os.Open(dir)
		if err != nil {
			return err
		}
		items, readErr := fd.ReadDir(n124ProjectionEntryLimit + 1)
		closeErr := fd.Close()
		if (readErr != nil && !errors.Is(readErr, io.EOF)) || closeErr != nil || len(items) > n124ProjectionEntryLimit {
			return errors.New("native directory membership changed beyond bounds")
		}
		for _, item := range items {
			p := filepath.Join(dir, item.Name())
			found := false
			if item.IsDir() {
				for _, x := range h.directories {
					if x == p {
						found = true
						break
					}
				}
			} else {
				found = all[p]
			}
			if !found {
				return errors.New("native projection gained an unexpected directory entry")
			}
		}
	}
	return nil
}
