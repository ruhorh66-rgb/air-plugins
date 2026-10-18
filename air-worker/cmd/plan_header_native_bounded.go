package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const planHeaderNodeLimit = 1024 * 1024

// planHeaderListNodesBounded reads candidate N-nodes under the read-only
// DEV136 preflight. It never calls the general listPlanNodes helper, whose
// os.ReadFile is unbounded. This is NOT a generation lock or mutation grant:
// the native PLAN owner must still prove N124 cross-process authority.
func planHeaderListNodesBounded(root string) ([]planNode, error) {
	dir := planNodeDir(root)
	if err := planHeaderValidateAncestors(dir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []planNode{}, nil
		}
		return nil, err
	}
	nodes := make([]planNode, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "N-") || !strings.HasSuffix(strings.ToLower(name), ".md") {
			continue
		}
		if entry.IsDir() {
			return nil, fmt.Errorf("native node path must be file: %s", name)
		}
		leaf := filepath.Join(dir, name)
		if err := planHeaderRejectOSReparse(leaf); err != nil {
			return nil, fmt.Errorf("native node reparse leaf: %w", err)
		}
		before, err := os.Lstat(leaf)
		if err != nil || !before.Mode().IsRegular() || before.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("native node leaf is not regular: %s", name)
		}
		if before.Size() > planHeaderNodeLimit {
			return nil, fmt.Errorf("native node leaf exceeds preflight byte limit: %s", name)
		}
		f, err := os.Open(leaf)
		if err != nil {
			return nil, err
		}
		opened, err := f.Stat()
		if err != nil || !os.SameFile(before, opened) || opened.Size() > planHeaderNodeLimit {
			f.Close()
			return nil, fmt.Errorf("native node file identity or size changed while opening: %s", name)
		}
		raw, readErr := io.ReadAll(io.LimitReader(f, planHeaderNodeLimit+1))
		afterOpen, statErr := f.Stat()
		closeErr := f.Close()
		if readErr != nil || statErr != nil || closeErr != nil || len(raw) > planHeaderNodeLimit ||
			!os.SameFile(before, afterOpen) || opened.Size() != afterOpen.Size() ||
			!opened.ModTime().Equal(afterOpen.ModTime()) {
			return nil, fmt.Errorf("native node changed or exceeded bounded read: %s", name)
		}
		if err := planHeaderRejectOSReparse(leaf); err != nil {
			return nil, fmt.Errorf("native node changed to reparse leaf: %w", err)
		}
		latest, err := os.Lstat(leaf)
		if err != nil || !os.SameFile(before, latest) || latest.Size() != opened.Size() ||
			!latest.ModTime().Equal(opened.ModTime()) {
			return nil, fmt.Errorf("native node path generation changed: %s", name)
		}
		node, err := parsePlanNode(leaf, raw)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, node)
	}
	if err := planHeaderValidateAncestors(dir); err != nil {
		return nil, err
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes, nil
}
