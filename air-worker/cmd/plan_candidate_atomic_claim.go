package main

import (
	"context"
	"fmt"
)

// planCandidateNumericClaimRef binds the Git reservation to the NUMBER, not
// the slug: N-121_next and N-121_other must compete for the same claim.
// This is an OS-temp fixture-only protocol primitive, NOT a production
// authority grant or a path to bypass planNodeAuthorityReason.
func planCandidateNumericClaimRef(c planCandidate, id string) (string, error) {
	match := planNodeIDRE.FindStringSubmatch(id)
	if match == nil || !candidateOperationRE.MatchString(c.Operation) {
		return "", fmt.Errorf("invalid numeric node claim identity")
	}
	prefix := ""
	if _, ok := c.Files["plan/"+id+".md"]; !ok {
		prefix = "air-worker/"
	}
	if _, ok := c.Files[prefix+"plan/"+id+".md"]; !ok {
		return "", fmt.Errorf("claimed node is not the exact candidate file")
	}
	if len(c.Files) != 3 {
		return "", fmt.Errorf("claim requires exactly PLAN/node/outbox")
	}
	if err := validateCandidateContent(prefix, c.Operation, c.Files); err != nil {
		return "", fmt.Errorf("claim content validation: %w", err)
	}
	// The only supported claim path is numeric, independent of display title.
	// An unexpected case, slash, or another node slug cannot expand this ref.
	return "refs/air-worker/claims/N-" + match[1], nil
}

// publishPlanCandidateAtomicClaim MUST publish both the new canonical
// generation and the N-ID reservation in one local bare Git transaction.
// It only works inside the existing verified standalone OS-temp fixture.
// Failure/pending status NEVER authorizes writes to local PLAN/N/LEARN.
func publishPlanCandidateAtomicClaim(
	ctx context.Context, repo, originBare string,
	c planCandidate, expectedOldSHA, nodeID string,
) error {
	claimRef, err := planCandidateNumericClaimRef(c, nodeID)
	if err != nil {
		return &planPublishError{State: "conflict", Reason: err.Error()}
	}
	return publishPlanCandidateWithClaim(
		ctx, repo, originBare, "refs/heads/canonical",
		c, expectedOldSHA, nil, claimRef,
	)
}
