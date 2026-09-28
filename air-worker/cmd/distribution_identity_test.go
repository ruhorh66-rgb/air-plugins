package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeClaudeDistributionFixture(t *testing.T, configDir, version string) string {
	t.Helper()
	cache := filepath.Join(configDir, "plugins", "cache", canonicalMarketplace, "air-worker", version)
	if err := os.MkdirAll(filepath.Join(cache, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	marketplace, _ := json.Marshal(map[string]any{
		"air-plugins": map[string]any{
			"source":          map[string]any{"source": "github", "repo": canonicalRepo},
			"installLocation": "x",
		},
	})
	writeTree(t, filepath.Join(configDir, "plugins", "known_marketplaces.json"), string(marketplace))
	installed, _ := json.Marshal(map[string]any{
		"version": 2,
		"plugins": map[string]any{
			canonicalPluginKey: []map[string]any{{
				"scope": "user", "installPath": cache, "version": version,
			}},
		},
	})
	writeTree(t, filepath.Join(configDir, "plugins", "installed_plugins.json"), string(installed))
	writeTree(t, filepath.Join(cache, "bin", executableName()), "fixture-binary")
	manifest, _ := json.Marshal(map[string]any{"name": "air-worker", "version": version})
	writeTree(t, filepath.Join(cache, ".claude-plugin", "plugin.json"), string(manifest))
	return cache
}

func writeCodexDistributionFixture(t *testing.T, configDir, version string) string {
	t.Helper()
	cache := filepath.Join(configDir, "plugins", "cache", canonicalMarketplace, "air-worker", version)
	if err := os.MkdirAll(filepath.Join(cache, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeTree(t, filepath.Join(configDir, "config.toml"),
		"[marketplaces.air-plugins]\nsource_type = \"git\"\nsource = \"https://github.com/ruhorh66-rgb/air-plugins\"\n\n"+
			"[plugins.\"air-worker@air-plugins\"]\nenabled = true\n")
	writeTree(t, filepath.Join(cache, "bin", executableName()), "fixture-binary")
	writeTree(t, filepath.Join(cache, ".claude-plugin", "plugin.json"), `{"name":"air-worker","version":"`+version+`"}`)
	return cache
}

func TestPayloadSnapshotIsDeterministicAndContentBound(t *testing.T) {
	root := t.TempDir()
	writeTree(t, filepath.Join(root, "a.txt"), "A")
	writeTree(t, filepath.Join(root, "nested", "b.txt"), "B")
	first, count, err := payloadSnapshot(root)
	if err != nil || count != 2 || first == "" {
		t.Fatalf("first snapshot=%q count=%d err=%v", first, count, err)
	}
	second, count2, err := payloadSnapshot(root)
	if err != nil || count2 != count || second != first {
		t.Fatalf("snapshot not deterministic: first=%q second=%q count=%d/%d err=%v", first, second, count, count2, err)
	}
	writeTree(t, filepath.Join(root, ".in_use", "1234"), "host runtime marker")
	withMarker, markerCount, err := payloadSnapshot(root)
	if err != nil || withMarker != first || markerCount != count {
		t.Fatalf("host .in_use metadata changed payload identity: first=%q marker=%q count=%d/%d err=%v", first, withMarker, count, markerCount, err)
	}
	writeTree(t, filepath.Join(root, "nested", "b.txt"), "B changed")
	third, _, err := payloadSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("payload content mutation did not change snapshot")
	}
}

func TestCompareDistributionIdentityNamesSameVersionRevisionMismatch(t *testing.T) {
	modified := false
	live := distributionBinaryIdentity{
		Path: "live", Version: "0.10.13", Revision: strings.Repeat("a", 40),
		SHA256: "live-sha", Readable: true, Executable: true, Modified: &modified,
	}
	cacheID := distributionBinaryIdentity{
		Path: "cache", Version: "0.10.13", Revision: strings.Repeat("b", 40),
		SHA256: "cache-sha", Readable: true, Executable: true, Modified: &modified,
	}
	profile := distributionProfile{
		Host: "claude", ConfigDir: "cfg", HasState: true, Canonical: true, PluginEnabled: true,
		InstallPath: "cache-root", Version: "0.10.13", Revision: cacheID.Revision,
		PayloadSHA256: "payload", CacheBinary: cacheID,
	}
	_, violations, notProven := compareDistributionIdentity(live, []distributionProfile{profile})
	if len(notProven) != 0 {
		t.Fatalf("unexpected not-proven: %v", notProven)
	}
	text := strings.Join(violations, "\n")
	if !strings.Contains(text, "SAME-VERSION REVISION MISMATCH") || !strings.Contains(text, "SAME-VERSION SHA MISMATCH") {
		t.Fatalf("mismatch not named: %v", violations)
	}
}

func TestCompareDistributionIdentityRejectsDifferentVersions(t *testing.T) {
	modified := false
	live := distributionBinaryIdentity{
		Path: "live", Version: "0.10.14", Revision: strings.Repeat("a", 40),
		SHA256: "live-sha", Readable: true, Executable: true, Modified: &modified,
	}
	cacheID := distributionBinaryIdentity{
		Path: "cache", Version: "0.10.13", Revision: strings.Repeat("b", 40),
		SHA256: "cache-sha", Readable: true, Executable: true, Modified: &modified,
	}
	profile := distributionProfile{
		Host: "claude", ConfigDir: "cfg", HasState: true, Canonical: true, PluginEnabled: true,
		InstallPath: "cache-root", Version: "0.10.13", Revision: cacheID.Revision,
		PayloadSHA256: "payload", CacheBinary: cacheID,
	}
	_, violations, notProven := compareDistributionIdentity(live, []distributionProfile{profile})
	if len(notProven) != 0 {
		t.Fatalf("unexpected not-proven: %v", notProven)
	}
	if !strings.Contains(strings.Join(violations, "\n"), "VERSION MISMATCH") {
		t.Fatalf("different versions must be a violation: %v", violations)
	}
}

func TestBuildSelfcheckWorksWithoutSourceCheckoutAndWarnsMultipleProfiles(t *testing.T) {
	user := t.TempDir()
	t.Setenv("USERPROFILE", user)
	t.Setenv("HOME", user)
	defaultClaude := filepath.Join(user, ".claude")
	activeClaude := filepath.Join(user, "claude-alt")
	activeCodex := filepath.Join(user, "codex-active")
	writeClaudeDistributionFixture(t, defaultClaude, "0.10.13")
	writeClaudeDistributionFixture(t, activeClaude, "0.10.13")
	writeCodexDistributionFixture(t, activeCodex, "0.10.13")

	old := readDistributionBinaryIdentity
	defer func() { readDistributionBinaryIdentity = old }()
	modified := false
	readDistributionBinaryIdentity = func(path string) distributionBinaryIdentity {
		return distributionBinaryIdentity{
			Path: path, Version: "0.10.13", Revision: strings.Repeat("a", 40),
			SHA256: "same-sha", Readable: true, Executable: true, Modified: &modified,
		}
	}

	report := buildSelfcheckReport(filepath.Join(user, "live", executableName()), activeClaude, activeCodex)
	if code := selfcheckExit(report); code != 0 {
		t.Fatalf("selfcheck code=%d violations=%v notProven=%v warnings=%v", code, report.Violations, report.NotProven, report.Warnings)
	}
	if !strings.Contains(strings.Join(report.Warnings, "\n"), "MULTIPLE PROFILE AMBIGUITY") {
		t.Fatalf("multiple profile ambiguity not named: %v", report.Warnings)
	}
	foundActiveClaude, foundActiveCodex := false, false
	for _, p := range report.Profiles {
		if p.Host == "claude" && p.Active && samePath(p.ConfigDir, activeClaude) {
			foundActiveClaude = true
		}
		if p.Host == "codex" && p.Active && samePath(p.ConfigDir, activeCodex) {
			foundActiveCodex = true
		}
	}
	if !foundActiveClaude || !foundActiveCodex {
		t.Fatalf("active config dirs missing: %#v", report.Profiles)
	}
}

func TestSelfcheckSameVersionRevisionMismatchIsNonzero(t *testing.T) {
	user := t.TempDir()
	t.Setenv("USERPROFILE", user)
	t.Setenv("HOME", user)
	claude := filepath.Join(user, ".claude")
	writeClaudeDistributionFixture(t, claude, "0.10.13")

	old := readDistributionBinaryIdentity
	defer func() { readDistributionBinaryIdentity = old }()
	modified := false
	readDistributionBinaryIdentity = func(path string) distributionBinaryIdentity {
		rev := strings.Repeat("b", 40)
		sha := "cache-sha"
		if strings.Contains(path, "live") {
			rev = strings.Repeat("a", 40)
			sha = "live-sha"
		}
		return distributionBinaryIdentity{
			Path: path, Version: "0.10.13", Revision: rev, SHA256: sha,
			Readable: true, Executable: true, Modified: &modified,
		}
	}
	report := buildSelfcheckReport(filepath.Join(user, "live", executableName()), claude, filepath.Join(user, ".codex"))
	if code := selfcheckExit(report); code != 1 {
		t.Fatalf("same-version mismatch must be violation code 1, got %d: %#v", code, report)
	}
	if !strings.Contains(strings.Join(report.Violations, "\n"), "SAME-VERSION REVISION MISMATCH") {
		t.Fatalf("revision mismatch not named: %v", report.Violations)
	}
}

func TestSelfcheckMissingRevisionIsNotProven(t *testing.T) {
	modified := false
	live := distributionBinaryIdentity{
		Path: "live", Version: "0.10.13", SHA256: "sha", Readable: true, Executable: true, Modified: &modified,
	}
	_, violations, notProven := compareDistributionIdentity(live, nil)
	if len(violations) != 0 || !strings.Contains(strings.Join(notProven, "\n"), "vcs.revision") {
		t.Fatalf("missing revision must be NOT_PROVEN: violations=%v notProven=%v", violations, notProven)
	}
}

func TestInstallStatusUsesSelfcheckSummary(t *testing.T) {
	user := t.TempDir()
	t.Setenv("USERPROFILE", user)
	t.Setenv("HOME", user)
	claude := filepath.Join(user, ".claude")
	writeClaudeDistributionFixture(t, claude, "0.10.13")

	old := readDistributionBinaryIdentity
	defer func() { readDistributionBinaryIdentity = old }()
	modified := false
	readDistributionBinaryIdentity = func(path string) distributionBinaryIdentity {
		return distributionBinaryIdentity{
			Path: path, Version: "0.10.13", Revision: strings.Repeat("a", 40),
			SHA256: "same-sha", Readable: true, Executable: true, Modified: &modified,
		}
	}
	lines := installIdentityStatusLines(filepath.Join(user, "live", executableName()), claude, filepath.Join(user, ".codex"))
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "Live identity:") || !strings.Contains(text, "Claude config:") || !strings.Contains(text, "Selfcheck: PASS") {
		t.Fatalf("install identity summary does not reuse selfcheck facts:\n%s", text)
	}
}

func TestSelfcheckSummaryUsesStableEnglishNoStateText(t *testing.T) {
	report := selfcheckReport{
		Live:     distributionBinaryIdentity{Path: "live", Version: "0.10.13", Revision: "rev", SHA256: "sha"},
		Profiles: []distributionProfile{{Host: "codex", ConfigDir: "cfg", Active: false, HasState: false}},
	}
	text := strings.Join(selfcheckSummaryLines(report), "\n")
	if !strings.Contains(text, "no air-worker state") {
		t.Fatalf("no-state summary is not stable English text: %s", text)
	}
	if strings.Contains(text, "нет air-worker state") {
		t.Fatalf("legacy mixed-language text leaked: %s", text)
	}
}
