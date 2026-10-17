package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	planManagedHeaderBegin = "<!-- air-worker-managed-header:start -->"
	planManagedHeaderEnd   = "<!-- air-worker-managed-header:end -->"
)

var (
	planHeaderGitSHARegex   = regexp.MustCompile("^[0-9a-f]{40}$")
	planHeaderFileSHARegex  = regexp.MustCompile("^[0-9a-f]{64}$")
	planHeaderStableVersion = regexp.MustCompile("^(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$")
	planHeaderBranchRegex   = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$")
)

type planHeaderFacts struct {
	Branch                string
	Head                  string
	RemoteHead            string
	InstalledVersion      string
	InstalledBinarySHA256 string
	ActiveRelease         string
	NextOpenID            string
}

func planHeaderSHA(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func safePlanHeaderText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, c := range s {
		if c < 0x20 || (c >= 0x7f && c <= 0x9f) {
			return false
		}
	}
	return true
}

func validatePlanHeaderFacts(s planHeaderFacts) error {
	if !planHeaderGitSHARegex.MatchString(s.Head) || !planHeaderGitSHARegex.MatchString(s.RemoteHead) || s.Head != s.RemoteHead {
		return errors.New("Git authority HEAD and remote HEAD are missing or different")
	}
	if !planHeaderBranchRegex.MatchString(s.Branch) || strings.Contains(s.Branch, "..") ||
		strings.HasSuffix(s.Branch, ".") || strings.Contains(s.Branch, "//") {
		return errors.New("invalid or unsafe Git branch")
	}
	if !planHeaderStableVersion.MatchString(s.InstalledVersion) || !planHeaderStableVersion.MatchString(s.ActiveRelease) {
		return errors.New("missing or unverified numeric product/release version")
	}
	if !planHeaderFileSHARegex.MatchString(s.InstalledBinarySHA256) {
		return errors.New("installed binary SHA is not verified")
	}
	if s.NextOpenID != "NONE" && !planNodeIDRE.MatchString(s.NextOpenID) {
		return errors.New("next node identity is not a canonical N-ID")
	}
	for _, value := range []string{s.Branch, s.Head, s.RemoteHead, s.InstalledVersion, s.ActiveRelease, s.NextOpenID} {
		if !safePlanHeaderText(value) {
			return errors.New("unsafe characters in PLAN header values")
		}
	}
	return nil
}

func renderManagedPlanHeader(s planHeaderFacts, newline string) ([]byte, error) {
	if err := validatePlanHeaderFacts(s); err != nil {
		return nil, err
	}
	if newline != "\n" && newline != "\r\n" {
		return nil, errors.New("invalid newline convention")
	}
	lines := []string{
		"- **Git branch:** `" + s.Branch + "`",
		"- **Git HEAD:** `" + s.Head + "`",
		"- **Verified installed version:** `" + s.InstalledVersion + "`",
		"- **Installed binary SHA256:** `" + s.InstalledBinarySHA256 + "`",
		"- **Active release:** `" + s.ActiveRelease + "`",
		"- **Next OPEN node:** `" + s.NextOpenID + "`",
	}
	return []byte(newline + strings.Join(lines, newline) + newline), nil
}

// Byte-preserving transform. A legacy PLAN requires explicit bootstrap.
func replaceManagedPlanHeader(raw []byte, facts planHeaderFacts, bootstrap bool) ([]byte, bool, error) {
	if !utf8.Valid(raw) {
		return nil, false, errors.New("PLAN is not valid UTF-8")
	}
	for _, r := range string(raw) {
		if (r < 32 && r != '\n' && r != '\r' && r != '\t') || (r >= 0x7f && r <= 0x9f) {
			return nil, false, errors.New("PLAN contains disallowed C0/C1 control")
		}
	}
	begin, end := []byte(planManagedHeaderBegin), []byte(planManagedHeaderEnd)
	nBegin, nEnd := bytes.Count(raw, begin), bytes.Count(raw, end)
	nl := "\n"
	if bytes.Contains(raw, []byte("\r\n")) {
		nl = "\r\n"
	}
	block, err := renderManagedPlanHeader(facts, nl)
	if err != nil {
		return nil, false, err
	}
	if bootstrap {
		if nBegin != 0 || nEnd != 0 {
			return nil, false, errors.New("PLAN already managed or markers broken")
		}
		idx := bytes.IndexByte(raw, '\n')
		if idx < 3 || raw[0] != '#' || raw[1] != ' ' {
			return nil, false, errors.New("bootstrap requires first-level heading")
		}
		prefix := append([]byte(nil), raw[:idx+1]...)
		segment := append([]byte(planManagedHeaderBegin), block...)
		segment = append(segment, []byte(planManagedHeaderEnd+nl)...)
		updated := append(prefix, segment...)
		updated = append(updated, raw[idx+1:]...)
		return updated, true, nil
	}
	if nBegin != 1 || nEnd != 1 {
		return nil, false, errors.New("PLAN managed header needs one marker pair")
	}
	i, j := bytes.Index(raw, begin), bytes.Index(raw, end)
	if i < 0 || j <= i+len(begin) {
		return nil, false, errors.New("PLAN managed header order invalid")
	}
	before, after := raw[:i+len(begin)], raw[j:]
	updated := make([]byte, 0, len(before)+len(block)+len(after))
	updated = append(updated, before...)
	updated = append(updated, block...)
	updated = append(updated, after...)
	return updated, !bytes.Equal(raw, updated), nil
}

// The caller holds the native plan-nodes lock and rechecks HEAD/branch/remote.
func writeManagedPlanHeaderCAS(path, expectedSHA string, desired []byte, beforePublish func() error) error {
	if !planHeaderFileSHARegex.MatchString(expectedSHA) {
		return errors.New("missing expected PLAN SHA")
	}
	st, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("PLAN is not a regular file")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if planHeaderSHA(before) != expectedSHA {
		return errors.New("stale PLAN SHA")
	}
	if beforePublish != nil {
		if err := beforePublish(); err != nil {
			return err
		}
	}
	latestInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(st, latestInfo) {
		return errors.New("PLAN file identity changed")
	}
	latest, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if planHeaderSHA(latest) != expectedSHA || !bytes.Equal(before, latest) {
		return errors.New("concurrent PLAN change")
	}
	if bytes.Equal(latest, desired) {
		return nil
	}
	if err := writeFileAtomicDurable(path, desired); err != nil {
		return fmt.Errorf("atomic PLAN header update: %w", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, desired) {
		return errors.New("PLAN header readback mismatch")
	}
	return nil
}

var managedPlanSpineLine = regexp.MustCompile("^- \\[(open|blocked|closed)\\].*\\[\\[(N-[0-9]{3}_[^\\]]+)\\]\\]")

// selectCanonicalNextOpenNode only uses the native spine order, never an
// arbitrary lexicographic sort or text heading. This local preflight does
// not replace global N124 Git authority reconciliation.
func selectCanonicalNextOpenNode(root string, plan []byte) (string, error) {
	if !utf8.Valid(plan) {
		return "", errors.New("PLAN is not valid UTF-8")
	}
	if bytes.Count(plan, []byte(planNodeMarker)) != 1 {
		return "", errors.New("missing or ambiguous native plan spine")
	}
	nodes, err := listPlanNodes(root)
	if err != nil {
		return "", err
	}
	seen := make(map[string]planNode, len(nodes))
	numeric := make(map[string]string, len(nodes))
	for _, node := range nodes {
		if !safePlanHeaderText(node.ID) || !safePlanHeaderText(node.Title) ||
			!safePlanHeaderText(node.Trigger) || !safePlanHeaderText(node.DoneWhen) ||
			!safePlanHeaderText(node.Owner) || !safePlanHeaderText(node.Parent) {
			return "", errors.New("unsafe C0/C1 or Unicode in native node")
		}
		full := node.ID
		if !planNodeIDRE.MatchString(full) {
			return "", errors.New("invalid native node ID")
		}
		if filepath.Base(node.Path) != full+".md" {
			return "", errors.New("node filename disagrees with native ID")
		}
		st, err := os.Lstat(node.Path)
		if err != nil {
			return "", err
		}
		if !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("native node file is not regular")
		}
		parts := planNodeIDRE.FindStringSubmatch(full)
		if prev, exists := numeric[parts[1]]; exists && prev != full {
			return "", errors.New("duplicate numeric native N-ID")
		}
		numeric[parts[1]] = full
		if _, exists := seen[full]; exists {
			return "", errors.New("duplicate native N-ID")
		}
		seen[full] = node
	}
	position := bytes.Index(plan, []byte(planNodeMarker))
	var firstOpen string
	matched := make(map[string]bool)
	for _, line := range strings.Split(string(plan[position+len(planNodeMarker):]), "\n") {
		match := managedPlanSpineLine.FindStringSubmatch(strings.TrimSuffix(line, "\r"))
		if match == nil {
			continue
		}
		state, id := match[1], match[2]
		node, exists := seen[id]
		if !exists {
			return "", errors.New("spine references unknown native node")
		}
		if matched[id] || state != node.Status {
			return "", errors.New("spine duplicates or contradicts node status")
		}
		matched[id] = true
		if state == "open" && firstOpen == "" {
			firstOpen = id
		}
	}
	if len(matched) != len(seen) {
		return "", errors.New("spine missing native node")
	}
	if firstOpen == "" {
		return "NONE", nil
	}
	return firstOpen, nil
}
