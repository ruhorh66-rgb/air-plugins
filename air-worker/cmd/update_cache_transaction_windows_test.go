//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func n130WriteFile(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}

func n130FakeCacheProfile(t *testing.T, host string) (distributionProfile, []string) {
	t.Helper()
	home := t.TempDir()
	old := filepath.Join(home, "plugins", "cache", "air-plugins", "air-worker", "0.11.11-rc.2", "bin", executableName())
	n130WriteFile(t, old, host+" original safe binary")
	hash, err := sha256File(old)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	if host == "claude" {
		paths = []string{
			filepath.Join(home, "plugins", "installed_plugins.json"),
			filepath.Join(home, "plugins", "known_marketplaces.json"),
		}
	} else {
		paths = []string{filepath.Join(home, "config.toml")}
	}
	for _, p := range paths {
		n130WriteFile(t, p, host+" original plugin metadata")
	}
	return distributionProfile{
		Host: host, ConfigDir: home, Canonical: true, PluginEnabled: true,
		InstallPath: filepath.Dir(filepath.Dir(old)), Version: "0.11.11-rc.2",
		CacheBinary: distributionBinaryIdentity{Readable: true, SHA256: hash},
	}, paths
}

func TestN130RollbackBothPluginCachesKeepsOwnOriginalVersion(t *testing.T) {
	claude, cm := n130FakeCacheProfile(t, "claude")
	codex, dm := n130FakeCacheProfile(t, "codex")
	root := t.TempDir()
	m := updateManifest{Version: "0.11.11-rc.4"}
	snap, err := snapshotManagedUpdateCaches(root, m, []distributionProfile{claude, codex})
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.profiles) != 2 || len(snap.profiles[0].files) != 2 || len(snap.profiles[1].files) != 1 {
		t.Fatalf("snapshot incomplete: %+v", snap)
	}
	for _, p := range snap.profiles {
		n130WriteFile(t, filepath.Join(p.targetDir, "bin", executableName()), "new candidate plugin")
		n130WriteFile(t, filepath.Join(p.targetDir, "skills", "woody", "SKILL.md"), "new candidate skill")
		n130WriteFile(t, filepath.Join(p.configDir, "plugins", "cache", "foreign-product", "keep.txt"), "foreign unchanged")
		for _, entry := range p.files {
			n130WriteFile(t, entry.live, "new vendor metadata")
		}
	}
	if err := snap.rollback(); err != nil {
		t.Fatal(err)
	}
	for _, p := range snap.profiles {
		if _, err := os.Stat(p.targetDir); !os.IsNotExist(err) {
			t.Fatalf("new cache not removed: %s %v", p.targetDir, err)
		}
		old := filepath.Join(p.configDir, "plugins", "cache", "air-plugins", "air-worker", "0.11.11-rc.2", "bin", executableName())
		sha, err := sha256File(old)
		if err != nil || sha != p.oldBinSHA {
			t.Fatalf("original binary drift: %s %s %v", p.host, sha, err)
		}
		raw, err := os.ReadFile(filepath.Join(p.configDir, "plugins", "cache", "foreign-product", "keep.txt"))
		if err != nil || string(raw) != "foreign unchanged" {
			t.Fatal("foreign cache modified")
		}
		for _, file := range p.files {
			sha, err := sha256File(file.live)
			if err != nil || !strings.EqualFold(sha, file.beforeSHA) {
				t.Fatalf("metadata not restored: %s err=%v", file.live, err)
			}
		}
	}
	_ = cm
	_ = dm
}

func TestN130CacheSnapshotRejectsAmbiguousOrForeignTarget(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "claude")
	m := updateManifest{Version: "0.11.11-rc.4"}
	target, err := managedCacheTarget(p.ConfigDir, m.Version)
	if err != nil {
		t.Fatal(err)
	}
	n130WriteFile(t, filepath.Join(target, "bin", executableName()), "already exists")
	if _, err := snapshotManagedUpdateCaches(t.TempDir(), m, []distributionProfile{p}); err == nil {
		t.Fatal("pre-existing version must block transaction before vendor mutation")
	}
	if _, err := managedCacheTarget(p.ConfigDir, "../foreign-product"); err == nil {
		t.Fatal("path traversal accepted")
	}
	p.Canonical = false
	if _, err := snapshotManagedUpdateCaches(t.TempDir(), m, []distributionProfile{p}); err == nil {
		t.Fatal("non-GitHub marketplace accepted")
	}
}

func TestN130RollbackRejectsLinkedCacheTarget(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "codex")
	snap, err := snapshotManagedUpdateCaches(t.TempDir(), updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	target := snap.profiles[0].targetDir
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("Windows symlink privilege missing: %v", err)
	}
	if err := snap.rollback(); err == nil {
		t.Fatal("linked candidate cache allowed to be deleted")
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("foreign symlink destination erased")
	}
}

func TestN130MustRestorePreviousManagedCacheAfterPartialVendorFailure(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "claude")
	old := filepath.Join(p.ConfigDir, "plugins", "cache", "air-plugins", "air-worker",
		"0.11.11-rc.2", "bin", executableName())
	original, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a vendor update that damages the previous managed version
	// before the second host fails. Only own t.TempDir contents are changed.
	if err := os.WriteFile(old, []byte("modified previous managed binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := snap.rollback(); err != nil {
		t.Fatalf("expected transactional rollback of previous owned cache after partial failure: %v", err)
	}
	restored, err := os.ReadFile(old)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Fatal("old managed cache binary bytes were not restored")
	}
}

func TestN130MustRejectParentJunctionBeforeCacheMutation(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "codex")
	cacheParent := filepath.Join(p.ConfigDir, "plugins", "cache", "air-plugins", "air-worker")
	physical := cacheParent + "-physical"
	if err := os.Rename(cacheParent, physical); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", cacheParent, physical).CombinedOutput()
	if err != nil {
		t.Fatalf("Windows unprivileged junction fixture required for release test: %v %s", err, out)
	}
	defer func() {
		if out, err := exec.Command("cmd.exe", "/d", "/c", "rmdir", cacheParent).CombinedOutput(); err != nil {
			t.Errorf("own fixture link cleanup failed: %v %s", err, out)
		}
	}()
	// The previous binary is still reachable, but a parent component now
	// redirects through a Windows reparse point. Must fail before vendor IO.
	_, err = snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err == nil {
		t.Fatal("parent junction was accepted for managed cache transaction")
	}
}
