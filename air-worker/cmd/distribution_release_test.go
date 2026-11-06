package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitFixtureRun(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Aworker", "GIT_AUTHOR_EMAIL=aworker@test.local",
		"GIT_COMMITTER_NAME=Aworker", "GIT_COMMITTER_EMAIL=aworker@test.local")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func distributionGuardPath(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	path, err := filepath.Abs(filepath.Join(wd, "..", "tools", "check-distribution-identity.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func runDistributionGuard(t *testing.T, repo, version, commit string) (int, string) {
	t.Helper()
	cmd := newPowerShellCommand("-NoProfile", "-ExecutionPolicy", "Bypass", "-File",
		distributionGuardPath(t), "-Repo", repo, "-Version", version, "-Commit", commit)
	out, err := cmd.CombinedOutput()
	return exitCode(cmd, err), decodeOutput(out)
}

// The native publisher must bind a GitHub RELEASE tag to the same clean Git
// source revision as the VCS-stamped CLI and tray BEFORE signing any feed.
// This is a source contract, not an external GitHub publication test.
func TestN130PublisherRequiresTagCommitBeforeSigning(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "tools", "publish-github-update.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	must := []string{
		"if ([string]$release.tagName -ne $tag)",
		"gh api \"repos/$GitHubRepo/commits/$tag\" --jq '.sha'",
		"$tagCommitSha -ne $sourceCommit",
		"release tag $tag resolves to $tagCommitSha",
		"vcs\\.revision",
		"vcs\\.modified=false",
		"sign-update-manifest.py",
	}
	for _, phrase := range must {
		if !strings.Contains(s, phrase) {
			t.Fatalf("release provenance contract missing %q", phrase)
		}
	}
	tagCheck := strings.Index(s, "$tagCommitSha -ne $sourceCommit")
	signCall := strings.Index(s, "sign-update-manifest.py")
	if tagCheck < 0 || signCall < 0 || tagCheck >= signCall {
		t.Fatal("GitHub release tag must be resolved and compared before manifest signing")
	}
}

func TestDistributionReleaseGuardRejectsDirtyAndReusedVersion(t *testing.T) {
	repo := t.TempDir()
	gitFixtureRun(t, repo, "init")
	writeTree(t, filepath.Join(repo, "air-worker", "payload.txt"), "one")
	gitFixtureRun(t, repo, "add", "air-worker/payload.txt")
	gitFixtureRun(t, repo, "commit", "-m", "first")
	first := gitFixtureRun(t, repo, "rev-parse", "HEAD")

	if code, out := runDistributionGuard(t, repo, "0.10.13", first); code != 0 {
		t.Fatalf("unused clean identity rejected: code=%d\n%s", code, out)
	}
	gitFixtureRun(t, repo, "tag", "air-worker--v0.10.13", first)
	if code, out := runDistributionGuard(t, repo, "0.10.13", first); code != 0 {
		t.Fatalf("same tag/commit rejected: code=%d\n%s", code, out)
	}

	writeTree(t, filepath.Join(repo, "air-worker", "payload.txt"), "dirty")
	if code, out := runDistributionGuard(t, repo, "0.10.13", first); code != 1 || !strings.Contains(out, "payload is dirty") {
		t.Fatalf("dirty payload not rejected: code=%d\n%s", code, out)
	}
	gitFixtureRun(t, repo, "checkout", "--", "air-worker/payload.txt")

	writeTree(t, filepath.Join(repo, "air-worker", "payload.txt"), "two")
	gitFixtureRun(t, repo, "add", "air-worker/payload.txt")
	gitFixtureRun(t, repo, "commit", "-m", "second")
	second := gitFixtureRun(t, repo, "rev-parse", "HEAD")
	if second == first {
		t.Fatal("fixture did not create a second commit")
	}
	if code, out := runDistributionGuard(t, repo, "0.10.13", second); code != 1 || !strings.Contains(out, "already published") {
		t.Fatalf("reused tagged version not rejected: code=%d\n%s", code, out)
	}
}
