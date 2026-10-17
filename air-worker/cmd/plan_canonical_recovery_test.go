package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// LPR chose one trusted physical publisher and two logical stale clients.
// A copied second physical publisher must remain BLOCKED: re-declaring the
// authority path or copying Git metadata is not a legitimate recovery grant.
func TestPlanCanonicalRecoveryRejectsSecondPhysicalPublisher(t *testing.T) {
	t.Setenv("GIT_PAGER", "")
	for _, rebind := range []bool{false, true} {
		name := "canonical-declaration"
		if rebind {
			name = "local-declaration"
		}
		t.Run(name, func(t *testing.T) {
			root, err := resolveLinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			other, err := resolveLinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			origin, err := resolveLinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			writeAuthority := func(dir string) {
				t.Helper()
				raw, err := json.Marshal(map[string]string{"schema": "air-worker.plan.authority/v1", "mode": "git-canonical-fixture", "root": dir, "origin": origin})
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "plan-authority.json"), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# Fixture\n"), 0600); err != nil {
				t.Fatal(err)
			}
			n124SeedNode(t, root, "N-119_base", "verified")
			writeAuthority(root)
			candidateGit(t, root, "init")
			candidateGit(t, root, "-c", "core.autocrlf=false", "add", ".")
			candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "two-client fixture")
			old := candidateGit(t, root, "rev-parse", "HEAD")
			candidateGit(t, origin, "init", "--bare")
			declareIsolatedPlanFixture(t, origin)
			if err := os.CopyFS(filepath.Join(origin, "objects"), os.DirFS(filepath.Join(root, ".git", "objects"))); err != nil {
				t.Fatal(err)
			}
			candidateGit(t, origin, "update-ref", publishRef, old)
			if err := os.CopyFS(other, os.DirFS(root)); err != nil {
				t.Fatal(err)
			}
			if rebind {
				writeAuthority(other)
			}
			if candidateGit(t, other, "rev-parse", "HEAD") != old {
				t.Fatal("clients do not share canonical base")
			}
			before := candidateOriginal(t, other)
			out, code := captureStdout(t, func() int {
				return cmdPlanNode([]string{"new", "-product", other, "-request-id", "recovery-120", "-title", "Next", "-parent", "PLAN.md", "-owner", "fixture", "-done-when", "verified", "-trigger", "fixture", "-json"})
			})
			if !reflect.DeepEqual(before, candidateOriginal(t, other)) {
				t.Fatal("second client changed local state before verified acceptance")
			}
			var conflict struct {
				Action string `json:"action"`
			}
			if err := json.Unmarshal([]byte(out), &conflict); err != nil || code != 2 || conflict.Action != "conflict" {
				t.Fatalf("untrusted second physical publisher must fail closed: exit=%d output=%s", code, out)
			}
			if candidateGit(t, origin, "rev-parse", publishRef) != old {
				t.Fatal("rejected physical client advanced canonical ref")
			}
		})
	}
}
