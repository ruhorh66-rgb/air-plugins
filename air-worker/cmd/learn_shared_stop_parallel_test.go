package main

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Stop persists independent owner events before dispatch, but one slow disk
// writer must not delay starting the other owner's durable completion.
func TestStopIndependentOwnerCompletionConcurrentAndJoined(t *testing.T) {
	self := filepath.Join(t.TempDir(), "self")
	target := filepath.Join(t.TempDir(), "target")
	selfStarted := make(chan struct{})
	targetPersisted := make(chan struct{})
	releaseSelf := make(chan struct{})
	done := make(chan []sharedStopCompletion, 1)
	go func() {
		done <- completeSharedStopOwners([]string{self, target}, hookInput{RunID: "run-1"}, func(product string, in hookInput) (bool, hookResult, error) {
			if in.RunID != "run-1" {
				return false, hookResult{}, errors.New("run identity lost")
			}
			if product == self {
				close(selfStarted)
				<-releaseSelf
				return true, hookResult{}, errors.New("self error preserved")
			}
			close(targetPersisted)
			return true, hookResult{}, nil
		})
	}()
	select {
	case <-selfStarted:
	case <-time.After(3 * time.Second):
		close(releaseSelf)
		t.Fatal("self owner did not start")
	}
	select {
	case <-targetPersisted:
	case <-time.After(3 * time.Second):
		close(releaseSelf)
		<-done
		t.Fatal("slow self completion prevented target durability")
	}
	select {
	case <-done:
		close(releaseSelf)
		t.Fatal("Stop returned before both owner completions resolved")
	default:
	}
	close(releaseSelf)
	results := <-done
	if len(results) != 2 || !results[0].handled || !results[1].handled ||
		!strings.Contains(results[0].err.Error(), "self error preserved") || results[1].err != nil {
		t.Fatalf("independent owner results reordered or lost: %+v", results)
	}
}

func TestStopSingleOwnerCompletionPreservesErrors(t *testing.T) {
	root := filepath.Join(t.TempDir(), "single")
	results := completeSharedStopOwners([]string{root}, hookInput{RunID: "single-run"}, func(product string, in hookInput) (bool, hookResult, error) {
		if product != root || in.RunID != "single-run" {
			t.Fatal("wrong product or run")
		}
		return true, hookResult{}, errors.New("single owner failure")
	})
	if len(results) != 1 || !results[0].handled || results[0].err == nil ||
		results[0].err.Error() != "single owner failure" {
		t.Fatalf("single owner failure suppressed: %+v", results)
	}
}
