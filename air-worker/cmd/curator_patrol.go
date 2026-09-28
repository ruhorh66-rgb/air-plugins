package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type curatorPatrolWindow struct {
	Window        string   `json:"window"`
	URL           string   `json:"url"`
	Product       string   `json:"product"`
	Repo          string   `json:"repo,omitempty"`
	Commit        string   `json:"commit,omitempty"`
	CommitAt      string   `json:"commit_at,omitempty"`
	Subject       string   `json:"subject,omitempty"`
	Files30m      []string `json:"files_30m"`
	DirtyFiles30m int      `json:"dirty_files_30m"`
	State         string   `json:"state"`
	Error         string   `json:"error,omitempty"`
}

type curatorPatrolCoverage struct {
	Schema   string                `json:"schema"`
	Source   string                `json:"source"`
	Expected int                   `json:"expected"`
	Checked  int                   `json:"checked"`
	Complete bool                  `json:"complete"`
	Summary  string                `json:"summary"`
	Judge    string                `json:"judge"`
	Rows     []curatorPatrolWindow `json:"rows"`
}

type patrolRepoSnapshot struct {
	Path        string
	Commit      string
	CommitAt    time.Time
	Subject     string
	Files30m    []string
	DirtyTotal  int
	DirtyRecent int
	State       string
}

func buildCuratorPatrolCoverage(root string, now time.Time) (*curatorPatrolCoverage, bool, error) {
	matches, err := filepath.Glob(filepath.Join(root, planNodeDirName, "N-012*.md"))
	if err != nil {
		return nil, false, err
	}
	if len(matches) == 0 {
		return nil, false, nil
	}
	if len(matches) != 1 {
		return nil, true, fmt.Errorf("patrol coverage requires exactly one N-012 node, found %d", len(matches))
	}
	entries, err := parseCuratorWindowNode(matches[0])
	if err != nil {
		return nil, true, err
	}
	cov := &curatorPatrolCoverage{
		Schema:   "air-worker.curator.patrol/v1",
		Source:   filepath.ToSlash(matches[0]),
		Expected: len(entries),
		Rows:     make([]curatorPatrolWindow, 0, len(entries)),
	}
	for _, entry := range entries {
		row := entry
		snap, snapErr := newestPatrolRepoSnapshot(entry.Product, now)
		if snapErr != nil {
			row.State = "unresolved"
			row.Error = snapErr.Error()
			cov.Rows = append(cov.Rows, row)
			continue
		}
		row.Repo = filepath.Clean(snap.Path)
		row.Commit = snap.Commit
		row.CommitAt = snap.CommitAt.Format(time.RFC3339)
		row.Subject = snap.Subject
		row.Files30m = snap.Files30m
		row.DirtyFiles30m = snap.DirtyRecent
		row.State = snap.State
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(entry.Window)), "(пауза)") {
			row.State = "paused"
		}
		cov.Checked++
		cov.Rows = append(cov.Rows, row)
	}
	cov.Complete = cov.Expected > 0 && cov.Checked == cov.Expected
	cov.Summary = fmt.Sprintf("проверено %d из %d", cov.Checked, cov.Expected)
	if cov.Complete {
		cov.Judge = "PASS"
	} else {
		cov.Judge = "REJECT"
	}
	return cov, true, nil
}

func parseCuratorWindowNode(path string) ([]curatorPatrolWindow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var rows []curatorPatrolWindow
	seen := map[string]bool{}
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		parts := strings.SplitN(body, " — ", 2)
		if len(parts) != 2 {
			continue
		}
		label := strings.TrimSpace(parts[0])
		url := strings.Fields(strings.TrimSpace(parts[1]))
		if len(url) == 0 || !strings.HasPrefix(url[0], "https://chatgpt.com/") {
			continue
		}
		if !(strings.HasPrefix(label, "AC·") || strings.HasPrefix(strings.ToLower(label), "(пауза)")) {
			continue
		}
		product := patrolProductKey(label)
		if product == "" {
			return nil, fmt.Errorf("cannot resolve patrol product from window %q", label)
		}
		key := label + "\x00" + url[0]
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, curatorPatrolWindow{
			Window: label, URL: url[0], Product: product, Files30m: []string{}, State: "unknown",
		})
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("N-012 has no patrol window entries")
	}
	return rows, nil
}

func patrolProductKey(label string) string {
	lower := strings.ToLower(label)
	switch {
	case strings.Contains(lower, "airstorage"):
		return "AirStorage"
	case strings.Contains(lower, "airworker"):
		return "AirWorker"
	case strings.Contains(lower, "airguard"):
		return "AirGuard"
	case strings.Contains(lower, "airlegal"):
		return "AirLegal"
	case strings.Contains(lower, "airhr"):
		return "AirHR"
	case strings.Contains(lower, "airsync"):
		return "AirSync"
	case strings.Contains(lower, "airks"):
		return "AIRKS"
	case strings.Contains(lower, "asw"):
		return "ASW"
	default:
		return ""
	}
}

func patrolRepoPatterns(product string) []string {
	if raw := strings.TrimSpace(os.Getenv("AIR_WORKER_PATROL_REPOS_JSON")); raw != "" {
		var custom map[string][]string
		if json.Unmarshal([]byte(raw), &custom) == nil && len(custom[product]) > 0 {
			return custom[product]
		}
	}
	switch product {
	case "AirGuard":
		return []string{"F:/-8-/airguard/_wt/*", "F:/-8-/_worktrees/airguard*", "F:/-8-/airguard"}
	case "AirStorage":
		return []string{"F:/-5-/011_Plugins/_worktrees/air-storage-*", "F:/-5-/011_Plugins/AirStorage_Wiki"}
	case "AirWorker":
		return []string{"F:/-7-/_worktrees/air-worker-*", "F:/-5-/011_Plugins/AirWorker_Wiki", "F:/-7-"}
	case "AirLegal":
		return []string{"F:/-5-/011_Plugins/_worktrees/airlegal-*", "F:/-5-/011_Plugins/AirLegal_Wiki"}
	case "AirHR":
		return []string{"F:/-5-/011_Plugins/_worktrees/airhr-*", "F:/-5-/011_Plugins/AirHR_Wiki"}
	case "AirSync":
		return []string{"F:/-8-/_worktrees/airsync*", "F:/-8-/airsync", "F:/-5-/011_Plugins/AirSync_Wiki"}
	case "ASW":
		return []string{"F:/-8-/_worktrees/asw*", "F:/-8-/asw"}
	case "AIRKS":
		return []string{"F:/-8-/airks/_wt/*", "F:/-8-/_worktrees/airks*", "F:/-8-/airks"}
	default:
		return nil
	}
}

func expandPatrolRepoPatterns(patterns []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range patterns {
		p := filepath.FromSlash(raw)
		var candidates []string
		if strings.ContainsAny(p, "*?[") {
			matches, _ := filepath.Glob(p)
			candidates = matches
		} else {
			candidates = []string{p}
		}
		for _, candidate := range candidates {
			if seen[candidate] {
				continue
			}
			st, err := os.Stat(candidate)
			if err != nil || !st.IsDir() {
				continue
			}
			seen[candidate] = true
			out = append(out, candidate)
		}
	}
	return out
}

func newestPatrolRepoSnapshot(product string, now time.Time) (*patrolRepoSnapshot, error) {
	candidates := expandPatrolRepoPatterns(patrolRepoPatterns(product))
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no repository candidates for %s", product)
	}
	var best *patrolRepoSnapshot
	var errs []string
	fileSet := map[string]bool{}
	totalDirtyRecent := 0
	anyActive := false
	anyDirty := false
	for _, candidate := range candidates {
		snap, err := patrolSnapshot(candidate, now)
		if err != nil {
			errs = append(errs, candidate+": "+err.Error())
			continue
		}
		if best == nil || snap.CommitAt.After(best.CommitAt) {
			copySnap := *snap
			best = &copySnap
		}
		totalDirtyRecent += snap.DirtyRecent
		if snap.State == "active" {
			anyActive = true
		}
		if snap.DirtyTotal > 0 {
			anyDirty = true
		}
		repoLabel := filepath.Base(filepath.Clean(snap.Path))
		for _, file := range snap.Files30m {
			fileSet[repoLabel+":"+file] = true
		}
	}
	if best == nil {
		return nil, fmt.Errorf("no readable git repository for %s: %s", product, strings.Join(errs, "; "))
	}
	best.Files30m = best.Files30m[:0]
	for file := range fileSet {
		best.Files30m = append(best.Files30m, file)
	}
	sort.Strings(best.Files30m)
	best.DirtyRecent = totalDirtyRecent
	switch {
	case anyActive || totalDirtyRecent > 0:
		best.State = "active"
	case anyDirty:
		best.State = "stale-dirty"
	default:
		best.State = "idle"
	}
	return best, nil
}

func patrolSnapshot(repo string, now time.Time) (*patrolRepoSnapshot, error) {
	top, err := patrolGit(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	repo = strings.TrimSpace(top)
	line, err := patrolGit(repo, "log", "-1", "--format=%h%x09%cI%x09%s")
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(strings.TrimSpace(line), "\t", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("unexpected git log format")
	}
	commitAt, err := time.Parse(time.RFC3339, parts[1])
	if err != nil {
		return nil, fmt.Errorf("parse commit time: %w", err)
	}
	subject := ""
	if len(parts) == 3 {
		subject = parts[2]
	}
	cutoffTime := now.Add(-30 * time.Minute)
	cutoff := cutoffTime.Format(time.RFC3339)
	filesRaw, filesErr := patrolGit(repo, "log", "--since="+cutoff, "--name-only", "--pretty=format:", "--diff-filter=ACMRT")
	if filesErr != nil {
		return nil, filesErr
	}
	fileSet := map[string]bool{}
	for _, line := range strings.Split(strings.ReplaceAll(filesRaw, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			fileSet[filepath.ToSlash(line)] = true
		}
	}

	statusRaw, err := patrolGit(repo, "status", "--porcelain")
	if err != nil {
		return nil, err
	}
	dirtyTotal := 0
	for _, line := range strings.Split(strings.ReplaceAll(statusRaw, "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			dirtyTotal++
		}
	}
	dirtyRecent, err := patrolRecentDirtyFiles(repo, cutoffTime)
	if err != nil {
		return nil, err
	}
	for _, file := range dirtyRecent {
		fileSet[file] = true
	}

	files := make([]string, 0, len(fileSet))
	for file := range fileSet {
		files = append(files, file)
	}
	sort.Strings(files)

	state := "idle"
	if !commitAt.Before(cutoffTime) || len(files) > 0 {
		state = "active"
	} else if dirtyTotal > 0 {
		state = "stale-dirty"
	}
	return &patrolRepoSnapshot{
		Path: repo, Commit: parts[0], CommitAt: commitAt, Subject: subject,
		Files30m: files, DirtyTotal: dirtyTotal, DirtyRecent: len(dirtyRecent), State: state,
	}, nil
}

func patrolRecentDirtyFiles(repo string, cutoff time.Time) ([]string, error) {
	fileSet := map[string]bool{}
	for _, args := range [][]string{
		{"diff", "--name-only"},
		{"diff", "--cached", "--name-only"},
		{"ls-files", "--others", "--exclude-standard"},
	} {
		raw, err := patrolGit(repo, args...)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			path := filepath.Join(repo, filepath.FromSlash(line))
			st, err := os.Stat(path)
			if err == nil && !st.ModTime().Before(cutoff) {
				fileSet[filepath.ToSlash(line)] = true
			}
		}
	}
	out := make([]string, 0, len(fileSet))
	for file := range fileSet {
		out = append(out, file)
	}
	sort.Strings(out)
	return out, nil
}

func patrolGit(repo string, args ...string) (string, error) {
	cmdArgs := append([]string{"-C", repo}, args...)
	cmd := exec.Command("git", cmdArgs...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
