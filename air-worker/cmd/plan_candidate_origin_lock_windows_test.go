//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

func n124SnapshotDiff(before, after map[string]string) string {
	var changed []string
	for key, value := range before {
		if after[key] != value {
			changed = append(changed, key)
		}
	}
	for key := range after {
		if _, ok := before[key]; !ok {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return strings.Join(changed, ", ")
}

func TestPlanCandidatePublishReplacement(t *testing.T) {
	for _, ancestor := range []bool{false, true} {
		t.Run(map[bool]string{false: "root", true: "ancestor"}[ancestor], func(t *testing.T) {
			repo, origin, old, c := publishFixture(t)
			foreign := t.TempDir()
			candidateGit(t, foreign, "init", "--bare")
			if err := os.CopyFS(filepath.Join(foreign, "objects"), os.DirFS(filepath.Join(repo, ".git", "objects"))); err != nil {
				t.Fatal(err)
			}
			candidateGit(t, foreign, "update-ref", publishRef, old)
			foreignBefore := n124Snapshot(t, foreign)
			for _, name := range []string{"air-worker/PLAN.md", "air-worker/learn/events.jsonl"} {
				local := filepath.Join(repo, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(local, []byte("preserve local WIP\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := candidateOriginal(t, repo)
			target := origin
			if ancestor {
				target = filepath.Dir(origin)
			}
			moved := target + "-displaced"
			attempted, replaced := false, false
			err := publishPlanCandidate(context.Background(), repo, origin, publishRef, c, old, func() {
				attempted = true
				if os.Rename(target, moved) == nil {
					replaced = true
					if !ancestor {
						if err := os.Rename(foreign, origin); err != nil {
							t.Fatal(err)
						}
					} else {
						// Ancestor rename alone breaks the transport path fence.
						if err := os.Rename(moved, target); err != nil {
							t.Fatal(err)
						}
					}
				}
			})
			if replaced && !ancestor {
				if err := os.Rename(origin, foreign); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(moved, origin); err != nil {
					t.Fatal(err)
				}
			}
			if !attempted || replaced {
				t.Fatalf("replacement fence: attempted=%v replaced=%v publish=%v", attempted, replaced, err)
			}
			if after := candidateOriginal(t, repo); !reflect.DeepEqual(before, after) {
				t.Fatalf("local mutation: %s", n124SnapshotDiff(before, after))
			}
			if after := n124Snapshot(t, foreign); !reflect.DeepEqual(foreignBefore, after) {
				t.Fatalf("foreign mutation: %s", n124SnapshotDiff(foreignBefore, after))
			}
			skipPublishSandbox(t, err)
			if err != nil {
				t.Fatal(err)
			}
			if candidateGit(t, origin, "rev-parse", publishRef) != c.OID {
				t.Fatal("false publish success")
			}
			if err := os.Rename(origin, origin+"-closed"); err != nil {
				t.Fatalf("fence leaked: %v", err)
			}
			if err := os.Rename(origin+"-closed", origin); err != nil {
				t.Fatal(err)
			}
		})
	}
}
