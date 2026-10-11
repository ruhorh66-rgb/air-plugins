package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func candidateGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.CommandContext(context.Background(), "git", append([]string{"-C", root}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	b, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func candidateFixture(t *testing.T) (string, string, map[string][]byte) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			t.Setenv(key, "")
		}
	}
	root, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "air-worker"), 0700); err != nil {
		t.Fatal(err)
	}
	declareIsolatedPlanFixture(t, root)
	if err := os.WriteFile(filepath.Join(root, "air-worker", "PLAN.md"), []byte("original\n"), 0600); err != nil {
		t.Fatal(err)
	}
	candidateGit(t, root, "init")
	candidateGit(t, root, "-c", "core.autocrlf=false", "add", ".")
	candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "base")
	return root, candidateGit(t, root, "rev-parse", "HEAD"), candidatePayload("op-1", "N-121_next")
}

func candidatePayload(operation, id string) map[string][]byte {
	n := planNode{ID: id, Title: "Next", Parent: "stage", Trigger: "fixture", Owner: "fixture", DoneWhen: "verified", Status: "open", ReturnTo: "stage", CreatedAt: "2000-01-01T00:00:00Z", UpdatedAt: "2000-01-01T00:00:00Z"}
	plan := []byte("original\n" + planNodeMarker + "\n" + spineNodeLine(n) + "\n")
	node := []byte(strings.ReplaceAll(string(renderPlanNode(n)), "\n", "\r\n"))
	outbox, _ := json.Marshal(map[string]string{"schema": "air-worker.plan.candidate-operation/v1", "operation_id": operation, "node_id": id, "status": n.Status, "parent": n.Parent, "done_when": n.DoneWhen, "plan_sha256": fmt.Sprintf("%x", sha256.Sum256(plan)), "node_sha256": fmt.Sprintf("%x", sha256.Sum256(node))})
	return map[string][]byte{"air-worker/PLAN.md": plan, "air-worker/plan/" + id + ".md": node, "air-worker/plan/event-outbox/" + operation + ".json": outbox}
}

func TestPlanCandidateContentRejects(t *testing.T) {
	for _, mode := range []string{"missing-node", "wrong-id", "duplicate-id", "duplicate-plan-id", "conflicting-number", "status", "parent", "done-when", "malformed", "operation", "outbox-node", "digest", "unknown", "duplicate-key", "case-alias", "null", "trailing"} {
		t.Run(mode, func(t *testing.T) {
			root, old, files := candidateFixture(t)
			plan, node, out := "air-worker/PLAN.md", "air-worker/plan/N-121_next.md", "air-worker/plan/event-outbox/op-1.json"
			switch mode {
			case "missing-node":
				files[plan] = []byte("original\n")
			case "wrong-id":
				files[node] = []byte(strings.Replace(string(files[node]), `id: "N-121_next"`, `id: "N-122_wrong"`, 1))
			case "duplicate-id":
				files[node] = []byte(strings.Replace(string(files[node]), `id: "N-121_next"`, "id: \"N-121_next\"\r\nid: \"N-121_next\"", 1))
			case "duplicate-plan-id":
				files[plan] = append(files[plan], []byte("[[N-121_next]]\n")...)
			case "conflicting-number":
				files[plan] = append(files[plan], []byte("[[N-121_other]]\n")...)
			case "status", "parent", "done-when":
				key := strings.ReplaceAll(mode, "-", "_")
				var payload map[string]string
				json.Unmarshal(files[out], &payload)
				payload[key] = "forged"
				files[out], _ = json.Marshal(payload)
			case "malformed":
				files[out] = []byte("{")
			case "operation":
				files[out] = []byte(strings.Replace(string(files[out]), "op-1", "op-2", 1))
			case "outbox-node":
				files[out] = []byte(strings.Replace(string(files[out]), "N-121_next", "N-122_wrong", 1))
			case "digest":
				files[out] = []byte(strings.Replace(string(files[out]), fmt.Sprintf("%x", sha256.Sum256(files[node])), strings.Repeat("0", 64), 1))
			case "unknown":
				files[out] = append([]byte(`{"unknown":"value",`), files[out][1:]...)
			case "duplicate-key":
				files[out] = append([]byte(`{"operation_id":"op-1",`), files[out][1:]...)
			case "case-alias":
				files[out] = []byte(strings.Replace(string(files[out]), "operation_id", "OPERATION_ID", 1))
			case "null":
				files[out] = []byte(strings.Replace(string(files[out]), `"op-1"`, "null", 1))
			case "trailing":
				files[out] = append(files[out], []byte(" {}")...)
			}
			// Repair hashes after document forgery to exercise identity checks independently.
			if mode == "missing-node" || mode == "wrong-id" || mode == "duplicate-id" || mode == "duplicate-plan-id" || mode == "conflicting-number" {
				var payload map[string]string
				json.Unmarshal(files[out], &payload)
				payload["plan_sha256"] = fmt.Sprintf("%x", sha256.Sum256(files[plan]))
				payload["node_sha256"] = fmt.Sprintf("%x", sha256.Sum256(files[node]))
				files[out], _ = json.Marshal(payload)
			}
			before := n124Snapshot(t, root)
			if got, err := preparePlanCandidate(context.Background(), root, old, "op-1", files); err == nil || got.OID != "" {
				t.Fatalf("accepted forged content: %+v %v", got, err)
			}
			if !reflect.DeepEqual(before, n124Snapshot(t, root)) {
				t.Fatal("rejection wrote files or Git objects")
			}
		})
	}
}

func candidateOriginal(t *testing.T, root string) map[string]string {
	t.Helper()
	all := n124Snapshot(t, root)
	for p := range all {
		rel, _ := filepath.Rel(root, p)
		if strings.HasPrefix(rel, ".git"+string(filepath.Separator)) && rel != filepath.Join(".git", "index") && rel != filepath.Join(".git", "HEAD") && !strings.HasPrefix(rel, filepath.Join(".git", "refs")) {
			delete(all, p)
		}
	}
	return all
}

func TestPlanCandidateImmutable(t *testing.T) {
	root, old, files := candidateFixture(t)
	before := candidateOriginal(t, root)
	got, err := preparePlanCandidate(context.Background(), root, old, "op-1", files)
	if err != nil {
		t.Fatal(err)
	}
	if got.Parent != old || got.Tree == "" || got.OID == old || !reflect.DeepEqual(got.Files, files) {
		t.Fatalf("bad readback: %+v", got)
	}
	again, err := preparePlanCandidate(context.Background(), root, old, "op-1", files)
	if err != nil || again.OID != got.OID {
		t.Fatalf("retry: %+v %v", again, err)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("original files/index/refs changed")
	}
	if candidateGit(t, root, "rev-parse", "HEAD") != old {
		t.Fatal("HEAD advanced")
	}
	if got := candidateGit(t, root, "diff-tree", "--no-commit-id", "--name-only", "-r", old, got.OID); got != "air-worker/PLAN.md\nair-worker/plan/N-121_next.md\nair-worker/plan/event-outbox/op-1.json" {
		t.Fatalf("unexpected tree delta: %s", got)
	}
	t.Run("clone-old-head", func(t *testing.T) {
		clone := filepath.Join(t.TempDir(), "clone")
		c := exec.CommandContext(context.Background(), "git", "-C", root, "clone", "--local", ".", clone)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		out, err := c.CombinedOutput()
		if err != nil && strings.Contains(string(out), "couldn't create signal pipe, Win32 error 5") {
			t.Skipf("sandbox blocks local clone: %s", out)
		}
		if err != nil {
			t.Fatalf("clone: %v %s", err, out)
		}
		if candidateGit(t, clone, "rev-parse", "HEAD") != old || candidateGit(t, clone, "show", "HEAD:air-worker/PLAN.md") != "original" {
			t.Fatal("unpublished candidate visible")
		}
	})
}

func TestPlanCandidateRejects(t *testing.T) {
	for _, mode := range []string{"traversal", "absolute", "backslash", "unauthorized", "collision", "directory-collision", "number-collision", "duplicate", "duplicate-number", "empty-oid", "bad-oid", "unknown-oid", "dirty", "staged", "ignored", "skip-worktree", "authority", "missing-git", "git-env", "remote", "cancelled", "oversize", "empty-operation", "plan-only", "missing-outbox", "missing-node", "extra-node-outbox", "wrong-outbox", "mismatched-prefix"} {
		t.Run(mode, func(t *testing.T) {
			root, old, files := candidateFixture(t)
			operation := "op-1"
			ctx := context.Background()
			switch mode {
			case "traversal":
				files["air-worker/plan/../../escape"] = []byte("bad")
			case "absolute":
				files[filepath.ToSlash(filepath.Join(root, "escape"))] = []byte("bad")
			case "backslash":
				files[`air-worker\plan\N-121_next.md`] = []byte("bad")
			case "unauthorized":
				files["air-worker/learn/events.jsonl"] = []byte("bad")
			case "directory-collision":
				if err := os.MkdirAll(filepath.Join(root, "air-worker", "plan", "N-121_next.md"), 0700); err != nil {
					t.Fatal(err)
				}
			case "collision", "number-collision":
				name := "N-121_next.md"
				if mode == "number-collision" {
					name = "N-121_other.md"
				}
				if err := os.MkdirAll(filepath.Join(root, "air-worker", "plan"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "air-worker", "plan", name), []byte("existing"), 0600); err != nil {
					t.Fatal(err)
				}
				candidateGit(t, root, "add", ".")
				candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "collision")
				old = candidateGit(t, root, "rev-parse", "HEAD")
			case "duplicate":
				files["air-worker/plan/N-121_NEXT.md"] = []byte("bad")
			case "duplicate-number":
				files["air-worker/plan/N-121_other.md"] = []byte("bad")
			case "empty-oid":
				old = ""
			case "bad-oid":
				old = "HEAD"
			case "unknown-oid":
				old = strings.Repeat("a", 40)
			case "dirty", "staged", "ignored", "skip-worktree":
				if mode == "ignored" {
					if err := os.WriteFile(filepath.Join(root, ".git", "info", "exclude"), []byte("air-worker/junk\n"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				name := "PLAN.md"
				if mode == "ignored" {
					name = "junk"
				}
				if mode == "skip-worktree" {
					candidateGit(t, root, "update-index", "--skip-worktree", "air-worker/PLAN.md")
				}
				if err := os.WriteFile(filepath.Join(root, "air-worker", name), []byte("dirty"), 0600); err != nil {
					t.Fatal(err)
				}
				if mode == "staged" {
					candidateGit(t, root, "add", ".")
				}
			case "authority":
				if err := os.Remove(filepath.Join(root, "plan-authority.json")); err != nil {
					t.Fatal(err)
				}
			case "missing-git":
				t.Setenv("PATH", "")
			case "git-env":
				t.Setenv("GIT_INDEX_FILE", filepath.Join(root, "injected"))
			case "remote":
				candidateGit(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "unused.git"))
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "oversize":
				files["air-worker/PLAN.md"] = make([]byte, 4*1024*1024+1)
			case "empty-operation":
				operation = ""
			case "plan-only":
				files = map[string][]byte{"air-worker/PLAN.md": []byte("candidate\n")}
			case "missing-outbox":
				delete(files, "air-worker/plan/event-outbox/op-1.json")
			case "missing-node":
				delete(files, "air-worker/plan/N-121_next.md")
			case "extra-node-outbox":
				files["air-worker/plan/N-122_extra.md"] = []byte("extra\n")
				files["air-worker/plan/event-outbox/op-1-extra.json"] = []byte(`{"event":"extra"}`)
			case "wrong-outbox":
				delete(files, "air-worker/plan/event-outbox/op-1.json")
				files["air-worker/plan/event-outbox/op-2.json"] = []byte(`{"event":"synthetic"}`)
			case "mismatched-prefix":
				files["PLAN.md"] = files["air-worker/PLAN.md"]
			}
			before := candidateOriginal(t, root)
			if got, err := preparePlanCandidate(ctx, root, old, operation, files); err == nil || got.OID != "" {
				t.Fatalf("accepted %s: %+v %v", mode, got, err)
			}
			if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
				t.Fatal("rejection changed original")
			}
		})
	}
}

func TestPlanCandidateExistingJunctionAncestor(t *testing.T) {
	for current := os.TempDir(); filepath.Dir(current) != current; current = filepath.Dir(current) {
		if _, err := os.Readlink(current); err == nil {
			if err := candidateNoLinks(filepath.Join(current, "missing-child", "PLAN.md")); err == nil {
				t.Fatal("junction ancestor accepted")
			}
			return
		}
	}
	t.Skip("OS temp path has no existing junction/link ancestor")
}

func TestPlanCandidateLinkAncestor(t *testing.T) {
	root, old, files := candidateFixture(t)
	link := filepath.Join(root, "air-worker", "plan")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Skipf("symlink privilege unavailable: %v", err)
	}
	if err := candidateNoLinks(filepath.Join(link, "N-121_next.md")); err == nil {
		t.Fatal("link ancestor accepted")
	}
	if _, err := preparePlanCandidate(context.Background(), root, old, "op-1", files); err == nil {
		t.Fatal("linked fixture accepted")
	}
}
