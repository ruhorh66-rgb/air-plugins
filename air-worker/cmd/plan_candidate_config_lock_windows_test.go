//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestPlanCandidateConfigLock(t *testing.T) {
	for _, mode := range []string{"identity", "existing-writer", "hardlink", "missing", "directory"} {
		t.Run(mode, func(t *testing.T) {
			repo, _, _ := candidateFixture(t)
			path := filepath.Join(repo, ".git", "config")
			switch mode {
			case "existing-writer":
				writer, err := os.OpenFile(path, os.O_WRONLY, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer writer.Close()
			case "hardlink":
				if err := os.Link(path, filepath.Join(repo, "config-alias")); err != nil {
					t.Fatal(err)
				}
			case "missing", "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if mode == "directory" {
					if err := os.Mkdir(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
			}
			lock, err := lockCandidateConfig(repo)
			if mode != "identity" {
				if err == nil {
					lock.Close()
					t.Fatal("accepted unsafe config")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Close()
			id, err := nativeInputPathIdentity(path)
			if err != nil {
				t.Fatal(err)
			}
			last := len(lock.paths) - 1
			if lock.paths[last] != path || lock.identities[last] != id {
				t.Fatal("wrong pinned config")
			}
			if err := lock.Verify(); err != nil {
				t.Fatal(err)
			}
			lock.identities[last].Low ^= 1
			if lock.Verify() == nil {
				t.Fatal("accepted identity drift")
			}
		})
	}
}

func TestPlanCandidatePublishConfigRace(t *testing.T) {
	for _, mode := range []string{"git-config", "write", "replace", "git-dir", "repo", "ancestor"} {
		t.Run(mode, func(t *testing.T) {
			repo, origin, old, c := publishFixture(t)
			foreign := t.TempDir()
			candidateGit(t, foreign, "init", "--bare")
			if err := os.CopyFS(filepath.Join(foreign, "objects"), os.DirFS(filepath.Join(repo, ".git", "objects"))); err != nil {
				t.Fatal(err)
			}
			candidateGit(t, foreign, "update-ref", publishRef, old)
			config := filepath.Join(repo, ".git", "config")
			raw, err := os.ReadFile(config)
			if err != nil {
				t.Fatal(err)
			}
			malicious := append(append([]byte(nil), raw...), []byte("\n[url \""+filepath.ToSlash(foreign)+"\"]\n\tinsteadOf = "+filepath.ToSlash(origin)+"\n")...)
			replacement := filepath.Join(t.TempDir(), "config")
			if err := os.WriteFile(replacement, malicious, 0600); err != nil {
				t.Fatal(err)
			}
			before, foreignBefore := n124Snapshot(t, repo), n124Snapshot(t, foreign)
			attempted, blocked := false, false
			err = publishPlanCandidate(context.Background(), repo, origin, publishRef, c, old, func() {
				attempted = true
				var editErr error
				switch mode {
				case "git-config":
					out, e := exec.Command("git", "-C", repo, "config", "url."+filepath.ToSlash(foreign)+".insteadOf", filepath.ToSlash(origin)).CombinedOutput()
					editErr = e
					t.Logf("config writer: %v %s", e, out)
				case "write":
					editErr = os.WriteFile(config, malicious, 0600)
				case "replace":
					editErr = os.Rename(replacement, config)
				default:
					target := repo
					if mode == "git-dir" {
						target = filepath.Dir(config)
					}
					if mode == "ancestor" {
						target = filepath.Dir(repo)
					}
					editErr = os.Rename(target, target+"-moved")
					if editErr == nil {
						if e := os.Rename(target+"-moved", target); e != nil {
							t.Fatal(e)
						}
					}
				}
				blocked = editErr != nil
			})
			if !attempted || !blocked {
				t.Fatalf("config fence: attempted=%v blocked=%v publish=%v", attempted, blocked, err)
			}
			if after := n124Snapshot(t, repo); !reflect.DeepEqual(before, after) {
				t.Fatalf("source mutation: %s", n124SnapshotDiff(before, after))
			}
			if after := n124Snapshot(t, foreign); !reflect.DeepEqual(foreignBefore, after) {
				t.Fatalf("foreign mutation: %s", n124SnapshotDiff(foreignBefore, after))
			}
			skipPublishSandbox(t, err)
			if err != nil {
				t.Fatal(err)
			}
			if candidateGit(t, origin, "rev-parse", publishRef) != c.OID {
				t.Fatal("original did not advance")
			}
			if candidateGit(t, foreign, "rev-parse", publishRef) != old {
				t.Fatal("foreign advanced")
			}
			if err := os.WriteFile(config, raw, 0600); err != nil {
				t.Fatalf("config handle leaked: %v", err)
			}
		})
	}
}
