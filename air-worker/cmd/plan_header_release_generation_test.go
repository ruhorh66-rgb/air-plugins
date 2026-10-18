package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// DEV-136 release facts are read-only but must not treat a file that changed
// between its initial Lstat and opened file handle as the same generation.
// The test constructs real temp files with unchanged physical identity.
func TestPlanHeaderReleaseSidecarPreOpenFacts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		kind   string
		accept bool
	}{
		{"unchanged", "", true},
		{"same-inode-different-size", "size", false},
		{"same-inode-same-size-changed-mtime", "mtime", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "release-slices.json")
			if err := os.WriteFile(path, []byte("native-preview"), 0600); err != nil {
				t.Fatal(err)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatal(err)
			}
			switch tc.kind {
			case "size":
				if err := os.WriteFile(path, []byte("native-preview-growth"), 0600); err != nil {
					t.Fatal(err)
				}
			case "mtime":
				if err := os.Chtimes(path, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC), time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
					t.Fatal(err)
				}
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			opened, err := f.Stat()
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, opened) {
				t.Fatal("invalid fixture: underlying inode changed")
			}
			err = planHeaderReleaseCheckPreOpenFacts(before, opened)
			if (err == nil) != tc.accept {
				t.Fatalf("pre-open and opened generation comparison: expected accept=%v got=%v before-size=%d opened-size=%d before-mtime=%s opened-mtime=%s err=%v",
					tc.accept, err == nil, before.Size(), opened.Size(), before.ModTime(), opened.ModTime(), err)
			}
		})
	}
}
