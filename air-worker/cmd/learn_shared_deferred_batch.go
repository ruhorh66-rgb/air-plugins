package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// review-batch is an internal, bounded, ONE-PROCESS detached dispatcher.
// Only the module-owned review operation can create proposals; no approval,
// apply override, arbitrary executable, or journal mutation exists here.
type sharedDeferredReviewResult struct {
	Product   string `json:"product"`
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	ReceiptID string `json:"receipt_id,omitempty"`
	Error     string `json:"error,omitempty"`
}

// A stalled reviewer must not prevent the other learning owner from completing.
// Never run two reviews of the SAME product concurrently: its journal and
// proposal state remain serialized. The CLI bounds jobs before this function,
// so the number of goroutines cannot exceed the allowed job count.
// Results remain in input order for exact per-run machine receipts.
func runSharedDeferredReviewBatch(jobs []sharedDeferredReviewJob, run func(sharedDeferredReviewJob) sharedDeferredReviewResult) []sharedDeferredReviewResult {
	results := make([]sharedDeferredReviewResult, len(jobs))
	byOwner := make(map[string][]int)
	var owners []string
	for i, job := range jobs {
		key := strings.ToLower(filepath.Clean(job.Product))
		if _, exists := byOwner[key]; !exists {
			owners = append(owners, key)
		}
		byOwner[key] = append(byOwner[key], i)
	}
	// A host hook has at most two independent learning owners (AirWorker
	// self and the current product). An internal CLI packet can contain more
	// products, but must never fan out unbounded paid reviewer processes.
	const maxConcurrentReviewOwners = 2
	slots := make(chan struct{}, maxConcurrentReviewOwners)
	var wg sync.WaitGroup
	for _, owner := range owners {
		indexes := byOwner[owner]
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			for _, i := range indexes {
				results[i] = run(jobs[i])
			}
		}()
	}
	wg.Wait()
	return results
}

func cmdSharedLearningReviewBatch(argv []string) int {
	fs := flag.NewFlagSet("learn review-batch", flag.ContinueOnError)
	jobsJSON := fs.String("jobs-json", "", "JSON array of previously persisted product/run identities")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 {
		return 2
	}
	if len(*jobsJSON) < 2 || len(*jobsJSON) > 16*1024 {
		fmt.Fprintln(os.Stderr, "review-batch: jobs-json size invalid")
		return 2
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(*jobsJSON)))
	dec.DisallowUnknownFields()
	var jobs []sharedDeferredReviewJob
	if err := dec.Decode(&jobs); err != nil {
		fmt.Fprintln(os.Stderr, "review-batch:", err)
		return 2
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		fmt.Fprintln(os.Stderr, "review-batch: invalid extra JSON")
		return 2
	}
	if len(jobs) == 0 || len(jobs) > 2*sharedReviewRecoveryMaxDispatch {
		fmt.Fprintln(os.Stderr, "review-batch: unexpected number of jobs")
		return 2
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		if !filepath.IsAbs(job.Product) || len(job.RunID) == 0 || len(job.RunID) > 256 {
			fmt.Fprintln(os.Stderr, "review-batch: invalid product/run identity")
			return 2
		}
		k := strings.ToLower(filepath.Clean(job.Product)) + "\n" + job.RunID
		if seen[k] {
			fmt.Fprintln(os.Stderr, "review-batch: duplicate job")
			return 2
		}
		seen[k] = true
	}
	results := runSharedDeferredReviewBatch(jobs, func(job sharedDeferredReviewJob) sharedDeferredReviewResult {
		row := sharedDeferredReviewResult{Product: job.Product, RunID: job.RunID}
		s, on, err := readSharedLearningSettings(job.Product)
		if err == nil && !on {
			err = errors.New("shared learning disabled")
		}
		if err == nil {
			res, reviewErr := executeSharedLearning(job.Product, s, "review", map[string]string{"run_id": job.RunID})
			row.Status, row.ReceiptID, err = res.Status, res.ID, reviewErr
			if err == nil && (res.Status == "error" || res.Status == "running" || res.Status == "prepared") {
				err = fmt.Errorf("non-terminal reviewer result %s", res.Status)
			}
		}
		if err != nil {
			row.Status, row.Error = "error", err.Error()
		}
		return row
	})
	failed := false
	for _, row := range results {
		if row.Error != "" {
			failed = true
		}
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"schema":   "air-worker.deferred-review-batch/v1",
		"results":  results,
		"complete": !failed,
	})
	if failed {
		return 1
	}
	return 0
}
