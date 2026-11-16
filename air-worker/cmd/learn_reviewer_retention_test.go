package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type retentionJob struct {
	name, status, phase string
	age                 time.Duration
	sealed, tamper      bool
	archived            bool
}

func TestA24RetentionArchivesOnlySettledJobsOverLimit(t *testing.T) {
	product, runtime := t.TempDir(), t.TempDir()
	scope := legacyScope(product)
	now := time.Now().UTC()
	old := 8 * 24 * time.Hour
	jobs := []retentionJob{
		{"settled-a", jobStatusDone, "complete", old + time.Hour, true, false, true},
		{"settled-b", jobStatusFailed, "complete", old, true, false, true},
		{"running", jobStatusRunning, "reviewing", old, false, false, false},
		{"unsealed", jobStatusDone, "complete", old, false, false, false},
		{"tampered", jobStatusDone, "complete", old, true, true, false},
		{"prepared", jobStatusDone, "prepared", old, true, false, false},
		{"reviewing", jobStatusDone, "reviewing", old, true, false, false},
		{"young", jobStatusDone, "complete", 24 * time.Hour, true, false, false},
	}
	if err := os.MkdirAll(filepath.Join(runtime, "reviews"), 0o700); err != nil {
		t.Fatal(err)
	}
	var journal []string
	paths := map[string][2]string{}
	for _, j := range jobs {
		event := map[string]any{"schema": "air.learning.event/v1", "event_id": "EV-" + j.name, "run_id": "AW-CLI-" + j.name, "kind": "run_completed"}
		line, _ := json.Marshal(event)
		journal = append(journal, string(line))
		sha := learnSHA(line)
		review, _ := json.Marshal(map[string]string{"event_id": "EV-" + j.name, "phase": j.phase})
		if err := os.WriteFile(filepath.Join(runtime, "reviews", "EV-"+j.name+".json"), review, 0o600); err != nil {
			t.Fatal(err)
		}
		receipt, output := writeReviewerJob(t, scope, learningReviewerStep(sha), sha, j.status, "out "+j.name)
		r, err := readJobReceipt(receipt)
		if err != nil {
			t.Fatal(err)
		}
		r.StartedAt = now.Add(-j.age)
		if !j.sealed {
			r.OutputSHA256 = ""
		}
		if err := writeJobReceipt(receipt, r); err != nil {
			t.Fatal(err)
		}
		if j.tamper {
			if err := os.WriteFile(output, []byte("changed"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		paths[j.name] = [2]string{receipt, output}
	}
	if err := os.WriteFile(filepath.Join(runtime, "events.jsonl"), []byte(strings.Join(journal, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Under the limit nothing moves.
	if err := archiveSettledReviewerJobsAt(scope, runtime, len(jobs), now); err != nil {
		t.Fatal(err)
	}
	for _, j := range jobs {
		if _, err := os.Stat(paths[j.name][0]); err != nil {
			t.Fatalf("%s moved while under the limit", j.name)
		}
	}

	if err := archiveSettledReviewerJobsAt(scope, runtime, 2, now); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(product, ".woody", "jobs-archive", now.Format("200601"))
	for _, j := range jobs {
		receipt, output := paths[j.name][0], paths[j.name][1]
		_, inJobs := os.Stat(receipt)
		archivedOut, inArchive := os.ReadFile(filepath.Join(archive, filepath.Base(output)))
		if j.archived {
			if inJobs == nil || inArchive != nil || string(archivedOut) != "out "+j.name {
				t.Fatalf("%s: settled job not archived intact", j.name)
			}
			if _, err := os.Stat(filepath.Join(archive, filepath.Base(receipt))); err != nil {
				t.Fatalf("%s: receipt missing from archive", j.name)
			}
			continue
		}
		if inJobs != nil {
			t.Fatalf("%s: protected evidence was moved", j.name)
		}
	}
	trace, err := os.ReadFile(filepath.Join(runtime, "reviewer-job-archive.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(trace), `"outcome":"moving"`) != 2 || strings.Count(string(trace), `"outcome":"archived"`) != 2 {
		t.Fatalf("trace must record each move before and after:\n%s", trace)
	}
}

// A3/A2.4 boundary: the recovery logic sits right before the start guard. When
// another process holds the job lock, the review still gets the old job_busy.
func TestA24LockHeldElsewhereStillJobBusy(t *testing.T) {
	scope := legacyScope(t.TempDir())
	sha := learnSHA([]byte("busy"))
	step := learningReviewerStep(sha)
	if _, found, err := recoverLearningReviewerJob(scope, step, sha); found || err != nil {
		t.Fatalf("no receipt yet: found=%v err=%v", found, err)
	}
	saved := jobReceiptAcquireLock
	jobReceiptAcquireLock = func(string) (*osLock, bool) { return nil, false }
	defer func() { jobReceiptAcquireLock = saved }()
	_, err := runReceiptedWithMeta(t.Context(), scope, step, "learning-reviewer", nil, jobReceiptMeta{Role: "learning-reviewer"})
	if err == nil {
		t.Fatal("start under a lock held elsewhere was not refused")
	}
	if reason := learningFailureReason([]byte("learning reviewer: stage=invoke: " + err.Error() + "\n")); reason != "reviewer_job_busy" {
		t.Fatalf("lock held elsewhere gave %q, want reviewer_job_busy (%v)", reason, err)
	}
	receipt, _ := jobReceiptPaths(scope, step, "learning-reviewer")
	if _, statErr := os.Stat(receipt); !os.IsNotExist(statErr) {
		t.Fatalf("refused start left a receipt: %v", statErr)
	}
}
