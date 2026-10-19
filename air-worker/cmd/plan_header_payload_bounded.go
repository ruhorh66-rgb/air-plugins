package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// The installed 0.11.8 cache has 489 files / 77MB including its 61MB Go
// executable. These conservative limits allow it but deny unbounded reads
// from hostile local marketplace data. All of this is source-only DEV136
// preview; changing release/install authority is NOT part of this patch.
type planHeaderPayloadLimits struct {
	maxEntries      int
	maxFiles        int
	maxTotalBytes   int64
	maxFileBytes    int64
	maxDepth        int
	maxRelativePath int
}

var planHeaderDefaultPayloadLimits = planHeaderPayloadLimits{
	maxEntries: 8192, maxFiles: 4096,
	maxTotalBytes: 256 << 20, maxFileBytes: 128 << 20,
	maxDepth: 32, maxRelativePath: 2048,
}

type planHeaderPayloadItem struct {
	rel, path string
	size      int64
}

func planHeaderBoundedPayloadSnapshot(root string) (string, int, error) {
	return planHeaderBoundedPayloadSnapshotWithLimits(root, planHeaderDefaultPayloadLimits)
}

// planHeaderBoundedPayloadSnapshotWithLimits preserves the native
// payloadSnapshot SHA256 framing for ordinary canonical cache files:
// path + NUL + hex(sha256(contents)) + NUL, sorted by relative path.
// Unlike the native whole-product helper, this read-only preflight refuses
// symlinks, reparse/hardlinks, excessive file counts, oversized files, and
// unusually deep/long paths. It enumerates directories in 128-entry batches,
// rather than allocating an unbounded os.ReadDir list for one directory.
func planHeaderBoundedPayloadSnapshotWithLimits(root string, lim planHeaderPayloadLimits) (string, int, error) {
	// A caller may tighten limits for isolated tests but must never widen
	// the fixed production cap. Besides preventing bypasses through custom
	// settings, it keeps the expectedSize+1 read bound below int64 overflow.
	baseline := planHeaderDefaultPayloadLimits
	if lim.maxEntries < 1 || lim.maxEntries > baseline.maxEntries ||
		lim.maxFiles < 1 || lim.maxFiles > baseline.maxFiles ||
		lim.maxTotalBytes < 1 || lim.maxTotalBytes > baseline.maxTotalBytes ||
		lim.maxFileBytes < 1 || lim.maxFileBytes > baseline.maxFileBytes ||
		lim.maxDepth < 1 || lim.maxDepth > baseline.maxDepth ||
		lim.maxRelativePath < 1 || lim.maxRelativePath > baseline.maxRelativePath {
		return "", 0, errors.New("marketplace payload limits must not exceed immutable production bounds")
	}
	if !filepath.IsAbs(root) {
		return "", 0, errors.New("marketplace cache root must be absolute")
	}
	root = filepath.Clean(root)
	if err := planHeaderValidateAncestors(root); err != nil {
		return "", 0, err
	}
	if err := planHeaderRejectOSReparse(root); err != nil {
		return "", 0, err
	}

	const directoryBatch = 128
	var entries, files int
	var total int64
	items := make([]planHeaderPayloadItem, 0, 256)
	var scan func(string, int) error
	scan = func(dir string, depth int) error {
		if depth > lim.maxDepth {
			return errors.New("marketplace cache nesting exceeds depth limit")
		}
		if err := planHeaderRejectOSReparse(dir); err != nil {
			return err
		}
		before, err := os.Lstat(dir)
		if err != nil || !before.IsDir() {
			return errors.New("marketplace cache directory unavailable")
		}
		handle, err := os.Open(dir)
		if err != nil {
			return err
		}
		defer handle.Close()
		start, err := handle.Stat()
		if err != nil || !os.SameFile(before, start) ||
			!before.ModTime().Equal(start.ModTime()) {
			return errors.New("marketplace cache directory changed on open")
		}
		for {
			batch, readErr := handle.ReadDir(directoryBatch)
			for _, entry := range batch {
				entries++
				if entries > lim.maxEntries {
					return errors.New("marketplace cache directory entries exceed limit")
				}
				path := filepath.Join(dir, entry.Name())
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				rel = filepath.ToSlash(rel)
				if len(rel) > lim.maxRelativePath {
					return errors.New("marketplace cache relative path exceeds limit")
				}
				if entry.IsDir() {
					switch entry.Name() {
					case ".git", ".woody", ".air-worker", ".in_use", "__pycache__":
						// Preserve the original native payloadSnapshot skip
						// semantics for runtime/host-only directories.
						continue
					}
					if err := scan(path, depth+1); err != nil {
						return err
					}
					continue
				}
				if err := planHeaderRejectOSReparse(path); err != nil {
					return err
				}
				info, err := os.Lstat(path)
				if err != nil || !info.Mode().IsRegular() {
					return errors.New("marketplace payload contains non-regular entry")
				}
				files++
				if files > lim.maxFiles {
					return errors.New("marketplace cache file count exceeds limit")
				}
				size := info.Size()
				if size < 0 || size > lim.maxFileBytes {
					return errors.New("marketplace cache file exceeds per-file byte limit")
				}
				if size > lim.maxTotalBytes-total {
					return errors.New("marketplace cache total bytes exceed limit")
				}
				total += size
				items = append(items, planHeaderPayloadItem{rel: rel, path: path, size: size})
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				return readErr
			}
		}
		finish, err := handle.Stat()
		if err != nil || !os.SameFile(start, finish) ||
			!start.ModTime().Equal(finish.ModTime()) {
			return errors.New("marketplace cache directory changed during enumeration")
		}
		return nil
	}
	if err := scan(root, 0); err != nil {
		return "", 0, fmt.Errorf("bounded marketplace enumeration failed: %w", err)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].rel < items[j].rel })

	h := sha256.New()
	for _, item := range items {
		fingerprint, err := planHeaderBoundedPayloadFileHash(item)
		if err != nil {
			return "", 0, err
		}
		_, _ = io.WriteString(h, item.rel)
		_, _ = h.Write([]byte{0})
		_, _ = io.WriteString(h, fingerprint)
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), len(items), nil
}

func planHeaderBoundedPayloadFileHash(item planHeaderPayloadItem) (string, error) {
	if err := planHeaderRejectOSReparse(item.path); err != nil {
		return "", err
	}
	before, err := os.Lstat(item.path)
	if err != nil || !before.Mode().IsRegular() || before.Size() != item.size {
		return "", errors.New("marketplace cache payload changed before bounded hashing")
	}
	f, err := os.Open(item.path)
	if err != nil {
		return "", err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) ||
		opened.Size() != item.size ||
		!before.ModTime().Equal(opened.ModTime()) {
		_ = f.Close()
		return "", errors.New("marketplace cache payload changed on open")
	}
	// The Windows DEV136 opened-handle guard rejects hardlinks and reparse
	// points even when they appear regular through os.Lstat.
	if err := planHeaderVerifyReleaseHandle(f); err != nil {
		_ = f.Close()
		return "", err
	}
	h := sha256.New()
	// This exact-length reader handles growth during hashing itself, before
	// the independent opened-handle/size/mtime readback below.
	n, readErr := planHeaderHashPayloadExactly(h, f, item.size)
	after, statErr := f.Stat()
	handleErr := planHeaderVerifyReleaseHandle(f)
	closeErr := f.Close()
	if readErr != nil || statErr != nil || handleErr != nil || closeErr != nil ||
		n != item.size || !os.SameFile(opened, after) ||
		after.Size() != opened.Size() ||
		!opened.ModTime().Equal(after.ModTime()) {
		return "", errors.New("marketplace cache payload changed during bounded hashing")
	}
	if err := planHeaderRejectOSReparse(item.path); err != nil {
		return "", err
	}
	latest, err := os.Lstat(item.path)
	if err != nil || !os.SameFile(before, latest) ||
		latest.Size() != after.Size() || !latest.ModTime().Equal(after.ModTime()) {
		return "", errors.New("marketplace cache payload changed after bounded hashing")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// planHeaderHashPayloadExactly rejects a read stream that shrinks or grows
// while hashing, even when the caller's opened-file metadata is unchanged.
// expected+1 never overflows: the immutable production cap is 128 MiB.
// An io.Reader seam makes mid-read growth deterministic in the unit test,
// without a callback or mutable global in the production file-hash path.
func planHeaderHashPayloadExactly(dst io.Writer, reader io.Reader, expected int64) (int64, error) {
	if expected < 0 || expected > planHeaderDefaultPayloadLimits.maxFileBytes {
		return 0, errors.New("marketplace payload expected bytes outside immutable read bound")
	}
	n, err := io.Copy(dst, io.LimitReader(reader, expected+1))
	if err != nil {
		return n, err
	}
	if n != expected {
		return n, fmt.Errorf("marketplace payload grew or shrank during bounded read: expected %d got %d", expected, n)
	}
	return n, nil
}
