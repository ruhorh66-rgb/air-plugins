package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

const planHeaderSyncPreviewSchema = "air-worker.plan-header-sync-preview/v1"

type planHeaderSyncPreview struct {
	Schema           string   `json:"schema"`
	Action           string   `json:"action"`
	Status           string   `json:"status"`
	ExitCode         int      `json:"exit_code"`
	DryRun           bool     `json:"dry_run"`
	Product          string   `json:"product"`
	PlanSHA256       string   `json:"plan_sha256,omitempty"`
	GitBranch        string   `json:"git_branch,omitempty"`
	GitHead          string   `json:"git_head,omitempty"`
	GitRemote        string   `json:"git_remote,omitempty"`
	RemoteHead       string   `json:"remote_head,omitempty"`
	ActiveRelease    string   `json:"active_release,omitempty"`
	NextOpenNode     string   `json:"next_open_node,omitempty"`
	InstalledVersion string   `json:"installed_version,omitempty"`
	Changed          bool     `json:"changed"`
	Blockers         []string `json:"blockers"`
}

func cmdPlanHeader(argv []string) int {
	if len(argv) == 0 || argv[0] != "sync" {
		fmt.Fprintln(os.Stderr, "usage: air-worker plan header sync -product <root> -dry-run [-json] [-remote origin]")
		return 2
	}
	return cmdPlanHeaderSync(argv[1:])
}

// The preview is independent of future mutation code. In A1 it never obtains
// write privileges or certificates: no report it produces can be a release gate.
func cmdPlanHeaderSync(argv []string) int {
	fs := flag.NewFlagSet("plan header sync", flag.ContinueOnError)
	product := fs.String("product", ".", "canonical product root")
	dryRun := fs.Bool("dry-run", false, "read-only verified preflight")
	apply := fs.Bool("apply", false, "reserved; returns BLOCKED until native authority is accepted")
	remote := fs.String("remote", "origin", "authorized GitHub remote name")
	asJSON := fs.Bool("json", false, "machine-readable result")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		return 2
	}
	report := planHeaderSyncPreview{
		Schema:           planHeaderSyncPreviewSchema,
		Action:           "preflight",
		Status:           "BLOCKED",
		DryRun:           *dryRun,
		ExitCode:         2,
		Changed:          false,
		Blockers:         []string{},
		InstalledVersion: "UNVERIFIED",
	}
	block := func(reason string) { report.Blockers = append(report.Blockers, reason) }
	if !*dryRun {
		block("explicit_dry_run_required")
	}
	if *apply {
		block("native_mutation_not_accepted_N124_and_root_handle_CAS")
	}
	if !safePlanHeaderRemoteName(*remote) {
		block("unsafe_remote_name")
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		block("invalid_product_root")
	} else {
		report.Product = root
		if err := planHeaderValidateAncestors(root); err != nil {
			block("untrusted_product_path_or_reparse")
		} else {
			plan := filepath.Join(root, "PLAN.md")
			if raw, err := os.ReadFile(plan); err != nil {
				block("plan_read_failed")
			} else {
				report.PlanSHA256 = planHeaderSHA(raw)
				node, err := selectCanonicalNextOpenNode(root, raw)
				if err != nil {
					block("native_plan_node_validation_failed")
				} else {
					report.NextOpenNode = node
				}
				if release, err := planHeaderActiveRelease(root); err != nil {
					block("active_release_not_proven")
				} else {
					report.ActiveRelease = release
				}
			}
			branch, brErr := planHeaderGitRead(root, "branch", "--show-current")
			head, shaErr := planHeaderGitRead(root, "rev-parse", "--verify", "HEAD")
			if brErr != nil || shaErr != nil ||
				!planHeaderBranchRegex.MatchString(branch) || !planHeaderGitSHARegex.MatchString(head) {
				block("source_git_identity_not_proven")
			} else {
				report.GitBranch, report.GitHead = branch, head
				if *remote != "" && safePlanHeaderRemoteName(*remote) {
					remoteURL, e := planHeaderGitRead(root, "remote", "get-url", *remote)
					if e != nil || !planHeaderAllowedRemote(remoteURL) {
						block("remote_is_not_authorized_github")
					} else {
						report.GitRemote = *remote
						ref := "refs/heads/" + branch
						raw, e := planHeaderGitRead(root, "ls-remote", "--exit-code", *remote, ref)
						if e != nil {
							block("remote_head_not_proven")
						} else {
							fields := strings.Fields(raw)
							if len(fields) != 2 || fields[1] != ref || !planHeaderGitSHARegex.MatchString(fields[0]) {
								block("remote_response_invalid")
							} else {
								report.RemoteHead = fields[0]
								if fields[0] != head {
									block("remote_head_diverged")
								}
							}
						}
					}
				}
			}
			if status, e := planHeaderGitRead(root, "status", "--porcelain", "-uall", "--", "PLAN.md", "plan"); e != nil {
				block("git_plan_status_unavailable")
			} else if status != "" {
				block("local_plan_or_nodes_dirty")
			}
		}
	}
	// A local product manifest, stale prose or a separately supplied string is
	// not attestation of the *installed* product binary/version. N111 may only
	// become writable after integration with the native trusted product registry
	// and exact versioned artifact readback.
	block("installed_version_binary_attestation_not_implemented")
	block("N124_global_git_generation_not_accepted")
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(report)
	} else {
		fmt.Printf("PLAN HEADER SYNC: BLOCKED (read-only=%v)\n", *dryRun)
		for _, code := range report.Blockers {
			fmt.Println(" -", code)
		}
	}
	return report.ExitCode
}

func safePlanHeaderRemoteName(s string) bool {
	if s == "" || len(s) > 70 {
		return false
	}
	first := s[0]
	if (first < 'A' || first > 'Z') && (first < 'a' || first > 'z') &&
		(first < '0' || first > '9') {
		return false
	}
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') &&
			(r < '0' || r > '9') && r != '-' && r != '_' {
			return false
		}
	}
	return true
}

func planHeaderAllowedRemote(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "https") ||
		!strings.EqualFold(u.Host, "github.com") || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" || !strings.HasSuffix(u.Path, ".git") {
		return false
	}
	return len(strings.Split(strings.Trim(u.Path, "/"), "/")) == 2
}

// All path components are checked for link/reparse changes; this provides
// a read-only preflight, NOT stable root-handle authorization for mutation.
func planHeaderValidateAncestors(root string) error {
	clean := filepath.Clean(root)
	expanded, err := filepath.EvalSymlinks(clean)
	if err != nil || !strings.EqualFold(filepath.Clean(expanded), clean) {
		return errors.New("linked, unresolved or redirected product root")
	}
	for p := clean; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("untrusted ancestor")
		}
		if parent := filepath.Dir(p); parent == p {
			break
		}
	}
	return nil
}

func planHeaderGitRead(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_PAGER=cat", "GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil || len(out) > 65536 {
		return "", errors.New("git preflight unavailable")
	}
	s := strings.TrimSpace(string(out))
	if !utf8.ValidString(s) {
		return "", errors.New("Git output is not UTF-8")
	}
	return s, nil
}

func planHeaderActiveRelease(root string) (string, error) {
	path := filepath.Join(root, "plan", "release-slices.json")
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > 1048576 {
		return "", errors.New("machine release-slices not available")
	}
	raw, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(raw) {
		return "", errors.New("release slice read or UTF-8 error")
	}
	var doc struct {
		Active   string `json:"active_release"`
		Releases []struct {
			Version string `json:"version"`
		} `json:"releases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil ||
		!planHeaderStableVersion.MatchString(doc.Active) {
		return "", errors.New("invalid active release")
	}
	matches := 0
	for _, entry := range doc.Releases {
		if entry.Version == doc.Active {
			matches++
		}
	}
	if matches != 1 {
		return "", errors.New("active release missing or duplicated")
	}
	return doc.Active, nil
}
