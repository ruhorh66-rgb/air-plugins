package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// This is a deterministic fixture-only race. The native local bytes have
// ALREADY been read; Git supplies the authentic parent blob; a concurrent
// writer changes the very same file before projectionUnchanged compares it.
// A successful replay must not acknowledge an out-of-date local generation.
func TestPlanCanonicalRecoveryRejectsMidReadMutation(t *testing.T) {
	for _, tc := range []struct {
		label, path string
		nth         int
	}{
		{"native-node", "plan/N-119_base.md", 2},
		{"learn-journal", "learn/events.jsonl", 2},
		{"plan-spine", "PLAN.md", 3},
	} {
		t.Run(tc.label, func(t *testing.T) {
			root, origin, old, _, _ := canonicalCloseFixture(t)
			req := sharedPlanCreateRequest{
				ID: "race-new-120", Title: "Next", Parent: "PLAN.md",
				Owner: "fixture", DoneWhen: "verified", Trigger: "fixture",
			}
			initial, code := captureStdout(t, func() int {
				return cmdPlanNode([]string{
					"new", "-product", root, "-request-id", req.ID,
					"-title", req.Title, "-parent", req.Parent,
					"-owner", req.Owner, "-done-when", req.DoneWhen,
					"-trigger", req.Trigger, "-json",
				})
			})
			if code != 0 {
				t.Fatalf("fixture initial atomic new rejected: %s", initial)
			}
			remote := candidateGit(t, origin, "rev-parse", publishRef)
			if remote == old || candidateGit(t, origin, "rev-parse", "refs/air-worker/claims/N-120") != remote {
				t.Fatal("fixture did not publish exact atomic native new")
			}
			// Independent Git primitives, with callback under test control.
			gitRaw := func(dir string, args ...string) ([]byte, error) {
				cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
				cmd.Env = append(os.Environ(),
					"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
					"GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1",
					"GIT_TERMINAL_PROMPT=0")
				return cmd.Output()
			}
			run := func(dir string, args ...string) (string, error) {
				raw, err := gitRaw(dir, args...)
				return strings.TrimSpace(string(raw)), err
			}
			hits, changed := 0, false
			runRaw := func(dir string, args ...string) ([]byte, error) {
				raw, err := gitRaw(dir, args...)
				if err == nil && len(args) == 3 && args[0] == "cat-file" &&
					args[1] == "blob" && args[2] == old+":"+tc.path {
					hits++
					if hits == tc.nth {
						leaf := filepath.Join(root, filepath.FromSlash(tc.path))
						if err := os.WriteFile(leaf, []byte(fmt.Sprintf("RACE %s\n", tc.path)), 0600); err != nil {
							t.Fatalf("unable to inject concurrent mutation: %v", err)
						}
						changed = true
					}
				}
				return raw, err
			}
			node, err := recoverCanonicalFixtureNew(
				root, origin, old, remote, req, req.Owner, run, runRaw,
			)
			if !changed || hits < tc.nth {
				t.Fatalf("race injection was not reached: hits=%d changed=%t", hits, changed)
			}
			if err == nil {
				t.Fatalf("accepted stale %s after deterministic mid-read mutation: node=%s", tc.label, node.ID)
			}
			if candidateGit(t, origin, "rev-parse", publishRef) != remote ||
				candidateGit(t, origin, "rev-parse", "refs/air-worker/claims/N-120") != remote {
				t.Fatalf("rejected replay altered immutable canonical Git refs: %s", tc.label)
			}
			// Only this disposable local fixture was altered; canonical
			// commit and numeric claim remain exactly readback-verified.
		})
	}
}
