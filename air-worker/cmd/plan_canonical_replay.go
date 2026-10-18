package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// recoverCanonicalFixtureNew is strictly read-only. A single physical
// publisher has an accepted child commit but the local source is still on its
// parent. Only that exact commit and an identical logical request can be
// acknowledged. This is NOT a production or cross-machine authority grant.
func recoverCanonicalFixtureNew(
	root, origin, old, remote string,
	req sharedPlanCreateRequest,
	actor string,
	run func(string, ...string) (string, error),
	runRaw func(string, ...string) ([]byte, error),
) (planNode, error) {
	conflict := func(message string) (planNode, error) {
		return planNode{}, errors.New(message)
	}
	if !candidateOIDRE.MatchString(old) || !candidateOIDRE.MatchString(remote) ||
		!candidateOperationRE.MatchString(req.ID) || old == remote {
		return conflict("invalid exact replay identity")
	}
	declared, err := planCanonicalFixtureOrigin(root)
	if err != nil || declared != origin || candidatePublishFixture(origin) != nil {
		return conflict("fixture identity changed before recovery")
	}
	// The existing outbox v1 binds node identity and fields, but has no
	// persisted actor override. Never reconstruct such an override as if it
	// had been authenticated. Normal owner-bound requests are supported.
	if actor != req.Owner {
		return conflict("actor override replay requires a sealed actor identity")
	}
	// An immutable remote outbox must NOT justify returning "created" if
	// local native PLAN/nodes/LEARN have changed since the original parent.
	// Foreign WIP outside these native roots is preserved, not a blocker.
	// Verify actual bytes against immutable parent Git blobs, not just
	// git status (skip-worktree/assume-unchanged can hide dirty content).
	projectionUnchanged := func() error {
		for _, rel := range []string{"PLAN.md", "plan", "learn"} {
			if err := candidateNoLinks(filepath.Join(root, rel)); err != nil {
				return err
			}
		}
		status, err := run(root, "status", "--porcelain=v1", "--untracked-files=all", "--ignored", "--", "PLAN.md", "plan", "learn")
		if err != nil || status != "" {
			return errors.New("local native PLAN/node/LEARN projection is dirty")
		}
		paths, err := runRaw(origin, "ls-tree", "-r", "-z", "--name-only", old, "--", "PLAN.md", "plan", "learn")
		if err != nil || len(paths) == 0 || len(paths) > 1024*1024 {
			return errors.New("original native tree not bounded/readable")
		}
		count, total := 0, 0
		for _, entry := range bytes.Split(paths, []byte{0}) {
			if len(entry) == 0 {
				continue
			}
			name := string(entry)
			if name != "PLAN.md" && !strings.HasPrefix(name, "plan/") && !strings.HasPrefix(name, "learn/") {
				return errors.New("untrusted native projection tree path")
			}
			if strings.ContainsAny(name, "\\\r\n\t:") || filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name {
				return errors.New("unsafe native projection tree path")
			}
			full := filepath.Join(root, filepath.FromSlash(name))
			if err := candidateNoLinks(full); err != nil {
				return err
			}
			local, e1 := os.ReadFile(full)
			base, e2 := runRaw(origin, "cat-file", "blob", old+":"+name)
			count++
			total += len(local) + len(base)
			if e1 != nil || e2 != nil || len(local) > 4*1024*1024 || total > 32*1024*1024 ||
				count > 4096 || !bytes.Equal(local, base) {
				return errors.New("local native projection differs from canonical parent blob")
			}
		}
		if count == 0 {
			return errors.New("empty original native projection")
		}
		return nil
	}
	if err := projectionUnchanged(); err != nil {
		return conflict("local native projection changed before replay: " + err.Error())
	}
	typ, err := run(origin, "cat-file", "-t", remote)
	if err != nil || typ != "commit" {
		return conflict("accepted Git commit type unverified")
	}
	parent, err := run(origin, "show", "-s", "--format=%P", remote)
	if err != nil || parent != old {
		return conflict("canonical remote is not the exact direct child")
	}
	message, err := run(origin, "show", "-s", "--format=%B", remote)
	if err != nil || message != "AirWorker candidate\n\nOperation-ID: "+req.ID {
		return conflict("canonical commit operation identity mismatch")
	}
	id, err := nextPlanNodeID(root, req.Title)
	if err != nil || !planNodeIDRE.MatchString(id) {
		return conflict("next node identity cannot be read from the original source")
	}
	names := []string{
		"PLAN.md",
		"plan/" + id + ".md",
		"plan/event-outbox/" + req.ID + ".json",
	}
	changed, err := run(origin, "diff-tree", "--no-commit-id", "--name-only", "-r", old, remote)
	if err != nil || changed != strings.Join(names, "\n") {
		return conflict("accepted canonical delta is not exactly one new node")
	}
	// Reject stale/modified local PLAN even if the remote appears valid.
	localPlan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil {
		return conflict("local original PLAN unavailable")
	}
	oldPlan, err := runRaw(origin, "cat-file", "blob", old+":PLAN.md")
	if err != nil || !bytes.Equal(oldPlan, localPlan) {
		return conflict("local PLAN differs from original canonical generation")
	}
	files := make(map[string][]byte, len(names))
	for _, name := range names {
		raw, err := runRaw(origin, "cat-file", "blob", remote+":"+name)
		if err != nil || len(raw) == 0 || len(raw) > 4*1024*1024 {
			return conflict("immutable candidate blob unavailable")
		}
		files[name] = raw
	}
	if err := validateCandidateContent("", req.ID, files); err != nil {
		return conflict("immutable outbox and PLAN/node identity invalid: " + err.Error())
	}
	node, err := parsePlanNode(id+".md", files[names[1]])
	if err != nil {
		return conflict("accepted node cannot be decoded")
	}
	if node.ID != id || node.Status != "open" || node.Title != req.Title ||
		node.Parent != req.Parent || node.Owner != req.Owner ||
		node.DoneWhen != req.DoneWhen {
		return conflict("accepted node differs from exact logical request")
	}
	wantReturn := req.ReturnTo
	if wantReturn == "" {
		wantReturn = req.Parent
	}
	if node.ReturnTo != wantReturn {
		return conflict("accepted node return-to differs from request")
	}
	if req.Trigger != "" {
		if node.Trigger != req.Trigger {
			return conflict("accepted node trigger differs from request")
		}
	} else if node.Trigger != req.Title+" @ "+node.CreatedAt {
		return conflict("accepted auto-trigger is not bound to node creation")
	}
	if _, err := time.Parse(time.RFC3339Nano, node.CreatedAt); err != nil ||
		node.CreatedAt != node.UpdatedAt || len(node.Receipts) != 0 {
		return conflict("accepted node creation provenance malformed")
	}
	// A canonical child alone is not an accepted atomic node allocation.
	// The original numeric N-ID must still be bound to that EXACT commit,
	// with no symbolic ref or competing slug. The caller already holds the
	// disposable origin lock. This grants no GitHub or production authority.
	match := planNodeIDRE.FindStringSubmatch(id)
	if match == nil {
		return conflict("accepted native node number unavailable")
	}
	claimRef := "refs/air-worker/claims/N-" + match[1]
	claimed, err := run(origin, "for-each-ref", "--format=%(refname)|%(objectname)|%(symref)", claimRef)
	if err != nil || claimed != claimRef+"|"+remote+"|" {
		return conflict("canonical exact replay lacks immutable numeric N-ID claim")
	}
	if accepted, err := run(origin, "rev-parse", "--verify", "refs/heads/canonical"); err != nil || accepted != remote {
		return conflict("canonical ref changed while verifying recovery")
	}
	// Final claim readback fences an independently altered reservation after
	// verifying the canonical ref; mismatch fails closed, never repairs refs.
	if confirmed, err := run(origin, "for-each-ref", "--format=%(refname)|%(objectname)|%(symref)", claimRef); err != nil || confirmed != claimed {
		return conflict("numeric N-ID claim changed while verifying recovery")
	}
	// Recheck the complete local native projection after remote verification.
	// A concurrent local writer invalidates the accepted retry; this does
	// not grant stable-handle production authority.
	if err := projectionUnchanged(); err != nil {
		return conflict("local native projection changed during replay: " + err.Error())
	}
	return node, nil
}
