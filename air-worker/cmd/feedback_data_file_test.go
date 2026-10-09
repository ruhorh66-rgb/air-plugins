package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n105Fixture(t *testing.T) (string, sharedLearningSettings) {
	t.Helper()
	root, config := sharedProductFixture(t, false)
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte(`{"plan":"PLAN.md"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# N105 feedback input\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, config
}

func TestN105FeedbackDataFilePreservesExactNestedQuotesAndNewlines(t *testing.T) {
	root, config := n105Fixture(t)
	observed := "ACTUAL CLI FAILURE: air-worker learn self status -json\nexit code 2; message: \"unknown learn action \\\"self\\\"\".\nNo remediation is proven."
	request := map[string]string{
		"kind": "error", "run_id": "N105-complete-json-feedback-1",
		"source": "n105-gpt", "text": observed, "ref": "fixture/machine-receipt.json",
		"severity": "P0", "source-version": "0.11.8", "type": "defect",
		"expected": "persistent owner enabled", "workaround": "native feedback with JSON",
	}
	b, err := json.MarshalIndent(request, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "exact-feedback.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	code, result := captureLoopOutput(t, func() int {
		return cmdFeedback([]string{"add", "-product", root, "-data-file", path})
	})
	if code != 0 {
		t.Fatalf("feedback JSON was not accepted: rc=%d out=%s", code, result)
	}
	runID := request["run_id"]
	eventCount := 0
	var fromEvent map[string]any
	if err := scanLearnJSONL(filepath.Join(config.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var e map[string]any
		if err := json.Unmarshal(b, &e); err != nil {
			return err
		}
		if e["schema"] == "air.learning.event/v1" && e["run_id"] == runID {
			eventCount++
			fromEvent = e
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || fromEvent["observed"] != observed {
		t.Fatalf("complete quoted observation not persisted: n=%d observed=%q", eventCount, fromEvent["observed"])
	}
	id := "FB-" + learnSHA([]byte("air-worker.feedback/v1\n" + runID))[:24]
	receipt, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(feedbackEvidenceRel(id))))
	if err != nil {
		t.Fatal(err)
	}
	var rec feedbackRecord
	if err := json.Unmarshal(receipt, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Observed != observed || rec.InputSHA256 != learnSHA(b) || rec.Severity != "P0" {
		t.Fatalf("canonical receipt corrupted input: %+v", rec)
	}
}

func TestN105RejectsDuplicateUnknownInvalidAndOversizedJSONBeforeLearning(t *testing.T) {
	for name, content := range map[string]string{
		"duplicate":  `{"kind":"error","kind":"idea","run_id":"x","text":"real"}`,
		"unknown":    `{"kind":"error","run_id":"x","text":"real","bypass":true}`,
		"wrong-type": `{"kind":"error","run_id":42,"text":"real"}`,
		"trailing":   `{"kind":"error","run_id":"x","text":"real"} {"kind":"idea"}`,
		"oversize":   `{"kind":"error","run_id":"x","text":"` + strings.Repeat("X", 66*1024) + `"}`,
	} {
		t.Run(name, func(t *testing.T) {
			root, config := n105Fixture(t)
			file := filepath.Join(t.TempDir(), "bad-data.json")
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			rc, _ := captureLoopOutput(t, func() int {
				return cmdFeedback([]string{"add", "-product", root, "-data-file", file})
			})
			if rc == 0 {
				t.Fatalf("invalid request %s accepted", name)
			}
			if _, err := os.Stat(filepath.Join(config.RuntimeRoot, "events.jsonl")); !os.IsNotExist(err) {
				t.Fatalf("invalid request mutated module events: %v", err)
			}
		})
	}
}

func TestN105StrayArgumentOrConflictingInlineTextIsRejected(t *testing.T) {
	for name, tail := range map[string][]string{
		"quote-tail": {"-kind", "error", "-text", "truncated", "unexpected-more"},
		"mixed-file": {"-data-file", "fake-path", "-text", "conflicting"},
	} {
		t.Run(name, func(t *testing.T) {
			root, settings := n105Fixture(t)
			args := append([]string{"add", "-product", root}, tail...)
			code, _ := captureLoopOutput(t, func() int { return cmdFeedback(args) })
			if code == 0 {
				t.Fatalf("%s unexpectedly succeeded", name)
			}
			if _, err := os.Stat(filepath.Join(settings.RuntimeRoot, "events.jsonl")); !os.IsNotExist(err) {
				t.Fatalf("%s created a shared event despite failed validation", name)
			}
		})
	}
}
