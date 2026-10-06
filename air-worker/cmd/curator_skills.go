package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	curatorSkillRegistryRel          = "skills/air-curator/registry.json"
	curatorSkillRegistrySchema       = "air-worker.curator-skills/v1"
	curatorSkillRegistryRequiredFrom = "0.11.7"
	curatorSkillMirrorSchema         = "air-worker.curator-skill-mirror/v1"
	curatorSkillMirrorMarkerName     = ".air-worker-managed.json"
	curatorSkillStatusPass           = "PASS"
	curatorSkillStatusFail           = "FAIL"
	curatorHostRequired              = "required"
	curatorHostManagedMirror         = "managed_mirror"
	curatorHostNotApplicable         = "not_applicable"
	curatorMirrorSynced              = "synced"
	curatorMirrorCurrent             = "current"
	curatorMirrorMissing             = "missing"
	curatorMirrorDrift               = "drift"
)

type curatorSkillHost struct {
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
}

type curatorSkillSpec struct {
	ID       string            `json:"id"`
	Path     string            `json:"path"`
	SHA256   string            `json:"sha256"`
	Role     string            `json:"role"`
	Load     string            `json:"load"`
	Triggers []string          `json:"triggers"`
	Mirrors  map[string]string `json:"mirrors,omitempty"`
}

type curatorSkillRegistry struct {
	Schema          string                      `json:"schema"`
	Product         string                      `json:"product"`
	SourceOfTruth   string                      `json:"source_of_truth"`
	StartupContract map[string]any              `json:"startup_contract,omitempty"`
	Hosts           map[string]curatorSkillHost `json:"hosts"`
	Skills          []curatorSkillSpec          `json:"skills"`
}

type curatorSkillHealthItem struct {
	ID          string   `json:"id"`
	Path        string   `json:"path"`
	Role        string   `json:"role"`
	SHA256      string   `json:"sha256,omitempty"`
	Frontmatter string   `json:"frontmatter_name,omitempty"`
	Triggers    []string `json:"triggers,omitempty"`
	Status      string   `json:"status"`
}

type curatorSkillHealth struct {
	Schema       string                      `json:"schema"`
	Root         string                      `json:"root"`
	RegistryPath string                      `json:"registry_path"`
	Status       string                      `json:"status"`
	Hosts        map[string]curatorSkillHost `json:"hosts,omitempty"`
	Skills       []curatorSkillHealthItem    `json:"skills,omitempty"`
	Violations   []string                    `json:"violations"`
}

type curatorSkillMirrorState struct {
	Host         string `json:"host"`
	ConfigDir    string `json:"config_dir"`
	SkillID      string `json:"skill_id"`
	SourcePath   string `json:"source_path"`
	SourceSHA256 string `json:"source_sha256"`
	MirrorPath   string `json:"mirror_path"`
	MirrorSHA256 string `json:"mirror_sha256,omitempty"`
	Managed      bool   `json:"managed"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
}

type curatorSkillMirrorMarker struct {
	Schema          string `json:"schema"`
	Product         string `json:"product"`
	SkillID         string `json:"skill_id"`
	SourcePath      string `json:"source_path"`
	SourceSHA256    string `json:"source_sha256"`
	DeliveredSHA256 string `json:"delivered_sha256"`
}

type curatorSkillHookError struct {
	Err error
}

func (e *curatorSkillHookError) Error() string {
	if e == nil || e.Err == nil {
		return "AirCurator skill startup failed"
	}
	return "AirCurator skill startup: " + e.Err.Error()
}

func (e *curatorSkillHookError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var curatorSecretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\bghp_[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`(?i)\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`(?i)\bsk-[A-Za-z0-9]{20,}\b`),
	regexp.MustCompile(`(?i)\bglif_[A-Za-z0-9_]{20,}\b`),
	regexp.MustCompile(`(?i)(api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret)\s*[:=]\s*["']?[A-Za-z0-9_./+=-]{12,}`),
}

func curatorBytesSHA(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func curatorSafeRelative(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("path is empty")
	}
	if filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return "", fmt.Errorf("absolute path is not allowed: %s", raw)
	}
	rel := filepath.Clean(filepath.FromSlash(raw))
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes root: %s", raw)
	}
	return rel, nil
}

func curatorFrontmatter(data []byte) (name, description string, err error) {
	text := strings.ReplaceAll(string(bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return "", "", errors.New("SKILL.md frontmatter is missing")
	}
	end := strings.Index(text[4:], "\n---\n")
	if end < 0 {
		return "", "", errors.New("SKILL.md frontmatter terminator is missing")
	}
	for _, raw := range strings.Split(text[4:4+end], "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "name:") {
			name = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "name:")), "'\"")
		}
		if strings.HasPrefix(line, "description:") {
			description = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "description:")), "'\"")
		}
	}
	if name == "" || description == "" {
		return name, description, errors.New("SKILL.md frontmatter requires name and description")
	}
	return name, description, nil
}

func curatorSkillHasSecret(data []byte) bool {
	for _, pattern := range curatorSecretPatterns {
		if pattern.Match(data) {
			return true
		}
	}
	return false
}

func loadCuratorSkillRegistry(root string) (curatorSkillRegistry, error) {
	var reg curatorSkillRegistry
	root = filepath.Clean(root)
	path := filepath.Join(root, filepath.FromSlash(curatorSkillRegistryRel))
	raw, err := os.ReadFile(path)
	if err != nil {
		return reg, fmt.Errorf("read curator skill registry: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&reg); err != nil {
		return reg, fmt.Errorf("decode curator skill registry: %w", err)
	}
	if reg.Schema != curatorSkillRegistrySchema || reg.Product != "air-worker" || reg.SourceOfTruth != "release-payload" {
		return reg, errors.New("curator skill registry schema/product/source_of_truth mismatch")
	}
	if len(reg.Skills) == 0 {
		return reg, errors.New("curator skill registry has no skills")
	}
	for host, info := range reg.Hosts {
		switch info.Status {
		case curatorHostRequired, curatorHostManagedMirror:
		case curatorHostNotApplicable:
			if strings.TrimSpace(info.Reason) == "" {
				return reg, fmt.Errorf("host %s NOT_APPLICABLE requires reason", host)
			}
		default:
			return reg, fmt.Errorf("host %s has unsupported status %q", host, info.Status)
		}
	}
	for _, required := range []string{"windows", "claude", "codex", "hermes", "android"} {
		if _, ok := reg.Hosts[required]; !ok {
			return reg, fmt.Errorf("curator skill registry missing host applicability: %s", required)
		}
	}
	seenID := map[string]bool{}
	seenPath := map[string]bool{}
	seenMirrorDestination := map[string]string{}
	seenMirrorMarker := map[string]string{}
	for i := range reg.Skills {
		s := &reg.Skills[i]
		s.ID = strings.TrimSpace(s.ID)
		s.Path = filepath.ToSlash(strings.TrimSpace(s.Path))
		s.SHA256 = strings.ToLower(strings.TrimSpace(s.SHA256))
		s.Role = strings.TrimSpace(s.Role)
		s.Load = strings.TrimSpace(s.Load)
		if s.ID == "" || s.Path == "" || s.Role == "" || len(s.Triggers) == 0 {
			return reg, fmt.Errorf("registered curator skill requires id/path/role/triggers")
		}
		if s.Load != "startup" && s.Load != "on_trigger" {
			return reg, fmt.Errorf("skill %s has unsupported load mode %q", s.ID, s.Load)
		}
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(s.ID) {
			return reg, fmt.Errorf("skill id is not canonical: %s", s.ID)
		}
		if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(s.SHA256) {
			return reg, fmt.Errorf("skill %s has invalid sha256", s.ID)
		}
		rel, err := curatorSafeRelative(s.Path)
		if err != nil {
			return reg, fmt.Errorf("skill %s source path: %w", s.ID, err)
		}
		if !strings.HasPrefix(filepath.ToSlash(rel), "skills/") {
			return reg, fmt.Errorf("skill %s source must stay under skills/: %s", s.ID, s.Path)
		}
		keyPath := strings.ToLower(filepath.Clean(rel))
		if seenID[s.ID] {
			return reg, fmt.Errorf("duplicate curator skill id: %s", s.ID)
		}
		if seenPath[keyPath] {
			return reg, fmt.Errorf("duplicate curator skill source path: %s", s.Path)
		}
		seenID[s.ID], seenPath[keyPath] = true, true
		for host, mirror := range s.Mirrors {
			if host != "claude" && host != "codex" {
				return reg, fmt.Errorf("skill %s mirror host is unsupported: %s", s.ID, host)
			}
			mrel, err := curatorSafeRelative(mirror)
			if err != nil || !strings.HasPrefix(filepath.ToSlash(mrel), "skills/") {
				return reg, fmt.Errorf("skill %s mirror path for %s is unsafe: %s", s.ID, host, mirror)
			}
			destinationKey := host + "\x00" + strings.ToLower(filepath.Clean(mrel))
			if other, ok := seenMirrorDestination[destinationKey]; ok {
				return reg, fmt.Errorf("curator mirror destination collision for %s: skills %s and %s use %s", host, other, s.ID, mirror)
			}
			seenMirrorDestination[destinationKey] = s.ID
			markerRel := filepath.Join(filepath.Dir(mrel), curatorSkillMirrorMarkerName)
			markerKey := host + "\x00" + strings.ToLower(filepath.Clean(markerRel))
			if other, ok := seenMirrorMarker[markerKey]; ok {
				return reg, fmt.Errorf("curator mirror marker collision for %s: skills %s and %s share %s", host, other, s.ID, filepath.ToSlash(markerRel))
			}
			seenMirrorMarker[markerKey] = s.ID
		}
		for j := range s.Triggers {
			s.Triggers[j] = strings.TrimSpace(s.Triggers[j])
			if s.Triggers[j] == "" {
				return reg, fmt.Errorf("skill %s has empty trigger", s.ID)
			}
		}
	}
	return reg, nil
}

func curatorResolvedRoot(root string) (string, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	abs = filepath.Clean(abs)
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func curatorSkillSource(root string, skill curatorSkillSpec) (string, []byte, error) {
	rel, err := curatorSafeRelative(skill.Path)
	if err != nil {
		return "", nil, err
	}
	path := filepath.Join(root, rel)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, nil, fmt.Errorf("registered skill source missing: %s", skill.Path)
		}
		return path, nil, err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return path, nil, fmt.Errorf("registered skill source is not an ordinary file: %s", skill.Path)
	}
	resolvedRoot, err := curatorResolvedRoot(root)
	if err != nil {
		return path, nil, fmt.Errorf("resolve payload root: %w", err)
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return path, nil, fmt.Errorf("resolve registered skill source: %w", err)
	}
	resolvedPath, err = filepath.Abs(resolvedPath)
	if err != nil {
		return path, nil, err
	}
	if !pathWithinRoot(resolvedRoot, filepath.Clean(resolvedPath)) {
		return path, nil, fmt.Errorf("registered skill source escapes payload root: %s", skill.Path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return path, nil, err
	}
	return path, body, nil
}

func curatorSkillRegistryRequired(packageVersion string) bool {
	packageVersion = strings.TrimSpace(packageVersion)
	return packageVersion != "" && compareVersions(packageVersion, curatorSkillRegistryRequiredFrom) >= 0
}

func missingCuratorSkillRegistryHealth(root string, err error) *curatorSkillHealth {
	root = filepath.Clean(root)
	reason := "required AirCurator skill registry missing"
	if err != nil {
		reason += ": " + err.Error()
	}
	return &curatorSkillHealth{
		Schema:       "air-worker.curator-skill-health/v1",
		Root:         root,
		RegistryPath: filepath.Join(root, filepath.FromSlash(curatorSkillRegistryRel)),
		Status:       curatorSkillStatusFail,
		Violations:   []string{reason},
	}
}

func inspectCuratorProfileSkills(root, host, configDir string, active bool, packageVersion string) (*curatorSkillHealth, []curatorSkillMirrorState, error) {
	registryPath := filepath.Join(filepath.Clean(root), filepath.FromSlash(curatorSkillRegistryRel))
	info, statErr := os.Stat(registryPath)
	if statErr != nil || info.IsDir() {
		if !curatorSkillRegistryRequired(packageVersion) {
			return nil, nil, nil
		}
		if statErr == nil && info.IsDir() {
			statErr = fmt.Errorf("registry path is a directory: %s", registryPath)
		}
		return missingCuratorSkillRegistryHealth(root, statErr), nil, nil
	}
	health := inspectCuratorSkillPackage(root)
	if health.Status != curatorSkillStatusPass || !active {
		return &health, nil, nil
	}
	mirrors, err := curatorSkillMirrorStatusReadOnly(root, host, configDir)
	return &health, mirrors, err
}

func inspectCuratorSkillPackage(root string) curatorSkillHealth {
	root = filepath.Clean(root)
	health := curatorSkillHealth{
		Schema:       "air-worker.curator-skill-health/v1",
		Root:         root,
		RegistryPath: filepath.Join(root, filepath.FromSlash(curatorSkillRegistryRel)),
		Status:       curatorSkillStatusPass,
		Violations:   []string{},
	}
	reg, err := loadCuratorSkillRegistry(root)
	if err != nil {
		health.Status = curatorSkillStatusFail
		health.Violations = append(health.Violations, err.Error())
		return health
	}
	health.Hosts = reg.Hosts
	for _, skill := range reg.Skills {
		item := curatorSkillHealthItem{ID: skill.ID, Path: skill.Path, Role: skill.Role, Triggers: append([]string(nil), skill.Triggers...), Status: curatorSkillStatusPass}
		_, body, sourceErr := curatorSkillSource(root, skill)
		if sourceErr != nil {
			item.Status = curatorSkillStatusFail
			health.Violations = append(health.Violations, fmt.Sprintf("%s: %v", skill.ID, sourceErr))
			health.Skills = append(health.Skills, item)
			continue
		}
		item.SHA256 = curatorBytesSHA(body)
		name, _, frontErr := curatorFrontmatter(body)
		item.Frontmatter = name
		if frontErr != nil || name != skill.ID {
			item.Status = curatorSkillStatusFail
			health.Violations = append(health.Violations, fmt.Sprintf("%s: frontmatter name mismatch: got %q", skill.ID, name))
		}
		if !strings.EqualFold(item.SHA256, skill.SHA256) {
			item.Status = curatorSkillStatusFail
			health.Violations = append(health.Violations, fmt.Sprintf("%s: sha256 mismatch: registry=%s payload=%s", skill.ID, skill.SHA256, item.SHA256))
		}
		if curatorSkillHasSecret(body) {
			item.Status = curatorSkillStatusFail
			health.Violations = append(health.Violations, fmt.Sprintf("%s: secret-like material found in release-owned skill", skill.ID))
		}
		health.Skills = append(health.Skills, item)
	}
	if len(health.Violations) > 0 {
		health.Status = curatorSkillStatusFail
	}
	return health
}

func resolveCuratorSkill(reg curatorSkillRegistry, role, trigger string) (curatorSkillSpec, error) {
	role = strings.ToLower(strings.TrimSpace(role))
	trigger = strings.ToLower(strings.TrimSpace(trigger))
	if role != "" {
		var matches []curatorSkillSpec
		for _, skill := range reg.Skills {
			if strings.EqualFold(skill.Role, role) {
				matches = append(matches, skill)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) == 0 {
			return curatorSkillSpec{}, errors.New("no registered AirCurator skill matches role/trigger")
		}
		var ids []string
		for _, match := range matches {
			ids = append(ids, match.ID)
		}
		sort.Strings(ids)
		return curatorSkillSpec{}, fmt.Errorf("ambiguous AirCurator role match: %s", strings.Join(ids, ", "))
	}

	type triggerMatch struct {
		skill curatorSkillSpec
		width int
	}
	var matches []triggerMatch
	best := 0
	for _, skill := range reg.Skills {
		width := 0
		for _, candidate := range skill.Triggers {
			candidate = strings.ToLower(strings.TrimSpace(candidate))
			if candidate != "" && strings.Contains(trigger, candidate) && len(candidate) > width {
				width = len(candidate)
			}
		}
		if width == 0 {
			continue
		}
		if width > best {
			best = width
			matches = matches[:0]
		}
		if width == best {
			matches = append(matches, triggerMatch{skill: skill, width: width})
		}
	}
	if len(matches) == 0 {
		return curatorSkillSpec{}, errors.New("no registered AirCurator skill matches role/trigger")
	}
	if len(matches) > 1 {
		var ids []string
		for _, match := range matches {
			ids = append(ids, match.skill.ID)
		}
		sort.Strings(ids)
		return curatorSkillSpec{}, fmt.Errorf("ambiguous AirCurator trigger match: %s", strings.Join(ids, ", "))
	}
	return matches[0].skill, nil
}

func curatorSkillStartupContext(reg curatorSkillRegistry) string {
	var startup []string
	var triggered []string
	for _, skill := range reg.Skills {
		entry := skill.ID + " [role=" + skill.Role + "]"
		if skill.Load == "startup" {
			startup = append(startup, entry)
		} else {
			triggered = append(triggered, entry+" triggers="+strings.Join(skill.Triggers, ","))
		}
	}
	sort.Strings(startup)
	sort.Strings(triggered)
	parts := []string{"AirCurator skill registry source=release-payload."}
	if len(startup) > 0 {
		parts = append(parts, "startup: "+strings.Join(startup, "; "))
	}
	if len(triggered) > 0 {
		parts = append(parts, "on-trigger: "+strings.Join(triggered, "; "))
	}
	return strings.Join(parts, " ")
}

func curatorMirrorMarkerPath(dest string) string {
	return filepath.Join(filepath.Dir(dest), curatorSkillMirrorMarkerName)
}

func readCuratorMirrorMarker(path string) (curatorSkillMirrorMarker, error) {
	var marker curatorSkillMirrorMarker
	raw, err := os.ReadFile(path)
	if err != nil {
		return marker, err
	}
	if err := json.Unmarshal(raw, &marker); err != nil {
		return marker, err
	}
	if marker.Schema != curatorSkillMirrorSchema || marker.Product != "air-worker" || marker.SkillID == "" || marker.DeliveredSHA256 == "" {
		return marker, errors.New("invalid managed mirror marker")
	}
	return marker, nil
}

func writeCuratorMirrorMarker(path string, marker curatorSkillMirrorMarker) error {
	raw, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeFileAtomic(path, raw)
}

func curatorMirrorDestination(configDir, rel string) (string, error) {
	// Check the nearest existing ancestor before MkdirAll so an existing
	// junction/symlink under <config>/skills cannot redirect directory creation
	// outside the host config root.
	dest, err := curatorMirrorDestinationStatus(configDir, rel)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	resolvedConfig, err := curatorResolvedRoot(configDir)
	if err != nil {
		return "", fmt.Errorf("resolve host config root: %w", err)
	}
	resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(dest))
	if err != nil {
		return "", fmt.Errorf("resolve mirror parent: %w", err)
	}
	resolvedDir, err = filepath.Abs(resolvedDir)
	if err != nil {
		return "", err
	}
	if !pathWithinRoot(resolvedConfig, filepath.Clean(resolvedDir)) {
		return "", errors.New("mirror parent resolves outside host config root")
	}
	return dest, nil
}

func syncCuratorSkillMirrors(root, host, configDir string) ([]curatorSkillMirrorState, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	if host != "claude" && host != "codex" {
		return nil, fmt.Errorf("unsupported AirCurator mirror host: %s", host)
	}
	health := inspectCuratorSkillPackage(root)
	if health.Status != curatorSkillStatusPass {
		return nil, fmt.Errorf("curator skill package health failed: %s", strings.Join(health.Violations, "; "))
	}
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
		sourceSHA := curatorBytesSHA(sourceBody)
		dest, err := curatorMirrorDestination(configDir, mirrorRel)
		if err != nil {
			return states, fmt.Errorf("%s: %w", skill.ID, err)
		}
		state := curatorSkillMirrorState{
			Host: host, ConfigDir: filepath.Clean(configDir), SkillID: skill.ID,
			SourcePath: sourcePath, SourceSHA256: sourceSHA, MirrorPath: dest,
		}
		existing, readErr := os.ReadFile(dest)
		if errors.Is(readErr, os.ErrNotExist) {
			if err := writeFileAtomic(dest, sourceBody); err != nil {
				return states, fmt.Errorf("%s mirror write: %w", skill.ID, err)
			}
			marker := curatorSkillMirrorMarker{
				Schema: curatorSkillMirrorSchema, Product: "air-worker", SkillID: skill.ID,
				SourcePath: skill.Path, SourceSHA256: sourceSHA, DeliveredSHA256: sourceSHA,
			}
			if err := writeCuratorMirrorMarker(curatorMirrorMarkerPath(dest), marker); err != nil {
				return states, fmt.Errorf("%s marker write: %w", skill.ID, err)
			}
			state.MirrorSHA256, state.Managed, state.State = sourceSHA, true, curatorMirrorSynced
			states = append(states, state)
			continue
		}
		if readErr != nil {
			return states, fmt.Errorf("%s mirror read: %w", skill.ID, readErr)
		}
		existingSHA := curatorBytesSHA(existing)
		state.MirrorSHA256 = existingSHA
		markerPath := curatorMirrorMarkerPath(dest)
		marker, markerErr := readCuratorMirrorMarker(markerPath)
		if existingSHA == sourceSHA {
			if markerErr != nil || marker.SkillID != skill.ID || marker.DeliveredSHA256 != existingSHA || marker.SourcePath != skill.Path {
				marker = curatorSkillMirrorMarker{
					Schema: curatorSkillMirrorSchema, Product: "air-worker", SkillID: skill.ID,
					SourcePath: skill.Path, SourceSHA256: sourceSHA, DeliveredSHA256: sourceSHA,
				}
				if err := writeCuratorMirrorMarker(markerPath, marker); err != nil {
					return states, fmt.Errorf("%s marker adopt: %w", skill.ID, err)
				}
				state.State = curatorMirrorSynced
			} else {
				state.State = curatorMirrorCurrent
			}
			state.Managed = true
			states = append(states, state)
			continue
		}
		if markerErr != nil || marker.SkillID != skill.ID || marker.SourcePath != skill.Path || !strings.EqualFold(marker.DeliveredSHA256, existingSHA) {
			state.State = curatorMirrorDrift
			state.Reason = "mirror differs from release source and is not an unchanged AirWorker-managed copy"
			states = append(states, state)
			return states, fmt.Errorf("%s: %s: %s", skill.ID, state.Reason, dest)
		}
		if err := writeFileAtomic(dest, sourceBody); err != nil {
			return states, fmt.Errorf("%s managed mirror update: %w", skill.ID, err)
		}
		marker.SourceSHA256 = sourceSHA
		marker.DeliveredSHA256 = sourceSHA
		if err := writeCuratorMirrorMarker(markerPath, marker); err != nil {
			return states, fmt.Errorf("%s marker update: %w", skill.ID, err)
		}
		state.MirrorSHA256, state.Managed, state.State = sourceSHA, true, curatorMirrorSynced
		states = append(states, state)
	}
	return states, nil
}

func curatorSkillMirrorStatus(root, host, configDir string) ([]curatorSkillMirrorState, error) {
	return curatorSkillMirrorStatusReadOnly(root, host, configDir)
}

func curatorSkillStartup(in hookInput) (hookResult, error) {
	declaredRoot := strings.TrimSpace(os.Getenv("AIR_WORKER_PLUGIN_ROOT"))
	if declaredRoot == "" {
		declaredRoot = strings.TrimSpace(os.Getenv("CLAUDE_PLUGIN_ROOT"))
	}
	root, err := curatorPluginRoot("")
	if err != nil {
		// A standalone installed binary has no plugin payload root. That is not a
		// SessionStart integrity failure unless the host explicitly declared one.
		if declaredRoot == "" {
			return hookResult{}, nil
		}
		return hookResult{}, &curatorSkillHookError{Err: err}
	}
	health := inspectCuratorSkillPackage(root)
	if health.Status != curatorSkillStatusPass {
		return hookResult{}, &curatorSkillHookError{Err: fmt.Errorf("package health failed: %s", strings.Join(health.Violations, "; "))}
	}
	reg, err := loadCuratorSkillRegistry(root)
	if err != nil {
		return hookResult{}, &curatorSkillHookError{Err: err}
	}
	host := strings.ToLower(strings.TrimSpace(hookPrincipal(in)))
	var configDir string
	switch host {
	case "claude":
		configDir = claudeConfigDir()
	case "codex":
		configDir = codexConfigDir()
	default:
		// Host-neutral adapters still receive registry discovery context; only
		// Claude/Codex have managed mirror roots in the current contract.
		return hookResult{Context: curatorSkillStartupContext(reg)}, nil
	}
	if _, err := syncCuratorSkillMirrors(root, host, configDir); err != nil {
		return hookResult{}, &curatorSkillHookError{Err: err}
	}
	return hookResult{Context: curatorSkillStartupContext(reg)}, nil
}
