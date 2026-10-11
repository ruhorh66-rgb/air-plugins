//go:build !windows

package main

import (
	"context"
	"reflect"
	"testing"
)

func TestPlanCandidatePublishUnsupportedFence(t *testing.T) {
	repo, origin, old, c := publishFixture(t)
	before, remoteBefore := n124Snapshot(t, repo), n124Snapshot(t, origin)
	assertPublishState(t, publishPlanCandidateCAS(context.Background(), repo, origin, publishRef, c, old), "conflict")
	if !reflect.DeepEqual(before, n124Snapshot(t, repo)) || !reflect.DeepEqual(remoteBefore, n124Snapshot(t, origin)) {
		t.Fatal("unsupported fence mutated fixture")
	}
}
