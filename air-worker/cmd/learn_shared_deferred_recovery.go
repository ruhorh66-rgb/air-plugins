package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

const sharedReviewRecoveryMaxBytes int64 = 8 * 1024 * 1024
const sharedReviewRecoveryMaxDispatch = 12

type sharedDeferredReviewJob struct {
	Product string `json:"product"`
	RunID   string `json:"run_id"`
}

type sharedDeferredCompletion struct {
	Schema  string `json:"schema"`
	EventID string `json:"event_id"`
	RunID   string `json:"run_id"`
	Kind    string `json:"kind"`
	Source  string `json:"source"`
}

// Review recovery is a READ-ONLY view over module-owned events/reviews.
// The module remains the only journal/ledger writer and decides idempotency.
func pendingSharedLearningReviewRuns(product string) ([]string, error) {
	s, enabled, err := readSharedLearningSettings(product)
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, nil
	}
	path := filepath.Join(s.RuntimeRoot, "events.jsonl")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	st, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() > sharedReviewRecoveryMaxBytes {
		return nil, fmt.Errorf("deferred review journal exceeds bounded scan: %d bytes", st.Size())
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	var recent []sharedDeferredCompletion
	seen := make(map[string]bool)
	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		var event sharedDeferredCompletion
		if err := json.Unmarshal([]byte(raw), &event); err != nil {
			return nil, err
		}
		if event.Schema != "air.learning.event/v1" || event.Kind != "run_completed" ||
			event.Source != "host-finalize" {
			continue
		}
		if !strings.HasPrefix(event.EventID, "EV-") || event.RunID == "" {
			return nil, errors.New("deferred review event has invalid identity")
		}
		if seen[event.EventID] {
			continue
		}
		seen[event.EventID] = true
		recent = append(recent, event)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	var pending []string
	for i := len(recent) - 1; i >= 0 && len(pending) < sharedReviewRecoveryMaxDispatch; i-- {
		ev := recent[i]
		raw, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "reviews", ev.EventID+".json"))
		if errors.Is(err, os.ErrNotExist) {
			pending = append(pending, ev.RunID)
			continue
		}
		if runtime.GOOS == "windows" &&
			(errors.Is(err, syscall.Errno(32)) || errors.Is(err, syscall.Errno(33))) {
			// A concurrent reviewer is publishing its index. Do not treat a
			// temporary Windows sharing violation as an absent review.
			continue
		}
		if err != nil {
			return nil, err
		}
		var review struct {
			EventID string `json:"event_id"`
			Phase   string `json:"phase"`
			Outcome string `json:"outcome"`
			Attempt int    `json:"attempt"`
			At      string `json:"at"`
		}
		if err := json.Unmarshal(raw, &review); err != nil {
			return nil, err
		}
		if review.EventID != ev.EventID {
			return nil, errors.New("deferred review index does not match event")
		}
		switch review.Phase {
		case "prepared":
			// A preserved candidate must be completed without a second model call.
			pending = append(pending, ev.RunID)
		case "complete":
			// The shared module caps retry attempts and controls proposal identity.
			if review.Outcome == "error" && review.Attempt < 2 {
				pending = append(pending, ev.RunID)
			}
		case "reviewing":
			// Do not race a live reviewer. Only a plausibly abandoned transaction
			// is offered back to the module on a later session.
			started, err := time.Parse(time.RFC3339Nano, review.At)
			if err != nil {
				return nil, err
			}
			if time.Since(started) > 6*time.Minute {
				pending = append(pending, ev.RunID)
			}
		default:
			return nil, fmt.Errorf("unknown deferred review phase %q", review.Phase)
		}
	}
	return pending, nil
}

func resumeSharedLearningReviews(product string, start func(string, string) error) error {
	runs, err := pendingSharedLearningReviewRuns(product)
	if err != nil {
		return err
	}
	if start == nil {
		return nil
	}
	for _, runID := range runs {
		if err := start(product, runID); err != nil {
			return err
		}
	}
	return nil
}
