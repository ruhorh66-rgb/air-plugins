package main

import (
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Two distinct learning owners must not be serialized behind each other's
// potentially slow reviewer. The dispatcher is still one bounded process.
func TestDeferredBatchIndependentOwnerIsNotBlockedBySlowReviewer(t *testing.T) {
	self := filepath.Join(t.TempDir(), "self")
	target := filepath.Join(t.TempDir(), "target")
	jobs := []sharedDeferredReviewJob{
		{Product: self, RunID: "slow"},
		{Product: target, RunID: "fast"},
	}
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	fastDone := make(chan struct{})
	done := make(chan []sharedDeferredReviewResult, 1)
	go func() {
		results := runSharedDeferredReviewBatch(jobs, func(job sharedDeferredReviewJob) sharedDeferredReviewResult {
			if job.Product == self {
				close(slowStarted)
				<-releaseSlow
				return sharedDeferredReviewResult{Product: job.Product, RunID: job.RunID, Status: "error", Error: "simulated slow reviewer failure"}
			}
			close(fastDone)
			return sharedDeferredReviewResult{Product: job.Product, RunID: job.RunID, Status: "candidate", ReceiptID: "RV-fast"}
		})
		done <- results
	}()
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		close(releaseSlow)
		t.Fatal("slow reviewer was not scheduled")
	}
	select {
	case <-fastDone:
		// A separate owner progressed before the slow review was released.
	case <-time.After(2 * time.Second):
		close(releaseSlow)
		<-done
		t.Fatal("slow reviewer blocked independent learning owner")
	}
	close(releaseSlow)
	results := <-done
	if len(results) != 2 || results[0].Status != "error" || results[1].Status != "candidate" ||
		results[1].ReceiptID != "RV-fast" || !strings.Contains(results[0].Error, "slow reviewer") {
		t.Fatalf("ordered independent outcomes lost: %+v", results)
	}
}

// Multiple pending runs of the SAME owner must remain sequential even when
// another owner can make progress independently. Output order is input order.
func TestDeferredBatchSerializesRunsPerOwnerAndPreservesOrder(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "owner")
	other := filepath.Join(t.TempDir(), "other")
	jobs := []sharedDeferredReviewJob{
		{Product: owner, RunID: "first"},
		{Product: other, RunID: "parallel"},
		{Product: owner, RunID: "second"},
	}
	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	otherDone := make(chan struct{})
	secondStarted := make(chan struct{})
	done := make(chan []sharedDeferredReviewResult, 1)
	go func() {
		results := runSharedDeferredReviewBatch(jobs, func(job sharedDeferredReviewJob) sharedDeferredReviewResult {
			switch job.RunID {
			case "first":
				close(firstStarted)
				<-releaseFirst
			case "parallel":
				close(otherDone)
			case "second":
				close(secondStarted)
			}
			return sharedDeferredReviewResult{Product: job.Product, RunID: job.RunID, Status: "no_candidate"}
		})
		done <- results
	}()
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		close(releaseFirst)
		t.Fatal("first owner job did not start")
	}
	select {
	case <-otherDone:
	case <-time.After(2 * time.Second):
		close(releaseFirst)
		<-done
		t.Fatal("independent owner did not progress")
	}
	select {
	case <-secondStarted:
		close(releaseFirst)
		<-done
		t.Fatal("second job of same owner ran before first completed")
	default:
	}
	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		<-done
		t.Fatal("second job never resumed after first completed")
	}
	results := <-done
	if len(results) != 3 {
		t.Fatalf("lost review results: %+v", results)
	}
	for i, row := range results {
		if row.Product != jobs[i].Product || row.RunID != jobs[i].RunID || row.Status != "no_candidate" {
			t.Fatalf("review result index %d differs from input: %+v", i, row)
		}
	}
}

func TestDeferredBatchBoundsSimultaneousOwnerReviewers(t *testing.T) {
	jobs := []sharedDeferredReviewJob{
		{Product: filepath.Join(t.TempDir(), "owner-one"), RunID: "one"},
		{Product: filepath.Join(t.TempDir(), "owner-two"), RunID: "two"},
		{Product: filepath.Join(t.TempDir(), "owner-three"), RunID: "three"},
	}
	var running, maximum atomic.Int32
	started := make(chan struct{}, len(jobs))
	release := make(chan struct{})
	done := make(chan []sharedDeferredReviewResult, 1)
	go func() {
		done <- runSharedDeferredReviewBatch(jobs, func(job sharedDeferredReviewJob) sharedDeferredReviewResult {
			n := running.Add(1)
			for {
				old := maximum.Load()
				if n <= old || maximum.CompareAndSwap(old, n) {
					break
				}
			}
			started <- struct{}{}
			<-release
			running.Add(-1)
			return sharedDeferredReviewResult{Product: job.Product, RunID: job.RunID, Status: "no_candidate"}
		})
	}()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			close(release)
			<-done
			t.Fatal("two independent owners did not start")
		}
	}
	select {
	case <-started:
		close(release)
		<-done
		t.Fatal("more than two review owners executed concurrently")
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	select {
	case results := <-done:
		if len(results) != len(jobs) || maximum.Load() != 2 || running.Load() != 0 {
			t.Fatalf("bounded concurrent review failed: max=%d active=%d results=%+v", maximum.Load(), running.Load(), results)
		}
		for i, result := range results {
			if result.Product != jobs[i].Product || result.RunID != jobs[i].RunID || result.Status != "no_candidate" {
				t.Fatalf("results not stable after bounded execution: %+v", results)
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bounded queued owner did not resume")
	}
}
