package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type openAIResponsesRequest struct {
	Model     string `json:"model"`
	Input     string `json:"input"`
	Store     bool   `json:"store"`
	Reasoning *struct {
		Effort string `json:"effort"`
	} `json:"reasoning,omitempty"`
}

type openAIResponsesResponse struct {
	ID    string `json:"id"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Output []struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
}

func openAIResponsesURL(cfg openAIConfig) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	return base + "/responses"
}

func callOpenAIResponses(ctx context.Context, cfg openAIConfig, runner runnerSpec, prompt string) (openAIResponsesResponse, error) {
	keyEnv := cfg.APIKeyEnv
	if keyEnv == "" {
		keyEnv = "OPENAI_API_KEY"
	}
	key := os.Getenv(keyEnv)
	if key == "" {
		return openAIResponsesResponse{}, fmt.Errorf("OpenAI auth: %s is not set", keyEnv)
	}
	req := openAIResponsesRequest{Model: runner.Model, Input: prompt}
	if runner.Effort != "" {
		req.Reasoning = &struct {
			Effort string `json:"effort"`
		}{runner.Effort}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return openAIResponsesResponse{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIResponsesURL(cfg), bytes.NewReader(body))
	if err != nil {
		return openAIResponsesResponse{}, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+key)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return openAIResponsesResponse{}, err
	}
	defer resp.Body.Close()
	var out openAIResponsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := fmt.Sprintf("OpenAI HTTP %d", resp.StatusCode)
		if out.Error != nil {
			msg += ": " + out.Error.Message
		}
		return out, fmt.Errorf("%s", msg)
	}
	return out, nil
}

func (c *loopCtx) invokeOpenAI(prompt string, runner runnerSpec, stepID string) stepResult {
	operation := runnerReceiptOperation("executor-openai", runner, c.iter)
	receiptPath, outputPath := jobReceiptPaths(c.scope(), stepID, operation)
	if err := os.MkdirAll(filepath.Dir(receiptPath), 0o755); err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	receipt, lock, err := startJobReceipt(c.scope(), stepID, operation, receiptPath, outputPath)
	if err != nil {
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	defer lock.release()
	receipt.Runner, receipt.Provider, receipt.Model, receipt.Effort = "openai", "openai", runner.Model, runner.Effort
	receipt.RequestStarted = true
	if err := writeJobReceipt(receiptPath, receipt); err != nil {
		_ = finishJobReceipt(receiptPath, jobStatusFailed)
		return stepResult{Subtype: "runner_error", Detail: err.Error()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	out, requestErr := callOpenAIResponses(ctx, c.Cfg.OpenAI, runner, prompt)
	data, marshalErr := json.Marshal(out)
	if marshalErr == nil {
		marshalErr = os.WriteFile(outputPath, data, 0o644)
	}
	status := jobStatusDone
	if requestErr != nil || marshalErr != nil {
		status = jobStatusFailed
	}
	finishErr := finishJobReceipt(receiptPath, status)
	if marshalErr != nil {
		return stepResult{Subtype: "runner_error", Detail: marshalErr.Error()}
	}
	if finishErr != nil {
		return stepResult{Subtype: "runner_error", Detail: finishErr.Error()}
	}
	if requestErr != nil {
		err = requestErr
		sub := "runner_error"
		if isVendorLimit(err.Error()) {
			sub = "vendor_limit"
		}
		return stepResult{Subtype: sub, Detail: err.Error()}
	}
	var detail []string
	for _, item := range out.Output {
		for _, content := range item.Content {
			if strings.TrimSpace(content.Text) != "" {
				detail = append(detail, content.Text)
			}
		}
	}
	turns := 1
	return stepResult{Ok: true, Session: out.ID, Turns: &turns, Subtype: "success", Detail: strings.Join(detail, "\n")}
}
