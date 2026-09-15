package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeCodexConfig(t *testing.T, cfg, sourceType, source string, enabled bool) {
	t.Helper()
	body := `[plugins."air-worker@air-plugins"]
enabled = ` + map[bool]string{true: "true", false: "false"}[enabled] + `

[marketplaces.air-plugins]
source_type = "` + sourceType + `"
source = "` + source + `"
ref = "main"
`
	mustWrite(t, filepath.Join(cfg, "config.toml"), body)
}

func codexCacheSelf(cfg, ver string) string {
	return filepath.Join(cfg, "plugins", "cache", "air-plugins", "air-worker", ver, "bin", "air-worker.exe")
}

func TestCodexGitHubCacheAllowed(t *testing.T) {
	cfg := t.TempDir()
	writeCodexConfig(t, cfg, "git", "https://github.com/ruhorh66-rgb/air-plugins.git", true)
	g := decideInstallGuard(installGuard{
		Self: codexCacheSelf(cfg, "0.10.2"), CodexDir: cfg, SrcVersion: "0.10.2",
	})
	if !g.Allow {
		t.Fatalf("Codex GitHub cache must be allowed: %v", g.Reasons)
	}
}

func TestCodexWrongMarketplaceDenied(t *testing.T) {
	cfg := t.TempDir()
	writeCodexConfig(t, cfg, "git", "https://github.com/example/wrong.git", true)
	g := decideInstallGuard(installGuard{
		Self: codexCacheSelf(cfg, "0.10.2"), CodexDir: cfg, SrcVersion: "0.10.2",
	})
	if g.Allow || g.Code != 2 {
		t.Fatalf("wrong Codex marketplace must fail with code 2: allow=%v code=%d", g.Allow, g.Code)
	}
	if !containsSub(g.Reasons, "Codex marketplace") {
		t.Fatalf("failure must name Codex marketplace: %v", g.Reasons)
	}
}

func TestCodexOnlyStatusAcceptsMatchingSHA(t *testing.T) {
	cfg := t.TempDir()
	writeCodexConfig(t, cfg, "git", "https://github.com/ruhorh66-rgb/air-plugins.git", true)
	content := []byte("same codex cache and installed binary")
	cacheBin := codexCacheSelf(cfg, "0.10.2")
	if err := os.MkdirAll(filepath.Dir(cacheBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cacheBin, content, 0o644); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(t.TempDir(), "air-worker", "bin", "air-worker.exe")
	if err := os.MkdirAll(filepath.Dir(installed), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, content, 0o644); err != nil {
		t.Fatal(err)
	}
	lines, violations := installSourceStatusLinesForHosts("", cfg, installed, installed)
	if len(violations) != 0 {
		t.Fatalf("Codex-only GitHub status must be clean: %v", violations)
	}
	if !containsSub(lines, "Маркетплейс Codex") || !containsSub(lines, "SHA-256 совпадает") {
		t.Fatalf("status must prove Codex marketplace and SHA: %v", lines)
	}
}

func TestCodexInstalledPluginUsesNewestCacheVersion(t *testing.T) {
	cfg := t.TempDir()
	writeCodexConfig(t, cfg, "git", "ruhorh66-rgb/air-plugins", true)
	for _, ver := range []string{"0.10.1", "0.10.2"} {
		bin := codexCacheSelf(cfg, ver)
		if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(bin, []byte(ver), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	rec, ok := readCodexInstalledPlugin(cfg)
	if !ok || rec.Version != "0.10.2" {
		t.Fatalf("newest Codex cache version not selected: ok=%v rec=%+v", ok, rec)
	}
}
