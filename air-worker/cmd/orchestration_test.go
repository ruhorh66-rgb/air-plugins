package main

import (
	"strings"
	"testing"
)

func TestCriterion55AgentEvidence(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"agent-1","name":"Agent","input":{"subagent_type":"Explore","run_in_background":false}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"agent-1","content":"one done"}]}}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"agent-2","name":"Agent","input":{"subagent_type":"Plan","run_in_background":false}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"agent-2","content":"two done"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"session_id":"s1","num_turns":4,"result":"leader done"}`,
	}, "\n")
	res, stats := parseClaudeStream(raw, 2)
	if res == nil || res.IsError {
		t.Fatalf("result not parsed: %+v", res)
	}
	if !orchestrationProven(stats) {
		t.Fatalf("valid orchestration not proven: %+v", stats)
	}
	if stats.Started != 2 || stats.Completed != 2 || len(stats.IDs) != 2 {
		t.Fatalf("wrong agent counts: %+v", stats)
	}
}

func TestOrchestrationRejectsPromptOnly(t *testing.T) {
	raw := `{"type":"result","subtype":"success","is_error":false,"session_id":"s1","result":"I delegated work"}`
	_, stats := parseClaudeStream(raw, 2)
	if orchestrationProven(stats) {
		t.Fatalf("prompt-only orchestration must not pass: %+v", stats)
	}
	if stats.Started != 0 || stats.Completed != 0 || len(stats.Problems) == 0 {
		t.Fatalf("missing factual failure evidence: %+v", stats)
	}
}

func TestOrchestrationRequiresSynchronousAgentResult(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"agent-bg","name":"Agent","input":{"run_in_background":true}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"agent-bg","content":"launched"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}`,
	}, "\n")
	_, stats := parseClaudeStream(raw, 1)
	if orchestrationProven(stats) || stats.Background != 1 || stats.Completed != 0 {
		t.Fatalf("background spawn is not completed orchestration evidence: %+v", stats)
	}
}

func TestOrchestrationAddsAgentToolOnce(t *testing.T) {
	got := ensureCSVItem("Read,Write,Agent", "Agent")
	if got != "Read,Write,Agent" {
		t.Fatalf("duplicate Agent tool: %q", got)
	}
	got = ensureCSVItem("Read,Write", "Agent")
	if got != "Read,Write,Agent" {
		t.Fatalf("Agent tool not added: %q", got)
	}
}

func TestOrchestrationRejectsOverFanout(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"assistant","message":{"content":[{"type":"tool_use","id":"a1","name":"Agent","input":{"run_in_background":false}},{"type":"tool_use","id":"a2","name":"Agent","input":{"run_in_background":false}},{"type":"tool_use","id":"a3","name":"Agent","input":{"run_in_background":false}}]}}`,
		`{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"a1"},{"type":"tool_result","tool_use_id":"a2"},{"type":"tool_result","tool_use_id":"a3"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"result":"done"}`,
	}, "\n")
	_, stats := parseClaudeStream(raw, 2)
	if orchestrationProven(stats) {
		t.Fatalf("over-fanout must be rejected: %+v", stats)
	}
}
