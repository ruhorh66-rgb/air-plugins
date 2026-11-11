package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Complete previously accepted owned attempts before counting the capacity.
func finalizeRetainedCodingWorkspace(top, productRel, base string) {
	raw, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "reservation.json"), 8192)
	if err != nil || rejectDuplicateJSONKeys(raw) != nil {
		return
	}
	var reservation codingWorkspaceReservation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&reservation) != nil || decoder.Decode(new(any)) != io.EOF || reservation.Schema != "air-worker.coding-workspace-reservation/v1" || reservation.Principal == "" || reservation.SessionKey == "" {
		return
	}
	task, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "task.json"), 48<<10)
	if err != nil || shaCodingBytes(task) != reservation.TaskSHA256 {
		return
	}
	root := filepath.Join(top, productRel)
	scope := newScope(root, sessionIdentity{Principal: reservation.Principal, SessionKey: reservation.SessionKey})
	step := strings.TrimSuffix(reservation.Step, "-retry-1")
	_ = finalizeCodingWorkspaces(root, scope, step, string(task))
}

func cmdCodingFinalize(argv []string) int {
	fs := flag.NewFlagSet("orchestrate code finalize", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	task := fs.String("task-file", "", "original task file")
	step := fs.String("step", "", "original coordinator step")
	principal := fs.String("principal", "", "original principal")
	session := fs.String("session-key", "", "original session")
	state := fs.String("state-dir", "", "native session state")
	asJSON := fs.Bool("json", false, "machine output")
	if fs.Parse(argv) != nil || fs.NArg() != 0 {
		return 2
	}
	err := func() error {
		root, cfg, err := loadExecutorConfig(*product, "")
		if err != nil {
			return err
		}
		if !cfg.Orchestration.Enabled || !cfg.Orchestration.RequireNativeSubagents || !cfg.Orchestration.RequireAnthropicReviewer || cfg.Orchestration.Subagents != 2 {
			return errors.New("managed two-worker configuration required")
		}
		scope, err := executorScopeForSession(root, *principal, *session, *state)
		if err != nil {
			return err
		}
		taskPath, err := taskFileInsideProduct(root, *task)
		if err != nil {
			return err
		}
		if *step != "coord-"+filepath.Base(taskPath) {
			return errors.New("step does not match original task file")
		}
		body, err := os.ReadFile(taskPath)
		if err != nil || len(body) == 0 || len(body) > 48*1024 {
			return errors.New("invalid original task")
		}
		return finalizeCodingWorkspaces(root, scope, *step, string(body))
	}()
	result := map[string]string{"status": "FINALIZED"}
	if err != nil {
		result = map[string]string{"status": "NOT_PROVEN", "error": err.Error()}
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(result)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "NOT_PROVEN: "+err.Error())
	} else {
		fmt.Println("FINALIZED")
	}
	if err != nil {
		return 2
	}
	return 0
}

var codingRemoveWorktree = func(top, worker string) error {
	_, err := gitCodingOutput(top, "worktree", "remove", "--force", worker)
	return err
}

func preserveIncompleteCodingArchive(owner *os.Root) error {
	info, err := owner.Lstat("archive-candidate")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("incomplete archive is not an owned directory")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	name := "archive-incomplete-" + hex.EncodeToString(id[:])
	if _, err := owner.Lstat(name); err == nil {
		return errors.New("incomplete archive destination exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := owner.Rename("archive-candidate", name); err != nil {
		return fmt.Errorf("incomplete archive retained: %w", err)
	}
	if _, err := owner.Lstat(name); err != nil {
		return errors.New("incomplete archive readback failed")
	}
	return nil
}

func codingArchivePinRef(id, kind string) string {
	return "refs/airworker/archive/" + id + "/" + kind
}

func codingArchivePinsMatch(top, id, source, accepted string) bool {
	for kind, sha := range map[string]string{"source": source, "accepted": accepted} {
		ref := codingArchivePinRef(id, kind)
		if exec.Command("git", "-C", top, "symbolic-ref", "-q", ref).Run() == nil {
			return false
		}
		got, err := gitCodingOutput(top, "rev-parse", "--verify", ref)
		if err != nil || got != sha {
			return false
		}
		kind, err := gitCodingOutput(top, "cat-file", "-t", ref)
		if err != nil || kind != "commit" {
			return false
		}
	}
	return true
}

func pinCodingArchiveCommits(top, id, source, accepted string) error {
	if len(id) != 18 || !operationalSHA.MatchString(source) || !operationalSHA.MatchString(accepted) {
		return errors.New("invalid archive commit identity")
	}
	if _, err := hex.DecodeString(id); err != nil {
		return errors.New("invalid archive identity")
	}
	for _, item := range []struct{ kind, sha string }{{"source", source}, {"accepted", accepted}} {
		ref := codingArchivePinRef(id, item.kind)
		if exec.Command("git", "-C", top, "symbolic-ref", "-q", ref).Run() == nil {
			return errors.New("archive ref is symbolic")
		}
		cmd := exec.Command("git", "-C", top, "rev-parse", "--verify", "--quiet", ref)
		out, err := cmd.Output()
		if err == nil {
			if strings.TrimSpace(string(out)) != item.sha {
				return errors.New("archive ref points to a different commit")
			}
		} else {
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 1 {
				return errors.New("archive ref cannot be inspected")
			}
			if _, err := gitCodingOutput(top, "update-ref", "--no-deref", ref, item.sha, strings.Repeat("0", len(item.sha))); err != nil {
				return err
			}
		}
	}
	if !codingArchivePinsMatch(top, id, source, accepted) {
		return errors.New("archive refs could not be verified")
	}
	return nil
}

// finalizeCodingWorkspaces applies only to newly reserved workers with a
// verified process-tree receipt. Unknown historical attempts remain intact.
func finalizeCodingWorkspaces(root string, scope sessionScope, step, task string) error {
	doc, physical, err := effectiveCodingEvidence(root, scope, step, task)
	if err != nil || len(doc.Workers) != 2 {
		return errors.New("worker evidence is not verified")
	}
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	top, err = filepath.Abs(top)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(top, root)
	if err != nil || (rel != "." && !isCodingProductPath(rel)) {
		return errors.New("product is outside its repository")
	}
	id := shaCodingBytes([]byte(top + "|" + scope.key() + "|" + physical))[:18]
	base := filepath.Join(filepath.Dir(top), ".airworker-coding-workspaces", filepath.Base(top), id)
	if !sameCodingPath(filepath.Dir(doc.Report), base) || filepath.Base(doc.Report) != "handoff.json" {
		return errors.New("worker namespace is not owned by this task")
	}
	info, err := os.Lstat(base)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("worker namespace is not a real directory")
	}
	resolved, err := filepath.EvalSymlinks(base)
	if err != nil || !sameCodingPath(base, resolved) {
		return errors.New("worker namespace is not canonical")
	}
	owner, err := os.OpenRoot(base)
	if err != nil {
		return err
	}
	defer owner.Close()
	if runtime.GOOS != "windows" {
		return errors.New("cross-process finalization lock is unavailable on this platform")
	}
	lock, ok := acquireLock(lockName("coding-finalize", base))
	if !ok || !validAutoSelfDeliveryLock(lock) {
		if lock != nil {
			lock.release()
		}
		return errors.New("finalization is busy or its lock is unavailable")
	}
	defer lock.release()
	raw, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "reservation.json"), 8192)
	if err != nil || rejectDuplicateJSONKeys(raw) != nil {
		return errors.New("workspace reservation is not verified")
	}
	var reservation codingWorkspaceReservation
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&reservation) != nil || decoder.Decode(new(any)) != io.EOF || reservation.Schema != "air-worker.coding-workspace-reservation/v1" || reservation.Step != physical || reservation.Principal != scope.ID.Principal || reservation.SessionKey != scope.ID.SessionKey || reservation.SourceHead != doc.SourceHead || reservation.TaskSHA256 != doc.TaskSHA256 {
		return errors.New("workspace reservation identity mismatch")
	}
	for i, path := range reservation.WorkerPaths {
		if !sameCodingPath(path, filepath.Join(base, fmt.Sprintf("worker-%d", i+1), rel)) {
			return errors.New("workspace reservation path mismatch")
		}
	}
	storedTask, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "task.json"), 1<<20)
	if err != nil || !bytes.Equal(storedTask, []byte(task)) {
		return errors.New("workspace reservation task mismatch")
	}
	if _, _, err := verifyHistoricalCodingAck(root, scope, step, task); err != nil {
		return err
	}
	for _, worker := range doc.Workers {
		receipt, err := readJobReceipt(worker.Receipt)
		if err != nil || receipt.Status != jobStatusDone || receipt.Role != "coding-subagent/worker" || !receipt.ProcessTreeVerified {
			return errors.New("worker process tree termination is not verified")
		}
	}
	if _, err := owner.Lstat("archive-seal.json"); os.IsNotExist(err) {
		if err := preserveIncompleteCodingArchive(owner); err != nil {
			return err
		}
		if _, _, err := prepareCodingFinalization(root, scope, step, task); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	manifest, err := loadVerifiedCodingArchive(base, root, doc.TaskSHA256, doc.SourceHead)
	if err != nil {
		return err
	}
	if err := pinCodingArchiveCommits(top, id, manifest.SourceHead, manifest.AcceptedCommit); err != nil {
		return err
	}
	for i, worker := range doc.Workers {
		workerTop := filepath.Join(base, fmt.Sprintf("worker-%d", i+1))
		removed, err := codingCheckoutIsRemoved(top, workerTop)
		if err != nil {
			return err
		}
		if removed {
			continue
		}
		manifest, err := loadVerifiedCodingArchive(base, root, doc.TaskSHA256, doc.SourceHead)
		if err != nil {
			return err
		}
		if _, _, err := verifyHistoricalCodingAck(root, scope, step, task); err != nil {
			return errors.New("acceptance evidence changed before removal")
		}
		receipt, err := readJobReceipt(worker.Receipt)
		if err != nil || receipt.Status != jobStatusDone || receipt.Role != "coding-subagent/worker" || !receipt.ProcessTreeVerified {
			return errors.New("worker process tree evidence changed before removal")
		}
		if !sameCodingPath(worker.Product, filepath.Join(workerTop, rel)) {
			return errors.New("worker product path mismatch")
		}
		workerInfo, err := os.Lstat(workerTop)
		if err != nil || !workerInfo.IsDir() || workerInfo.Mode()&os.ModeSymlink != 0 {
			return errors.New("worker checkout is not a real directory")
		}
		workerReal, err := filepath.EvalSymlinks(workerTop)
		if err != nil || !sameCodingPath(workerTop, workerReal) {
			return errors.New("worker checkout is not canonical")
		}
		actualTop, err := gitCodingOutput(workerTop, "rev-parse", "--show-toplevel")
		if err != nil || !sameCodingPath(actualTop, workerTop) {
			return errors.New("worker checkout identity mismatch")
		}
		parentGit, err := gitCodingOutput(top, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return err
		}
		workerGit, err := gitCodingOutput(workerTop, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil || !sameCodingPath(parentGit, workerGit) {
			return errors.New("worker checkout belongs to another repository")
		}
		head, err := gitCodingOutput(workerTop, "rev-parse", "HEAD")
		if err != nil || head != doc.SourceHead {
			return errors.New("worker checkout source changed")
		}
		prefix := fmt.Sprintf("worker-%d.", i+1)
		archive := filepath.Join(base, "archive-candidate")
		status, err := exec.Command("git", "-C", workerTop, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored").Output()
		archivedStatus, readErr := readOwnedCodingArchiveInput(archive, filepath.Join(archive, prefix+"status"), 1<<20)
		if err != nil || readErr != nil || !bytes.Equal(status, archivedStatus) {
			return errors.New("worker state changed after archive")
		}
		for _, item := range []struct {
			path, archive string
			max           int64
		}{
			{filepath.Join(worker.Product, filepath.FromSlash(worker.File)), prefix + "changed", 2 << 20},
			{filepath.Join(worker.Product, ".woody", "TASK-CODE-WORKER.md"), prefix + "task", 48 << 10},
		} {
			actual, err := readOwnedCodingArchiveInput(worker.Product, item.path, item.max)
			stored, storedErr := readOwnedCodingArchiveInput(archive, filepath.Join(archive, item.archive), item.max)
			if err != nil || storedErr != nil || !bytes.Equal(actual, stored) {
				return errors.New("worker content changed after archive")
			}
		}
		extraPaths, err := codingArchiveExtraPaths(workerTop, rel)
		if err != nil {
			return err
		}
		extras := make(map[string]codingArchiveEntry)
		for _, entry := range manifest.Entries {
			if entry.SourcePath != "" && entry.Name == codingArchiveExtraName(i+1, entry.SourcePath) {
				extras[entry.SourcePath] = entry
			}
		}
		if len(extraPaths) != len(extras) {
			return errors.New("worker extra file list changed after archive")
		}
		for _, name := range extraPaths {
			entry, ok := extras[name]
			if !ok {
				return errors.New("worker extra file was not archived")
			}
			actual, err := readOwnedCodingArchiveInput(workerTop, filepath.Join(workerTop, filepath.FromSlash(name)), 32<<20)
			stored, storedErr := readOwnedCodingArchiveInput(archive, filepath.Join(archive, entry.Name), 32<<20)
			if err != nil || storedErr != nil || !bytes.Equal(actual, stored) {
				return errors.New("worker extra file changed after archive")
			}
		}
		if !codingArchivePinsMatch(top, id, manifest.SourceHead, manifest.AcceptedCommit) {
			return errors.New("archive commit references changed before removal")
		}
		if err := codingRemoveWorktree(top, workerTop); err != nil {
			return fmt.Errorf("verified worktree retained: %w", err)
		}
		if removed, err := codingCheckoutIsRemoved(top, workerTop); err != nil || !removed {
			return errors.New("worktree removal not verified")
		}
	}
	if !codingWorkspaceReleased(top, rel, base) {
		return errors.New("archive or removal readback failed")
	}
	return nil
}
