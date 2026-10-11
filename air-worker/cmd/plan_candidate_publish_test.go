package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

const publishRef = "refs/heads/canonical"

func publishFixture(t *testing.T) (string, string, string, planCandidate) {
	t.Helper()
	repo, old, files := candidateFixture(t)
	candidate, err := preparePlanCandidate(context.Background(), repo, old, "op-1", files)
	if err != nil {
		t.Fatal(err)
	}
	origin, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	candidateGit(t, origin, "init", "--bare")
	declareIsolatedPlanFixture(t, origin)
	// Copy objects without transport so negative tests run even when sandbox push is blocked.
	if err := os.CopyFS(filepath.Join(origin, "objects"), os.DirFS(filepath.Join(repo, ".git", "objects"))); err != nil {
		t.Fatal(err)
	}
	candidateGit(t, origin, "update-ref", publishRef, old)
	return repo, origin, old, candidate
}

func assertPublishState(t *testing.T, err error, state string) {
	t.Helper()
	var failure *planPublishError
	if !errors.As(err, &failure) || failure.State != state {
		t.Fatalf("want %s, got %v", state, err)
	}
}

func skipPublishSandbox(t *testing.T, err error) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), "couldn't create signal pipe, Win32 error 5") {
		t.Skipf("sandbox blocks Git transport; run host scoped tests: %v", err)
	}
}

func TestPlanCandidatePublishRejects(t *testing.T) {
	for _, mode := range []string{"parent", "object-parent", "tree", "oid", "files", "operation", "unprepared", "ref", "non-bare", "foreign-origin", "foreign-marker", "stale", "tag-ref", "wrong-old", "network", "rewrite", "receive-command", "git-env", "alternates", "active-hook"} {
		t.Run(mode, func(t *testing.T) {
			repo, origin, old, c := publishFixture(t)
			ref := publishRef
			switch mode {
			case "parent":
				c.Parent = strings.Repeat("a", 40)
			case "object-parent":
				c.OID = n124ProbeMustGit(t, repo, "AirWorker candidate\n\nOperation-ID: op-1\n", "commit-tree", c.Tree)
				// Fault injection past the seal exercises independent Git parent verification.
				c.preparedDigest = candidatePublishDigest(c)
			case "tree":
				c.Tree = strings.Repeat("a", 40)
			case "oid":
				c.OID = old
			case "files":
				c.Files["air-worker/PLAN.md"] = []byte("foreign")
			case "operation":
				c.Operation = "other-operation"
			case "unprepared":
				c = planCandidate{OID: c.OID, Tree: c.Tree, Parent: c.Parent, Files: c.Files, Operation: c.Operation}
			case "ref":
				ref = "refs/heads/foreign"
			case "non-bare":
				origin = repo
			case "foreign-origin":
				if err := os.Remove(filepath.Join(origin, "plan-authority.json")); err != nil {
					t.Fatal(err)
				}
			case "stale":
				other := n124ProbeMustGit(t, origin, "another operation\n", "commit-tree", c.Tree, "-p", old)
				candidateGit(t, origin, "update-ref", publishRef, other)
			case "tag-ref":
				candidateGit(t, origin, "-c", "tag.gpgsign=false", "tag", "-a", "wrapped", "-m", "wrapped candidate", c.OID)
				tag := candidateGit(t, origin, "rev-parse", "refs/tags/wrapped")
				if err := os.WriteFile(filepath.Join(origin, "refs", "heads", "canonical"), []byte(tag+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "wrong-old":
				old = strings.Repeat("b", 40)
			case "foreign-marker":
				raw, err := os.ReadFile(filepath.Join(repo, "plan-authority.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(origin, "plan-authority.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			case "alternates":
				if err := os.WriteFile(filepath.Join(origin, "objects", "info", "alternates"), []byte(filepath.Join(repo, ".git", "objects")), 0600); err != nil {
					t.Fatal(err)
				}
			case "network":
				origin = "https://example.invalid/repo.git"
			case "rewrite":
				candidateGit(t, repo, "config", "url.https://example.invalid/.insteadOf", origin)
			case "receive-command":
				candidateGit(t, origin, "config", "receive.procReceiveRefs", "refs/heads")
			case "git-env":
				t.Setenv("GIT_CONFIG_COUNT", "1")
			case "active-hook":
				if err := os.WriteFile(filepath.Join(origin, "hooks", "pre-receive"), []byte("#!/bin/sh\\nexit 1\\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := candidateOriginal(t, repo)
			var remoteBefore map[string]string
			if mode != "network" {
				remoteBefore = n124Snapshot(t, origin)
			}
			err := publishPlanCandidateCAS(context.Background(), repo, origin, ref, c, old)
			assertPublishState(t, err, "conflict")
			if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
				t.Fatal("rejection changed local files/index/refs")
			}
			if mode != "network" && !reflect.DeepEqual(remoteBefore, n124Snapshot(t, origin)) {
				t.Fatal("rejection changed remote")
			}
		})
	}
}

func TestPlanCandidatePublishReplay(t *testing.T) {
	repo, origin, old, c := publishFixture(t)
	if err := os.WriteFile(filepath.Join(repo, "foreign-wip"), []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	before := candidateOriginal(t, repo)
	for i := 0; i < 2; i++ {
		err := publishPlanCandidateCAS(context.Background(), repo, origin, publishRef, c, old)
		if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
			t.Fatal("attempt changed local files/index/refs")
		}
		skipPublishSandbox(t, err)
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := candidateGit(t, origin, "show", "-s", "--format=%T%n%P", publishRef); got != c.Tree+"\n"+old {
		t.Fatal(got)
	}
	for name, content := range c.Files {
		got, err := exec.CommandContext(context.Background(), "git", "-C", origin, "cat-file", "blob", c.OID+":"+name).Output()
		if err != nil || !bytes.Equal(got, content) {
			t.Fatalf("remote bytes: %s: %v", name, err)
		}
	}
	if got := candidateGit(t, origin, "for-each-ref", "--format=%(refname)"); got != publishRef {
		t.Fatalf("unexpected remote refs: %s", got)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
		t.Fatal("publish changed worktree/index/refs")
	}
}

func TestPlanCandidatePublishPending(t *testing.T) {
	for _, mode := range []string{"missing-ref", "locked-ref"} {
		t.Run(mode, func(t *testing.T) {
			repo, origin, old, c := publishFixture(t)
			if mode == "missing-ref" {
				candidateGit(t, origin, "update-ref", "-d", publishRef)
			} else {
				if err := os.WriteFile(filepath.Join(origin, "refs", "heads", "canonical.lock"), []byte("foreign lock"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := candidateOriginal(t, repo)
			err := publishPlanCandidateCAS(context.Background(), repo, origin, publishRef, c, old)
			assertPublishState(t, err, "pending")
			if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
				t.Fatal("pending changed local state")
			}
			if mode == "locked-ref" {
				if candidateGit(t, origin, "rev-parse", publishRef) != old {
					t.Fatal("failed push advanced ref")
				}
				skipPublishSandbox(t, err)
			}
		})
	}
}

func TestPlanCandidatePublishSameObjectIsReplayNotClaim(t *testing.T) {
	repo, origin, old, c := publishFixture(t)
	candidateGit(t, origin, "update-ref", publishRef, c.OID)
	if err := publishPlanCandidateCAS(context.Background(), repo, origin, publishRef, c, old); err != nil {
		t.Fatal(err)
	}
	c.Operation = "different-owner"
	assertPublishState(t, publishPlanCandidateCAS(context.Background(), repo, origin, publishRef, c, old), "conflict")
}

func TestPlanCandidatePublishRace(t *testing.T) {
	a, origin, old, ca := publishFixture(t)
	// Prepare both commits before copying to an independent client; A1 stays clean/no-remote.
	files := map[string][]byte{"air-worker/PLAN.md": []byte("second"), "air-worker/plan/N-121_other.md": []byte("second node"), "air-worker/plan/event-outbox/op-2.json": []byte("second outbox")}
	cb, err := preparePlanCandidate(context.Background(), a, old, "op-2", files)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(b, os.DirFS(a)); err != nil {
		t.Fatal(err)
	}
	declareIsolatedPlanFixture(t, b)
	candidateGit(t, a, "branch", "client-a")
	candidateGit(t, b, "branch", "client-b")
	for _, root := range []string{a, b} {
		for _, name := range []string{"plan/N-121_foreign.md", "learn/events.jsonl"} {
			local := filepath.Join(root, "air-worker", filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(local), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(local, []byte("foreign WIP\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, "air-worker", "PLAN.md"), []byte("foreign dirty PLAN"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "foreign-wip"), []byte("preserve"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	roots := []string{a, b}
	candidates := []planCandidate{ca, cb}
	before := []map[string]string{candidateOriginal(t, a), candidateOriginal(t, b)}
	errs := make([]error, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range roots {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs[i] = publishPlanCandidateCAS(context.Background(), roots[i], origin, publishRef, candidates[i], old)
		}(i)
	}
	close(start)
	wg.Wait()
	for i, root := range roots {
		if !reflect.DeepEqual(before[i], candidateOriginal(t, root)) {
			t.Fatal("race changed local state")
		}
	}
	for _, err := range errs {
		skipPublishSandbox(t, err)
	}
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("want one winner: %v", errs)
	}
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	assertPublishState(t, errs[1-winner], "conflict")
	if got := candidateGit(t, origin, "rev-parse", publishRef); got != candidates[winner].OID {
		t.Fatal(got)
	}
	assertPublishState(t, publishPlanCandidateCAS(context.Background(), roots[1-winner], origin, publishRef, candidates[1-winner], old), "conflict")
}
