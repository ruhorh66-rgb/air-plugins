package main

// Source-review vault binding is deliberately separate from same-repository
// ancestry. It accepts only the product's tracked vault declaration, an exact
// clean vault revision/ref, and a task whose bytes match the in-product copy.
import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var sourceReviewGitHubRepo = regexp.MustCompile(`(?i)^https://github\.com/[a-z0-9_.-]+/[a-z0-9_.-]+(?:\.git)?$`)

type sourceReviewVaultAuthority struct {
	Schema           string `json:"schema"`
	Product          string `json:"product"`
	Authority        string `json:"authority"`
	CodeRepo         string `json:"code_repo"`
	PlanAuthority    string `json:"plan_authority"`
	WorkerEntrypoint string `json:"worker_entrypoint"`
}
type sourceReviewProductIdentity struct {
	Name       string `json:"name"`
	Repository string `json:"repository"`
}

func sourceReviewTrackedFile(root, name string, max int) (string, []byte, error) {
	if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	p, err := taskFileInsideProduct(root, name)
	if err != nil || jobOwnedEvidencePath(root, p) != nil {
		return "", nil, errors.New("vault evidence path is not an owned regular file")
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return "", nil, err
	}
	if err = exec.Command("git", "-C", root, "ls-files", "--error-unmatch", "--", filepath.ToSlash(rel)).Run(); err != nil {
		return "", nil, errors.New("vault evidence is not tracked")
	}
	raw, err := os.ReadFile(p)
	if err != nil || len(raw) == 0 || len(raw) > max {
		return "", nil, errors.New("vault evidence is unreadable or exceeds limit")
	}
	return p, raw, nil
}

func sourceReviewRepoIdentity(root string) (string, error) {
	value, err := gitCodingOutput(root, "remote", "get-url", "origin")
	value = strings.TrimSpace(value)
	if err != nil || !sourceReviewGitHubRepo.MatchString(value) {
		return "", errors.New("source-review requires an explicit canonical HTTPS GitHub origin")
	}
	return strings.TrimSuffix(strings.ToLower(value), ".git"), nil
}

func bindSourceReviewVaultNode(sourceRoot, sourceHead, vaultRoot, vaultHead, vaultRef, expectedVaultRepo, nodeID, vaultTask, taskSHA string) (sourceReviewNodeBinding, error) {
	var b sourceReviewNodeBinding
	fail := func(s string) (sourceReviewNodeBinding, error) { return b, errors.New(s) }
	if !operationalSHA.MatchString(sourceHead) || !operationalSHA.MatchString(vaultHead) || !runtimeHash.MatchString(taskSHA) {
		return fail("exact source, vault and task hashes required")
	}
	for _, root := range []*string{&sourceRoot, &vaultRoot} {
		abs, err := filepath.Abs(*root)
		if err != nil {
			return b, err
		}
		real, err := filepath.EvalSymlinks(abs)
		if err != nil || !sameReviewPath(abs, real) {
			return fail("review root is not canonical")
		}
		*root = abs
	}
	if got, err := sourceReviewGitState(sourceRoot); err != nil || got != sourceHead {
		return fail("source checkout is not clean at source-head")
	}
	if got, err := sourceReviewGitState(vaultRoot); err != nil || got != vaultHead {
		return fail("vault checkout is not clean at vault-head")
	}
	srcTop, err := gitCodingOutput(sourceRoot, "rev-parse", "--show-toplevel")
	if err != nil || !sameReviewPath(sourceRoot, strings.TrimSpace(srcTop)) {
		return fail("source product must be the repository root")
	}
	if !strings.HasPrefix(vaultRef, "refs/heads/") && !strings.HasPrefix(vaultRef, "refs/tags/") {
		return fail("vault ref must be an explicit local branch or tag")
	}
	if exec.Command("git", "check-ref-format", vaultRef).Run() != nil {
		return fail("invalid vault ref")
	}
	refHead, err := gitCodingOutput(vaultRoot, "rev-parse", "--verify", vaultRef+"^{commit}")
	if err != nil || strings.TrimSpace(refHead) != vaultHead {
		return fail("vault ref does not resolve to vault-head")
	}
	authorityPath, authorityBytes, err := sourceReviewTrackedFile(vaultRoot, filepath.Join(vaultRoot, "product-vault.json"), 1<<20)
	if err != nil {
		return b, err
	}
	_ = authorityPath
	if err := rejectDuplicateJSONKeys(authorityBytes); err != nil {
		return fail("duplicate vault authority keys")
	}
	var a sourceReviewVaultAuthority
	if json.Unmarshal(authorityBytes, &a) != nil || a.Schema != "air.product-vault/v1" || a.Authority != "product_vault" || a.PlanAuthority != "PLAN.md" || a.Product == "" || a.CodeRepo == "" || a.WorkerEntrypoint == "" {
		return fail("invalid vault authority")
	}
	if !filepath.IsAbs(a.CodeRepo) || !filepath.IsAbs(a.WorkerEntrypoint) || filepath.Base(a.WorkerEntrypoint) != "PLAN.md" {
		return fail("vault owner paths must be absolute and name PLAN.md")
	}
	ownerRoot, err := filepath.Abs(a.CodeRepo)
	if err != nil {
		return b, err
	}
	ownerReal, err := filepath.EvalSymlinks(ownerRoot)
	if err != nil || !sameReviewPath(ownerRoot, ownerReal) {
		return fail("owner code root is not canonical")
	}
	ownerTop, err := gitCodingOutput(ownerRoot, "rev-parse", "--show-toplevel")
	if err != nil || !sameReviewPath(ownerRoot, strings.TrimSpace(ownerTop)) {
		return fail("vault code_repo is not the repository root")
	}
	ownerRepo, err := sourceReviewRepoIdentity(ownerRoot)
	if err != nil {
		return b, err
	}
	sourceRepo, err := sourceReviewRepoIdentity(sourceRoot)
	if err != nil || sourceRepo != ownerRepo {
		return fail("source repository differs from vault code owner")
	}
	vaultRepo, err := sourceReviewRepoIdentity(vaultRoot)
	if err != nil {
		return b, err
	}
	if !sourceReviewGitHubRepo.MatchString(expectedVaultRepo) || strings.TrimSuffix(strings.ToLower(expectedVaultRepo), ".git") != vaultRepo {
		return fail("vault repository differs from explicitly pinned authority")
	}
	if exec.Command("git", "-C", ownerRoot, "cat-file", "-e", sourceHead+"^{commit}").Run() != nil {
		return fail("source commit is not present in declared owner repository")
	}
	if exec.Command("git", "-C", ownerRoot, "merge-base", "--is-ancestor", sourceHead, "HEAD").Run() != nil {
		return fail("source commit is not in declared owner history")
	}
	entryRoot := filepath.Dir(filepath.FromSlash(a.WorkerEntrypoint))
	entryRepo, err := sourceReviewRepoIdentity(entryRoot)
	if err != nil || entryRepo != vaultRepo {
		return fail("vault checkout differs from declared plan authority")
	}
	_, sourceProduct, err := sourceReviewTrackedFile(sourceRoot, filepath.Join(sourceRoot, "product.json"), 1<<20)
	if err != nil {
		return b, err
	}
	_, vaultProduct, err := sourceReviewTrackedFile(vaultRoot, filepath.Join(vaultRoot, "product.json"), 1<<20)
	if err != nil {
		return b, err
	}
	var sp, vp sourceReviewProductIdentity
	if rejectDuplicateJSONKeys(sourceProduct) != nil || rejectDuplicateJSONKeys(vaultProduct) != nil || json.Unmarshal(sourceProduct, &sp) != nil || json.Unmarshal(vaultProduct, &vp) != nil || !strings.EqualFold(sp.Name, a.Product) || !strings.EqualFold(vp.Name, a.Product) || !sourceReviewGitHubRepo.MatchString(sp.Repository) || !sourceReviewGitHubRepo.MatchString(vp.Repository) || strings.TrimSuffix(strings.ToLower(sp.Repository), ".git") != ownerRepo || strings.TrimSuffix(strings.ToLower(vp.Repository), ".git") != ownerRepo {
		return fail("product identity does not match vault owner")
	}
	nodePath, err := planNodePathByID(vaultRoot, nodeID)
	if err != nil {
		return b, err
	}
	_, nodeBytes, err := sourceReviewTrackedFile(vaultRoot, nodePath, 1<<20)
	if err != nil {
		return b, err
	}
	node, err := readPlanNode(nodePath)
	if err != nil || node.Status != "open" {
		return fail("vault node is not open")
	}
	taskPath, taskBytes, err := sourceReviewTrackedFile(vaultRoot, vaultTask, 48<<10)
	if err != nil || sha256Hex(taskBytes) != taskSHA {
		return fail("tracked vault task does not match task hash")
	}
	again, err := os.ReadFile(nodePath)
	if err != nil || !bytes.Equal(nodeBytes, again) {
		return fail("vault node changed during binding")
	}
	return sourceReviewNodeBinding{Root: vaultRoot, Head: vaultHead, Path: nodePath, SHA256: sha256Hex(nodeBytes), Node: node, VaultRef: vaultRef, VaultRepo: vaultRepo, SourceRepo: sourceRepo, AuthoritySHA256: sha256Hex(authorityBytes), VaultTaskPath: taskPath, VaultTaskSHA256: taskSHA}, nil
}
