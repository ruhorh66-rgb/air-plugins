package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Independent requests share the declared publisher; authority is never rebound.
func TestPlanCanonicalRecoveryLogicalRequests(t *testing.T) {
	t.Setenv("GIT_PAGER", "")
	root, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin, err := resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("PLAN.md", []byte("# Fixture\n"))
	n124SeedNode(t, root, "N-119_base", "verified")
	authority, err := json.Marshal(map[string]string{"schema": "air-worker.plan.authority/v1", "mode": "git-canonical-fixture", "root": root, "origin": origin})
	if err != nil {
		t.Fatal(err)
	}
	write("plan-authority.json", authority)
	if err := os.Mkdir(filepath.Join(root, "learn"), 0700); err != nil {
		t.Fatal(err)
	}
	write("learn/events.jsonl", []byte("fixture journal\n"))
	write("foreign.txt", []byte("foreign committed content\n"))
	candidateGit(t, root, "init")
	candidateGit(t, root, "-c", "core.autocrlf=false", "add", ".")
	candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "logical recovery fixture")
	old := candidateGit(t, root, "rev-parse", "HEAD")
	candidateGit(t, origin, "init", "--bare")
	declareIsolatedPlanFixture(t, origin)
	if err := os.CopyFS(filepath.Join(origin, "objects"), os.DirFS(filepath.Join(root, ".git", "objects"))); err != nil {
		t.Fatal(err)
	}
	candidateGit(t, origin, "update-ref", publishRef, old)
	before := candidateOriginal(t, root)
	request := func(id string) (string, int) {
		t.Helper()
		return captureStdout(t, func() int {
			return cmdPlanNode([]string{"new", "-product", root, "-request-id", id, "-title", "Next", "-parent", "PLAN.md", "-owner", "fixture", "-done-when", "verified", "-trigger", "fixture", "-json"})
		})
	}
	accepted, code := request("logical-120")
	var result struct {
		Node planNode `json:"node"`
		OID  string   `json:"remote_oid"`
	}
	if err := json.Unmarshal([]byte(accepted), &result); err != nil || code != 0 || result.Node.ID != "N-120_next" || result.OID == old || result.OID != candidateGit(t, origin, "rev-parse", publishRef) {
		t.Fatalf("first logical request must accept canonical N120: exit=%d output=%s", code, accepted)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("acceptance changed local publisher state")
	}
	write("foreign.txt", []byte("foreign uncommitted work\n"))
	before = candidateOriginal(t, root)
	// Accepted replay is only read-only when the local native projection is
	// still its original source generation. Foreign WIP outside PLAN/LEARN
	// may coexist, but a tampered native node or LEARN must never be ignored.
	for _, name := range []string{"plan/N-119_base.md", "learn/events.jsonl"} {
		original, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		write(name, []byte("tampered native projection\n"))
		tamperedBefore, canonicalBefore := candidateOriginal(t, root), candidateGit(t, origin, "rev-parse", publishRef)
		out, tamperedRC := request("logical-120")
		var denied struct {
			Action string `json:"action"`
		}
		if tamperedRC == 0 || json.Unmarshal([]byte(out), &denied) != nil || denied.Action != "conflict" ||
			candidateGit(t, origin, "rev-parse", publishRef) != canonicalBefore ||
			!reflect.DeepEqual(tamperedBefore, candidateOriginal(t, root)) {
			t.Fatalf("tampered %s must deny exact replay without any mutation: rc=%d %s", name, tamperedRC, out)
		}
		write(name, original)
		if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
			t.Fatalf("failed to restore native %s fixture bytes", name)
		}
	}
	// Index flags can hide a modified tracked file from git status.
	// The immutable parent blob comparison must still deny exact replay.
	for _, scenario := range []struct{ file, set, clear string }{
		{"plan/N-119_base.md", "--skip-worktree", "--no-skip-worktree"},
		{"learn/events.jsonl", "--assume-unchanged", "--no-assume-unchanged"},
	} {
		original, err := os.ReadFile(filepath.Join(root, scenario.file))
		if err != nil {
			t.Fatal(err)
		}
		candidateGit(t, root, "update-index", scenario.set, scenario.file)
		write(scenario.file, []byte("modified behind index flag\n"))
		hiddenBefore, remoteBefore := candidateOriginal(t, root), candidateGit(t, origin, "rev-parse", publishRef)
		out, hiddenRC := request("logical-120")
		var response struct {
			Action string `json:"action"`
		}
		if hiddenRC == 0 || json.Unmarshal([]byte(out), &response) != nil || response.Action != "conflict" ||
			candidateGit(t, origin, "rev-parse", publishRef) != remoteBefore ||
			!reflect.DeepEqual(hiddenBefore, candidateOriginal(t, root)) {
			t.Fatalf("hidden native modification %s accepted replay: rc=%d %s", scenario.file, hiddenRC, out)
		}
		write(scenario.file, original)
		candidateGit(t, root, "update-index", scenario.clear, scenario.file)
		before = candidateOriginal(t, root)
	}
	loser, code := request("logical-loser")
	var conflict struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(loser), &conflict); err != nil || code == 0 || conflict.Action != "conflict" {
		t.Fatalf("stale competitor must conflict: exit=%d output=%s", code, loser)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("stale competitor changed PLAN/node/LEARN/index/foreign WIP")
	}
	replay, code := request("logical-120")
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("recovery changed PLAN/node/LEARN/index/foreign WIP")
	}
	if got := candidateGit(t, origin, "rev-parse", publishRef); got != result.OID {
		t.Fatalf("retry advanced canonical ref: %s", got)
	}
	if code != 0 {
		t.Fatalf("exact accepted retry unexpectedly failed: %s", replay)
	}
	var previous struct {
		Node      planNode `json:"node"`
		RemoteOID string   `json:"remote_oid"`
	}
	var retried struct {
		Node      planNode `json:"node"`
		RemoteOID string   `json:"remote_oid"`
	}
	if json.Unmarshal([]byte(accepted), &previous) != nil || json.Unmarshal([]byte(replay), &retried) != nil || retried.RemoteOID != previous.RemoteOID || retried.Node.ID != previous.Node.ID || retried.Node.Title != previous.Node.Title || retried.Node.CreatedAt != previous.Node.CreatedAt {
		t.Fatalf("same request must recover original accepted result: exit=%d replay=%s accepted=%s", code, replay, accepted)
	}
	// A distinct logical request, still based on stale HEAD, must not locally allocate N121.
	next, code := request("logical-121")
	if code == 0 {
		t.Fatalf("stale next request unexpectedly succeeded: %s", next)
	}
	var nextResult struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal([]byte(next), &nextResult); err != nil || nextResult.Action != "conflict" {
		t.Fatalf("stale next request must fail closed: %s", next)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("stale next request changed PLAN/node/LEARN/index/foreign WIP")
	}
	// A different operation has advanced canonical HEAD. The original request must not be
	// replayed from an unrelated descendant merely because its outbox entry still exists.
	otherTree := candidateGit(t, origin, "rev-parse", result.OID+"^{tree}")
	other := n124ProbeMustGit(t, origin, "unrelated operation\n", "commit-tree", otherTree, "-p", result.OID)
	candidateGit(t, origin, "update-ref", publishRef, other, result.OID)
	remoteBefore := candidateGit(t, origin, "rev-parse", publishRef)
	localBefore := candidateOriginal(t, root)
	diverged, code := request("logical-120")
	if code == 0 {
		t.Fatalf("request on non-accepted canonical descendant must conflict: %s", diverged)
	}
	var divergedResult struct {
		Action string `json:"action"`
	}
	if json.Unmarshal([]byte(diverged), &divergedResult) != nil || divergedResult.Action != "conflict" {
		t.Fatalf("expected fail-closed conflict for non-accepted descendant: %s", diverged)
	}
	if diverged == "" {
		t.Fatalf("expected JSON conflict response")
	}
	if candidateGit(t, origin, "rev-parse", publishRef) != remoteBefore || !reflect.DeepEqual(localBefore, candidateOriginal(t, root)) {
		t.Fatal("descendant replay changed local state or canonical ref")
	}
	// The original accepted commit is canonical again. The same logical request should recover it.
	candidateGit(t, origin, "update-ref", publishRef, result.OID, remoteBefore)
	readback, code := request("logical-120")
	if code != 0 {
		t.Fatalf("canonical exact replay after ref restoration failed: %s", readback)
	}
	var restored struct {
		OID  string   `json:"remote_oid"`
		Node planNode `json:"node"`
	}
	if json.Unmarshal([]byte(readback), &restored) != nil || restored.OID != result.OID || restored.Node.ID != result.Node.ID {
		t.Fatalf("recovered result differs from accepted canonical commit: %s", readback)
	}
}
