package main

import (
	"crypto/sha256"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Local policy limits are not user-overridable back doors. A custom
// MaxInt64 per-file limit would overflow the size+1 read bound; refuse the
// configuration before hashing even a trivially small cache member.
func TestPlanHeaderBoundedPayloadRejectsUnsafeCustomLimits(t *testing.T) {
	root := t.TempDir()
	planHeaderPayloadWrite(t, root, "small.txt", "tiny")
	for _, tc := range []struct {
		name   string
		change func(*planHeaderPayloadLimits)
	}{
		{"max-int64-file", func(l *planHeaderPayloadLimits) { l.maxFileBytes = math.MaxInt64 }},
		{"max-int64-total", func(l *planHeaderPayloadLimits) { l.maxTotalBytes = math.MaxInt64 }},
		{"excess-file-count", func(l *planHeaderPayloadLimits) { l.maxFiles = planHeaderDefaultPayloadLimits.maxFiles + 1 }},
		{"excess-directory-count", func(l *planHeaderPayloadLimits) { l.maxEntries = planHeaderDefaultPayloadLimits.maxEntries + 1 }},
		{"excess-depth", func(l *planHeaderPayloadLimits) { l.maxDepth = planHeaderDefaultPayloadLimits.maxDepth + 1 }},
		{"excess-relative-path", func(l *planHeaderPayloadLimits) {
			l.maxRelativePath = planHeaderDefaultPayloadLimits.maxRelativePath + 1
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limit := planHeaderDefaultPayloadLimits
			tc.change(&limit)
			if _, _, err := planHeaderBoundedPayloadSnapshotWithLimits(root, limit); err == nil {
				t.Fatal("caller-controlled marketplace byte/count bound was elevated")
			}
		})
	}
}

// A 128MiB+1 sparse file costs negligible disk allocation. It must be
// rejected on Lstat before the scanner allocates or hashes its apparent
// content. Avoid generating a real 128MiB string in a fixture.
func TestPlanHeaderBoundedPayloadRejectsOversizedSparseFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "oversized-sparse.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(planHeaderDefaultPayloadLimits.maxFileBytes + 1); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_, _, err = planHeaderBoundedPayloadSnapshot(root)
	if err == nil {
		t.Fatal("sparse cache file above per-file bound was accepted")
	}
}

// The enumerated size must remain authoritative. Growth between directory
// enumeration and the opened file read must fail closed rather than silently
// hashing extra bytes.
func TestPlanHeaderBoundedPayloadRejectsSizeGrowthAfterEnumeration(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "growth.bin")
	initial := []byte(strings.Repeat("A", 256))
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	item := planHeaderPayloadItem{rel: "growth.bin", path: path, size: int64(len(initial))}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write([]byte("-new-record-after-enumeration"))
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("cannot produce a real growing NTFS file: write=%v close=%v", err, closeErr)
	}
	if _, err := planHeaderBoundedPayloadFileHash(item); err == nil {
		t.Fatal("grow-after-enumeration accepted despite pinned prior size")
	}
}

// growAfterFirstPayloadRead uses the same opened file descriptor as the
// production path. After the first successful Read it appends actual bytes
// to the *same on-disk file*, before subsequent bounded Read calls.
type growAfterFirstPayloadRead struct {
	fd     *os.File
	path   string
	t      *testing.T
	didAdd bool
}

func (g *growAfterFirstPayloadRead) Read(p []byte) (int, error) {
	// Guarantee multiple Read calls for the 64KiB fixture even if io.Copy's
	// internal buffer size changes between Go versions.
	if len(p) > 4096 {
		p = p[:4096]
	}
	n, err := g.fd.Read(p)
	if n > 0 && !g.didAdd {
		g.t.Helper()
		grow, openErr := os.OpenFile(g.path, os.O_APPEND|os.O_WRONLY, 0600)
		if openErr != nil {
			return n, openErr
		}
		_, writeErr := grow.Write([]byte("-REAL-GROWTH-WHILE-READING-"))
		closeErr := grow.Close()
		if writeErr != nil {
			return n, writeErr
		}
		if closeErr != nil {
			return n, closeErr
		}
		g.didAdd = true
	}
	return n, err
}

func TestPlanHeaderBoundedPayloadRejectsGrowthMidRead(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "actual-growing-payload.bin")
	initial := []byte(strings.Repeat("B", 64*1024))
	if err := os.WriteFile(path, initial, 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	reader := &growAfterFirstPayloadRead{fd: f, path: path, t: t}
	n, err := planHeaderHashPayloadExactly(sha256.New(), reader, int64(len(initial)))
	if !reader.didAdd {
		t.Fatalf("negative fixture did not append a real payload file while reading: %v", err)
	}
	after, statErr := os.Stat(path)
	if statErr != nil || after.Size() <= int64(len(initial)) {
		t.Fatalf("negative fixture did not grow the OS file: size=%v err=%v", after, statErr)
	}
	if err == nil || n != int64(len(initial))+1 {
		t.Fatalf("growing marketplace payload stream not rejected: n=%d want=%d err=%v", n, len(initial)+1, err)
	}
}

func TestPlanHeaderBoundedPayloadRejectsShrunkStreamMidRead(t *testing.T) {
	originalSize := int64(1024)
	n, err := planHeaderHashPayloadExactly(io.Discard, strings.NewReader(strings.Repeat("C", 512)), originalSize)
	if err == nil || n != 512 {
		t.Fatalf("truncated payload stream accepted: n=%d err=%v", n, err)
	}
}
