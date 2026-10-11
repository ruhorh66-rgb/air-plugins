package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func declareIsolatedPlanFixture(t *testing.T, root string) {
	t.Helper()
	physical, err := resolveLinks(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]string{"schema": "air-worker.plan.authority/v1", "mode": "isolated-fixture", "root": physical})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan-authority.json"), b, 0600); err != nil {
		t.Fatal(err)
	}
}

func n124SeedNode(t *testing.T, root, id, done string) planNode {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	n := planNode{ID: id, Title: id, Parent: "PLAN.md", ReturnTo: "PLAN.md", Owner: "fixture", Trigger: "test", DoneWhen: done, Status: "open", CreatedAt: now, UpdatedAt: now}
	if err := os.MkdirAll(planNodeDir(root), 0700); err != nil {
		t.Fatal(err)
	}
	n.Path = filepath.Join(planNodeDir(root), id+".md")
	if err := os.WriteFile(n.Path, renderPlanNode(n), 0600); err != nil {
		t.Fatal(err)
	}
	if err := addNodeToSpine(filepath.Join(root, "PLAN.md"), n); err != nil {
		t.Fatal(err)
	}
	return n
}

func n124Snapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			out[path] = "directory"
			return nil
		}
		b, err := os.ReadFile(path)
		if err == nil {
			out[path] = learnSHA(b)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func n124NewArgs(root string) []string {
	return []string{"-product", root, "-title", "next", "-parent", "PLAN.md", "-owner", "fixture", "-done-when", "verified", "-json"}
}

func n124AssertConflict(t *testing.T, code int, output, reason string) {
	t.Helper()
	var doc struct{ Action, Reason string }
	if code != 2 || json.Unmarshal([]byte(output), &doc) != nil || doc.Action != "conflict" || doc.Reason != reason {
		t.Fatalf("want rc=2 conflict %s, got rc=%d %s", reason, code, output)
	}
}

func TestPlanNodeN124MissingAuthorityDoesNotWrite(t *testing.T) {
	root := seedPlanNodeProduct(t)
	_ = os.Remove(filepath.Join(root, "plan-authority.json"))
	n := n124SeedNode(t, root, "N-120_existing", "original criterion")
	before := n124Snapshot(t, root)
	for _, action := range []string{"new", "close"} {
		code, output := captureLoopOutput(t, func() int {
			if action == "new" {
				return cmdPlanNodeNew(n124NewArgs(root))
			}
			return cmdPlanNodeClose([]string{n.ID, "-product", root, "-receipt", "test", "-json"})
		})
		n124AssertConflict(t, code, output, "authority_missing")
	}
	if !reflect.DeepEqual(before, n124Snapshot(t, root)) {
		t.Fatal("rejection changed product")
	}
}

func TestPlanNodeN124IsolatedFixtureN121(t *testing.T) {
	root := seedPlanNodeProduct(t)
	declareIsolatedPlanFixture(t, root)
	n := n124SeedNode(t, root, "N-120_existing", "stable criterion")
	before, _ := os.ReadFile(n.Path)
	if code := cmdPlanNodeNew(n124NewArgs(root)); code != 0 {
		t.Fatalf("create: %d", code)
	}
	after, _ := os.ReadFile(n.Path)
	if string(before) != string(after) {
		t.Fatal("existing parent/done_when/blob changed")
	}
	path, err := planNodePathByID(root, "N-121")
	if err != nil {
		t.Fatal(err)
	}
	created, err := readPlanNode(path)
	if err != nil || created.ID != "N-121_next" {
		t.Fatalf("%+v %v", created, err)
	}
}

func TestPlanNodeN124FixtureAmbiguityDoesNotWrite(t *testing.T) {
	root := seedPlanNodeProduct(t)
	declareIsolatedPlanFixture(t, root)
	n124SeedNode(t, root, "N-120_one", "first")
	n124SeedNode(t, root, "N-120_two", "second")
	before := n124Snapshot(t, root)
	code, output := captureLoopOutput(t, func() int { return cmdPlanNodeNew(n124NewArgs(root)) })
	n124AssertConflict(t, code, output, "node_identity_conflict")
	code, output = captureLoopOutput(t, func() int {
		return cmdPlanNodeClose([]string{"N-120_one", "-product", root, "-receipt", "test", "-json"})
	})
	n124AssertConflict(t, code, output, "node_identity_conflict")
	if !reflect.DeepEqual(before, n124Snapshot(t, root)) {
		t.Fatal("ambiguous product changed")
	}
}

func TestPlanNodeN124SharedRejectsBeforeLearningOrRecovery(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if err := os.Remove(filepath.Join(root, "plan-authority.json")); err != nil {
		t.Fatal(err)
	}
	n := n124SeedNode(t, root, "N-120_shared", "stable criterion")
	s := sharedLearningSettings{RuntimeRoot: t.TempDir()}
	// Even an existing recovery outbox must not be replayed without authority.
	if err := os.Mkdir(filepath.Join(s.RuntimeRoot, "plan-close"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.RuntimeRoot, "plan-close", "pending.json"), []byte(`{"phase":"prepared"}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(root, sharedLearningConfigFile), settings, 0600); err != nil {
		t.Fatal(err)
	}
	beforeRoot, beforeRuntime := n124Snapshot(t, root), n124Snapshot(t, s.RuntimeRoot)
	original := sharedPlanPreflight
	defer func() { sharedPlanPreflight = original }()
	sharedPlanPreflight = func(string, sharedLearningSettings) error {
		t.Fatal("authority rejection reached learning preflight")
		return nil
	}
	for _, run := range []func() int{
		func() int { return cmdPlanNodeNew(n124NewArgs(root)) },
		func() int { return cmdPlanNodeClose([]string{n.ID, "-product", root, "-receipt", "test", "-json"}) },
		func() int {
			return cmdSharedPlanCreate(root, s, sharedPlanCreateRequest{Operation: "new"}, "fixture", true)
		},
		func() int { return cmdSharedPlanNodeClose(root, s, n, "", n.Path, "test", "fixture", nil, true) },
	} {
		code, out := captureLoopOutput(t, run)
		n124AssertConflict(t, code, out, "authority_missing")
	}
	if !reflect.DeepEqual(beforeRoot, n124Snapshot(t, root)) || !reflect.DeepEqual(beforeRuntime, n124Snapshot(t, s.RuntimeRoot)) {
		t.Fatal("untrusted shared mutation changed PLAN, LEARN or recovery outbox")
	}
}

func TestPlanNodeN124MissingGitCannotEnableFixture(t *testing.T) {
	root := seedPlanNodeProduct(t)
	if err := os.Mkdir(filepath.Join(root, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	code, out := captureLoopOutput(t, func() int { return cmdPlanNodeNew(n124NewArgs(root)) })
	n124AssertConflict(t, code, out, "git_authority_unproven")
}

func n124Git(t *testing.T, root string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestPlanNodeN124ConcurrentStaleWorktreesFailClosed(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatal("Git required for N124 worktree test")
	}
	base := seedPlanNodeProduct(t)
	n124SeedNode(t, base, "N-119_base", "base criterion")
	n124Git(t, base, "init")
	n124Git(t, base, "add", ".")
	n124Git(t, base, "commit", "-m", "fixture common base")
	other := filepath.Join(t.TempDir(), "stale")
	n124Git(t, base, "worktree", "add", "-b", "fixture-stale", other)
	// All Git mutations are confined to disposable fixtures; the remote is intentionally unavailable.
	n124Git(t, base, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing.git"))
	n124SeedNode(t, base, "N-120_authority", "canonical criterion")
	n124Git(t, base, "add", "PLAN.md", "plan")
	n124Git(t, base, "commit", "-m", "authority advances")
	n124SeedNode(t, other, "N-120_foreign", "conflicting dirty WIP")
	for _, root := range []string{base, other} {
		declareIsolatedPlanFixture(t, root)
	}
	beforeBase, beforeOther := n124Snapshot(t, base), n124Snapshot(t, other)
	type result struct {
		code int
		out  []byte
	}
	results := make(chan result, 4)
	var wg sync.WaitGroup
	for _, root := range []string{base, other} {
		for _, action := range []string{"new", "close"} {
			wg.Add(1)
			go func(root, action string) {
				defer wg.Done()
				args := n124NewArgs(root)
				if action == "close" {
					args = []string{"N-120", "-product", root, "-receipt", "test", "-json"}
				}
				code, out := runSharedPlanChild(t, action, args)
				results <- result{code, out}
			}(root, action)
		}
	}
	wg.Wait()
	close(results)
	for r := range results {
		n124AssertConflict(t, r.code, string(r.out), "git_authority_unproven")
	}
	if !reflect.DeepEqual(beforeBase, n124Snapshot(t, base)) || !reflect.DeepEqual(beforeOther, n124Snapshot(t, other)) {
		t.Fatal("foreign WIP, PLAN, LEARN or Git metadata changed")
	}
}

func TestPlanNodeN124RejectsInvalidFixtureContract(t *testing.T) {
	for _, mode := range []string{"malformed", "copied", "unknown", "git-directory", "git-file"} {
		t.Run(mode, func(t *testing.T) {
			root := seedPlanNodeProduct(t)
			declareIsolatedPlanFixture(t, root)
			reason := "authority_invalid"
			switch mode {
			case "malformed":
				_ = os.WriteFile(filepath.Join(root, "plan-authority.json"), []byte("{"), 0600)
			case "copied":
				other := seedPlanNodeProduct(t)
				declareIsolatedPlanFixture(t, other)
				b, _ := os.ReadFile(filepath.Join(other, "plan-authority.json"))
				_ = os.WriteFile(filepath.Join(root, "plan-authority.json"), b, 0600)
			case "unknown":
				b, _ := os.ReadFile(filepath.Join(root, "plan-authority.json"))
				_ = os.WriteFile(filepath.Join(root, "plan-authority.json"), []byte(strings.Replace(string(b), "isolated-fixture", "git", 1)), 0600)
			case "git-directory":
				_ = os.Mkdir(filepath.Join(root, ".git"), 0700)
				reason = "git_authority_unproven"
			case "git-file":
				_ = os.WriteFile(filepath.Join(root, ".git"), []byte("gitdir: unavailable"), 0600)
				reason = "git_authority_unproven"
			}
			before := n124Snapshot(t, root)
			code, output := captureLoopOutput(t, func() int { return cmdPlanNodeNew(n124NewArgs(root)) })
			n124AssertConflict(t, code, output, reason)
			if !reflect.DeepEqual(before, n124Snapshot(t, root)) {
				t.Fatal("invalid authority changed files")
			}
		})
	}
}
