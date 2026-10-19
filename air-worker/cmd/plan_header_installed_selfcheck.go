package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// The DEV-136 read-only installed facts are separate from active RELEASE
// candidate version. A fresh installed 0.11.8 may legitimately be paired
// with source-only planned 0.11.9.
type planHeaderInstalledFacts struct {
	Version  string
	SHA256   string
	Revision string
}

// planHeaderAttestInstalledSelfcheck validates the existing native core
// selfcheck/v1 distribution snapshot, not a PLAN heading or caller-provided
// version string. The caller must obtain a FRESH report from the accepted
// native selfcheck logic for its pinned installed path; an arbitrary JSON
// document cannot grant mutation. This function is a read-only preflight and
// never authorizes writes to PLAN/N/LEARN.
func planHeaderAttestInstalledSelfcheck(report selfcheckReport, expectedLivePath string) (planHeaderInstalledFacts, error) {
	var facts planHeaderInstalledFacts
	reject := func(reason string) (planHeaderInstalledFacts, error) {
		return facts, errors.New("installed distribution attestation not proven: " + reason)
	}
	if report.Schema != selfcheckSchemaVersion || selfcheckExit(report) != 0 ||
		len(report.Warnings) > 0 {
		return reject("native selfcheck is not fully accepted")
	}
	live := report.Live
	if !filepath.IsAbs(expectedLivePath) || !filepath.IsAbs(live.Path) ||
		!strings.EqualFold(filepath.Clean(live.Path), filepath.Clean(expectedLivePath)) {
		return reject("installed binary path is not pinned to expected live path")
	}
	if !live.Readable || !live.Executable || live.Error != "" ||
		live.Modified == nil || *live.Modified ||
		!planHeaderStableVersion.MatchString(live.Version) ||
		!planHeaderFileSHARegex.MatchString(live.SHA256) ||
		!planHeaderGitSHARegex.MatchString(live.Revision) {
		return reject("installed binary identity, version, SHA or clean revision missing")
	}
	count := 0
	seenHosts := make(map[string]bool)
	for _, profile := range report.Profiles {
		if !profile.Active {
			if profile.HasState {
				return reject("inactive profile has an ambiguous installed registration")
			}
			continue
		}
		count++
		if profile.Host != "claude" && profile.Host != "codex" {
			return reject("unknown active marketplace host")
		}
		if seenHosts[profile.Host] {
			return reject("duplicate active marketplace host")
		}
		seenHosts[profile.Host] = true
		if !profile.HasState || !profile.Canonical || !profile.PluginEnabled ||
			profile.Error != "" || !filepath.IsAbs(profile.ConfigDir) ||
			!filepath.IsAbs(profile.InstallPath) {
			return reject("active profile is not a canonical enabled GitHub installation")
		}
		cacheRoot := filepath.Join(profile.ConfigDir, "plugins", "cache", "air-plugins", "air-worker", live.Version)
		cacheBin := filepath.Join(cacheRoot, "bin", executableName())
		if !strings.EqualFold(filepath.Clean(profile.InstallPath), cacheRoot) ||
			!filepath.IsAbs(profile.CacheBinary.Path) ||
			!strings.EqualFold(filepath.Clean(profile.CacheBinary.Path), cacheBin) {
			return reject("registered marketplace binary redirected outside expected cache")
		}
		if profile.Version != live.Version ||
			!strings.EqualFold(profile.Revision, live.Revision) ||
			!planHeaderFileSHARegex.MatchString(profile.PayloadSHA256) {
			return reject("active cache version, revision or payload attestation differs")
		}
		binary := profile.CacheBinary
		if binary.Error != "" || !binary.Readable || !binary.Executable ||
			binary.Modified == nil || *binary.Modified ||
			binary.Version != live.Version ||
			!strings.EqualFold(binary.Revision, live.Revision) ||
			!strings.EqualFold(binary.SHA256, live.SHA256) {
			return reject(fmt.Sprintf("%s cached binary differs from installed SHA/revision", profile.Host))
		}
	}
	if count == 0 {
		return reject("no active canonical marketplace registration")
	}
	return planHeaderInstalledFacts{Version: live.Version, SHA256: live.SHA256, Revision: live.Revision}, nil
}
