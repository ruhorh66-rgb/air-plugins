package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const planHeaderInstalledExeLimit = 64 * 1024 * 1024

// planHeaderReadBoundedExeSHA hashes an already installed or cache copy
// without running it. A successful hash is only a freshness/identity
// predicate; it is not a signature or permission for PLAN mutation.
func planHeaderReadBoundedExeSHA(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("installed executable path not absolute")
	}
	if err := planHeaderValidateAncestors(filepath.Dir(path)); err != nil {
		return "", err
	}
	if err := planHeaderRejectOSReparse(path); err != nil {
		return "", err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 1 ||
		before.Size() > planHeaderInstalledExeLimit {
		return "", errors.New("installed executable not a bounded regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	opened, err := f.Stat()
	if err != nil || !os.SameFile(before, opened) ||
		opened.Size() != before.Size() ||
		!opened.ModTime().Equal(before.ModTime()) {
		_ = f.Close()
		return "", errors.New("installed executable changed while opening")
	}
	// Reuse the Windows OS-handle physical link/reparse check already
	// exercised by DEV136. Other platforms remain source-only.
	if err := planHeaderVerifyReleaseHandle(f); err != nil {
		_ = f.Close()
		return "", err
	}
	h := sha256.New()
	size, readErr := io.Copy(h, io.LimitReader(f, planHeaderInstalledExeLimit+1))
	after, statErr := f.Stat()
	handleErr := planHeaderVerifyReleaseHandle(f)
	closeErr := f.Close()
	if readErr != nil || statErr != nil || handleErr != nil || closeErr != nil ||
		size != before.Size() || size > planHeaderInstalledExeLimit ||
		!os.SameFile(opened, after) || opened.Size() != after.Size() ||
		!opened.ModTime().Equal(after.ModTime()) {
		return "", errors.New("installed executable changed during bounded read")
	}
	if err := planHeaderRejectOSReparse(path); err != nil {
		return "", err
	}
	latest, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, latest) || latest.Size() != after.Size() ||
		!latest.ModTime().Equal(after.ModTime()) {
		return "", errors.New("installed executable path changed during read")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func planHeaderCheckRegisteredCache(configRoot string, rec installedRec, liveSHA string) error {
	if !filepath.IsAbs(configRoot) || !filepath.IsAbs(rec.InstallPath) ||
		!planHeaderStableVersion.MatchString(rec.Version) {
		return errors.New("registered marketplace cache lacks trusted absolute identity")
	}
	expectedRoot := filepath.Join(configRoot, "plugins", "cache",
		canonicalMarketplace, "air-worker", rec.Version)
	if !strings.EqualFold(filepath.Clean(rec.InstallPath), filepath.Clean(expectedRoot)) {
		return errors.New("installed marketplace cache path is redirected")
	}
	cacheBin := filepath.Join(expectedRoot, "bin", executableName())
	sha, err := planHeaderReadBoundedExeSHA(cacheBin)
	if err != nil {
		return fmt.Errorf("marketplace cache binary not verified: %w", err)
	}
	if !strings.EqualFold(sha, liveSHA) {
		return errors.New("installed executable SHA differs from canonical marketplace cache")
	}
	return nil
}

// Before calling native selfcheck, verify every registration that it could
// execute for 'version'. The already-installed and cache binaries must be
// byte-identical and live in canonical GitHub marketplace cache locations.
// This prevents a forged installed_plugins.json from using DEV136 preview
// as an arbitrary executable launcher. Existing selfcheck remains the
// authoritative native distribution identity engine for factual readback.
func planHeaderPreflightSelfcheckExecutables(livePath, claudeRoot, codexRoot string) error {
	liveSHA, err := planHeaderReadBoundedExeSHA(livePath)
	if err != nil {
		return err
	}
	count := 0
	for _, root := range uniqueConfigRoots(claudeRoot, claudeDefaultConfigDir()) {
		rec, ok := readInstalledPlugin(root, canonicalPluginKey)
		if !ok {
			continue
		}
		mp, mpOK := readMarketplace(root, canonicalMarketplace)
		if !mpOK || !marketplaceIsGitHub(mp) {
			return errors.New("Claude installed marketplace not canonical GitHub")
		}
		if err := planHeaderCheckRegisteredCache(root, rec, liveSHA); err != nil {
			return err
		}
		count++
	}
	for _, root := range uniqueConfigRoots(codexRoot, codexDefaultConfigDir()) {
		rec, ok := readCodexInstalledPlugin(root)
		if !ok {
			continue
		}
		mp, mpOK, enabled := readCodexConfig(root)
		if !mpOK || !enabled || !codexMarketplaceIsGitHub(mp) {
			return errors.New("Codex installed marketplace not canonical GitHub")
		}
		if err := planHeaderCheckRegisteredCache(root, rec, liveSHA); err != nil {
			return err
		}
		count++
	}
	if count == 0 {
		return errors.New("no canonical GitHub marketplace binary to validate")
	}
	return nil
}

// planHeaderReadNativeInstalledIdentity is a source-only read of the
// already-installed product, never a request to install another version.
// The installed baseline can remain 0.11.8 while active planned release
// is 0.11.9. No caller-supplied JSON or override of the live path is trusted.
func planHeaderReadNativeInstalledIdentity() (planHeaderInstalledFacts, error) {
	var empty planHeaderInstalledFacts
	local := strings.TrimSpace(os.Getenv("LOCALAPPDATA"))
	if !filepath.IsAbs(local) {
		return empty, errors.New("Windows installed product root unavailable")
	}
	expected := filepath.Join(local, "air-worker", "bin", executableName())
	claudeRoot, codexRoot := claudeConfigDir(), codexConfigDir()
	if err := planHeaderPreflightSelfcheckExecutables(expected, claudeRoot, codexRoot); err != nil {
		return empty, err
	}
	report := buildSelfcheckReport(expected, claudeRoot, codexRoot)
	facts, err := planHeaderAttestInstalledSelfcheck(report, expected)
	if err != nil {
		return empty, err
	}
	// Selfcheck runs subprocess version checks. Re-read every binary after
	// the report: any in-flight cache or installed replacement is a conflict.
	if err := planHeaderPreflightSelfcheckExecutables(expected, claudeRoot, codexRoot); err != nil {
		return empty, err
	}
	if got, err := planHeaderReadBoundedExeSHA(expected); err != nil ||
		!strings.EqualFold(got, facts.SHA256) {
		return empty, errors.New("installed binary changed after native selfcheck")
	}
	return facts, nil
}
