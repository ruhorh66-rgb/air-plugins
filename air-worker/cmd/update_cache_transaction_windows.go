//go:build windows

package main

// Native update cache rollback belongs to the same signed transaction as
// the CLI/tray pair. No passwords, private tokens or foreign plugin paths.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type managedCacheFileSnapshot struct {
	live, saved, beforeSHA string
}
type managedCacheProfileSnapshot struct {
	host, configDir, targetDir, oldVersion, oldBinSHA, oldBinSaved string
	files                                                          []managedCacheFileSnapshot
	oldTreeFiles                                                   []managedCacheFileSnapshot
	oldTreeDirs                                                    []string
}
type managedCacheSnapshot struct {
	profiles []managedCacheProfileSnapshot
}

func managedCacheTarget(configDir, version string) (string, error) {
	// The manifest is signature-verified before call, but validate again
	// at the filesystem boundary.
	if version == "" || version == "." || version == ".." ||
		strings.ContainsAny(version, "\\/:") || strings.Contains(version, "..") {
		return "", fmt.Errorf("invalid plugin cache version %q", version)
	}
	root := filepath.Join(configDir, "plugins", "cache", canonicalMarketplace, "air-worker")
	target := filepath.Join(root, version)
	rel, err := filepath.Rel(root, target)
	if err != nil || rel != version {
		return "", fmt.Errorf("plugin cache target escaped canonical root")
	}
	return target, nil
}

// Snapshot every regular file of the previous owned version, not only the
// executable. Go os.Root pins a directory handle on Windows and constrains
// traversal, while the existing R04 primitive rejects parent reparse paths.
func snapshotOtherManagedVersionFiles(snapshotDir, oldDir string, index int, host string) ([]managedCacheFileSnapshot, []string, error) {
	unpin, err := releaseR04HoldSignedScratchUntilChild(oldDir)
	if err != nil {
		return nil, nil, err
	}
	defer unpin()
	source, err := os.OpenRoot(oldDir)
	if err != nil {
		return nil, nil, err
	}
	defer source.Close()
	destination, err := os.OpenRoot(snapshotDir)
	if err != nil {
		return nil, nil, err
	}
	defer destination.Close()
	var savedFiles []managedCacheFileSnapshot
	var dirs []string
	var count int
	var bytesTotal int64
	err = fs.WalkDir(source.FS(), ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if rel == "." {
			return nil
		}
		live := filepath.Join(oldDir, filepath.FromSlash(rel))
		pathID, err := nativeInputPathIdentity(live)
		if err != nil {
			return fmt.Errorf("old managed cache contains reparse/unreadable entry %s: %w", rel, err)
		}
		if entry.IsDir() {
			dirs = append(dirs, filepath.FromSlash(rel))
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("old managed cache contains special entry: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		count++
		bytesTotal += info.Size()
		if count > 2400 || info.Size() < 0 || info.Size() > 64<<20 || bytesTotal > 256<<20 {
			return errors.New("old managed cache exceeds bounded snapshot budget")
		}
		// The previous binary is already snapshotted and SHA-checked separately.
		if strings.EqualFold(filepath.FromSlash(rel), filepath.Join("bin", executableName())) {
			return nil
		}
		in, err := source.Open(rel)
		if err != nil {
			return err
		}
		openedID, err := nativeInputOpenedIdentity(in)
		if err != nil || !nativeInputSameIdentity(pathID, openedID) {
			_ = in.Close()
			return fmt.Errorf("old managed cache entry changed identity while opening: %s", rel)
		}
		destRel := filepath.Join("plugin-cache", fmt.Sprintf("%02d-%s-old-tree", index, host), filepath.FromSlash(rel))
		if err := destination.MkdirAll(filepath.Dir(destRel), 0700); err != nil {
			_ = in.Close()
			return err
		}
		out, err := destination.OpenFile(destRel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			_ = in.Close()
			return err
		}
		sum := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(out, sum), in)
		closeErr := out.Close()
		if copyErr != nil || closeErr != nil || n != info.Size() {
			_ = in.Close()
			return fmt.Errorf("old cache snapshot copy incomplete: %s: %v / %v", rel, copyErr, closeErr)
		}
		if _, err := in.Seek(0, io.SeekStart); err != nil {
			_ = in.Close()
			return err
		}
		check := sha256.New()
		_, checkErr := io.Copy(check, in)
		_ = in.Close()
		sha := hex.EncodeToString(sum.Sum(nil))
		if checkErr != nil || !strings.EqualFold(sha, hex.EncodeToString(check.Sum(nil))) {
			return fmt.Errorf("old managed cache changed during snapshot: %s", rel)
		}
		saved := filepath.Join(snapshotDir, destRel)
		if got, err := sha256File(saved); err != nil || !strings.EqualFold(got, sha) {
			return fmt.Errorf("old managed cache backup SHA mismatch: %s: %v", rel, err)
		}
		savedFiles = append(savedFiles, managedCacheFileSnapshot{live: live, saved: saved, beforeSHA: sha})
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	if count == 0 {
		return nil, nil, errors.New("empty previous managed cache")
	}
	return savedFiles, dirs, nil
}

// Restore the snapshotted non-binary files only under the previously
// installed AirWorker version. os.Root prevents traversing out of that cache
// while recreating directories; no foreign version or metadata is deleted.
func restoreOtherManagedVersionFiles(p managedCacheProfileSnapshot) error {
	cacheRoot := filepath.Join(p.configDir, "plugins", "cache", canonicalMarketplace, "air-worker")
	unpin, err := releaseR04HoldSignedScratchUntilChild(cacheRoot)
	if err != nil {
		return fmt.Errorf("previous managed cache parent cannot be pinned: %w", err)
	}
	defer unpin()
	root, err := os.OpenRoot(cacheRoot)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.MkdirAll(p.oldVersion, 0700); err != nil {
		return err
	}
	for _, rel := range p.oldTreeDirs {
		subdir := filepath.Join(p.oldVersion, rel)
		if err := root.MkdirAll(subdir, 0700); err != nil {
			return err
		}
		if _, err := nativeInputPathIdentity(filepath.Join(cacheRoot, subdir)); err != nil {
			return fmt.Errorf("old cache subdirectory was replaced by reparse: %s: %w", rel, err)
		}
	}
	for _, file := range p.oldTreeFiles {
		rel, err := filepath.Rel(cacheRoot, file.live)
		if err != nil || !strings.HasPrefix(strings.ToLower(rel), strings.ToLower(p.oldVersion)+string(filepath.Separator)) {
			return fmt.Errorf("old cache snapshot file escaped managed version: %s", file.live)
		}
		dir := filepath.Dir(file.live)
		unpinDir, err := releaseR04HoldSignedScratchUntilChild(dir)
		if err != nil {
			return fmt.Errorf("old cache file parent changed identity: %s: %w", file.live, err)
		}
		if info, err := os.Lstat(file.live); err == nil {
			if !info.Mode().IsRegular() {
				unpinDir()
				return fmt.Errorf("old managed cache file became reparse/special: %s", file.live)
			}
		} else if !os.IsNotExist(err) {
			unpinDir()
			return err
		}
		if got, err := sha256File(file.saved); err != nil || !strings.EqualFold(got, file.beforeSHA) {
			unpinDir()
			return fmt.Errorf("old cache backup SHA mismatch before restore: %s", file.saved)
		}
		data, err := os.ReadFile(file.saved)
		if err != nil || len(data) > 64<<20 {
			unpinDir()
			return fmt.Errorf("old cache backup unavailable or too large: %s", file.saved)
		}
		err = writeFileAtomic(file.live, data)
		unpinDir()
		if err != nil {
			return fmt.Errorf("restore previous managed cache file %s: %w", rel, err)
		}
		if got, err := sha256File(file.live); err != nil || !strings.EqualFold(got, file.beforeSHA) {
			return fmt.Errorf("old cache file SHA not restored: %s", rel)
		}
	}
	return nil
}

func snapshotManagedUpdateCaches(snapshotDir string, m updateManifest, profiles []distributionProfile) (managedCacheSnapshot, error) {
	var out managedCacheSnapshot
	if len(profiles) == 0 {
		return out, errors.New("no canonical installed AirWorker host plugins")
	}
	seen := make(map[string]bool)
	for index, p := range profiles {
		if !p.PluginEnabled || !p.Canonical || p.InstallPath == "" ||
			(p.Host != "claude" && p.Host != "codex") {
			return managedCacheSnapshot{}, fmt.Errorf("untrusted or inactive AirWorker host cache %s", p.Host)
		}
		if p.Version == m.Version || compareUpdateVersionStrings(p.Version, m.Version) >= 0 {
			return managedCacheSnapshot{}, fmt.Errorf("%s plugin target not a fresh upgrade: %s -> %s", p.Host, p.Version, m.Version)
		}
		if p.ConfigDir == "" || p.CacheBinary.SHA256 == "" || !p.CacheBinary.Readable {
			return managedCacheSnapshot{}, fmt.Errorf("%s old cache identity not provable", p.Host)
		}
		target, err := managedCacheTarget(p.ConfigDir, m.Version)
		if err != nil {
			return managedCacheSnapshot{}, err
		}
		// Reuse the existing native Windows R04 handle-based ancestor guard:
		// fail before any vendor cache mutation if a profile path is a junction
		// or another reparse alias. This check does not change cache content.
		unpin, err := releaseR04HoldSignedScratchUntilChild(filepath.Dir(target))
		if err != nil {
			return managedCacheSnapshot{}, fmt.Errorf("%s cache parent is not an ordinary pinned path: %w", p.Host, err)
		}
		unpin()
		if _, err := os.Lstat(target); err == nil {
			return managedCacheSnapshot{}, fmt.Errorf("%s target cache already exists: %s", p.Host, target)
		} else if !os.IsNotExist(err) {
			return managedCacheSnapshot{}, fmt.Errorf("inspect target cache %s: %w", target, err)
		}
		key := strings.ToLower(filepath.Clean(target))
		if seen[key] {
			return managedCacheSnapshot{}, errors.New("duplicate target cache")
		}
		seen[key] = true
		part := managedCacheProfileSnapshot{
			host: p.Host, configDir: p.ConfigDir, targetDir: target,
			oldVersion: p.Version, oldBinSHA: p.CacheBinary.SHA256,
		}
		// Preserve the exact previous managed binary before any vendor command.
		// Existing installed rc.2 and foreign plugin versions remain untouched.
		oldBinary := filepath.Join(p.ConfigDir, "plugins", "cache", canonicalMarketplace,
			"air-worker", p.Version, "bin", executableName())
		oldInfo, err := os.Lstat(oldBinary)
		if err != nil || !oldInfo.Mode().IsRegular() {
			return managedCacheSnapshot{}, fmt.Errorf("%s previous cache executable is not an ordinary file: %v", p.Host, err)
		}
		if got, err := sha256File(oldBinary); err != nil || !strings.EqualFold(got, p.CacheBinary.SHA256) {
			return managedCacheSnapshot{}, fmt.Errorf("%s previous cache SHA changed before snapshot: %v", p.Host, err)
		}
		oldSaved := filepath.Join(snapshotDir, "plugin-cache", fmt.Sprintf("%02d-%s-prior-bin.backup", index, p.Host))
		if err := os.MkdirAll(filepath.Dir(oldSaved), 0700); err != nil {
			return managedCacheSnapshot{}, err
		}
		if err := copyFile(oldBinary, oldSaved); err != nil {
			return managedCacheSnapshot{}, err
		}
		if got, err := sha256File(oldSaved); err != nil || !strings.EqualFold(got, p.CacheBinary.SHA256) {
			return managedCacheSnapshot{}, fmt.Errorf("%s previous binary backup SHA mismatch: %v", p.Host, err)
		}
		part.oldBinSaved = oldSaved
		// Own previous plugin files (skills/hooks/manifests), not just the CLI,
		// must survive a failed Claude/Codex vendor sync with exact SHA.
		part.oldTreeFiles, part.oldTreeDirs, err = snapshotOtherManagedVersionFiles(
			snapshotDir, filepath.Dir(filepath.Dir(oldBinary)), index, p.Host)
		if err != nil {
			return managedCacheSnapshot{}, fmt.Errorf("%s old cache tree snapshot: %w", p.Host, err)
		}
		var metadata []string
		if p.Host == "claude" {
			metadata = []string{"plugins/installed_plugins.json", "plugins/known_marketplaces.json"}
		} else {
			metadata = []string{"config.toml"}
		}
		for i, rel := range metadata {
			live := filepath.Join(p.ConfigDir, filepath.FromSlash(rel))
			info, err := os.Lstat(live)
			if err != nil {
				return managedCacheSnapshot{}, fmt.Errorf("%s metadata prestate: %w", p.Host, err)
			}
			if !info.Mode().IsRegular() {
				return managedCacheSnapshot{}, fmt.Errorf("%s metadata is not regular: %s", p.Host, live)
			}
			sha, err := sha256File(live)
			if err != nil {
				return managedCacheSnapshot{}, err
			}
			saved := filepath.Join(snapshotDir, "plugin-cache", fmt.Sprintf("%02d-%s-%d.backup", index, p.Host, i))
			if err := os.MkdirAll(filepath.Dir(saved), 0700); err != nil {
				return managedCacheSnapshot{}, err
			}
			if err := copyFile(live, saved); err != nil {
				return managedCacheSnapshot{}, err
			}
			if got, err := sha256File(saved); err != nil || !strings.EqualFold(got, sha) {
				return managedCacheSnapshot{}, fmt.Errorf("%s metadata backup SHA mismatch: %s: %v", p.Host, live, err)
			}
			part.files = append(part.files, managedCacheFileSnapshot{live: live, saved: saved, beforeSHA: sha})
		}
		out.profiles = append(out.profiles, part)
	}
	return out, nil
}

// Windows junctions are not consistently flagged by Lstat/ModeSymlink.
// Readlink can identify junctions, so reject them throughout a cache tree
// before removing anything. Do not follow a path into another product.
func validateNewCacheTree(path string) error {
	return filepath.WalkDir(path, func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache contains symlink: %s", current)
		}
		if _, err := os.Readlink(current); err == nil {
			return fmt.Errorf("cache contains Windows junction/link: %s", current)
		}
		// Reuse native Win32 OPEN_REPARSE_POINT identity inspection. WalkDir
		// alone does not reliably flag every NTFS junction/reparse tag.
		if _, err := nativeInputPathIdentity(current); err != nil {
			return fmt.Errorf("cache tree cannot prove ordinary Win32 identity %s: %w", current, err)
		}
		return nil
	})
}

// quarantineManagedCandidateCache removes only the active version name from
// the canonical AirWorker cache. It NEVER recursively deletes cache contents.
// On rollback failure the candidate remains in an own-product quarantine with
// its bytes intact for forensic inspection and later deliberate cleanup.
func quarantineManagedCandidateCache(path string) error {
	parent := filepath.Dir(path)
	unpin, err := releaseR04HoldSignedScratchUntilChild(parent)
	if err != nil {
		return fmt.Errorf("unsafe candidate parent: %w", err)
	}
	defer unpin()
	before, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil // no candidate created by the failed vendor update
	}
	if err != nil {
		return err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("candidate cache is not an ordinary directory: %s", path)
	}
	if err := validateNewCacheTree(path); err != nil {
		return err
	}
	beforeID, err := nativeInputPathIdentity(path)
	if err != nil {
		return fmt.Errorf("candidate Win32 identity unavailable: %w", err)
	}
	quarantine := filepath.Join(parent, fmt.Sprintf(".air-worker-rollback-%s-%d",
		filepath.Base(path), time.Now().UTC().UnixNano()))
	if _, err := os.Lstat(quarantine); !os.IsNotExist(err) {
		return fmt.Errorf("quarantine name is occupied or unreadable: %v", err)
	}
	// The same-volume directory rename is atomic. Even if the candidate
	// changes after validation, the operation moves an entry, never follows
	// a link to recursively erase a different product's contents.
	if err := os.Rename(path, quarantine); err != nil {
		return fmt.Errorf("quarantine new candidate without deletion: %w", err)
	}
	afterID, err := nativeInputPathIdentity(quarantine)
	if err != nil || !nativeInputSameIdentity(beforeID, afterID) {
		return errors.New("candidate changed Win32 identity during quarantine; bytes retained")
	}
	if err := validateNewCacheTree(quarantine); err != nil {
		return fmt.Errorf("quarantined candidate contains unsafe reparse contents: %w", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("candidate path reappeared after quarantine; rollback incomplete")
	}
	return nil
}

// An exact rollback cannot report PASS if a vendor left unknown files in the
// previous AirWorker version. Never delete those files; flag the incomplete
// restoration for an operator. Only the canonical own version is inspected.
func verifyRestoredManagedCacheTree(p managedCacheProfileSnapshot) error {
	oldDir := filepath.Join(p.configDir, "plugins", "cache", canonicalMarketplace,
		"air-worker", p.oldVersion)
	unpin, err := releaseR04HoldSignedScratchUntilChild(oldDir)
	if err != nil {
		return err
	}
	defer unpin()
	root, err := os.OpenRoot(oldDir)
	if err != nil {
		return err
	}
	defer root.Close()
	expectedFiles := map[string]string{
		strings.ToLower(filepath.Join("bin", executableName())): p.oldBinSHA,
	}
	for _, f := range p.oldTreeFiles {
		rel, err := filepath.Rel(oldDir, f.live)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("snapshot file outside own previous version: %s", f.live)
		}
		expectedFiles[strings.ToLower(filepath.Clean(rel))] = f.beforeSHA
	}
	expectedDirs := map[string]bool{".": true}
	for _, rel := range p.oldTreeDirs {
		expectedDirs[strings.ToLower(filepath.Clean(rel))] = true
	}
	seen := make(map[string]bool)
	err = fs.WalkDir(root.FS(), ".", func(rel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		live := filepath.Join(oldDir, filepath.FromSlash(rel))
		pathID, err := nativeInputPathIdentity(live)
		if err != nil {
			return fmt.Errorf("reparse or invalid path in restored cache %s: %w", rel, err)
		}
		key := strings.ToLower(filepath.Clean(filepath.FromSlash(rel)))
		if entry.IsDir() {
			if !expectedDirs[key] {
				return fmt.Errorf("unknown directory after vendor update, preserved: %s", rel)
			}
			return nil
		}
		want, ok := expectedFiles[key]
		if !ok {
			return fmt.Errorf("unknown old cache file after vendor update, preserved: %s", rel)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("old cache contains nonregular file: %s", rel)
		}
		file, err := root.Open(rel)
		if err != nil {
			return err
		}
		openedID, err := nativeInputOpenedIdentity(file)
		if err != nil || !nativeInputSameIdentity(pathID, openedID) {
			_ = file.Close()
			return fmt.Errorf("restored cache file identity changed: %s", rel)
		}
		h := sha256.New()
		_, readErr := io.Copy(h, file)
		_ = file.Close()
		if readErr != nil || !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), want) {
			return fmt.Errorf("restored cache file SHA mismatch: %s", rel)
		}
		seen[key] = true
		return nil
	})
	if err != nil {
		return err
	}
	if len(seen) != len(expectedFiles) {
		return fmt.Errorf("restored previous cache count %d != snapshot %d", len(seen), len(expectedFiles))
	}
	return nil
}

// Keep the exact canonical cache parents pinned for the entire vendor update,
// not only during individual snapshot/rollback checks. The native installer
// lock serializes AirWorker processes; these Win32 handles additionally
// prevent a foreign profile/path rename while vendor commands are running.
func (s managedCacheSnapshot) holdCanonicalParentsDuringUpdate() (func(), error) {
	var holds []func()
	var roots []*os.Root
	release := func() {
		for i := len(roots) - 1; i >= 0; i-- {
			_ = roots[i].Close()
		}
		for i := len(holds) - 1; i >= 0; i-- {
			holds[i]()
		}
	}
	for _, p := range s.profiles {
		parent := filepath.Dir(p.targetDir)
		// R04 validates ordinary physical ancestors; Go Root additionally
		// holds a Windows directory handle that actually blocks rename.
		hold, err := releaseR04HoldSignedScratchUntilChild(parent)
		if err != nil {
			release()
			return nil, fmt.Errorf("cannot validate own %s cache parent: %w", p.host, err)
		}
		root, err := os.OpenRoot(parent)
		if err != nil {
			hold()
			release()
			return nil, fmt.Errorf("cannot open durable %s Windows cache root: %w", p.host, err)
		}
		probe, err := root.Open(".")
		if err != nil {
			_ = root.Close()
			hold()
			release()
			return nil, err
		}
		opened, openedErr := nativeInputOpenedIdentity(probe)
		_ = probe.Close()
		current, pathErr := nativeInputPathIdentity(parent)
		if openedErr != nil || pathErr != nil || !nativeInputSameIdentity(opened, current) {
			_ = root.Close()
			hold()
			release()
			return nil, fmt.Errorf("cache parent identity changed before native hold: %s", parent)
		}
		holds = append(holds, hold)
		roots = append(roots, root)
	}
	return release, nil
}

func (s managedCacheSnapshot) rollback() error {
	var problems []string
	for _, p := range s.profiles {
		// Target version was absent at transaction start. Never recurse
		// through a mutable Windows path, even after a separate tree check.
		if err := quarantineManagedCandidateCache(p.targetDir); err != nil {
			problems = append(problems, "quarantine unsafe candidate cache "+p.targetDir+": "+err.Error())
		}
		for _, f := range p.files {
			// Host plugin metadata belongs to this profile, but cannot be
			// restored through a swapped parent junction or file link.
			unpin, err := releaseR04HoldSignedScratchUntilChild(filepath.Dir(f.live))
			if err != nil {
				problems = append(problems, "unsafe metadata parent "+f.live+": "+err.Error())
				continue
			}
			if info, err := os.Lstat(f.live); err == nil {
				if !info.Mode().IsRegular() {
					problems = append(problems, "refuse unsafe metadata "+f.live)
					unpin()
					continue
				}
				if _, err := nativeInputPathIdentity(f.live); err != nil {
					problems = append(problems, "refuse reparse metadata "+f.live+": "+err.Error())
					unpin()
					continue
				}
			} else if !os.IsNotExist(err) {
				problems = append(problems, "stat metadata "+f.live+": "+err.Error())
				unpin()
				continue
			}
			got, shaErr := sha256File(f.saved)
			if shaErr != nil || !strings.EqualFold(got, f.beforeSHA) {
				problems = append(problems, "saved metadata SHA mismatch "+f.live)
				unpin()
				continue
			}
			backup, readErr := os.ReadFile(f.saved)
			if readErr != nil || len(backup) > 8<<20 {
				problems = append(problems, "saved metadata missing or oversized "+f.live)
				unpin()
				continue
			}
			// Atomic same-directory replacement, never truncate-in-place.
			writeErr := writeFileAtomic(f.live, backup)
			unpin()
			if writeErr != nil {
				problems = append(problems, "restore metadata "+f.live+": "+writeErr.Error())
				continue
			}
			if got, err := sha256File(f.live); err != nil || !strings.EqualFold(got, f.beforeSHA) {
				problems = append(problems, "metadata SHA readback failed "+f.live)
			}
		}
		if err := restoreOtherManagedVersionFiles(p); err != nil {
			problems = append(problems, "restore prior "+p.host+" owned cache tree: "+err.Error())
		}
		old := filepath.Join(p.configDir, "plugins", "cache", canonicalMarketplace, "air-worker", p.oldVersion, "bin", executableName())
		// Restore only the previous AirWorker managed binary that we snapshotted.
		// A path changed into a junction/symlink is never a restoration target.
		unpin, pinErr := releaseR04HoldSignedScratchUntilChild(filepath.Dir(old))
		if pinErr != nil {
			problems = append(problems, "old "+p.host+" cache parent unsafe: "+pinErr.Error())
			continue
		}
		if info, err := os.Lstat(old); err == nil && !info.Mode().IsRegular() {
			problems = append(problems, "refuse unsafe old cache binary "+old)
			unpin()
			continue
		} else if err != nil && !os.IsNotExist(err) {
			problems = append(problems, "stat old cache binary "+old+": "+err.Error())
			unpin()
			continue
		}
		if got, err := sha256File(p.oldBinSaved); err != nil || !strings.EqualFold(got, p.oldBinSHA) {
			problems = append(problems, "old "+p.host+" binary backup SHA invalid")
			unpin()
			continue
		}
		backup, err := os.ReadFile(p.oldBinSaved)
		if err != nil || len(backup) > 64<<20 {
			problems = append(problems, "old "+p.host+" binary backup cannot be read safely")
			unpin()
			continue
		}
		if err := writeFileAtomic(old, backup); err != nil {
			problems = append(problems, "restore previous "+p.host+" managed cache binary: "+err.Error())
		}
		unpin()
		if got, err := sha256File(old); err != nil || !strings.EqualFold(got, p.oldBinSHA) {
			problems = append(problems, "old "+p.host+" cache binary SHA readback failed")
		}
		if err := verifyRestoredManagedCacheTree(p); err != nil {
			problems = append(problems, "previous "+p.host+" cache exact-tree verification: "+err.Error())
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("plugin cache rollback: %s", strings.Join(problems, "; "))
	}
	return nil
}
