package main

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func canonicalCloseFixture(t *testing.T) (root, origin, old string, n planNode, receipt []byte) {
	t.Helper()
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			t.Setenv(key, "")
		}
	}
	var err error
	root, err = resolveLinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origin, err = resolveLinks(t.TempDir())
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
	n = n124SeedNode(t, root, "N-119_base", "verified")
	authority, err := json.Marshal(map[string]string{"schema": "air-worker.plan.authority/v1", "mode": "git-canonical-fixture", "root": root, "origin": origin})
	if err != nil {
		t.Fatal(err)
	}
	write("plan-authority.json", authority)
	receipt, err = json.Marshal(map[string]string{"schema": "air-worker.plan.close-receipt/v1", "request_id": "close-119", "node_id": n.ID, "node_sha256": learnSHA(renderPlanNode(n)), "result": "passed"})
	if err != nil {
		t.Fatal(err)
	}
	write("close-receipt.json", receipt)
	write("foreign.txt", []byte("foreign committed content\n"))
	if err := os.Mkdir(filepath.Join(root, "learn"), 0700); err != nil {
		t.Fatal(err)
	}
	write("learn/events.jsonl", []byte("fixture journal\n"))
	candidateGit(t, root, "init")
	candidateGit(t, root, "-c", "core.autocrlf=false", "add", ".")
	candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "fixture base")
	old = candidateGit(t, root, "rev-parse", "HEAD")
	candidateGit(t, origin, "init", "--bare")
	declareIsolatedPlanFixture(t, origin)
	if err := os.CopyFS(filepath.Join(origin, "objects"), os.DirFS(filepath.Join(root, ".git", "objects"))); err != nil {
		t.Fatal(err)
	}
	candidateGit(t, origin, "update-ref", publishRef, old)
	return
}

func TestPlanCanonicalCloseNative(t *testing.T) {
	root, origin, old, n, receipt := canonicalCloseFixture(t)
	before := candidateOriginal(t, root)
	out, code := captureStdout(t, func() int {
		return cmdPlanNodeClose([]string{n.ID, "-product", root, "-request-id", "close-119", "-receipt", "close-receipt.json", "-receipt-sha256", learnSHA(receipt), "-json"})
	})
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("close changed local state before verified acceptance")
	}
	var result struct {
		Action    string   `json:"action"`
		OID       string   `json:"remote_oid"`
		Operation string   `json:"operation_id"`
		Node      planNode `json:"node"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil || code != 0 || result.Action != "closed" || result.Operation != "close-119" || result.Node.Status != "closed" {
		t.Fatalf("native canonical close: exit=%d output=%q parse=%v", code, out, err)
	}
	if result.OID == old || result.OID != candidateGit(t, origin, "rev-parse", publishRef) || candidateGit(t, origin, "show", "-s", "--format=%P", result.OID) != old {
		t.Fatal("accepted commit/ref/parent mismatch")
	}
	if paths := candidateGit(t, origin, "diff-tree", "--no-commit-id", "--name-only", "-r", old, result.OID); paths != "PLAN.md\nplan/N-119_base.md\nplan/event-outbox/close-119.json" {
		t.Fatalf("unexpected delta: %s", paths)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, root)) {
		t.Fatal("close changed local PLAN/node/journal/index/foreign content/refs")
	}
	outbox := candidateGit(t, origin, "cat-file", "blob", result.OID+":plan/event-outbox/close-119.json")
	var op map[string]string
	if err := json.Unmarshal([]byte(outbox), &op); err != nil {
		t.Fatal(err)
	}
	ref := "git:" + old + ":close-receipt.json#sha256:" + learnSHA(receipt)
	if op["operation_id"] != "close-119" || op["receipt_ref"] != ref || op["receipt_sha256"] != learnSHA(receipt) || len(result.Node.Receipts) != 1 || result.Node.Receipts[0] != ref {
		t.Fatal("receipt/request binding missing")
	}
	plan := candidateGit(t, origin, "cat-file", "blob", result.OID+":PLAN.md") + "\n"
	node := candidateGit(t, origin, "cat-file", "blob", result.OID+":plan/"+n.ID+".md") + "\n"
	if op["plan_sha256"] != learnSHA([]byte(plan)) || op["node_sha256"] != learnSHA([]byte(node)) || !strings.Contains(plan, spineNodeLine(result.Node)) {
		t.Fatal("accepted outbox/PLAN/node mismatch")
	}
	for _, changed := range []bool{false, true} {
		digest := learnSHA(receipt)
		if changed {
			digest = strings.Repeat("0", 64)
		}
		out, code := captureStdout(t, func() int {
			return cmdPlanNodeClose([]string{n.ID, "-product", root, "-request-id", "close-119", "-receipt", "close-receipt.json", "-receipt-sha256", digest, "-json"})
		})
		if code == 0 || !strings.Contains(out, `"action":"conflict"`) {
			t.Fatalf("replay accepted: %d %s", code, out)
		}
		if candidateGit(t, origin, "rev-parse", publishRef) != result.OID || !reflect.DeepEqual(before, candidateOriginal(t, root)) {
			t.Fatal("replay mutated state")
		}
	}
}

func TestPlanCanonicalCloseRejects(t *testing.T) {
	for _, mode := range []string{"missing-request", "unsafe-request", "missing-receipt", "missing-digest", "wrong-digest", "malformed", "duplicate-key", "trailing", "unknown", "case-key", "null", "wrong-node", "wrong-request", "wrong-node-digest", "failed-receipt", "stale", "dirty", "staged-wip", "duplicate-node", "duplicate-spine", "outbox-collision", "argv"} {
		t.Run(mode, func(t *testing.T) {
			root, origin, old, n, receipt := canonicalCloseFixture(t)
			args := []string{n.ID, "-product", root, "-request-id", "close-119", "-receipt", "close-receipt.json", "-receipt-sha256", learnSHA(receipt), "-json"}
			write := func(name string, raw []byte) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, name), raw, 0600); err != nil {
					t.Fatal(err)
				}
			}
			commit := false
			switch mode {
			case "missing-request":
				args = append(args, "-request-id", "")
			case "unsafe-request":
				args = append(args, "-request-id", "../escape")
			case "missing-receipt":
				args = append(args, "-receipt", "absent.json")
			case "missing-digest":
				args = append(args, "-receipt-sha256", "")
			case "wrong-digest":
				args = append(args, "-receipt-sha256", strings.Repeat("0", 64))
			case "malformed":
				receipt = []byte("{")
				commit = true
			case "duplicate-key":
				receipt = append([]byte(`{"request_id":"close-119",`), receipt[1:]...)
				commit = true
			case "trailing":
				receipt = append(receipt, []byte(` {}`)...)
				commit = true
			case "unknown":
				receipt = append([]byte(`{"unknown":"x",`), receipt[1:]...)
				commit = true
			case "case-key":
				receipt = []byte(strings.Replace(string(receipt), "request_id", "Request_ID", 1))
				commit = true
			case "null":
				receipt = []byte(strings.Replace(string(receipt), `"passed"`, `null`, 1))
				commit = true
			case "wrong-node", "wrong-request", "wrong-node-digest", "failed-receipt":
				var fields map[string]string
				if err := json.Unmarshal(receipt, &fields); err != nil {
					t.Fatal(err)
				}
				key := map[string]string{"wrong-node": "node_id", "wrong-request": "request_id", "wrong-node-digest": "node_sha256", "failed-receipt": "result"}[mode]
				fields[key] = "wrong"
				var err error
				receipt, err = json.Marshal(fields)
				if err != nil {
					t.Fatal(err)
				}
				commit = true
			case "stale":
				candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "local advanced")
			case "dirty":
				write("foreign.txt", []byte("foreign WIP\n"))
			case "staged-wip":
				write("foreign.txt", []byte("foreign staged WIP\n"))
				candidateGit(t, root, "add", "foreign.txt")
			case "duplicate-node":
				n124SeedNode(t, root, "N-119_other", "verified")
				commit = true
			case "duplicate-spine":
				write("PLAN.md", []byte(spineNodeLine(n)+"\n"+spineNodeLine(n)+"\n"))
				commit = true
			case "outbox-collision":
				if err := os.MkdirAll(filepath.Join(root, "plan", "event-outbox"), 0700); err != nil {
					t.Fatal(err)
				}
				write("plan/event-outbox/close-119.json", []byte(`{"foreign":"operation"}`))
				commit = true
			case "argv":
				args = append(args, "extra")
			}
			if commit {
				write("close-receipt.json", receipt)
				args = append(args, "-receipt-sha256", learnSHA(receipt))
				candidateGit(t, root, "-c", "core.autocrlf=false", "add", ".")
				candidateGit(t, root, "-c", "commit.gpgsign=false", "commit", "-m", "invalid request fixture")
				old = candidateGit(t, root, "rev-parse", "HEAD")
				// Copy only missing immutable loose objects; no transport is needed to seed the fixture.
				objects := filepath.Join(root, ".git", "objects")
				if err := filepath.WalkDir(objects, func(name string, entry fs.DirEntry, err error) error {
					if err != nil {
						return err
					}
					rel, err := filepath.Rel(objects, name)
					if err != nil {
						return err
					}
					dest := filepath.Join(origin, "objects", rel)
					if entry.IsDir() {
						return os.MkdirAll(dest, 0700)
					}
					if _, err := os.Stat(dest); err == nil {
						return nil
					} else if !os.IsNotExist(err) {
						return err
					}
					raw, err := os.ReadFile(name)
					if err != nil {
						return err
					}
					return os.WriteFile(dest, raw, 0600)
				}); err != nil {
					t.Fatal(err)
				}
				candidateGit(t, origin, "update-ref", publishRef, old)
			}
			before, remoteBefore := candidateOriginal(t, root), n124Snapshot(t, origin)
			out, code := captureStdout(t, func() int { return cmdPlanNodeClose(args) })
			if code == 0 || !strings.Contains(out, `"action":"conflict"`) {
				t.Fatalf("accepted %s: exit=%d %s", mode, code, out)
			}
			if !reflect.DeepEqual(before, candidateOriginal(t, root)) || !reflect.DeepEqual(remoteBefore, n124Snapshot(t, origin)) || candidateGit(t, origin, "rev-parse", publishRef) != old {
				t.Fatal("rejection mutated local or canonical state")
			}
		})
	}
}
