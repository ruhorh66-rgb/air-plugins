package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// R04 must neither follow a Windows junction in a scratch ancestor nor
// create through a mutable textual path. os.Root confines the child creation
// to the original Windows directory HANDLE, not an alias that may move.
func releaseR04OpenPinnedScratch(scratch string) (*os.Root, error) {
	if scratch == "" || !filepath.IsAbs(scratch) || filepath.Clean(scratch) != scratch {
		return nil, errors.New("R04: scratch root identity is not absolute and canonical")
	}
	for ancestor := scratch; ; ancestor = filepath.Dir(ancestor) {
		if err := n093CheckOrdinaryDir(ancestor); err != nil {
			return nil, fmt.Errorf("R04: unsafe scratch ancestor %q: %w", ancestor, err)
		}
		if filepath.Dir(ancestor) == ancestor {
			break
		}
	}
	root, err := os.OpenRoot(scratch)
	if err != nil {
		return nil, fmt.Errorf("R04: cannot pin physical scratch root: %w", err)
	}
	parent, err := root.Open(".")
	if err != nil {
		_ = root.Close()
		return nil, err
	}
	physical, pathErr := openedFileFinalPath(parent)
	opened, statErr := parent.Stat()
	plain, lstatErr := os.Lstat(scratch)
	_ = parent.Close()
	if pathErr != nil || statErr != nil || lstatErr != nil ||
		!sameLearningPath(physical, scratch) || !os.SameFile(opened, plain) {
		_ = root.Close()
		return nil, errors.New("R04: physical scratch root was redirected or changed")
	}
	return root, nil
}

// This verification is intentionally performed AFTER the caller has pinned
// the target and every replaceable ancestor with non-FILE_SHARE_DELETE handles.
// The first signed os.Root check can safely prepare the empty directory, but
// it cannot authorize child execution: a rename in the gap could replace it.
// Reopen through os.Root while the handles are held, require the same physical
// directory at the signed path, and reject any injected file before child use.
func releaseR04RecheckHeldScratchTarget(target string) error {
	if target == "" || !filepath.IsAbs(target) || filepath.Clean(target) != target ||
		!strings.HasPrefix(filepath.Base(target), "air-worker-r02-capture-") {
		return errors.New("R04: held target has invalid signed scratch identity")
	}
	root, err := releaseR04OpenPinnedScratch(filepath.Dir(target))
	if err != nil {
		return fmt.Errorf("R04: held scratch ancestor revalidation failed: %w", err)
	}
	defer root.Close()
	opened, err := root.Open(filepath.Base(target))
	if err != nil {
		return fmt.Errorf("R04: held signed scratch child cannot be reopened: %w", err)
	}
	defer opened.Close()
	physical, pathErr := openedFileFinalPath(opened)
	openedInfo, statErr := opened.Stat()
	currentInfo, lstatErr := os.Lstat(target)
	if pathErr != nil || statErr != nil || lstatErr != nil ||
		!openedInfo.IsDir() || !currentInfo.IsDir() ||
		openedInfo.Mode()&os.ModeSymlink != 0 || currentInfo.Mode()&os.ModeSymlink != 0 ||
		!sameLearningPath(physical, target) || !os.SameFile(openedInfo, currentInfo) {
		return errors.New("R04: signed scratch child changed physical identity before held use")
	}
	entries, readErr := opened.ReadDir(1)
	if len(entries) != 0 || (readErr != nil && !errors.Is(readErr, io.EOF)) {
		return errors.New("R04: held signed scratch target was replaced or modified before native child")
	}
	return nil
}
