package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestParseCodexResultTerminalOutcomeAndCause(t *testing.T) {
	tests := []struct {
		name, raw, subtype string
		runErr             error
		ok                 bool
	}{
		{"recovered error", `{"type":"error","message":"temporary failure"}` + "\n" + `{"type":"turn.completed"}`, "success", nil, true},
		{"earlier limit, permission failure", `{"type":"error","message":"HTTP 429 rate limit"}` + "\n" + `{"type":"turn.failed","error":{"message":"permission denied"}}`, "runner_error", nil, false},
		{"earlier error, limit failure", `{"type":"error","message":"temporary failure"}` + "\n" + `{"type":"turn.failed","error":{"message":"HTTP 429 rate limit"}}`, "vendor_limit", nil, false},
		{"completed, process failure", `{"type":"turn.completed"}`, "runner_error", errors.New("exit status 1"), false},
		{"duplicate completion", `{"type":"turn.completed"}` + "\n" + `{"type":"turn.completed"}`, "runner_error", nil, false},
		{"duplicate failure", `{"type":"turn.failed","error":{"message":"permission denied"}}` + "\n" + `{"type":"turn.failed","error":{"message":"permission denied"}}`, "runner_error", nil, false},
		{"completion then failure", `{"type":"turn.completed"}` + "\n" + `{"type":"turn.failed","error":{"message":"permission denied"}}`, "runner_error", nil, false},
		{"failure then completion", `{"type":"turn.failed","error":{"message":"permission denied"}}` + "\n" + `{"type":"turn.completed"}`, "runner_error", nil, false},
		{"agent prose is not a limit", `{"type":"item.completed","item":{"type":"agent_message","text":"HTTP 429 rate limit"}}` + "\n" + `{"type":"turn.failed","error":{"message":"permission denied"}}`, "runner_error", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parseCodexResult(tt.raw, tt.runErr)
			if result.Ok != tt.ok || result.Subtype != tt.subtype {
				t.Fatalf("result = %+v, want Ok=%v Subtype=%q", result, tt.ok, tt.subtype)
			}
		})
	}

	result := parseCodexResult(`{"type":"error","message":"HTTP 429 rate limit"}`, nil)
	if result.Ok || result.Subtype == "success" || result.Subtype == "vendor_limit" {
		t.Fatalf("unterminated error must not succeed or escalate: %+v", result)
	}
}

func TestVendorLimitClassification(t *testing.T) {
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

func TestVendorLimitWordingAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		message string
		want    bool
	}{
		{"You’ve hit your usage limit. Try again later.", true},
		{"You've hit your usage limit. Try again later.", true},
		{"HTTP 429", true},
		{"status 429", true},
		{`request failed: {"status":429,"message":"retry"}`, true},
		{"HTTP 4297", false},
		{"status 429.1", false},
		{"local record 429", false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			if got := isVendorLimit(tc.message); got != tc.want {
				t.Errorf("isVendorLimit(%q) = %v, want %v", tc.message, got, tc.want)
			}
		})
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
