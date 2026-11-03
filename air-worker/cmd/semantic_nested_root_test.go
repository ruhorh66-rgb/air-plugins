package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The actual air-plugins checkout has a repository root above air-worker.
// Git's <commit>:<path> syntax uses repository-relative, not cwd-relative
// paths; an incorrect lookup prevents a real node source-review before model
// invocation even when the frozen PLAN, N-ID and artifact are all valid.
func TestNativeSemanticPinnedSourceFromNestedGitProduct(t *testing.T) {
	repository := t.TempDir()
	product := filepath.Join(repository, "air-worker")
	if err := os.MkdirAll(filepath.Join(product, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	gitFixtureRun(t, repository, "init", "-b", "source")
	node := []byte("# Exact nested product node\n")
	artifact := []byte("package sample\nfunc SignedProcedure() bool { return true }\n")
	nodePath := filepath.Join(product, "plan", "N-015_nested-proof.md")
	artifactPath := filepath.Join(product, "source.go")
	for path, content := range map[string][]byte{nodePath: node, artifactPath: artifact} {
		if err := os.WriteFile(path, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	gitFixtureRun(t, repository, "add", "air-worker/plan", "air-worker/source.go")
	gitFixtureRun(t, repository, "commit", "-m", "freeze nested product files")
	commit := gitFixtureRun(t, repository, "rev-parse", "HEAD")

	gotNode, err := readGitPinnedSource(product, commit, "plan/N-015_nested-proof.md")
	if err != nil || !bytes.Equal(gotNode, node) {
		t.Fatalf("nested native node cannot be read from frozen Git commit: %v", err)
	}
	gotArtifact, err := readGitPinnedSource(product, commit, "source.go")
	if err != nil || !bytes.Equal(gotArtifact, artifact) {
		t.Fatalf("nested tracked source cannot be read from frozen Git commit: %v", err)
	}
	if _, err := readGitPinnedSource(product, commit, "../foreign.go"); err == nil {
		t.Fatal("nested source reviewer accepted path outside product root")
	}
	if _, err := readGitPinnedSource(product, commit, "missing.go"); err == nil {
		t.Fatal("nested source reviewer accepted untracked source")
	}
}

// Existing standalone product repositories remain valid: the Git prefix
// should be empty at the product root, not the literal '.' file component.
func TestNativeSemanticPinnedSourceFromRepositoryRoot(t *testing.T) {
	root, commit, _ := makeNativeSemanticFixture(t)
	got, err := readGitPinnedSource(root, commit, "source.go")
	want := []byte("package fixture\n// Source proof line\nfunc SourceProof() bool { return true }\n")
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("source-only reviewer broke standalone product repository: %v", err)
	}
}
