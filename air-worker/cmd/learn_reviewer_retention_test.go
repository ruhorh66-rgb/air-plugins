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
		setReviewerJobAge(t, now.Add(-j.age), receipt, output)
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

func setReviewerJobAge(t *testing.T, at time.Time, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if err := os.Chtimes(p, at, at); err != nil {
			t.Fatal(err)
		}
	}
}

// One settled job ready to archive, plus a second receipt so the limit of 1
// is exceeded.
func settledReviewerJobFixture(t *testing.T) (sessionScope, string, string, string, time.Time) {
	t.Helper()
	product, runtime := t.TempDir(), t.TempDir()
	scope := legacyScope(product)
	now := time.Now().UTC()
	old := now.Add(-10 * 24 * time.Hour)
	if err := os.MkdirAll(filepath.Join(runtime, "reviews"), 0o700); err != nil {
		t.Fatal(err)
	}
	var journal []string
	var receipt, output string
	for _, name := range []string{"one", "two"} {
		line, _ := json.Marshal(map[string]any{"schema": "air.learning.event/v1", "event_id": "EV-" + name, "run_id": "AW-CLI-" + name})
		journal = append(journal, string(line))
		sha := learnSHA(line)
		review, _ := json.Marshal(map[string]string{"event_id": "EV-" + name, "phase": "complete"})
		if err := os.WriteFile(filepath.Join(runtime, "reviews", "EV-"+name+".json"), review, 0o600); err != nil {
			t.Fatal(err)
		}
		rc, out := writeReviewerJob(t, scope, learningReviewerStep(sha), sha, jobStatusDone, "out "+name)
		r, _ := readJobReceipt(rc)
		r.StartedAt = old.Add(time.Duration(len(journal)) * time.Hour)
		if err := writeJobReceipt(rc, r); err != nil {
			t.Fatal(err)
		}
		setReviewerJobAge(t, old, rc, out)
		if name == "one" {
			receipt, output = rc, out
		}
	}
	if err := os.WriteFile(filepath.Join(runtime, "events.jsonl"), []byte(strings.Join(journal, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return scope, runtime, receipt, output, now
}

// Evidence already in the archive is never replaced, and the attempt is traced.
func TestA24RetentionNeverReplacesArchivedEvidence(t *testing.T) {
	scope, runtime, receipt, output, now := settledReviewerJobFixture(t)
	archive := filepath.Join(scope.Root, ".woody", "jobs-archive", now.Format("200601"))
	if err := os.MkdirAll(archive, 0o700); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(archive, filepath.Base(output))
	if err := os.WriteFile(existing, []byte("older evidence"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := archiveSettledReviewerJobsAt(scope, runtime, 1, now); err == nil {
		t.Fatal("archiving over existing evidence was not refused")
	}
	if b, _ := os.ReadFile(existing); string(b) != "older evidence" {
		t.Fatal("archived evidence was replaced")
	}
	for _, p := range []string{receipt, output} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("job left its place after a refused move: %v", err)
		}
	}
	trace, _ := os.ReadFile(filepath.Join(runtime, "reviewer-job-archive.jsonl"))
	if !strings.Contains(string(trace), `"outcome":"skipped-destination-exists"`) {
		t.Fatalf("refused move has no terminal trace:\n%s", trace)
	}
}

// A receipt whose own fields do not name this job's canonical files is not
// acted on: a modified output_path cannot steer a move elsewhere.
func TestA24RetentionIgnoresReceiptWithForeignPaths(t *testing.T) {
	scope, runtime, receipt, output, now := settledReviewerJobFixture(t)
	elsewhere := filepath.Join(t.TempDir(), "victim.out")
	if err := os.WriteFile(elsewhere, []byte("out one"), 0o600); err != nil {
		t.Fatal(err)
	}
	setReviewerJobAge(t, now.Add(-10*24*time.Hour), elsewhere)
	r, _ := readJobReceipt(receipt)
	r.OutputPath = elsewhere
	if err := writeJobReceipt(receipt, r); err != nil {
		t.Fatal(err)
	}
	setReviewerJobAge(t, now.Add(-10*24*time.Hour), receipt)
	if err := archiveSettledReviewerJobsAt(scope, runtime, 1, now); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{receipt, output, elsewhere} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("file moved through a receipt with a foreign output path: %v", err)
		}
	}
}

// A job whose lock is held elsewhere is in use and is skipped, not moved.
func TestA24RetentionSkipsJobWhoseLockIsHeld(t *testing.T) {
	scope, runtime, receipt, output, now := settledReviewerJobFixture(t)
	saved := jobReceiptAcquireLock
	jobReceiptAcquireLock = func(string) (*osLock, bool) { return nil, false }
	defer func() { jobReceiptAcquireLock = saved }()
	if err := archiveSettledReviewerJobsAt(scope, runtime, 1, now); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{receipt, output} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("job under a held lock was moved: %v", err)
		}
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
