package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Native feedback input is a local document, not a permission to follow
// filesystem aliases. Identity is checked before and AFTER reading the
// opened handle so a leaf swap during validation does not change the
// evidence silently. The same check covers every parent junction/reparse.
type nativeInputPart struct {
	path string
	info os.FileInfo
}

func inspectNativeInputPath(path string, max int) ([]nativeInputPart, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("feedback -data-file must be absolute")
	}
	path = filepath.Clean(path)
	volume := filepath.VolumeName(path)
	// On POSIX a leading slash is the volume root; Windows uses R:\
	// or a UNC volume returned by filepath.VolumeName.
	root := volume + string(filepath.Separator)
	relative := strings.TrimPrefix(path, root)
	if !filepath.IsLocal(relative) {
		return nil, errors.New("feedback data-file is not within an absolute local volume")
	}
	parts := strings.Split(relative, string(filepath.Separator))
	current := root
	var seen []nativeInputPart
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, errors.New("feedback data-file path has an unsafe component")
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("feedback data-file traverses a symlink/reparse: %s", current)
		}
		// os.Readlink also recognizes Windows junctions, which may not
		// report the same FileMode flags as standard symlinks.
		if _, err := os.Readlink(current); err == nil {
			return nil, fmt.Errorf("feedback data-file traverses a junction: %s", current)
		}
		if i < len(parts)-1 {
			if !info.IsDir() {
				return nil, errors.New("feedback data-file parent is not a directory")
			}
		} else if !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > int64(max) {
			return nil, errors.New("feedback data file must be a bounded regular JSON document")
		}
		seen = append(seen, nativeInputPart{path: current, info: info})
	}
	return seen, nil
}

func readNativeFeedbackDataSafe(path string, max int) ([]byte, error) {
	return readNativeFeedbackDataSafeWithOpen(path, max, os.Open)
}

// The opener is injectable ONLY in isolated tests to reproduce a leaf
// replacement between stat and open. Production uses os.Open unchanged.
func readNativeFeedbackDataSafeWithOpen(path string, max int, opener func(string) (*os.File, error)) ([]byte, error) {
	seen, err := inspectNativeInputPath(path, max)
	if err != nil {
		return nil, err
	}
	file, err := opener(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, err
	}
	final := seen[len(seen)-1].info
	if !opened.Mode().IsRegular() || opened.Size() < 2 || opened.Size() > int64(max) ||
		opened.Size() != final.Size() || !opened.ModTime().Equal(final.ModTime()) ||
		!os.SameFile(final, opened) {
		return nil, errors.New("feedback data file changed identity before its handle was opened")
	}
	body, err := io.ReadAll(io.LimitReader(file, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > max || int64(len(body)) != opened.Size() {
		return nil, errors.New("feedback data-file changed size during read")
	}
	// Reparse swaps at any ancestor or the leaf are denied even when the
	// bytes happened to match. A caller must obtain one stable source.
	for _, part := range seen {
		info, err := os.Lstat(part.path)
		if err != nil || !os.SameFile(part.info, info) || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("feedback data-file changed path identity after read: %s", part.path)
		}
		if _, err := os.Readlink(part.path); err == nil {
			return nil, fmt.Errorf("feedback data-file acquired junction after read: %s", part.path)
		}
	}
	return body, nil
}
