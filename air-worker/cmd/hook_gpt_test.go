package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func trustBridgeParentForTest(t *testing.T) {
	t.Helper()
	oldPath, oldParent, oldNow := trustedHostMarkerPath, trustedHostParentPID, trustedHostNow
	now := time.Date(2026, 9, 28, 17, 40, 0, 0, time.UTC)
	marker := filepath.Join(t.TempDir(), "health-marker.json")
	body := fmt.Sprintf(`{"pid":4242,"state":"RUNNING","updated_utc":%q}`, now.Format(time.RFC3339Nano))
	if err := os.WriteFile(marker, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	trustedHostMarkerPath = marker
	trustedHostParentPID = func() int { return 4242 }
	trustedHostNow = func() time.Time { return now }
	t.Cleanup(func() {
		trustedHostMarkerPath, trustedHostParentPID, trustedHostNow = oldPath, oldParent, oldNow
	})
}

func TestHookGPTPrincipalUsesGPTSessionNamespace(t *testing.T) {
	session := "gpt-session"
	hookTestStateForPrincipal(t, "chatgpt", session, "")
	trustBridgeParentForTest(t)
	stubHookHandler(t, "PreToolUse", func(in hookInput) (hookResult, error) {
		if in.Principal != "chatgpt" {
			t.Fatalf("principal=%q want chatgpt", in.Principal)
		}
		return hookResult{Block: true, Reason: "gpt control reached"}, nil
	})
	withStdin(t, `{"principal":"chatgpt","host_trusted":true,"session_id":"`+session+`","hook_event_name":"PreToolUse","tool_name":"PowerShell"}`)

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

func TestHookGPTTrustedHostMintsApproval(t *testing.T) {
	product := t.TempDir()
	session := "gpt-trusted-approval"
	hookTestStateForPrincipal(t, "chatgpt", session, product)
	trustBridgeParentForTest(t)
	id := "LP-gpt-trusted-approval"
	seedLearnProposal(t, product, id, "verify-before-claim", "Use evidence before claim.")

	withStdin(t, `{"principal":"chatgpt","host_trusted":true,"session_id":"`+session+`","hook_event_name":"UserPromptSubmit","prompt":"да `+id+`"}`)
	if code := cmdHook([]string{"UserPromptSubmit"}); code != 0 {
		t.Fatalf("trusted GPT UserPromptSubmit failed: %d", code)
	}

	grantPath, err := learnApprovalPath(learnPaths(product), "chatgpt", session, id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(grantPath); err != nil {
		t.Fatalf("trusted GPT transport did not mint approval: %v", err)
	}
}
