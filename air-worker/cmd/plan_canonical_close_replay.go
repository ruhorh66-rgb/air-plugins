package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// recoverCanonicalFixtureClose is read-only and fixture-scoped. A successful
// close has already advanced one trusted local Git publisher by one commit.
// Recover ONLY that exact child commit, its frozen receipt, and the matching
// actor. A different request, stale descendant, or unprovable state conflicts.
// This path never changes a PLAN, node, LEARN journal, or canonical Git ref.
func recoverCanonicalFixtureClose(
	root, origin, old, remote, id, requestID, receipt, receiptSHA, actorKind, actor string,
	run func(string, ...string) (string, error),
	runRaw func(string, ...string) ([]byte, error),
) (planNode, string, error) {
	fail := func(reason string) (planNode, string, error) {
		return planNode{}, "", fmt.Errorf("close replay not proven: %s", reason)
	}
	if !candidateOIDRE.MatchString(old) || !candidateOIDRE.MatchString(remote) ||
		old == remote || !candidateOperationRE.MatchString(requestID) || !planNodeIDRE.MatchString(id) {
		return fail("invalid immutable generation or operation")
	}
	if declared, err := planCanonicalFixtureOrigin(root); err != nil || declared != origin ||
		candidatePublishFixture(origin) != nil {
		return fail("fixture publisher identity changed")
	}
	if typ, err := run(origin, "cat-file", "-t", remote); err != nil || typ != "commit" {
		return fail("canonical commit type unavailable")
	}
	if parent, err := run(origin, "show", "-s", "--format=%P", remote); err != nil || parent != old {
		return fail("canonical commit is not the exact direct child")
	}
	if msg, err := run(origin, "show", "-s", "--format=%B", remote); err != nil ||
		msg != "AirWorker candidate\n\nOperation-ID: "+requestID {
		return fail("canonical operation message differs")
	}
	nodePath := "plan/" + id + ".md"
	outboxPath := "plan/event-outbox/" + requestID + ".json"
	names := []string{"PLAN.md", nodePath, outboxPath}
	if changed, err := run(origin, "diff-tree", "--no-commit-id", "--name-only", "-r", old, remote); err != nil ||
		changed != strings.Join(names, "\n") {
		return fail("canonical commit changed unrelated files")
	}
	for _, name := range []string{"PLAN.md", nodePath} {
		localPath := filepath.Join(root, filepath.FromSlash(name))
		if candidateNoLinks(localPath) != nil {
			return fail("local PLAN/node link is untrusted")
		}
		local, localErr := os.ReadFile(localPath)
		base, baseErr := runRaw(origin, "cat-file", "blob", old+":"+name)
		if localErr != nil || baseErr != nil || !bytes.Equal(local, base) {
			return fail("local PLAN/node is not the frozen original generation")
		}
	}
	readBase := func(name string) ([]byte, error) {
		return runRaw(origin, "cat-file", "blob", old+":"+name)
	}
	files := make(map[string][]byte, len(names))
	for _, name := range names {
		raw, err := runRaw(origin, "cat-file", "blob", remote+":"+name)
		if err != nil || len(raw) == 0 || len(raw) > 4*1024*1024 {
			return fail("canonical PLAN/node/outbox blob unavailable")
		}
		files[name] = raw
	}
	outbox, err := decodePlanCloseObject(
		files[outboxPath],
		"schema", "operation_id", "node_id", "status", "actor",
		"receipt_ref", "receipt_sha256", "node_before_sha256", "plan_sha256", "node_sha256",
	)
	if err != nil {
		return fail("outbox is malformed or not unique")
	}
	wantReceipt := "git:" + old + ":" + receipt + "#sha256:" + receiptSHA
	if outbox["operation_id"] != requestID || outbox["node_id"] != id ||
		outbox["receipt_sha256"] != receiptSHA || outbox["receipt_ref"] != wantReceipt {
		return fail("accepted receipt or operation differs from caller")
	}
	before, err := readBase(nodePath)
	if err != nil {
		return fail("original native node unavailable")
	}
	previous, err := parsePlanNode(nodePath, before)
	if err != nil || previous.ID != id || previous.Status != "open" {
		return fail("original node is not canonical open state")
	}
	acceptedActor, err := resolveMutationActor(actorKind, actor, previous.Owner)
	if err != nil || acceptedActor != outbox["actor"] {
		return fail("actor identity differs from accepted close")
	}
	if err := validatePlanCloseCandidate(root, old, requestID, files, readBase); err != nil {
		return fail("immutable close validation rejected candidate: " + err.Error())
	}
	closed, err := parsePlanNode(nodePath, files[nodePath])
	if err != nil || closed.ID != id || closed.Status != "closed" ||
		len(closed.Receipts) != len(previous.Receipts)+1 ||
		closed.Receipts[len(closed.Receipts)-1] != wantReceipt {
		return fail("closed native node differs from accepted receipt")
	}
	if current, err := run(origin, "rev-parse", "--verify", "refs/heads/canonical"); err != nil || current != remote {
		return fail("canonical ref moved during replay readback")
	}
	return closed, acceptedActor, nil
}
