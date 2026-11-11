package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodingPartialPreparationCleanupAndRetention(t *testing.T) {
	for _, withIgnored := range []bool{false, true} {
		name := "clean"
		if withIgnored {
			name = "ignored-data"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "product")
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("ignored.tmp\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "code.go"), []byte("package demo\n"), 0600); err != nil {
				t.Fatal(err)
			}
			codingFixtureGit(t, root, "init")
			codingFixtureGit(t, root, "config", "--local", "user.name", "AirWorker Test")
			codingFixtureGit(t, root, "config", "--local", "user.email", "airworker-test@example.invalid")
			codingFixtureGit(t, root, "add", ".")
			codingFixtureGit(t, root, "commit", "-m", "base")
			head, err := gitCodingOutput(root, "rev-parse", "HEAD")
			if err != nil {
				t.Fatal(err)
			}
			scope := sessionScope{ID: sessionIdentity{Principal: "codex", SessionKey: "partial-test"}}
			original := codingWorktreeAdd
			t.Cleanup(func() { codingWorktreeAdd = original })
			calls := 0
			var first string
			codingWorktreeAdd = func(repo, worker, source string) (string, error) {
				calls++
				if calls == 1 {
					first = worker
					return original(repo, worker, source)
				}
				if withIgnored {
					if err := os.WriteFile(filepath.Join(first, "ignored.tmp"), []byte("preserve"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				return "", errors.New("injected second checkout failure")
			}
			_, _, err = prepareCodingWorktrees(root, scope, "partial-step", head, "task bytes")
			if err == nil || calls != 2 {
				t.Fatalf("partial failure not reported: %v calls=%d", err, calls)
			}
			if withIgnored {
				if !strings.Contains(err.Error(), "retained") {
					t.Fatalf("retention not reported: %v", err)
				}
				raw, readErr := os.ReadFile(filepath.Join(first, "ignored.tmp"))
				if readErr != nil || string(raw) != "preserve" {
					t.Fatal("ignored data was changed or lost")
				}
			} else if _, statErr := os.Stat(first); !os.IsNotExist(statErr) {
				t.Fatalf("clean unstarted worktree remains: %v", statErr)
			}
			reservation := filepath.Dir(first)
			if raw, readErr := os.ReadFile(filepath.Join(reservation, "task.json")); readErr != nil || string(raw) != "task bytes" {
				t.Fatal("reservation task was not preserved")
			}
		})
	}
}

func TestCodingUnstartedCleanupRejectsForeignPath(t *testing.T) {
	work := t.TempDir()
	repo := filepath.Join(work, "repo")
	foreign := filepath.Join(work, "foreign")
	if err := os.MkdirAll(foreign, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(foreign, "sentinel")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := removeUnstartedCodingWorker(repo, foreign, foreign, "fake"); err == nil {
		t.Fatal("foreign directory accepted")
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "preserve" {
		t.Fatal("foreign data changed")
	}
}
