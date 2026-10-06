package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type curatorSkillStatusReport struct {
	Schema  string                    `json:"schema"`
	Status  string                    `json:"status"`
	Health  curatorSkillHealth        `json:"health"`
	Mirrors []curatorSkillMirrorState `json:"mirrors,omitempty"`
}

func curatorPluginRoot(explicit string) (string, error) {
	candidates := []string{
		strings.TrimSpace(explicit),
		strings.TrimSpace(os.Getenv("AIR_WORKER_PLUGIN_ROOT")),
		strings.TrimSpace(os.Getenv("CLAUDE_PLUGIN_ROOT")),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		if strings.EqualFold(filepath.Base(dir), "bin") {
			candidates = append(candidates, filepath.Dir(dir))
		}
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		abs, err := filepath.Abs(candidate)
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		key := strings.ToLower(abs)
		if seen[key] {
			continue
		}
		seen[key] = true
		if st, err := os.Stat(filepath.Join(abs, filepath.FromSlash(curatorSkillRegistryRel))); err == nil && !st.IsDir() {
			return abs, nil
		}
	}
	return "", errors.New("AirCurator release-owned skill registry not found; provide -plugin-root or AIR_WORKER_PLUGIN_ROOT")
}

func curatorMirrorDestinationStatus(configDir, rel string) (string, error) {
	configDir = filepath.Clean(configDir)
	if strings.TrimSpace(configDir) == "" {
		return "", errors.New("host config dir is empty")
	}
	relPath, err := curatorSafeRelative(rel)
	if err != nil {
		return "", err
	}
	dest := filepath.Join(configDir, relPath)
	if !pathWithinRoot(configDir, dest) {
		return "", errors.New("mirror destination escapes host config root")
	}
	parent := filepath.Dir(dest)
	existing := parent
	for {
		if _, err := os.Lstat(existing); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", err
		}
		next := filepath.Dir(existing)
		if next == existing || !pathWithinRoot(configDir, next) {
			return dest, nil
		}
		existing = next
	}
	resolvedConfig, err := filepath.Abs(configDir)
	if err != nil {
		return "", err
	}
	if r, resolveErr := filepath.EvalSymlinks(resolvedConfig); resolveErr == nil {
		resolvedConfig = r
	}
	resolvedExisting, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return "", err
	}
	resolvedExisting, err = filepath.Abs(resolvedExisting)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(filepath.Clean(resolvedConfig), filepath.Clean(resolvedExisting)) {
		return "", errors.New("mirror parent resolves outside host config root")
	}
	return dest, nil
}

func curatorSkillMirrorStatusReadOnly(root, host, configDir string) ([]curatorSkillMirrorState, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	reg, err := loadCuratorSkillRegistry(root)
	if err != nil {
		return nil, err
	}
	var states []curatorSkillMirrorState
	for _, skill := range reg.Skills {
		mirrorRel := strings.TrimSpace(skill.Mirrors[host])
		if mirrorRel == "" {
			continue
		}
		sourcePath, sourceBody, err := curatorSkillSource(root, skill)
		if err != nil {
			return states, err
		}
		dest, err := curatorMirrorDestinationStatus(configDir, mirrorRel)
		if err != nil {
			return states, err
		}
		sourceSHA := curatorBytesSHA(sourceBody)
		state := curatorSkillMirrorState{
			Host: host, ConfigDir: filepath.Clean(configDir), SkillID: skill.ID,
			SourcePath: sourcePath, SourceSHA256: sourceSHA, MirrorPath: dest,
		}
		body, err := os.ReadFile(dest)
		if errors.Is(err, os.ErrNotExist) {
			state.State = curatorMirrorMissing
			states = append(states, state)
			continue
		}
		if err != nil {
			return states, err
		}
		state.MirrorSHA256 = curatorBytesSHA(body)
		marker, markerErr := readCuratorMirrorMarker(curatorMirrorMarkerPath(dest))
		state.Managed = markerErr == nil && marker.SkillID == skill.ID && marker.SourcePath == skill.Path &&
			strings.EqualFold(marker.DeliveredSHA256, state.MirrorSHA256)
		switch {
		case state.MirrorSHA256 == sourceSHA && state.Managed:
			state.State = curatorMirrorCurrent
		case state.MirrorSHA256 == sourceSHA:
			state.State = curatorMirrorSynced
			state.Reason = "content matches release source but mirror is not marked managed"
		default:
			state.State = curatorMirrorDrift
			state.Reason = "mirror differs from release source"
		}
		states = append(states, state)
	}
	return states, nil
}

func printCuratorSkillJSON(value any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(value); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}

func curatorSkillStatusVerdict(report curatorSkillStatusReport) (string, int) {
	if report.Health.Status != curatorSkillStatusPass {
		return curatorSkillStatusFail, 1
	}
	for _, mirror := range report.Mirrors {
		if mirror.State == curatorMirrorDrift {
			return curatorSkillStatusFail, 1
		}
	}
	return curatorSkillStatusPass, 0
}

func cmdCuratorSkills(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker curator skills health|status|sync|resolve ...")
		return 2
	}
	switch argv[0] {
	case "health":
		fs := flag.NewFlagSet("curator skills health", flag.ContinueOnError)
		pluginRoot := fs.String("plugin-root", "", "release-owned AirWorker plugin root")
		asJSON := fs.Bool("json", false, "machine-readable output")
		if err := fs.Parse(argv[1:]); err != nil {
			return 2
		}
		root, err := curatorPluginRoot(*pluginRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		health := inspectCuratorSkillPackage(root)
		if *asJSON {
			_ = printCuratorSkillJSON(health)
		} else if health.Status == curatorSkillStatusPass {
			fmt.Printf("AirCurator skill package health: PASS · %d registered skills\n", len(health.Skills))
		} else {
			for _, violation := range health.Violations {
				fmt.Println("VIOLATION:", violation)
			}
		}
		if health.Status != curatorSkillStatusPass {
			return 1
		}
		return 0

	case "status":
		fs := flag.NewFlagSet("curator skills status", flag.ContinueOnError)
		pluginRoot := fs.String("plugin-root", "", "release-owned AirWorker plugin root")
		claudeDir := fs.String("claude-config", "", "Claude config root")
		codexDir := fs.String("codex-config", "", "Codex config root")
		asJSON := fs.Bool("json", false, "machine-readable output")
		if err := fs.Parse(argv[1:]); err != nil {
			return 2
		}
		root, err := curatorPluginRoot(*pluginRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		health := inspectCuratorSkillPackage(root)
		report := curatorSkillStatusReport{Schema: "air-worker.curator-skill-status/v1", Health: health}
		configs := map[string]string{
			"claude": strings.TrimSpace(*claudeDir),
			"codex":  strings.TrimSpace(*codexDir),
		}
		if configs["claude"] == "" {
			configs["claude"] = claudeConfigDir()
		}
		if configs["codex"] == "" {
			configs["codex"] = codexConfigDir()
		}
		for _, host := range []string{"claude", "codex"} {
			states, statusErr := curatorSkillMirrorStatusReadOnly(root, host, configs[host])
			if statusErr != nil {
				fmt.Fprintln(os.Stderr, statusErr)
				return 2
			}
			report.Mirrors = append(report.Mirrors, states...)
		}
		if *asJSON {
			return printCuratorSkillJSON(report)
		}
		fmt.Printf("AirCurator skill package: %s · registered=%d\n", health.Status, len(health.Skills))
		for _, mirror := range report.Mirrors {
			fmt.Printf("%s %s: %s · %s\n", mirror.Host, mirror.SkillID, mirror.State, mirror.MirrorPath)
		}
		if health.Status != curatorSkillStatusPass {
			return 1
		}
		return 0

	case "sync":
		fs := flag.NewFlagSet("curator skills sync", flag.ContinueOnError)
		pluginRoot := fs.String("plugin-root", "", "release-owned AirWorker plugin root")
		host := fs.String("host", "all", "claude|codex|all")
		claudeDir := fs.String("claude-config", "", "Claude config root")
		codexDir := fs.String("codex-config", "", "Codex config root")
		asJSON := fs.Bool("json", false, "machine-readable output")
		if err := fs.Parse(argv[1:]); err != nil {
			return 2
		}
		root, err := curatorPluginRoot(*pluginRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		configs := map[string]string{"claude": strings.TrimSpace(*claudeDir), "codex": strings.TrimSpace(*codexDir)}
		if configs["claude"] == "" {
			configs["claude"] = claudeConfigDir()
		}
		if configs["codex"] == "" {
			configs["codex"] = codexConfigDir()
		}
		var hosts []string
		switch strings.ToLower(strings.TrimSpace(*host)) {
		case "all":
			hosts = []string{"claude", "codex"}
		case "claude", "codex":
			hosts = []string{strings.ToLower(strings.TrimSpace(*host))}
		default:
			fmt.Fprintln(os.Stderr, "-host must be claude, codex, or all")
			return 2
		}
		var all []curatorSkillMirrorState
		for _, h := range hosts {
			states, syncErr := syncCuratorSkillMirrors(root, h, configs[h])
			all = append(all, states...)
			if syncErr != nil {
				if *asJSON {
					_ = printCuratorSkillJSON(map[string]any{"schema": "air-worker.curator-skill-sync/v1", "status": "FAIL", "states": all, "error": syncErr.Error()})
				} else {
					fmt.Fprintln(os.Stderr, syncErr)
				}
				return 1
			}
		}
		if *asJSON {
			return printCuratorSkillJSON(map[string]any{"schema": "air-worker.curator-skill-sync/v1", "status": "PASS", "states": all})
		}
		fmt.Printf("AirCurator skill mirrors: PASS · %d entries\n", len(all))
		return 0

	case "resolve":
		fs := flag.NewFlagSet("curator skills resolve", flag.ContinueOnError)
		pluginRoot := fs.String("plugin-root", "", "release-owned AirWorker plugin root")
		role := fs.String("role", "", "exact AirCurator role")
		trigger := fs.String("trigger", "", "task text matched against registered triggers")
		asJSON := fs.Bool("json", false, "machine-readable output")
		if err := fs.Parse(argv[1:]); err != nil {
			return 2
		}
		root, err := curatorPluginRoot(*pluginRoot)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		reg, err := loadCuratorSkillRegistry(root)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		skill, err := resolveCuratorSkill(reg, *role, *trigger)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		result := map[string]any{
			"schema": "air-worker.curator-skill-resolve/v1",
			"skill":  skill,
			"source": filepath.Join(root, filepath.FromSlash(skill.Path)),
		}
		if *asJSON {
			return printCuratorSkillJSON(result)
		}
		fmt.Printf("%s · role=%s · source=%s\n", skill.ID, skill.Role, result["source"])
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown curator skills action %q\n", argv[0])
		return 2
	}
}
