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

func TestSharedConfigEntryPresenceFailsClosed(t *testing.T) {
	t.Run("directory", func(t *testing.T) {
		product, _ := sharedProductFixture(t, false)
		path := filepath.Join(product, sharedLearningConfigFile)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if _, on, err := readSharedLearningSettings(product); !on || err == nil {
			t.Fatalf("non-regular config must select shared mode and fail: on=%v err=%v", on, err)
		}
		if handled, code := routeSharedLearn([]string{"event", "-product", product, "-actor", "worker", "-observed", "must not hit legacy"}); !handled || code == 0 {
			t.Fatalf("non-regular config fell through: handled=%v code=%d", handled, code)
		}
		if _, err := os.Stat(filepath.Join(product, "learn", "events.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("legacy journal created: %v", err)
		}
	})

	t.Run("dangling-symlink", func(t *testing.T) {
		product, _ := sharedProductFixture(t, false)
		path := filepath.Join(product, sharedLearningConfigFile)
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(product, "missing-config.json"), path); err != nil {
			t.Skipf("symlink unavailable on this host: %v", err)
		}
		if _, on, err := readSharedLearningSettings(product); !on || err == nil {
			t.Fatalf("dangling config link must select shared mode and fail: on=%v err=%v", on, err)
		}
	})
}

func TestSharedStopPersistenceFailureIsExplicitAndRetryable(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	session := "shared-stop-retry"
	seedActiveLearningSession(t, product, t.TempDir(), session)
	if err := os.MkdirAll(s.RuntimeRoot, 0700); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(s.RuntimeRoot, "events.jsonl")
	if err := os.Mkdir(blocked, 0700); err != nil {
		t.Fatal(err)
	}

	payload, _ := json.Marshal(hookInput{
		SessionID: session, Principal: "claude", HookEventName: "Stop",
		RunID: "stable-shared-stop", LastAssistantMessage: "done",
	})
	run := func() (string, int) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(payload); err != nil {
			t.Fatal(err)
		}
		_ = w.Close()
		old := os.Stdin
		os.Stdin = r
		stderr, code := captureStderr(t, func() int { return cmdHook([]string{"Stop"}) })
		os.Stdin = old
		_ = r.Close()
		return stderr, code
	}

	stderr, code := run()
	if code != 2 || !strings.Contains(stderr, "stable-shared-stop") || !strings.Contains(stderr, "shared learning finalize failed") {
		t.Fatalf("shared Stop failure was swallowed: code=%d stderr=%q", code, stderr)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	stderr, code = run()
	if code != 0 {
		t.Fatalf("retry of same run_id failed: code=%d stderr=%q", code, stderr)
	}
	body, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "stable-shared-stop") != 2 {
		// one event + one verdict for the single successful retry
		t.Fatalf("unexpected retry journal multiplicity:\n%s", body)
	}
}

func TestSharedContextPreservesVerifiedBodyWhenLaterSkillFails(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	for _, item := range []struct {
		id, target, content string
	}{
		{"LP-a", "skills/learned/a.md", sharedFixtureProcedure + "\nA marker.\n"},
		{"LP-b", "skills/learned/b.md", sharedFixtureProcedure + "\nB marker.\n"},
	} {
		if _, err := executeSharedLearning(product, s, "propose", map[string]string{
			"proposal_id": item.id, "kind": "procedure", "target": item.target, "pre_sha256": "", "content": item.content,
		}); err != nil {
			t.Fatal(err)
		}
	}
	index, err := executeSharedLearning(product, s, "index", nil)
	if err != nil {
		t.Fatal(err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(index.Data, &catalog); err != nil || len(catalog.Skills) != 2 {
		t.Fatalf("catalog: %v %s", err, index.Data)
	}
	if err := os.WriteFile(filepath.Join(product, "skills", "learned", "b.md"), []byte(sharedFixtureProcedure+"\nTampered after index.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	res, err := sharedLearningContextFromCatalog(product, s, hookInput{SessionID: "ctx-partial", Principal: "gpt"}, "ctx-run", catalog.Skills)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Context, "A marker.") || strings.Contains(res.Context, "Tampered after index.") {
		t.Fatalf("verified prior context was lost or bad later context leaked:\n%s", res.Context)
	}
	if !strings.Contains(res.Context, "skill_delivery_failed") {
		t.Fatalf("partial delivery warning missing: %s", res.Context)
	}
	body, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "\"kind\":\"skill_loaded\"") || !strings.Contains(text, "a.md@") {
		t.Fatalf("verified load receipt missing:\n%s", text)
	}
	if !strings.Contains(text, "\"kind\":\"skill_delivery_failed\"") || !strings.Contains(text, "b.md") {
		t.Fatalf("failed later delivery receipt missing:\n%s", text)
	}
	if strings.Contains(text, "b.md@") {
		t.Fatalf("failed later skill was falsely recorded loaded:\n%s", text)
	}
}
