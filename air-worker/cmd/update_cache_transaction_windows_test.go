//go:build windows

package main

import (
	"errors"
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

func TestN130RollbackRejectsNestedJunctionWithoutDeletingOwnExternalFixture(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "codex")
	snap, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	target := snap.profiles[0].targetDir
	n130WriteFile(t, filepath.Join(target, "skills", "woody", "SKILL.md"), "new candidate own skill")
	external := t.TempDir()
	sentinel := filepath.Join(external, "DO_NOT_TOUCH.txt")
	n130WriteFile(t, sentinel, "own external fixture unchanged")
	junction := filepath.Join(target, "skills", "linked-tree")
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", junction, external).CombinedOutput()
	if err != nil {
		t.Fatalf("Windows unprivileged nested junction fixture is required: %v %s", err, out)
	}
	defer func() {
		if out, err := exec.Command("cmd.exe", "/d", "/c", "rmdir", junction).CombinedOutput(); err != nil {
			t.Errorf("nested junction cleanup failed: %v %s", err, out)
		}
	}()
	if err := snap.rollback(); err == nil {
		t.Fatal("nested Windows junction in new candidate cache was accepted")
	}
	raw, err := os.ReadFile(sentinel)
	if err != nil || string(raw) != "own external fixture unchanged" {
		t.Fatalf("nested junction affected external test fixture: %v %q", err, raw)
	}
}

func TestN130RollbackRejectsSwappedParentJunctionAfterSnapshot(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "codex")
	snap, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(snap.profiles[0].targetDir)
	physical := parent + "-physical"
	if err := os.Rename(parent, physical); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", parent, physical).CombinedOutput()
	if err != nil {
		t.Fatalf("Windows parent junction fixture failed: %v %s", err, out)
	}
	defer func() {
		if out, err := exec.Command("cmd.exe", "/d", "/c", "rmdir", parent).CombinedOutput(); err != nil {
			t.Errorf("parent junction cleanup failed: %v %s", err, out)
		}
	}()
	if err := snap.rollback(); err == nil {
		t.Fatal("swapped parent junction after snapshot was accepted on rollback")
	}
	old := filepath.Join(physical, "0.11.11-rc.2", "bin", executableName())
	if got, err := sha256File(old); err != nil || !strings.EqualFold(got, p.CacheBinary.SHA256) {
		t.Fatalf("original own previous cache was damaged by parent junction: %s %v", got, err)
	}
}

func TestN130RollbackQuarantinesNewVersionWithoutRecursiveDeletion(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "claude")
	snap, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	target := snap.profiles[0].targetDir
	const evidence = "own disposable candidate bytes preserved"
	n130WriteFile(t, filepath.Join(target, "bin", executableName()), evidence)
	if err := snap.rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("candidate still active after rollback: %v", err)
	}
	quarantines, err := filepath.Glob(filepath.Join(filepath.Dir(target), ".air-worker-rollback-*"))
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("expected exactly one preserved own quarantine: %v %#v", err, quarantines)
	}
	raw, err := os.ReadFile(filepath.Join(quarantines[0], "bin", executableName()))
	if err != nil || string(raw) != evidence {
		t.Fatalf("candidate quarantined bytes not preserved: %v %q", err, raw)
	}
}

func TestN130MustRestoreOldManagedCacheSkillContentAfterVendorFailure(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "codex")
	old := filepath.Join(p.ConfigDir, "plugins", "cache", "air-plugins", "air-worker", "0.11.11-rc.2")
	skill := filepath.Join(old, "skills", "woody", "SKILL.md")
	const before = "original owned cache skill v0.11.11-rc.2"
	n130WriteFile(t, skill, before)
	snap, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	n130WriteFile(t, skill, "vendor changed existing skill before failure")
	if err := snap.rollback(); err != nil {
		t.Fatalf("previous managed cache tree rollback failed: %v", err)
	}
	after, err := os.ReadFile(skill)
	if err != nil || string(after) != before {
		t.Fatalf("previous owned nonbinary managed cache was not restored: %v %q", err, after)
	}
}

func TestN130RollbackRecreatesRemovedPreviousVersionFromSnapshot(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "codex")
	oldDir := filepath.Join(p.ConfigDir, "plugins", "cache", "air-plugins", "air-worker", "0.11.11-rc.2")
	skill := filepath.Join(oldDir, "skills", "woody", "SKILL.md")
	const original = "original own managed version skill"
	n130WriteFile(t, skill, original)
	snap, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(oldDir, oldDir+"-fixture-previous"); err != nil {
		t.Fatal(err)
	}
	if err := snap.rollback(); err != nil {
		t.Fatalf("missing prior owned version could not be reconstructed from snapshot: %v", err)
	}
	if got, err := os.ReadFile(skill); err != nil || string(got) != original {
		t.Fatalf("previous own version skill not reconstructed: %v %q", err, got)
	}
	oldBinary := filepath.Join(oldDir, "bin", executableName())
	if got, err := sha256File(oldBinary); err != nil || !strings.EqualFold(got, p.CacheBinary.SHA256) {
		t.Fatalf("previous own version executable not reconstructed: %v %s", err, got)
	}
}

func TestN130RollbackPreservesButRejectsUnknownPreviousVersionAddition(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "claude")
	snap, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	unexpected := filepath.Join(p.ConfigDir, "plugins", "cache", "air-plugins",
		"air-worker", "0.11.11-rc.2", "foreign-entry.txt")
	n130WriteFile(t, unexpected, "own test fixture added after snapshot")
	if err := snap.rollback(); err == nil {
		t.Fatal("unknown prior-cache addition accepted as exact rollback PASS")
	}
	if raw, err := os.ReadFile(unexpected); err != nil ||
		string(raw) != "own test fixture added after snapshot" {
		t.Fatalf("unknown prior-cache file was deleted or changed: %v %q", err, raw)
	}
}

func TestN130OfflinePartialSecondHostFailureRestoresPairAndOwnCaches(t *testing.T) {
	home, stage, backup := t.TempDir(), t.TempDir(), t.TempDir()
	oldCLI := filepath.Join(home, "bin", "air-worker.exe")
	oldTray := filepath.Join(home, "bin", "air-worker-tray.exe")
	const originalCLI, originalTray = "original rc2 CLI bytes", "original rc2 tray bytes"
	n130WriteFile(t, oldCLI, originalCLI)
	n130WriteFile(t, oldTray, originalTray)
	if err := copyUpdateSnapshot(oldCLI, filepath.Join(backup, "air-worker.exe")); err != nil {
		t.Fatal(err)
	}
	if err := copyUpdateSnapshot(oldTray, filepath.Join(backup, "air-worker-tray.exe")); err != nil {
		t.Fatal(err)
	}
	newCLI := filepath.Join(stage, "air-worker.exe")
	newTray := filepath.Join(stage, "air-worker-tray.exe")
	const candidateCLI, candidateTray = "candidate rc4 CLI bytes", "candidate rc4 tray bytes"
	n130WriteFile(t, newCLI, candidateCLI)
	n130WriteFile(t, newTray, candidateTray)
	cliSHA, err := sha256File(newCLI)
	if err != nil {
		t.Fatal(err)
	}
	traySHA, err := sha256File(newTray)
	if err != nil {
		t.Fatal(err)
	}
	m := updateManifest{
		Version: "0.11.11-rc.4",
		CLI:     updateArtifact{SHA256: cliSHA, Size: int64(len(candidateCLI))},
		Tray:    updateArtifact{SHA256: traySHA, Size: int64(len(candidateTray))},
	}
	claude, _ := n130FakeCacheProfile(t, "claude")
	codex, _ := n130FakeCacheProfile(t, "codex")
	profiles := []distributionProfile{claude, codex}
	for _, p := range profiles {
		oldSkill := filepath.Join(p.ConfigDir, "plugins", "cache", "air-plugins", "air-worker",
			"0.11.11-rc.2", "skills", "woody", "SKILL.md")
		n130WriteFile(t, oldSkill, p.Host+" original own skill")
	}
	cacheSnapshot, err := snapshotManagedUpdateCaches(backup, m, profiles)
	if err != nil {
		t.Fatal(err)
	}
	// The callback is a local fault injection, not a vendor CLI invocation.
	// Simulate the first host partially changing caches before the second fails.
	applyErr := replaceUpdatePairTransactional(
		backup, home, newCLI, newTray, m,
		func(liveCLI string) error {
			for _, p := range cacheSnapshot.profiles {
				oldBin := filepath.Join(p.configDir, "plugins", "cache", "air-plugins", "air-worker",
					"0.11.11-rc.2", "bin", executableName())
				n130WriteFile(t, oldBin, p.host+" corrupted old binary")
				oldSkill := filepath.Join(p.configDir, "plugins", "cache", "air-plugins", "air-worker",
					"0.11.11-rc.2", "skills", "woody", "SKILL.md")
				n130WriteFile(t, oldSkill, p.host+" corrupted old skill")
				n130WriteFile(t, filepath.Join(p.targetDir, "bin", executableName()),
					p.host+" partially created new version")
				for _, f := range p.files {
					n130WriteFile(t, f.live, "vendor changed owned metadata")
				}
			}
			return errors.New("injected second vendor host failure")
		},
	)
	if applyErr == nil {
		t.Fatal("injected vendor failure incorrectly accepted")
	}
	if err := cacheSnapshot.rollback(); err != nil {
		t.Fatalf("native cache transaction could not roll back after second vendor failure: %v", err)
	}
	for _, check := range []struct{ path, want string }{
		{oldCLI, originalCLI}, {oldTray, originalTray},
	} {
		got, err := os.ReadFile(check.path)
		if err != nil || string(got) != check.want {
			t.Fatalf("installed pair was not restored after partial vendor failure: %s %v", check.path, err)
		}
	}
	for _, p := range cacheSnapshot.profiles {
		if _, err := os.Stat(p.targetDir); !os.IsNotExist(err) {
			t.Fatalf("failed candidate remained active: %s %v", p.targetDir, err)
		}
		old := filepath.Join(p.configDir, "plugins", "cache", "air-plugins", "air-worker",
			"0.11.11-rc.2", "bin", executableName())
		if sha, err := sha256File(old); err != nil || !strings.EqualFold(sha, p.oldBinSHA) {
			t.Fatalf("old %s binary SHA drift: %v %s", p.host, err, sha)
		}
		oldSkill := filepath.Join(p.configDir, "plugins", "cache", "air-plugins", "air-worker",
			"0.11.11-rc.2", "skills", "woody", "SKILL.md")
		got, err := os.ReadFile(oldSkill)
		if err != nil || string(got) != p.host+" original own skill" {
			t.Fatalf("old %s managed skill was not restored: %v %q", p.host, err, got)
		}
		for _, f := range p.files {
			sha, err := sha256File(f.live)
			if err != nil || !strings.EqualFold(sha, f.beforeSHA) {
				t.Fatalf("old %s vendor metadata was not restored: %s %v", p.host, f.live, err)
			}
		}
	}
}

func TestN130NativeUpdatePinsOwnedCacheParentAgainstRename(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "claude")
	snapshot, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	release, err := snapshot.holdCanonicalParentsDuringUpdate()
	if err != nil {
		t.Fatal(err)
	}
	parent := filepath.Dir(snapshot.profiles[0].targetDir)
	renamed := parent + "-fixture-renamed"
	if err := os.Rename(parent, renamed); err == nil {
		_ = os.Rename(renamed, parent)
		release()
		t.Fatal("Windows parent cache was movable while native update pins were held")
	}
	release()
	if err := os.Rename(parent, renamed); err != nil {
		t.Fatalf("cache hold was not released after transaction: %v", err)
	}
	if err := os.Rename(renamed, parent); err != nil {
		t.Fatal(err)
	}
}

func TestN130GoRootHandlePinsDirectoryAgainstWindowsRename(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "own-root-fixture")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	renamed := dir + "-renamed"
	err = os.Rename(dir, renamed)
	if err == nil {
		_ = os.Rename(renamed, dir)
		_ = root.Close()
		t.Fatal("Go os.Root did not pin the Windows directory against rename")
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dir, renamed); err != nil {
		t.Fatalf("Go os.Root pin remained after Close: %v", err)
	}
	if err := os.Rename(renamed, dir); err != nil {
		t.Fatal(err)
	}
}

func TestN130MetadataRestoreRefusesSwappedProfileJunction(t *testing.T) {
	p, _ := n130FakeCacheProfile(t, "claude")
	snapshot, err := snapshotManagedUpdateCaches(t.TempDir(),
		updateManifest{Version: "0.11.11-rc.4"}, []distributionProfile{p})
	if err != nil {
		t.Fatal(err)
	}
	plugins := filepath.Join(p.ConfigDir, "plugins")
	physical := plugins + "-original-fixture"
	if err := os.Rename(plugins, physical); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", plugins, physical).CombinedOutput()
	if err != nil {
		t.Fatalf("cannot create own Windows metadata parent fixture: %v %s", err, out)
	}
	defer func() {
		if out, err := exec.Command("cmd.exe", "/d", "/c", "rmdir", plugins).CombinedOutput(); err != nil {
			t.Errorf("cannot remove own junction fixture: %v %s", err, out)
		}
		if err := os.Rename(physical, plugins); err != nil {
			t.Errorf("cannot restore own metadata fixture path: %v", err)
		}
	}()
	sentinel := filepath.Join(physical, "keep_own_metadata.txt")
	n130WriteFile(t, sentinel, "own file must remain unchanged")
	if err := snapshot.rollback(); err == nil {
		t.Fatal("swapped vendor metadata parent junction counted as successful rollback")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "own file must remain unchanged" {
		t.Fatalf("own external fixture changed through reparse: %v %q", err, got)
	}
}
