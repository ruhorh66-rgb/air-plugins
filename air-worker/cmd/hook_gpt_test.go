package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func hookTestStateForPrincipal(t *testing.T, principal, sessionID, product string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(hookStateDirEnv, dir)
	id, err := parseIdentity(principal, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	mode := sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}
	if err := writeStateJSON(sessionModePath(dir, id), mode); err != nil {
		t.Fatal(err)
	}
	if product != "" {
		state := sessionProductState{Path: product, Principal: id.Principal, SessionKey: id.SessionKey}
		if err := writeStateJSON(sessionProductPath(dir, id), state); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestHookGPTPrincipalUsesGPTSessionNamespace(t *testing.T) {
	session := "gpt-session"
	hookTestStateForPrincipal(t, "chatgpt", session, "")
	stubHookHandler(t, "PreToolUse", func(in hookInput) (hookResult, error) {
		if in.Principal != "chatgpt" {
			t.Fatalf("principal=%q want chatgpt", in.Principal)
		}
		return hookResult{Block: true, Reason: "gpt control reached"}, nil
	})
	withStdin(t, `{"principal":"chatgpt","session_id":"`+session+`","hook_event_name":"PreToolUse","tool_name":"PowerShell"}`)

	out, code := captureStderr(t, func() int { return cmdHook([]string{"PreToolUse"}) })
	if code != 2 || !strings.Contains(out, "gpt control reached") {
		t.Fatalf("GPT active session did not reach shared control handler: code=%d stderr=%q", code, out)
	}
}

func TestHookGPTUserPromptDoesNotMintApprovalWithoutTrustedTransport(t *testing.T) {
	product := t.TempDir()
	session := "gpt-approval"
	hookTestStateForPrincipal(t, "chatgpt", session, product)
	id := "LP-gpt-no-self-approval"
	seedLearnProposal(t, product, id, "verify-before-claim", "Use evidence before claim.")

	withStdin(t, `{"principal":"chatgpt","session_id":"`+session+`","hook_event_name":"UserPromptSubmit","prompt":"да `+id+`"}`)
	if code := cmdHook([]string{"UserPromptSubmit"}); code != 0 {
		t.Fatalf("GPT lifecycle event should remain fail-open, got %d", code)
	}

	pattern := filepath.Join(learnPaths(product).Root, "approvals", "*", id+".json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("untrusted GPT transport minted approval: %v", files)
	}
	if _, err := os.Stat(learnPaths(product).Proposals); err != nil {
		t.Fatalf("proposal state unexpectedly lost: %v", err)
	}
}
