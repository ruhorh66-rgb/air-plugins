package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
			"version": "0.2.9",
			"status":  "active",
			"stage":   "source",
			"owner":   "gpt-window:n124-test-worker",
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
				"node_refs":      []any{},
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
		HistoryManifestSHA256:  strings.Repeat("a", 64),
		SourceProvenanceRef:    "fixture/source",
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

func n124FixtureAuthorityVerifier(root string, req planRephaseRequest, auth planRephaseAuthority, authorityRaw []byte, actor, requestSHA string) error {
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
		args[0] = f.child.ID + "?"
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
		r.SourceProvenanceRef = "fixture/changed-provenance"
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
