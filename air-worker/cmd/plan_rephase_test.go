package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type n124RephaseFixture struct {
	root          string
	remote        string
	child         planNode
	parentB       planNode
	accepted      string
	head          string
	request       planRephaseRequest
	requestPath   string
	requestSHA    string
	authorityPath string
	authoritySHA  string
	requestID     string
}

func n124Git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=N124", "GIT_AUTHOR_EMAIL=n124@test.local",
		"GIT_COMMITTER_NAME=N124", "GIT_COMMITTER_EMAIL=n124@test.local",
		"GIT_OPTIONAL_LOCKS=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func n124WriteReleaseFixture(t *testing.T, root string) {
	t.Helper()
	doc := map[string]any{
		"schema":         releaseSlicesSchema,
		"policy":         "micro-releases/v1",
		"product":        "AirVera-fixture",
		"decision_date":  "2026-10-25",
		"active_release": "0.2.9",
		"native_plan_validator": map[string]any{
			"state": "fixture-extra-field-must-survive",
		},
		"releases": []any{map[string]any{
			"version":   "0.2.9",
			"status":    "active",
			"stage":     "source",
			"owner":     "gpt-window:n124-test-worker",
			"node_refs": []string{"N-027"},
			"steps": []any{map[string]any{
				"id":             "R0.2.9-01",
				"status":         "open",
				"owner":          "gpt-window:n124-test-worker",
				"trigger":        "release trigger before",
				"done_when":      "release done before",
				"judge":          "source judge",
				"receipt":        "docs/receipts/n124-step.json",
				"rollback_to":    "0.2.8",
				"budget_minutes": 5,
				"title":          "fixture step",
				"kind":           "source",
				"node_refs":      []string{"N-027"},
			}},
		}},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "plan"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plan", "release-slices.json"), append(raw, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	stepRaw, step, err := findPlanRephaseStep(append(raw, '\n'), "0.2.9", "R0.2.9-01")
	if err != nil || len(stepRaw) == 0 || step.Status != "open" {
		t.Fatalf("invalid generic release fixture: step=%+v err=%v", step, err)
	}
}

func n124WriteEvidence(t *testing.T, dir, name string, value any) (string, string) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path, learnSHA(raw)
}

func n124RephaseFixtureSetup(t *testing.T) n124RephaseFixture {
	t.Helper()
	root := seedPlanNodeProduct(t)
	parentA := n112NativeNew(t, root, "N124 parent A", "Stage: supervised tests")
	parentB := n112NativeNew(t, root, "N124 parent B", "Stage: supervised tests")
	child := n112NativeNew(t, root, "N124 bounded child", parentA.ID)
	// Seed metadata unknown to the node parser plus an existing receipt.
	// A rephase may not rewrite or discard either.
	childPathWithHistory := filepath.Join(root, "plan", child.ID+".md")
	childBytes, err := os.ReadFile(childPathWithHistory)
	if err != nil {
		t.Fatal(err)
	}
	historyNeedle := []byte("receipts:\n---\n")
	if bytes.Count(childBytes, historyNeedle) != 1 {
		t.Fatal("unexpected canonical node receipt header shape")
	}
	childBytes = bytes.Replace(childBytes, historyNeedle, []byte("extra_metadata: \"preserve-unknown-field\"\nreceipts:\n  - \"historic-receipt-must-survive\"\n---\n"), 1)
	if err := os.WriteFile(childPathWithHistory, childBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	planPath := filepath.Join(root, "PLAN.md")
	planRaw, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatal(err)
	}
	headerBefore := "**Authority generation:** OLD"
	headerAfter := "**Authority generation:** NEW"
	planRaw = bytes.Replace(planRaw, []byte("# Plan\n"), []byte("# Plan\n\n"+headerBefore+"\n"), 1)
	if err := os.WriteFile(planPath, planRaw, 0o644); err != nil {
		t.Fatal(err)
	}
	n124WriteReleaseFixture(t, root)

	n124Git(t, root, "init")
	n124Git(t, root, "branch", "-M", "n124-fixture")
	n124Git(t, root, "config", "user.name", "N124")
	n124Git(t, root, "config", "user.email", "n124@test.local")
	n124Git(t, root, "add", ".")
	n124Git(t, root, "commit", "-m", "accepted baseline")
	accepted := n124Git(t, root, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "generation.txt"), []byte("generation-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n124Git(t, root, "add", "generation.txt")
	n124Git(t, root, "commit", "-m", "generation 2")
	head := n124Git(t, root, "rev-parse", "HEAD")

	remote := filepath.Join(t.TempDir(), "remote.git")
	n124Git(t, filepath.Dir(remote), "init", "--bare", remote)
	n124Git(t, root, "remote", "add", "origin", remote)
	n124Git(t, root, "push", "-u", "origin", "HEAD:refs/heads/n124-fixture")
	repository := n124Git(t, root, "remote", "get-url", "origin")

	childPath := filepath.Join(root, "plan", child.ID+".md")
	nodeRaw, err := os.ReadFile(childPath)
	if err != nil {
		t.Fatal(err)
	}
	child, err = parsePlanNode(childPath, nodeRaw)
	if err != nil {
		t.Fatal(err)
	}
	planRaw, _ = os.ReadFile(planPath)
	releaseRaw, _ := os.ReadFile(filepath.Join(root, "plan", "release-slices.json"))
	req := planRephaseRequest{
		Schema:                 planRephaseRequestSchema,
		Repository:             repository,
		RemoteName:             "origin",
		CanonicalRef:           "refs/heads/n124-fixture",
		ExpectedHead:           head,
		AcceptedAncestor:       accepted,
		Generation:             "n124-fixture-generation-2",
		HistoryManifestSHA256:  "",
		SourceProvenanceRef:    "refs/heads/n124-fixture",
		SourceProvenanceCommit: head,
		Node: planRephaseNode{
			ID: child.ID, ExpectedSHA256: learnSHA(nodeRaw), ExpectedOwner: child.Owner,
			BeforeParent: child.Parent, AfterParent: parentB.ID,
			BeforeReturnTo: child.ReturnTo, AfterReturnTo: parentB.ID,
			BeforeTrigger: child.Trigger, AfterTrigger: "N124 rephase trigger after",
			BeforeDoneWhen: child.DoneWhen, AfterDoneWhen: "N124 bounded CAS done",
		},
		Plan: planRephasePlan{
			ExpectedSHA256: learnSHA(planRaw), HeaderBefore: headerBefore,
			HeaderBeforeSHA256: learnSHA([]byte(headerBefore)), HeaderAfter: headerAfter,
		},
		Release: planRephaseRelease{
			ExpectedSHA256: learnSHA(releaseRaw), Version: "0.2.9", StepID: "R0.2.9-01",
			BeforeTrigger: "release trigger before", AfterTrigger: "release trigger after",
			BeforeDoneWhen: "release done before", AfterDoneWhen: "release done after",
		},
	}
	evDir := t.TempDir()
	history := planRephaseHistoryEvidence{
		Schema: planRephaseHistorySchema, Repository: req.Repository, CanonicalRef: req.CanonicalRef,
		ExpectedHead: req.ExpectedHead, Generation: req.Generation,
		SourceProvenanceRef: req.SourceProvenanceRef, SourceProvenanceCommit: req.SourceProvenanceCommit,
	}
	req.HistoryManifestPath, req.HistoryManifestSHA256 = n124WriteEvidence(t, evDir, "history-manifest.json", history)
	requestPath, requestSHA := n124WriteEvidence(t, evDir, "request.json", req)
	auth := planRephaseAuthority{
		Schema: planRephaseAuthoritySchema, Operation: "plan.rephase.apply", Approved: true,
		Repository: repository, RemoteName: "origin", CanonicalRef: req.CanonicalRef,
		ExpectedHead: head, Generation: req.Generation, Actor: "gpt-window:n124-test-worker",
		NodeID: child.ID, ReleaseVersion: req.Release.Version, RequestSHA256: requestSHA,
	}
	authorityPath, authoritySHA := n124WriteEvidence(t, evDir, "authority.json", auth)
	return n124RephaseFixture{
		root: root, remote: remote, child: child, parentB: parentB, accepted: accepted, head: head,
		request: req, requestPath: requestPath, requestSHA: requestSHA,
		authorityPath: authorityPath, authoritySHA: authoritySHA, requestID: "n124-rephase-fixture-001",
	}
}

func (f n124RephaseFixture) args() []string {
	return []string{
		f.child.ID,
		"-product", f.root,
		"-manifest", f.requestPath,
		"-manifest-sha256", f.requestSHA,
		"-authority-receipt", f.authorityPath,
		"-authority-sha256", f.authoritySHA,
		"-request-id", f.requestID,
		"-actor-kind", "gpt-window",
		"-actor", "n124-test-worker",
		"-json",
	}
}

func n124RewriteEvidence(t *testing.T, f *n124RephaseFixture, mutate func(*planRephaseRequest)) {
	t.Helper()
	mutate(&f.request)
	evDir := filepath.Dir(f.requestPath)
	f.requestPath, f.requestSHA = n124WriteEvidence(t, evDir, "request-updated.json", f.request)
	auth := planRephaseAuthority{
		Schema: planRephaseAuthoritySchema, Operation: "plan.rephase.apply", Approved: true,
		Repository: f.request.Repository, RemoteName: f.request.RemoteName, CanonicalRef: f.request.CanonicalRef,
		ExpectedHead: f.request.ExpectedHead, Generation: f.request.Generation, Actor: "gpt-window:n124-test-worker",
		NodeID: f.request.Node.ID, ReleaseVersion: f.request.Release.Version, RequestSHA256: f.requestSHA,
	}
	f.authorityPath, f.authoritySHA = n124WriteEvidence(t, evDir, "authority-updated.json", auth)
}

func n124FixtureAuthorityVerifier(root string, req planRephaseRequest, auth planRephaseAuthority, authorityRaw []byte, actor, requestSHA, requestID string) error {
	return validatePlanRephaseAuthority(auth, req, actor, requestSHA)
}

func n124Apply(args []string) int {
	return cmdPlanRephaseApplyWithVerifier(args, n124FixtureAuthorityVerifier)
}

func TestN124PlanRephasePublicCommandHoldsWithoutTrustedAuthorityVerifier(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	nodePath := filepath.Join(f.root, "plan", f.child.ID+".md")
	before, _ := os.ReadFile(nodePath)
	if rc := cmdPlanRephaseApply(f.args()); rc != 3 {
		t.Fatalf("public source-only command rc=%d want 3/HOLD", rc)
	}
	after, _ := os.ReadFile(nodePath)
	if !bytes.Equal(before, after) {
		t.Fatal("public source-only authority HOLD mutated node")
	}
}

func TestN124PlanRephaseAppliesOneNodeOneReleaseAndReplaysIdempotently(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	beforeEvents, _ := os.ReadFile(planNodeEventsPath(f.root))
	if rc := n124Apply(f.args()); rc != 0 {
		t.Fatalf("rephase rc=%d", rc)
	}
	node, err := readPlanNode(filepath.Join(f.root, "plan", f.child.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != "open" || node.Owner != f.child.Owner || node.Title != f.child.Title {
		t.Fatalf("identity/status changed: %+v", node)
	}
	if len(node.Receipts) != 1 || node.Receipts[0] != "historic-receipt-must-survive" || node.UpdatedAt != f.child.UpdatedAt {
		t.Fatalf("historical receipts or untouched timestamp drifted: %+v", node)
	}
	rawNode, _ := os.ReadFile(filepath.Join(f.root, "plan", f.child.ID+".md"))
	if !bytes.Contains(rawNode, []byte("extra_metadata: \"preserve-unknown-field\"")) {
		t.Fatal("unknown node metadata was lost or rewritten")
	}
	if node.Parent != f.parentB.ID || node.ReturnTo != f.parentB.ID ||
		node.Trigger != "N124 rephase trigger after" || node.DoneWhen != "N124 bounded CAS done" {
		t.Fatalf("node rephase mismatch: %+v", node)
	}
	planRaw, _ := os.ReadFile(filepath.Join(f.root, "PLAN.md"))
	if !bytes.Contains(planRaw, []byte("**Authority generation:** NEW")) ||
		bytes.Contains(planRaw, []byte("**Authority generation:** OLD")) {
		t.Fatal("PLAN header was not rephased exactly")
	}
	releaseRaw, _ := os.ReadFile(filepath.Join(f.root, "plan", "release-slices.json"))
	_, step, err := findPlanRephaseStep(releaseRaw, "0.2.9", "R0.2.9-01")
	if err != nil {
		t.Fatal(err)
	}
	if step.Status != "open" || step.Trigger != "release trigger after" || step.DoneWhen != "release done after" {
		t.Fatalf("release step mismatch: %+v", step)
	}
	if !bytes.Contains(releaseRaw, []byte("fixture-extra-field-must-survive")) {
		t.Fatal("generic AirVera-like release metadata was not preserved")
	}
	events, _ := os.ReadFile(planNodeEventsPath(f.root))
	if bytes.Equal(events, beforeEvents) || strings.Count(string(events), "\"source\":\"plan-rephase\"") != 1 {
		t.Fatalf("expected one rephase native event")
	}
	if rc := n124Apply(f.args()); rc != 0 {
		t.Fatalf("idempotent replay rc=%d", rc)
	}
	replayedEvents, _ := os.ReadFile(planNodeEventsPath(f.root))
	if !bytes.Equal(events, replayedEvents) {
		t.Fatal("idempotent replay duplicated native event")
	}
	tx := filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))
	receiptRaw, err := os.ReadFile(filepath.Join(tx, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt planRephaseReceipt
	if err := json.Unmarshal(receiptRaw, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.StatusPreserved || !receipt.ReceiptsPreserved || receipt.SilentClosure ||
		receipt.ImportedHistoricalNodes != 0 || receipt.NoReleaseEffect != true {
		t.Fatalf("unsafe receipt: %+v", receipt)
	}
	remote := strings.Fields(n124Git(t, f.root, "ls-remote", "origin", f.request.CanonicalRef))
	if len(remote) != 2 || remote[0] != f.head || remote[1] != f.request.CanonicalRef {
		t.Fatalf("local rephase changed remote ref: %v", remote)
	}
}

func TestN124PlanRephaseFrozenS3HeadMustHold(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	nodePath := filepath.Join(f.root, "plan", f.child.ID+".md")
	planPath := filepath.Join(f.root, "PLAN.md")
	releasePath := filepath.Join(f.root, "plan", "release-slices.json")
	beforeNode, _ := os.ReadFile(nodePath)
	beforePlan, _ := os.ReadFile(planPath)
	beforeRelease, _ := os.ReadFile(releasePath)
	n124RewriteEvidence(t, &f, func(req *planRephaseRequest) {
		req.ExpectedHead = "f0241f52ce5d0929f1179dadc5f7be8bd95dedf8"
	})
	if rc := n124Apply(f.args()); rc != 3 {
		t.Fatalf("stale S3 head rc=%d want 3/HOLD", rc)
	}
	afterNode, _ := os.ReadFile(nodePath)
	afterPlan, _ := os.ReadFile(planPath)
	afterRelease, _ := os.ReadFile(releasePath)
	if !bytes.Equal(beforeNode, afterNode) || !bytes.Equal(beforePlan, afterPlan) || !bytes.Equal(beforeRelease, afterRelease) {
		t.Fatal("stale S3 CAS mutated native fixture")
	}
}

func TestN124PlanRephaseRejectsStaleDocumentCASAndForeignRef(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*planRephaseRequest)
	}{
		{"stale-node-sha", func(r *planRephaseRequest) { r.Node.ExpectedSHA256 = strings.Repeat("f", 64) }},
		{"stale-plan-sha", func(r *planRephaseRequest) { r.Plan.ExpectedSHA256 = strings.Repeat("f", 64) }},
		{"stale-release-sha", func(r *planRephaseRequest) { r.Release.ExpectedSHA256 = strings.Repeat("f", 64) }},
		{"foreign-ref", func(r *planRephaseRequest) { r.CanonicalRef = "refs/heads/foreign" }},
		{"wrong-ancestor", func(r *planRephaseRequest) { r.AcceptedAncestor = strings.Repeat("1", 40) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := n124RephaseFixtureSetup(t)
			nodePath := filepath.Join(f.root, "plan", f.child.ID+".md")
			beforeNode, _ := os.ReadFile(nodePath)
			n124RewriteEvidence(t, &f, tc.mutate)
			if rc := n124Apply(f.args()); rc == 0 {
				t.Fatal("invalid CAS succeeded")
			}
			afterNode, _ := os.ReadFile(nodePath)
			if !bytes.Equal(beforeNode, afterNode) {
				t.Fatal("invalid CAS mutated node")
			}
		})
	}
}

func TestN124PlanRephaseRejectsOwnerDriftBulkShapeAndAuthorityMismatch(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	n124RewriteEvidence(t, &f, func(r *planRephaseRequest) { r.Node.ExpectedOwner = "foreign-owner" })
	if rc := n124Apply(f.args()); rc == 0 {
		t.Fatal("owner drift succeeded")
	}

	f = n124RephaseFixtureSetup(t)
	raw, _ := os.ReadFile(f.requestPath)
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	generic["nodes"] = []any{map[string]any{"id": f.child.ID}}
	f.requestPath, f.requestSHA = n124WriteEvidence(t, filepath.Dir(f.requestPath), "bulk.json", generic)
	auth := planRephaseAuthority{
		Schema: planRephaseAuthoritySchema, Operation: "plan.rephase.apply", Approved: true,
		Repository: f.request.Repository, RemoteName: "origin", CanonicalRef: f.request.CanonicalRef,
		ExpectedHead: f.request.ExpectedHead, Generation: f.request.Generation, Actor: "gpt-window:n124-test-worker",
		NodeID: f.child.ID, ReleaseVersion: f.request.Release.Version, RequestSHA256: f.requestSHA,
	}
	f.authorityPath, f.authoritySHA = n124WriteEvidence(t, filepath.Dir(f.requestPath), "bulk-authority.json", auth)
	if rc := n124Apply(f.args()); rc != 2 {
		t.Fatalf("bulk/unknown schema rc=%d want 2", rc)
	}

	f = n124RephaseFixtureSetup(t)
	var authDoc planRephaseAuthority
	authRaw, _ := os.ReadFile(f.authorityPath)
	if err := json.Unmarshal(authRaw, &authDoc); err != nil {
		t.Fatal(err)
	}
	authDoc.Actor = "gpt-window:other-writer"
	f.authorityPath, f.authoritySHA = n124WriteEvidence(t, filepath.Dir(f.authorityPath), "wrong-actor.json", authDoc)
	if rc := n124Apply(f.args()); rc != 3 {
		t.Fatalf("wrong authority actor rc=%d want 3", rc)
	}
}

func TestN124PlanRephaseRejectsHeaderDriftDuplicateReleaseAndNonOpenNode(t *testing.T) {
	t.Run("header-drift", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		planPath := filepath.Join(f.root, "PLAN.md")
		planRaw, _ := os.ReadFile(planPath)
		planRaw = bytes.Replace(planRaw, []byte("**Authority generation:** OLD"), []byte("**Authority generation:** DRIFTED"), 1)
		if err := os.WriteFile(planPath, planRaw, 0o644); err != nil {
			t.Fatal(err)
		}
		n124RewriteEvidence(t, &f, func(r *planRephaseRequest) {
			r.Plan.ExpectedSHA256 = learnSHA(planRaw)
		})
		if rc := n124Apply(f.args()); rc != 3 {
			t.Fatalf("header drift rc=%d want 3", rc)
		}
	})

	t.Run("duplicate-release-key", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		releasePath := filepath.Join(f.root, "plan", "release-slices.json")
		raw, _ := os.ReadFile(releasePath)
		old := []byte("\"trigger\": \"release trigger before\"")
		dup := []byte("\"trigger\": \"release trigger before\", \"trigger\": \"release trigger before\"")
		if bytes.Count(raw, old) != 1 {
			t.Fatalf("fixture trigger token count=%d", bytes.Count(raw, old))
		}
		raw = bytes.Replace(raw, old, dup, 1)
		if bytes.Count(raw, []byte("\"trigger\"")) < 2 {
			t.Fatal("duplicate-key fixture was not created")
		}
		if err := os.WriteFile(releasePath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
		n124RewriteEvidence(t, &f, func(r *planRephaseRequest) {
			r.Release.ExpectedSHA256 = learnSHA(raw)
		})
		if rc := n124Apply(f.args()); rc != 3 {
			t.Fatalf("duplicate release key rc=%d want 3", rc)
		}
	})

	t.Run("non-open-target", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		nodePath := filepath.Join(f.root, "plan", f.child.ID+".md")
		nodeRaw, _ := os.ReadFile(nodePath)
		node, err := parsePlanNode(nodePath, nodeRaw)
		if err != nil {
			t.Fatal(err)
		}
		body, err := readPlanNodeBody(nodePath)
		if err != nil {
			t.Fatal(err)
		}
		node.Status = "closed"
		changed := renderPlanNodeWithBody(node, body)
		if err := os.WriteFile(nodePath, changed, 0o644); err != nil {
			t.Fatal(err)
		}
		planPath := filepath.Join(f.root, "PLAN.md")
		if err := updateNodeInSpine(planPath, node); err != nil {
			t.Fatal(err)
		}
		planRaw, _ := os.ReadFile(planPath)
		n124RewriteEvidence(t, &f, func(r *planRephaseRequest) {
			r.Node.ExpectedSHA256 = learnSHA(changed)
			r.Plan.ExpectedSHA256 = learnSHA(planRaw)
		})
		if rc := n124Apply(f.args()); rc != 3 {
			t.Fatalf("closed target rc=%d want 3", rc)
		}
	})
}

func TestN124PlanRephaseRejectsWrongRepositoryMissingAuthorityAndUnicodeMismatch(t *testing.T) {
	t.Run("wrong-repository", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		n124RewriteEvidence(t, &f, func(r *planRephaseRequest) {
			r.Repository = filepath.Join(t.TempDir(), "foreign.git")
		})
		if rc := n124Apply(f.args()); rc != 3 {
			t.Fatalf("wrong repository rc=%d want 3", rc)
		}
	})

	t.Run("missing-authority", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		args := f.args()
		for i := range args {
			if args[i] == f.authorityPath {
				args[i] = filepath.Join(t.TempDir(), "missing-authority.json")
			}
		}
		if rc := n124Apply(args); rc == 0 {
			t.Fatal("missing authority succeeded")
		}
	})

	t.Run("unicode-full-id-mismatch", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		args := f.args()
		args[0] = f.child.ID + "\u0301"
		if rc := n124Apply(args); rc == 0 {
			t.Fatal("foreign Unicode full ID succeeded")
		}
	})
}

func TestN124PlanRephaseRequestIDCannotRebindAfterSuccess(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	if rc := n124Apply(f.args()); rc != 0 {
		t.Fatalf("first apply rc=%d", rc)
	}
	n124RewriteEvidence(t, &f, func(r *planRephaseRequest) {
		// Rebind a valid request to different evidence path with identical
		// checked bytes so the request-id journal, not schema/CAS, rejects it.
		oldPath := r.HistoryManifestPath
		raw, err := os.ReadFile(oldPath)
		if err != nil {
			t.Fatal(err)
		}
		r.HistoryManifestPath = filepath.Join(filepath.Dir(oldPath), "history-copy.json")
		if err := os.WriteFile(r.HistoryManifestPath, raw, 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if rc := n124Apply(f.args()); rc != 3 {
		t.Fatalf("rebound request rc=%d want 3", rc)
	}
}

func TestN124PlanRephasePublishRollbackOnPartialWriteAndReadbackMismatch(t *testing.T) {
	root := t.TempDir()
	files := []planRephaseFile{}
	for i, name := range []string{"node", "PLAN", "release"} {
		path := filepath.Join(root, name)
		before := []byte{byte('a' + i)}
		after := []byte{byte('x' + i)}
		if err := os.WriteFile(path, before, 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, planRephaseFile{Path: path, Before: before, After: after})
	}
	failOnce := true
	ioLayer := planRephaseIO{
		write: func(path string, data []byte) error {
			if filepath.Base(path) == "PLAN" && bytes.Equal(data, files[1].After) && failOnce {
				failOnce = false
				return errors.New("synthetic write failure")
			}
			return os.WriteFile(path, data, 0o600)
		},
		read: os.ReadFile,
	}
	if err := publishPlanRephaseFiles(ioLayer, files); err == nil {
		t.Fatal("partial write failure was accepted")
	}
	for _, f := range files {
		got, _ := os.ReadFile(f.Path)
		if !bytes.Equal(got, f.Before) {
			t.Fatalf("rollback failed for %s", f.Path)
		}
	}

	corruptRead := true
	ioLayer = planRephaseIO{
		write: func(path string, data []byte) error { return os.WriteFile(path, data, 0o600) },
		read: func(path string) ([]byte, error) {
			got, err := os.ReadFile(path)
			if err == nil && filepath.Base(path) == "release" && corruptRead && bytes.Equal(got, files[2].After) {
				corruptRead = false
				return []byte("corrupt"), nil
			}
			return got, err
		},
	}
	if err := publishPlanRephaseFiles(ioLayer, files); err == nil {
		t.Fatal("readback mismatch was accepted")
	}
	for _, f := range files {
		got, _ := os.ReadFile(f.Path)
		if !bytes.Equal(got, f.Before) {
			t.Fatalf("readback rollback failed for %s", f.Path)
		}
	}
}

func TestN124PlanRephaseWrongActorAndStaleAuthorityGenerationHold(t *testing.T) {
	t.Run("unapproved-actor", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		before, _ := os.ReadFile(filepath.Join(f.root, "plan", f.child.ID+".md"))
		args := f.args()
		for i, arg := range args {
			if arg == "n124-test-worker" {
				args[i] = "foreign-actor"
			}
		}
		if rc := n124Apply(args); rc == 0 {
			t.Fatal("unapproved actor succeeded")
		}
		after, _ := os.ReadFile(filepath.Join(f.root, "plan", f.child.ID+".md"))
		if !bytes.Equal(before, after) {
			t.Fatal("unapproved actor mutated node")
		}
	})
	t.Run("stale-authority-generation", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		raw, err := os.ReadFile(f.authorityPath)
		if err != nil {
			t.Fatal(err)
		}
		var auth planRephaseAuthority
		if err := json.Unmarshal(raw, &auth); err != nil {
			t.Fatal(err)
		}
		auth.Generation = "stale-unselected-generation"
		f.authorityPath, f.authoritySHA = n124WriteEvidence(t, filepath.Dir(f.authorityPath), "stale-authority.json", auth)
		if rc := n124Apply(f.args()); rc != 3 {
			t.Fatalf("stale authority generation rc=%d want 3/HOLD", rc)
		}
		if _, err := os.Stat(filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))); !os.IsNotExist(err) {
			t.Fatalf("stale authority unexpectedly created transaction directory: %v", err)
		}
	})
}

func TestN124PlanRephaseInterruptedJournalRejectsBlindReplay(t *testing.T) {
	for _, phase := range []string{"prepared", "event_pending"} {
		t.Run(phase, func(t *testing.T) {
			f := n124RephaseFixtureSetup(t)
			tx := filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))
			if phase == "event_pending" {
				if rc := n124Apply(f.args()); rc != 0 {
					t.Fatalf("fixture apply rc=%d", rc)
				}
			} else {
				if err := os.MkdirAll(tx, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			intentPath := filepath.Join(tx, "intent.json")
			intent := planRephaseIntent{
				Schema: planRephaseIntentSchema, RequestID: f.requestID,
				RequestSHA256: f.requestSHA, AuthoritySHA256: f.authoritySHA,
				Actor: "gpt-window:n124-test-worker", NodeID: f.child.ID,
				Repository: f.request.Repository, CanonicalRef: f.request.CanonicalRef,
				ExpectedHead: f.head, Generation: f.request.Generation, Phase: phase,
			}
			if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
				t.Fatal(err)
			}
			paths := []string{
				filepath.Join(f.root, "plan", f.child.ID+".md"),
				filepath.Join(f.root, "PLAN.md"),
				filepath.Join(f.root, "plan", "release-slices.json"),
				planNodeEventsPath(f.root),
			}
			baseline := make([][]byte, len(paths))
			for i, path := range paths {
				baseline[i], _ = os.ReadFile(path)
			}
			for attempt := 0; attempt < 2; attempt++ {
				if rc := n124Apply(f.args()); rc != 3 {
					t.Fatalf("interrupted journal replay attempt=%d rc=%d want HOLD/3", attempt, rc)
				}
				for i, path := range paths {
					got, _ := os.ReadFile(path)
					if !bytes.Equal(got, baseline[i]) {
						t.Fatalf("interrupted replay mutated %s", path)
					}
				}
			}
		})
	}
}

func TestN124PlanRephaseRollbackIncludesWriteThatFailedAfterReplace(t *testing.T) {
	root := t.TempDir()
	files := []planRephaseFile{}
	for i, name := range []string{"node", "PLAN", "release"} {
		path := filepath.Join(root, name)
		before := []byte{byte('A' + i)}
		after := []byte{byte('X' + i)}
		if err := os.WriteFile(path, before, 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, planRephaseFile{Path: path, Before: before, After: after})
	}
	failAfterReplace := true
	ioLayer := planRephaseIO{
		write: func(path string, raw []byte) error {
			if err := os.WriteFile(path, raw, 0o600); err != nil {
				return err
			}
			if filepath.Base(path) == "PLAN" && bytes.Equal(raw, files[1].After) && failAfterReplace {
				failAfterReplace = false
				return errors.New("simulated post-replace sync failure")
			}
			return nil
		},
		read: os.ReadFile,
	}
	err := publishPlanRephaseFiles(ioLayer, files)
	if err == nil || !strings.Contains(err.Error(), "rollback verified") {
		t.Fatalf("post-replace failure not identified with verified rollback: %v", err)
	}
	for _, file := range files {
		raw, _ := os.ReadFile(file.Path)
		if !bytes.Equal(raw, file.Before) {
			t.Fatalf("failing write target not rolled back: %s", file.Path)
		}
	}
}

func TestN124PlanRephaseMalformedAndSymlinkEvidenceHold(t *testing.T) {
	t.Run("trailing-json", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		raw, _ := os.ReadFile(f.requestPath)
		bad := filepath.Join(t.TempDir(), "trailing.json")
		raw = append(raw, []byte("{}")...)
		if err := os.WriteFile(bad, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		f.requestPath, f.requestSHA = bad, learnSHA(raw)
		if rc := n124Apply(f.args()); rc != 2 {
			t.Fatalf("trailing JSON rc=%d want 2", rc)
		}
	})
	t.Run("symlink-authority", func(t *testing.T) {
		f := n124RephaseFixtureSetup(t)
		link := filepath.Join(t.TempDir(), "linked-authority.json")
		if err := os.Symlink(f.authorityPath, link); err != nil {
			t.Skipf("platform disallows fixture symlink: %v", err)
		}
		f.authorityPath = link
		if rc := n124Apply(f.args()); rc != 2 {
			t.Fatalf("symlink authority rc=%d want 2", rc)
		}
	})
}

func TestN124PlanRephaseCompletedReplayRejectsCorruptReceipt(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	if rc := n124Apply(f.args()); rc != 0 {
		t.Fatalf("first apply rc=%d", rc)
	}
	tx := filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))
	receiptPath := filepath.Join(tx, "receipt.json")
	receiptRaw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receiptPath, bytes.Replace(receiptRaw, []byte("\"status\": \"PASS\""), []byte("\"status\": \"FAIL\""), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	eventsBefore, _ := os.ReadFile(planNodeEventsPath(f.root))
	if rc := n124Apply(f.args()); rc != 3 {
		t.Fatalf("corrupt receipt replay rc=%d want 3/HOLD", rc)
	}
	eventsAfter, _ := os.ReadFile(planNodeEventsPath(f.root))
	if !bytes.Equal(eventsBefore, eventsAfter) {
		t.Fatal("corrupt receipt replay generated another event")
	}
}

func TestN124PlanRephaseReceiptFailureRestoresFilesAndHoldsEvent(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	nodePath := filepath.Join(f.root, "plan", f.child.ID+".md")
	planPath := filepath.Join(f.root, "PLAN.md")
	releasePath := filepath.Join(f.root, "plan", "release-slices.json")
	paths := []string{nodePath, planPath, releasePath}
	before := make([][]byte, len(paths))
	for i, p := range paths {
		before[i], _ = os.ReadFile(p)
	}
	tx := filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))
	if err := os.MkdirAll(filepath.Join(tx, "receipt.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if rc := n124Apply(f.args()); rc == 0 {
		t.Fatal("receipt write failure succeeded")
	}
	for i, p := range paths {
		after, _ := os.ReadFile(p)
		if !bytes.Equal(before[i], after) {
			t.Fatalf("receipt failure did not restore %s", p)
		}
	}
	raw, err := os.ReadFile(filepath.Join(tx, "intent.json"))
	if err != nil {
		t.Fatal(err)
	}
	var intent planRephaseIntent
	if err := json.Unmarshal(raw, &intent); err != nil {
		t.Fatal(err)
	}
	if intent.Phase != "receipt_failed_event_outcome_unknown" {
		t.Fatalf("unexpected post-receipt failure phase=%s", intent.Phase)
	}
	if rc := n124Apply(f.args()); rc != 3 {
		t.Fatalf("unsafe receipt failure replay rc=%d want HOLD/3", rc)
	}
}

func TestN124PlanRephasePreparedCrashAfterEveryWriteReconcilesSnapshots(t *testing.T) {
	for changed := 0; changed <= 3; changed++ {
		t.Run(fmt.Sprintf("after-%d-writes", changed), func(t *testing.T) {
			f := n124RephaseFixtureSetup(t)
			paths := []string{
				filepath.Join(f.root, "plan", f.child.ID+".md"),
				filepath.Join(f.root, "PLAN.md"),
				filepath.Join(f.root, "plan", "release-slices.json"),
			}
			before := make([][]byte, 3)
			for i := range paths {
				before[i], _ = os.ReadFile(paths[i])
			}
			node, err := parsePlanNode(paths[0], before[0])
			if err != nil {
				t.Fatal(err)
			}
			afterNode := node
			afterNode.Parent, afterNode.ReturnTo = f.request.Node.AfterParent, f.request.Node.AfterReturnTo
			afterNode.Trigger, afterNode.DoneWhen = f.request.Node.AfterTrigger, f.request.Node.AfterDoneWhen
			nodeAfter, err := plannedRephaseNode(before[0], node, afterNode)
			if err != nil {
				t.Fatal(err)
			}
			planAfter, err := plannedRephasePlan(before[1], node, afterNode, f.request.Plan)
			if err != nil {
				t.Fatal(err)
			}
			releaseAfter, err := plannedRephaseRelease(f.root, before[2], f.request.Release)
			if err != nil {
				t.Fatal(err)
			}
			after := [][]byte{nodeAfter, planAfter, releaseAfter}
			tx := filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))
			metaPath := filepath.Join(tx, "snapshot.json")
			intentPath := filepath.Join(tx, "intent.json")
			meta := planRephaseSnapshot{
				Schema: "air-worker.plan-rephase-snapshot/v1", RequestID: f.requestID,
				NodeFile: "node.before", PlanFile: "PLAN.before", ReleaseFile: "release-slices.before",
				NodeSHA256: learnSHA(before[0]), PlanSHA256: learnSHA(before[1]), ReleaseSHA256: learnSHA(before[2]),
			}
			metaRaw, _ := json.MarshalIndent(meta, "", "  ")
			metaRaw = append(metaRaw, '\n')
			if err := os.MkdirAll(tx, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := persistPlanRephaseSnapshot(tx, metaPath, metaRaw, before[0], before[1], before[2]); err != nil {
				t.Fatal(err)
			}
			intent := planRephaseIntent{
				Schema: planRephaseIntentSchema, RequestID: f.requestID,
				RequestSHA256: f.requestSHA, AuthoritySHA256: f.authoritySHA,
				Actor: "gpt-window:n124-test-worker", NodeID: f.child.ID,
				Repository: f.request.Repository, CanonicalRef: f.request.CanonicalRef,
				ExpectedHead: f.head, Generation: f.request.Generation,
				BeforeNodeSHA256: learnSHA(before[0]), AfterNodeSHA256: learnSHA(after[0]),
				BeforePlanSHA256: learnSHA(before[1]), AfterPlanSHA256: learnSHA(after[1]),
				BeforeReleaseSHA256: learnSHA(before[2]), AfterReleaseSHA256: learnSHA(after[2]),
				SnapshotSHA256: learnSHA(metaRaw), Phase: "prepared",
			}
			if err := persistPlanRephaseIntent(intentPath, intent); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < changed; i++ {
				if err := os.WriteFile(paths[i], after[i], 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// Reconciliation must not depend on a remote ref remaining at
			// its old SHA after the prepared-phase crash.
			if changed == 2 {
				c := exec.Command("git", "--git-dir", f.remote, "update-ref", "refs/heads/n124-fixture", f.accepted)
				if out, err := c.CombinedOutput(); err != nil {
					t.Fatalf("fixture remote ref advance failed: %v: %s", err, out)
				}
			}
			eventsBefore, _ := os.ReadFile(planNodeEventsPath(f.root))
			if rc := n124Apply(f.args()); rc != 3 {
				t.Fatalf("crash recovery rc=%d want HOLD/3", rc)
			}
			for i := range paths {
				now, _ := os.ReadFile(paths[i])
				if !bytes.Equal(now, before[i]) {
					t.Fatalf("crash recovery drift after %d writes: %s", changed, paths[i])
				}
			}
			eventsAfter, _ := os.ReadFile(planNodeEventsPath(f.root))
			if !bytes.Equal(eventsBefore, eventsAfter) {
				t.Fatal("prepared-phase reconcile emitted native event")
			}
			if rc := n124Apply(f.args()); rc != 3 {
				t.Fatalf("recovered request replay rc=%d want HOLD/3", rc)
			}
			raw, _ := os.ReadFile(intentPath)
			var recovered planRephaseIntent
			if json.Unmarshal(raw, &recovered) != nil || recovered.Phase != "recovered_rollback" {
				t.Fatalf("recovery journal not durable: %+v", recovered)
			}
		})
	}
}

func TestN124PlanRephaseLinkedControlledPlanDirectoryHold(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	originalPlanDir := filepath.Join(f.root, "plan")
	outsidePlanDir := filepath.Join(t.TempDir(), "actual-plan")
	if err := os.Rename(originalPlanDir, outsidePlanDir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsidePlanDir, originalPlanDir); err != nil {
		t.Skipf("platform rejects symlink fixture: %v", err)
	}
	if rc := n124Apply(f.args()); rc == 0 {
		t.Fatal("linked plan directory was accepted")
	}
	if _, err := os.Stat(filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))); !os.IsNotExist(err) {
		t.Fatalf("linked controlled directory created journal: %v", err)
	}
}

func TestN124PlanRephaseJournalLinkedAncestorHold(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	journal := filepath.Join(f.root, ".air-worker", "plan-rephase")
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Dir(journal), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, journal); err != nil {
		t.Skipf("platform disallows directory symlink fixture: %v", err)
	}
	if rc := n124Apply(f.args()); rc != 3 {
		t.Fatalf("linked journal root rc=%d want HOLD/3", rc)
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) > 0 {
		t.Fatalf("journal escaped into linked outside directory: %v %v", entries, err)
	}
}

func TestN124PlanRephaseNativeEventReadbackFailClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "learn"), 0o700); err != nil {
		t.Fatal(err)
	}
	n := planNode{ID: "N-001_n124-fixture", Owner: "fixture-worker"}
	ev := planNodeEventRowWithActor(n, "rephased", "exact-fixture#sha256=aaa", "plan-rephase", time.Now().UTC(), "gpt-window:fixture-worker")
	if err := verifyPlanRephaseNativeEvent(root, sharedLearningSettings{}, false, ev); err == nil {
		t.Fatal("missing event was claimed as readback PASS")
	}
	if err := appendPlanNodeEvents(root, []map[string]any{ev}); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanRephaseNativeEvent(root, sharedLearningSettings{}, false, ev); err != nil {
		t.Fatalf("exact native event not found: %v", err)
	}
	path := planNodeEventsPath(root)
	raw, _ := os.ReadFile(path)
	if err := os.WriteFile(path, bytes.Replace(raw, []byte("exact-fixture"), []byte("tampered-log!"), 1), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanRephaseNativeEvent(root, sharedLearningSettings{}, false, ev); err == nil {
		t.Fatal("tampered native event was claimed as readback PASS")
	}
}

func TestN124PlanRephaseSameParentRequiresUniqueNodeSpine(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	nodePath := filepath.Join(f.root, "plan", f.child.ID+".md")
	nodeRaw, err := os.ReadFile(nodePath)
	if err != nil {
		t.Fatal(err)
	}
	beforeNode, err := parsePlanNode(nodePath, nodeRaw)
	if err != nil {
		t.Fatal(err)
	}
	afterNode := beforeNode
	afterNode.Trigger = "only trigger rephased; parent unchanged"
	planRaw, err := os.ReadFile(filepath.Join(f.root, "PLAN.md"))
	if err != nil {
		t.Fatal(err)
	}
	oldLine := []byte(spineNodeLine(beforeNode))
	if bytes.Count(planRaw, oldLine) != 1 {
		t.Fatalf("fixture PLAN node line not unique")
	}
	withoutNode := bytes.Replace(planRaw, oldLine, nil, 1)
	req := f.request.Plan
	req.ExpectedSHA256 = learnSHA(withoutNode)
	if _, err := plannedRephasePlan(withoutNode, beforeNode, afterNode, req); err == nil {
		t.Fatal("PLAN missing the unchanged-parent node was accepted")
	}
	if _, err := plannedRephasePlan(planRaw, beforeNode, afterNode, f.request.Plan); err != nil {
		t.Fatalf("valid unchanged-parent PLAN rephase should pass: %v", err)
	}
}

func TestN124PlanRephaseLocalEventTimestampDriftHold(t *testing.T) {
	root := t.TempDir()
	node := planNode{ID: "N-001_rephase-time", Owner: "fixture-owner"}
	ev := planNodeEventRowWithActor(node, "rephased", "fixture-time-evidence", "plan-rephase", time.Now().UTC(), "gpt-window:fixture")
	if err := appendPlanNodeEvents(root, []map[string]any{ev}); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanRephaseNativeEvent(root, sharedLearningSettings{}, false, ev); err != nil {
		t.Fatalf("exact local event rejected: %v", err)
	}
	path := planNodeEventsPath(root)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &row); err != nil {
		t.Fatal(err)
	}
	row["created_at"] = "2020-01-01T00:00:00Z"
	corrupt, _ := json.Marshal(row)
	if err := os.WriteFile(path, append(corrupt, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := verifyPlanRephaseNativeEvent(root, sharedLearningSettings{}, false, ev); err == nil {
		t.Fatal("native event with changed created_at was accepted")
	}
}

func TestN124PlanRephaseJournalSnapshotLinkedFileHold(t *testing.T) {
	for _, name := range []string{"node.before", "PLAN.before", "release-slices.before"} {
		t.Run(name, func(t *testing.T) {
			f := n124RephaseFixtureSetup(t)
			tx := filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))
			if err := os.MkdirAll(tx, 0o700); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "untouchable.txt")
			if err := os.WriteFile(outside, []byte("protected outside bytes"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, filepath.Join(tx, name)); err != nil {
				t.Skipf("platform disallows file link fixture: %v", err)
			}
			if rc := n124Apply(f.args()); rc != 3 {
				t.Fatalf("linked snapshot %s rc=%d want HOLD/3", name, rc)
			}
			if bytes, err := os.ReadFile(outside); err != nil || string(bytes) != "protected outside bytes" {
				t.Fatalf("linked snapshot %s changed foreign protected bytes: %q err=%v", name, bytes, err)
			}
			if _, err := os.Stat(filepath.Join(tx, "intent.json")); !os.IsNotExist(err) {
				t.Fatalf("linked snapshot %s advanced journal intent: %v", name, err)
			}
		})
	}
}

func TestN124PlanRephaseSharedModuleEventReadbackIdentity(t *testing.T) {
	for _, badKey := range []string{"product_id", "event_id", "at", "malformed-event-id"} {
		t.Run(badKey, func(t *testing.T) {
			product, settings := sharedProductFixture(t, false)
			node := planNode{ID: "N-001_shared-rephase-evidence", Owner: "fixture-owner"}
			ev := planNodeEventRowWithActor(node, "rephased", "trusted-fixture-identity", "plan-rephase", time.Now().UTC(), "gpt-window:n124-test-worker")
			handled, err := appendSharedPlanEvents(product, []map[string]any{ev})
			if !handled || err != nil {
				t.Fatalf("shared module did not record actual event: handled=%v err=%v", handled, err)
			}
			if err := verifyPlanRephaseNativeEvent(product, settings, true, ev); err != nil {
				t.Fatalf("actual shared Module v0.1.1 event rejected: %v", err)
			}
			path := filepath.Join(settings.RuntimeRoot, "events.jsonl")
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var row map[string]any
			if err := json.Unmarshal(bytes.TrimSpace(raw), &row); err != nil {
				t.Fatal(err)
			}
			if badKey == "malformed-event-id" {
				row["event_id"] = "EV-abcde"
			} else {
				delete(row, badKey)
			}
			modified, _ := json.Marshal(row)
			if err := os.WriteFile(path, append(modified, '\n'), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := verifyPlanRephaseNativeEvent(product, settings, true, ev); err == nil {
				t.Fatalf("shared Module event missing %s incorrectly accepted", badKey)
			}
		})
	}
}

func TestN124PlanRephaseEvidenceBoundsAndExactFullID(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	oversized := filepath.Join(t.TempDir(), "oversized.json")
	if err := os.WriteFile(oversized, bytes.Repeat([]byte("x"), planRephaseMaxEvidence+1), 0o600); err != nil {
		t.Fatal(err)
	}
	args := f.args()
	for i := range args {
		if args[i] == f.authorityPath {
			args[i] = oversized
		}
	}
	if rc := n124Apply(args); rc == 0 {
		t.Fatal("oversized authority receipt succeeded")
	}
	args = f.args()
	args[0] = strings.SplitN(f.child.ID, "_", 2)[0]
	if rc := n124Apply(args); rc != 2 {
		t.Fatalf("numeric-only id rc=%d want 2", rc)
	}
}

// One native request binds the same exact node_refs transition to the active
// release and R0.2.9-01, without losing the original owned FILES-TXN/M3 goal.
func TestN124ScopedNodeRefsAtomicRebindAndIdempotentReadback(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	originalDone := f.request.Node.BeforeDoneWhen
	n124RewriteEvidence(t, &f, func(req *planRephaseRequest) {
		req.Node.AfterParent = req.Node.BeforeParent
		req.Node.AfterReturnTo = req.Node.BeforeReturnTo
		req.Node.AfterTrigger = req.Node.BeforeTrigger + "; limited CalDAV/CardDAV issuer scope"
		req.Node.AfterDoneWhen = originalDone + "; bounded CalDAV/CardDAV issuer scope, exact one-use approval, read-only ETag preflight, no provider write"
		req.Release.BeforeNodeRefs = []string{"N-027"}
		req.Release.AfterNodeRefs = []string{req.Node.ID}
	})
	if rc := n124Apply(f.args()); rc != 0 {
		t.Fatalf("scoped native rephase rc=%d", rc)
	}
	node, err := readPlanNode(filepath.Join(f.root, "plan", f.child.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != "open" || node.Owner != f.child.Owner ||
		node.Parent != f.child.Parent || node.ReturnTo != f.child.ReturnTo ||
		!strings.Contains(node.DoneWhen, originalDone) ||
		len(node.Receipts) != 1 || node.Receipts[0] != "historic-receipt-must-survive" {
		t.Fatalf("scoped rebind lost original node/M3 identity: %+v", node)
	}
	releaseAfter, err := os.ReadFile(filepath.Join(f.root, "plan", "release-slices.json"))
	if err != nil {
		t.Fatal(err)
	}
	activeRelease, _, err := findPlanRephaseReleaseObject(releaseAfter, "0.2.9")
	if err != nil {
		t.Fatal(err)
	}
	activeStep, stepView, err := findPlanRephaseStep(releaseAfter, "0.2.9", "R0.2.9-01")
	if err != nil || stepView.Status != "open" {
		t.Fatalf("step: %v", err)
	}
	for name, raw := range map[string][]byte{"release": activeRelease, "R0.2.9-01": activeStep} {
		refs, refErr := readPlanRephaseNodeRefs(raw)
		if refErr != nil || len(refs) != 1 || refs[0] != f.child.ID {
			t.Fatalf("%s node_refs not atomically rebound: refs=%v err=%v", name, refs, refErr)
		}
	}
	if rc := n124Apply(f.args()); rc != 0 {
		t.Fatalf("same generation native replay rc=%d", rc)
	}
	second, err := os.ReadFile(filepath.Join(f.root, "plan", "release-slices.json"))
	if err != nil || !bytes.Equal(releaseAfter, second) {
		t.Fatal("replay rewrote node_refs or changed release bytes")
	}
	tx := filepath.Join(f.root, ".air-worker", "plan-rephase", learnSHA([]byte(f.requestID)))
	receiptBytes, err := os.ReadFile(filepath.Join(tx, "receipt.json"))
	if err != nil {
		t.Fatal(err)
	}
	var receipt planRephaseReceipt
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatal(err)
	}
	if !receipt.StatusPreserved || !receipt.ReceiptsPreserved || receipt.SilentClosure ||
		!samePlanRephaseRefPair(receipt.BeforeNodeRefs, []string{"N-027"}) ||
		!samePlanRephaseRefPair(receipt.AfterNodeRefs, []string{f.child.ID}) {
		t.Fatalf("native rebind receipt incomplete: %+v", receipt)
	}
}

func TestN124ScopedNodeRefsRejectPartialStaleAndLostM3(t *testing.T) {
	f := n124RephaseFixtureSetup(t)
	good := f.request
	good.Node.AfterParent = good.Node.BeforeParent
	good.Node.AfterReturnTo = good.Node.BeforeReturnTo
	good.Node.AfterTrigger = good.Node.BeforeTrigger + "; bounded issuer scope"
	good.Node.AfterDoneWhen = good.Node.BeforeDoneWhen + "; bounded issuer scope"
	good.Release.BeforeNodeRefs = []string{"N-027"}
	good.Release.AfterNodeRefs = []string{good.Node.ID}
	if err := validatePlanRephaseRequest(good, good.Node.ID); err != nil {
		t.Fatalf("valid scoped node_refs contract rejected: %v", err)
	}
	for name, change := range map[string]func(*planRephaseRequest){
		"missing-after":  func(r *planRephaseRequest) { r.Release.AfterNodeRefs = nil },
		"foreign-target": func(r *planRephaseRequest) { r.Release.AfterNodeRefs[0] = "N-998_foreign" },
		"lost-M3":        func(r *planRephaseRequest) { r.Node.AfterDoneWhen = "only new issuer" },
		"lost-trigger":   func(r *planRephaseRequest) { r.Node.AfterTrigger = "only new issuer" },
		"changed-parent": func(r *planRephaseRequest) { r.Node.AfterParent = f.parentB.ID },
	} {
		r := good
		r.Release.BeforeNodeRefs = append([]string(nil), good.Release.BeforeNodeRefs...)
		r.Release.AfterNodeRefs = append([]string(nil), good.Release.AfterNodeRefs...)
		change(&r)
		if err := validatePlanRephaseRequest(r, r.Node.ID); err == nil {
			t.Fatalf("%s scoped authority was accepted", name)
		}
	}
	original, err := os.ReadFile(filepath.Join(f.root, "plan", "release-slices.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"release", "step"} {
		var doc map[string]any
		if err := json.Unmarshal(original, &doc); err != nil {
			t.Fatal(err)
		}
		release := doc["releases"].([]any)[0].(map[string]any)
		step := release["steps"].([]any)[0].(map[string]any)
		if target == "release" {
			release["node_refs"] = []string{"N-999"}
		} else {
			step["node_refs"] = []string{"N-999"}
		}
		stale, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		stale = append(stale, '\n')
		r := good.Release
		r.ExpectedSHA256 = learnSHA(stale)
		if _, err := plannedRephaseRelease(f.root, stale, r); err == nil {
			t.Fatalf("%s-only stale node_refs would allow partial rebind", target)
		}
	}
}

func TestN124AirVera029ExactN022ReleaseAndStepBinding(t *testing.T) {
	root := t.TempDir()
	n124WriteReleaseFixture(t, root)
	before, err := os.ReadFile(filepath.Join(root, "plan", "release-slices.json"))
	if err != nil {
		t.Fatal(err)
	}
	const n022 = "N-022_files-txn-public-prepare-approve-commit-reconcil"
	req := planRephaseRelease{
		ExpectedSHA256: learnSHA(before), Version: "0.2.9", StepID: "R0.2.9-01",
		BeforeTrigger: "release trigger before", AfterTrigger: "R0.2.9-01 scoped issuer after",
		BeforeDoneWhen: "release done before", AfterDoneWhen: "release done before; caldav.create/carddav.create/update/merge issuer after read-only ETag preflight",
		BeforeNodeRefs: []string{"N-027"}, AfterNodeRefs: []string{n022},
	}
	after, err := plannedRephaseRelease(root, before, req)
	if err != nil {
		t.Fatalf("exact N-022 scoped rebind: %v", err)
	}
	releaseRaw, _, err := findPlanRephaseReleaseObject(after, "0.2.9")
	if err != nil {
		t.Fatal(err)
	}
	stepRaw, step, err := findPlanRephaseStep(after, "0.2.9", "R0.2.9-01")
	if err != nil || step.Status != "open" {
		t.Fatalf("release step: %v", err)
	}
	for name, raw := range map[string][]byte{"0.2.9": releaseRaw, "R0.2.9-01": stepRaw} {
		refs, err := readPlanRephaseNodeRefs(raw)
		if err != nil || len(refs) != 1 || refs[0] != n022 {
			t.Fatalf("%s stale/mixed exact ID refs: %v %v", name, refs, err)
		}
	}
	if !bytes.Contains(after, []byte("fixture-extra-field-must-survive")) {
		t.Fatal("unrelated release metadata disappeared")
	}
}

// This check is intentionally IO-light. It covers the exact AirVera N-022
// atomic release/step CAS and negative contracts without seeding a Git repo.
// The separate native integration test must still pass before source acceptance.
func TestN124ScopedNodeRefsPureNativeContract(t *testing.T) {
	root := t.TempDir()
	n124WriteReleaseFixture(t, root)
	before, err := os.ReadFile(filepath.Join(root, "plan", "release-slices.json"))
	if err != nil {
		t.Fatal(err)
	}
	const n022 = "N-022_files-txn-public-prepare-approve-commit-reconcil"
	const oldDone = "Expose public typed DAV write transaction with immutable preview, exact target/payload digest/authority/TTL/idempotency, single-use approval, exactly-once commit, UNKNOWN reconcile and provider readback; normal host path never invokes internal protocol-grant or hand-builds envelopes."
	const oldTrigger = "After machine routing/authority source exists"
	req := planRephaseRequest{
		Schema:     planRephaseRequestSchema,
		Repository: "fixture-repo", RemoteName: "origin",
		CanonicalRef: "refs/heads/airvera-plan", ExpectedHead: strings.Repeat("a", 40),
		AcceptedAncestor:       strings.Repeat("a", 40),
		Generation:             "fixture-generation-1",
		HistoryManifestSHA256:  strings.Repeat("b", 64),
		HistoryManifestPath:    "fixture-manifest.json",
		SourceProvenanceRef:    "refs/heads/airvera-source",
		SourceProvenanceCommit: strings.Repeat("c", 40),
		Node: planRephaseNode{
			ID: n022, ExpectedSHA256: strings.Repeat("d", 64),
			ExpectedOwner:  "AC-DEV-AirVera",
			BeforeParent:   "M3 Owner workflows and fast path - beta.3",
			AfterParent:    "M3 Owner workflows and fast path - beta.3",
			BeforeReturnTo: "PLAN.md", AfterReturnTo: "PLAN.md",
			BeforeTrigger: oldTrigger, AfterTrigger: oldTrigger + "; bounded 0.2.9 issuer",
			BeforeDoneWhen: oldDone, AfterDoneWhen: oldDone + "; CalDAV/CardDAV scoped issuer, exact one-use grant, read-only ETag preflight, no provider write",
		},
		Plan: planRephasePlan{
			ExpectedSHA256: strings.Repeat("e", 64),
			HeaderBefore:   "plan generation old", HeaderBeforeSHA256: learnSHA([]byte("plan generation old")),
			HeaderAfter: "plan generation new",
		},
		Release: planRephaseRelease{
			ExpectedSHA256: learnSHA(before), Version: "0.2.9", StepID: "R0.2.9-01",
			BeforeTrigger: "release trigger before", AfterTrigger: "release trigger after",
			BeforeDoneWhen: "release done before", AfterDoneWhen: "release done after",
			BeforeNodeRefs: []string{"N-027"}, AfterNodeRefs: []string{n022},
		},
	}
	if err := validatePlanRephaseRequest(req, n022); err != nil {
		t.Fatalf("valid exact AirVera scoped contract rejected: %v", err)
	}
	after, err := plannedRephaseRelease(root, before, req.Release)
	if err != nil {
		t.Fatalf("bounded release/step rebind: %v", err)
	}
	release, _, err := findPlanRephaseReleaseObject(after, "0.2.9")
	if err != nil {
		t.Fatal(err)
	}
	step, _, err := findPlanRephaseStep(after, "0.2.9", "R0.2.9-01")
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string][]byte{"0.2.9": release, "R0.2.9-01": step} {
		refs, err := readPlanRephaseNodeRefs(raw)
		if err != nil || len(refs) != 1 || refs[0] != n022 {
			t.Fatalf("%s stale/ambiguous node refs %v %v", name, refs, err)
		}
	}
	for name, mutate := range map[string]func(*planRephaseRequest){
		"half-pair":      func(r *planRephaseRequest) { r.Release.AfterNodeRefs = nil },
		"foreign-target": func(r *planRephaseRequest) { r.Release.AfterNodeRefs = []string{"N-999_other"} },
		"lost-M3":        func(r *planRephaseRequest) { r.Node.AfterDoneWhen = "new issuer only" },
		"lost-trigger":   func(r *planRephaseRequest) { r.Node.AfterTrigger = "new issuer only" },
		"changed-parent": func(r *planRephaseRequest) { r.Node.AfterParent = "foreign" },
	} {
		copy := req
		copy.Release.BeforeNodeRefs = append([]string(nil), req.Release.BeforeNodeRefs...)
		copy.Release.AfterNodeRefs = append([]string(nil), req.Release.AfterNodeRefs...)
		mutate(&copy)
		if err := validatePlanRephaseRequest(copy, n022); err == nil {
			t.Fatalf("%s crossed scoped contract", name)
		}
	}
	for name, drift := range map[string]func(map[string]any){
		"release": func(rel map[string]any) { rel["node_refs"] = []string{"N-998"} },
		"step": func(rel map[string]any) {
			rel["steps"].([]any)[0].(map[string]any)["node_refs"] = []string{"N-998"}
		},
	} {
		var doc map[string]any
		if err := json.Unmarshal(before, &doc); err != nil {
			t.Fatal(err)
		}
		drift(doc["releases"].([]any)[0].(map[string]any))
		stale, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		stale = append(stale, '\n')
		rev := req.Release
		rev.ExpectedSHA256 = learnSHA(stale)
		if _, err := plannedRephaseRelease(root, stale, rev); err == nil {
			t.Fatalf("%s-only stale references allowed partial transition", name)
		}
	}
}
