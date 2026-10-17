package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Exercise the native entry point, never the candidate preparation helper.
func TestPlanCanonicalNewNative(t *testing.T) {
	root, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# Fixture\n"), 0600); err != nil {
		t.Fatal(err)
	}
	n124SeedNode(t, root, "N-119_base", "fixture base")
	authority, err := json.Marshal(map[string]string{
		"schema": "air-worker.plan.authority/v1", "mode": "git-canonical-fixture",
		"root": root, "origin": origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan-authority.json"), authority, 0600); err != nil {
		t.Fatal(err)
	}
	candidateGit(t, root, "init")
	candidateGit(t, root, "-c", "core.autocrlf=false", "add", ".")
	candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "fixture base")
	old := candidateGit(t, root, "rev-parse", "HEAD")
	candidateGit(t, origin, "init", "--bare")
	declareIsolatedPlanFixture(t, origin)
	if err := os.CopyFS(filepath.Join(origin, "objects"), os.DirFS(filepath.Join(root, ".git", "objects"))); err != nil {
		t.Fatal(err)
	}
	candidateGit(t, origin, "update-ref", publishRef, old)
	before := candidateOriginal(t, root)
	out, code := captureStdout(t, func() int {
		return cmdPlanNode([]string{"new", "-product", root, "-request-id", "native-120",
			"-title", "Next", "-parent", "PLAN.md", "-owner", "fixture",
			"-done-when", "verified", "-trigger", "fixture", "-json"})
	})
	var result struct {
		Action    string   `json:"action"`
		Source    string   `json:"source"`
		Node      planNode `json:"node"`
		OID       string   `json:"remote_oid"`
		Operation string   `json:"operation_id"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid native JSON: exit=%d output=%q: %v", code, out, err)
	}
	if code != 0 || result.Action != "created" || result.Source != "git-canonical-fixture" || result.Node.ID != "N-120_next" || result.Operation != "native-120" {
		t.Fatalf("native canonical new: exit=%d output=%s", code, out)
	}
	if result.OID == old || result.OID != candidateGit(t, origin, "rev-parse", publishRef) {
		t.Fatalf("native result does not identify remote candidate: %s", out)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("native canonical new changed local PLAN/node/LEARN/index/refs")
	}
	wantPaths := "PLAN.md\nplan/N-120_next.md\nplan/event-outbox/native-120.json"
	if paths := candidateGit(t, origin, "diff-tree", "--no-commit-id", "--name-only", "-r", old, result.OID); paths != wantPaths {
		t.Fatalf("unexpected native delta: %s", paths)
	}
	files := map[string][]byte{}
	for _, name := range strings.Split(wantPaths, "\n") {
		// candidateGit trims trailing whitespace; restore document final newlines.
		raw := candidateGit(t, origin, "cat-file", "blob", result.OID+":"+name)
		if strings.HasSuffix(name, ".md") {
			raw += "\n"
		}
		files[name] = []byte(raw)
	}
	if err := validateCandidateContent("", "native-120", files); err != nil {
		t.Fatal(err)
	}
	retry, code := captureStdout(t, func() int {
		return cmdPlanNode([]string{"new", "-product", root, "-request-id", "native-120",
			"-title", "Next", "-parent", "PLAN.md", "-owner", "fixture",
			"-done-when", "verified", "-trigger", "fixture", "-json"})
	})
	var replay struct {
		Action string   `json:"action"`
		OID    string   `json:"remote_oid"`
		Node   planNode `json:"node"`
	}
	if err := json.Unmarshal([]byte(retry), &replay); err != nil || code != 0 ||
		replay.Action != "created" || replay.OID != result.OID ||
		replay.Node.ID != result.Node.ID || replay.Node.CreatedAt != result.Node.CreatedAt {
		t.Fatalf("exact accepted replay did not recover the original immutable result: exit=%d %s", code, retry)
	}
	if candidateGit(t, origin, "rev-parse", publishRef) != result.OID || !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("retry mutated original or published another event")
	}
	changed, code := captureStdout(t, func() int {
		return cmdPlanNode([]string{"new", "-product", root, "-request-id", "native-120",
			"-title", "Different", "-parent", "PLAN.md", "-owner", "fixture",
			"-done-when", "verified", "-trigger", "fixture", "-json"})
	})
	var wrong struct {
		Action string `json:"action"`
	}
	if json.Unmarshal([]byte(changed), &wrong) != nil || code == 0 || wrong.Action != "conflict" {
		t.Fatalf("changed request payload was accepted: code=%d output=%s", code, changed)
	}
	if candidateGit(t, origin, "rev-parse", publishRef) != result.OID || !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("changed-payload retry mutated original or canonical ref")
	}
	// Only a readback-verified, clean disposable publisher may advance its
	// source generation. A different logical request after this sync must get
	// N121, not reuse the stale N120; production WIP is never reset.
	if dirty := candidateGit(t, root, "status", "--porcelain"); dirty != "" {
		t.Fatalf("cannot synchronize dirty disposable publisher: %s", dirty)
	}
	candidateGit(t, root, "reset", "--hard", result.OID)
	if got := candidateGit(t, root, "rev-parse", "HEAD"); got != result.OID {
		t.Fatalf("publisher did not sync accepted generation: %s", got)
	}
	syncedBefore := candidateOriginal(t, root)
	next, nextCode := captureStdout(t, func() int {
		return cmdPlanNode([]string{"new", "-product", root, "-request-id", "fresh-121",
			"-title", "After", "-parent", "PLAN.md", "-owner", "fixture",
			"-done-when", "verified", "-trigger", "fixture", "-json"})
	})
	var nextResult struct {
		Action string   `json:"action"`
		OID    string   `json:"remote_oid"`
		Node   planNode `json:"node"`
	}
	if err := json.Unmarshal([]byte(next), &nextResult); err != nil ||
		nextCode != 0 || nextResult.Action != "created" ||
		nextResult.Node.ID != "N-121_after" ||
		nextResult.OID == result.OID ||
		candidateGit(t, origin, "rev-parse", publishRef) != nextResult.OID ||
		candidateGit(t, origin, "show", "-s", "--format=%P", nextResult.OID) != result.OID {
		t.Fatalf("fresh request must allocate unique canonical N121: exit=%d output=%s", nextCode, next)
	}
	if !reflect.DeepEqual(syncedBefore, candidateOriginal(t, root)) {
		t.Fatal("fresh N121 allocation modified local PLAN/LEARN/WIP")
	}
}

func TestPlanCanonicalNewRejects(t *testing.T) {
	for _, mode := range []string{"missing-authority", "duplicate-authority", "case-authority", "foreign-origin", "dirty", "stale", "request", "actor", "owner", "parent", "argv", "git-env", "config", "duplicate-number"} {
		t.Run(mode, func(t *testing.T) {
			root, err := resolveLinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			origin, err := resolveLinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			write := func(name string, raw []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			write("PLAN.md", []byte("# Fixture\n"))
			n124SeedNode(t, root, "N-119_base", "fixture")
			raw, err := json.Marshal(map[string]string{"schema": "air-worker.plan.authority/v1", "mode": "git-canonical-fixture", "root": root, "origin": origin})
			if err != nil {
				t.Fatal(err)
			}
			write("plan-authority.json", raw)
			candidateGit(t, root, "init")
			candidateGit(t, root, "-c", "core.autocrlf=false", "add", ".")
			candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "base")
			old := candidateGit(t, root, "rev-parse", "HEAD")
			candidateGit(t, origin, "init", "--bare")
			declareIsolatedPlanFixture(t, origin)
			if err := os.CopyFS(filepath.Join(origin, "objects"), os.DirFS(filepath.Join(root, ".git", "objects"))); err != nil {
				t.Fatal(err)
			}
			candidateGit(t, origin, "update-ref", publishRef, old)
			args := []string{"new", "-product", root, "-request-id", "reject-120", "-title", "Next", "-parent", "PLAN.md", "-owner", "fixture", "-done-when", "verified", "-json"}
			switch mode {
			case "missing-authority":
				if err := os.Remove(filepath.Join(root, "plan-authority.json")); err != nil {
					t.Fatal(err)
				}
			case "duplicate-authority":
				write("plan-authority.json", append([]byte(`{"mode":"git-canonical-fixture",`), raw[1:]...))
			case "case-authority":
				write("plan-authority.json", []byte(strings.Replace(string(raw), `"mode"`, `"Mode"`, 1)))
			case "foreign-origin":
				write("plan-authority.json", []byte(strings.Replace(string(raw), `"origin":`, `"unknown":`, 1)))
			case "dirty":
				write("PLAN.md", []byte("dirty\n"))
			case "stale":
				candidateGit(t, origin, "update-ref", "-d", publishRef)
			case "request":
				args = append(args, "-request-id", "../escape")
			case "actor":
				args = append(args, "-actor-kind", "invalid", "-actor", "name")
			case "owner":
				args = append(args, "-owner", "")
			case "parent":
				args = append(args, "-parent", "N-118_missing")
			case "argv":
				args = append(args, "extra")
			case "git-env":
				t.Setenv("GIT_INDEX_FILE", filepath.Join(root, "foreign-index"))
			case "config":
				candidateGit(t, root, "config", "core.fsmonitor", "invalid-fixture-monitor")
			case "duplicate-number":
				n124SeedNode(t, root, "N-119_other", "fixture")
			}
			before, remoteBefore := candidateOriginal(t, root), n124Snapshot(t, origin)
			out, code := captureStdout(t, func() int { return cmdPlanNode(args) })
			if code == 0 || !strings.Contains(out, `"action":"conflict"`) {
				t.Fatalf("accepted %s: exit=%d %s", mode, code, out)
			}
			if !reflect.DeepEqual(before, candidateOriginal(t, root)) || !reflect.DeepEqual(remoteBefore, n124Snapshot(t, origin)) {
				t.Fatal("rejected request mutated local state or origin")
			}
		})
	}
}
