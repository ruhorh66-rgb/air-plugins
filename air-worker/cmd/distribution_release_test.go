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

// The native publisher must bind a GitHub RELEASE tag to the clean binary
// distribution commit B, which contains CLI/tray built at source SHA S.
// This is a source contract, not an external GitHub publication test.
func TestN130PublisherRequiresTagCommitBeforeSigning(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "tools", "publish-github-update.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	must := []string{
		"if ([string]$release.tagName -ne $tag)",
		"repos/$GitHubRepo/git/ref/tags/$TagName",
		"repos/$GitHubRepo/git/tags/$sha",
		"if ([string]$ref.ref -cne \"refs/tags/$TagName\")",
		"if ($kind -ceq 'commit')",
		"$tagCommitSha -ne $packageCommit",
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
	if strings.Contains(s, "repos/$GitHubRepo/commits/$tag") {
		t.Fatal("ambiguous generic commits/<ref> can resolve a branch instead of the release tag")
	}
	tagCheck := strings.Index(s, "$tagCommitSha -ne $packageCommit")
	signCall := strings.Index(s, "sign-update-manifest.py")
	secondCheck := strings.Index(s, "if ((Resolve-ReleaseTagCommit $tag) -ne $packageCommit)")
	feedWrite := strings.Index(s, "$feedPath = Publish-Manifest $manifestPath")
	if tagCheck < 0 || signCall < 0 || secondCheck < 0 || feedWrite < 0 ||
		tagCheck >= signCall || secondCheck <= signCall || secondCheck >= feedWrite {
		t.Fatal("explicit release tag identity must be checked before signing and rechecked immediately before feed publication")
	}
}

// Historical accepted S->B->R release: embedded CLI/tray VCS revision identifies
// source S, while GitHub release tag points at the independently committed
// installable binary package B. A source-root payload containing 0.11.9 EXE
// must never be signed as a newer release's host-plugin cache payload.
func TestN130PublisherSeparatesSourceAndInstallableDistributionCommit(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "tools", "publish-github-update.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, required := range []string{
		"$packageCommit",
		"$sourceRevision",
		"air-worker/cmd",
		"bin\\air-worker.exe",
		"bin\\air-worker-tray.exe",
		"bundled CLI SHA-256",
		"bundled tray SHA-256",
		"$tagCommitSha -ne $packageCommit",
		"(Resolve-ReleaseTagCommit $tag) -ne $packageCommit",
		"sign-update-manifest.py",
	} {
		if !strings.Contains(s, required) {
			t.Errorf("publisher missing accepted source→binary package release contract %q", required)
		}
	}
	check := strings.Index(s, "bundled CLI SHA-256")
	sign := strings.Index(s, "sign-update-manifest.py")
	if check < 0 || sign < 0 || check >= sign {
		t.Fatal("publisher might sign source tree before validating installable cached binary identity")
	}
	if strings.Contains(s, "$tagCommitSha -ne $sourceCommit") {
		t.Fatal("GitHub release tag must point to the package commit B, not the compiled source revision S")
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
