//go:build windows

package main

// Native update cache rollback belongs to the same signed transaction as
// the CLI/tray pair. No passwords, private tokens or foreign plugin paths.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type managedCacheFileSnapshot struct {
	live, saved, beforeSHA string
}
type managedCacheProfileSnapshot struct {
	host, configDir, targetDir, oldVersion, oldBinSHA string
	files                                             []managedCacheFileSnapshot
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
		return nil
	})
}

func (s managedCacheSnapshot) rollback() error {
	var problems []string
	for _, p := range s.profiles {
		// Target version was absent at transaction start. Do not remove old
		// releases or unrelated cache trees; reject a newly linked target.
		if info, err := os.Lstat(p.targetDir); err == nil {
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				problems = append(problems, "refuse unsafe target cache "+p.targetDir)
			} else if err := validateNewCacheTree(p.targetDir); err != nil {
				problems = append(problems, "refuse linked target cache "+p.targetDir+": "+err.Error())
			} else if err := os.RemoveAll(p.targetDir); err != nil {
				problems = append(problems, "remove new cache "+p.targetDir+": "+err.Error())
			}
		} else if !os.IsNotExist(err) {
			problems = append(problems, "stat new cache "+p.targetDir+": "+err.Error())
		}
		for _, f := range p.files {
			if info, err := os.Lstat(f.live); err == nil && !info.Mode().IsRegular() {
				problems = append(problems, "refuse unsafe metadata "+f.live)
				continue
			} else if err != nil && !os.IsNotExist(err) {
				problems = append(problems, "stat metadata "+f.live+": "+err.Error())
				continue
			}
			if err := copyFile(f.saved, f.live); err != nil {
				problems = append(problems, "restore metadata "+f.live+": "+err.Error())
				continue
			}
			if got, err := sha256File(f.live); err != nil || !strings.EqualFold(got, f.beforeSHA) {
				problems = append(problems, "metadata SHA readback failed "+f.live)
			}
		}
		old := filepath.Join(p.configDir, "plugins", "cache", canonicalMarketplace, "air-worker", p.oldVersion, "bin", executableName())
		if got, err := sha256File(old); err != nil || !strings.EqualFold(got, p.oldBinSHA) {
			problems = append(problems, "old "+p.host+" cache binary SHA readback failed")
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("plugin cache rollback: %s", strings.Join(problems, "; "))
	}
	return nil
}
