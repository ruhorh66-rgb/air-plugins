package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func planHeaderSelfcheckFixture(t *testing.T) (selfcheckReport, string) {
	t.Helper()
	root := t.TempDir()
	expectedLive := filepath.Join(root, "local", "air-worker", "bin", executableName())
	version := "0.11.8"
	sha := strings.Repeat("a", 64)
	revision := strings.Repeat("b", 40)
	unmodified := false
	report := selfcheckReport{
		Schema: selfcheckSchemaVersion,
		Live: distributionBinaryIdentity{
			Path: expectedLive, Version: version, Revision: revision,
			SHA256: sha, Readable: true, Executable: true, Modified: &unmodified,
		},
	}
	for _, host := range []string{"claude", "codex"} {
		config := filepath.Join(root, host+"-home")
		cacheRoot := filepath.Join(config, "plugins", "cache", "air-plugins", "air-worker", version)
		report.Profiles = append(report.Profiles, distributionProfile{
			Host: host, ConfigDir: config, Active: true, HasState: true,
			Canonical: true, PluginEnabled: true, InstallPath: cacheRoot,
			Version: version, Revision: revision,
			PayloadSHA256: strings.Repeat("d", 64),
			CacheBinary: distributionBinaryIdentity{
				Path:    filepath.Join(cacheRoot, "bin", executableName()),
				Version: version, Revision: revision, SHA256: sha,
				Readable: true, Executable: true, Modified: &unmodified,
			},
		})
	}
	return report, expectedLive
}

func TestPlanHeaderAttestInstalledSelfcheck(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*selfcheckReport)
		accept bool
	}{
		{"valid-registered-profiles", nil, true},
		{"missing-report-schema", func(r *selfcheckReport) { r.Schema = "" }, false},
		{"not-proven", func(r *selfcheckReport) { r.NotProven = []string{"missing-cache"} }, false},
		{"violation", func(r *selfcheckReport) { r.Violations = []string{"tampered-live"} }, false},
		{"unreadable-live", func(r *selfcheckReport) { r.Live.Readable = false }, false},
		{"unexecutable-live", func(r *selfcheckReport) { r.Live.Executable = false }, false},
		{"missing-live-sha", func(r *selfcheckReport) { r.Live.SHA256 = "" }, false},
		{"missing-live-revision", func(r *selfcheckReport) { r.Live.Revision = "" }, false},
		{"nonsemver-live", func(r *selfcheckReport) { r.Live.Version = "0.11.8-unapproved" }, false},
		{"modified-live", func(r *selfcheckReport) { v := true; r.Live.Modified = &v }, false},
		{"unknown-live-build-modified", func(r *selfcheckReport) { r.Live.Modified = nil }, false},
		{"no-installed-plugin", func(r *selfcheckReport) { r.Profiles = nil }, false},
		{"noncanonical-host", func(r *selfcheckReport) { r.Profiles[0].Canonical = false }, false},
		{"disabled-host", func(r *selfcheckReport) { r.Profiles[0].PluginEnabled = false }, false},
		{"cache-path-outside-registry", func(r *selfcheckReport) {
			r.Profiles[0].InstallPath = filepath.Join(r.Profiles[0].ConfigDir, "unapproved")
		}, false},
		{"cache-binary-redirect", func(r *selfcheckReport) {
			r.Profiles[0].CacheBinary.Path = filepath.Join(r.Profiles[0].ConfigDir, "unapproved", executableName())
		}, false},
		{"cache-version-mismatch", func(r *selfcheckReport) { r.Profiles[1].Version = "0.11.9" }, false},
		{"cache-revision-mismatch", func(r *selfcheckReport) { r.Profiles[0].Revision = strings.Repeat("c", 40) }, false},
		{"cache-sha-mismatch", func(r *selfcheckReport) { r.Profiles[0].CacheBinary.SHA256 = strings.Repeat("c", 64) }, false},
		{"cache-binary-unreadable", func(r *selfcheckReport) { r.Profiles[0].CacheBinary.Readable = false }, false},
		{"cache-binary-not-executable", func(r *selfcheckReport) { r.Profiles[0].CacheBinary.Executable = false }, false},
		{"cache-binary-modified", func(r *selfcheckReport) { v := true; r.Profiles[0].CacheBinary.Modified = &v }, false},
		{"cache-payload-missing", func(r *selfcheckReport) { r.Profiles[1].PayloadSHA256 = "" }, false},
		{"stale-cache-binary-version", func(r *selfcheckReport) { r.Profiles[1].CacheBinary.Version = "0.11.7" }, false},
		{"foreign-active-state", func(r *selfcheckReport) { r.Profiles[0].HasState = false }, false},
		{"extra-duplicate-active-claude", func(r *selfcheckReport) { r.Profiles = append(r.Profiles, r.Profiles[0]) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, livePath := planHeaderSelfcheckFixture(t)
			if tc.mutate != nil {
				tc.mutate(&report)
			}
			facts, err := planHeaderAttestInstalledSelfcheck(report, livePath)
			if (err == nil) != tc.accept {
				t.Fatalf("unexpected installed identity acceptance: want=%t facts=%+v err=%v", tc.accept, facts, err)
			}
			if tc.accept {
				if facts.Version != "0.11.8" || facts.SHA256 != strings.Repeat("a", 64) ||
					facts.Revision != strings.Repeat("b", 40) {
					t.Fatalf("attested wrong installed facts: %+v", facts)
				}
			}
		})
	}
	t.Run("wrong-installed-path", func(t *testing.T) {
		report, expectedLive := planHeaderSelfcheckFixture(t)
		if _, err := planHeaderAttestInstalledSelfcheck(report, expectedLive+"-other"); err == nil {
			t.Fatal("caller-controlled installed path accepted")
		}
	})
}

func TestPlanHeaderRegisteredCacheBinaryProvenance(t *testing.T) {
	root := t.TempDir()
	version := "0.11.8"
	cacheRoot := filepath.Join(root, "plugins", "cache", "air-plugins", "air-worker", version)
	dir := filepath.Join(cacheRoot, "bin")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte("fixture binary bytes from canonical cache, no execution")
	binary := filepath.Join(dir, executableName())
	if err := os.WriteFile(binary, content, 0600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(content)
	sha := hex.EncodeToString(digest[:])
	if err := planHeaderCheckRegisteredCache(root, installedRec{
		Version: version, InstallPath: cacheRoot,
	}, sha); err != nil {
		t.Fatalf("valid isolated cache was rejected: %v", err)
	}
	if err := planHeaderCheckRegisteredCache(root, installedRec{
		Version: version, InstallPath: cacheRoot,
	}, strings.Repeat("0", 64)); err == nil {
		t.Fatal("mismatched live executable accepted as canonical cache")
	}
	if err := planHeaderCheckRegisteredCache(root, installedRec{
		Version: version, InstallPath: filepath.Join(root, "foreign"),
	}, sha); err == nil {
		t.Fatal("cache outside canonical marketplace accepted")
	}
	if err := planHeaderCheckRegisteredCache(root, installedRec{
		Version: "0.11.9", InstallPath: cacheRoot,
	}, sha); err == nil {
		t.Fatal("registered version mismatch accepted")
	}
	if err := os.Truncate(binary, planHeaderInstalledExeLimit+1); err != nil {
		t.Fatal(err)
	}
	if _, err := planHeaderReadBoundedExeSHA(binary); err == nil {
		t.Fatal("oversized executable accepted for preview")
	}
}

func TestPlanHeaderInstalledSourceLiveOptIn(t *testing.T) {
	if os.Getenv("AIRWORKER_DEV136_LIVE_IDENTITY_TEST") != "1" {
		t.Skip("live installed identity is a separate explicit read-only acceptance")
	}
	facts, err := planHeaderReadNativeInstalledIdentity()
	if err != nil {
		t.Fatalf("registered installed GitHub plugin identity not proven: %v", err)
	}
	if facts.Version != "0.11.8" ||
		!planHeaderFileSHARegex.MatchString(facts.SHA256) ||
		!planHeaderGitSHARegex.MatchString(facts.Revision) {
		t.Fatalf("host reported unexpected installed identity: %+v", facts)
	}
	t.Logf("native installed AirWorker validated: version=%s sha=%s revision=%s",
		facts.Version, facts.SHA256, facts.Revision)
}
