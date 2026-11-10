package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

func TestN130SourceReviewPassRejectsPathAliasAndNonCodeCitationInflation(t *testing.T) {
	root, commit, taskSHA, refs := sourceReviewFixture(t)
	aliasA, aliasB, aliasC := refs[0], refs[0], refs[0]
	aliasA.Path = "cmd/executor.go"
	aliasB.Path = "./cmd/executor.go"
	aliasC.Path = "cmd//executor.go"
	v := sourceReviewVerdict{
		Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: taskSHA,
		TaskRead: "TASK_READ_OK", Checked: []sourceReviewLocation{aliasA, aliasB, aliasC},
		Next: "do not accept path aliases as multiple read files",
	}
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, taskSHA); err == nil {
		t.Fatal("one Go source file counted as three distinct cited code files")
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "README.md"), []byte("documentation alone cannot prove code inspection\n"), 0600); err != nil {
		t.Fatal(err)
	}
	v.Checked = []sourceReviewLocation{refs[0], refs[1], {
		Path: "docs/README.md", Line: 1, Quote: "documentation alone cannot prove code inspection",
	}}
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, taskSHA); err == nil {
		t.Fatal("documentation was counted as a third distinct source-code file")
	}
	v.Checked = refs
	if got, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, taskSHA); err != nil ||
		got.Verdict != "PASS_SOURCE_ONLY" {
		t.Fatalf("three true Go source citations should still pass: %+v %v", got, err)
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

func TestN133Legal19HR14UnreadTaskNeverPassesNativeSourceReview(t *testing.T) {
	root, commit, taskSHA, refs := sourceReviewFixture(t)
	verified := sourceReviewVerdict{
		Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: taskSHA,
		TaskRead: "TASK_READ_OK", Checked: refs, Next: "accepted source evidence",
	}

	// Both incidents had a successful outer receipt although the executor
	// explicitly could not read the task. Process exit 0 is not review PASS.
	for _, tc := range []struct{ name, answer string }{
		{"Legal19", "I couldn't read TASK.md because access was denied"},
		{"HR14", "I couldn't open the review task; I cannot inspect the files"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream, err := json.Marshal(map[string]any{
				"type": "result", "result": tc.answer, "is_error": false,
				"session_id": "outer-success-only",
			})
			if err != nil {
				t.Fatal(err)
			}
			reply, _, _, _, err := parseClaudeSemanticResult(string(stream)+"\n", nil)
			if err != nil {
				t.Fatalf("fixture must reproduce successful outer process: %v", err)
			}
			if _, err := parseSourceReviewVerdict(reply, root, commit, taskSHA); err == nil {
				t.Fatal("unread task was promoted from outer success to source-review PASS")
			}
		})
	}

	unread := verified
	unread.TaskRead = "TASK_READ_FAIL"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, unread), root, commit, taskSHA); err == nil {
		t.Fatal("structured PASS with explicit failed task read was accepted")
	}
	missingEvidence := verified
	missingEvidence.Checked = nil
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, missingEvidence), root, commit, taskSHA); err == nil {
		t.Fatal("TASK_READ_OK without real code citations was accepted")
	}
	if got, err := parseSourceReviewVerdict(sourceReviewJSON(t, verified), root, commit, taskSHA); err != nil || got.Verdict != "PASS_SOURCE_ONLY" {
		t.Fatalf("genuine exact-task, source-cited read-only review was rejected: %+v %v", got, err)
	}
}

func TestSourceReviewPromptRequiresThreeExactOneBasedCitations(t *testing.T) {
	prompt := sourceReviewPrompt("Read docs/tasks/TASK.md", strings.Repeat("a", 40), strings.Repeat("b", 64))
	for _, required := range []string{
		"EXACTLY THREE checked citations",
		"one per DISTINCT",
		"ACTUAL 1-based file line number",
		"a single mismatched citation makes the entire PASS",
		"Read, Glob and Grep only, never Bash",
		"actually reading the named TASK file from disk",
	} {
		if !strings.Contains(prompt, required) {
			t.Fatalf("source reviewer instruction is missing machine precision rule %q", required)
		}
	}
}

func TestSourceReviewRejectsOneBadCitationEvenWithThreeValid(t *testing.T) {
	root, commit, taskSHA, refs := sourceReviewFixture(t)
	extra := append(append([]sourceReviewLocation(nil), refs...), sourceReviewLocation{
		Path: refs[0].Path, Line: 2, Quote: refs[0].Quote,
	})
	v := sourceReviewVerdict{
		Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: taskSHA,
		TaskRead: "TASK_READ_OK", Checked: extra, Next: "source-only acceptance",
	}
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, taskSHA); err == nil {
		t.Fatal("one mismatched extra citation must invalidate PASS rather than being ignored")
	}
}

func TestSourceReviewClaudeCommandUsesStrictSchemaAndReadOnlyTools(t *testing.T) {
	cmd := sourceReviewClaudeCommand("claude", t.TempDir(), "review task", runnerSpec{Model: "sonnet"})
	args := cmd.Args
	value := func(flag string) string {
		t.Helper()
		for i := 0; i+1 < len(args); i++ {
			if args[i] == flag {
				return args[i+1]
			}
		}
		t.Fatalf("missing %s in %v", flag, args)
		return ""
	}
	if !containsArg(args, "-p") || value("--output-format") != "stream-json" || value("--allowed-tools") != "Read,Glob,Grep" || value("--tools") != "Read,Glob,Grep" || !containsArg(args, "--strict-mcp-config") || value("--mcp-config") != `{ "mcpServers": {} }` || value("--permission-mode") != "dontAsk" || value("--permission-prompts") != "none" || !containsArg(args, "--no-chrome") {
		t.Fatalf("unexpected source review command: %v", args)
	}
	raw := value("--json-schema")
	var schema map[string]any
	if err := json.Unmarshal([]byte(raw), &schema); err != nil {
		t.Fatal(err)
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(raw)); err != nil || compact.String() != raw {
		t.Fatalf("schema must be compact JSON: %s", raw)
	}
	checkSourceReviewSchema(t, schema, map[string]bool{
		"verdict": true, "source_commit": true, "task_sha256": true, "task_read": true,
		"checked": true, "findings": true, "next": true,
	}, nil)
	props := schema["properties"].(map[string]any)
	for _, tc := range []struct {
		name               string
		required, optional map[string]bool
	}{
		{"checked", map[string]bool{"path": true, "line": true, "quote": true}, nil},
		{"findings", map[string]bool{"severity": true, "path": true, "line": true, "quote": true, "detail": true}, map[string]bool{"next": true}},
	} {
		array, ok := props[tc.name].(map[string]any)
		if !ok || array["type"] != "array" {
			t.Fatalf("%s is not an array schema", tc.name)
		}
		items, ok := array["items"].(map[string]any)
		if !ok {
			t.Fatalf("%s has no item schema", tc.name)
		}
		checkSourceReviewSchema(t, items, tc.required, tc.optional)
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func checkSourceReviewSchema(t *testing.T, schema map[string]any, required, optional map[string]bool) {
	t.Helper()
	for key := range schema {
		if key != "type" && key != "properties" && key != "required" && key != "additionalProperties" {
			t.Fatalf("unsupported object schema key %q", key)
		}
	}
	if schema["type"] != "object" || schema["additionalProperties"] != false {
		t.Fatalf("not a closed object schema: %v", schema)
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok || len(props) != len(required)+len(optional) {
		t.Fatalf("unexpected properties: %v", schema["properties"])
	}
	for key := range props {
		if !required[key] && !optional[key] {
			t.Fatalf("unexpected property %q", key)
		}
	}
	list, ok := schema["required"].([]any)
	if !ok || len(list) != len(required) {
		t.Fatalf("unexpected required fields: %v", schema["required"])
	}
	seen := map[string]bool{}
	for _, item := range list {
		name, ok := item.(string)
		if !ok || !required[name] || seen[name] {
			t.Fatalf("unexpected required field %v", item)
		}
		seen[name] = true
	}
}

func TestSourceReviewStructuredEnvelopeFailsClosed(t *testing.T) {
	root, commit, task, refs := sourceReviewFixture(t)
	valid := sourceReviewVerdict{Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: task,
		TaskRead: "TASK_READ_OK", Checked: refs, Findings: []sourceReviewFinding{}, Next: "review complete"}
	payload := sourceReviewJSON(t, valid)
	envelope := func(fields map[string]any) string {
		t.Helper()
		fields["type"] = "result"
		raw, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	for _, raw := range []string{
		envelope(map[string]any{"structured_output": json.RawMessage(payload), "is_error": false}),
		envelope(map[string]any{"result": payload, "is_error": false}),
	} {
		reply, _, err := parseClaudeStructuredSourceResult(raw, nil)
		if err != nil {
			t.Fatalf("valid envelope: %v", err)
		}
		if got, err := parseSourceReviewVerdict(reply, root, commit, task); err != nil || got.Verdict != "PASS_SOURCE_ONLY" {
			t.Fatalf("valid structured verdict rejected: %+v %v", got, err)
		}
	}
	for _, tc := range []struct {
		name, raw string
		runErr    error
	}{
		{"prose prefix", envelope(map[string]any{"result": "Here is the review: " + payload}), nil},
		{"prose suffix", envelope(map[string]any{"result": payload + " done"}), nil},
		{"malformed envelope", `{`, nil},
		{"missing payload", envelope(map[string]any{"is_error": false}), nil},
		{"malformed structured object", envelope(map[string]any{"structured_output": map[string]any{"verdict": "PASS_SOURCE_ONLY"}}), nil},
		{"error result", envelope(map[string]any{"structured_output": json.RawMessage(payload), "is_error": true}), nil},
		{"nonzero process", envelope(map[string]any{"structured_output": json.RawMessage(payload)}), errors.New("exit status 1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reply, _, err := parseClaudeStructuredSourceResult(tc.raw, tc.runErr)
			if err == nil {
				if _, err = parseSourceReviewVerdict(reply, root, commit, task); err == nil {
					t.Fatal("invalid result accepted as PASS")
				}
			}
		})
	}
	for _, mutate := range []func(*sourceReviewVerdict){
		func(v *sourceReviewVerdict) { v.SourceCommit = strings.Repeat("c", 40) },
		func(v *sourceReviewVerdict) { v.TaskSHA256 = strings.Repeat("d", 64) },
		func(v *sourceReviewVerdict) { v.TaskRead = "TASK_READ_FAIL" },
		func(v *sourceReviewVerdict) { v.Checked = v.Checked[:2] },
	} {
		v := valid
		mutate(&v)
		raw := envelope(map[string]any{"structured_output": json.RawMessage(sourceReviewJSON(t, v))})
		reply, _, err := parseClaudeStructuredSourceResult(raw, nil)
		if err == nil {
			_, err = parseSourceReviewVerdict(reply, root, commit, task)
		}
		if err == nil {
			t.Fatal("invalid source evidence accepted")
		}
	}
}

func TestSourceReviewProductPrefixCitations(t *testing.T) {
	fixture, commit, task, refs := sourceReviewFixture(t)
	root := filepath.Join(t.TempDir(), "air-worker")
	for _, ref := range refs {
		path := filepath.Join(root, filepath.FromSlash(ref.Path))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(ref.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	valid := sourceReviewVerdict{Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: task,
		TaskRead: "TASK_READ_OK", Checked: refs, Next: "review complete"}
	for _, mixed := range []bool{false, true} {
		v := valid
		v.Checked = append([]sourceReviewLocation(nil), refs...)
		for i := range v.Checked {
			if !mixed || i != 1 {
				v.Checked[i].Path = "air-worker/" + v.Checked[i].Path
			}
		}
		got, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task)
		if err != nil {
			t.Fatalf("mixed=%v: %v", mixed, err)
		}
		for i := range refs {
			if got.Checked[i].Path != refs[i].Path {
				t.Fatalf("citation not normalized: %+v", got.Checked[i])
			}
		}
	}
	for _, path := range []string{
		"foreign/cmd/executor.go", "air-worker/air-worker/cmd/executor.go",
		filepath.Join(root, "cmd", "executor.go"), "../cmd/executor.go", "air-worker/../cmd/executor.go",
	} {
		v := valid
		v.Checked = append([]sourceReviewLocation(nil), refs...)
		v.Checked[0].Path = path
		if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
			t.Errorf("accepted invalid path %q", path)
		}
	}
	for _, mutate := range []func(*sourceReviewVerdict){
		func(v *sourceReviewVerdict) { v.Checked[0].Quote = "invented quote" },
		func(v *sourceReviewVerdict) { v.Checked[0].Line = 2 },
		func(v *sourceReviewVerdict) { v.SourceCommit = strings.Repeat("c", 40) },
		func(v *sourceReviewVerdict) { v.TaskSHA256 = strings.Repeat("d", 64) },
		func(v *sourceReviewVerdict) {
			v.Checked[1], v.Checked[2] = v.Checked[0], v.Checked[0]
			v.Checked[1].Path = refs[0].Path
			v.Checked[2].Path = "./" + refs[0].Path
		},
	} {
		v := valid
		v.Checked = append([]sourceReviewLocation(nil), refs...)
		v.Checked[0].Path = "air-worker/" + v.Checked[0].Path
		mutate(&v)
		if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
			t.Errorf("accepted invalid evidence: %+v", v)
		}
	}
	payload := sourceReviewJSON(t, valid)
	for _, raw := range []string{"prose " + payload, payload + " prose", payload + " {}"} {
		if _, err := parseSourceReviewVerdict(raw, root, commit, task); err == nil {
			t.Error("accepted extra prose or JSON")
		}
	}
	v := valid
	v.Verdict = "FAIL_P1"
	v.Findings = []sourceReviewFinding{{Severity: "P1", Path: "air-worker/" + refs[0].Path, Line: refs[0].Line, Quote: refs[0].Quote, Detail: "verified finding"}}
	got, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task)
	if err != nil || got.Findings[0].Path != refs[0].Path {
		t.Fatalf("finding normalization: %+v %v", got, err)
	}
	v.Verdict = "PASS_SOURCE_ONLY"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Error("P1 promoted to PASS")
	}
	v.Verdict, v.Findings[0].Severity = "FAIL_P1", "P0"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Error("P0 downgraded to P1")
	}
	v.Verdict, v.Findings[0].Severity, v.Findings[0].Quote = "FAIL_P0", "P0", "invented quote"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Error("accepted forged P0 citation")
	}

	outside := filepath.Join(t.TempDir(), "outside.go")
	if err := os.WriteFile(outside, []byte(refs[0].Quote+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "cmd", "escape.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Logf("symlink unavailable: %v", err)
		return
	}
	v = valid
	v.Checked = append([]sourceReviewLocation(nil), refs...)
	v.Checked[0].Path = "air-worker/cmd/escape.go"
	if _, err := parseSourceReviewVerdict(sourceReviewJSON(t, v), root, commit, task); err == nil {
		t.Error("accepted symlink escape")
	}
}

func TestSourceReviewRestrictionsRefuseMissingAndChangedFlags(t *testing.T) {
	cmd := sourceReviewClaudeCommand("claude", t.TempDir(), "task", runnerSpec{})
	if _, err := sourceReviewRestrictions(cmd.Args); err != nil {
		t.Fatal(err)
	}
	for i, arg := range cmd.Args {
		if arg != "--tools" && arg != "--strict-mcp-config" && arg != "--mcp-config" {
			continue
		}
		altered := append([]string(nil), cmd.Args...)
		altered[i] = "--unrelated"
		if _, err := sourceReviewRestrictions(altered); err == nil {
			t.Fatalf("accepted missing %s", arg)
		}
	}
}

func TestSourceReviewTraceRequiresSuccessfulReadsAndNoMCP(t *testing.T) {
	root, commit, _, refs := sourceReviewFixture(t)
	task := []byte("review this task\n")
	taskPath := filepath.Join(root, "task.md")
	if err := os.WriteFile(taskPath, task, 0600); err != nil {
		t.Fatal(err)
	}
	sha := sha256Hex(task)
	v := sourceReviewVerdict{Verdict: "PASS_SOURCE_ONLY", SourceCommit: commit, TaskSHA256: sha, TaskRead: "TASK_READ_OK", Checked: refs, Findings: []sourceReviewFinding{}, Next: "ok"}
	enc := func(x any) string {
		b, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	events := []string{enc(map[string]any{"type": "system", "subtype": "init", "session_id": "s", "tools": []string{"Read", "Glob", "Grep"}, "mcp_servers": []any{}})}
	paths := []string{taskPath}
	for _, r := range refs {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(r.Path)))
	}
	for i, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("r%d", i)
		events = append(events, enc(map[string]any{"type": "assistant", "message": map[string]any{"content": []any{map[string]any{"type": "tool_use", "id": id, "name": "Read", "input": map[string]any{"file_path": p}}}}}))
		events = append(events, enc(map[string]any{"type": "user", "message": map[string]any{"content": []any{map[string]any{"type": "tool_result", "tool_use_id": id}}}, "tool_use_result": map[string]any{"file": map[string]any{"filePath": p, "content": string(data), "startLine": 1}}}))
	}
	events = append(events, enc(map[string]any{"type": "result", "session_id": "s", "structured_output": v}))
	if _, _, err := parseAndVerifySourceReviewStream(strings.Join(events, "\n"), nil, root, sha); err != nil {
		t.Fatal(err)
	}
	missing := append([]string(nil), events...)
	missing[2] = "{}"
	if _, _, err := parseAndVerifySourceReviewStream(strings.Join(missing, "\n"), nil, root, sha); err == nil {
		t.Fatal("accepted task without successful read")
	}
	missing = append([]string(nil), events...)
	missing[4] = "{}"
	if _, _, err := parseAndVerifySourceReviewStream(strings.Join(missing, "\n"), nil, root, sha); err == nil {
		t.Fatal("accepted citation without successful read")
	}
	foreign := append([]string(nil), events...)
	foreign[0] = enc(map[string]any{"type": "system", "subtype": "init", "session_id": "s", "tools": []string{"Read"}, "mcp_servers": []any{map[string]any{"name": "remote"}}})
	if _, _, err := parseAndVerifySourceReviewStream(strings.Join(foreign, "\n"), nil, root, sha); err == nil {
		t.Fatal("accepted active MCP server")
	}
}
