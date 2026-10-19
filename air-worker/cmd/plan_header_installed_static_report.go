package main

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// This DEV136 preflight never calls a registered executable's "version"
// command. Native selfcheck normally invokes cache binaries to obtain their
// version; a source-only header preview must not create a new execution path
// for mutable registry/cache entries. Read Go VCS buildinfo + SHA only.
func planHeaderStaticBinaryIdentity(path, version string) (distributionBinaryIdentity, error) {
	raw, err := planHeaderReadBoundedExeSnapshot(path)
	if err != nil {
		return distributionBinaryIdentity{Path: filepath.Clean(path)}, err
	}
	// Parse and hash the SAME validated snapshot, not two path generations.
	return planHeaderStaticIdentityFromSnapshot(path, version, raw)
}

// From this boundary forward, only immutable bytes—not a pathname or OS
// executable—provide both the SHA and VCS metadata. A path replacement
// after snapshot acquisition cannot mix two different binary generations.
func planHeaderStaticIdentityFromSnapshot(path, version string, raw []byte) (distributionBinaryIdentity, error) {
	id := distributionBinaryIdentity{Path: filepath.Clean(path)}
	sumBytes := sha256.Sum256(raw)
	sum := hex.EncodeToString(sumBytes[:])
	info, err := buildinfo.Read(bytes.NewReader(raw))
	if err != nil {
		return id, fmt.Errorf("static installed build info unavailable: %w", err)
	}
	id.SHA256, id.Readable, id.Executable = sum, true, true
	id.Version = version // from canonical marketplace registration; NOT from executing binary
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			id.Revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			changed := strings.EqualFold(strings.TrimSpace(setting.Value), "true")
			id.Modified = &changed
		}
	}
	if id.Modified == nil || *id.Modified || id.Revision == "" {
		return id, errors.New("static installed binary revision/build cleanliness unavailable")
	}
	return id, nil
}

// Build the same distribution facts needed for installed SHA/revision/version
// consistency using existing marketplace readers and the native comparison
// engine. Unlike buildSelfcheckReport, it never calls binaryVersionPortable,
// exec.CommandContext, any plugin shell, or an untrusted cache executable.
// Curator skill health is outside this bounded DEV136 binary-only scope.
func planHeaderStaticInstalledReport(livePath, claudeActive, codexActive string) (selfcheckReport, error) {
	report := selfcheckReport{Schema: selfcheckSchemaVersion, At: time.Now().UTC().Format(time.RFC3339Nano),
		Warnings: []string{}, Violations: []string{}, NotProven: []string{}}
	live, err := planHeaderStaticBinaryIdentity(livePath, "")
	if err != nil {
		return report, err
	}
	version := ""
	appendProfile := func(host, root string, active bool, rec installedRec, canonical bool) error {
		if !canonical {
			return errors.New("registered marketplace is not canonical GitHub")
		}
		if version == "" {
			version = rec.Version
		} else if version != rec.Version {
			return errors.New("two registered marketplace versions differ")
		}
		if err := planHeaderCheckRegisteredCache(root, rec, live.SHA256); err != nil {
			return err
		}
		cachePath := filepath.Join(rec.InstallPath, "bin", executableName())
		cache, err := planHeaderStaticBinaryIdentity(cachePath, rec.Version)
		if err != nil {
			return err
		}
		payload, _, err := payloadSnapshot(rec.InstallPath)
		if err != nil {
			return fmt.Errorf("native marketplace payload not readable: %w", err)
		}
		report.Profiles = append(report.Profiles, distributionProfile{
			Host: host, ConfigDir: root, Active: active, HasState: true,
			Canonical: canonical, PluginEnabled: true, InstallPath: rec.InstallPath,
			Version: rec.Version, Revision: cache.Revision,
			PayloadSHA256: payload, CacheBinary: cache,
		})
		return nil
	}
	for _, root := range uniqueConfigRoots(claudeActive, claudeDefaultConfigDir()) {
		rec, ok := readInstalledPlugin(root, canonicalPluginKey)
		if !ok {
			if strings.EqualFold(filepath.Clean(root), filepath.Clean(claudeActive)) {
				return report, errors.New("active Claude installation missing from native registry")
			}
			continue
		}
		mp, mpOK := readMarketplace(root, canonicalMarketplace)
		if err := appendProfile("claude", root,
			strings.EqualFold(filepath.Clean(root), filepath.Clean(claudeActive)),
			rec, mpOK && marketplaceIsGitHub(mp)); err != nil {
			return report, err
		}
	}
	for _, root := range uniqueConfigRoots(codexActive, codexDefaultConfigDir()) {
		rec, ok := readCodexInstalledPlugin(root)
		if !ok {
			if strings.EqualFold(filepath.Clean(root), filepath.Clean(codexActive)) {
				return report, errors.New("active Codex installation missing from native cache")
			}
			continue
		}
		mp, mpOK, enabled := readCodexConfig(root)
		if err := appendProfile("codex", root,
			strings.EqualFold(filepath.Clean(root), filepath.Clean(codexActive)),
			rec, mpOK && enabled && codexMarketplaceIsGitHub(mp)); err != nil {
			return report, err
		}
	}
	if !planHeaderStableVersion.MatchString(version) {
		return report, errors.New("active registered release version not proven")
	}
	live.Version = version
	report.Live = live
	report.Warnings, report.Violations, report.NotProven = compareDistributionIdentity(report.Live, report.Profiles)
	return report, nil
}
