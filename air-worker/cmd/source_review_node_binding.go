package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func sameReviewPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

type sourceReviewNodeBinding struct {
	Root   string
	Head   string
	Path   string
	SHA256 string
	Node   planNode
}

// bindSourceReviewNode binds a later plan node to the same product history as
// the exact source checkout, without changing the source under review.
func bindSourceReviewNode(sourceRoot, sourceHead, nodeRoot, nodeHead, nodeID string) (sourceReviewNodeBinding, error) {
	var b sourceReviewNodeBinding
	fail := func(s string) (sourceReviewNodeBinding, error) { return b, errors.New(s) }
	if !operationalSHA.MatchString(sourceHead) || !operationalSHA.MatchString(nodeHead) {
		return fail("source and node heads must be exact commit SHA values")
	}
	if sourceRoot == "" || nodeRoot == "" {
		return fail("source and node roots are required")
	}
	sourceAbsolute, err := filepath.Abs(sourceRoot)
	if err != nil {
		return b, err
	}
	sourceResolved, err := filepath.EvalSymlinks(sourceAbsolute)
	if err != nil || !sameReviewPath(sourceResolved, sourceAbsolute) {
		return fail("source product root is not canonical")
	}
	sourceRoot = sourceAbsolute
	sourceActual, err := sourceReviewGitState(sourceRoot)
	if err != nil || sourceActual != sourceHead {
		return fail("source-head must equal the exact clean source checkout SHA")
	}
	absolute, err := filepath.Abs(nodeRoot)
	if err != nil {
		return b, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil || !sameReviewPath(resolved, absolute) {
		return fail("node product root is not canonical")
	}
	nodeRoot = absolute
	got, err := sourceReviewGitState(nodeRoot)
	if err != nil || got != nodeHead {
		return fail("node-head must equal the exact clean node checkout SHA")
	}
	srcTop, err := gitCodingOutput(sourceRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return b, err
	}
	nodeTop, err := gitCodingOutput(nodeRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return b, err
	}
	srcRel, err := filepath.Rel(srcTop, sourceRoot)
	if err != nil || (srcRel != "." && !isCodingProductPath(srcRel)) {
		return fail("source product is outside repository")
	}
	nodeRel, err := filepath.Rel(nodeTop, nodeRoot)
	if err != nil || (nodeRel != "." && !isCodingProductPath(nodeRel)) || !sameReviewPath(srcRel, nodeRel) {
		return fail("node root is not the same product path")
	}
	if err := exec.Command("git", "-C", nodeRoot, "merge-base", "--is-ancestor", sourceHead, nodeHead).Run(); err != nil {
		return fail("source SHA is not an ancestor of node SHA")
	}
	path, err := planNodePathByID(nodeRoot, nodeID)
	if err != nil {
		return b, err
	}
	if jobOwnedEvidencePath(nodeRoot, path) != nil {
		return fail("node file is not an owned regular file")
	}
	rel, err := filepath.Rel(nodeRoot, path)
	if err != nil {
		return b, err
	}
	if err := exec.Command("git", "-C", nodeRoot, "ls-files", "--error-unmatch", "--", filepath.ToSlash(rel)).Run(); err != nil {
		return fail("node file is not tracked at node SHA")
	}
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 || len(raw) > 1<<20 {
		return fail("node file is unreadable or too large")
	}
	node, err := readPlanNode(path)
	if err != nil {
		return b, err
	}
	if node.Status != "open" {
		return fail(fmt.Sprintf("native node %s status=%s is not open", node.ID, node.Status))
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(raw, after) || jobOwnedEvidencePath(nodeRoot, path) != nil {
		return fail("node file changed while being read")
	}
	return sourceReviewNodeBinding{Root: nodeRoot, Head: nodeHead, Path: path, SHA256: sha256Hex(raw), Node: node}, nil
}
