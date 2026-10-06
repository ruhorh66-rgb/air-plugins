package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLearningReviewerAdapterProducesManagedProcedure(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	original := learningReviewerInvoke
	defer func() { learningReviewerInvoke = original }()
	learningReviewerInvoke = func(root, prompt, model, effort string) (string, error) {
		if root != product {
			t.Fatalf("reviewer root=%q want %q", root, product)
		}
		if !strings.Contains(prompt, "skills/learned/") {
			t.Fatalf("managed target missing from prompt: %s", prompt)
		}
		return `{"apply":true,"content":"# Verify exact package identity\\n\\n## When to apply\\nWhen accepting a release candidate.\\n\\n## Procedure\\n1. Compare the receipt package head with the exact Git HEAD before PASS.\\n\\n## Pitfalls\\nDo not treat an old receipt as evidence for a newer package.\\n"}`, nil
	}

	inPath := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(inPath, []byte(`{"event":{"class":"receipt-head-mismatch","kind":"run_completed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	oldIn := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = oldIn }()

	code, out := captureLoopOutput(t, func() int {
		return cmdLearningReviewerAdapter([]string{"-product", product, "-model", "gpt-6-sol", "-effort", "medium"})
	})
	if code != 0 {
		t.Fatalf("adapter exit=%d output=%s", code, out)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("invalid adapter JSON: %v %s", err, out)
	}
	if got["kind"] != "procedure" || !strings.HasPrefix(got["target"], "skills/learned/") || got["pre_sha256"] != "" {
		t.Fatalf("unexpected candidate: %#v", got)
	}
	if !validLearningProcedureMarkdown(got["content"]) {
		t.Fatalf("invalid procedure content: %q", got["content"])
	}
}

func TestLearningReviewerAdapterNoCandidate(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	original := learningReviewerInvoke
	defer func() { learningReviewerInvoke = original }()
	learningReviewerInvoke = func(root, prompt, model, effort string) (string, error) {
		return `{"apply":false,"content":""}`, nil
	}
	inPath := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(inPath, []byte(`{"event":{"class":"one-off","kind":"run_completed"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := os.Open(inPath)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	oldIn := os.Stdin
	os.Stdin = in
	defer func() { os.Stdin = oldIn }()

	code, out := captureLoopOutput(t, func() int {
		return cmdLearningReviewerAdapter([]string{"-product", product})
	})
	if code != 0 || strings.TrimSpace(out) != "{}" {
		t.Fatalf("no-candidate adapter result: code=%d out=%q", code, out)
	}
}
