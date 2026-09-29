package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const ponytailSkillMaxBytes = 64 * 1024

type ponytailSkill struct {
	Version string
	Path    string
	Body    string
}

var loadPonytailSkill = readInstalledPonytailSkill

const ponytailVendorSource = "https://github.com/DietrichGebert/ponytail.git"

func ponytailPluginState(configDir string) (enabled bool, source string) {
	raw, err := os.ReadFile(filepath.Join(configDir, "config.toml"))
	if err != nil {
		return false, ""
	}
	section := ""
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
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
		key, value := strings.TrimSpace(parts[0]), unquoteTomlValue(parts[1])
		switch section {
		case `plugins."ponytail@ponytail"`:
			if key == "enabled" {
				enabled = strings.EqualFold(strings.TrimSpace(value), "true")
			}
		case "marketplaces.ponytail", `marketplaces."ponytail"`:
			if key == "source" {
				source = strings.TrimSpace(value)
			}
		}
	}
	return enabled, source
}

func semverParts(version string) [3]int {
	var out [3]int
	for i, part := range strings.SplitN(version, ".", 4) {
		if i >= len(out) {
			break
		}
		n, _ := strconv.Atoi(part)
		out[i] = n
	}
	return out
}

func semverLess(a, b string) bool {
	aa, bb := semverParts(a), semverParts(b)
	for i := range aa {
		if aa[i] != bb[i] {
			return aa[i] < bb[i]
		}
	}
	return a < b
}

func readInstalledPonytailSkill() (ponytailSkill, error) {
	configDir := codexConfigDir()
	enabled, source := ponytailPluginState(configDir)
	if !enabled {
		return ponytailSkill{}, fmt.Errorf("Codex plugin ponytail@ponytail is not enabled in %s", filepath.Join(configDir, "config.toml"))
	}
	if !strings.EqualFold(strings.TrimRight(source, "/"), strings.TrimRight(ponytailVendorSource, "/")) {
		return ponytailSkill{}, fmt.Errorf("Ponytail marketplace source %q is not the approved vendor source %q", source, ponytailVendorSource)
	}
	pattern := filepath.Join(configDir, "plugins", "cache", "ponytail", "ponytail", "*", "skills", "ponytail", "SKILL.md")
	paths, err := filepath.Glob(pattern)
	if err != nil {
		return ponytailSkill{}, fmt.Errorf("resolve Ponytail skill: %w", err)
	}
	if len(paths) == 0 {
		return ponytailSkill{}, fmt.Errorf("installed Ponytail skill not found under %s", pattern)
	}
	sort.Slice(paths, func(i, j int) bool {
		vi := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(paths[i]))))
		vj := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(paths[j]))))
		return semverLess(vi, vj)
	})
	path := paths[len(paths)-1]
	info, err := os.Stat(path)
	if err != nil {
		return ponytailSkill{}, err
	}
	if info.Size() <= 0 || info.Size() > ponytailSkillMaxBytes {
		return ponytailSkill{}, fmt.Errorf("Ponytail skill size %d outside 1..%d bytes", info.Size(), ponytailSkillMaxBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ponytailSkill{}, err
	}
	version := filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(path))))
	return ponytailSkill{Version: version, Path: path, Body: strings.TrimSpace(string(raw))}, nil
}

func requirePonytailSkill() error {
	_, err := loadPonytailSkill()
	return err
}

// ponytailPrompt activates the officially installed Ponytail plugin skill for a
// non-interactive Codex call by supplying the exact installed vendor SKILL.md.
// Do not emit an @ponytail command marker here: a live codex exec smoke proved that the
// marker can be consumed as a standalone skill command, leaving the following task unread.
func ponytailPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	skill, err := loadPonytailSkill()
	if err != nil {
		return "[PONYTAIL_SKILL_LOAD_FAILED: " + err.Error() + "]\n\nAIRWORKER TASK:\n" + prompt
	}
	return "PONYTAIL VENDOR SKILL · installed plugin ponytail@ponytail v" + skill.Version +
		" · ACTIVE MODE: full\n--- BEGIN VENDOR SKILL ---\n" + skill.Body +
		"\n--- END VENDOR SKILL ---\n\nAIRWORKER TASK:\n" + prompt
}
