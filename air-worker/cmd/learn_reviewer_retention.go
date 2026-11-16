package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// With one job per reviewed event the receipts in .woody/jobs grow with every
// review. Policy (curator decision 16.11.2026): only when there are more than
// reviewerJobKeep reviewer receipts, the oldest settled ones are MOVED to
// .woody/jobs-archive/<yyyymm>/; nothing is deleted. A receipt is evidence
// that must stay where recovery looks for it while it is running, unsealed or
// mismatched, younger than reviewerJobMinAge, or while its event's review is
// not complete.
const (
	reviewerJobKeep   = 200
	reviewerJobMinAge = 7 * 24 * time.Hour
)

type reviewerJobArchiveTrace struct {
	Schema       string `json:"schema"`
	At           string `json:"at"`
	Receipt      string `json:"receipt"`
	From         string `json:"from"`
	To           string `json:"to"`
	OutputSHA256 string `json:"output_sha256"`
	Outcome      string `json:"outcome"`
}

func archiveSettledReviewerJobs(product, runtimeRoot string) error {
	return archiveSettledReviewerJobsAt(legacyScope(product), runtimeRoot, reviewerJobKeep, time.Now().UTC())
}

func archiveSettledReviewerJobsAt(scope sessionScope, runtimeRoot string, keep int, now time.Time) error {
	jobs := filepath.Join(scope.Root, ".woody", "jobs")
	paths, err := filepath.Glob(filepath.Join(jobs, "*learning-*learning-reviewer.receipt.json"))
	if err != nil {
		return err
	}
	type candidate struct {
		receipt string
		r       *jobReceipt
	}
	var reviewer []candidate
	for _, path := range paths {
		r, err := readJobReceipt(path)
		if err == nil && strings.HasPrefix(r.Step, "learning-") && r.Operation == "learning-reviewer" && r.Role == "learning-reviewer" {
			reviewer = append(reviewer, candidate{path, r})
		}
	}
	if len(reviewer) <= keep {
		return nil
	}
	complete, err := completedReviewEventSHAs(runtimeRoot)
	if err != nil {
		return err
	}
	var settled []candidate
	for _, c := range reviewer {
		if reviewerJobSettled(c.receipt, c.r, complete, now) {
			settled = append(settled, c)
		}
	}
	sort.Slice(settled, func(i, j int) bool { return settled[i].r.StartedAt.Before(settled[j].r.StartedAt) })
	excess := len(reviewer) - keep
	for _, c := range settled {
		if excess <= 0 {
			break
		}
		moved, err := archiveReviewerJobLocked(scope, runtimeRoot, c.receipt, complete, now)
		if err != nil {
			return err
		}
		if moved {
			excess--
		}
	}
	return nil
}

// reviewerJobSettled: terminal, sealed, output intact, its event's review
// complete, and both started and last written more than reviewerJobMinAge
// ago. The file age also covers the window in which runReceipted rereads a
// just-sealed receipt after releasing the job lock.
func reviewerJobSettled(receiptPath string, r *jobReceipt, complete map[string]bool, now time.Time) bool {
	if (r.Status != jobStatusDone && r.Status != jobStatusFailed) || now.Sub(r.StartedAt) < reviewerJobMinAge || r.OutputSHA256 == "" || !complete[r.ReviewTaskSHA256] {
		return false
	}
	for _, p := range []string{receiptPath, r.OutputPath} {
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() || now.Sub(st.ModTime()) < reviewerJobMinAge {
			return false
		}
	}
	out, err := readLearningBounded(r.OutputPath, 32<<20)
	return err == nil && learnSHA(out) == r.OutputSHA256
}

// The move runs under the same job lock runReceipted and recovery use, and the
// receipt is reread under it; a lock held elsewhere means the job is in use and
// it is skipped, not waited for.
func archiveReviewerJobLocked(scope sessionScope, runtimeRoot, receiptPath string, complete map[string]bool, now time.Time) (bool, error) {
	r, err := readJobReceipt(receiptPath)
	if err != nil {
		return false, nil
	}
	lock, ok := jobReceiptAcquireLock(scopedLockName(jobLockKind(r.Step, r.Operation), scope))
	if !ok {
		if lock != nil {
			lock.release()
		}
		return false, nil
	}
	defer lock.release()
	r, err = readJobReceipt(receiptPath)
	if err != nil || !reviewerJobSettled(receiptPath, r, complete, now) {
		return false, nil
	}
	return true, archiveReviewerJob(scope.Root, runtimeRoot, receiptPath, r, now)
}

// completedReviewEventSHAs maps the event SHA the adapter uses as job identity
// (learnSHA of the re-marshalled event row) to events whose review index is
// phase=complete.
func completedReviewEventSHAs(runtimeRoot string) (map[string]bool, error) {
	file, err := os.Open(filepath.Join(runtimeRoot, "events.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return map[string]bool{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	done := map[string]bool{}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 256*1024)
	for scanner.Scan() {
		var event map[string]any
		if json.Unmarshal(scanner.Bytes(), &event) != nil || jsonString(event["schema"]) != "air.learning.event/v1" {
			continue
		}
		id := jsonString(event["event_id"])
		raw, err := os.ReadFile(filepath.Join(runtimeRoot, "reviews", id+".json"))
		if err != nil {
			continue
		}
		var review struct {
			Phase string `json:"phase"`
		}
		if json.Unmarshal(raw, &review) != nil || review.Phase != "complete" {
			continue
		}
		b, _ := json.Marshal(event)
		done[learnSHA(b)] = true
	}
	return done, scanner.Err()
}

// The trace line is written before the move and every attempt ends with its
// own outcome line. A destination that already exists is never replaced. The
// move counts only after the archived output reads back with its sealed SHA;
// otherwise both files are moved back, and a failed restore is reported with
// where each file actually is instead of being called restored.
func archiveReviewerJob(root, runtimeRoot, receiptPath string, r *jobReceipt, now time.Time) error {
	dir := filepath.Join(root, ".woody", "jobs-archive", now.Format("200601"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	toReceipt := filepath.Join(dir, filepath.Base(receiptPath))
	toOutput := filepath.Join(dir, filepath.Base(r.OutputPath))
	trace := reviewerJobArchiveTrace{Schema: "air-worker.reviewer-job-archive/v1", At: now.Format(time.RFC3339),
		Receipt: filepath.Base(receiptPath), From: filepath.Dir(receiptPath), To: dir, OutputSHA256: r.OutputSHA256, Outcome: "moving"}
	if err := appendReviewerJobArchiveTrace(runtimeRoot, trace); err != nil {
		return err
	}
	finish := func(outcome string, err error) error {
		trace.Outcome = outcome
		if terr := appendReviewerJobArchiveTrace(runtimeRoot, trace); err == nil {
			err = terr
		}
		return err
	}
	for _, dst := range []string{toReceipt, toOutput} {
		if _, err := os.Lstat(dst); !errors.Is(err, os.ErrNotExist) {
			return finish("skipped-destination-exists", fmt.Errorf("reviewer job archive already holds %s", filepath.Base(dst)))
		}
	}
	if err := os.Rename(r.OutputPath, toOutput); err != nil {
		return finish("skipped-move-failed", err)
	}
	if err := os.Rename(receiptPath, toReceipt); err != nil {
		if rerr := os.Rename(toOutput, r.OutputPath); rerr != nil {
			return finish("restore-failed: output in archive, receipt in jobs", fmt.Errorf("%v; restore failed: %v", err, rerr))
		}
		return finish("restored-after-move-failed", err)
	}
	out, err := readLearningBounded(toOutput, 32<<20)
	if _, rerr := os.Lstat(toReceipt); err == nil && rerr == nil && learnSHA(out) == r.OutputSHA256 {
		return finish("archived", nil)
	}
	backReceipt := os.Rename(toReceipt, receiptPath)
	backOutput := os.Rename(toOutput, r.OutputPath)
	if backReceipt != nil || backOutput != nil {
		return finish(fmt.Sprintf("restore-failed: receipt back=%t, output back=%t", backReceipt == nil, backOutput == nil),
			errors.New("reviewer job archive readback failed and restore failed"))
	}
	return finish("readback-failed-restored", errors.New("reviewer job archive readback failed; files restored"))
}

func appendReviewerJobArchiveTrace(runtimeRoot string, trace reviewerJobArchiveTrace) error {
	line, err := json.Marshal(trace)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(runtimeRoot, "reviewer-job-archive.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
