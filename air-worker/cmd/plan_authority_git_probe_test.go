package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// These protocol probes are not native canonical acceptance tests.
func n124ProbeGit(root, input string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	cmd.Stdin = strings.NewReader(input)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func n124ProbeMustGit(t *testing.T, root, input string, args ...string) string {
	t.Helper()
	out, err := n124ProbeGit(root, input, args...)
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return out
}

func n124ProbeClones(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	n124ProbeMustGit(t, root, "", "init", "--bare", origin)
	seed := seedPlanNodeProduct(t)
	n124SeedNode(t, seed, "N-119_base", "verified")
	n124ProbeMustGit(t, seed, "", "init", "-b", "canonical")
	n124ProbeMustGit(t, seed, "", "add", ".")
	n124ProbeMustGit(t, seed, "", "commit", "-m", "fixture")
	n124ProbeMustGit(t, seed, "", "push", origin, "HEAD:refs/heads/canonical")
	a, b := filepath.Join(root, "a"), filepath.Join(root, "b")
	for _, clone := range []string{a, b} {
		n124ProbeMustGit(t, root, "", "clone", "--branch", "canonical", origin, clone)
	}
	return origin, a, b
}

func TestPlanNodeN124GitClaimSameObjectIsNotExclusive(t *testing.T) {
	_, a, b := n124ProbeClones(t)
	ref := "refs/air-worker/claims/N-120"
	for _, clone := range []string{a, b} {
		// A stale empty lease is not tested when the desired object is already present.
		n124ProbeMustGit(t, clone, "", "push", "--porcelain", "--force-with-lease="+ref+":", "origin", "HEAD:"+ref)
	}
	t.Log("HOLD: both clones receive success for the same object; push exit zero alone does not prove claim ownership")
}

func TestPlanNodeN124GitDistinctClaimsHaveOneWinner(t *testing.T) {
	origin, a, b := n124ProbeClones(t)
	ref := "refs/air-worker/claims/N-120"
	clones := []string{a, b}
	claims := make([]string, 2)
	for i, clone := range clones {
		tree := n124ProbeMustGit(t, clone, "", "rev-parse", "HEAD^{tree}")
		claims[i] = n124ProbeMustGit(t, clone, "claim owner "+clone+"\n", "commit-tree", tree, "-p", "HEAD")
	}
	if claims[0] == claims[1] {
		t.Fatal("claim identities are not distinct")
	}
	var wg sync.WaitGroup
	errs := make([]error, 2)
	start := make(chan struct{})
	for i, clone := range clones {
		wg.Add(1)
		go func(i int, clone string) {
			defer wg.Done()
			<-start
			_, errs[i] = n124ProbeGit(clone, "", "push", "--porcelain", "--force-with-lease="+ref+":", "origin", claims[i]+":"+ref)
		}(i, clone)
	}
	close(start)
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("want exactly one successful claim, got %v", errs)
	}
	winner := 0
	if errs[0] != nil {
		winner = 1
	}
	if got := n124ProbeMustGit(t, origin, "", "rev-parse", ref); got != claims[winner] {
		t.Fatalf("remote claim=%s, winner=%s", got, claims[winner])
	}
	t.Log("distinct claim CAS works; native mutation and recovery are not established by this probe")
}

func TestPlanNodeN124GitAtomicClaimDoesNotLockCanonical(t *testing.T) {
	origin, a, b := n124ProbeClones(t)
	base := n124ProbeMustGit(t, a, "", "rev-parse", "HEAD")
	tree := n124ProbeMustGit(t, a, "", "rev-parse", "HEAD^{tree}")
	claim := n124ProbeMustGit(t, a, "unique owner\n", "commit-tree", tree, "-p", base)
	ref := "refs/air-worker/claims/N-120"
	// Including the unchanged canonical ref does not lock it through local publication.
	n124ProbeMustGit(t, a, "", "push", "--atomic", "--force-with-lease=refs/heads/canonical:"+base, "--force-with-lease="+ref+":", "origin", base+":refs/heads/canonical", claim+":"+ref)
	advanced := n124ProbeMustGit(t, b, "authority advanced\n", "commit-tree", tree, "-p", base)
	n124ProbeMustGit(t, b, "", "push", "origin", advanced+":refs/heads/canonical")
	if got := n124ProbeMustGit(t, origin, "", "rev-parse", ref); got != claim {
		t.Fatal("claim unexpectedly disappeared")
	}
	t.Log("HOLD: canonical advances while claim remains; claiming does not fence subsequent local PLAN/LEARN publication")
}

func TestPlanNodeN124GitClaimDoesNotEnableNativeMutation(t *testing.T) {
	_, a, _ := n124ProbeClones(t)
	ref := "refs/air-worker/claims/N-120"
	n124ProbeMustGit(t, a, "", "push", "--force-with-lease="+ref+":", "origin", "HEAD:"+ref)
	before := n124Snapshot(t, a)
	for _, run := range []func() int{
		func() int { return cmdPlanNodeNew(n124NewArgs(a)) },
		func() int {
			return cmdPlanNodeClose([]string{"N-119_base", "-product", a, "-receipt", "fixture", "-json"})
		},
	} {
		code, out := captureLoopOutput(t, run)
		n124AssertConflict(t, code, out, "git_authority_unproven")
	}
	if !reflect.DeepEqual(before, n124Snapshot(t, a)) {
		t.Fatal("unproven claim changed product")
	}
	t.Log("HOLD: native canonical new/close remain unsupported; remote primitive success is not product acceptance")
}
