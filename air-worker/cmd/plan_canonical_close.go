package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// shortcut: advanced canonical refs and retries fail closed; refresh/recovery and
// external LEARN reconciliation belong to the next A3-2 slice.
func cmdPlanCanonicalFixtureClose(root, origin, id, requestID, receipt, receiptSHA, actorKind, actor string, extra []string, asJSON bool) int {
	fail := func(err error) int {
		action := "conflict"
		if e, ok := err.(*planPublishError); ok {
			action = e.State
		}
		if asJSON {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]string{"schema": "air-worker.plan.node.mutation/v1", "source": "git-canonical-fixture", "action": action, "operation_id": requestID, "error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, err)
		}
		return 2
	}
	if len(extra) != 0 || !candidateOperationRE.MatchString(requestID) || !planNodeIDRE.MatchString(id) {
		return fail(fmt.Errorf("safe request ID and full node ID required; unexpected arguments forbidden"))
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
	// Immutable replay needs raw Git blob bytes; metadata reads may trim them.
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
	if remote != old {
		// Only an accepted direct child for this exact request and receipt
		// may be acknowledged, without any new Git write or local mutation.
		closed, acceptedActor, err := recoverCanonicalFixtureClose(
			root, origin, old, remote, id, requestID, receipt, receiptSHA,
			actorKind, actor, run, runRaw,
		)
		if err != nil {
			return fail(fmt.Errorf("canonical differs from source HEAD; recovery refused: %w", err))
		}
		if asJSON {
			if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
				"schema": "air-worker.plan.node.mutation/v1", "source": "git-canonical-fixture",
				"action": "closed", "operation_id": requestID, "node": closed,
				"remote_oid": remote, "actor": acceptedActor,
			}); err != nil {
				return fail(fmt.Errorf("accepted replay JSON output unavailable"))
			}
		} else {
			fmt.Printf("%s %s%s", id, remote, lineEnding)
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

	nodePath := "plan/" + id + ".md"
	before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(nodePath)))
	if err != nil {
		return fail(err)
	}
	n, err := parsePlanNode(nodePath, before)
	if err != nil {
		return fail(err)
	}
	body, err := parsePlanNodeBody(nodePath, before)
	if err != nil || n.ID != id || n.Status != "open" || !bytes.Equal(renderPlanNodeWithBody(n, body), before) {
		return fail(fmt.Errorf("canonical open node required"))
	}
	eventActor, err := resolveMutationActor(actorKind, actor, n.Owner)
	if err != nil {
		return fail(err)
	}
	if err := validateCloseReceipt(root, receipt, receiptSHA, requestID, id, learnSHA(before)); err != nil {
		return fail(err)
	}
	receiptRef := "git:" + old + ":" + receipt + "#sha256:" + receiptSHA
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		return fail(err)
	}
	closed := n
	closed.Status = "closed"
	closed.UpdatedAt = planNodeNow().UTC().Format(time.RFC3339Nano)
	closed.Receipts = append(append([]string(nil), n.Receipts...), receiptRef)
	plan, err = planCloseSpine(plan, n, closed)
	if err != nil {
		return fail(err)
	}
	node := renderPlanNodeWithBody(closed, body)
	outbox, err := json.Marshal(map[string]string{
		"schema": "air-worker.plan.close-operation/v1", "operation_id": requestID, "node_id": id,
		"status": "closed", "actor": eventActor, "receipt_ref": receiptRef, "receipt_sha256": receiptSHA,
		"node_before_sha256": learnSHA(before), "plan_sha256": learnSHA(plan), "node_sha256": learnSHA(node),
	})
	if err != nil {
		return fail(err)
	}
	files := map[string][]byte{"PLAN.md": plan, nodePath: node, "plan/event-outbox/" + requestID + ".json": outbox}
	candidate, err := preparePlanCloseCandidate(ctx, root, old, requestID, files)
	if err != nil {
		return fail(err)
	}
	if err := configLock.Verify(); err != nil {
		return fail(err)
	}
	if err := publishPlanCandidateCAS(ctx, root, origin, "refs/heads/canonical", candidate, old); err != nil {
		return fail(err)
	}
	if asJSON {
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.plan.node.mutation/v1", "source": "git-canonical-fixture", "action": "closed", "operation_id": requestID, "node": closed, "remote_oid": candidate.OID, "actor": eventActor}); err != nil {
			return fail(&planPublishError{State: "pending", Reason: err.Error()})
		}
	} else {
		fmt.Printf("%s %s%s", id, candidate.OID, lineEnding)
	}
	return 0
}

// Both wire objects are flat string maps with exact, case-sensitive keys.
func decodePlanCloseObject(raw []byte, keys ...string) (map[string]string, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("invalid close object")
	}
	allowed := map[string]bool{}
	for _, key := range keys {
		allowed[key] = true
	}
	fields := map[string]string{}
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || !allowed[key] {
			return nil, fmt.Errorf("unknown or duplicate close field")
		}
		delete(allowed, key)
		token, err = d.Token()
		value, ok := token.(string)
		if err != nil || !ok {
			return nil, fmt.Errorf("close field must be a string")
		}
		fields[key] = value
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || len(allowed) != 0 || d.Decode(new(any)) != io.EOF {
		return nil, fmt.Errorf("incomplete or trailing close object")
	}
	return fields, nil
}

func validateCloseReceipt(root, receipt, digest, requestID, id, beforeSHA string) error {
	if receipt == "." || receipt != path.Clean(receipt) || strings.HasPrefix(receipt, "../") || strings.HasPrefix(receipt, "/") || strings.ContainsAny(receipt, "\\:\x00\r\n") {
		return fmt.Errorf("unsafe receipt path")
	}
	name := filepath.Join(root, filepath.FromSlash(receipt))
	if err := candidateNoLinks(name); err != nil {
		return err
	}
	info, err := os.Stat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return fmt.Errorf("missing or oversized receipt")
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return err
	}
	if learnSHA(raw) != digest {
		return fmt.Errorf("receipt digest mismatch")
	}
	fields, err := decodePlanCloseObject(raw, "schema", "request_id", "node_id", "node_sha256", "result")
	if err != nil {
		return err
	}
	if fields["schema"] != "air-worker.plan.close-receipt/v1" || fields["request_id"] != requestID || fields["node_id"] != id || fields["node_sha256"] != beforeSHA || fields["result"] != "passed" {
		return fmt.Errorf("receipt request/node/result mismatch")
	}
	return nil
}

func planCloseSpine(plan []byte, before, closed planNode) ([]byte, error) {
	text := string(plan)
	oldLine := spineNodeLine(before)
	if strings.Count(text, "[["+before.ID+"]]") != 1 || strings.Count("\n"+text, "\n"+oldLine+"\n") != 1 {
		return nil, fmt.Errorf("canonical unique spine line required")
	}
	return []byte(strings.Replace(text, oldLine+"\n", spineNodeLine(closed)+"\n", 1)), nil
}

func validatePlanCloseCandidate(root, old, operation string, files map[string][]byte, readBase func(string) ([]byte, error)) error {
	raw := files["plan/event-outbox/"+operation+".json"]
	op, err := decodePlanCloseObject(raw, "schema", "operation_id", "node_id", "status", "actor", "receipt_ref", "receipt_sha256", "node_before_sha256", "plan_sha256", "node_sha256")
	if err != nil {
		return err
	}
	id := op["node_id"]
	if !planNodeIDRE.MatchString(id) {
		return fmt.Errorf("invalid close node")
	}
	name := "plan/" + id + ".md"
	before, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return err
	}
	baseNode, err := readBase(name)
	if err != nil || !bytes.Equal(baseNode, before) {
		return fmt.Errorf("close node differs from canonical base")
	}
	prefix, suffix := "git:"+old+":", "#sha256:"+op["receipt_sha256"]
	ref := op["receipt_ref"]
	if !strings.HasPrefix(ref, prefix) || !strings.HasSuffix(ref, suffix) {
		return fmt.Errorf("immutable receipt reference required")
	}
	receipt := strings.TrimSuffix(strings.TrimPrefix(ref, prefix), suffix)
	if err := validateCloseReceipt(root, receipt, op["receipt_sha256"], operation, id, learnSHA(before)); err != nil {
		return err
	}
	baseReceipt, err := readBase(receipt)
	if err != nil || learnSHA(baseReceipt) != op["receipt_sha256"] {
		return fmt.Errorf("receipt differs from canonical base")
	}
	n, err := parsePlanNode(name, before)
	if err != nil {
		return err
	}
	body, err := parsePlanNodeBody(name, before)
	if err != nil {
		return err
	}
	closed, err := parsePlanNode(name, files[name])
	if err != nil {
		return err
	}
	if n.ID != id || n.Status != "open" || !bytes.Equal(before, renderPlanNodeWithBody(n, body)) {
		return fmt.Errorf("noncanonical close source")
	}
	if _, err := time.Parse(time.RFC3339Nano, closed.UpdatedAt); err != nil {
		return err
	}
	expected := n
	expected.Status, expected.UpdatedAt = "closed", closed.UpdatedAt
	expected.Receipts = append(append([]string(nil), n.Receipts...), op["receipt_ref"])
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		return err
	}
	basePlan, err := readBase("PLAN.md")
	if err != nil || !bytes.Equal(basePlan, plan) {
		return fmt.Errorf("PLAN differs from canonical base")
	}
	plan, err = planCloseSpine(plan, n, expected)
	if err != nil {
		return err
	}
	if !bytes.Equal(files[name], renderPlanNodeWithBody(expected, body)) || !bytes.Equal(files["PLAN.md"], plan) || op["schema"] != "air-worker.plan.close-operation/v1" || op["operation_id"] != operation || op["status"] != "closed" || op["actor"] == "" || op["node_before_sha256"] != learnSHA(before) || op["plan_sha256"] != learnSHA(plan) || op["node_sha256"] != learnSHA(files[name]) {
		return fmt.Errorf("close candidate content mismatch")
	}
	return nil
}
