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
	rt, err := os.OpenRoot(scope.Root)
	if err != nil {
		return err
	}
	err = reconcileReviewerJobArchiveTrace(rt, runtimeRoot, now)
	rt.Close()
	if err != nil {
		return err
	}
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
	// Like recovery, act only on a receipt whose own fields name this job's
	// canonical files: a modified receipt must not steer a move elsewhere.
	canonReceipt, canonOutput := jobReceiptPaths(scope, r.Step, r.Operation)
	if !sameLearningPath(r.Product, scope.Root) || !sameLearningPath(receiptPath, canonReceipt) || !sameLearningPath(r.OutputPath, canonOutput) {
		return false, nil
	}
	return true, archiveReviewerJob(scope.Root, runtimeRoot, receiptPath, r, now)
}

// All archive file operations go through an os.Root opened at the product
// root: path resolution can then never leave it, even if a level is swapped
// for a symlink or junction between a check and its use.

// mkdirPlain creates one archive level or accepts an existing ordinary
// directory; anything else is refused instead of followed.
func mkdirPlain(rt *os.Root, rel string) error {
	st, err := rt.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return rt.Mkdir(rel, 0o700)
	}
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
		return fmt.Errorf("reviewer job archive path is not a plain directory: %s", rel)
	}
	return nil
}

// moveNoReplace links the file at dst and then drops the old name, as
// publishNewPlanNode does: os.Rename on Windows replaces an existing target,
// a link never does. If dropping the old name fails both names remain; no
// data is lost.
func moveNoReplace(rt *os.Root, src, dst string) error {
	if err := rt.Link(src, dst); err != nil {
		return err
	}
	return rt.Remove(src)
}

func outputNameForReceipt(receipt string) string {
	return strings.TrimSuffix(receipt, ".receipt.json") + ".out"
}

// reconcileReviewerJobArchiveTrace gives every attempt left at "moving" (for
// example after a failed final trace append) a terminal line that states
// where its two files actually are. It only reads file metadata.
func reconcileReviewerJobArchiveTrace(rt *os.Root, runtimeRoot string, now time.Time) error {
	raw, err := os.ReadFile(filepath.Join(runtimeRoot, "reviewer-job-archive.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	last := map[string]reviewerJobArchiveTrace{}
	var order []string
	for _, line := range strings.Split(string(raw), "\n") {
		var t reviewerJobArchiveTrace
		if json.Unmarshal([]byte(line), &t) != nil || t.Receipt == "" {
			continue
		}
		key := t.To + "|" + t.Receipt
		if _, seen := last[key]; !seen {
			order = append(order, key)
		}
		last[key] = t
	}
	// "" means the location cannot be known (access error, path escaping the
	// root): such an attempt keeps "moving" and is retried on the next run
	// instead of getting a false terminal line.
	present := func(rel string) (bool, bool) {
		_, err := rt.Lstat(rel)
		if err == nil {
			return true, true
		}
		return false, errors.Is(err, os.ErrNotExist)
	}
	where := func(name, archived string) string {
		inJobs, okJobs := present(filepath.Join(".woody", "jobs", name))
		inArch, okArch := present(filepath.Join(archived, name))
		switch {
		case !okJobs || !okArch:
			return ""
		case inJobs && inArch:
			return "both"
		case inJobs:
			return "jobs"
		case inArch:
			return "archive"
		}
		return "missing"
	}
	for _, key := range order {
		t := last[key]
		if t.Outcome != "moving" {
			continue
		}
		archived := filepath.Join(".woody", "jobs-archive", filepath.Base(t.To))
		receiptAt, outputAt := where(t.Receipt, archived), where(outputNameForReceipt(t.Receipt), archived)
		if receiptAt == "" || outputAt == "" {
			continue
		}
		t.At = now.Format(time.RFC3339)
		t.Outcome = fmt.Sprintf("reconciled: receipt in %s, output in %s", receiptAt, outputAt)
		if err := appendReviewerJobArchiveTrace(runtimeRoot, t); err != nil {
			return err
		}
	}
	return nil
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
	rt, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer rt.Close()
	archRel := filepath.Join(".woody", "jobs-archive")
	dirRel := filepath.Join(archRel, now.Format("200601"))
	if err := mkdirPlain(rt, archRel); err != nil {
		return err
	}
	if err := mkdirPlain(rt, dirRel); err != nil {
		return err
	}
	// The caller verified both paths are this job's canonical files under
	// .woody/jobs, so their base names locate them inside the root.
	fromReceipt := filepath.Join(".woody", "jobs", filepath.Base(receiptPath))
	fromOutput := filepath.Join(".woody", "jobs", filepath.Base(r.OutputPath))
	toReceipt := filepath.Join(dirRel, filepath.Base(receiptPath))
	toOutput := filepath.Join(dirRel, filepath.Base(r.OutputPath))
	trace := reviewerJobArchiveTrace{Schema: "air-worker.reviewer-job-archive/v1", At: now.Format(time.RFC3339),
		Receipt: filepath.Base(receiptPath), From: filepath.Dir(receiptPath), To: filepath.Join(root, dirRel), OutputSHA256: r.OutputSHA256, Outcome: "moving"}
	if err := appendReviewerJobArchiveTrace(runtimeRoot, trace); err != nil {
		return err
	}
	// A failed final append leaves "moving"; the next run reconciles it from
	// the actual file locations (reconcileReviewerJobArchiveTrace).
	finish := func(outcome string, err error) error {
		trace.Outcome = outcome
		if terr := appendReviewerJobArchiveTrace(runtimeRoot, trace); err == nil {
			err = terr
		}
		return err
	}
	for _, dst := range []string{toReceipt, toOutput} {
		if _, err := rt.Lstat(dst); !errors.Is(err, os.ErrNotExist) {
			return finish("skipped-destination-exists", fmt.Errorf("reviewer job archive already holds %s", filepath.Base(dst)))
		}
	}
	if err := moveNoReplace(rt, fromOutput, toOutput); err != nil {
		return finish("skipped-move-failed", err)
	}
	if err := moveNoReplace(rt, fromReceipt, toReceipt); err != nil {
		if rerr := moveNoReplace(rt, toOutput, fromOutput); rerr != nil {
			return finish("restore-failed: output in archive, receipt in jobs", fmt.Errorf("%v; restore failed: %v", err, rerr))
		}
		return finish("restored-after-move-failed", err)
	}
	var out []byte
	st, err := rt.Lstat(toOutput)
	if err == nil && st.Mode().IsRegular() && st.Size() <= 32<<20 {
		out, err = rt.ReadFile(toOutput)
	}
	if _, rerr := rt.Lstat(toReceipt); err == nil && rerr == nil && out != nil && learnSHA(out) == r.OutputSHA256 {
		return finish("archived", nil)
	}
	backReceipt := moveNoReplace(rt, toReceipt, fromReceipt)
	backOutput := moveNoReplace(rt, toOutput, fromOutput)
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
