package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSharedProductRoutingMatchesFlagValues(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
		fail bool
	}{
		{"duplicate", []string{"event", "-product", "legacy", "--product=shared"}, "", true},
		{"value-is-flag", []string{"event", "-observed", "-product", "-product", "shared"}, "shared", false},
		{"terminator", []string{"event", "--", "-product", "not-a-flag"}, "", false},
		{"json-bool", []string{"event", "-json", "-product=shared"}, "shared", false},
		{"legacy-bool", []string{"context", "-legacy", "--product", "shared"}, "shared", false},
		{"positional-stop", []string{"event", "positional", "-product", "not-parsed"}, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := learningProductArg(c.args)
			if got != c.want || (err != nil) != c.fail {
				t.Fatalf("%q %v", got, err)
			}
		})
	}
}

func TestSharedDuplicateProductCannotReachLegacyFeedback(t *testing.T) {
	legacy := t.TempDir()
	shared, s := sharedProductFixture(t, false)
	os.WriteFile(filepath.Join(shared, sharedLearningConfigFile), []byte(`{"schema":"broken"}`), 0600)
	handled, code := routeSharedFeedback([]string{"-product", legacy, "-product", shared, "-observed", "must not write"})
	if !handled || code == 0 {
		t.Fatal("ambiguous product routed to legacy")
	}
	for _, root := range []string{legacy, shared, s.RuntimeRoot} {
		if files, _ := filepath.Glob(filepath.Join(root, ".air-worker/feedback/FB-*.json")); len(files) > 0 {
			t.Fatal("legacy feedback mutated")
		}
	}
	if _, err := os.Stat(filepath.Join(s.RuntimeRoot, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("shared journal mutated")
	}
}

func TestSharedRejectedCutoverBlocksActualLifecycleHook(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	session := "rejected-cutover"
	seedActiveLearningSession(t, product, t.TempDir(), session)
	path := filepath.Join(product, ".air-worker/learn/RULES.md")
	os.MkdirAll(filepath.Dir(path), 0700)
	original := []byte("# Existing restriction\n")
	os.WriteFile(path, original, 0600)
	input, err := os.CreateTemp(t.TempDir(), "stdin-*.json")
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(hookInput{SessionID: session, Principal: "claude", HookEventName: "UserPromptSubmit", Prompt: "continue"})
	input.Write(payload)
	input.Seek(0, 0)
	previous := os.Stdin
	os.Stdin = input
	defer func() { os.Stdin = previous; input.Close() }()
	if code := cmdHook([]string{"UserPromptSubmit"}); code != 2 {
		t.Fatalf("lifecycle silently continued with lost guidance: %d", code)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(original) {
		t.Fatal("prior restriction modified")
	}
}

func TestSharedLegacyFeedbackMetadataPreserved(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	handled, code := routeSharedFeedback([]string{"-product", product, "-run-id", "old-feedback", "-type", "defect", "-severity", "P1", "-source-version", "0.11.5", "-observed", "wrapper failed", "-expected", "must reject overflow", "-reproduction", "send oversized JSON", "-workaround", "bound the writer", "-proposed-outcome", "never ignore output bounds", "-evidence", "fixture/receipt"})
	if !handled || code != 0 {
		t.Fatal("legacy feedback call failed")
	}
	body, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var event map[string]any
	for _, line := range strings.Split(string(body), "\n") {
		var row map[string]any
		if json.Unmarshal([]byte(line), &row) == nil && row["schema"] == "air.learning.event/v1" {
			event = row
			break
		}
	}
	if event["class"] != "feedback-error" {
		t.Fatal(event)
	}
	var metadata map[string]string
	if json.Unmarshal([]byte(event["feedback"].(string)), &metadata) != nil {
		t.Fatal("missing structured feedback")
	}
	for key, value := range map[string]string{"type": "defect", "severity": "P1", "source-version": "0.11.5", "expected": "must reject overflow", "reproduction": "send oversized JSON", "workaround": "bound the writer", "proposed-outcome": "never ignore output bounds"} {
		if metadata[key] != value {
			t.Errorf("lost %s: %q", key, metadata[key])
		}
	}
}

func TestSharedOversizeContextDoesNotRecordLoad(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	content := sharedFixtureProcedure + strings.Repeat("More detail.\n", 2000)
	if _, err := executeSharedLearning(product, s, "propose", map[string]string{"proposal_id": "LP-large", "kind": "procedure", "target": "skills/learned/large.md", "pre_sha256": "", "content": content}); err != nil {
		t.Fatal(err)
	}
	on, result, err := sharedLearningContext(product, hookInput{SessionID: "budget", RunID: "budget-run"})
	if !on || err != nil || strings.Contains(result.Context, "More detail.") {
		t.Fatal("oversize context included")
	}
	body, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "skill_loaded") {
		t.Fatal("omitted body received a load receipt")
	}
}
