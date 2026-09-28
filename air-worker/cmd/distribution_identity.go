package main

import (
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const selfcheckSchemaVersion = "air-worker.selfcheck/v1"

type distributionBinaryIdentity struct {
	Path       string `json:"path"`
	Version    string `json:"version,omitempty"`
	Revision   string `json:"revision,omitempty"`
	Modified   *bool  `json:"vcs_modified,omitempty"`
	SHA256     string `json:"sha256,omitempty"`
	Readable   bool   `json:"readable"`
	Executable bool   `json:"executable"`
	Error      string `json:"error,omitempty"`
}

type distributionProfile struct {
	Host          string                     `json:"host"`
	ConfigDir     string                     `json:"config_dir"`
	Active        bool                       `json:"active"`
	HasState      bool                       `json:"has_air_worker_state"`
	Marketplace   string                     `json:"marketplace,omitempty"`
	Canonical     bool                       `json:"canonical_github"`
	PluginEnabled bool                       `json:"plugin_enabled"`
	InstallPath   string                     `json:"install_path,omitempty"`
	Version       string                     `json:"version,omitempty"`
	Revision      string                     `json:"revision,omitempty"`
	PayloadSHA256 string                     `json:"payload_sha256,omitempty"`
	CacheBinary   distributionBinaryIdentity `json:"cache_binary"`
	Error         string                     `json:"error,omitempty"`
}

type selfcheckReport struct {
	Schema     string                     `json:"schema"`
	At         string                     `json:"at"`
	Live       distributionBinaryIdentity `json:"live"`
	Profiles   []distributionProfile      `json:"profiles"`
	Warnings   []string                   `json:"warnings"`
	Violations []string                   `json:"violations"`
	NotProven  []string                   `json:"not_proven"`
}

var readDistributionBinaryIdentity = binaryIdentityFromPath

func executableName() string {
	if runtime.GOOS == "windows" {
		return "air-worker.exe"
	}
	return "air-worker"
}

func defaultLiveBinaryPath() string {
	if root := strings.TrimSpace(os.Getenv("AIR_WORKER_HOME")); root != "" {
		return filepath.Join(root, "bin", executableName())
	}
	if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
		return filepath.Join(local, "air-worker", "bin", executableName())
	}
	if self, err := os.Executable(); err == nil {
		return self
	}
	return executableName()
}

func binaryVersionPortable(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return ""
	}
	return parseVersionToken(decodeOutput(out))
}

func binaryIdentityFromPath(path string) distributionBinaryIdentity {
	id := distributionBinaryIdentity{Path: filepath.Clean(path)}
	if strings.TrimSpace(path) == "" {
		id.Error = "binary path is empty"
		return id
	}
	if _, err := os.Stat(path); err != nil {
		id.Error = err.Error()
		return id
	}
	id.Readable = true
	if sum, err := sha256File(path); err == nil {
		id.SHA256 = sum
	} else {
		id.Error = err.Error()
		return id
	}
	id.Version = binaryVersionPortable(path)
	id.Executable = id.Version != ""

	info, err := buildinfo.ReadFile(path)
	if err != nil {
		if id.Error == "" {
			id.Error = "build info unavailable: " + err.Error()
		}
		return id
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			id.Revision = strings.TrimSpace(setting.Value)
		case "vcs.modified":
			v := strings.EqualFold(strings.TrimSpace(setting.Value), "true")
			id.Modified = &v
		}
	}
	return id
}

func payloadSnapshot(root string) (string, int, error) {
	root = filepath.Clean(root)
	type item struct {
		rel  string
		path string
		link string
	}
	var items []item
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", ".woody", ".air-worker", ".in_use":
				// Host/cache runtime metadata is not part of the immutable plugin payload.
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(path)
			if readErr != nil {
				return readErr
			}
			items = append(items, item{rel: rel, link: target})
			return nil
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		items = append(items, item{rel: rel, path: path})
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].rel < items[j].rel })
	h := sha256.New()
	for _, it := range items {
		_, _ = io.WriteString(h, it.rel)
		_, _ = h.Write([]byte{0})
		if it.link != "" {
			_, _ = io.WriteString(h, "SYMLINK:")
			_, _ = io.WriteString(h, it.link)
		} else {
			f, openErr := os.Open(it.path)
			if openErr != nil {
				return "", 0, openErr
			}
			fileHash := sha256.New()
			if _, copyErr := io.Copy(fileHash, f); copyErr != nil {
				_ = f.Close()
				return "", 0, copyErr
			}
			_ = f.Close()
			_, _ = io.WriteString(h, hex.EncodeToString(fileHash.Sum(nil)))
		}
		_, _ = h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)), len(items), nil
}

func claudeDefaultConfigDir() string {
	up := os.Getenv("USERPROFILE")
	if up == "" {
		up = os.Getenv("HOME")
	}
	return filepath.Join(up, ".claude")
}

func codexDefaultConfigDir() string {
	up := os.Getenv("USERPROFILE")
	if up == "" {
		up = os.Getenv("HOME")
	}
	return filepath.Join(up, ".codex")
}

func uniqueConfigRoots(active, fallback string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range []string{active, fallback} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		abs, err := filepath.Abs(value)
		if err != nil {
			abs = filepath.Clean(value)
		}
		key := strings.ToLower(filepath.Clean(abs))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, filepath.Clean(abs))
	}
	return out
}

func claudeProfileSnapshot(configDir string, active bool) distributionProfile {
	p := distributionProfile{Host: "claude", ConfigDir: configDir, Active: active}
	mp, mpOK := readMarketplace(configDir, canonicalMarketplace)
	rec, recOK := readInstalledPlugin(configDir, canonicalPluginKey)
	p.HasState = mpOK || recOK
	p.Canonical = mpOK && marketplaceIsGitHub(mp)
	p.Marketplace = describeMarketplace(mpOK, mp)
	p.PluginEnabled = recOK
	if !recOK {
		return p
	}
	p.InstallPath, p.Version = rec.InstallPath, rec.Version
	cacheBinary := filepath.Join(rec.InstallPath, "bin", executableName())
	p.CacheBinary = readDistributionBinaryIdentity(cacheBinary)
	p.Revision = p.CacheBinary.Revision
	if digest, _, err := payloadSnapshot(rec.InstallPath); err == nil {
		p.PayloadSHA256 = digest
	} else {
		p.Error = "payload snapshot: " + err.Error()
	}
	return p
}

func codexProfileSnapshot(configDir string, active bool) distributionProfile {
	p := distributionProfile{Host: "codex", ConfigDir: configDir, Active: active}
	mp, mpOK, enabled := readCodexConfig(configDir)
	rec, recOK := readCodexInstalledPlugin(configDir)
	p.HasState = mpOK || recOK || enabled
	p.Canonical = mpOK && codexMarketplaceIsGitHub(mp)
	p.Marketplace = describeCodexMarketplace(mpOK, mp)
	p.PluginEnabled = enabled && recOK
	if !recOK {
		return p
	}
	p.InstallPath, p.Version = rec.InstallPath, rec.Version
	cacheBinary := filepath.Join(rec.InstallPath, "bin", executableName())
	p.CacheBinary = readDistributionBinaryIdentity(cacheBinary)
	p.Revision = p.CacheBinary.Revision
	if digest, _, err := payloadSnapshot(rec.InstallPath); err == nil {
		p.PayloadSHA256 = digest
	} else {
		p.Error = "payload snapshot: " + err.Error()
	}
	return p
}

func profileKey(p distributionProfile) string {
	return p.Host + "|" + strings.ToLower(filepath.Clean(p.ConfigDir))
}

func appendUnique(items []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return items
	}
	for _, existing := range items {
		if existing == value {
			return items
		}
	}
	return append(items, value)
}

func compareDistributionIdentity(live distributionBinaryIdentity, profiles []distributionProfile) (warnings, violations, notProven []string) {
	if !live.Readable || live.SHA256 == "" {
		notProven = append(notProven, "live binary is not readable/hashable: "+live.Path)
	} else {
		if live.Version == "" {
			notProven = append(notProven, "live binary version unavailable: "+live.Path)
		}
		if live.Revision == "" {
			notProven = append(notProven, "live binary embedded vcs.revision unavailable: "+live.Path)
		}
		if live.Modified != nil && *live.Modified {
			violations = append(violations, "live binary was built from vcs.modified=true: "+live.Path)
		}
	}

	var installed []distributionProfile
	for _, p := range profiles {
		if !p.HasState {
			continue
		}
		if !p.Canonical {
			violations = appendUnique(violations,
				fmt.Sprintf("%s config %s: air-plugins marketplace is not canonical GitHub (%s)", p.Host, p.ConfigDir, p.Marketplace))
		}
		if !p.PluginEnabled || strings.TrimSpace(p.InstallPath) == "" {
			warnings = appendUnique(warnings,
				fmt.Sprintf("%s config %s: air-worker plugin is not enabled/installed", p.Host, p.ConfigDir))
			continue
		}
		installed = append(installed, p)
		if p.Error != "" {
			notProven = appendUnique(notProven, fmt.Sprintf("%s cache %s: %s", p.Host, p.InstallPath, p.Error))
		}
		if p.Revision == "" {
			notProven = appendUnique(notProven, fmt.Sprintf("%s cache %s: embedded vcs.revision unavailable", p.Host, p.InstallPath))
		}
		if p.PayloadSHA256 == "" {
			notProven = appendUnique(notProven, fmt.Sprintf("%s cache %s: payload SHA-256 unavailable", p.Host, p.InstallPath))
		}
		if p.CacheBinary.Modified != nil && *p.CacheBinary.Modified {
			violations = appendUnique(violations, fmt.Sprintf("%s cache binary has vcs.modified=true: %s", p.Host, p.CacheBinary.Path))
		}
		if live.Readable && p.CacheBinary.Readable {
			if live.Version != "" && p.Version != "" && live.Version != p.Version {
				violations = appendUnique(violations,
					fmt.Sprintf("VERSION MISMATCH: live %s@%s != %s cache %s; use canonical GitHub update",
						appName, live.Version, p.Host, p.Version))
			}
			if live.Version != "" && p.Version != "" && live.Version == p.Version &&
				live.Revision != "" && p.Revision != "" && !strings.EqualFold(live.Revision, p.Revision) {
				violations = appendUnique(violations,
					fmt.Sprintf("SAME-VERSION REVISION MISMATCH: live %s@%s revision %s != %s cache revision %s; use canonical marketplace update/reinstall",
						appName, live.Version, live.Revision, p.Host, p.Revision))
			}
			if live.Version == p.Version && live.SHA256 != "" && p.CacheBinary.SHA256 != "" &&
				!strings.EqualFold(live.SHA256, p.CacheBinary.SHA256) {
				violations = appendUnique(violations,
					fmt.Sprintf("SAME-VERSION SHA MISMATCH: live %s != %s cache binary; use canonical marketplace update/reinstall",
						live.Path, p.Host))
			}
		}
	}
	for i := 0; i < len(installed); i++ {
		for j := i + 1; j < len(installed); j++ {
			a, b := installed[i], installed[j]
			if a.Version == "" || b.Version == "" {
				continue
			}
			if a.Version != b.Version {
				violations = appendUnique(violations,
					fmt.Sprintf("CACHE VERSION MISMATCH: %s %s != %s %s", a.Host, a.Version, b.Host, b.Version))
				continue
			}
			if a.Revision != "" && b.Revision != "" && !strings.EqualFold(a.Revision, b.Revision) {
				violations = appendUnique(violations,
					fmt.Sprintf("SAME-VERSION CACHE REVISION MISMATCH: %s %s != %s %s for version %s",
						a.Host, a.Revision, b.Host, b.Revision, a.Version))
			}
			if a.PayloadSHA256 != "" && b.PayloadSHA256 != "" && !strings.EqualFold(a.PayloadSHA256, b.PayloadSHA256) {
				violations = appendUnique(violations,
					fmt.Sprintf("SAME-VERSION PAYLOAD MISMATCH: %s cache != %s cache for version %s", a.Host, b.Host, a.Version))
			}
		}
	}
	return
}

func buildSelfcheckReport(livePath, claudeActive, codexActive string) selfcheckReport {
	report := selfcheckReport{
		Schema: selfcheckSchemaVersion, At: time.Now().UTC().Format(time.RFC3339Nano),
		Warnings: []string{}, Violations: []string{}, NotProven: []string{},
	}
	report.Live = readDistributionBinaryIdentity(livePath)

	claudeRoots := uniqueConfigRoots(claudeActive, claudeDefaultConfigDir())
	codexRoots := uniqueConfigRoots(codexActive, codexDefaultConfigDir())
	for _, root := range claudeRoots {
		report.Profiles = append(report.Profiles, claudeProfileSnapshot(root, samePath(root, claudeActive)))
	}
	for _, root := range codexRoots {
		report.Profiles = append(report.Profiles, codexProfileSnapshot(root, samePath(root, codexActive)))
	}

	for _, host := range []string{"claude", "codex"} {
		var roots []string
		for _, p := range report.Profiles {
			if p.Host == host && p.HasState {
				roots = append(roots, p.ConfigDir)
			}
		}
		if len(roots) > 1 {
			report.Warnings = appendUnique(report.Warnings,
				fmt.Sprintf("MULTIPLE PROFILE AMBIGUITY: %s has air-worker state in %s", host, strings.Join(roots, ", ")))
		}
	}
	warnings, violations, notProven := compareDistributionIdentity(report.Live, report.Profiles)
	report.Warnings = append(report.Warnings, warnings...)
	report.Violations = append(report.Violations, violations...)
	report.NotProven = append(report.NotProven, notProven...)
	return report
}

func selfcheckExit(report selfcheckReport) int {
	if len(report.Violations) > 0 {
		return 1
	}
	if len(report.NotProven) > 0 {
		return 2
	}
	return 0
}

func selfcheckSummaryLines(report selfcheckReport) []string {
	lines := []string{fmt.Sprintf("Live identity: %s · version=%s · revision=%s · sha256=%s",
		report.Live.Path, verOrDash(report.Live.Version), verOrDash(report.Live.Revision), report.Live.SHA256)}
	for _, p := range report.Profiles {
		state := "no air-worker state"
		if p.HasState {
			state = fmt.Sprintf("version=%s revision=%s payload=%s canonical=%t",
				verOrDash(p.Version), verOrDash(p.Revision), p.PayloadSHA256, p.Canonical)
		}
		active := ""
		if p.Active {
			active = " ACTIVE"
		}
		lines = append(lines, fmt.Sprintf("%s config: %s%s · %s", strings.Title(p.Host), p.ConfigDir, active, state))
	}
	for _, warning := range report.Warnings {
		lines = append(lines, "WARNING: "+warning)
	}
	for _, np := range report.NotProven {
		lines = append(lines, "NOT_PROVEN: "+np)
	}
	for _, violation := range report.Violations {
		lines = append(lines, "VIOLATION: "+violation)
	}
	if selfcheckExit(report) == 0 {
		lines = append(lines, "Selfcheck: PASS")
	}
	return lines
}

func printSelfcheck(report selfcheckReport) {
	fmt.Printf("Selfcheck   : %s%s", appName, lineEnding)
	for _, line := range selfcheckSummaryLines(report) {
		fmt.Print(line + lineEnding)
	}
}

func installIdentityStatusLines(livePath, claudeDir, codexDir string) []string {
	return selfcheckSummaryLines(buildSelfcheckReport(livePath, claudeDir, codexDir))
}

func cmdSelfcheck(argv []string) int {
	fs := flag.NewFlagSet("selfcheck", flag.ContinueOnError)
	live := fs.String("live", "", "path to live air-worker binary; default installed user runtime")
	claudeDir := fs.String("claude-config", "", "active Claude config dir")
	codexDir := fs.String("codex-config", "", "active Codex config dir")
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	livePath := strings.TrimSpace(*live)
	if livePath == "" {
		livePath = defaultLiveBinaryPath()
	}
	activeClaude := strings.TrimSpace(*claudeDir)
	if activeClaude == "" {
		activeClaude = claudeConfigDir()
	}
	activeCodex := strings.TrimSpace(*codexDir)
	if activeCodex == "" {
		activeCodex = codexConfigDir()
	}
	report := buildSelfcheckReport(livePath, activeClaude, activeCodex)
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		printSelfcheck(report)
	}
	return selfcheckExit(report)
}

func requireDistributionIdentity(report selfcheckReport) error {
	switch selfcheckExit(report) {
	case 0:
		return nil
	case 1:
		return errors.New(strings.Join(report.Violations, "; "))
	default:
		return errors.New(strings.Join(report.NotProven, "; "))
	}
}
