package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSourceReviewSeparateNodeCheckoutBinding(t *testing.T) {
	work := t.TempDir()
	nodeRoot := filepath.Join(work, "product")
	if err := os.Mkdir(nodeRoot, 0700); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, nodeRoot, "init")
	codingFixtureGit(t, nodeRoot, "config", "--local", "user.name", "AirWorker Test")
	codingFixtureGit(t, nodeRoot, "config", "--local", "user.email", "airworker-test@example.invalid")
	if err := os.WriteFile(filepath.Join(nodeRoot, "code.py"), []byte("print('source')\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeRoot, ".gitignore"), []byte(".woody/\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := executorTestConfig()
	cfg.Orchestration.RequireAnthropicReviewer = true
	cfgBytes, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeRoot, "run-config.json"), cfgBytes, 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, nodeRoot, "add", "code.py", ".gitignore", "run-config.json")
	codingFixtureGit(t, nodeRoot, "commit", "-m", "frozen source")
	sourceHead, err := gitCodingOutput(nodeRoot, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot := filepath.Join(work, "frozen-source")
	codingFixtureGit(t, nodeRoot, "worktree", "add", "--detach", sourceRoot, sourceHead)
	node := planNode{ID: "N-130_product-review", Title: "Review", Parent: "N-129_parent", Trigger: "Review frozen code", Owner: "owner", DoneWhen: "Check exact frozen source", Status: "open", ReturnTo: "N-129_parent", CreatedAt: "2026-11-05T00:00:00Z", UpdatedAt: "2026-11-05T00:00:00Z"}
	if err := os.Mkdir(filepath.Join(nodeRoot, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	nodePath := filepath.Join(nodeRoot, "plan", node.ID+".md")
	nodeBytes := renderPlanNode(node)
	if err := os.WriteFile(nodePath, nodeBytes, 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, nodeRoot, "add", "plan")
	codingFixtureGit(t, nodeRoot, "commit", "-m", "add later review node")
	nodeHead, err := gitCodingOutput(nodeRoot, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	got, err := bindSourceReviewNode(sourceRoot, sourceHead, nodeRoot, nodeHead, "N-130")
	if err != nil || got.Node.ID != node.ID || got.Head != nodeHead || got.SHA256 != sha256Hex(nodeBytes) {
		t.Fatalf("binding failed: %+v %v", got, err)
	}
	state := t.TempDir()
	identity := sessionIdentity{Principal: "codex", SessionKey: "node-binding-test"}
	if err := writeStateJSON(sessionModePath(state, identity), sessionModeState{Enabled: true, Principal: identity.Principal, SessionKey: identity.SessionKey}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, identity), sessionProductState{Path: sourceRoot, Principal: identity.Principal, SessionKey: identity.SessionKey}); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(sourceRoot, ".woody"), 0700); err != nil {
		t.Fatal(err)
	}
	task := filepath.Join(sourceRoot, ".woody", "task.md")
	if err := os.WriteFile(task, []byte("Review the frozen source."), 0600); err != nil {
		t.Fatal(err)
	}
	oldInvoke := invokeNativeSourceReviewWithNode
	defer func() { invokeNativeSourceReviewWithNode = oldInvoke }()
	called := false
	invokeNativeSourceReviewWithNode = func(_ sessionScope, _ string, _ runnerSpec, _, _ string, _ *sourceReviewNodeBinding) (string, string, error) {
		called = true
		if err := os.WriteFile(nodePath, append(append([]byte(nil), nodeBytes...), []byte("changed")...), 0600); err != nil {
			t.Fatal(err)
		}
		return "{}", "synthetic", nil
	}
	rc := cmdSourceReview([]string{"-product", sourceRoot, "-node", "N-130", "-node-product", nodeRoot, "-node-head", nodeHead, "-source-head", sourceHead, "-task-file", task, "-principal", identity.Principal, "-session-key", identity.SessionKey, "-state-dir", state, "-json"})
	if !called || rc != 2 {
		t.Fatalf("changed node not rejected after runner: called=%v rc=%d", called, rc)
	}
	if err := os.WriteFile(nodePath, nodeBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bindSourceReviewNode(sourceRoot, nodeHead, nodeRoot, nodeHead, "N-130"); err == nil {
		t.Fatal("source SHA different from checkout accepted")
	}
	if _, err := bindSourceReviewNode(sourceRoot, sourceHead, nodeRoot, sourceHead, "N-130"); err == nil {
		t.Fatal("node SHA different from checkout accepted")
	}
	if _, err := bindSourceReviewNode(sourceRoot, sourceHead, filepath.Join(nodeRoot, "plan"), nodeHead, "N-130"); err == nil {
		t.Fatal("different product path accepted")
	}
	unrelated := filepath.Join(work, "unrelated")
	if err := os.Mkdir(unrelated, 0700); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, unrelated, "init")
	codingFixtureGit(t, unrelated, "config", "--local", "user.name", "AirWorker Test")
	codingFixtureGit(t, unrelated, "config", "--local", "user.email", "airworker-test@example.invalid")
	if err := os.Mkdir(filepath.Join(unrelated, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unrelated, "plan", node.ID+".md"), nodeBytes, 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, unrelated, "add", "plan")
	codingFixtureGit(t, unrelated, "commit", "-m", "unrelated node")
	unrelatedHead, err := gitCodingOutput(unrelated, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bindSourceReviewNode(sourceRoot, sourceHead, unrelated, unrelatedHead, "N-130"); err == nil {
		t.Fatal("unrelated product history accepted")
	}
	if err := os.WriteFile(nodePath, append(append([]byte(nil), nodeBytes...), []byte("changed")...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := bindSourceReviewNode(sourceRoot, sourceHead, nodeRoot, nodeHead, "N-130"); err == nil {
		t.Fatal("dirty node checkout accepted")
	}
	if err := os.WriteFile(nodePath, nodeBytes, 0600); err != nil {
		t.Fatal(err)
	}
	node.Status = "closed"
	if err := os.WriteFile(nodePath, renderPlanNode(node), 0600); err != nil {
		t.Fatal(err)
	}
	codingFixtureGit(t, nodeRoot, "add", "plan")
	codingFixtureGit(t, nodeRoot, "commit", "-m", "close node")
	closedHead, err := gitCodingOutput(nodeRoot, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bindSourceReviewNode(sourceRoot, sourceHead, nodeRoot, closedHead, "N-130"); err == nil {
		t.Fatal("closed node accepted")
	}
}

func TestSourceReviewNodeBindingReceiptMetadata(t *testing.T) {
	root := t.TempDir()
	scope := newScope(root, sessionIdentity{Principal: "codex", SessionKey: "node-binding-test"})
	t.Setenv("AW_N123_CHILD_MARKER", filepath.Join(root, "marker"))
	cmd := exec.Command(os.Args[0], "-test.run=^TestN123ReceiptChildHelper$")
	meta := jobReceiptMeta{ReviewTaskSHA256: "task-sha", ReviewNodeID: "N-130_test", ReviewNodeProduct: "node-root", ReviewNodeCommit: "node-head", ReviewNodeSHA256: "node-sha"}
	if _, err := runReceiptedWithMeta(context.Background(), scope, "N130", "node-binding", cmd, meta); err != nil {
		t.Fatal(err)
	}
	receiptPath, _ := jobReceiptPaths(scope, "N130", "node-binding")
	receipt, err := readJobReceipt(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != jobStatusDone || receipt.ReviewTaskSHA256 != meta.ReviewTaskSHA256 || receipt.ReviewNodeID != meta.ReviewNodeID || receipt.ReviewNodeProduct != meta.ReviewNodeProduct || receipt.ReviewNodeCommit != meta.ReviewNodeCommit || receipt.ReviewNodeSHA256 != meta.ReviewNodeSHA256 || receipt.OutputSHA256 == "" {
		t.Fatal("terminal receipt lost node binding or output seal")
	}
}
