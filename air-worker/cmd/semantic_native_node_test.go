package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const nodeFixtureID = "N-015_native-source-review"

func makeNativeSemanticFixture(t *testing.T) (root, commit, artifactSHA string) {
	t.Helper()
	root = t.TempDir()
	gitFixtureRun(t, root, "init", "-b", "source")
	if err := os.MkdirAll(filepath.Join(root, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	plan := "# PLAN\n\n## Canonical native nodes\n\n<!-- air-worker-plan-nodes -->\n" +
		"- [open] parent-stage · owner:fixture · [[" + nodeFixtureID + "]] · Static source review\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0600); err != nil {
		t.Fatal(err)
	}
	node := `---
id: "N-015_native-source-review"
title: "Static source review"
parent: "parent-stage"
trigger: "LPR: source-only review, never execute blocked tests"
owner: "fixture sole owner"
done_when: "Anthropic reads pinned source and reports risks, without functional tests"
status: "open"
return_to: "parent-stage"
created_at: "2026-11-02T00:00:00Z"
updated_at: "2026-11-02T00:00:00Z"
receipts:
---

# Static source review
`
	if err := os.WriteFile(filepath.Join(root, "plan", nodeFixtureID+".md"), []byte(node), 0600); err != nil {
		t.Fatal(err)
	}
	artifact := "package fixture\n// Source proof line\nfunc SourceProof() bool { return true }\n"
	if err := os.WriteFile(filepath.Join(root, "source.go"), []byte(artifact), 0600); err != nil {
		t.Fatal(err)
	}
	// If the native static-only route called the ordinary factual judge this
	// selected command would run. Its existence must never be interpreted as QA.
	cfg := runConfig{Judge: judgeSpec{Checks: []checkSpec{
		{Name: "must-not-execute", Command: filepath.Join(root, "missing-qa-executable.exe")},
	}}}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	gitFixtureRun(t, root, "add", "PLAN.md", "plan", "source.go", "run-config.json")
	gitFixtureRun(t, root, "commit", "-m", "frozen source-only fixture")
	commit = gitFixtureRun(t, root, "rev-parse", "HEAD")
	digest := sha256.Sum256([]byte(artifact))
	artifactSHA = hex.EncodeToString(digest[:])
	return
}

func nativeSemanticArgs(root, commit, sha string) []string {
	return []string{
		"-product", root,
		"-node", nodeFixtureID,
		"-scope", "source-only",
		"-source-commit", commit,
		"-artifact", "source.go",
		"-artifact-sha256", sha,
		"-executor", "codex",
		"-reviewer", "claude-sonnet-5-5",
		"-effort", "low",
	}
}

func TestNativeSemanticSourceOnlyAcceptedWithoutFunctionalJudge(t *testing.T) {
	root, commit, sha := makeNativeSemanticFixture(t)
	original := semanticCommand
	t.Cleanup(func() { semanticCommand = original })
	launched := 0
	var argsSeen []string
	semanticCommand = func(_ string, args ...string) *exec.Cmd {
		launched++
		argsSeen = append([]string(nil), args...)
		return exec.Command(os.Args[0], "-test.run=TestNativeSemanticHelperProcess", "--", "PASS")
	}
	output, rc := captureStdout(t, func() int {
		return cmdSemantic(nativeSemanticArgs(root, commit, sha))
	})
	if rc != 0 || launched != 1 || !strings.Contains(output, "SUBSTANTIVE_SOURCE_REVIEW_NOT_TASK_PASS") {
		t.Fatalf("source-only review did not execute expected provider exactly once: rc=%d launched=%d output=%s", rc, launched, output)
	}
	joined := strings.Join(argsSeen, " ")
	if !strings.Contains(joined, "--allowed-tools Read,Glob,Grep") ||
		!strings.Contains(joined, "--model claude-sonnet-5-5") ||
		!strings.Contains(joined, "--effort low") ||
		strings.Contains(joined, "--dangerously-skip-permissions") {
		t.Fatalf("non-canonical source-only Anthropic reviewer route: %v", argsSeen)
	}
	receiptFile := filepath.Join(root, ".woody", "semantic-verdict.json")
	var rec semanticRecord
	if err := readJSON(receiptFile, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.NodeID != nodeFixtureID || rec.SourceCommit != commit ||
		rec.ArtifactSHA256 != sha || rec.ReviewMode != "source-only" ||
		rec.FactualChecksExecuted == nil || *rec.FactualChecksExecuted ||
		!rec.ModelInvoked || rec.ReviewStage != "SUBSTANTIVE_SOURCE_REVIEW" ||
		rec.Verdict == nil || rec.Verdict.Verdict != "PASS" ||
		rec.ReviewerVendor != "claude" || rec.ReviewerModel != "claude-sonnet-5-5" {
		t.Fatalf("source-only semantic receipt not self-describing: %+v", rec)
	}
	if _, err := os.Stat(filepath.Join(root, "missing-qa-executable.exe")); !os.IsNotExist(err) {
		t.Fatal("no executable QA command should be invoked")
	}
	current := gitFixtureRun(t, root, "status", "--porcelain", "--", "PLAN.md", "plan", "source.go")
	if current != "" {
		t.Fatalf("native protected node/plan or source was modified: %s", current)
	}
}

func TestNativeSemanticSourceOnlyPreModelRejectsBadIdentity(t *testing.T) {
	cases := []struct {
		name string
		edit func([]string) []string
	}{
		{"unknown_node", func(a []string) []string { return mutateNativeFlag(a, "-node", "N-016_other-node") }},
		{"short_node", func(a []string) []string { return mutateNativeFlag(a, "-node", "N-015") }},
		{"wrong_sha", func(a []string) []string { return mutateNativeFlag(a, "-artifact-sha256", strings.Repeat("0", 64)) }},
		{"stale_commit", func(a []string) []string { return mutateNativeFlag(a, "-source-commit", strings.Repeat("a", 40)) }},
		{"wrong_mode", func(a []string) []string { return mutateNativeFlag(a, "-scope", "factual") }},
		{"wrong_reviewer", func(a []string) []string { return mutateNativeFlag(a, "-reviewer", "claude-opus-5-5") }},
		{"wrong_executor", func(a []string) []string { return mutateNativeFlag(a, "-executor", "claude") }},
		{"foreign_artifact", func(a []string) []string {
			return mutateNativeFlag(a, "-artifact", filepath.Join(t.TempDir(), "foreign.txt"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root, commit, sha := makeNativeSemanticFixture(t)
			original := semanticCommand
			t.Cleanup(func() { semanticCommand = original })
			launched := false
			semanticCommand = func(_ string, _ ...string) *exec.Cmd {
				launched = true
				return exec.Command("not-expected")
			}
			output, rc := captureStdout(t, func() int {
				return cmdSemantic(tc.edit(nativeSemanticArgs(root, commit, sha)))
			})
			if rc != 2 || launched || !strings.Contains(output, "ОТКАЗ") &&
				!strings.Contains(output, "REJECTED_PRE_MODEL") {
				t.Fatalf("pre-model fail-closed failed: rc=%d launched=%v out=%s", rc, launched, output)
			}
			if _, err := os.Stat(filepath.Join(root, ".woody", "semantic-verdict.json")); !os.IsNotExist(err) {
				t.Fatalf("premodel failure wrote semantic verdict: %v", err)
			}
		})
	}
}

func TestNativeSemanticLegacyStepDoesNotResolveNativeNID(t *testing.T) {
	// No model/QA may run when the caller uses the historically wrong -step
	// parameter for the native N015 ID; the error must tell them exact syntax.
	root, _, _ := makeNativeSemanticFixture(t)
	text, rc := captureStdout(t, func() int {
		return cmdSemantic([]string{"-product", root, "-step", nodeFixtureID, "-executor", "codex"})
	})
	if rc != 2 || !strings.Contains(text, "Для штатного plan/N-ID используй -node") {
		t.Fatalf("native node was mistaken for legacy step: rc=%d stdout=%s", rc, text)
	}
}

func TestNativeSemanticModelInvokedButSourceNotRead(t *testing.T) {
	root, commit, sha := makeNativeSemanticFixture(t)
	original := semanticCommand
	t.Cleanup(func() { semanticCommand = original })
	semanticCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestNativeSemanticHelperProcess", "--", "NOT_PROVEN")
	}
	_, rc := captureStdout(t, func() int { return cmdSemantic(nativeSemanticArgs(root, commit, sha)) })
	if rc != 2 {
		t.Fatalf("model not reading source must not be accepted, got %d", rc)
	}
	var rec semanticRecord
	if err := readJSON(filepath.Join(root, ".woody", "semantic-verdict.json"), &rec); err != nil {
		t.Fatal(err)
	}
	if !rec.ModelInvoked || rec.ReviewStage != "MODEL_INVOKED_SOURCE_NOT_PROVEN" ||
		rec.FactualChecksExecuted == nil || *rec.FactualChecksExecuted ||
		rec.Verdict == nil || rec.Verdict.Verdict != "NOT_PROVEN" {
		t.Fatalf("machine receipt conflated model/tool failure and content review: %+v", rec)
	}
}

func mutateNativeFlag(a []string, flag, value string) []string {
	out := append([]string(nil), a...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == flag {
			out[i+1] = value
			return out
		}
	}
	panic(fmt.Sprintf("flag %s absent", flag))
}

func TestNativeSemanticHelperProcess(t *testing.T) {
	if !strings.Contains(strings.Join(os.Args, "|"), "-test.run=TestNativeSemanticHelperProcess") {
		return
	}
	mode := os.Args[len(os.Args)-1]
	stdin, err := io.ReadAll(os.Stdin)
	if err != nil || !strings.Contains(string(stdin), "\nPACKET:\n") ||
		!strings.Contains(string(stdin), nodeFixtureID) ||
		!strings.Contains(string(stdin), "func SourceProof() bool") ||
		!strings.Contains(string(stdin), "factual_checks_executed") {
		fmt.Fprintln(os.Stderr, "source-only packet missing verified native node/artifact bytes or QA boundary")
		os.Exit(5)
	}
	answer := `{"verdict":"PASS","step_done":"yes","drift":"none","evidence":["source.go:3 | func SourceProof() bool { return true }"],"next":"save source-only acceptance evidence"}`
	if mode == "NOT_PROVEN" {
		answer = `{"verdict":"NOT_PROVEN","step_done":"partial","drift":"none","evidence":["Source could not be read by reviewer"],"next":"retry only after source availability is resolved"}`
	}
	if mode == "FABRICATED" {
		answer = `{"verdict":"PASS","step_done":"yes","drift":"none","evidence":["source.go:3 seems okay"],"next":"accept without source text"}`
	}
	result := map[string]any{
		"type": "result", "is_error": false, "session_id": "native-node-review-helper",
		"result": answer, "num_turns": 1, "total_cost_usd": 0.0,
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestNativeSemanticRejectsUntrackedAndClosedNodePreModel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*testing.T, string) (artifact string, sha string)
	}{
		{
			name: "closed_native_node",
			change: func(t *testing.T, root string) (string, string) {
				t.Helper()
				path := filepath.Join(root, "plan", nodeFixtureID+".md")
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				modified := strings.Replace(string(raw), "status: \"open\"", "status: \"closed\"", 1)
				if err := os.WriteFile(path, []byte(modified), 0600); err != nil {
					t.Fatal(err)
				}
				return "", ""
			},
		},
		{
			name: "untracked_artifact",
			change: func(t *testing.T, root string) (string, string) {
				t.Helper()
				raw := []byte("untracked source evidence must never be trusted\n")
				if err := os.WriteFile(filepath.Join(root, "untracked.txt"), raw, 0600); err != nil {
					t.Fatal(err)
				}
				sum := sha256.Sum256(raw)
				return "untracked.txt", hex.EncodeToString(sum[:])
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, commit, hash := makeNativeSemanticFixture(t)
			name, changedHash := tc.change(t, root)
			argv := nativeSemanticArgs(root, commit, hash)
			if name != "" {
				argv = mutateNativeFlag(argv, "-artifact", name)
				argv = mutateNativeFlag(argv, "-artifact-sha256", changedHash)
			}
			old := semanticCommand
			t.Cleanup(func() { semanticCommand = old })
			started := false
			semanticCommand = func(_ string, _ ...string) *exec.Cmd {
				started = true
				return exec.Command(os.Args[0], "-test.run=TestNativeSemanticHelperProcess", "--", "PASS")
			}
			_, rc := captureStdout(t, func() int { return cmdSemantic(argv) })
			if rc != 2 || started {
				t.Fatalf("unsafe node/artifact reached reviewer rc=%d started=%t", rc, started)
			}
		})
	}
}

func TestNativeSemanticRejectsReleaseScopedReviewerOverrideBeforeModel(t *testing.T) {
	root, commit, sha := makeNativeSemanticFixture(t)
	if err := os.MkdirAll(filepath.Join(root, "goal"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "goal", "goal.json"),
		[]byte(`{"release_version":"0.11.11"}`), 0600); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(root, "run-config.json")
	var cfg runConfig
	if err := readJSON(configPath, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg.SemanticJudge = semanticJudgePolicy{
		ReleaseVersion: "0.11.11", Kind: "codex", Model: "gpt-6-sol", Effort: "low",
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	old := semanticCommand
	t.Cleanup(func() { semanticCommand = old })
	started := false
	semanticCommand = func(_ string, _ ...string) *exec.Cmd {
		started = true
		return exec.Command("must-not-run")
	}
	out, rc := captureStdout(t, func() int { return cmdSemantic(nativeSemanticArgs(root, commit, sha)) })
	if rc != 2 || started || !strings.Contains(out, "reviewer policy mismatch") {
		t.Fatalf("silently changed reviewer route rc=%d started=%v out=%s", rc, started, out)
	}
}

func TestNativeSemanticRejectsFabricatedSourceCitation(t *testing.T) {
	root, commit, sha := makeNativeSemanticFixture(t)
	old := semanticCommand
	t.Cleanup(func() { semanticCommand = old })
	semanticCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestNativeSemanticHelperProcess", "--", "FABRICATED")
	}
	out, rc := captureStdout(t, func() int { return cmdSemantic(nativeSemanticArgs(root, commit, sha)) })
	if rc != 2 || !strings.Contains(out, "\"phase\":\"MODEL_INVOKED_SOURCE_NOT_PROVEN\"") {
		t.Fatalf("fabricated source.go:3 passed without exact source text: rc=%d out=%s", rc, out)
	}
	var result semanticRecord
	if err := readJSON(filepath.Join(root, ".woody", "semantic-verdict.json"), &result); err != nil {
		t.Fatal(err)
	}
	if result.Verdict == nil || result.Verdict.Verdict != "NOT_PROVEN" ||
		!result.ModelInvoked || result.FactualChecksExecuted == nil || *result.FactualChecksExecuted {
		t.Fatalf("fabricated reviewer evidence incorrectly accepted: %+v", result)
	}
}

func TestNativeSemanticReviewerFailedToStartCannotClaimModelInvoked(t *testing.T) {
	root, commit, sha := makeNativeSemanticFixture(t)
	old := semanticCommand
	t.Cleanup(func() { semanticCommand = old })
	semanticCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(filepath.Join(root, "no-such-reviewer-binary.exe"))
	}
	out, rc := captureStdout(t, func() int { return cmdSemantic(nativeSemanticArgs(root, commit, sha)) })
	if rc != 2 || !strings.Contains(out, "\"phase\":\"MODEL_ATTEMPTED_NOT_PROVEN\"") {
		t.Fatalf("failed reviewer process was reported as successful: rc=%d out=%s", rc, out)
	}
	var result semanticRecord
	if err := readJSON(filepath.Join(root, ".woody", "semantic-verdict.json"), &result); err != nil {
		t.Fatal(err)
	}
	if result.ModelInvoked || !result.ModelRequestAttempted ||
		result.ReviewStage != "MODEL_ATTEMPTED_NOT_PROVEN" ||
		result.FactualChecksExecuted == nil || *result.FactualChecksExecuted {
		t.Fatalf("failed model start confused with model invocation: %+v", result)
	}
}

func TestNativeSemanticEvidenceMustMatchExactArtifactLine(t *testing.T) {
	s := &semanticNodeSourceScope{
		ArtifactPath: "internal/source.go",
		ArtifactText: "package main\n// Human explanation\nfunc SourceProof() bool { return true }\n",
	}
	for _, tc := range []struct {
		name     string
		evidence string
		pass     bool
	}{
		{"real_line", "internal/source.go:3 | func SourceProof() bool { return true }", true},
		{"fake_claim", "internal/source.go:3 source was reviewed", false},
		{"wrong_path", "other/source.go:3 | func SourceProof() bool { return true }", false},
		{"wrong_line", "internal/source.go:2 | func SourceProof() bool { return true }", false},
		{"line_suffix", "internal/source.go:31 | func SourceProof() bool { return true }", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := semanticSourceCited(semanticVerdict{Evidence: []string{tc.evidence}}, s)
			if got != tc.pass {
				t.Fatalf("source evidence grounding mismatch got=%v want=%v", got, tc.pass)
			}
		})
	}
}

func TestNativeSemanticReceiptReadbackRejectsMissingAndStalePass(t *testing.T) {
	root := t.TempDir()
	scope := &semanticNodeSourceScope{
		Node:           planNode{ID: nodeFixtureID},
		ReviewID:       "fresh-current-id",
		SourceCommit:   strings.Repeat("a", 40),
		ArtifactPath:   "source.go",
		ArtifactSHA256: strings.Repeat("b", 64),
		ArtifactText:   "package main\n// Source test\nfunc SourceProof() bool { return true }\n",
	}
	request := semanticNodeSourceRequest{ExpectedReviewer: "claude-sonnet-5-5", ExpectedEffort: "low"}
	passed := semanticVerdict{
		Verdict: "PASS", StepDone: "yes", Drift: "none",
		Evidence: []string{"source.go:3 | func SourceProof() bool { return true }"},
		Next:     "source review complete",
	}
	if err := verifySemanticNodeReceipt(root, scope, request, passed); err == nil {
		t.Fatal("missing native semantic receipt incorrectly accepted")
	}
	dir := filepath.Join(root, ".woody")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	factualRan := false
	stale := semanticRecord{
		ReviewID: "stale-previous-id", NodeID: scope.Node.ID,
		SourceCommit: scope.SourceCommit, ArtifactSHA256: scope.ArtifactSHA256,
		ReviewMode: "source-only", ModelInvoked: true,
		ModelRequestAttempted: true, FactualChecksExecuted: &factualRan,
		ReviewerVendor: "claude", ReviewerModel: "claude-sonnet-5-5",
		ReviewerEffort: "low", ReviewStage: "SUBSTANTIVE_SOURCE_REVIEW",
		Verdict: &passed,
	}
	if err := writeStateJSON(filepath.Join(dir, "semantic-verdict.json"), stale); err != nil {
		t.Fatal(err)
	}
	if err := verifySemanticNodeReceipt(root, scope, request, passed); err == nil {
		t.Fatal("stale source PASS incorrectly accepted")
	}
	stale.ReviewID = scope.ReviewID
	if err := writeStateJSON(filepath.Join(dir, "semantic-verdict.json"), stale); err != nil {
		t.Fatal(err)
	}
	if err := verifySemanticNodeReceipt(root, scope, request, passed); err != nil {
		t.Fatalf("fresh exact grounded native receipt rejected: %v", err)
	}
	stale.FactualChecksExecuted = nil
	if err := writeStateJSON(filepath.Join(dir, "semantic-verdict.json"), stale); err != nil {
		t.Fatal(err)
	}
	if err := verifySemanticNodeReceipt(root, scope, request, passed); err == nil {
		t.Fatal("missing factual-check execution flag incorrectly accepted")
	}
}
