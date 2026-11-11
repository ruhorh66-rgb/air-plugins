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
	if err != nil || rejectDuplicateJSONKeys(raw) != nil {
		return false
	}
	var r codingWorkspaceReservation
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&r) != nil || dec.Decode(new(any)) != io.EOF || r.Schema != "air-worker.coding-workspace-reservation/v1" || r.Step == "" || r.Principal == "" || r.SessionKey == "" {
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

func sameCodingPath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// codingWorkspaceLimit bounds retained attempts, including unknown legacy attempts.
const codingWorkspaceLimit = 8

type codingWorkspaceReservation struct {
	Schema      string    `json:"schema"`
	Step        string    `json:"step"`
	Principal   string    `json:"principal"`
	SessionKey  string    `json:"session_key"`
	SourceHead  string    `json:"source_head"`
	TaskSHA256  string    `json:"task_sha256"`
	WorkerPaths [2]string `json:"worker_paths"`
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
	if runtime.GOOS == "windows" {
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
	} else {
		if err := attempts.Mkdir(".reserve-lock", 0700); err != nil {
			return "", fmt.Errorf("coding workspace reservation is busy or interrupted: %w", err)
		}
		defer attempts.Remove(".reserve-lock")
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
	for _, entry := range entries {
		if entry.Name() == ".reserve-lock" {
			continue
		}
		candidate := filepath.Join(filepath.Dir(base), entry.Name())
		if codingWorkspaceReleased(repoTop, productRel, candidate) {
			continue
		}
		finalizeRetainedCodingWorkspace(repoTop, productRel, candidate)
		if !codingWorkspaceReleased(repoTop, productRel, candidate) {
			held = append(held, entry.Name())
		}
	}
	if len(held) >= codingWorkspaceLimit {
		return "", fmt.Errorf("coding workspace limit %d reached; retained attempts: %s", codingWorkspaceLimit, strings.Join(held, ", "))
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
