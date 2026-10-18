package main

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// The proposed CAS must move the canonical PLAN generation and the numeric
// N-ID reservation IN ONE Git transaction. A claim alone never grants native
// mutation authority; this tests only an isolated OS-temp bare Git receiver.
func TestPlanCandidateAtomicClaimAndExactReplay(t *testing.T) {
	repo, origin, old, candidate := publishFixture(t)
	before := candidateOriginal(t, repo)
	ref := "refs/air-worker/claims/N-121"

	if err := publishPlanCandidateAtomicClaim(context.Background(), repo, origin, candidate, old, "N-121_next"); err != nil {
		t.Fatalf("atomic canonical+claim accept: %v", err)
	}
	if canonical := candidateGit(t, origin, "rev-parse", publishRef); canonical != candidate.OID {
		t.Fatalf("canonical mismatch: %s", canonical)
	}
	if claim := candidateGit(t, origin, "rev-parse", ref); claim != candidate.OID {
		t.Fatalf("numeric claim mismatch: %s", claim)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
		t.Fatal("atomic publish changed local PLAN/node/LEARN or foreign files")
	}
	// Readback of the exact accepted operation is idempotent, NOT proof of
	// exclusive owner identity.
	if err := publishPlanCandidateAtomicClaim(context.Background(), repo, origin, candidate, old, "N-121_next"); err != nil {
		t.Fatalf("accepted exact replay: %v", err)
	}
	if got := candidateGit(t, origin, "for-each-ref", "--format=%(refname)", "refs/air-worker/claims/"); got != ref {
		t.Fatalf("unexpected claims after replay: %q", got)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
		t.Fatal("accepted replay modified the local source")
	}
}

func TestPlanCandidateAtomicClaimRejectsUnprovenInputs(t *testing.T) {
	for _, variant := range []string{"wrong-slug", "wrong-number", "traversal", "already-claimed", "preclaimed-same-object", "symbolic-ref", "remote-advanced", "corrupt-outbox"} {
		t.Run(variant, func(t *testing.T) {
			repo, origin, old, c := publishFixture(t)
			name := "N-121_next"
			switch variant {
			case "wrong-slug":
				name = "N-121_different"
			case "wrong-number":
				name = "N-122_next"
			case "traversal":
				name = "../N-121_next"
			case "already-claimed":
				candidateGit(t, origin, "update-ref", "refs/air-worker/claims/N-121", old)
			case "preclaimed-same-object":
				candidateGit(t, origin, "update-ref", "refs/air-worker/claims/N-121", c.OID)
			case "symbolic-ref":
				candidateGit(t, origin, "symbolic-ref", "refs/air-worker/claims/N-121", publishRef)
			case "remote-advanced":
				candidateGit(t, origin, "update-ref", publishRef, c.OID, old)
			case "corrupt-outbox":
				c.Files["air-worker/plan/event-outbox/op-1.json"] = []byte("{}")
			}
			before := candidateOriginal(t, repo)
			canonical := candidateGit(t, origin, "rev-parse", publishRef)
			err := publishPlanCandidateAtomicClaim(context.Background(), repo, origin, c, old, name)
			if err == nil {
				t.Fatalf("accepted unsupported %s", variant)
			}
			if !strings.Contains(err.Error(), "conflict") && !strings.Contains(err.Error(), "pending") {
				t.Fatalf("%s did not fail closed: %v", variant, err)
			}
			if got := candidateGit(t, origin, "rev-parse", publishRef); got != canonical {
				t.Fatalf("%s changed canonical: %s", variant, got)
			}
			if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
				t.Fatalf("%s changed local files/index", variant)
			}
		})
	}
}

func TestPlanCandidateAtomicClaimOneWinnerForNumericID(t *testing.T) {
	repo, origin, old, a := publishFixture(t)
	b, err := preparePlanCandidate(context.Background(), repo, old, "op-2", candidatePayload("op-2", "N-121_other"))
	if err != nil {
		t.Fatal(err)
	}
	if a.OID == b.OID {
		t.Fatal("two logical clients must have distinct immutable candidates")
	}
	before := candidateOriginal(t, repo)
	candidates := []planCandidate{a, b}
	names := []string{"N-121_next", "N-121_other"}
	results := make([]error, 2)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range candidates {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = publishPlanCandidateAtomicClaim(context.Background(), repo, origin, candidates[i], old, names[i])
		}(i)
	}
	close(start)
	wg.Wait()
	if (results[0] == nil) == (results[1] == nil) {
		t.Fatalf("expected one winner, one nonzero conflict: %v", results)
	}
	winner := 0
	if results[0] != nil {
		winner = 1
	}
	if canonical := candidateGit(t, origin, "rev-parse", publishRef); canonical != candidates[winner].OID {
		t.Fatalf("canonical has unexpected winner: %s", canonical)
	}
	if claim := candidateGit(t, origin, "rev-parse", "refs/air-worker/claims/N-121"); claim != candidates[winner].OID {
		t.Fatalf("claim and PLAN published by different operations: %s", claim)
	}
	if !reflect.DeepEqual(before, candidateOriginal(t, repo)) {
		t.Fatal("race modified source files/PLAN/index")
	}
	if err := publishPlanCandidateAtomicClaim(context.Background(), repo, origin, candidates[1-winner], old, names[1-winner]); err == nil {
		t.Fatal("loser successfully replayed a different node with the same numeric ID")
	}
}
