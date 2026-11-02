package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

const semanticNodeSourceSchema = "air-worker.semantic-native-node/v1"
const semanticNodeMaxArtifact = 48 * 1024

var semanticSourceLineCitation = regexp.MustCompile(`(?i)[[:alnum:]_./\\-]+\.(go|ts|tsx|js|jsx|py|md|json|ps1)(:[0-9]+|#L[0-9]+|[[:space:]]+line[[:space:]]+[0-9]+)`)

type semanticNodeSourceScope struct {
	Node           planNode `json:"node"`
	SourceCommit   string   `json:"source_commit"`
	ArtifactPath   string   `json:"artifact_path"`
	ArtifactSHA256 string   `json:"artifact_sha256"`
	ArtifactBytes  int      `json:"artifact_bytes"`
	ArtifactText   string   `json:"artifact_text"`
}

type semanticNodeSourceRequest struct {
	NodeID           string
	SourceCommit     string
	ArtifactPath     string
	ArtifactSHA256   string
	Executor         string
	ExpectedReviewer string
	ExpectedEffort   string
}

type semanticNodeMachineResult struct {
	Schema                string `json:"schema"`
	Phase                 string `json:"phase"`
	Code                  int    `json:"code"`
	NodeID                string `json:"node_id"`
	SourceCommit          string `json:"source_commit"`
	ArtifactPath          string `json:"artifact_path"`
	ArtifactSHA256        string `json:"artifact_sha256"`
	ReviewerVendor        string `json:"reviewer_vendor"`
	ReviewerModel         string `json:"reviewer_model"`
	ReviewerEffort        string `json:"reviewer_effort"`
	ReviewScope           string `json:"review_scope"`
	ModelInvoked          bool   `json:"model_invoked"`
	FactualChecksExecuted bool   `json:"factual_checks_executed"`
	TaskPassed            bool   `json:"task_passed"`
	Verdict               string `json:"verdict,omitempty"`
	Detail                string `json:"detail,omitempty"`
}

func semanticSourceCited(verdict semanticVerdict) bool {
	for _, evidence := range verdict.Evidence {
		if semanticSourceLineCitation.MatchString(evidence) {
			return true
		}
	}
	return false
}

func gitReadOnly(root string, args ...string) ([]byte, error) {
	// Git is used only for SHA identity and frozen readback, never for hooks,
	// builds, test execution, code checkout or writes.
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("source Git readback: %w", err)
	}
	return out, nil
}

func readGitPinnedSource(root, commit, relative string) ([]byte, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.IsLocal(relative) == false {
		return nil, fmt.Errorf("source artifact must be a file within the product root")
	}
	return gitReadOnly(root, "show", commit+":"+filepath.ToSlash(relative))
}

func verifySemanticNodeSource(root string, request semanticNodeSourceRequest) (*semanticNodeSourceScope, runnerSpec, error) {
	if !planNodeIDRE.MatchString(request.NodeID) {
		return nil, runnerSpec{}, fmt.Errorf("exact native node ID required, e.g. N-015_title; legacy -step and short N-ID are not accepted")
	}
	if len(request.SourceCommit) != 40 {
		return nil, runnerSpec{}, fmt.Errorf("-source-commit must be exactly 40 lowercase Git hex characters")
	}
	for _, c := range request.SourceCommit {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, runnerSpec{}, fmt.Errorf("-source-commit must be lowercase Git hex")
		}
	}
	if len(request.ArtifactSHA256) != 64 {
		return nil, runnerSpec{}, fmt.Errorf("-artifact-sha256 must contain 64 lowercase hex characters")
	}
	for _, c := range request.ArtifactSHA256 {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return nil, runnerSpec{}, fmt.Errorf("-artifact-sha256 must be lowercase hex")
		}
	}
	if request.Executor != "codex" {
		return nil, runnerSpec{}, fmt.Errorf("native source-only reviewer requires explicit -executor codex (OpenAI executor identity)")
	}
	if request.ExpectedReviewer != "claude-sonnet-5-5" || request.ExpectedEffort != "low" {
		return nil, runnerSpec{}, fmt.Errorf("native source-only reviewer must assert -reviewer claude-sonnet-5-5 -effort low; no silent provider/model substitute")
	}
	nodePath, err := planNodePathByID(root, request.NodeID)
	if err != nil {
		return nil, runnerSpec{}, fmt.Errorf("native node not resolved: %w", err)
	}
	node, err := readPlanNode(nodePath)
	if err != nil || node.ID != request.NodeID {
		return nil, runnerSpec{}, fmt.Errorf("native node metadata mismatch: %v", err)
	}
	if node.Status != "open" {
		return nil, runnerSpec{}, fmt.Errorf("native node %s has status %s, expected open", node.ID, node.Status)
	}
	plan, err := os.ReadFile(filepath.Join(root, "PLAN.md"))
	if err != nil || bytes.Count(plan, []byte("[["+request.NodeID+"]]")) != 1 {
		return nil, runnerSpec{}, fmt.Errorf("native node %s must appear exactly once in canonical PLAN.md spine", request.NodeID)
	}
	commit, err := gitReadOnly(root, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(commit)) != request.SourceCommit {
		return nil, runnerSpec{}, fmt.Errorf("source Git HEAD differs from -source-commit %s", request.SourceCommit)
	}
	relativeNode := filepath.ToSlash(filepath.Join("plan", request.NodeID+".md"))
	nodeFrozen, err := readGitPinnedSource(root, request.SourceCommit, relativeNode)
	if err != nil {
		return nil, runnerSpec{}, fmt.Errorf("native node not present at pinned commit: %w", err)
	}
	nodeCurrent, err := os.ReadFile(nodePath)
	if err != nil || !bytes.Equal(nodeFrozen, nodeCurrent) {
		return nil, runnerSpec{}, fmt.Errorf("native node bytes differ from pinned source commit")
	}
	planFrozen, err := readGitPinnedSource(root, request.SourceCommit, "PLAN.md")
	if err != nil || !bytes.Equal(planFrozen, plan) {
		return nil, runnerSpec{}, fmt.Errorf("canonical PLAN.md differs from pinned source commit")
	}
	artifact := strings.TrimSpace(request.ArtifactPath)
	if artifact == "" {
		return nil, runnerSpec{}, fmt.Errorf("-artifact is required and must identify one SHA-pinned, tracked source file")
	}
	if !filepath.IsAbs(artifact) {
		artifact = filepath.Join(root, artifact)
	}
	path, err := taskFileInsideProduct(root, artifact)
	if err != nil {
		return nil, runnerSpec{}, fmt.Errorf("source artifact path: %w", err)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > semanticNodeMaxArtifact || info.Size() == 0 {
		return nil, runnerSpec{}, fmt.Errorf("source artifact must be a nonempty regular file within %d bytes", semanticNodeMaxArtifact)
	}
	raw, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(raw) {
		return nil, runnerSpec{}, fmt.Errorf("source artifact must be UTF-8 and readable: %v", err)
	}
	sha := sha256.Sum256(raw)
	if hex.EncodeToString(sha[:]) != request.ArtifactSHA256 {
		return nil, runnerSpec{}, fmt.Errorf("source artifact SHA256 mismatch; model not started")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return nil, runnerSpec{}, fmt.Errorf("source artifact escaped product")
	}
	frozenArtifact, err := readGitPinnedSource(root, request.SourceCommit, rel)
	if err != nil || !bytes.Equal(raw, frozenArtifact) {
		return nil, runnerSpec{}, fmt.Errorf("source artifact not tracked at exact pinned commit or bytes differ")
	}
	scope := &semanticNodeSourceScope{
		Node: node, SourceCommit: request.SourceCommit,
		ArtifactPath: filepath.ToSlash(rel), ArtifactSHA256: request.ArtifactSHA256,
		ArtifactBytes: len(raw), ArtifactText: string(raw),
	}
	return scope, runnerSpec{Kind: "codex"}, nil
}

func emitSemanticNodeMachine(doc semanticNodeMachineResult) {
	_ = json.NewEncoder(os.Stdout).Encode(doc)
}

func cmdSemanticNativeNode(root string, cfg runConfig, request semanticNodeSourceRequest) int {
	doc := semanticNodeMachineResult{
		Schema: semanticNodeSourceSchema, Phase: "REJECTED_PRE_MODEL", Code: 2,
		NodeID: request.NodeID, SourceCommit: request.SourceCommit,
		ArtifactPath: request.ArtifactPath, ArtifactSHA256: request.ArtifactSHA256,
		ReviewerVendor: "anthropic", ReviewerModel: request.ExpectedReviewer,
		ReviewerEffort: request.ExpectedEffort, ReviewScope: "source-only",
		FactualChecksExecuted: false, ModelInvoked: false, TaskPassed: false,
	}
	source, executor, err := verifySemanticNodeSource(root, request)
	if err != nil {
		doc.Detail = err.Error()
		emitSemanticNodeMachine(doc)
		return 2
	}
	reviewer, _, err := cfg.SemanticJudge.Resolve(executor, activeProductReleaseVersion(root))
	if err != nil || reviewer.Kind != "claude" || reviewer.Model != request.ExpectedReviewer || reviewer.Effort != request.ExpectedEffort {
		doc.Detail = fmt.Sprintf("reviewer policy mismatch: expected Anthropic %s/%s, resolved %+v: %v", request.ExpectedReviewer, request.ExpectedEffort, reviewer, err)
		emitSemanticNodeMachine(doc)
		return 2
	}
	step := workStep{Num: source.Node.ID, Title: source.Node.Title, Tier: "source-only"}
	ctx := &loopCtx{
		Root: root, Cfg: cfg, goals: readPlanGoals(filepath.Join(root, "PLAN.md")),
		SemanticSource: source,
	}
	// File content, node and frozen source identity are passed as actual bytes.
	// This scoped SHA preflight is factual code=0 but executes NO product tests.
	factual := semanticFactualPacket{
		Scope: "source-hash-only", Code: 0,
		Text:    "Native node, Git source commit, tracked artifact bytes and SHA256 verified. Product/functional QA NOT_RUN.",
		Passed:  []string{"exact_source_artifact_sha256"},
		Unknown: []string{"functional tests NOT_RUN"},
	}
	run := ctx.semanticJudge(step, executor, factual,
		"Review only this exact native node source and pinned artifact; do not run QA or claim task completion")
	doc.ModelInvoked = run.ModelInvoked
	if run.Error != "" {
		if run.ModelInvoked {
			doc.Phase = "MODEL_INVOKED_SOURCE_NOT_PROVEN"
		} else {
			doc.Phase = "REJECTED_PRE_MODEL"
		}
		doc.Detail = run.Error
		emitSemanticNodeMachine(doc)
		return 2
	}
	doc.Verdict = run.Verdict.Verdict
	doc.Phase = "MODEL_INVOKED_SOURCE_NOT_PROVEN"
	doc.Code = 2
	if run.Verdict.Verdict == "PASS" && run.Verdict.StepDone == "yes" && run.Verdict.Drift == "none" {
		doc.Phase = "SUBSTANTIVE_SOURCE_REVIEW_NOT_TASK_PASS"
		doc.Code = 0
	} else if run.Verdict.Verdict == "DRIFT" {
		doc.Phase = "SOURCE_REVIEW_DRIFT"
		doc.Code = 1
	}
	doc.ArtifactPath = source.ArtifactPath
	emitSemanticNodeMachine(doc)
	return doc.Code
}
