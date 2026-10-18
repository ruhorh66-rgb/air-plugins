package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// This declaration grants only disposable local fixture access, never production authority.
func planCanonicalFixtureOrigin(root string) (string, error) {
	fail := func() (string, error) { return "", fmt.Errorf("invalid git-canonical-fixture authority") }
	physical, err := resolveLinks(root)
	if err != nil || root != physical || candidateNoLinks(root) != nil {
		return fail()
	}
	temp, err := resolveLinks(os.TempDir())
	if err != nil {
		return fail()
	}
	rel, err := filepath.Rel(temp, root)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fail()
	}
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") && value != "" {
			return fail()
		}
	}
	name := filepath.Join(root, "plan-authority.json")
	info, err := os.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 || candidateNoLinks(name) != nil {
		return fail()
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return fail()
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return fail()
	}
	fields := map[string]string{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return fail()
		}
		k, ok := key.(string)
		if !ok || (k != "schema" && k != "mode" && k != "root" && k != "origin") {
			return fail()
		}
		if _, exists := fields[k]; exists {
			return fail()
		}
		value, err := d.Token()
		v, ok := value.(string)
		if err != nil || !ok {
			return fail()
		}
		fields[k] = v
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || d.Decode(new(any)) != io.EOF || len(fields) != 4 ||
		fields["schema"] != "air-worker.plan.authority/v1" || fields["mode"] != "git-canonical-fixture" || fields["root"] != root {
		return fail()
	}
	origin := fields["origin"]
	physicalOrigin, err := resolveLinks(origin)
	if err != nil || origin != physicalOrigin || origin == root || candidatePublishFixture(origin) != nil {
		return fail()
	}
	return origin, nil
}

func cmdPlanCanonicalFixtureNew(root, origin string, req sharedPlanCreateRequest, actorKind, actor string, extra []string, asJSON bool) int {
	fail := func(err error) int {
		action := "conflict"
		if e, ok := err.(*planPublishError); ok {
			action = e.State
		}
		if asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"schema": "air-worker.plan.node.mutation/v1", "source": "git-canonical-fixture", "action": action, "operation_id": req.ID, "error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 2
	}
	if len(extra) != 0 || !candidateOperationRE.MatchString(req.ID) {
		return fail(fmt.Errorf("safe -request-id required; unexpected positional arguments forbidden"))
	}
	for name, value := range map[string]string{"title": req.Title, "parent": req.Parent, "owner": req.Owner, "done-when": req.DoneWhen} {
		if value == "" || strings.ContainsAny(value, "\r\n\x00") {
			return fail(fmt.Errorf("invalid or missing %s", name))
		}
	}
	eventActor, err := resolveMutationActor(actorKind, actor, req.Owner)
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	// Fence configuration before invoking Git, including A1's object construction.
	configLock, err := lockCandidateConfig(root)
	if err != nil {
		return fail(err)
	}
	defer configLock.Close()
	originLock, err := lockCandidateOrigin(origin)
	if err != nil {
		return fail(err)
	}
	defer originLock.Close()
	if declared, err := planCanonicalFixtureOrigin(root); err != nil || declared != origin {
		return fail(fmt.Errorf("fixture authority changed"))
	}
	git, err := exec.LookPath("git")
	if err != nil {
		return fail(err)
	}
	// Preserve raw blob bytes for accepted-operation recovery. Metadata-only
	// reads may trim whitespace, but signed PLAN/node/outbox evidence may not.
	runRaw := func(dir string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, git, append([]string{"-C", dir, "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
		cmd.WaitDelay = time.Second
		return cmd.Output()
	}
	run := func(dir string, args ...string) (string, error) {
		out, err := runRaw(dir, args...)
		return strings.TrimSpace(string(out)), err
	}
	for _, item := range []struct{ dir, metadata, bare string }{{root, filepath.Join(root, ".git"), "false"}, {origin, origin, "true"}} {
		for _, name := range []string{"commondir", "objects/info/alternates", "objects/info/http-alternates", "shallow", "info/grafts", "refs/replace"} {
			if _, err := os.Lstat(filepath.Join(item.metadata, filepath.FromSlash(name))); !os.IsNotExist(err) {
				return fail(fmt.Errorf("unsupported Git metadata: %s", name))
			}
		}
		configPath := filepath.Join(item.metadata, "config")
		if err := candidateNoLinks(configPath); err != nil {
			return fail(err)
		}
		config, err := run(item.dir, "config", "--file", configPath, "--no-includes", "--list", "-z")
		if err != nil {
			return fail(err)
		}
		for _, entry := range strings.Split(config, "\x00") {
			if entry == "" {
				continue
			}
			key, value, ok := strings.Cut(entry, "\n")
			allowed := false
			switch key {
			case "core.repositoryformatversion":
				allowed = value == "0"
			case "core.bare":
				allowed = value == item.bare
			case "core.filemode", "core.logallrefupdates", "core.ignorecase", "core.symlinks":
				allowed = value == "true" || value == "false"
			}
			if !ok || !allowed {
				return fail(fmt.Errorf("unsupported fixture config: %s", key))
			}
		}
		actual, err := run(item.dir, "rev-parse", "--absolute-git-dir")
		if err != nil || actual != filepath.ToSlash(item.metadata) {
			return fail(fmt.Errorf("Git physical identity mismatch"))
		}
		bare, err := run(item.dir, "rev-parse", "--is-bare-repository")
		if err != nil || bare != item.bare {
			return fail(fmt.Errorf("Git bare identity mismatch"))
		}
	}
	old, err := run(root, "rev-parse", "--verify", "HEAD")
	if err != nil || !candidateOIDRE.MatchString(old) {
		return fail(fmt.Errorf("source HEAD unavailable"))
	}
	remote, err := run(origin, "rev-parse", "--verify", "refs/heads/canonical")
	if err != nil {
		return fail(fmt.Errorf("canonical ref unavailable"))
	}
	// The already accepted request can be recovered from the exact single
	// canonical child, with no new commit, local projection or authority grant.
	// All other stale operations remain a nonzero conflict.
	if remote != old {
		node, err := recoverCanonicalFixtureNew(root, origin, old, remote, req, eventActor, run, runRaw)
		if err != nil {
			return fail(fmt.Errorf("canonical differs from source HEAD; retry requires reconciliation: %w", err))
		}
		if confirmed, e := run(origin, "rev-parse", "--verify", "refs/heads/canonical"); e != nil || confirmed != remote {
			return fail(fmt.Errorf("canonical ref changed during recovery"))
		}
		if asJSON {
			if e := json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.plan.node.mutation/v1", "action": "created", "source": "git-canonical-fixture", "node": node, "remote_oid": remote, "operation_id": req.ID, "actor": eventActor}); e != nil {
				return fail(fmt.Errorf("accepted recovery JSON output unavailable"))
			}
		} else {
			fmt.Printf("%s %s%s", node.ID, remote, lineEnding)
		}
		return 0
	}
	entries, err := os.ReadDir(planNodeDir(root))
	if err != nil && !os.IsNotExist(err) {
		return fail(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(strings.ToUpper(entry.Name()), "N-") &&
			(entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || !planNodeIDRE.MatchString(strings.TrimSuffix(entry.Name(), ".md"))) {
			return fail(fmt.Errorf("ambiguous node path: %s", entry.Name()))
		}
	}
	nodes, err := listPlanNodes(root)
	if err != nil {
		return fail(err)
	}
	seen := map[string]bool{}
	for _, n := range nodes {
		m := planNodeIDRE.FindStringSubmatch(n.ID)
		if m == nil || seen[m[1]] || filepath.Base(n.Path) != n.ID+".md" || candidateNoLinks(n.Path) != nil {
			return fail(fmt.Errorf("node identity conflict"))
		}
		seen[m[1]] = true
	}
	id, err := nextPlanNodeID(root, req.Title)
	if err != nil {
		return fail(err)
	}
	if err := validateExistingPlanParent(root, req.Parent, id); err != nil {
		return fail(err)
	}
	now := planNodeNow().UTC().Format(time.RFC3339Nano)
	if req.Trigger == "" {
		req.Trigger = req.Title + " @ " + now
	}
	if req.ReturnTo == "" {
		req.ReturnTo = req.Parent
	}
	n := planNode{ID: id, Title: req.Title, Parent: req.Parent, Owner: req.Owner, DoneWhen: req.DoneWhen, Trigger: req.Trigger, ReturnTo: req.ReturnTo, Status: "open", Receipts: []string{}, CreatedAt: now, UpdatedAt: now}
	if err := validatePlanNode(n); err != nil {
		return fail(err)
	}
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		return fail(err)
	}
	spine := strings.ReplaceAll(string(plan), "\r\n", "\n")
	if !strings.Contains(spine, planNodeMarker) {
		spine = strings.TrimRight(spine, "\n") + "\n\n## План: нить и узлы\n\n" + planNodeMarker + "\n"
	}
	plan = []byte(strings.TrimRight(spine, "\n") + "\n" + spineNodeLine(n) + "\n")
	node := renderPlanNode(n)
	outbox, err := json.Marshal(candidateOperation{Schema: "air-worker.plan.candidate-operation/v1", OperationID: req.ID, NodeID: id, Status: n.Status, Parent: n.Parent, DoneWhen: n.DoneWhen, PlanSHA256: learnSHA(plan), NodeSHA256: learnSHA(node)})
	if err != nil {
		return fail(err)
	}
	files := map[string][]byte{"PLAN.md": plan, "plan/" + id + ".md": node, "plan/event-outbox/" + req.ID + ".json": outbox}
	candidate, err := preparePlanCandidate(ctx, root, old, req.ID, files)
	if err != nil {
		return fail(err)
	}
	if err := configLock.Verify(); err != nil {
		return fail(err)
	}
	// The standalone Git fixture publisher must commit its numeric N-ID
	// reservation and canonical PLAN generation in the SAME atomic transaction.
	// No production path is routed here: planCanonicalFixtureOrigin checked
	// disposable OS-temp Git identity above, and production remains blocked.
	if err := publishPlanCandidateAtomicClaim(ctx, root, origin, candidate, old, id); err != nil {
		return fail(err)
	}
	if asJSON {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.plan.node.mutation/v1", "action": "created", "source": "git-canonical-fixture", "node": n, "remote_oid": candidate.OID, "operation_id": req.ID, "actor": eventActor}); err != nil {
			return fail(&planPublishError{State: "pending", Reason: err.Error()})
		}
	} else {
		fmt.Printf("%s %s%s", n.ID, candidate.OID, lineEnding)
	}
	return 0
}
