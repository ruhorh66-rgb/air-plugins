package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodingPreservesIncompleteArchiveForRetry(t *testing.T) {
	base := t.TempDir()
	partial := filepath.Join(base, "archive-candidate")
	if err := os.Mkdir(partial, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(partial, "partial"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	owner, err := os.OpenRoot(base)
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if err := preserveIncompleteCodingArchive(owner); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(partial); !os.IsNotExist(err) {
		t.Fatal("candidate still occupies retry path")
	}
	entries, err := os.ReadDir(base)
	if err != nil || len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "archive-incomplete-") {
		t.Fatalf("partial archive not preserved: %v %v", entries, err)
	}
	data, err := os.ReadFile(filepath.Join(base, entries[0].Name(), "partial"))
	if err != nil || string(data) != "preserve" {
		t.Fatal("partial archive contents changed")
	}
	if err := os.WriteFile(partial, []byte("foreign"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := preserveIncompleteCodingArchive(owner); err == nil {
		t.Fatal("foreign file accepted as partial archive")
	}
	data, err = os.ReadFile(partial)
	if err != nil || string(data) != "foreign" {
		t.Fatal("foreign file changed")
	}
}

func TestCodingArchiveExtraPaths(t *testing.T) {
	root := t.TempDir()
	codingFixtureGit(t, root, "init")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".woody/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, root, "add", ".gitignore")
	if err := os.Mkdir(filepath.Join(root, ".woody"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TASK-CODE-WORKER.md", "data.bin"} {
		if err := os.WriteFile(filepath.Join(root, ".woody", name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	paths, err := codingArchiveExtraPaths(root, ".")
	if err != nil || len(paths) != 1 || paths[0] != ".woody/data.bin" {
		t.Fatalf("extra file discovery: %v %v", paths, err)
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.WriteFile(foreign, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := codingArchiveExtraPaths(root, "."); err == nil {
		t.Fatal("unowned extra file accepted")
	}
	if data, err := os.ReadFile(foreign); err != nil || string(data) != "preserve" {
		t.Fatal("foreign file modified")
	}
	if err := os.Remove(foreign); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "foreign-empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := codingArchiveExtraPaths(root, "."); err == nil {
		t.Fatal("unowned empty directory accepted")
	}
	if err := os.Remove(filepath.Join(root, "foreign-empty")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".woody", "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := codingArchiveExtraPaths(root, "."); err == nil {
		t.Fatal("unarchived empty directory accepted")
	}
}

func TestCodingArchiveCommitPins(t *testing.T) {
	root := t.TempDir()
	codingFixtureGit(t, root, "init")
	codingFixtureGit(t, root, "config", "--local", "user.name", "Test")
	codingFixtureGit(t, root, "config", "--local", "user.email", "test@example.invalid")
	codingFixtureGit(t, root, "commit", "--allow-empty", "-m", "source")
	source, err := gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, root, "commit", "--allow-empty", "-m", "accepted")
	accepted, err := gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	id := "001122334455667788"
	if err := pinCodingArchiveCommits(root, id, source, accepted); err != nil {
		t.Fatal(err)
	}
	if !codingArchivePinsMatch(root, id, source, accepted) {
		t.Fatal("archive pins not readable")
	}
	if err := pinCodingArchiveCommits(root, id, accepted, accepted); err == nil {
		t.Fatal("existing archive pin was overwritten")
	}
	if !codingArchivePinsMatch(root, id, source, accepted) {
		t.Fatal("archive pin changed on mismatch")
	}
}
