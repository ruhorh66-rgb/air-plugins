package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"time"
)

const (
	n124AuditSchema = "air-worker.plan.authority-audit/v1"
	// Source-only scope: a fixed repository, not a configurable authority grant.
	n124AuditRepo = "https://github.com/ruhorh66-rgb/air-plugins.git"
)

type planAuditNode struct {
	ID      string `json:"id"`
	Number  string `json:"number"`
	SHA256  string `json:"sha256"`
	BlobOID string `json:"blob_oid"`
}

type planAuditCollision struct {
	Number       string `json:"number"`
	CanonicalID  string `json:"canonical_id"`
	PeerID       string `json:"peer_id"`
	Reason       string `json:"reason"`
	CanonicalSHA string `json:"canonical_sha256"`
	PeerSHA      string `json:"peer_sha256"`
}

type planAuditResult struct {
	Schema             string               `json:"schema"`
	Status             string               `json:"status"`
	ExitCode           int                  `json:"exit_code"`
	AuthorityGranted   bool                 `json:"authority_granted"`
	Changed            bool                 `json:"changed"`
	Scope              string               `json:"scope"`
	Product            string               `json:"product,omitempty"`
	CanonicalRef       string               `json:"canonical_ref,omitempty"`
	CanonicalHead      string               `json:"canonical_head,omitempty"`
	PeerRef            string               `json:"peer_ref,omitempty"`
	PeerHead           string               `json:"peer_head,omitempty"`
	CanonicalNodeCount int                  `json:"canonical_node_count"`
	PeerNodeCount      int                  `json:"peer_node_count"`
	Collisions         []planAuditCollision `json:"collisions"`
	Blockers           []string             `json:"blockers"`
}

type planAuditSnapshot struct {
	Head  string
	Nodes []planAuditNode
}

func planAuditCompare(canonical, peer planAuditSnapshot) (string, []planAuditCollision, error) {
	if !candidateOIDRE.MatchString(canonical.Head) || !candidateOIDRE.MatchString(peer.Head) {
		return "NOT_PROVEN", nil, errors.New("unverified Git commit identity")
	}
	checked := func(s planAuditSnapshot) (map[string]planAuditNode, error) {
		out := make(map[string]planAuditNode, len(s.Nodes))
		ids := make(map[string]bool, len(s.Nodes))
		for _, node := range s.Nodes {
			match := planNodeIDRE.FindStringSubmatch(node.ID)
			if match == nil || node.Number != match[1] ||
				!candidateOIDRE.MatchString(node.BlobOID) || !planAuditSHA256Valid(node.SHA256) {
				return nil, errors.New("invalid node/number/blob/digest")
			}
			if _, exists := out[node.Number]; exists || ids[node.ID] {
				return nil, errors.New("duplicate node number or ID inside one branch")
			}
			out[node.Number], ids[node.ID] = node, true
		}
		return out, nil
	}
	c, err := checked(canonical)
	if err != nil {
		return "NOT_PROVEN", nil, fmt.Errorf("canonical snapshot: %w", err)
	}
	p, err := checked(peer)
	if err != nil {
		return "NOT_PROVEN", nil, fmt.Errorf("peer snapshot: %w", err)
	}
	conflicts := make([]planAuditCollision, 0)
	for number, n := range c {
		other, ok := p[number]
		if !ok || (other.ID == n.ID && other.SHA256 == n.SHA256) {
			continue
		}
		reason := "same_id_content_diverged"
		if other.ID != n.ID {
			reason = "numeric_id_reallocated"
		}
		conflicts = append(conflicts, planAuditCollision{
			Number: number, CanonicalID: n.ID, PeerID: other.ID, Reason: reason,
			CanonicalSHA: n.SHA256, PeerSHA: other.SHA256,
		})
	}
	sort.Slice(conflicts, func(i, j int) bool { return conflicts[i].Number < conflicts[j].Number })
	if len(conflicts) != 0 {
		return "CONFLICT", conflicts, nil
	}
	// Even identical local snapshots have NOT verified a trusted global publisher.
	return "NO_COLLISIONS_LOCAL_ONLY", conflicts, nil
}

func planAuditSHA256Valid(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			if c < 'a' || c > 'f' {
				return false
			}
		}
	}
	return true
}

func planAuditGit(root string, args ...string) ([]byte, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, git, append([]string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "credential.interactive=never"}, args...)...)
	cmd.WaitDelay = time.Second
	env := make([]string, 0, len(os.Environ())+7)
	for _, item := range os.Environ() {
		key, _, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		upper := strings.ToUpper(key)
		if strings.HasPrefix(upper, "GIT_") || strings.HasPrefix(upper, "GCM_") ||
			strings.HasPrefix(upper, "SSH_") || upper == "PAGER" {
			continue
		}
		env = append(env, item)
	}
	cmd.Env = append(env, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_NO_LAZY_FETCH=1",
		"GIT_NO_REPLACE_OBJECTS=1", "GIT_OPTIONAL_LOCKS=0")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("read-only Git %s failed: %w (%s)", args[0], err, stderr.String())
	}
	return out, nil
}

func planAuditBranchName(value string) bool {
	if value == "" || len(value) > 150 || strings.HasPrefix(value, "-") ||
		strings.HasPrefix(value, "/") || strings.HasSuffix(value, "/") ||
		strings.HasSuffix(value, ".") || strings.Contains(value, "..") ||
		strings.Contains(value, "//") || strings.Contains(value, "@{") ||
		strings.Contains(value, "\\") || strings.ContainsAny(value, " :?*[]~^\r\n\t\x00") {
		return false
	}
	for _, c := range value {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' || c == '/') {
			return false
		}
	}
	return !strings.HasSuffix(value, ".lock")
}

// Never let a product-controlled local Git config rewrite our pinned GitHub
// HTTPS address or start a credential/proxy helper for this read-only audit.
// This is still NOT a GitHub trust grant; it is a hardening preflight only.
func planAuditLocalGitConfigSafe(root string) error {
	raw, err := planAuditGit(root, "config", "--local", "--null", "--list")
	if err != nil {
		return err
	}
	for _, part := range bytes.Split(raw, []byte{0}) {
		if len(part) == 0 {
			continue
		}
		key, _, ok := strings.Cut(string(part), "\n")
		key = strings.ToLower(key)
		if !ok {
			return errors.New("invalid local Git configuration shape")
		}
		if strings.HasPrefix(key, "url.") || strings.HasPrefix(key, "include.") ||
			strings.HasPrefix(key, "includeif.") || strings.HasPrefix(key, "credential.") ||
			strings.HasPrefix(key, "http.") || key == "core.sshcommand" ||
			key == "remote.github.pushurl" || key == "remote.github.mirror" {
			return errors.New("untrusted Git configuration could redirect the pinned publisher")
		}
	}
	return nil
}

func planAuditRemoteHead(root, branch string) (string, error) {
	if !planAuditBranchName(branch) {
		return "", errors.New("unsafe Git branch")
	}
	ref := "refs/heads/" + branch
	raw, err := planAuditGit(root, "ls-remote", "--heads", n124AuditRepo, ref)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(raw))
	if len(fields) != 2 || !candidateOIDRE.MatchString(fields[0]) || fields[1] != ref {
		return "", errors.New("GitHub response is missing, ambiguous, or malformed")
	}
	return fields[0], nil
}

func planAuditReadSnapshot(root, prefix, commit string) (planAuditSnapshot, error) {
	if !candidateOIDRE.MatchString(commit) {
		return planAuditSnapshot{}, errors.New("invalid commit SHA")
	}
	typ, err := planAuditGit(root, "cat-file", "-t", commit)
	if err != nil || strings.TrimSpace(string(typ)) != "commit" {
		return planAuditSnapshot{}, errors.New("commit object unavailable")
	}
	name := prefix + "PLAN.md"
	planRaw, err := planAuditGit(root, "show", commit+":"+name)
	if err != nil || !bytes.Contains(planRaw, []byte(planNodeMarker)) {
		return planAuditSnapshot{}, errors.New("canonical PLAN marker unavailable")
	}
	paths, err := planAuditGit(root, "ls-tree", "-r", "-z", "--full-tree", commit, "--", prefix+"plan/")
	if err != nil {
		return planAuditSnapshot{}, err
	}
	snapshot := planAuditSnapshot{Head: commit, Nodes: make([]planAuditNode, 0)}
	for _, row := range bytes.Split(paths, []byte{0}) {
		if len(row) == 0 {
			continue
		}
		meta, file, ok := bytes.Cut(row, []byte{'\t'})
		if !ok {
			return planAuditSnapshot{}, errors.New("malformed Git tree entry")
		}
		parts := strings.Fields(string(meta))
		filePath := string(file)
		if len(parts) != 3 || parts[0] != "100644" || parts[1] != "blob" ||
			!candidateOIDRE.MatchString(parts[2]) {
			return planAuditSnapshot{}, errors.New("nonregular or ambiguous Git node tree")
		}
		local := strings.TrimPrefix(filePath, prefix)
		if !strings.HasPrefix(filePath, prefix) || !strings.HasPrefix(local, "plan/") {
			return planAuditSnapshot{}, errors.New("Git node escaped product prefix")
		}
		base := strings.TrimPrefix(local, "plan/")
		if !strings.HasPrefix(base, "N-") {
			continue
		} // release-slices and event-outbox are not nodes.
		if path.Dir(base) != "." || !strings.HasSuffix(base, ".md") {
			return planAuditSnapshot{}, errors.New("ambiguous native node filename")
		}
		id := strings.TrimSuffix(base, ".md")
		match := planNodeIDRE.FindStringSubmatch(id)
		if match == nil {
			return planAuditSnapshot{}, errors.New("invalid native node identifier")
		}
		content, err := planAuditGit(root, "cat-file", "blob", parts[2])
		if err != nil {
			return planAuditSnapshot{}, errors.New("native node Git blob unreadable")
		}
		n, err := parsePlanNode(local, content)
		if err != nil || n.ID != id {
			return planAuditSnapshot{}, errors.New("native node filename/ID/content mismatch")
		}
		if bytes.Count(planRaw, []byte("[["+id+"]]")) != 1 {
			return planAuditSnapshot{}, errors.New("native node does not have exactly one canonical PLAN spine entry")
		}
		sum := sha256.Sum256(content)
		snapshot.Nodes = append(snapshot.Nodes, planAuditNode{
			ID: id, Number: match[1], BlobOID: parts[2], SHA256: hex.EncodeToString(sum[:]),
		})
	}
	if len(snapshot.Nodes) == 0 {
		return planAuditSnapshot{}, errors.New("empty native plan snapshot")
	}
	return snapshot, nil
}

// cmdPlanAuthorityAudit cannot grant source authority or write a Git ref. It
// intentionally returns RC2 on clean snapshots: remote identity plus local Git
// objects alone cannot attest a global publisher or an installed binary.
func cmdPlanAuthorityAudit(argv []string) int {
	fs := flag.NewFlagSet("plan authority audit", flag.ContinueOnError)
	product := fs.String("product", ".", "product source root")
	canonicalBranch := fs.String("canonical-branch", "", "GitHub branch, explicit full name")
	canonicalSHA := fs.String("canonical-head", "", "expected 40-char GitHub SHA")
	peerBranch := fs.String("peer-branch", "", "second GitHub branch")
	peerSHA := fs.String("peer-head", "", "expected 40-char GitHub SHA")
	asJSON := fs.Bool("json", false, "machine JSON")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	out := planAuditResult{
		Schema: n124AuditSchema, Status: "NOT_PROVEN", ExitCode: 2,
		Scope:    "read_only_git_branch_snapshot_no_write_no_authority",
		Blockers: []string{}, Collisions: []planAuditCollision{},
		CanonicalRef: *canonicalBranch, CanonicalHead: *canonicalSHA,
		PeerRef: *peerBranch, PeerHead: *peerSHA,
	}
	block := func(reason string) { out.Blockers = append(out.Blockers, reason) }
	if fs.NArg() != 0 || !planAuditBranchName(*canonicalBranch) ||
		!planAuditBranchName(*peerBranch) || *canonicalBranch == *peerBranch ||
		!candidateOIDRE.MatchString(*canonicalSHA) || !candidateOIDRE.MatchString(*peerSHA) {
		block("invalid_or_ambiguous_pinned_branch_identity")
	} else {
		root, err := normalizePlanNodeProduct(*product)
		if err != nil {
			block("invalid_product_root")
		} else {
			out.Product = root
			if err := candidateNoLinks(root); err != nil {
				block("untrusted_product_root_or_link")
			} else {
				prefixBytes, err := planAuditGit(root, "rev-parse", "--show-prefix")
				if err != nil {
					block("git_product_prefix_unavailable")
				} else {
					prefix := strings.TrimSpace(string(prefixBytes))
					if prefix != "" && (!strings.HasSuffix(prefix, "/") ||
						strings.Contains(prefix, "..") || strings.Contains(prefix, "\\")) {
						block("unsafe_git_product_prefix")
					} else {
						remote, e := planAuditGit(root, "config", "--local", "--get", "remote.github.url")
						if e != nil || strings.TrimSpace(string(remote)) != n124AuditRepo {
							block("github_remote_binding_not_pinned")
						} else if err := planAuditLocalGitConfigSafe(root); err != nil {
							block("git_local_config_redirect_or_credential_untrusted")
						} else {
							head, e := planAuditGit(root, "rev-parse", "--verify", "HEAD")
							if e != nil || strings.TrimSpace(string(head)) != *canonicalSHA {
								block("local_head_not_pinned_to_canonical")
							}
							status, e := planAuditGit(root, "status", "--porcelain=v1", "--untracked-files=all", "--", "PLAN.md", "plan")
							if e != nil || strings.TrimSpace(string(status)) != "" {
								block("local_native_plan_dirty_or_status_unavailable")
							}
							gotC, errC := planAuditRemoteHead(root, *canonicalBranch)
							gotP, errP := planAuditRemoteHead(root, *peerBranch)
							if errC != nil || errP != nil || gotC != *canonicalSHA || gotP != *peerSHA {
								block("github_branch_head_unverified_or_diverged")
							} else {
								c, errC := planAuditReadSnapshot(root, prefix, *canonicalSHA)
								p, errP := planAuditReadSnapshot(root, prefix, *peerSHA)
								if errC != nil || errP != nil {
									block("immutable_git_node_snapshot_unavailable_or_invalid")
								} else {
									out.CanonicalNodeCount, out.PeerNodeCount = len(c.Nodes), len(p.Nodes)
									result, collisions, err := planAuditCompare(c, p)
									if err != nil {
										block("native_snapshot_identity_not_proven")
									} else {
										out.Collisions = collisions
										if result == "CONFLICT" {
											out.Status = "CONFLICT"
											block("cross_branch_node_number_or_content_collision")
										} else {
											out.Status = "NO_COLLISIONS_LOCAL_ONLY"
										}
									}
									// A remote branch can advance during a multi-file immutable
									// audit. Re-read both refs; a stable first read is not
									// proof of a stable decision boundary.
									lastC, checkC := planAuditRemoteHead(root, *canonicalBranch)
									lastP, checkP := planAuditRemoteHead(root, *peerBranch)
									if checkC != nil || checkP != nil ||
										lastC != *canonicalSHA || lastP != *peerSHA {
										block("github_ref_changed_during_audit")
										out.Status = "NOT_PROVEN"
									}
								}
							}
						}
					}
				}
			}
		}
	}
	// A clean comparison is advisory: never treat it as permission to create or
	// close native nodes until GitHub/global generation and LPR authority exist.
	block("N124_global_trusted_publisher_and_installed_attestation_not_accepted")
	if out.Status == "NO_COLLISIONS_LOCAL_ONLY" && len(out.Blockers) > 1 {
		out.Status = "NOT_PROVEN"
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(out)
	} else {
		fmt.Printf("PLAN AUTHORITY AUDIT: %s; changed=false; authority=false\n", out.Status)
		for _, b := range out.Blockers {
			fmt.Println(" -", b)
		}
	}
	return 2
}
