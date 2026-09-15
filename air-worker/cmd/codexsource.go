package main

import (
	"os"
	"path/filepath"
	"strings"
)

// Codex хранит registry иначе Claude: marketplace/plugin state в config.toml,
// а установленные файлы — в <CODEX_HOME>/plugins/cache/<marketplace>/<plugin>/<version>.
func codexConfigDir() string {
	if v := strings.TrimSpace(os.Getenv("CODEX_HOME")); v != "" {
		return v
	}
	up := os.Getenv("USERPROFILE")
	if up == "" {
		up = os.Getenv("HOME")
	}
	return filepath.Join(up, ".codex")
}

type codexMarketplace struct {
	SourceType string
	Source     string
}

func unquoteTomlValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return strings.ReplaceAll(v[1:len(v)-1], `\"`, `"`)
	}
	return v
}

func readCodexConfig(configDir string) (mp codexMarketplace, mpOK, pluginEnabled bool) {
	data, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	if err != nil {
		return codexMarketplace{}, false, false
	}
	section := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "#", 2)[0])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := unquoteTomlValue(parts[1])
		switch section {
		case "marketplaces.air-plugins", `marketplaces."air-plugins"`:
			mpOK = true
			switch key {
			case "source_type":
				mp.SourceType = val
			case "source":
				mp.Source = val
			}
		case `plugins."air-worker@air-plugins"`:
			if key == "enabled" {
				pluginEnabled = strings.EqualFold(strings.TrimSpace(val), "true")
			}
		}
	}
	return mp, mpOK, pluginEnabled
}

func normalizeRepoSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimPrefix(s, "https://github.com/")
	s = strings.TrimPrefix(s, "http://github.com/")
	s = strings.TrimPrefix(s, "git@github.com:")
	return s
}

func codexMarketplaceIsGitHub(mp codexMarketplace) bool {
	return strings.EqualFold(strings.TrimSpace(mp.SourceType), "git") &&
		normalizeRepoSource(mp.Source) == strings.ToLower(canonicalRepo)
}

func binaryInCanonicalCodexCache(self, configDir string) bool {
	if strings.TrimSpace(self) == "" || strings.TrimSpace(configDir) == "" {
		return false
	}
	binDir := filepath.Dir(self)
	if !strings.EqualFold(filepath.Base(binDir), "bin") {
		return false
	}
	verDir := filepath.Dir(binDir)
	pluginDir := filepath.Dir(verDir)
	cacheRoot := filepath.Join(configDir, "plugins", "cache", canonicalMarketplace, "air-worker")
	return samePath(pluginDir, cacheRoot)
}

func codexMigrateHint() string {
	return "codex plugin marketplace remove air-plugins → " +
		"codex plugin marketplace add ruhorh66-rgb/air-plugins → " +
		"codex plugin add air-worker@air-plugins"
}
func readCodexInstalledPlugin(configDir string) (installedRec, bool) {
	_, _, enabled := readCodexConfig(configDir)
	if !enabled {
		return installedRec{}, false
	}
	root := filepath.Join(configDir, "plugins", "cache", canonicalMarketplace, "air-worker")
	entries, err := os.ReadDir(root)
	if err != nil {
		return installedRec{}, false
	}
	best := ""
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		ver := e.Name()
		bin := filepath.Join(root, ver, "bin", "air-worker.exe")
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		if best == "" || compareVersions(ver, best) > 0 {
			best = ver
		}
	}
	if best == "" {
		return installedRec{}, false
	}
	return installedRec{Scope: "codex", InstallPath: filepath.Join(root, best), Version: best}, true
}
func describeCodexMarketplace(ok bool, mp codexMarketplace) string {
	if !ok {
		return canonicalMarketplace + " не зарегистрирован"
	}
	if codexMarketplaceIsGitHub(mp) {
		return "github " + canonicalRepo
	}
	if strings.TrimSpace(mp.Source) != "" {
		return strings.TrimSpace(mp.SourceType) + " " + strings.TrimSpace(mp.Source)
	}
	return strings.TrimSpace(mp.SourceType)
}

func codexSourceShort(mp codexMarketplace) string {
	if strings.TrimSpace(mp.Source) != "" {
		return strings.TrimSpace(mp.SourceType) + " " + strings.TrimSpace(mp.Source)
	}
	return strings.TrimSpace(mp.SourceType)
}
