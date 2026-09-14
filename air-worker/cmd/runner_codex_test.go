package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseCodexResultSuccessWithWarning(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-1"}`,
		`{"type":"item.completed","item":{"type":"error","message":"nonfatal hook warning"}}`,
		`{"type":"turn.started"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"DONE"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":2}}`,
	}, "\n")
	r := parseCodexResult(raw, nil)
	if !r.Ok || r.Subtype != "success" || r.Session != "th-1" || r.Turns == nil || *r.Turns != 1 {
		t.Fatalf("unexpected result: %+v", r)
	}
	if !strings.Contains(r.Detail, "warning: nonfatal hook warning") || !strings.Contains(r.Detail, "DONE") {
		t.Fatalf("detail lost warning or message: %q", r.Detail)
	}
}
func TestParseCodexResultTurnFailed(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"thread.started","thread_id":"th-2"}`,
		`{"type":"turn.started"}`,
		`{"type":"error","message":"budget exhausted"}`,
		`{"type":"turn.failed","error":{"message":"budget exhausted"}}`,
	}, "\n")
	r := parseCodexResult(raw, errors.New("exit status 1"))
	if r.Ok || r.Subtype != "runner_error" {
		t.Fatalf("fatal stream accepted: %+v", r)
	}
	if !strings.Contains(r.Detail, "budget exhausted") {
		t.Fatalf("fatal detail lost: %q", r.Detail)
	}
}

func TestParseCodexResultMissingTerminal(t *testing.T) {
	r := parseCodexResult(`{"type":"thread.started","thread_id":"th-3"}`, nil)
	if r.Ok || r.Subtype != "unparsed" {
		t.Fatalf("unterminated stream accepted: %+v", r)
	}
}
