package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// An archive frees capacity only when its identity and both removals verify.
func codingWorkspaceReleased(repoTop, productRel, base string) bool {
	raw, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "reservation.json"), 8192)
	if err != nil {
		return false
	}
	r, err := decodeCodingWorkspaceReservation(raw)
	if err != nil {
		return false
	}
	product := filepath.Join(repoTop, productRel)
	scope := newScope(product, sessionIdentity{Principal: r.Principal, SessionKey: r.SessionKey})
	id := shaCodingBytes([]byte(repoTop + "|" + scope.key() + "|" + r.Step))[:18]
	if filepath.Base(base) != id {
		return false
	}
	for i := range r.WorkerPaths {
		if !sameCodingPath(r.WorkerPaths[i], filepath.Join(base, fmt.Sprintf("worker-%d", i+1), productRel)) {
			return false
		}
		removed, err := codingCheckoutIsRemoved(repoTop, filepath.Join(base, fmt.Sprintf("worker-%d", i+1)))
		if err != nil || !removed {
			return false
		}
	}
	task, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "task.json"), 1<<20)
	if err != nil || shaCodingBytes(task) != r.TaskSHA256 {
		return false
	}
	ackRaw, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "coordinator-integration.json"), 8192)
	if err != nil || rejectDuplicateJSONKeys(ackRaw) != nil {
		return false
	}
	var ack codingIntegrationAck
	if json.Unmarshal(ackRaw, &ack) != nil || ack.Principal != r.Principal || ack.SessionKey != r.SessionKey || (r.Step != ack.Step && r.Step != ack.Step+"-retry-1") {
		return false
	}
	manifest, err := loadVerifiedCodingArchive(base, product, r.TaskSHA256, r.SourceHead)
	return err == nil && codingArchivePinsMatch(repoTop, id, manifest.SourceHead, manifest.AcceptedCommit)
}

// A failed predecessor stays on disk. It can be accounted separately only
// after both failed process trees and the accepted, archived retry verify.
func codingRetainedFailedPredecessor(repoTop, productRel, base string) bool {
	raw, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "reservation.json"), 8192)
	if err != nil {
		return false
	}
	r, err := decodeCodingWorkspaceReservation(raw)
	if err != nil || strings.HasSuffix(r.Step, "-retry-1") {
		return false
	}
	root := filepath.Join(repoTop, productRel)
	scope := newScope(root, sessionIdentity{Principal: r.Principal, SessionKey: r.SessionKey})
	id := shaCodingBytes([]byte(repoTop + "|" + scope.key() + "|" + r.Step))[:18]
	if filepath.Base(base) != id {
		return false
	}
	task, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "task.json"), 48<<10)
	if err != nil || shaCodingBytes(task) != r.TaskSHA256 {
		return false
	}
	original, err := codingStatus(root, scope, r.Step, string(task))
	if err != nil || original.Status != jobStatusFailed || original.SourceHead != r.SourceHead || len(original.Workers) != 2 || !sameCodingPath(filepath.Dir(original.Report), base) {
		return false
	}
	for _, worker := range original.Workers {
		receipt, err := readJobReceipt(worker.Receipt)
		if err != nil || receipt == nil || receipt.Status != jobStatusFailed || receipt.Role != "coding-subagent/worker" || !receipt.ProcessTreeVerified || receipt.SourceHead != r.SourceHead {
			return false
		}
	}
	_, physical, err := effectiveCodingEvidence(root, scope, r.Step, string(task))
	if err != nil || physical != r.Step+"-retry-1" {
		return false
	}
	retryID := shaCodingBytes([]byte(repoTop + "|" + scope.key() + "|" + physical))[:18]
	retryBase := filepath.Join(filepath.Dir(base), retryID)
	return codingWorkspaceReleased(repoTop, productRel, retryBase)
}

func sameCodingPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// New attempts and unregistered legacy entries have separate observed bounds.
// Legacy data is never deleted merely to free capacity.
const (
	codingWorkspaceLimit       = 8
	codingWorkspaceLegacyLimit = 256
)

type codingWorkspaceReservation struct {
	Schema      string    `json:"schema"`
	Step        string    `json:"step"`
	Principal   string    `json:"principal"`
	SessionKey  string    `json:"session_key"`
	SourceHead  string    `json:"source_head"`
	TaskSHA256  string    `json:"task_sha256"`
	WorkerPaths [2]string `json:"worker_paths"`
}

func decodeCodingWorkspaceReservation(raw []byte) (codingWorkspaceReservation, error) {
	var r codingWorkspaceReservation
	if rejectDuplicateJSONKeys(raw) != nil {
		return r, errors.New("invalid workspace reservation JSON")
	}
	var fields map[string]json.RawMessage
	var paths []string
	if json.Unmarshal(raw, &fields) != nil || json.Unmarshal(fields["worker_paths"], &paths) != nil || len(paths) != 2 {
		return r, errors.New("workspace reservation must contain exactly two paths")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil || dec.Decode(new(any)) != io.EOF || r.Schema != "air-worker.coding-workspace-reservation/v1" || r.Step == "" || r.Principal == "" || r.SessionKey == "" {
		return r, errors.New("invalid workspace reservation")
	}
	return r, nil
}

func codingWorkspaceOpenDir(parent *os.Root, name string) (*os.Root, error) {
	if err := parent.Mkdir(name, 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	info, err := parent.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("coding workspace directory is not a real directory")
	}
	return parent.OpenRoot(name)
}

func codingWorkspaceWriteNew(root *os.Root, name string, data []byte) error {
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// reserveCodingWorkspace records ownership before git creates either checkout.
// A crash leaves the reservation or lock in place; neither is guessed safe to clear.
func reserveCodingWorkspace(repoTop, productRel string, scope sessionScope, step, sourceHead, task string) (string, error) {
	if runtime.GOOS != "windows" {
		return "", errors.New("coding workspace lifecycle is not supported on this platform")
	}
	repoParent := filepath.Dir(repoTop)
	resolved, err := filepath.EvalSymlinks(repoParent)
	if err != nil || !sameCodingPath(resolved, repoParent) {
		return "", errors.New("coding workspace parent is not canonical")
	}
	parent, err := os.OpenRoot(repoParent)
	if err != nil {
		return "", err
	}
	defer parent.Close()
	namespace, err := codingWorkspaceOpenDir(parent, ".airworker-coding-workspaces")
	if err != nil {
		return "", err
	}
	defer namespace.Close()
	attempts, err := codingWorkspaceOpenDir(namespace, filepath.Base(repoTop))
	if err != nil {
		return "", err
	}
	defer attempts.Close()
	lock, ok := acquireLock(lockName("coding-reserve", filepath.Join(repoParent, ".airworker-coding-workspaces", filepath.Base(repoTop))))
	if !ok || !validAutoSelfDeliveryLock(lock) {
		if lock != nil {
			lock.release()
		}
		return "", errors.New("coding workspace reservation is busy or its lock is unavailable")
	}
	defer lock.release()
	if _, err := attempts.Lstat(".reserve-lock"); err == nil {
		return "", errors.New("coding workspace reservation is busy or interrupted")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	id := shaCodingBytes([]byte(repoTop + "|" + scope.key() + "|" + step))[:18]
	base := filepath.Join(repoParent, ".airworker-coding-workspaces", filepath.Base(repoTop), id)
	if _, err := attempts.Lstat(id); err == nil {
		return "", fmt.Errorf("worker workspace %s already exists; use receipt readback, never rerun", base)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	dir, err := attempts.Open(".")
	if err != nil {
		return "", err
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	held := make([]string, 0)
	legacy := make([]string, 0)
	retainedCandidates := make([]string, 0)
	for _, entry := range entries {
		if entry.Name() == ".reserve-lock" {
			continue
		}
		candidate := filepath.Join(filepath.Dir(base), entry.Name())
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			legacy = append(legacy, entry.Name()+" (not an owned directory)")
			continue
		}
		owned, err := attempts.OpenRoot(entry.Name())
		if err != nil {
			held = append(held, entry.Name()+" (directory unavailable)")
			continue
		}
		_, statErr := owned.Lstat("reservation.json")
		_ = owned.Close()
		if os.IsNotExist(statErr) {
			legacy = append(legacy, entry.Name()+" (legacy or unregistered)")
			continue
		}
		if statErr != nil {
			held = append(held, entry.Name()+" (reservation unavailable)")
			continue
		}
		if codingWorkspaceReleased(repoTop, productRel, candidate) {
			continue
		}
		finalizeErr := finalizeRetainedCodingWorkspace(repoTop, productRel, candidate)
		if !codingWorkspaceReleased(repoTop, productRel, candidate) {
			reason := "not verified"
			if finalizeErr != nil {
				reason = finalizeErr.Error()
			}
			retainedCandidates = append(retainedCandidates, entry.Name()+"\x00"+reason)
		}
	}
	for _, item := range retainedCandidates {
		name, reason, _ := strings.Cut(item, "\x00")
		if codingRetainedFailedPredecessor(repoTop, productRel, filepath.Join(filepath.Dir(base), name)) {
			legacy = append(legacy, name+" (verified failed predecessor retained)")
		} else {
			held = append(held, name+" ("+reason+")")
		}
	}
	if len(legacy) >= codingWorkspaceLegacyLimit {
		return "", fmt.Errorf("legacy workspace limit %d reached; retained entries: %s", codingWorkspaceLegacyLimit, strings.Join(legacy, ", "))
	}
	if len(held) >= codingWorkspaceLimit {
		return "", fmt.Errorf("coding workspace limit %d reached; retained attempts: %s; legacy entries held separately: %d", codingWorkspaceLimit, strings.Join(held, ", "), len(legacy))
	}
	if err := attempts.Mkdir(id, 0700); err != nil {
		return "", err
	}
	reserved, err := attempts.OpenRoot(id)
	if err != nil {
		return "", err
	}
	defer reserved.Close()
	reservation := codingWorkspaceReservation{
		Schema: "air-worker.coding-workspace-reservation/v1", Step: step,
		Principal: scope.ID.Principal, SessionKey: scope.ID.SessionKey,
		SourceHead: sourceHead, TaskSHA256: shaCodingBytes([]byte(task)),
		WorkerPaths: [2]string{filepath.Join(base, "worker-1", productRel), filepath.Join(base, "worker-2", productRel)},
	}
	raw, err := json.MarshalIndent(reservation, "", "  ")
	if err != nil {
		return "", err
	}
	if err := codingWorkspaceWriteNew(reserved, "task.json", []byte(task)); err != nil {
		return "", err
	}
	if err := codingWorkspaceWriteNew(reserved, "reservation.json", append(raw, '\n')); err != nil {
		return "", err
	}
	return base, nil
}
