package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func initFreshnessGitRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runFreshnessGit(t, root, "init")
	runFreshnessGit(t, root, "config", "user.email", "air-worker-test@example.invalid")
	runFreshnessGit(t, root, "config", "user.name", "AirWorker Test")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(".woody/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFreshnessGit(t, root, "add", "tracked.txt", ".gitignore")
	runFreshnessGit(t, root, "commit", "-m", "fixture")
	return root
}

func runFreshnessGit(t *testing.T, root string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestMachineVerdictFreshnessRejectsOlderThanSixHours(t *testing.T) {
	now := time.Now()
	mv := machineVerdict{At: now.Add(-machineVerdictMaxAge - time.Second).Format(time.RFC3339Nano)}
	err := machineVerdictFreshness(t.TempDir(), mv, now)
	if err == nil || !strings.Contains(err.Error(), "age") {
		t.Fatalf("verdict older than six hours must be stale: %v", err)
	}
}

func TestMachineVerdictFreshnessRequiresCurrentGitHead(t *testing.T) {
	root := initFreshnessGitRepo(t)
	now := time.Now()
	head := currentGitHead(root)
	if head == "" {
		t.Fatal("fixture has no git HEAD")
	}

	fresh := machineVerdict{At: now.Format(time.RFC3339Nano), GitHead: head, SourceClean: true}
	if err := machineVerdictFreshness(root, fresh, now); err != nil {
		t.Fatalf("current HEAD verdict rejected: %v", err)
	}

	unproven := fresh
	unproven.SourceClean = false
	if err := machineVerdictFreshness(root, unproven, now); err == nil || !strings.Contains(err.Error(), "dirty or unproven") {
		t.Fatalf("git verdict without explicit source_clean proof must be stale: %v", err)
	}

	missing := fresh
	missing.GitHead = ""
	if err := machineVerdictFreshness(root, missing, now); err == nil || !strings.Contains(err.Error(), "no git_head") {
		t.Fatalf("git verdict without head must be stale: %v", err)
	}

	wrong := fresh
	wrong.GitHead = strings.Repeat("a", 40)
	if err := machineVerdictFreshness(root, wrong, now); err == nil || !strings.Contains(err.Error(), "current HEAD") {
		t.Fatalf("mismatched git head must be stale: %v", err)
	}
}

func TestMachineVerdictFreshnessChecksGitWorktree(t *testing.T) {
	root := initFreshnessGitRepo(t)
	now := time.Now()
	fresh := machineVerdict{At: now.Format(time.RFC3339Nano), GitHead: currentGitHead(root), SourceClean: true}
	check := func(wantFresh bool) {
		t.Helper()
		err := machineVerdictFreshness(root, fresh, now)
		if wantFresh && err != nil || !wantFresh && (err == nil || !strings.Contains(err.Error(), "dirty or unproven")) {
			t.Fatalf("want fresh=%t, got %v", wantFresh, err)
		}
	}
	write := func(name, contents string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("tracked.txt", "dirty\n")
	check(false)
	runFreshnessGit(t, root, "add", "tracked.txt")
	check(false)
	write("tracked.txt", "v1\n")
	runFreshnessGit(t, root, "add", "tracked.txt")
	check(true)

	write("source.go", "package fixture\n")
	check(false)
	if err := os.Remove(filepath.Join(root, "source.go")); err != nil {
		t.Fatal(err)
	}
	check(true)

	write(".goal-verdict", "old\n")
	runFreshnessGit(t, root, "add", ".goal-verdict")
	runFreshnessGit(t, root, "commit", "-m", "track verdict")
	fresh.GitHead = currentGitHead(root)
	check(true)
	write(".goal-verdict", "changed\n")
	check(false)
	write(".goal-verdict", "old\n")
	check(true)

	write("tracked.txt", "v2\n")
	runFreshnessGit(t, root, "add", "tracked.txt")
	runFreshnessGit(t, root, "commit", "-m", "new HEAD")
	if err := machineVerdictFreshness(root, fresh, now); err == nil || !strings.Contains(err.Error(), "current HEAD") {
		t.Fatalf("old HEAD verdict must be stale after commit: %v", err)
	}
}

func TestMachineVerdictFreshnessNestedProduct(t *testing.T) {
	root := initFreshnessGitRepo(t)
	product := filepath.Join(root, "product")
	if err := os.Mkdir(product, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(product, "source.go"), []byte("package fixture\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runFreshnessGit(t, root, "add", "product/source.go")
	runFreshnessGit(t, root, "commit", "-m", "nested product")
	now := time.Now()
	fresh := machineVerdict{At: now.Format(time.RFC3339Nano), GitHead: currentGitHead(root), SourceClean: true}
	if err := machineVerdictFreshness(product, fresh, now); err != nil {
		t.Fatalf("clean nested product rejected: %v", err)
	}
	if err := os.WriteFile(filepath.Join(product, "source.go"), []byte("package changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := machineVerdictFreshness(product, fresh, now); err == nil || !strings.Contains(err.Error(), "dirty or unproven") {
		t.Fatalf("dirty nested product must be stale: %v", err)
	}
}

func TestMachineVerdictFreshnessAcceptsLegacyLocalTimestamp(t *testing.T) {
	now := time.Now()
	mv := machineVerdict{At: now.Add(-time.Hour).Format("2006-01-02T15:04:05")}
	if err := machineVerdictFreshness(t.TempDir(), mv, now); err != nil {
		t.Fatalf("legacy local timestamp within six hours must remain readable: %v", err)
	}
}

func TestJudgeSourceIdentityRejectsHiddenIndexFlags(t *testing.T) {
	for _, flags := range [][]string{{"assume-unchanged"}, {"skip-worktree"}, {"assume-unchanged", "skip-worktree"}} {
		t.Run(strings.Join(flags, "+"), func(t *testing.T) {
			root := initFreshnessGitRepo(t)
			name := "source with spaces.go"
			path := filepath.Join(root, name)
			write := func(contents string) {
				t.Helper()
				if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			check := func(stage string, wantClean bool) {
				t.Helper()
				_, clean, err := judgeSourceIdentity(root)
				if wantClean && (err != nil || !clean) || !wantClean && err == nil && clean {
					t.Errorf("%s: clean=%t, err=%v; want clean=%t", stage, clean, err, wantClean)
				}
			}
			write("package fixture\n")
			runFreshnessGit(t, root, "add", name)
			runFreshnessGit(t, root, "commit", "-m", "add source")
			check("normal clean", true)
			write("package changed\n")
			check("normal dirty", false)
			write("package fixture\n")
			check("restored", true)
			for _, flag := range flags {
				runFreshnessGit(t, root, "update-index", "--"+flag, "--", name)
			}
			check("flagged unmodified", false)
			write("package changed\n")
			check("flagged modified", false)
			for _, flag := range flags {
				runFreshnessGit(t, root, "update-index", "--no-"+flag, "--", name)
			}
			check("flag removed, still modified", false)
			write("package fixture\n")
			check("flag removed, restored", true)
		})
	}
}
