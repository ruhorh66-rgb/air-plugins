package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type updateCacheSyncResult struct {
	Host      string `json:"host"`
	ConfigDir string `json:"config_dir"`
	Changed   bool   `json:"changed"`
	Output    string `json:"output,omitempty"`
}

func withUpdateEnv(base []string, key, value string) []string {
	prefix := strings.ToUpper(key) + "="
	out := make([]string, 0, len(base)+1)
	for _, item := range base {
		if strings.HasPrefix(strings.ToUpper(item), prefix) {
			continue
		}
		out = append(out, item)
	}
	return append(out, key+"="+value)
}

func runUpdateHostCommand(ctx context.Context, envKey, envValue, name string, args ...string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found: %w", name, err)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	profile := ""
	if envKey == "CLAUDE_CONFIG_DIR" {
		profile = envValue
	}
	cmd.Env, err = childEnvironment(name, os.Environ(), profile)
	if err != nil {
		return "", err
	}
	cmd.Env = withUpdateEnv(cmd.Env, envKey, envValue)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(decodeOutput(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
	}
	return text, nil
}

func knownUpdateProfiles() []distributionProfile {
	report := buildSelfcheckReport(defaultLiveBinaryPath(), claudeConfigDir(), codexConfigDir())
	var out []distributionProfile
	seen := map[string]bool{}
	for _, p := range report.Profiles {
		if !p.HasState || !p.PluginEnabled || strings.TrimSpace(p.InstallPath) == "" {
			continue
		}
		key := profileKey(p)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p)
	}
	return out
}

func verifyUpdateCachesForManifest(m updateManifest) error {
	var problems []string
	for _, p := range knownUpdateProfiles() {
		if !p.Canonical {
			problems = append(problems, fmt.Sprintf("%s %s marketplace is not canonical GitHub", p.Host, p.ConfigDir))
			continue
		}
		if p.Version != m.Version {
			problems = append(problems, fmt.Sprintf("%s %s version=%s want=%s", p.Host, p.ConfigDir, verOrDash(p.Version), m.Version))
		}
		if p.Revision == "" || !strings.EqualFold(p.Revision, m.VCSRevision) {
			problems = append(problems, fmt.Sprintf("%s %s revision=%s want=%s", p.Host, p.ConfigDir, verOrDash(p.Revision), m.VCSRevision))
		}
		if p.PayloadSHA256 == "" || !strings.EqualFold(p.PayloadSHA256, m.PayloadSHA256) {
			problems = append(problems, fmt.Sprintf("%s %s payload=%s want=%s", p.Host, p.ConfigDir, verOrDash(p.PayloadSHA256), m.PayloadSHA256))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("plugin cache identity is not target release: %s", strings.Join(problems, "; "))
	}
	return nil
}

func verifyUpdateLiveIdentity(m updateManifest, livePath string) error {
	id := readDistributionBinaryIdentity(livePath)
	if !id.Readable {
		return fmt.Errorf("live binary is not readable: %s", livePath)
	}
	if id.Version != m.Version {
		return fmt.Errorf("live version=%s want=%s", verOrDash(id.Version), m.Version)
	}
	if id.Revision == "" || !strings.EqualFold(id.Revision, m.VCSRevision) {
		return fmt.Errorf("live revision=%s want=%s", verOrDash(id.Revision), m.VCSRevision)
	}
	if id.Modified != nil && *id.Modified {
		return fmt.Errorf("live binary has vcs.modified=true")
	}
	if id.SHA256 == "" || !strings.EqualFold(id.SHA256, m.CLI.SHA256) {
		return fmt.Errorf("live SHA-256=%s want=%s", verOrDash(id.SHA256), m.CLI.SHA256)
	}
	return verifyUpdateCachesForManifest(m)
}

func syncKnownPluginCaches(m updateManifest) ([]updateCacheSyncResult, error) {
	if strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")) == "" && strings.TrimSpace(userEnvVar("CLAUDE_CONFIG_DIR")) == "" {
		return nil, fmt.Errorf("active Claude profile is not explicit; set CLAUDE_CONFIG_DIR before plugin update")
	}
	profiles := knownUpdateProfiles()
	active, err := activeClaudeConfigDir()
	if err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if p.Host == "claude" && !samePath(p.ConfigDir, active) {
			return nil, fmt.Errorf("inactive Claude profile %s has AirWorker installed; resolve multiple profiles before plugin update", p.ConfigDir)
		}
	}
	if len(profiles) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	var results []updateCacheSyncResult
	for _, p := range profiles {
		res := updateCacheSyncResult{Host: p.Host, ConfigDir: p.ConfigDir}
		switch p.Host {
		case "claude":
			out, err := runUpdateHostCommand(ctx, "CLAUDE_CONFIG_DIR", p.ConfigDir,
				"claude", "plugin", "update", "air-worker@air-plugins", "--json", "-y")
			res.Output = out
			if err != nil {
				results = append(results, res)
				return results, err
			}
			res.Changed = true
		case "codex":
			out1, err := runUpdateHostCommand(ctx, "CODEX_HOME", p.ConfigDir,
				"codex", "plugin", "marketplace", "upgrade", "air-plugins", "--json")
			if err != nil {
				res.Output = out1
				results = append(results, res)
				return results, err
			}
			out2, err := runUpdateHostCommand(ctx, "CODEX_HOME", p.ConfigDir,
				"codex", "plugin", "add", "air-worker@air-plugins", "--json")
			res.Output = strings.TrimSpace(out1 + "\n" + out2)
			if err != nil {
				results = append(results, res)
				return results, err
			}
			res.Changed = true
		default:
			continue
		}
		results = append(results, res)
	}
	if err := verifyUpdateCachesForManifest(m); err != nil {
		return results, err
	}
	return results, nil
}
