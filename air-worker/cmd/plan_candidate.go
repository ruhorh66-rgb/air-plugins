package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type planCandidate struct {
	OID, Tree, Parent string
	Operation         string
	Files             map[string][]byte
	preparedDigest    [32]byte
}

var candidateOIDRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
var candidateOperationRE = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

// preparePlanCandidate is fixture-only A1 plumbing, intentionally not a production caller.
// shortcut: identical inputs have identical OIDs, cross-request operation fencing belongs to A2 CAS/recovery.
func preparePlanCandidate(ctx context.Context, root, old, operation string, files map[string][]byte) (planCandidate, error) {
	return preparePlanCandidateWithMode(ctx, root, old, operation, files, false)
}

// Only the already authenticated canonical fixture close may replace an
// existing native node. The generic create path remains strictly additive.
func preparePlanCloseCandidate(ctx context.Context, root, old, operation string, files map[string][]byte) (planCandidate, error) {
	return preparePlanCandidateWithMode(ctx, root, old, operation, files, true)
}

func preparePlanCandidateWithMode(ctx context.Context, root, old, operation string, files map[string][]byte, closeMode bool) (planCandidate, error) {
	fail := func(err error) (planCandidate, error) { return planCandidate{}, err }
	if !candidateOIDRE.MatchString(old) || !candidateOperationRE.MatchString(operation) || len(files) == 0 || len(files) > 16 {
		return fail(fmt.Errorf("invalid candidate identity or file count"))
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return fail(err)
	}
	if err := candidateNoLinks(root); err != nil {
		return fail(err)
	}
	temp, err := resolveLinks(os.TempDir())
	if err != nil {
		return fail(err)
	}
	rel, err := filepath.Rel(temp, root)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fail(fmt.Errorf("candidate requires an OS-temp fixture"))
	}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") && value != "" {
			return fail(fmt.Errorf("unknown Git environment authority: %s", key))
		}
	}
	if err := candidateNoLinks(filepath.Join(root, "plan-authority.json")); err != nil {
		return fail(err)
	}
	info, err := os.Stat(filepath.Join(root, "plan-authority.json"))
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
		return fail(fmt.Errorf("unknown fixture authority"))
	}
	raw, err := os.ReadFile(filepath.Join(root, "plan-authority.json"))
	if err != nil {
		return fail(err)
	}
	var authority struct{ Schema, Mode, Root string }
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&authority) != nil || d.Decode(new(any)) != io.EOF || authority.Schema != "air-worker.plan.authority/v1" || authority.Mode != "isolated-fixture" || authority.Root != root {
		if _, err := planCanonicalFixtureOrigin(root); err != nil {
			return fail(fmt.Errorf("unknown fixture authority"))
		}
	}
	for _, name := range []string{"objects", "config", "index", "HEAD"} {
		if err := candidateNoLinks(filepath.Join(root, ".git", name)); err != nil {
			return fail(err)
		}
	}
	info, err = os.Lstat(filepath.Join(root, ".git"))
	if err != nil || !info.IsDir() {
		return fail(fmt.Errorf("standalone fixture Git directory required"))
	}
	for _, name := range []string{"commondir", "objects/info/alternates", "shallow"} {
		if _, err := os.Lstat(filepath.Join(root, ".git", filepath.FromSlash(name))); !os.IsNotExist(err) {
			return fail(fmt.Errorf("unsupported fixture Git metadata: %s", name))
		}
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return fail(err)
	}
	env := append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0", "GIT_AUTHOR_NAME=AirWorker fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=AirWorker fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid", "GIT_AUTHOR_DATE=2000-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2000-01-01T00:00:00Z")
	run := func(input []byte, args ...string) ([]byte, error) {
		c := exec.CommandContext(ctx, git, append([]string{"-C", root, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false"}, args...)...)
		c.Env, c.Stdin, c.WaitDelay = env, bytes.NewReader(input), time.Second
		var stderr bytes.Buffer
		c.Stderr = &stderr
		out, err := c.Output()
		if err != nil {
			return nil, fmt.Errorf("git %s: %w: %s", args[0], err, stderr.String())
		}
		return out, nil
	}
	for _, check := range []struct {
		args     []string
		expected string
	}{
		{[]string{"rev-parse", "--show-toplevel"}, filepath.ToSlash(root)},
		{[]string{"rev-parse", "HEAD"}, old},
		{[]string{"cat-file", "-t", old}, "commit"},
		{[]string{"remote"}, ""},
		{[]string{"status", "--porcelain=v1", "--untracked-files=all", "--ignored"}, ""},
	} {
		out, err := run(nil, check.args...)
		if err != nil {
			return fail(err)
		}
		if strings.TrimSpace(string(out)) != check.expected {
			return fail(fmt.Errorf("fixture authority/cleanliness check failed: %s", check.args[0]))
		}
	}
	flags, err := run(nil, "ls-files", "-v", "-z")
	if err != nil {
		return fail(err)
	}
	for _, entry := range bytes.Split(flags, []byte{0}) {
		if len(entry) > 0 && entry[0] != 'H' {
			return fail(fmt.Errorf("unsupported index flags"))
		}
	}
	// Include tree entries so a new file cannot replace a directory or traverse a Git symlink.
	tree, err := run(nil, "ls-tree", "-r", "-t", "-z", old)
	if err != nil {
		return fail(err)
	}
	entries := map[string]string{}
	for _, entry := range bytes.Split(tree, []byte{0}) {
		if len(entry) == 0 {
			continue
		}
		meta, name, ok := strings.Cut(string(entry), "\t")
		if !ok {
			return fail(fmt.Errorf("invalid tree readback"))
		}
		entries[name] = meta
	}
	prefix := ""
	if _, ok := files["air-worker/PLAN.md"]; ok {
		prefix = "air-worker/"
	}
	if _, ok := files[prefix+"PLAN.md"]; !ok {
		return fail(fmt.Errorf("PLAN spine required"))
	}
	if len(files) != 3 {
		return fail(fmt.Errorf("candidate requires exactly PLAN, one node, and one operation outbox"))
	}
	paths, seen, total := []string{}, map[string]bool{}, 0
	numbers := map[string]bool{}
	hasOutbox := false
	for name, content := range files {
		local := strings.TrimPrefix(name, prefix)
		validNode := strings.HasPrefix(local, "plan/") && strings.HasSuffix(local, ".md") && planNodeIDRE.MatchString(strings.TrimSuffix(strings.TrimPrefix(local, "plan/"), ".md"))
		if name != path.Clean(name) || strings.ContainsAny(name, "\\:\x00\r\n\t") || !strings.HasPrefix(name, prefix) || (local != "PLAN.md" && !validNode && local != "plan/event-outbox/"+operation+".json") {
			return fail(fmt.Errorf("unauthorized candidate path: %q", name))
		}
		key := strings.ToLower(name)
		if seen[key] {
			return fail(fmt.Errorf("duplicate candidate path"))
		}
		seen[key] = true
		if validNode {
			number := path.Base(name)[:5]
			if numbers[number] {
				return fail(fmt.Errorf("duplicate candidate node number"))
			}
			numbers[number] = true
		}
		if local == "plan/event-outbox/"+operation+".json" {
			hasOutbox = true
		}
		total += len(content)
		if total > 4*1024*1024 {
			return fail(fmt.Errorf("candidate exceeds byte limit"))
		}
		if err := candidateNoLinks(filepath.Join(root, filepath.FromSlash(name))); err != nil {
			return fail(err)
		}
		if local != "PLAN.md" {
			localPath := filepath.Join(root, filepath.FromSlash(name))
			if closeMode && validNode {
				st, err := os.Lstat(localPath)
				if err != nil || !st.Mode().IsRegular() || st.Mode()&os.ModeSymlink != 0 {
					return fail(fmt.Errorf("close requires an existing regular canonical node: %s", name))
				}
			} else if _, err := os.Lstat(localPath); !os.IsNotExist(err) {
				return fail(fmt.Errorf("existing candidate worktree path collision: %s", name))
			}
		}
		for existing, meta := range entries {
			if strings.EqualFold(existing, name) {
				if (local != "PLAN.md" && !(closeMode && validNode)) || existing != name || !strings.HasPrefix(meta, "100644 blob ") {
					return fail(fmt.Errorf("existing candidate path collision: %s", existing))
				}
			}
			if strings.HasPrefix(key, strings.ToLower(existing)+"/") && (!strings.HasPrefix(meta, "040000 tree ") || !strings.HasPrefix(name, existing+"/")) {
				return fail(fmt.Errorf("candidate ancestor collision"))
			}
			if validNode && path.Dir(existing) == prefix+"plan" && strings.HasPrefix(path.Base(existing), path.Base(name)[:5]+"_") && !(closeMode && existing == name) {
				return fail(fmt.Errorf("node number collision"))
			}
		}
		if local == "PLAN.md" && !strings.HasPrefix(entries[name], "100644 blob ") {
			return fail(fmt.Errorf("missing canonical PLAN"))
		}
		paths = append(paths, name)
	}
	if len(numbers) != 1 || !hasOutbox {
		return fail(fmt.Errorf("candidate requires exactly one new plan node and operation outbox"))
	}
	if closeMode {
		// A close changes exactly the already tracked PLAN and node, and adds
		// exactly one receipt-bound outbox. Validate the entire old Git blob
		// generation before preparing the private, unpublished candidate.
		readBase := func(name string) ([]byte, error) {
			if name == "" || name != path.Clean(name) || path.IsAbs(name) || strings.ContainsAny(name, "\\:\x00\r\n\t") {
				return nil, fmt.Errorf("unsafe canonical base blob path")
			}
			return run(nil, "cat-file", "blob", old+":"+name)
		}
		if err := validatePlanCloseCandidate(root, old, operation, files, readBase); err != nil {
			return fail(err)
		}
	} else if err := validateCandidateContent(prefix, operation, files); err != nil {
		return fail(err)
	}
	sort.Strings(paths)
	private, err := os.MkdirTemp("", "air-worker-candidate-*")
	if err != nil {
		return fail(err)
	}
	defer os.RemoveAll(private)
	env = append(env, "GIT_INDEX_FILE="+filepath.Join(private, "index"))
	if _, err := run(nil, "read-tree", old+"^{tree}"); err != nil {
		return fail(err)
	}
	for _, name := range paths {
		oid, err := run(files[name], "hash-object", "--stdin", "--no-filters", "-w")
		if err != nil {
			return fail(err)
		}
		if _, err := run(nil, "update-index", "--add", "--cacheinfo", "100644", strings.TrimSpace(string(oid)), name); err != nil {
			return fail(err)
		}
	}
	written, err := run(nil, "write-tree")
	if err != nil {
		return fail(err)
	}
	treeOID := strings.TrimSpace(string(written))
	commit, err := run([]byte("AirWorker candidate\n\nOperation-ID: "+operation+"\n"), "commit-tree", treeOID, "-p", old)
	if err != nil {
		return fail(err)
	}
	oid := strings.TrimSpace(string(commit))
	if !candidateOIDRE.MatchString(oid) {
		return fail(fmt.Errorf("invalid candidate OID"))
	}
	readback, err := run(nil, "show", "-s", "--format=%T%n%P", oid)
	if err != nil {
		return fail(err)
	}
	if string(bytes.TrimSpace(readback)) != treeOID+"\n"+old {
		return fail(fmt.Errorf("candidate tree/parent mismatch"))
	}
	result := planCandidate{OID: oid, Tree: treeOID, Parent: old, Operation: operation, Files: map[string][]byte{}}
	for _, name := range paths {
		content, err := run(nil, "cat-file", "blob", oid+":"+name)
		if err != nil {
			return fail(err)
		}
		if !bytes.Equal(content, files[name]) {
			return fail(fmt.Errorf("candidate bytes mismatch: %s", name))
		}
		result.Files[name] = content
	}
	result.preparedDigest = candidatePublishDigest(result)
	return result, nil
}

type candidateOperation struct {
	Schema      string `json:"schema"`
	OperationID string `json:"operation_id"`
	NodeID      string `json:"node_id"`
	Status      string `json:"status"`
	Parent      string `json:"parent"`
	DoneWhen    string `json:"done_when"`
	PlanSHA256  string `json:"plan_sha256"`
	NodeSHA256  string `json:"node_sha256"`
}

func validateCandidateContent(prefix, operation string, files map[string][]byte) error {
	var nodePath string
	for name := range files {
		if strings.HasPrefix(name, prefix+"plan/N-") {
			nodePath = name
		}
	}
	nodeBytes := files[nodePath]
	n, err := parsePlanNode(nodePath, nodeBytes)
	if err != nil {
		return err
	}
	if n.ID != strings.TrimSuffix(path.Base(nodePath), ".md") || n.Status != "open" {
		return fmt.Errorf("candidate node identity/status mismatch")
	}
	body, err := parsePlanNodeBody(nodePath, nodeBytes)
	if err != nil {
		return err
	}
	// Canonical round-trip rejects duplicate declarations and ambiguous YAML syntax.
	if string(renderPlanNodeWithBody(n, body)) != strings.ReplaceAll(string(nodeBytes), "\r\n", "\n") {
		return fmt.Errorf("candidate node must have canonical unique declarations")
	}
	planBytes := files[prefix+"PLAN.md"]
	planText := strings.ReplaceAll(string(planBytes), "\r\n", "\n")
	refs := regexp.MustCompile(`N-`+n.ID[2:5]+`(?:_[\p{L}\p{N}_-]+)?`).FindAllString(planText, -1)
	if len(refs) != 1 || refs[0] != n.ID || !strings.Contains("\n"+planText, "\n"+spineNodeLine(n)+"\n") {
		return fmt.Errorf("PLAN must reference exactly the matching new node")
	}
	raw := files[prefix+"plan/event-outbox/"+operation+".json"]
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return fmt.Errorf("invalid operation object")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return fmt.Errorf("duplicate operation field")
		}
		switch key {
		case "schema", "operation_id", "node_id", "status", "parent", "done_when", "plan_sha256", "node_sha256":
		default:
			return fmt.Errorf("unknown operation field: %s", key)
		}
		seen[key] = true
		value, err := decoder.Token()
		if _, ok := value.(string); err != nil || !ok {
			return fmt.Errorf("operation field must be a string: %s", key)
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(seen) != 8 || decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("invalid operation schema")
	}
	var op candidateOperation
	if err := json.Unmarshal(raw, &op); err != nil {
		return err
	}
	want := candidateOperation{Schema: "air-worker.plan.candidate-operation/v1", OperationID: operation, NodeID: n.ID, Status: n.Status, Parent: n.Parent, DoneWhen: n.DoneWhen, PlanSHA256: fmt.Sprintf("%x", sha256.Sum256(planBytes)), NodeSHA256: fmt.Sprintf("%x", sha256.Sum256(nodeBytes))}
	if op != want {
		return fmt.Errorf("operation/node/PLAN content mismatch")
	}
	return nil
}

func candidateNoLinks(name string) error {
	for current := name; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		if err == nil {
			if info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
				return fmt.Errorf("candidate link/reparse path: %s", current)
			}
			if _, err := os.Readlink(current); err == nil {
				return fmt.Errorf("candidate link/junction path: %s", current)
			}
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}
