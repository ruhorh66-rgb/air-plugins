package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Runtime evidence may be untrusted even when its containing module wrote the
// original bytes. Reject links and reparse points on every proof read. The
// shared skill reader also validates the opened OS file handle's final path,
// preventing parent-swap races across the runtime confinement boundary.
func n093ReadRuntimeEvidence(runtimeRoot, relative string, limit int64) ([]byte, error) {
	if !filepath.IsAbs(runtimeRoot) || !filepath.IsLocal(filepath.FromSlash(relative)) ||
		relative == "" || strings.Contains(relative, "\\") ||
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative ||
		limit <= 0 || limit > 64*1024*1024 {
		return nil, errors.New("N093: invalid native runtime evidence path")
	}
	if err := n093CheckOrdinaryDir(runtimeRoot); err != nil {
		return nil, fmt.Errorf("N093: runtime root identity: %w", err)
	}
	path := filepath.Join(runtimeRoot, filepath.FromSlash(relative))
	// Reject any reparse ancestor, including Windows junctions that may not
	// report ModeSymlink. A final handle check still catches races after Lstat.
	for dir := filepath.Dir(path); !sameLearningPath(dir, runtimeRoot); dir = filepath.Dir(dir) {
		if !pathWithinRoot(runtimeRoot, dir) || filepath.Dir(dir) == dir {
			return nil, errors.New("N093: native runtime evidence ancestor escaped")
		}
		if err := n093CheckOrdinaryDir(dir); err != nil {
			return nil, err
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > limit ||
		info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return nil, errors.New("N093: evidence is not a bounded ordinary file")
	}
	if _, err := os.Readlink(path); err == nil {
		return nil, errors.New("N093: runtime evidence is linked/reparse")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || opened.Size() < 0 || opened.Size() > limit {
		return nil, errors.New("N093: opened runtime evidence is not bounded regular")
	}
	if err := n093RejectHardlinkedEvidence(f); err != nil {
		return nil, err
	}
	// An attacker may replace a path after lstat. Trust the OS handle final
	// path and file object identity, not the originally inspected pathname.
	resolved, err := openedFileFinalPath(f)
	if err != nil || !sameLearningPath(resolved, path) {
		return nil, errors.New("N093: opened runtime evidence is a redirected link")
	}
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(opened, current) {
		return nil, errors.New("N093: runtime evidence path replaced after open")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, errors.New("N093: bounded runtime evidence read failed")
	}
	current, err = os.Lstat(path)
	if err != nil || !os.SameFile(opened, current) {
		return nil, errors.New("N093: runtime evidence was swapped while reading")
	}
	return data, nil
}

func n093ScanRuntimeEvidenceJSONL(runtimeRoot, relative string, callback func([]byte) error) error {
	data, err := n093ReadRuntimeEvidence(runtimeRoot, relative, 64*1024*1024)
	if err != nil {
		return err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	index := 0
	for scanner.Scan() {
		index++
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := callback(append([]byte(nil), line...)); err != nil {
			return fmt.Errorf("N093: runtime %s line %d: %w", relative, index, err)
		}
	}
	return scanner.Err()
}
