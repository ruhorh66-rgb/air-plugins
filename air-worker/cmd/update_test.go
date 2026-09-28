package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func signedUpdateManifestForTest(t *testing.T, versionText, channel string) (updateManifest, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	v := strings.TrimPrefix(versionText, "v")
	m := updateManifest{
		Schema:        updateSchema,
		Channel:       channel,
		Version:       v,
		PublishedAt:   "2026-09-28T08:00:00Z",
		VCSRevision:   strings.Repeat("a", 40),
		PayloadSHA256: strings.Repeat("b", 64),
		CLI: updateArtifact{
			Name:   "air-worker-" + v + "-windows-x64.exe",
			URL:    "https://github.com/ruhorh66-rgb/air-plugins/releases/download/air-worker--v" + v + "/air-worker-" + v + "-windows-x64.exe",
			SHA256: strings.Repeat("c", 64),
			Size:   123,
		},
		Tray: updateArtifact{
			Name:   "air-worker-tray-" + v + "-windows-x64.exe",
			URL:    "https://github.com/ruhorh66-rgb/air-plugins/releases/download/air-worker--v" + v + "/air-worker-tray-" + v + "-windows-x64.exe",
			SHA256: strings.Repeat("d", 64),
			Size:   456,
		},
		SigningKeyID:    "test-key",
		ReleaseNotesURL: "https://github.com/ruhorh66-rgb/air-plugins/releases/tag/air-worker--v" + v,
	}
	m.ManifestSig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, updateSignatureMessage(m)))
	return m, base64.StdEncoding.EncodeToString(pub)
}

func TestVerifyUpdateManifestAcceptsSignedImmutableRelease(t *testing.T) {
	m, pub := signedUpdateManifestForTest(t, "0.10.15", "stable")
	available, err := verifyUpdateManifestWithKey(m, "0.10.14", "stable", pub, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if !available {
		t.Fatal("newer signed release was not offered")
	}
}

func TestVerifyUpdateManifestTamperFails(t *testing.T) {
	m, pub := signedUpdateManifestForTest(t, "0.10.15", "stable")
	m.CLI.SHA256 = strings.Repeat("e", 64)
	if _, err := verifyUpdateManifestWithKey(m, "0.10.14", "stable", pub, "test-key"); err == nil {
		t.Fatal("tampered manifest passed signature verification")
	}
}

func TestVerifyUpdateManifestNeverDowngrades(t *testing.T) {
	m, pub := signedUpdateManifestForTest(t, "0.10.13", "stable")
	available, err := verifyUpdateManifestWithKey(m, "0.10.14", "stable", pub, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	if available {
		t.Fatal("older signed release was offered as an update")
	}
}

func TestVerifyUpdateManifestChannelPolicy(t *testing.T) {
	m, pub := signedUpdateManifestForTest(t, "0.10.15-beta.1", "prerelease")
	if _, err := verifyUpdateManifestWithKey(m, "0.10.14", "stable", pub, "test-key"); err == nil {
		t.Fatal("stable channel accepted prerelease manifest")
	}
	available, err := verifyUpdateManifestWithKey(m, "0.10.14", "prerelease", pub, "test-key")
	if err != nil || !available {
		t.Fatalf("prerelease channel rejected signed beta: available=%v err=%v", available, err)
	}
}

func TestValidateUpdateArtifactRequiresImmutableGitHubTag(t *testing.T) {
	m, _ := signedUpdateManifestForTest(t, "0.10.15", "stable")
	bad := m.CLI
	bad.URL = "https://github.com/ruhorh66-rgb/air-plugins/releases/latest/download/" + bad.Name
	if err := validateUpdateArtifact(bad, bad.Name, m.Version); err == nil {
		t.Fatal("latest/download URL was accepted")
	}
	bad = m.CLI
	bad.URL = "https://example.com/" + bad.Name
	if err := validateUpdateArtifact(bad, bad.Name, m.Version); err == nil {
		t.Fatal("non-GitHub release URL was accepted")
	}
}

func TestPrereleaseChannelAlsoChecksStableFeed(t *testing.T) {
	got := manifestURLsForChannel("prerelease")
	if len(got) != 2 || got[0] != updateStableManifestURL || got[1] != updatePrereleaseManifestURL {
		t.Fatalf("prerelease feeds=%#v", got)
	}
}

func TestUpdateSemverNumericPrereleaseOrdering(t *testing.T) {
	a, err := parseUpdateSemVersion("0.11.0-beta.10")
	if err != nil {
		t.Fatal(err)
	}
	b, err := parseUpdateSemVersion("0.11.0-beta.4")
	if err != nil {
		t.Fatal(err)
	}
	if compareUpdateSemVersion(a, b) <= 0 {
		t.Fatal("beta.10 must be newer than beta.4")
	}
	stable, _ := parseUpdateSemVersion("0.11.0")
	if compareUpdateSemVersion(stable, a) <= 0 {
		t.Fatal("stable 0.11.0 must be newer than its prerelease")
	}
}

func TestUpdateCheckFresh(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	cfg := updateConfig{Schema: updateSchema, Channel: "stable", CheckIntervalHours: 6}
	fresh := updateState{
		Schema: updateSchema, CurrentVersion: version, Channel: "stable",
		Phase: "current", CheckedAt: now.Add(-5 * time.Hour).Format(time.RFC3339Nano),
	}
	if !updateCheckFresh(fresh, cfg, now) {
		t.Fatal("fresh successful check should skip network")
	}
	stale := fresh
	stale.CheckedAt = now.Add(-7 * time.Hour).Format(time.RFC3339Nano)
	if updateCheckFresh(stale, cfg, now) {
		t.Fatal("stale check should hit network")
	}
	failed := fresh
	failed.Phase = "error"
	if updateCheckFresh(failed, cfg, now) {
		t.Fatal("error state must retry")
	}
	wrongChannel := fresh
	wrongChannel.Channel = "prerelease"
	if updateCheckFresh(wrongChannel, cfg, now) {
		t.Fatal("channel change must force a check")
	}
	wrongVersion := fresh
	wrongVersion.CurrentVersion = "0.10.13"
	if updateCheckFresh(wrongVersion, cfg, now) {
		t.Fatal("binary version change must force a check")
	}
}
