package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSourceReviewVaultBinding(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	vault := filepath.Join(root, "vault")
	for _, dir := range []string{source, vault} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		codingFixtureGit(t, dir, "init")
		codingFixtureGit(t, dir, "config", "--local", "user.name", "Test")
		codingFixtureGit(t, dir, "config", "--local", "user.email", "test@example.invalid")
	}
	const sourceRepo = "https://github.com/example/product.git"
	const vaultRepo = "https://github.com/example/product-wiki.git"
	codingFixtureGit(t, source, "remote", "add", "origin", sourceRepo)
	codingFixtureGit(t, vault, "remote", "add", "origin", vaultRepo)
	product, _ := json.Marshal(map[string]string{"name": "sample", "repository": sourceRepo})
	write := func(root, name string, b []byte) {
		t.Helper()
		p := filepath.Join(root, name)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	write(source, "product.json", product)
	write(source, "code.go", []byte("package sample\n"))
	codingFixtureGit(t, source, "add", ".")
	codingFixtureGit(t, source, "commit", "-m", "source")
	sourceHead, e := gitCodingOutput(source, "rev-parse", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	a := map[string]string{"schema": "air.product-vault/v1", "product": "sample", "authority": "product_vault", "code_repo": source, "plan_authority": "PLAN.md", "worker_entrypoint": filepath.Join(vault, "PLAN.md")}
	authority, _ := json.Marshal(a)
	write(vault, "product-vault.json", authority)
	write(vault, "product.json", product)
	write(vault, "PLAN.md", []byte("# PLAN\n"))
	node := planNode{ID: "N-215_review", Title: "Review", Parent: "N-214", Trigger: "review", Owner: "owner", DoneWhen: "review", Status: "open", ReturnTo: "PLAN.md", CreatedAt: "2026-11-05T00:00:00Z", UpdatedAt: "2026-11-05T00:00:00Z"}
	nodePath := filepath.Join(vault, "plan", node.ID+".md")
	write(vault, filepath.Join("plan", node.ID+".md"), renderPlanNode(node))
	task := []byte("Review source safely.\n")
	taskPath := filepath.Join(vault, "production", "task.md")
	write(vault, filepath.Join("production", "task.md"), task)
	codingFixtureGit(t, vault, "add", ".")
	codingFixtureGit(t, vault, "commit", "-m", "authority and task")
	vaultHead, e := gitCodingOutput(vault, "rev-parse", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	branch, e := gitCodingOutput(vault, "branch", "--show-current")
	if e != nil {
		t.Fatal(e)
	}
	ref := "refs/heads/" + strings.TrimSpace(branch)
	bind := func(s, sh, v, vh, r, repo, n, tp, h string) (sourceReviewNodeBinding, error) {
		return bindSourceReviewVaultNode(s, sh, v, vh, r, repo, n, tp, h)
	}
	good, e := bind(source, sourceHead, vault, vaultHead, ref, vaultRepo, "N-215", taskPath, sha256Hex(task))
	if e != nil || good.Node.ID != node.ID || good.AuthoritySHA256 != sha256Hex(authority) || good.VaultTaskSHA256 != sha256Hex(task) {
		t.Fatalf("binding = %+v, %v", good, e)
	}
	// A clean independently cloned Wiki must work even if the declared owner's
	// checkout has local edits. Do not modify or reset that owner's checkout.
	vaultCopy := filepath.Join(root, "vault-copy")
	codingFixtureGit(t, root, "-c", "core.autocrlf=false", "clone", vault, vaultCopy)
	codingFixtureGit(t, vaultCopy, "remote", "set-url", "origin", vaultRepo)
	write(vault, "PLAN.md", []byte("owner work in progress\n"))
	copyTask := filepath.Join(vaultCopy, "production", "task.md")
	if _, e := bind(source, sourceHead, vaultCopy, vaultHead, ref, vaultRepo, "N-215", copyTask, sha256Hex(task)); e != nil {
		t.Fatalf("independent clean vault binding: %v", e)
	}
	write(vault, "PLAN.md", []byte("# PLAN\n"))
	cases := []struct{ name, sh, vh, ref, repo, node, task, hash string }{
		{"wrong source", strings.Repeat("1", 40), vaultHead, ref, vaultRepo, "N-215", taskPath, sha256Hex(task)},
		{"wrong wiki", sourceHead, strings.Repeat("2", 40), ref, vaultRepo, "N-215", taskPath, sha256Hex(task)},
		{"wrong ref", sourceHead, vaultHead, "refs/heads/missing", vaultRepo, "N-215", taskPath, sha256Hex(task)},
		{"wrong authority", sourceHead, vaultHead, ref, "https://github.com/other/vault.git", "N-215", taskPath, sha256Hex(task)},
		{"wrong node", sourceHead, vaultHead, ref, vaultRepo, "N-999", taskPath, sha256Hex(task)},
		{"wrong task hash", sourceHead, vaultHead, ref, vaultRepo, "N-215", taskPath, strings.Repeat("0", 64)},
		{"task outside wiki", sourceHead, vaultHead, ref, vaultRepo, "N-215", filepath.Join(source, "product.json"), sha256Hex(task)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, e := bind(source, c.sh, vault, c.vh, c.ref, c.repo, c.node, c.task, c.hash); e == nil {
				t.Fatal("untrusted binding accepted")
			}
		})
	}
	write(vault, filepath.Join("production", "task.md"), []byte("tampered"))
	if _, e := bind(source, sourceHead, vault, vaultHead, ref, vaultRepo, "N-215", taskPath, sha256Hex(task)); e == nil {
		t.Fatal("dirty task accepted")
	}
	write(vault, filepath.Join("production", "task.md"), task)
	node.Status = "closed"
	if e := os.WriteFile(nodePath, renderPlanNode(node), 0600); e != nil {
		t.Fatal(e)
	}
	codingFixtureGit(t, vault, "add", "plan")
	codingFixtureGit(t, vault, "commit", "-m", "closed")
	closed, e := gitCodingOutput(vault, "rev-parse", "HEAD")
	if e != nil {
		t.Fatal(e)
	}
	if _, e := bind(source, sourceHead, vault, closed, ref, vaultRepo, "N-215", taskPath, sha256Hex(task)); e == nil {
		t.Fatal("closed node accepted")
	}
}
