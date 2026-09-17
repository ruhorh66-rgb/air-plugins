package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenAILadderFallback(t *testing.T) {
	var cfg runConfig
	if err := readJSON(filepath.Join("..", "run-config.json"), &cfg); err != nil {
		t.Fatal(err)
	}
	want := []string{"luna:medium", "luna:max", "terra:medium", "terra:ultra", "sol:medium", "sol:ultra"}
	start := -1
	for i, tier := range cfg.Ladder {
		if tier == want[0] {
			start = i
			break
		}
	}
	if start < 0 || start+len(want) > len(cfg.Ladder) || strings.Join(cfg.Ladder[start:start+len(want)], "|") != strings.Join(want, "|") {
		t.Fatalf("OpenAI ladder must be contiguous and exact: %v", cfg.Ladder)
	}
	models := map[string]string{"luna": "gpt-5.6-luna", "terra": "gpt-5.6-terra", "sol": "gpt-5.6-sol"}
	for name, model := range models {
		r := cfg.Runners[name]
		if r.Kind != "codex" || r.Model != model {
			t.Fatalf("%s runner=%+v", name, r)
		}
	}
	index := start
	for _, expected := range want[1:] {
		var ok bool
		index, _, ok = nextTierAfterVendorLimit(cfg.Ladder, index)
		if !ok || cfg.Ladder[index] != expected {
			t.Fatalf("fallback stopped at %q, want %q", cfg.Ladder[index], expected)
		}
	}
	if _, _, ok := nextTierAfterVendorLimit(cfg.Ladder, len(cfg.Ladder)-1); ok {
		t.Fatal("last rung must exhaust the ladder")
	}

	for _, msg := range []string{
		"HTTP 429 rate limit",
		"insufficient_quota",
		"You've hit your weekly limit · resets 10am",
		"usage limit reached",
	} {
		if !isVendorLimit(msg) {
			t.Fatalf("%q must advance", msg)
		}
	}
	for _, msg := range []string{"unauthorized", "permission denied", "network failure"} {
		if isVendorLimit(msg) {
			t.Fatalf("%q incorrectly classified", msg)
		}
	}
	if parseCodexResult(`{"type":"turn.failed","error":{"message":"HTTP 429 rate limit"}}`, nil).Subtype != "vendor_limit" {
		t.Fatal("Codex 429 must become vendor_limit")
	}
}

func TestOpenAIAPIRunner(t *testing.T) {
	t.Run("success receipt", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer test-key" {
				t.Fatalf("bad request: %s %s", r.URL, r.Header.Get("Authorization"))
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["model"] != "gpt-test" {
				t.Fatalf("bad body: %#v %v", body, err)
			}
			reasoning, _ := body["reasoning"].(map[string]any)
			if reasoning["effort"] != "medium" || body["store"] != false {
				t.Fatalf("reasoning/store=%#v/%#v", reasoning, body["store"])
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp-1","output":[{"content":[{"text":"DONE"}]}]}`))
		}))
		defer server.Close()

		t.Setenv("OPENAI_TEST_KEY", "test-key")
		root := t.TempDir()
		runner := runnerSpec{Kind: "openai", Model: "gpt-test", Effort: "medium"}
		c := &loopCtx{Root: root, iter: 3, Cfg: runConfig{OpenAI: openAIConfig{BaseURL: server.URL + "/v1", APIKeyEnv: "OPENAI_TEST_KEY"}}}
		result := c.invokeOpenAI("task", runner, "89")
		if !result.Ok || result.Session != "resp-1" || !strings.Contains(result.Detail, "DONE") {
			t.Fatalf("bad API result: %+v", result)
		}
		operation := runnerReceiptOperation("executor-openai", runner, c.iter)
		receiptPath, _ := jobReceiptPaths(c.scope(), "89", operation)
		receipt, err := readJobReceipt(receiptPath)
		if err != nil || receipt.Status != jobStatusDone || receipt.Provider != "openai" ||
			receipt.Model != "gpt-test" || receipt.Effort != "medium" || !receipt.RequestStarted || receipt.ProcessStarted {
			t.Fatalf("bad receipt: %+v %v", receipt, err)
		}
		data, err := os.ReadFile(receiptPath)
		if err != nil || strings.Contains(string(data), "test-key") {
			t.Fatalf("receipt leaked API key: %v", err)
		}
		if operation == runnerReceiptOperation("executor-openai", runner, c.iter+1) {
			t.Fatal("separate attempts must not overwrite one receipt")
		}
	})

	t.Run("quota advances", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"quota exceeded"}}`))
		}))
		defer server.Close()

		t.Setenv("OPENAI_TEST_KEY", "test-key")
		runner := runnerSpec{Kind: "openai", Model: "gpt-test", Effort: "max"}
		c := &loopCtx{Root: t.TempDir(), iter: 4, Cfg: runConfig{OpenAI: openAIConfig{BaseURL: server.URL + "/v1", APIKeyEnv: "OPENAI_TEST_KEY"}}}
		result := c.invokeOpenAI("task", runner, "89")
		if result.Ok || result.Subtype != "vendor_limit" {
			t.Fatalf("quota result=%+v", result)
		}
		receiptPath, _ := jobReceiptPaths(c.scope(), "89", runnerReceiptOperation("executor-openai", runner, c.iter))
		receipt, err := readJobReceipt(receiptPath)
		if err != nil || receipt.Status != jobStatusFailed || receipt.Model != "gpt-test" || receipt.Effort != "max" {
			t.Fatalf("failed receipt=%+v %v", receipt, err)
		}
	})
}
