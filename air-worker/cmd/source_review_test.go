package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sourceReviewFixture(t *testing.T) (string, string, string, []sourceReviewLocation) {
	t.Helper()
	root := t.TempDir()
	names := []string{"cmd/executor.go", "cmd/semantic.go", "cmd/update.go"}
	var checked []sourceReviewLocation
	for _, n := range names {
		path := filepath.Join(root, filepath.FromSlash(n))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		line := "func checked_" + strings.TrimSuffix(filepath.Base(n), ".go") + "() bool { return true }"
		if err := os.WriteFile(path, []byte(line+"\n"), 0600); err != nil {
			t.Fatal(err)
		}
		checked = append(checked, sourceReviewLocation{Path: n, Line: 1, Quote: line})
	}
	return root, strings.Repeat("a", 40), strings.Repeat("b", 64), checked
}

func sourceReviewJSON(t *testing.T, v sourceReviewVerdict) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestNativeSourceReviewRequiresVerifiedDistinctEvidenceForPass(t *testing.T) {
	root, commit, task, refs := sourceReviewFixture(t)
	v := sourceReviewVerdict{
		Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: task,
		TaskRead: "TASK_READ_OK", Checked: refs, Next: "ready for independent release acceptance",
	}
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err != nil {
		t.Fatal(err)
	}
	v.Checked = refs[:1]
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Fatal("PASS without three separately verified files")
	}
	v.Checked = refs
	v.Checked[1].Quote = "made up content from unrelated file"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Fatal("PASS with invented source quotation")
	}
}

func TestNativeSourceReviewRejectsStaleOrUnreadableInputs(t *testing.T) {
	root, commit, task, refs := sourceReviewFixture(t)
	v := sourceReviewVerdict{
		Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: task,
		TaskRead: "TASK_READ_OK", Checked: refs, Next: "re-review after fixing",
	}
	for _, mutation := range []func(*sourceReviewVerdict){
		func(v *sourceReviewVerdict) { v.TaskRead = "TASK_READ_FAIL" },
		func(v *sourceReviewVerdict) { v.SourceCommit = strings.Repeat("c", 40) },
		func(v *sourceReviewVerdict) { v.TaskSHA256 = strings.Repeat("d", 64) },
		func(v *sourceReviewVerdict) { v.Verdict = "PASS" },
		func(v *sourceReviewVerdict) { v.Checked[0].Path = "../../other-product.txt" },
	} {
		c := v
		c.Checked = append([]sourceReviewLocation(nil), refs...)
		mutation(&c)
		if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, c), root, commit, task); err == nil {
			t.Fatalf("accepted invalid or stale source review: %+v", c)
		}
	}
}

func TestNativeSourceReviewPreservesCodeReviewFindingAsFailure(t *testing.T) {
	root, commit, task, refs := sourceReviewFixture(t)
	finding := sourceReviewFinding{Severity: "P1", Path: refs[0].Path, Line: 1, Quote: refs[0].Quote, Detail: "verified reference"}
	v := sourceReviewVerdict{
		Verdict: "FAIL_P1", SourceCommit: commit, TaskSHA256: task,
		TaskRead: "TASK_READ_OK", Checked: refs[:1],
		Findings: []sourceReviewFinding{finding}, Next: "fix P1",
	}
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err != nil {
		t.Fatal(err)
	}
	v.Verdict = "PASS_SOURCE_ONLY"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Fatal("a review with P1 was promoted to PASS")
	}
	v.Verdict = "FAIL_P1"
	v.Findings[0].Severity = "P0"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Fatal("P0 was downgraded to FAIL_P1")
	}
}

func TestNativeSourceReviewUsesIndependentAnthropicPolicy(t *testing.T) {
	cfg := runConfig{
		ModelPolicy: modelPolicySpec{Judges: map[string]modelJudgeLane{
			"default": {Model: "claude-sonnet-5-5", Efforts: []string{"low", "medium", "high"}},
			"complex": {Model: "claude-opus-5-5", Efforts: []string{"low", "medium", "high"}},
		}},
	}
	cfg.Orchestration.RequireAnthropicReviewer = true
	for _, class := range []string{"default", "complex"} {
		judge, err := judgeLaneRunner(cfg, class)
		if err != nil {
			t.Fatal(err)
		}
		if judge.Kind != "claude" || err != nil {
			t.Fatalf("source review selected non-Anthropic judge: %+v", judge)
		}
		if err := enforceIndependentAnthropicReviewer(cfg, runnerSpec{Kind: "codex"}, judge); err != nil {
			t.Fatalf("source review routing is not valid: %v", err)
		}
	}
	if got := cmdReview([]string{"source", "-executor", "claude", "-json"}); got != 2 {
		t.Fatalf("source review allowed Claude executor/same-vendor substitution: rc=%d", got)
	}
}

func TestNativeSourceReviewRejectsForeignOrFalseCitation(t *testing.T) {
	root, _, _, refs := sourceReviewFixture(t)
	if err := verifyReviewCitation(root, refs[0]); err != nil {
		t.Fatal(err)
	}
	for _, c := range []sourceReviewLocation{
		{Path: "cmd/missing.go", Line: 1, Quote: "some arbitrary words"},
		{Path: "../foreign.go", Line: 1, Quote: "some arbitrary words"},
		{Path: refs[0].Path, Line: 30, Quote: refs[0].Quote},
		{Path: refs[0].Path, Line: 1, Quote: "a line that is not present"},
	} {
		if err := verifyReviewCitation(root, c); err == nil {
			t.Fatalf("accepted invalid citation %+v", c)
		}
	}
}

func TestNativeSourceReviewRoutesNativeNNodeWithoutLegacyStep(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "plan"), 0700); err != nil {
		t.Fatal(err)
	}
	node := planNode{
		ID:        "N-130_rc4-native-security-review",
		Title:     "Native source review",
		Parent:    "N-129_parent",
		Trigger:   "LPR requires Anthropic independent code review",
		Owner:     "Dev Work 27",
		DoneWhen:  "Source SHA and code/security verdict checked",
		Status:    "open",
		ReturnTo:  "N-129_parent",
		CreatedAt: "2026-11-05T00:00:00Z",
		UpdatedAt: "2026-11-05T00:00:00Z",
	}
	path := filepath.Join(root, "plan", node.ID+".md")
	if err := os.WriteFile(path, renderPlanNode(node), 0600); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{node.ID, "N-130"} {
		got, err := sourceReviewNode(root, id)
		if err != nil || got.ID != node.ID {
			t.Fatalf("valid native node %q not found: %+v %v", id, got, err)
		}
	}
	node.Status = "closed"
	if err := os.WriteFile(path, renderPlanNode(node), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := sourceReviewNode(root, "N-130"); err == nil {
		t.Fatal("closed native N-node accepted for a new review")
	}
	if _, err := sourceReviewNode(root, "N-999"); err == nil {
		t.Fatal("invented native node accepted")
	}
}

func TestNativeSourceReviewAcceptsBoundedFindingFixWhileRejectingUnknownFields(t *testing.T) {
	root, commit, task, refs := sourceReviewFixture(t)
	v := sourceReviewVerdict{
		Verdict: "FAIL_P1", SourceCommit: commit, TaskSHA256: task,
		TaskRead: "TASK_READ_OK", Checked: refs[:1],
		Findings: []sourceReviewFinding{{
			Severity: "P1", Path: refs[0].Path, Line: refs[0].Line, Quote: refs[0].Quote,
			Detail: "verified regression", Next: "restore the own cache before another update",
		}},
		Next: "fix before release",
	}
	if got, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err != nil || got.Verdict != "FAIL_P1" {
		t.Fatalf("bounded Claude per-finding next was rejected or promoted: %+v %v", got, err)
	}
	v.Findings[0].Next = strings.Repeat("x", 1025)
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Fatal("oversized suggested fix accepted")
	}
	raw := strings.Replace(sourceReviewJSON(t, sourceReviewVerdict{
		Verdict: "NOT_PROVEN", SourceCommit: commit, TaskSHA256: task, TaskRead: "TASK_READ_OK",
		Checked: refs[:1], Next: "not enough evidence",
	}), `"line":1`, `"line":1,"next":"unexpected"`, 1)
	if _, err := parseSourceReviewVerdict(raw, root, commit, task); err == nil {
		t.Fatal("unknown field inside checked evidence accepted")
	}
}
