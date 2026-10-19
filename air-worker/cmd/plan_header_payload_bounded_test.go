package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planHeaderPayloadWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPlanHeaderBoundedPayloadSnapshot(t *testing.T) {
	makeLimits := func() planHeaderPayloadLimits {
		return planHeaderPayloadLimits{
			maxEntries: 16, maxFiles: 5,
			maxTotalBytes: 1024, maxFileBytes: 512,
			maxDepth: 8, maxRelativePath: 256,
		}
	}
	t.Run("ordinary-payload-hash-identical-to-installed-core", func(t *testing.T) {
		root := t.TempDir()
		planHeaderPayloadWrite(t, root, "a.txt", "ABC")
		planHeaderPayloadWrite(t, root, "nested/b.txt", "DEF")
		planHeaderPayloadWrite(t, root, ".in_use/marker", "not-payload")
		planHeaderPayloadWrite(t, root, "nested/__pycache__/x.pyc", "not-payload")
		planHeaderPayloadWrite(t, root, ".air-worker/private", "not-payload")
		want, count, err := payloadSnapshot(root)
		if err != nil {
			t.Fatal(err)
		}
		got, n, err := planHeaderBoundedPayloadSnapshotWithLimits(root, makeLimits())
		if err != nil || n != count || got != want {
			t.Fatalf("DEV136 changed native canonical cache digest: count=%d/%d digest=%s/%s err=%v", n, count, got, want, err)
		}
	})
	t.Run("reject-file-count-over-limit", func(t *testing.T) {
		root := t.TempDir()
		for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
			planHeaderPayloadWrite(t, root, name, "x")
		}
		limits := makeLimits()
		limits.maxFiles = 2
		if _, _, err := planHeaderBoundedPayloadSnapshotWithLimits(root, limits); err == nil {
			t.Fatal("cache file-count greater than bound accepted")
		}
	})
	t.Run("reject-total-byte-budget", func(t *testing.T) {
		root := t.TempDir()
		planHeaderPayloadWrite(t, root, "a.txt", "0123456789")
		planHeaderPayloadWrite(t, root, "b.txt", "0123456789")
		limits := makeLimits()
		limits.maxTotalBytes = 15
		if _, _, err := planHeaderBoundedPayloadSnapshotWithLimits(root, limits); err == nil {
			t.Fatal("cache total bytes exceed budget but were accepted")
		}
	})
	t.Run("reject-large-single-file-before-reading", func(t *testing.T) {
		root := t.TempDir()
		planHeaderPayloadWrite(t, root, "oversized.bin", strings.Repeat("X", 40))
		limits := makeLimits()
		limits.maxFileBytes = 16
		if _, _, err := planHeaderBoundedPayloadSnapshotWithLimits(root, limits); err == nil {
			t.Fatal("oversized cache member was read without refusing limit")
		}
	})
	t.Run("reject-too-many-directory-entries", func(t *testing.T) {
		root := t.TempDir()
		planHeaderPayloadWrite(t, root, "one/a.txt", "A")
		planHeaderPayloadWrite(t, root, "two/b.txt", "B")
		limits := makeLimits()
		limits.maxEntries = 2
		if _, _, err := planHeaderBoundedPayloadSnapshotWithLimits(root, limits); err == nil {
			t.Fatal("excessive directory entries accepted")
		}
	})
	t.Run("reject-nested-depth", func(t *testing.T) {
		root := t.TempDir()
		planHeaderPayloadWrite(t, root, "deep/one/two/three/payload", "x")
		limits := makeLimits()
		limits.maxDepth = 2
		if _, _, err := planHeaderBoundedPayloadSnapshotWithLimits(root, limits); err == nil {
			t.Fatal("excessive nested cache depth accepted")
		}
	})
	t.Run("reject-long-path", func(t *testing.T) {
		root := t.TempDir()
		planHeaderPayloadWrite(t, root, "nested/very-long-filename.txt", "x")
		limits := makeLimits()
		limits.maxRelativePath = 12
		if _, _, err := planHeaderBoundedPayloadSnapshotWithLimits(root, limits); err == nil {
			t.Fatal("excessive cache relative path accepted")
		}
	})
}

// This test is opt-in and strictly read-only: no cache file is created,
// overwritten, executed, or cleaned. Compare the new bounded digest with
// installed 0.11.8's canonical native digest for a known existing path.
func TestPlanHeaderBoundedPayloadLiveCacheOptIn(t *testing.T) {
	root := os.Getenv("AIRWORKER_DEV136_BOUND_CACHE")
	if root == "" {
		t.Skip("requires one explicitly selected installed cache fixture")
	}
	want, beforeN, err := payloadSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	got, afterN, err := planHeaderBoundedPayloadSnapshot(root)
	if err != nil || want != got || beforeN != afterN {
		t.Fatalf("installed-cache parity failed: legacy=%s/%d bounded=%s/%d err=%v", want, beforeN, got, afterN, err)
	}
	t.Logf("bounded canonical payload parity PASS: files=%d sha256=%s", afterN, got)
}
