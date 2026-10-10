package main

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestParseSemanticHermesOutputRequiresPinnedNoToolsResult(t *testing.T) {
	const wantModel = "anthropic/claude-sonnet-5.5"
	valid := []byte("{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"anthropic/claude-sonnet-5.5\",\"session_id\":\"s-1\"}\n" +
		"{\"type\":\"text\",\"text\":\"{\\\"verdict\\\":\\\"PASS\\\"}\"}\n" +
		"{\"type\":\"result\",\"exit_code\":0,\"text\":\"{\\\"verdict\\\":\\\"PASS\\\"}\",\"session_id\":\"s-1\"}\n")
	text, session, err := parseSemanticHermesOutput(valid, wantModel)
	if err != nil || text != `{"verdict":"PASS"}` || session != "s-1" {
		t.Fatalf("parsed text=%q session=%q err=%v", text, session, err)
	}
	for name, stream := range map[string][]byte{
		"wrong model":       []byte("{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"anthropic/claude-opus-5.5\",\"session_id\":\"s-1\"}\n{\"type\":\"result\",\"exit_code\":0,\"text\":\"x\",\"session_id\":\"s-1\"}\n"),
		"nonzero exit":      []byte("{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"anthropic/claude-sonnet-5.5\",\"session_id\":\"s-1\"}\n{\"type\":\"result\",\"exit_code\":1,\"text\":\"x\",\"session_id\":\"s-1\"}\n"),
		"missing exit code": []byte("{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"anthropic/claude-sonnet-5.5\",\"session_id\":\"s-1\"}\n{\"type\":\"result\",\"text\":\"x\",\"session_id\":\"s-1\"}\n"),
		"tool use":          []byte("{\"type\":\"system\",\"subtype\":\"init\",\"model\":\"anthropic/claude-sonnet-5.5\",\"session_id\":\"s-1\"}\n{\"type\":\"tool_use\",\"name\":\"shell\"}\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseSemanticHermesOutput(stream, wantModel); err == nil {
				t.Fatal("expected fail-closed parse error")
			}
		})
	}
}

func TestSemanticHermesCommandPinsNousAndMediumWithoutPromptArg(t *testing.T) {
	const prompt = "private semantic packet: do not put this in argv"
	reviewer := runnerSpec{Kind: "hermes", Provider: "nous", Model: "anthropic/claude-sonnet-5.5", Effort: "medium"}
	cmd := semanticHermesCommand(context.Background(), "hermes.exe", "C:/air-worker", prompt, reviewer)
	args := strings.Join(cmd.Args[1:], " ")
	for _, required := range []string{
		"--provider nous", "--model anthropic/claude-sonnet-5.5", "--reasoning medium",
		"--query-file -", "--oneshot", "--max-turns 1", "--run-budget 120",
		"--toolsets bot_room", "--safe-mode", "--ignore-user-config", "--ignore-rules", "--format stream-json", "--source tool",
	} {
		if !strings.Contains(args, required) {
			t.Fatalf("Hermes args %q missing %q", args, required)
		}
	}
	if strings.Contains(args, prompt) {
		t.Fatalf("prompt leaked into Hermes argv: %q", args)
	}
	if cmd.Dir != "C:/air-worker" {
		t.Fatalf("Hermes working directory=%q", cmd.Dir)
	}
	input, err := io.ReadAll(cmd.Stdin)
	if err != nil || string(input) != prompt {
		t.Fatalf("Hermes stdin did not contain the prompt: err=%v", err)
	}
	wantEnv := map[string]bool{"HERMES_IGNORE_USER_CONFIG": false, "HERMES_IGNORE_RULES": false, "HERMES_SAFE_MODE": false}
	blockedEnv := map[string]struct{}{"ANTHROPIC_API_KEY": {}, "ANTHROPIC_AUTH_TOKEN": {}, "ANTHROPIC_TOKEN": {}, "ANTHROPIC_BASE_URL": {}, "CLAUDE_CODE_OAUTH_TOKEN": {}}
	for _, item := range cmd.Env {
		key, _, _ := strings.Cut(item, "=")
		key = strings.ToUpper(key)
		if _, blocked := blockedEnv[key]; blocked {
			t.Fatalf("Hermes child environment contains blocked provider variable %s", key)
		}
		if _, ok := wantEnv[key]; ok {
			wantEnv[key] = true
		}
	}
	for key, present := range wantEnv {
		if !present {
			t.Fatalf("Hermes child environment lacks %s=1", key)
		}
	}
}
