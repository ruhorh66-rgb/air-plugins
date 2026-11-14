//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type runtimeCacheFile struct {
	Live   string
	Saved  string
	SHA256 string
}
type runtimeCacheProfile struct {
	Host        string
	ConfigDir   string
	TargetDir   string
	OldVersion  string
	OldBinSHA   string
	OldBinSaved string
	Files       []runtimeCacheFile
	TreeFiles   []runtimeCacheFile
	TreeDirs    []string
}
type nativeRuntimeIntent struct {
	Schema     string
	SnapshotID string
	Manifest   updateManifest
	Packet     []byte
	Before     []byte
	Existed    bool
	After      []byte
	OldCLI     string
	OldTray    string
	Caches     []runtimeCacheProfile
}
type nativeRuntimeUpdate struct {
	releaseRoot func()
	tx          *runtimeDeploymentTransaction
	intent      nativeRuntimeIntent
	raw         []byte
	root        string
}

func cacheRecoveryRecord(s managedCacheSnapshot) []runtimeCacheProfile {
	var out []runtimeCacheProfile
	for _, p := range s.profiles {
		r := runtimeCacheProfile{Host: p.host, ConfigDir: p.configDir, TargetDir: p.targetDir, OldVersion: p.oldVersion, OldBinSHA: p.oldBinSHA, OldBinSaved: p.oldBinSaved, TreeDirs: p.oldTreeDirs}
		for _, f := range p.files {
			r.Files = append(r.Files, runtimeCacheFile{f.live, f.saved, f.beforeSHA})
		}
		for _, f := range p.oldTreeFiles {
			r.TreeFiles = append(r.TreeFiles, runtimeCacheFile{f.live, f.saved, f.beforeSHA})
		}
		out = append(out, r)
	}
	return out
}
func runtimeInside(root, path string) bool {
	rel, e := filepath.Rel(root, path)
	return e == nil && rel != "." && filepath.IsLocal(rel)
}
func validateRuntimeSaved(snapshot, path, sha string) error {
	if !runtimeInside(snapshot, path) || !runtimeHash.MatchString(sha) {
		return errors.New("runtime recovery snapshot escaped root")
	}
	if _, e := nativeInputPathIdentity(path); e != nil {
		return e
	}
	got, e := sha256File(path)
	if e != nil || got != sha {
		return errors.New("runtime recovery backup digest mismatch")
	}
	return nil
}
func cacheFromRecovery(records []runtimeCacheProfile, snapshot string, m updateManifest) (managedCacheSnapshot, error) {
	var out managedCacheSnapshot
	seen := map[string]bool{}
	for _, r := range records {
		allowed := false
		roots := uniqueConfigRoots(claudeConfigDir(), claudeDefaultConfigDir())
		if r.Host == "codex" {
			roots = uniqueConfigRoots(codexConfigDir(), codexDefaultConfigDir())
		} else if r.Host != "claude" {
			return out, errors.New("unknown recovery host")
		}
		for _, root := range roots {
			if samePath(root, r.ConfigDir) {
				allowed = true
			}
		}
		key := strings.ToLower(filepath.Clean(r.ConfigDir))
		if !allowed || seen[key] || !runtimeComponent(r.OldVersion) || compareUpdateVersionStrings(r.OldVersion, m.Version) >= 0 {
			return out, errors.New("untrusted recovery host scope")
		}
		seen[key] = true
		target, e := managedCacheTarget(r.ConfigDir, m.Version)
		if e != nil || !samePath(target, r.TargetDir) {
			return out, errors.New("runtime recovery target mismatch")
		}
		oldRoot := filepath.Join(filepath.Dir(target), r.OldVersion)
		if e = validateRuntimeSaved(snapshot, r.OldBinSaved, r.OldBinSHA); e != nil {
			return out, e
		}
		p := managedCacheProfileSnapshot{host: r.Host, configDir: r.ConfigDir, targetDir: r.TargetDir, oldVersion: r.OldVersion, oldBinSHA: r.OldBinSHA, oldBinSaved: r.OldBinSaved, oldTreeDirs: r.TreeDirs}
		for _, d := range r.TreeDirs {
			if !filepath.IsLocal(d) {
				return out, errors.New("runtime recovery tree directory escaped")
			}
		}
		allowedMeta := map[string]bool{}
		if r.Host == "claude" {
			allowedMeta[strings.ToLower(filepath.Join(r.ConfigDir, "plugins", "installed_plugins.json"))] = true
			allowedMeta[strings.ToLower(filepath.Join(r.ConfigDir, "plugins", "known_marketplaces.json"))] = true
		} else {
			allowedMeta[strings.ToLower(filepath.Join(r.ConfigDir, "config.toml"))] = true
		}
		if len(r.Files) != len(allowedMeta) {
			return out, errors.New("runtime recovery metadata incomplete")
		}
		for _, f := range r.Files {
			k := strings.ToLower(filepath.Clean(f.Live))
			if !allowedMeta[k] {
				return out, errors.New("runtime recovery metadata escaped scope")
			}
			delete(allowedMeta, k)
			if e = validateRuntimeSaved(snapshot, f.Saved, f.SHA256); e != nil {
				return out, e
			}
			p.files = append(p.files, managedCacheFileSnapshot{live: f.Live, saved: f.Saved, beforeSHA: f.SHA256})
		}
		for _, f := range r.TreeFiles {
			if !runtimeInside(oldRoot, f.Live) {
				return out, errors.New("runtime recovery old tree escaped scope")
			}
			if e = validateRuntimeSaved(snapshot, f.Saved, f.SHA256); e != nil {
				return out, e
			}
			p.oldTreeFiles = append(p.oldTreeFiles, managedCacheFileSnapshot{live: f.Live, saved: f.Saved, beforeSHA: f.SHA256})
		}
		out.profiles = append(out.profiles, p)
	}
	return out, nil
}
func runtimeIntentPath(root string) string { return filepath.Join(root, "update-intent.json") }
func runtimeCommitPath(root string) string { return filepath.Join(root, "update-committed.json") }

// Fetch/preflight before stopping or replacing live processes. A 404 is a
// legacy ordinary update ONLY when runtime discovery has never been promoted.
// Network/invalid signature/partial contract never silently downgrade.
func preflightNativeRuntimeUpdate(m updateManifest, stage, home string) (*nativeRuntimeUpdate, error) {
	root, e := runtimeOwnerRoot()
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	packet, found, e := fetchRuntimeReleasePacket(ctx, m)
	if e != nil {
		return nil, e
	}
	if !found {
		if _, e := os.Lstat(filepath.Join(root, "production.json")); e == nil {
			return nil, errors.New("promoted runtime requires a release acceptance packet")
		} else if !os.IsNotExist(e) {
			return nil, e
		}
		fmt.Fprintln(os.Stdout, "No runtime acceptance asset: ordinary signed update only; runtime not promoted.")
		return nil, nil
	}
	return prepareNativeRuntimePacket(m, stage, home, root, packet)
}

func prepareNativeRuntimePacket(m updateManifest, stage, home, root string, packet []byte) (*nativeRuntimeUpdate, error) {
	consumer, e := installedRuntimeConsumerSHA()
	if e != nil {
		return nil, e
	}
	current, e := os.ReadFile(filepath.Join(stage, "air-worker.exe"))
	if e != nil {
		return nil, e
	}
	previousPath := filepath.Join(home, "bin", "air-worker.exe")
	previous, e := os.ReadFile(previousPath)
	if e != nil {
		return nil, e
	}
	plan, verify, e := runtimeReleasePlan(packet, m, consumer, current, previous, updatePublicKeyB64)
	if e != nil {
		return nil, e
	}
	var old runtimeManifest
	if e = runtimeJSON(plan.Previous.Manifest, &old); e != nil {
		return nil, e
	}
	id := readDistributionBinaryIdentity(previousPath)
	if !id.Readable || id.Modified == nil || *id.Modified || id.Revision != old.SourceRevision || id.SHA256 != old.Artifact.SHA256 {
		return nil, errors.New("installed rollback binary differs from accepted source/artifact")
	}
	// Pin the existing native owner directory before the first mkdir. Never
	// recursively create a runtime through an unchecked ancestor junction.
	releaseRoot, e := createAndHoldNativeRuntimeRoot(root)
	if e != nil {
		return nil, e
	}
	tx, e := prepareRuntimeDeployment(root, plan, verify)
	if e != nil {
		releaseRoot()
		return nil, e
	}
	return &nativeRuntimeUpdate{releaseRoot: releaseRoot, tx: tx, root: root, intent: nativeRuntimeIntent{Schema: "air-worker.runtime-update-intent/v1", Manifest: m, Packet: packet, Before: tx.before, Existed: tx.existed, After: tx.after}}, nil
}
func (u *nativeRuntimeUpdate) Begin(snapshot string, caches managedCacheSnapshot) error {
	return u.beginWithKey(snapshot, caches, updatePublicKeyB64)
}

// Test-only callers may supply an ephemeral public key. The native updater
// always enters through Begin with the compiled release authority.
func (u *nativeRuntimeUpdate) beginWithKey(snapshot string, caches managedCacheSnapshot, key string) error {
	u.intent.SnapshotID = filepath.Base(snapshot)
	if !runtimeComponent(u.intent.SnapshotID) || !samePath(snapshot, filepath.Join(updateRootDir(), "rollback", u.intent.SnapshotID)) {
		return errors.New("runtime snapshot outside updater")
	}
	var e error
	u.intent.OldCLI, e = sha256File(filepath.Join(snapshot, "air-worker.exe"))
	if e != nil {
		return e
	}
	u.intent.OldTray, e = sha256File(filepath.Join(snapshot, "air-worker-tray.exe"))
	if e != nil {
		return e
	}
	u.intent.Caches = cacheRecoveryRecord(caches)
	// Flush every recovery input before publishing durable intent.
	paths := []string{filepath.Join(snapshot, "air-worker.exe"), filepath.Join(snapshot, "air-worker-tray.exe")}
	for _, p := range u.intent.Caches {
		paths = append(paths, p.OldBinSaved)
		for _, f := range append(append([]runtimeCacheFile(nil), p.Files...), p.TreeFiles...) {
			paths = append(paths, f.Saved)
		}
	}
	for _, p := range paths {
		f, e := os.OpenFile(p, os.O_RDWR, 0)
		if e != nil {
			return e
		}
		e = f.Sync()
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
	}
	u.raw, e = json.Marshal(u.intent)
	if len(u.raw) > maxUpdateManifestBytes {
		return errors.New("runtime recovery intent exceeds bounded budget")
	}
	if e != nil {
		return e
	}
	if _, e = decodeNativeRuntimeIntent(u.raw, key); e != nil {
		return e
	}
	if _, e = os.Lstat(runtimeIntentPath(u.root)); e == nil {
		return errors.New("pending runtime intent must be recovered first")
	} else if !os.IsNotExist(e) {
		return e
	}
	return writeFileAtomicDurable(runtimeIntentPath(u.root), u.raw)
}
func (u *nativeRuntimeUpdate) Publish() error {
	actual, e := installedRuntimeConsumerSHA()
	if e != nil {
		return e
	}
	if actual != u.tx.consumerSHA256 {
		return errors.New("installed consumer changed during update")
	}
	return u.tx.Publish()
}
func (u *nativeRuntimeUpdate) Commit() error {
	if e := sameRuntimeDeploymentPointer(u.root, u.tx.after, true); e != nil {
		return e
	}
	return writeFileAtomicDurable(runtimeCommitPath(u.root), []byte(runtimeDigest(u.raw)))
}
func (u *nativeRuntimeUpdate) Clear() error {
	// A retained commit marker is harmless: it is bound to exact immutable intent.
	return os.Remove(runtimeIntentPath(u.root))
}

func recoverNativeRuntimeUpdate(home string) (bool, error) {
	return recoverNativeRuntimeUpdateWithTrust(home, updatePublicKeyB64, knownUpdateProfiles)
}

// Internal seam for isolated signed fixtures; no key/profile override is
// exposed by the CLI. Production always uses the compiled release authority.
func recoverNativeRuntimeUpdateWithTrust(home, key string, profiles func() []distributionProfile) (bool, error) {
	return recoverNativeRuntimeUpdateWithActions(home, key, profiles, nativeRuntimeRecoveryActions{
		stop: stopTray, restart: startTrayDetached, disabled: trayDisabled, version: binaryVersion,
	})
}

type nativeRuntimeRecoveryActions struct {
	stop       func() (bool, bool)
	restart    func(string) error
	disabled   func() bool
	version    func(string) string
	checkpoint func(string)
}

func (a nativeRuntimeRecoveryActions) reached(stage string) {
	if a.checkpoint != nil {
		a.checkpoint(stage)
	}
}

// This private seam lets process-kill tests use genuine disk recovery without
// stopping production tray processes or launching a fixture as an executable.
func recoverNativeRuntimeUpdateWithActions(home, key string, profiles func() []distributionProfile, actions nativeRuntimeRecoveryActions) (bool, error) {
	if actions.stop == nil || actions.restart == nil || actions.disabled == nil || actions.version == nil {
		return false, errors.New("runtime recovery process controls unavailable")
	}
	root, e := runtimeOwnerRoot()
	if e != nil {
		return false, e
	}
	path := runtimeIntentPath(root)
	if _, e = os.Lstat(path); os.IsNotExist(e) {
		return false, nil
	} else if e != nil {
		return false, e
	}
	if !samePath(home, updateInstallHome()) {
		return false, errors.New("runtime recovery home differs from native install")
	}
	unpin, e := releaseR04HoldSignedScratchUntilChild(root)
	if e != nil {
		return false, e
	}
	defer unpin()
	raw, e := runtimeRead(path)
	if e != nil {
		return false, e
	}
	intent, e := decodeNativeRuntimeIntent(raw, key)
	if e != nil {
		return false, e
	}
	snapshot := filepath.Join(updateRootDir(), "rollback", intent.SnapshotID)
	unpinSnapshot, e := releaseR04HoldSignedScratchUntilChild(snapshot)
	if e != nil {
		return false, e
	}
	defer unpinSnapshot()
	if e = validateRuntimeSaved(snapshot, filepath.Join(snapshot, "air-worker.exe"), intent.OldCLI); e != nil {
		return false, e
	}
	if e = validateRuntimeSaved(snapshot, filepath.Join(snapshot, "air-worker-tray.exe"), intent.OldTray); e != nil {
		return false, e
	}
	caches, e := cacheFromRecovery(intent.Caches, snapshot, intent.Manifest)
	if e != nil {
		return false, e
	}
	// Establish the complete allowed prestate before any recovery write or
	// process control. A third-party binary must never be replaced by rollback.
	if e = validateNativeRuntimeRecoveryState(root, home, intent); e != nil {
		return false, e
	}
	commit, e := os.ReadFile(runtimeCommitPath(root))
	if e == nil && string(commit) == runtimeDigest(raw) {
		if e = sameRuntimeDeploymentPointer(root, intent.After, true); e != nil {
			return false, e
		}
		if e = verifyDownloadedArtifact(filepath.Join(home, "bin", "air-worker.exe"), intent.Manifest.CLI); e != nil {
			return false, e
		}
		if e = verifyDownloadedArtifact(filepath.Join(home, "bin", "air-worker-tray.exe"), intent.Manifest.Tray); e != nil {
			return false, e
		}
		if profiles == nil {
			return false, errors.New("runtime cache identity provider unavailable")
		}
		if e = verifyNativeRuntimeCommitted(root, home, snapshot, intent, key, profiles()); e != nil {
			return false, e
		}
		return false, os.Remove(path)
	}
	if e != nil && !os.IsNotExist(e) {
		return false, e
	}
	tx := runtimeDeploymentTransaction{root: root, before: intent.Before, existed: intent.Existed, after: intent.After, published: true}
	// Preflight pointer conflict before restoring any native/cache bytes.
	if a := sameRuntimeDeploymentPointer(root, intent.Before, intent.Existed); a != nil {
		if e = sameRuntimeDeploymentPointer(root, intent.After, true); e != nil {
			return false, e
		}
	}
	release, e := caches.holdCanonicalParentsDuringUpdate()
	if e != nil {
		return false, e
	}
	defer release()
	if asked, gone := actions.stop(); asked && !gone {
		return false, errors.New("tray did not stop for runtime recovery")
	}
	// Recheck after stopping the tray. A conflict aborts without restoring
	// binaries/caches; never mix a foreign pointer with an old bootstrap.
	if e = validateNativeRuntimeRecoveryState(root, home, intent); e != nil {
		return false, e
	}
	actions.reached("recovery-before-pointer")
	if e = tx.Rollback(); e != nil {
		return false, e
	}
	actions.reached("recovery-after-pointer")
	var problems []error
	if e = restoreUpdateSnapshot(snapshot, home); e != nil {
		problems = append(problems, e)
	}
	actions.reached("recovery-after-binaries")
	if e = caches.rollback(); e != nil {
		problems = append(problems, e)
	}
	if len(problems) > 0 {
		return false, errors.Join(problems...)
	}
	if e = writeUpdateState(updateState{CurrentVersion: actions.version(filepath.Join(home, "bin", "air-worker.exe")), Channel: intent.Manifest.Channel, LatestVersion: intent.Manifest.Version, Phase: "rolled_back", Error: "recovered interrupted runtime update"}); e != nil {
		return false, e
	}
	actions.reached("recovery-after-state")
	if e = os.Remove(path); e != nil {
		return false, e
	}
	actions.reached("recovery-after-intent-clear")
	if !actions.disabled() {
		if e = actions.restart(filepath.Join(home, "bin", "air-worker-tray.exe")); e != nil {
			return true, e
		}
	}
	return true, nil
}
func decodeNativeRuntimeIntent(raw []byte, key string) (nativeRuntimeIntent, error) {
	var e error
	var intent nativeRuntimeIntent
	if e = runtimeJSON(raw, &intent); e != nil {
		return intent, e
	}
	if intent.Schema != "air-worker.runtime-update-intent/v1" || !runtimeComponent(intent.SnapshotID) {
		return intent, errors.New("invalid runtime recovery intent")
	}
	if _, e = verifyUpdateManifestWithKey(intent.Manifest, intent.Manifest.Version, intent.Manifest.Channel, key, updateKeyID); e != nil {
		return intent, e
	}
	var packet runtimeReleasePacket
	if e = runtimeJSON(intent.Packet, &packet); e != nil {
		return intent, e
	}
	if _, e = verifyRuntimeReleasePacket(intent.Packet, intent.Manifest, packet.ConsumerSHA256, key); e != nil {
		return intent, e
	}
	var current, previous runtimeManifest
	if e = runtimeJSON(packet.Current.Manifest, &current); e != nil {
		return intent, e
	}
	if e = runtimeJSON(packet.Previous.Manifest, &previous); e != nil {
		return intent, e
	}
	expected, _ := json.Marshal(runtimePointer{1, "air-worker", current.ReleaseID, runtimeDigest(packet.Current.Manifest), true, previous.ReleaseID})
	expected = append(expected, '\n')
	if !bytes.Equal(expected, intent.After) || (!intent.Existed && len(intent.Before) != 0) || intent.OldCLI != previous.Artifact.SHA256 {
		return intent, errors.New("runtime recovery binding mismatch")
	}
	return intent, nil
}
func validateNativeRuntimeRecoveryState(root, home string, intent nativeRuntimeIntent) error {
	if e := sameRuntimeDeploymentPointer(root, intent.Before, intent.Existed); e != nil {
		if e = sameRuntimeDeploymentPointer(root, intent.After, true); e != nil {
			return e
		}
	}
	for _, file := range []struct{ name, old, next string }{
		{"air-worker.exe", intent.OldCLI, intent.Manifest.CLI.SHA256},
		{"air-worker-tray.exe", intent.OldTray, intent.Manifest.Tray.SHA256},
	} {
		path := filepath.Join(home, "bin", file.name)
		if _, e := nativeInputPathIdentity(path); e != nil {
			return e
		}
		got, e := sha256File(path)
		if e != nil {
			return e
		}
		if got != file.old && got != file.next {
			return fmt.Errorf("foreign live %s; recovery refused", file.name)
		}
	}
	return nil
}

func validateRuntimeCommittedCaches(records []runtimeCacheProfile, actual []distributionProfile, m updateManifest) error {
	if len(records) != len(actual) {
		return errors.New("committed runtime host profile set changed")
	}
	expected := map[string]bool{}
	for _, r := range records {
		k := strings.ToLower(r.Host + "|" + filepath.Clean(r.ConfigDir))
		if expected[k] {
			return errors.New("duplicate committed host profile")
		}
		expected[k] = true
	}
	for _, p := range actual {
		k := strings.ToLower(p.Host + "|" + filepath.Clean(p.ConfigDir))
		if !expected[k] || !p.HasState || !p.PluginEnabled || !p.Canonical || p.Error != "" ||
			p.Version != m.Version || !strings.EqualFold(p.Revision, m.VCSRevision) || !strings.EqualFold(p.PayloadSHA256, m.PayloadSHA256) ||
			!p.CacheBinary.Readable || !strings.EqualFold(p.CacheBinary.SHA256, m.CLI.SHA256) {
			return errors.New("committed runtime host cache identity changed")
		}
		delete(expected, k)
	}
	return nil
}
func verifyNativeRuntimeCommitted(root, home, snapshot string, intent nativeRuntimeIntent, key string, profiles []distributionProfile) error {
	consumer, e := installedRuntimeConsumerSHA()
	if e != nil {
		return e
	}
	current, e := os.ReadFile(filepath.Join(home, "bin", "air-worker.exe"))
	if e != nil {
		return e
	}
	previous, e := os.ReadFile(filepath.Join(snapshot, "air-worker.exe"))
	if e != nil {
		return e
	}
	plan, verify, e := runtimeReleasePlan(intent.Packet, intent.Manifest, consumer, current, previous, key)
	if e != nil {
		return e
	}
	for _, slot := range []runtimeDeploymentSlot{plan.Current, plan.Previous} {
		m, e := validateRuntimeDeploymentSlot(slot, consumer, verify)
		if e != nil {
			return e
		}
		if e = runtimeSlotBytes(root, m, slot); e != nil {
			return e
		}
	}
	if _, e = resolveRuntime(root, "air-curator", "legacy-learning", "air-worker.legacy-learning/v1"); e != nil {
		return e
	}
	if e = validateRuntimeCommittedCaches(intent.Caches, profiles, intent.Manifest); e != nil {
		return e
	}
	return sameRuntimeDeploymentPointer(root, intent.After, true)
}

func createAndHoldNativeRuntimeRoot(root string) (func(), error) {
	if !filepath.IsAbs(root) {
		return nil, errors.New("runtime root must be absolute")
	}
	releaseParent, e := releaseR04HoldSignedScratchUntilChild(filepath.Dir(root))
	if e != nil {
		return nil, e
	}
	// The owner installation must already exist. Create only the runtime leaf;
	// MkdirAll before pinning could write through an existing ancestor junction.
	if e = os.Mkdir(root, 0700); e != nil && !os.IsExist(e) {
		releaseParent()
		return nil, e
	}
	releaseRoot, e := releaseR04HoldSignedScratchUntilChild(root)
	if e != nil {
		releaseParent()
		return nil, e
	}
	return func() { releaseRoot(); releaseParent() }, nil
}
