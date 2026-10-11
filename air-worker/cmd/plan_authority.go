package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// shortcut: production new/close stay blocked, enable after an atomic authority protocol exists.
// It must reserve IDs and fence PLAN/LEARN publication across branches and machines.
// A read-only remote HEAD check plus a per-product mutex cannot provide that.
// The only supported contract is a disposable, offline fixture: an exact root
// under OS temp, outside Git, explicitly declared in plan-authority.json:
// {"schema":"air-worker.plan.authority/v1","mode":"isolated-fixture","root":"<absolute physical path>"}
// This mode preserves transaction tests; it makes no production authority claim.
func planNodeAuthorityReason(root string) string {
	physical, err := resolveLinks(root)
	if err != nil {
		return "authority_unreadable"
	}
	for _, key := range []string{"GIT_DIR", "GIT_COMMON_DIR", "GIT_WORK_TREE"} {
		if os.Getenv(key) != "" {
			return "git_authority_unproven"
		}
	}
	// Inspect metadata without invoking Git: missing/broken Git or an unreachable
	// remote must never turn a repository into an allowed offline fixture.
	for dir := physical; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return "git_authority_unproven"
		} else if !os.IsNotExist(err) {
			return "authority_unreadable"
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	path := filepath.Join(physical, "plan-authority.json")
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return "authority_missing"
	}
	if err != nil {
		return "authority_unreadable"
	}
	if !info.Mode().IsRegular() || info.Size() > 4096 {
		return "authority_invalid"
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "authority_unreadable"
	}
	var contract struct {
		Schema string `json:"schema"`
		Mode   string `json:"mode"`
		Root   string `json:"root"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&contract) != nil || decoder.Decode(new(any)) != io.EOF ||
		contract.Schema != "air-worker.plan.authority/v1" || contract.Mode != "isolated-fixture" ||
		contract.Root != physical {
		return "authority_invalid"
	}
	temp, err := resolveLinks(os.TempDir())
	if err != nil {
		return "authority_unreadable"
	}
	rel, err := filepath.Rel(temp, physical)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "authority_invalid"
	}
	nodes, err := listPlanNodes(physical)
	if err != nil {
		return "node_identity_conflict"
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		match := planNodeIDRE.FindStringSubmatch(n.ID)
		if match == nil {
			return "node_identity_conflict"
		}
		number := match[1]
		if seen[number] || filepath.Base(n.Path) != n.ID+".md" {
			return "node_identity_conflict"
		}
		seen[number] = true
	}
	return ""
}

func rejectUnprovenPlanAuthority(root string, asJSON bool) bool {
	reason := planNodeAuthorityReason(root)
	if reason == "" {
		return false
	}
	output := os.Stderr
	if asJSON {
		output = os.Stdout
	}
	_ = json.NewEncoder(output).Encode(map[string]string{
		"schema": "air-worker.plan.node.mutation/v1", "action": "conflict", "reason": reason,
		"error": "PLAN authority is unproven; production new/close require an atomic cross-branch authority protocol",
	})
	return true
}
