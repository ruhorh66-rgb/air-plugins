package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
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
		Action string `json:"action"`
		Source string `json:"source"`
		Node planNode `json:"node"`
		OID string `json:"remote_oid"`
		Operation string `json:"operation_id"`
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
}
