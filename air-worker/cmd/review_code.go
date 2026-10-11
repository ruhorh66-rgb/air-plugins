package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const codeReviewOperation = "code-review"
const codeReviewMaxTask = 16 * 1024

func codeReviewer() runnerSpec { return runnerSpec{Kind: "codex", Model: "gpt-6-sol", Effort: "low"} }

var codeReviewResolve = resolveRunnerTool
var codeReviewRun = runReceiptedWithMeta
var codeReviewRemote = codeReviewRemoteHead
var codeReviewLookupRemote = func(root, remote, ref string) (string, error) {
	return codeReviewGit(root, "ls-remote", "--exit-code", "--refs", remote, ref)
}

var codeReviewRemoteURL = func(root, remote string) (string, error) {
	return codeReviewGit(root, "remote", "get-url", remote)
}

var codeReviewRemotes = func(root string) ([]string, error) {
	result, err := codeReviewGit(root, "remote")
	if err != nil {
		return nil, err
	}
	if result == "" {
		return nil, nil
	}
	return strings.Split(result, "\n"), nil
}

var codeReviewGitHubURL = regexp.MustCompile(`^(https://github\.com/|git@github\.com:|ssh://git@github\.com/)[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+(?:\.git)?$`)

func codeReviewIsGitHubRemote(root, remote string) bool {
	url, err := codeReviewRemoteURL(root, remote)
	return err == nil && codeReviewGitHubURL.MatchString(url)
}

func codeReviewSelectRemote(root string, remotes []string) (string, error) {
	var selected string
	for _, remote := range remotes {
		if !codeReviewIsGitHubRemote(root, remote) {
			continue
		}
		if selected != "" {
			return "", fmt.Errorf("multiple GitHub remotes")
		}
		selected = remote
	}
	if selected == "" {
		return "", fmt.Errorf("no GitHub remote")
	}
	return selected, nil
}

func codeReviewDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

type codeReviewFinding struct {
	Severity   string `json:"severity"`
	Location   string `json:"location"`
	Issue      string `json:"issue"`
	MinimalFix string `json:"minimal_fix"`
}
type codeReviewVerdict struct {
	Verdict    string              `json:"verdict"`
	SourceHead string              `json:"source_head"`
	P0         int                 `json:"p0_open"`
	P1         int                 `json:"p1_open"`
	P2         int                 `json:"p2_open"`
	Findings   []codeReviewFinding `json:"findings"`
	NextGate   string              `json:"next_gate"`
}
type codeReviewReceipt struct {
	Verdict    string             `json:"verdict"`
	Error      string             `json:"error,omitempty"`
	SourceSHA  string             `json:"source_sha"`
	TaskDigest string             `json:"task_digest"`
	PlanSHA    string             `json:"plan_sha"`
	Provider   string             `json:"provider"`
	Model      string             `json:"model"`
	Effort     string             `json:"effort"`
	Sandbox    string             `json:"sandbox"`
	Executable string             `json:"executable"`
	Runner     string             `json:"runner"`
	OutputSHA  string             `json:"output_sha,omitempty"`
	SessionID  string             `json:"codex_session_id,omitempty"`
	JobReceipt string             `json:"job_receipt"`
	Reviewer   *codeReviewVerdict `json:"reviewer,omitempty"`
}

func codeReviewGit(root string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return strings.TrimSpace(string(out)), nil
}

func codeReviewRemoteHead(root string) (string, error) {
	branch, err := codeReviewGit(root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	trackingRemote, remoteErr := codeReviewGit(root, "config", "--get", "branch."+branch+".remote")
	trackingRef, refErr := codeReviewGit(root, "config", "--get", "branch."+branch+".merge")
	remote, ref := "", ""
	if remoteErr == nil || refErr == nil {
		if remoteErr != nil || refErr != nil || trackingRemote == "" || trackingRemote == "." || strings.HasPrefix(trackingRemote, "-") || trackingRef != "refs/heads/"+branch {
			return "", fmt.Errorf("invalid branch tracking configuration")
		}
		remote, ref = trackingRemote, trackingRef
	} else {
		remotes, err := codeReviewRemotes(root)
		if err != nil {
			return "", err
		}
		remote, err = codeReviewSelectRemote(root, remotes)
		if err != nil {
			return "", err
		}
		ref = "refs/heads/" + branch
	}
	if !codeReviewIsGitHubRemote(root, remote) {
		return "", fmt.Errorf("remote must be GitHub")
	}
	out, err := codeReviewLookupRemote(root, remote, ref)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 || fields[1] != ref {
		return "", fmt.Errorf("ambiguous remote SHA")
	}
	return fields[0], nil
}

func codeReviewSource(root, sha string) error {
	top, err := codeReviewGit(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}

	remote, err := codeReviewRemote(root)
	if err != nil {
		return err
	}
	if remote != sha {
		return fmt.Errorf("remote source SHA mismatch")
	}
	head, err := codeReviewGit(root, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != sha {
		return fmt.Errorf("source HEAD mismatch")
	}
	dirty, err := codeReviewGit(top, "status", "--porcelain=v1", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return err
	}
	if dirty != "" {
		return fmt.Errorf("repository is dirty")
	}
	return nil
}

// Check every ancestor, including the product itself: junctions can hide above the task.
func codeReviewPathIdentity(path string) (nativeInputIdentity, error) {
	for p := path; ; p = filepath.Dir(p) {
		info, err := os.Lstat(p)
		if err != nil {
			return nativeInputIdentity{}, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nativeInputIdentity{}, fmt.Errorf("symlink input prohibited")
		}
		if _, err := nativeInputPathIdentity(p); err != nil {
			return nativeInputIdentity{}, err
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	return nativeInputPathIdentity(path)
}

func readCodeReviewTask(root, name string) ([]byte, nativeInputIdentity, error) {
	var zero nativeInputIdentity
	if !filepath.IsAbs(name) {
		name = filepath.Join(root, name)
	}
	path, err := filepath.Abs(name)
	if err != nil {
		return nil, zero, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, zero, fmt.Errorf("task must be inside product")
	}
	before, err := codeReviewPathIdentity(path)
	if err != nil {
		return nil, zero, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, zero, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, zero, err
	}
	if !info.Mode().IsRegular() || info.Size() > codeReviewMaxTask {
		return nil, zero, fmt.Errorf("task must be regular and <=16KiB")
	}
	opened, err := nativeInputOpenedIdentity(f)
	if err != nil {
		return nil, zero, err
	}
	data, err := io.ReadAll(io.LimitReader(f, codeReviewMaxTask+1))
	if err != nil {
		return nil, zero, err
	}
	after, err := codeReviewPathIdentity(path)
	if err != nil {
		return nil, zero, err
	}
	if len(data) > codeReviewMaxTask || !nativeInputSameIdentity(before, opened) || !nativeInputSameIdentity(opened, after) {
		return nil, zero, fmt.Errorf("task swapped or oversized")
	}
	return data, opened, nil
}

func parseCodeReview(stream, sha string) (codeReviewVerdict, string, error) {
	var v codeReviewVerdict
	session, message := "", ""
	structuredMessage, trailingCommentary := false, false
	structuredCount := 0
	completed, failed := false, false
	if len(stream) > 4*1024*1024 {
		return v, session, fmt.Errorf("review output exceeds 4MiB")
	}
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			continue // Non-JSON stderr warnings are diagnostic noise, not protocol events.
		}
		var e codexEvent
		if codeReviewUniqueJSON(json.NewDecoder(strings.NewReader(line))) != nil || json.Unmarshal([]byte(line), &e) != nil || e.Type == "" {
			return v, session, fmt.Errorf("invalid event stream")
		}
		if completed {
			return v, session, fmt.Errorf("event after terminal completion")
		}
		switch e.Type {
		case "thread.started":
			if session != "" || e.ThreadID == "" {
				return v, session, fmt.Errorf("invalid thread")
			}
			session = e.ThreadID
		case "turn.completed":
			completed = true
		case "turn.failed", "error":
			failed = true
		}
		if e.Error != nil || (e.Item != nil && e.Item.Type == "error") {
			failed = true
		}
		if e.Type == "item.completed" && e.Item != nil && e.Item.Type == "agent_message" {
			text := e.Item.Text
			if structuredMessage {
				trailingCommentary = true
			}
			if strings.HasPrefix(strings.TrimSpace(text), "{") {
				structuredCount++
				if structuredCount > 1 {
					return v, session, fmt.Errorf("multiple structured review messages")
				}
				structuredMessage = true
				message = text
			}
		}
	}
	if failed || !completed || session == "" || !structuredMessage || trailingCommentary {
		return v, session, fmt.Errorf("unsuccessful terminal event")
	}
	// Decode required fields separately so missing zero counts cannot silently become valid.
	if err := codeReviewUniqueJSON(json.NewDecoder(strings.NewReader(message))); err != nil {
		return v, session, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(message), &fields); err != nil {
		return v, session, err
	}
	for _, key := range []string{"verdict", "source_head", "p0_open", "p1_open", "p2_open", "findings", "next_gate"} {
		if value, ok := fields[key]; !ok || string(value) == "null" {
			return v, session, fmt.Errorf("missing %s", key)
		}
	}
	decoder := json.NewDecoder(strings.NewReader(message))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&v); err != nil {
		return v, session, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return v, session, fmt.Errorf("expected single JSON")
	}
	if v.SourceHead != sha || strings.TrimSpace(v.NextGate) == "" {
		return v, session, fmt.Errorf("invalid source or next gate")
	}
	counts := map[string]int{"P0": 0, "P1": 0, "P2": 0}
	for _, f := range v.Findings {
		if _, ok := counts[f.Severity]; !ok || strings.TrimSpace(f.Location) == "" || strings.TrimSpace(f.Issue) == "" || strings.TrimSpace(f.MinimalFix) == "" {
			return v, session, fmt.Errorf("invalid finding")
		}
		counts[f.Severity]++
	}
	if v.P0 != counts["P0"] || v.P1 != counts["P1"] || v.P2 != counts["P2"] {
		return v, session, fmt.Errorf("finding count mismatch")
	}
	switch v.Verdict {
	case "PASS":
		if len(v.Findings) > 0 {
			return v, session, fmt.Errorf("PASS with open findings")
		}
	case "CHANGES_REQUIRED", "NOT_PROVEN":
	default:
		return v, session, fmt.Errorf("unknown verdict")
	}
	return v, session, nil
}

const codeReviewSchema = `{"type":"object","additionalProperties":false,"required":["verdict","source_head","p0_open","p1_open","p2_open","findings","next_gate"],"properties":{"verdict":{"type":"string","enum":["PASS","CHANGES_REQUIRED","NOT_PROVEN"]},"source_head":{"type":"string"},"p0_open":{"type":"integer","minimum":0},"p1_open":{"type":"integer","minimum":0},"p2_open":{"type":"integer","minimum":0},"findings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["severity","location","issue","minimal_fix"],"properties":{"severity":{"type":"string","enum":["P0","P1","P2"]},"location":{"type":"string"},"issue":{"type":"string"},"minimal_fix":{"type":"string"}}}},"next_gate":{"type":"string"}}}`

func cmdCodeReview(argv []string) int {
	fs := flag.NewFlagSet("review code", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	source := fs.String("source-sha", "", "exact Git source SHA")
	task := fs.String("task-file", "", "task inside product")
	principal := fs.String("principal", "", "declared principal")
	session := fs.String("session-key", "", "declared session")
	state := fs.String("state-dir", "", "session state directory")
	preflight := fs.Bool("preflight", false, "validate without model process")
	asJSON := fs.Bool("json", false, "machine JSON")
	if fs.Parse(argv) != nil {
		return 2
	}
	doc := codeReviewReceipt{Verdict: "NOT_PROVEN", SourceSHA: *source, Provider: "codex", Model: "gpt-6-sol", Effort: "low", Sandbox: "read-only", Runner: "codex"}
	fail := func(err error) int {
		doc.Error = err.Error()
		if *asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(doc)
		} else {
			fmt.Fprintln(os.Stderr, "NOT_PROVEN:", err)
		}
		return 2
	}
	if fs.NArg() != 0 || !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(*source) || *task == "" {
		return fail(fmt.Errorf("requires source SHA, task file, and no unexpected arguments"))
	}
	root, cfg, err := loadExecutorConfig(*product, "")
	if err != nil {
		return fail(err)
	}
	scope, err := executorScopeForSession(root, *principal, *session, *state)
	if err != nil {
		return fail(err)
	}
	approved := false
	for _, tier := range cfg.Ladder {
		if tier == "gpt6-sol:low" {
			approved = true
		}
	}
	runner := resolveRunner(cfg, "gpt6-sol:low")
	if cfg.ModelPolicy.Schema != modelPolicySchemaV1 || !approved || runner.Kind != "codex" || runner.Model != "gpt-6-sol" || runner.Effort != "low" {
		return fail(fmt.Errorf("gpt6-sol:low is not policy-approved with pinned runner"))
	}
	data, identity, err := readCodeReviewTask(root, *task)
	if err != nil {
		return fail(err)
	}
	doc.TaskDigest = codeReviewDigest(data)
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		return fail(err)
	}
	doc.PlanSHA = codeReviewDigest(plan)
	step := "code-" + *source + "-" + doc.TaskDigest
	receipt, _ := jobReceiptPaths(scope, step, codeReviewOperation)
	doc.JobReceipt = receipt
	for _, path := range []string{receipt, receipt + ".claim"} {
		if _, err := os.Lstat(path); err == nil {
			return fail(fmt.Errorf("existing job; inspect receipt/status: %s", receipt))
		} else if !os.IsNotExist(err) {
			return fail(err)
		}
	}
	if err := codeReviewSource(root, *source); err != nil {
		return fail(err)
	}
	doc.Executable, err = codeReviewResolve("codex")
	if err != nil {
		return fail(err)
	}
	if err := requirePonytailSkill(); err != nil {
		return fail(err)
	}
	recheck := func() error {
		if err := codeReviewSource(root, *source); err != nil {
			return err
		}
		again, id, err := readCodeReviewTask(root, *task)
		if err != nil {
			return err
		}
		if !nativeInputSameIdentity(identity, id) || codeReviewDigest(again) != doc.TaskDigest {
			return fmt.Errorf("task file swapped or changed")
		}
		currentPlan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
		if err != nil {
			return err
		}
		if codeReviewDigest(currentPlan) != doc.PlanSHA {
			return fmt.Errorf("PLAN changed")
		}
		return nil
	}
	if err := recheck(); err != nil {
		return fail(err)
	}
	if *preflight {
		doc.Verdict = "PREFLIGHT_OK"
		_ = json.NewEncoder(os.Stdout).Encode(doc)
		return 0
	}
	// Permanent exclusive claim closes the check/start race and forbids replay after interruption.
	if err := os.MkdirAll(filepath.Dir(receipt), 0755); err != nil {
		return fail(err)
	}
	claim, err := os.OpenFile(receipt+".claim", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fail(fmt.Errorf("review already claimed or unavailable; inspect %s: %w", receipt, err))
	}
	if err := claim.Close(); err != nil {
		return fail(err)
	}
	schemaPath := receipt + ".schema.json"
	if err := os.WriteFile(schemaPath, []byte(codeReviewSchema), 0600); err != nil {
		return fail(err)
	}
	if err := recheck(); err != nil {
		return fail(err)
	}
	prompt := fmt.Sprintf("Review code at exact source HEAD %s. You are the independent reviewer. Read-only: never edit PLAN, plan nodes, LEARN or any product file; do not close native nodes. Return exactly the required JSON schema. Count every P0/P1/P2 finding; PASS requires all zero. next_gate must identify remaining human/native acceptance. Task text follows as review scope:\n%s", *source, data)
	output, runErr := codeReviewRun(context.Background(), scope, step, codeReviewOperation, semanticCodexCommand(doc.Executable, root, prompt, codeReviewer(), schemaPath), jobReceiptMeta{Runner: "codex", Provider: "codex", Model: "gpt-6-sol", Effort: "low", Role: "code-reviewer", Sandbox: "read-only"})
	doc.OutputSHA = codeReviewDigest(output)
	v, sid, parseErr := parseCodeReview(string(output), *source)
	doc.SessionID = sid
	if v.Verdict != "" {
		doc.Reviewer = &v
	}
	afterErr := recheck()
	job, jobErr := readJobReceipt(receipt)
	switch {
	case runErr != nil:
		doc.Error = runErr.Error()
	case jobErr != nil:
		doc.Error = jobErr.Error()
	case job.Status != jobStatusDone || !job.ProcessStarted:
		doc.Error = "underlying job is not DONE"
	case parseErr != nil:
		doc.Error = parseErr.Error()
	case afterErr != nil:
		doc.Error = afterErr.Error()
	default:
		doc.Verdict = v.Verdict
	}
	encoded, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := writeFileAtomic(receipt+".review.json", encoded); err != nil {
		doc.Verdict = "NOT_PROVEN"
		return fail(err)
	}
	_ = json.NewEncoder(os.Stdout).Encode(doc)
	if doc.Verdict == "PASS" {
		return 0
	}
	if doc.Verdict == "CHANGES_REQUIRED" {
		return 1
	}
	return 2
}

// Reject duplicate JSON keys before Go's decoder can silently accept the last value.
func codeReviewUniqueJSON(dec *json.Decoder) error {
	token, err := dec.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			key, err := dec.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate or invalid JSON key")
			}
			seen[name] = true
			if err := codeReviewUniqueJSON(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
	case json.Delim('['):
		for dec.More() {
			if err := codeReviewUniqueJSON(dec); err != nil {
				return err
			}
		}
		_, err = dec.Token()
	}
	return err
}
