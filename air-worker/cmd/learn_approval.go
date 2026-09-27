package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const learnApprovalMaxAge = 24 * time.Hour

type learnApprovalGrant struct {
	Schema         string `json:"schema"`
	ID             string `json:"id"`
	ProposalID     string `json:"proposal_id"`
	ProposalSHA256 string `json:"proposal_sha256"`
	Principal      string `json:"principal"`
	SessionKey     string `json:"session_key"`
	PromptSHA256   string `json:"prompt_sha256"`
	CreatedAt      string `json:"created_at"`
	ConsumedAt     string `json:"consumed_at,omitempty"`
	LedgerID       string `json:"ledger_id,omitempty"`
}

func learnProposalSHA(p learnProposal) string {
	stable := struct {
		ID        string   `json:"id"`
		CreatedAt string   `json:"created_at"`
		Class     string   `json:"class"`
		Rule      string   `json:"rule"`
		SourceIDs []string `json:"source_ids,omitempty"`
	}{
		ID: p.ID, CreatedAt: p.CreatedAt, Class: p.Class, Rule: p.Rule, SourceIDs: p.SourceIDs,
	}
	b, _ := json.Marshal(stable)
	return learnSHA(b)
}

func parseLearnApprovalPrompt(prompt string) (string, bool) {
	text := strings.TrimSpace(prompt)
	if !strings.HasPrefix(text, "да ") {
		return "", false
	}
	id := strings.TrimSpace(strings.TrimPrefix(text, "да "))
	if id == "" || strings.ContainsAny(id, " \t\r\n") || !strings.HasPrefix(id, "LP-") {
		return "", false
	}
	if text != "да "+id {
		return "", false
	}
	return id, true
}

func learnApprovalPath(paths learnPathsSet, principal, session, proposalID string) (string, error) {
	id, err := parseIdentity(principal, session)
	if err != nil {
		return "", err
	}
	return filepath.Join(paths.Root, "approvals", id.namespace(), proposalID+".json"), nil
}

func captureLearnApprovalGrant(product, principal, session, prompt string) (string, error) {
	proposalID, ok := parseLearnApprovalPrompt(prompt)
	if !ok {
		return "", nil
	}
	paths := learnPaths(product)
	proposals, err := readLearnProposals(paths.Proposals)
	if err != nil {
		return "", err
	}
	_, proposal := findLearnProposal(proposals, proposalID)
	if proposal == nil {
		return "", fmt.Errorf("approval names unknown proposal %s", proposalID)
	}
	if proposal.Status != learnPending {
		return "", fmt.Errorf("approval names proposal %s with status %s", proposalID, proposal.Status)
	}
	grantPath, err := learnApprovalPath(paths, principal, session, proposalID)
	if err != nil {
		return "", err
	}
	if raw, err := os.ReadFile(grantPath); err == nil {
		var existing learnApprovalGrant
		if json.Unmarshal(raw, &existing) == nil &&
			existing.ProposalSHA256 == learnProposalSHA(*proposal) &&
			existing.ConsumedAt == "" {
			return existing.ID, nil
		}
	}
	now := time.Now().UTC()
	grantID, err := newLearnID("LG", now)
	if err != nil {
		return "", err
	}
	grant := learnApprovalGrant{
		Schema: learnSchemaVersion, ID: grantID, ProposalID: proposalID,
		ProposalSHA256: learnProposalSHA(*proposal),
		Principal:      principal, SessionKey: session,
		PromptSHA256: learnSHA([]byte(strings.TrimSpace(prompt))),
		CreatedAt:    now.Format(time.RFC3339Nano),
	}
	b, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		return "", err
	}
	if err := writeLearnAtomic(grantPath, append(b, byte(10))); err != nil {
		return "", err
	}
	return grantID, nil
}

type learnGrantCandidate struct {
	Path  string
	Grant learnApprovalGrant
	At    time.Time
}

func findLearnApprovalGrant(product string, proposal learnProposal) (learnApprovalGrant, string, error) {
	paths := learnPaths(product)
	pattern := filepath.Join(paths.Root, "approvals", "*", proposal.ID+".json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return learnApprovalGrant{}, "", err
	}
	now := time.Now().UTC()
	var candidates []learnGrantCandidate
	for _, path := range files {
		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var grant learnApprovalGrant
		if json.Unmarshal(raw, &grant) != nil ||
			grant.Schema != learnSchemaVersion ||
			grant.ProposalID != proposal.ID ||
			grant.ProposalSHA256 != learnProposalSHA(proposal) ||
			grant.ConsumedAt != "" {
			continue
		}
		if _, parseErr := parseIdentity(grant.Principal, grant.SessionKey); parseErr != nil {
			continue
		}
		at, parseErr := time.Parse(time.RFC3339Nano, grant.CreatedAt)
		if parseErr != nil || at.After(now.Add(5*time.Minute)) || now.Sub(at) > learnApprovalMaxAge {
			continue
		}
		candidates = append(candidates, learnGrantCandidate{Path: path, Grant: grant, At: at})
	}
	if len(candidates) == 0 {
		return learnApprovalGrant{}, "", errors.New("no unconsumed user-prompt approval grant; submit exact 'да <proposal-id>' in an active AirWorker session")
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].At.After(candidates[j].At) })
	return candidates[0].Grant, candidates[0].Path, nil
}

func claimLearnApprovalGrant(product string, proposal learnProposal) (learnApprovalGrant, string, string, error) {
	grant, originalPath, err := findLearnApprovalGrant(product, proposal)
	if err != nil {
		return learnApprovalGrant{}, "", "", err
	}
	claimPath := fmt.Sprintf("%s.claim-%d-%s", originalPath, os.Getpid(), grant.ID)
	if err := os.Rename(originalPath, claimPath); err != nil {
		return learnApprovalGrant{}, "", "", fmt.Errorf("approval grant is already claimed or unavailable: %w", err)
	}
	return grant, claimPath, originalPath, nil
}

func releaseLearnApprovalClaim(claimPath, originalPath string) {
	if claimPath == "" || originalPath == "" {
		return
	}
	_ = os.Rename(claimPath, originalPath)
}

func consumeLearnApprovalGrant(claimPath, originalPath string, grant learnApprovalGrant, ledgerID string) error {
	grant.ConsumedAt = time.Now().UTC().Format(time.RFC3339Nano)
	grant.LedgerID = ledgerID
	b, err := json.MarshalIndent(grant, "", "  ")
	if err != nil {
		return err
	}
	if err := writeLearnAtomic(claimPath, append(b, byte(10))); err != nil {
		return err
	}
	if err := os.Rename(claimPath, originalPath); err != nil {
		return fmt.Errorf("publish consumed approval grant: %w", err)
	}
	return nil
}
