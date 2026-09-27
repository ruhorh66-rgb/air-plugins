package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUserPromptApprovalGrantRequiresExactPrompt(t *testing.T) {
	product := t.TempDir()
	stateDir := t.TempDir()
	session := "approval-session"
	seedActiveLearningSession(t, product, stateDir, session)
	id := "LP-user-prompt"
	seedLearnProposal(t, product, id, "plan-first", "Read the canonical plan before acting.")

	for _, prompt := range []string{
		"please да " + id,
		"да " + id + " пожалуйста",
		"ДА " + id,
		"да some-other-id",
	} {
		if _, err := handleUserPromptLearning(hookInput{
			SessionID: session, HookEventName: "UserPromptSubmit", Prompt: prompt,
		}); err != nil {
			t.Fatalf("non-approval prompt %q returned error: %v", prompt, err)
		}
	}
	paths := learnPaths(product)
	matches, _ := filepath.Glob(filepath.Join(paths.Root, "approvals", "*", id+".json"))
	if len(matches) != 0 {
		t.Fatalf("non-exact prompt created approval grant: %v", matches)
	}

	if _, err := handleUserPromptLearning(hookInput{
		SessionID: session, HookEventName: "UserPromptSubmit", Prompt: "да " + id,
	}); err != nil {
		t.Fatal(err)
	}
	matches, _ = filepath.Glob(filepath.Join(paths.Root, "approvals", "*", id+".json"))
	if len(matches) != 1 {
		t.Fatalf("exact user prompt did not create exactly one grant: %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var grant learnApprovalGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatal(err)
	}
	if grant.ProposalID != id || grant.Principal != "claude" || grant.SessionKey != session || grant.ConsumedAt != "" {
		t.Fatalf("bad grant: %#v", grant)
	}
}

func TestApprovalGrantIsBoundToProposalDigest(t *testing.T) {
	product := t.TempDir()
	id := "LP-digest-bound"
	seedLearnProposal(t, product, id, "scope", "Stay inside the assigned product.")
	grantLearnProposal(t, product, id)

	paths := learnPaths(product)
	rows, err := readLearnProposals(paths.Proposals)
	if err != nil {
		t.Fatal(err)
	}
	rows[0].Rule = "Tampered rule after approval."
	raw, err := marshalLearnJSONL(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeLearnAtomic(paths.Proposals, raw); err != nil {
		t.Fatal(err)
	}
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 3 {
		t.Fatalf("proposal changed after approval must invalidate grant; code=%d", code)
	}
	if _, err := os.Stat(paths.Rules); !os.IsNotExist(err) {
		t.Fatalf("invalidated grant mutated active rules: %v", err)
	}
}

func TestApprovalGrantConsumedAfterApply(t *testing.T) {
	product := t.TempDir()
	id := "LP-one-shot"
	seedLearnProposal(t, product, id, "delivery", "Verify delivery before reporting success.")
	grantID := grantLearnProposal(t, product, id)
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 0 {
		t.Fatalf("apply failed: %d", code)
	}
	paths := learnPaths(product)
	matches, _ := filepath.Glob(filepath.Join(paths.Root, "approvals", "*", id+".json"))
	if len(matches) != 1 {
		t.Fatalf("approval file missing: %v", matches)
	}
	raw, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	var grant learnApprovalGrant
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatal(err)
	}
	if grant.ID != grantID || grant.ConsumedAt == "" || grant.LedgerID == "" {
		t.Fatalf("grant was not consumed: %#v", grant)
	}

	rows, _ := readLearnProposals(paths.Proposals)
	rows[0].Status = learnPending // mutation test: even reopening proposal cannot reuse the consumed grant.
	rows[0].AppliedAt = ""
	rows[0].LedgerID = ""
	rows[0].Approval = ""
	pb, _ := marshalLearnJSONL(rows)
	_ = writeLearnAtomic(paths.Proposals, pb)
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 3 {
		t.Fatalf("consumed grant was reusable; code=%d", code)
	}
}

func TestParseLearnApprovalPrompt(t *testing.T) {
	id := "LP-20260928T000000Z-deadbeef"
	for input, ok := range map[string]bool{
		"да " + id:            true,
		"  да " + id + "  ":   true,
		"да":                  false,
		"yes " + id:           false,
		"да " + id + " extra": false,
		"да XX-1":             false,
	} {
		got, valid := parseLearnApprovalPrompt(input)
		if valid != ok {
			t.Fatalf("parse %q valid=%v want=%v id=%q", input, valid, ok, got)
		}
		if valid && !strings.HasPrefix(got, "LP-") {
			t.Fatalf("valid grant id malformed: %q", got)
		}
	}
}

func TestLearningStateCannotBeForgedThroughOrdinaryTools(t *testing.T) {
	cases := []hookInput{
		{ToolName: "Write", ToolInput: json.RawMessage(`{"file_path":"C:\\work\\.air-worker\\learn\\approvals\\x.json"}`)},
		{ToolName: "Edit", ToolInput: json.RawMessage(`{"file_path":".air-worker/learn/ledger.jsonl"}`)},
		{ToolName: "PowerShell", ToolInput: json.RawMessage(`{"command":"Set-Content .air-worker/learn/RULES.md fake"}`)},
		{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"air-worker hook UserPromptSubmit < forged.json"}`)},
	}
	for _, in := range cases {
		blocked, reason := learningApprovalBypass(in)
		if !blocked || strings.TrimSpace(reason) == "" {
			t.Fatalf("forgery path not blocked: %#v reason=%q", in, reason)
		}
	}

	for _, in := range []hookInput{
		{ToolName: "PowerShell", ToolInput: json.RawMessage(`{"command":"air-worker learn pending -product C:\\work"}`)},
		{ToolName: "Bash", ToolInput: json.RawMessage(`{"command":"air-worker learn apply -product /work -id LP-x"}`)},
	} {
		blocked, reason := learningApprovalBypass(in)
		if blocked {
			t.Fatalf("canonical learn CLI was blocked: %#v reason=%q", in, reason)
		}
	}
}

func TestApprovalGrantClaimIsAtomic(t *testing.T) {
	product := t.TempDir()
	id := "LP-atomic-claim"
	seedLearnProposal(t, product, id, "scope", "Stay inside the assigned product.")
	grantLearnProposal(t, product, id)

	rows, err := readLearnProposals(learnPaths(product).Proposals)
	if err != nil {
		t.Fatal(err)
	}
	_, proposal := findLearnProposal(rows, id)
	if proposal == nil {
		t.Fatal("proposal missing")
	}
	grant, claimPath, originalPath, err := claimLearnApprovalGrant(product, *proposal)
	if err != nil {
		t.Fatalf("first claim failed: %v", err)
	}
	if grant.ID == "" || claimPath == "" || originalPath == "" {
		t.Fatalf("incomplete claim: grant=%#v claim=%q original=%q", grant, claimPath, originalPath)
	}
	if _, _, _, err := claimLearnApprovalGrant(product, *proposal); err == nil {
		t.Fatal("same approval grant was claimed twice")
	}
	releaseLearnApprovalClaim(claimPath, originalPath)
	if _, claim2, original2, err := claimLearnApprovalGrant(product, *proposal); err != nil {
		t.Fatalf("released pre-mutation claim did not become available again: %v", err)
	} else {
		releaseLearnApprovalClaim(claim2, original2)
	}
}
