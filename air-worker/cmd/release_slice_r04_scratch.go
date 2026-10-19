package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
