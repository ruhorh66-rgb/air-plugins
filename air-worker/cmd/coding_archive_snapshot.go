package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type codingArchiveEntry struct {
	Name       string `json:"name"`
	SHA256     string `json:"sha256"`
	Bytes      int    `json:"bytes"`
	SourcePath string `json:"source_path,omitempty"`
}
type codingArchiveSeal struct {
	Schema         string `json:"schema"`
	ManifestSHA256 string `json:"manifest_sha256"`
	SourceHead     string `json:"source_head"`
	TaskSHA256     string `json:"task_sha256"`
	AcceptedCommit string `json:"accepted_commit"`
	AckSHA256      string `json:"ack_sha256"`
	HandoffSHA256  string `json:"handoff_sha256"`
}

type codingArchiveManifest struct {
	Schema         string               `json:"schema"`
	State          string               `json:"state"`
	SourceHead     string               `json:"source_head"`
	AcceptedCommit string               `json:"accepted_commit"`
	TaskSHA256     string               `json:"task_sha256"`
	AckSHA256      string               `json:"ack_sha256"`
	HandoffSHA256  string               `json:"handoff_sha256"`
	Entries        []codingArchiveEntry `json:"entries"`
}

func readOwnedCodingArchiveInput(root, path string, limit int64) ([]byte, error) {
	if jobOwnedEvidencePath(root, path) != nil {
		return nil, errors.New("archive input is not an owned regular file")
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() < 0 || before.Size() > limit {
		return nil, errors.New("archive input size or type invalid")
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, errors.New("archive input outside owner")
	}
	owner, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer owner.Close()
	f, err := owner.Open(rel)
	if err != nil {
		return nil, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, limit+1))
	opened, statErr := f.Stat()
	closeErr := f.Close()
	after, afterErr := os.Lstat(path)
	if readErr != nil || statErr != nil || closeErr != nil || afterErr != nil || !os.SameFile(before, opened) || !os.SameFile(before, after) || int64(len(data)) != before.Size() || int64(len(data)) > limit {
		return nil, errors.New("archive input changed during read")
	}
	return data, nil
}

func writeCodingArchiveEntry(root *os.Root, name string, data []byte) (codingArchiveEntry, error) {
	if !filepath.IsLocal(name) || filepath.Base(name) != name {
		return codingArchiveEntry{}, errors.New("invalid archive entry name")
	}
	f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return codingArchiveEntry{}, err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return codingArchiveEntry{}, err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return codingArchiveEntry{}, err
	}
	if err := f.Close(); err != nil {
		return codingArchiveEntry{}, err
	}
	readback, err := root.Open(name)
	if err != nil {
		return codingArchiveEntry{}, err
	}
	got, readErr := io.ReadAll(io.LimitReader(readback, int64(len(data))+1))
	closeErr := readback.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(got, data) {
		return codingArchiveEntry{}, errors.New("archive entry readback failed")
	}
	return codingArchiveEntry{Name: name, SHA256: shaCodingBytes(data), Bytes: len(data)}, nil
}

// prepareCodingFinalization refuses historical receipts without a verified
// process tree; it prepares evidence but does not itself remove a checkout.
func prepareCodingFinalization(root string, scope sessionScope, step, task string) (string, string, error) {
	doc, _, err := verifiedCodingWorkerEvidence(root, scope, step, task)
	if err != nil || len(doc.Workers) != 2 {
		return "", "", errors.New("worker evidence is not verified")
	}
	for _, worker := range doc.Workers {
		receipt, err := readJobReceipt(worker.Receipt)
		if err != nil || receipt.Status != jobStatusDone || receipt.Role != "coding-subagent/worker" || !receipt.ProcessTreeVerified {
			return "", "", errors.New("worker process tree termination is not verified")
		}
	}
	return archiveCodingEvidence(root, scope, step, task)
}

// archiveCodingEvidence creates evidence only; it never removes a worktree.
// Unknown extra, staged, untracked, ignored or nonregular content is retained.
func archiveCodingEvidence(root string, scope sessionScope, step, task string) (string, string, error) {
	ack, accepted, err := verifyHistoricalCodingAck(root, scope, step, task)
	if err != nil {
		return "", "", err
	}
	doc, _, err := verifiedCodingWorkerEvidence(root, scope, step, task)
	if err != nil || len(doc.Workers) != 2 {
		return "", "", errors.New("workers no longer verified")
	}
	base := filepath.Dir(doc.Report)
	if jobOwnedEvidencePath(base, doc.Report) != nil {
		return "", "", errors.New("unsafe handoff")
	}
	repoTop, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	owner, err := os.OpenRoot(base)
	if err != nil {
		return "", "", err
	}
	defer owner.Close()
	if err := owner.Mkdir("archive-candidate", 0700); err != nil {
		return "", "", fmt.Errorf("archive candidate exists or cannot be created: %w", err)
	}
	archive, err := owner.OpenRoot("archive-candidate")
	if err != nil {
		return "", "", err
	}
	defer archive.Close()
	path := filepath.Join(base, "archive-candidate")
	manifest := codingArchiveManifest{Schema: "air-worker.coding-archive/v1", State: "SNAPSHOT_ONLY_NOT_REMOVABLE", SourceHead: doc.SourceHead, AcceptedCommit: accepted, TaskSHA256: ack.TaskSHA256, AckSHA256: ack.AckSHA256, HandoffSHA256: ack.HandoffSHA256}
	add := func(name string, data []byte) error {
		e, err := writeCodingArchiveEntry(archive, name, data)
		if err == nil {
			manifest.Entries = append(manifest.Entries, e)
		}
		return err
	}
	addOwned := func(name, ownerRoot, input string, limit int64) error {
		data, err := readOwnedCodingArchiveInput(ownerRoot, input, limit)
		if err != nil {
			return err
		}
		return add(name, data)
	}
	if err := add("task.input", []byte(task)); err != nil {
		return "", "", err
	}
	if err := addOwned("handoff.json", base, doc.Report, 1<<20); err != nil {
		return "", "", err
	}
	if err := addOwned("ack.json", base, filepath.Join(base, "coordinator-integration.json"), 8192); err != nil {
		return "", "", err
	}
	parsed, err := parseCodingMicrosteps(task)
	if err != nil {
		return "", "", err
	}
	initial := make([][]byte, 2)
	for i, w := range doc.Workers {
		workerTop, err := gitCodingOutput(w.Product, "rev-parse", "--show-toplevel")
		if err != nil {
			return "", "", err
		}
		rel, err := filepath.Rel(workerTop, w.Product)
		if err != nil || (rel != "." && !isCodingProductPath(rel)) {
			return "", "", errors.New("worker product outside checkout")
		}
		if len(w.Changed) != 1 {
			return "", "", errors.New("unsupported worker changed-file scope")
		}
		out, err := exec.Command("git", "-C", workerTop, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored").Output()
		if err != nil {
			return "", "", err
		}
		trackedStatus := " M " + w.Changed[0]
		woodyPrefix := filepath.ToSlash(filepath.Join(rel, ".woody")) + "/"
		parts := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		trackedCount := 0
		for _, part := range parts {
			if part == trackedStatus {
				trackedCount++
				continue
			}
			if len(part) > 3 && (strings.HasPrefix(part, "?? ") || strings.HasPrefix(part, "!! ")) && strings.HasPrefix(part[3:], woodyPrefix) {
				continue
			}
			return "", "", errors.New("worker contains unsupported or unverified data; archive retained without removal")
		}
		if trackedCount != 1 {
			return "", "", errors.New("worker tracked state is not verified")
		}
		initial[i] = out
		prefix := fmt.Sprintf("worker-%d.", i+1)
		extraPaths, err := codingArchiveExtraPaths(workerTop, rel)
		if err != nil {
			return "", "", err
		}
		var extraBytes int64
		for _, name := range extraPaths {
			data, err := readOwnedCodingArchiveInput(workerTop, filepath.Join(workerTop, filepath.FromSlash(name)), 32<<20)
			if err != nil {
				return "", "", err
			}
			extraBytes += int64(len(data))
			if extraBytes > 128<<20 {
				return "", "", errors.New("worker extra data exceeds archive limit")
			}
			entry, err := writeCodingArchiveEntry(archive, codingArchiveExtraName(i+1, name), data)
			if err != nil {
				return "", "", err
			}
			entry.SourcePath = name
			manifest.Entries = append(manifest.Entries, entry)
		}
		if err := add(prefix+"status", out); err != nil {
			return "", "", err
		}
		changed := filepath.Join(w.Product, filepath.FromSlash(w.File))
		data, err := readOwnedCodingArchiveInput(w.Product, changed, 2<<20)
		if err != nil {
			return "", "", err
		}
		blob, err := exec.Command("git", "-C", repoTop, "show", accepted+":"+w.Changed[0]).Output()
		if err != nil || !bytes.Equal(data, blob) {
			return "", "", errors.New("worker file differs from accepted commit blob")
		}
		if err := add(prefix+"changed", data); err != nil {
			return "", "", err
		}
		if err := addOwned(prefix+"task", w.Product, filepath.Join(w.Product, ".woody", "TASK-CODE-WORKER.md"), 48<<10); err != nil {
			return "", "", err
		}
		if err := addOwned(prefix+"patch", base, w.Patch, 256<<10); err != nil {
			return "", "", err
		}
		receipt, err := readJobReceipt(w.Receipt)
		if err != nil {
			return "", "", err
		}
		version := codingReceiptVersion(receipt.Operation, ack.TaskSHA256, scope.ID.Principal)
		if version == "" {
			return "", "", errors.New("unmeasured worker task")
		}
		fullTask := codingWorkerTaskVersion(parsed, parsed.Workers[i], ack.TaskSHA256, doc.SourceHead, version)
		taskPath := filepath.Join(w.Product, ".woody", "TASK-CODE-WORKER.md")
		workerTask, err := readOwnedCodingArchiveInput(w.Product, taskPath, 48<<10)
		if err != nil || string(workerTask) != fullTask {
			return "", "", errors.New("worker task changed")
		}
		stdin := []byte(ponytailPrompt(codingWorkerPrompt(taskPath, fullTask)))
		if shaCodingBytes(stdin) != receipt.StdinSHA256 {
			return "", "", errors.New("worker stdin does not match receipt")
		}
		if err := add(prefix+"stdin", stdin); err != nil {
			return "", "", err
		}
		if err := addOwned(prefix+"receipt", scope.Root, w.Receipt, 1<<20); err != nil {
			return "", "", err
		}
		if err := addOwned(prefix+"output", scope.Root, receipt.OutputPath, 32<<20); err != nil {
			return "", "", err
		}
	}
	again, commit, err := verifyHistoricalCodingAck(root, scope, step, task)
	if err != nil || commit != accepted || again.AckSHA256 != ack.AckSHA256 {
		return "", "", errors.New("evidence changed while archive was prepared")
	}
	for i, w := range doc.Workers {
		top, err := gitCodingOutput(w.Product, "rev-parse", "--show-toplevel")
		if err != nil {
			return "", "", err
		}
		status, err := exec.Command("git", "-C", top, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignored").Output()
		if err != nil || !bytes.Equal(status, initial[i]) {
			return "", "", errors.New("worker state changed while archive was prepared")
		}
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", "", err
	}
	data = append(data, '\n')
	if _, err := writeCodingArchiveEntry(archive, "manifest.json", data); err != nil {
		return "", "", err
	}
	manifestSHA := shaCodingBytes(data)
	verified, err := verifyCodingArchive(path, manifestSHA, repoTop)
	if err != nil {
		return "", "", fmt.Errorf("archive verification failed before sealing: %w", err)
	}
	if verified.AcceptedCommit != accepted {
		return "", "", errors.New("archive accepted commit changed before sealing")
	}
	seal := codingArchiveSeal{Schema: "air-worker.coding-archive-seal/v1", ManifestSHA256: manifestSHA, SourceHead: doc.SourceHead, TaskSHA256: ack.TaskSHA256, AcceptedCommit: accepted, AckSHA256: ack.AckSHA256, HandoffSHA256: ack.HandoffSHA256}
	sealRaw, err := json.MarshalIndent(seal, "", "  ")
	if err != nil {
		return "", "", err
	}
	if _, err := writeCodingArchiveEntry(owner, "archive-seal.json", append(sealRaw, '\n')); err != nil {
		return "", "", err
	}
	return path, manifestSHA, nil
}

func verifyCodingArchive(path, expectedManifestSHA, repoTop string) (codingArchiveManifest, error) {
	var m codingArchiveManifest
	if len(expectedManifestSHA) != 64 {
		return m, errors.New("external archive manifest SHA is required")
	}
	raw, err := readOwnedCodingArchiveInput(path, filepath.Join(path, "manifest.json"), 1<<20)
	if err != nil || shaCodingBytes(raw) != expectedManifestSHA || rejectDuplicateJSONKeys(raw) != nil {
		return m, errors.New("archive manifest mismatch")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&m) != nil || dec.Decode(new(any)) != io.EOF || m.Schema != "air-worker.coding-archive/v1" || m.State != "SNAPSHOT_ONLY_NOT_REMOVABLE" || len(m.Entries) < 17 || len(m.Entries) > 4113 || !operationalSHA.MatchString(m.SourceHead) || !operationalSHA.MatchString(m.AcceptedCommit) {
		return m, errors.New("archive manifest invalid")
	}
	allowed := map[string]bool{"task.input": true, "handoff.json": true, "ack.json": true}
	for i := 1; i <= 2; i++ {
		for _, suffix := range []string{"status", "changed", "task", "patch", "stdin", "receipt", "output"} {
			allowed[fmt.Sprintf("worker-%d.%s", i, suffix)] = true
		}
	}
	captured := map[string][]byte{}
	seen := map[string]bool{}
	var extraTotal int64
	for _, e := range m.Entries {
		validName := allowed[e.Name] && e.SourcePath == ""
		if !validName && e.SourcePath != "" && filepath.IsLocal(filepath.FromSlash(e.SourcePath)) {
			validName = e.Name == codingArchiveExtraName(1, e.SourcePath) || e.Name == codingArchiveExtraName(2, e.SourcePath)
		}
		if !validName || seen[e.Name] || e.Bytes < 0 || e.Bytes > 32<<20 || len(e.SHA256) != 64 {
			return m, errors.New("archive entry invalid")
		}
		if e.SourcePath != "" {
			extraTotal += int64(e.Bytes)
			if extraTotal > 256<<20 {
				return m, errors.New("archive extra data exceeds limit")
			}
		}
		data, err := readOwnedCodingArchiveInput(path, filepath.Join(path, e.Name), 32<<20)
		if err != nil || len(data) != e.Bytes || shaCodingBytes(data) != e.SHA256 {
			return m, errors.New("archive entry mismatch")
		}
		captured[e.Name] = data
		seen[e.Name] = true
	}
	for name := range allowed {
		if !seen[name] {
			return m, errors.New("archive required entry missing")
		}
	}
	if shaCodingBytes(captured["task.input"]) != m.TaskSHA256 || shaCodingBytes(captured["handoff.json"]) != m.HandoffSHA256 || shaCodingBytes(captured["ack.json"]) != m.AckSHA256 {
		return m, errors.New("archive binding mismatch")
	}
	var doc codingWorkersHandoff
	if json.Unmarshal(captured["handoff.json"], &doc) != nil || doc.SourceHead != m.SourceHead || doc.TaskSHA256 != m.TaskSHA256 || doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" || len(doc.Workers) != 2 {
		return m, errors.New("archive handoff invalid")
	}
	for _, entry := range m.Entries {
		if entry.SourcePath == "" {
			continue
		}
		worker := 1
		if entry.Name == codingArchiveExtraName(2, entry.SourcePath) {
			worker = 2
		}
		workerTop := filepath.Join(filepath.Dir(doc.Report), fmt.Sprintf("worker-%d", worker))
		rel, err := filepath.Rel(workerTop, doc.Workers[worker-1].Product)
		if err != nil || (rel != "." && !isCodingProductPath(rel)) || !strings.HasPrefix(entry.SourcePath, filepath.ToSlash(filepath.Join(rel, ".woody"))+"/") {
			return m, errors.New("archive extra file is outside its worker")
		}
	}
	if rejectDuplicateJSONKeys(captured["ack.json"]) != nil {
		return m, errors.New("archive acknowledgement invalid")
	}
	var ack codingIntegrationAck
	ad := json.NewDecoder(bytes.NewReader(captured["ack.json"]))
	ad.DisallowUnknownFields()
	if ad.Decode(&ack) != nil || ad.Decode(new(any)) != io.EOF || ack.Schema != "air-worker.coordinator-integration/v1" || ack.Status != "ACK_RECORDED" || ack.SourceHead != m.SourceHead || ack.TaskSHA256 != m.TaskSHA256 || ack.HandoffSHA256 != m.HandoffSHA256 {
		return m, errors.New("archive acknowledgement mismatch")
	}
	parsed, err := parseCodingMicrosteps(string(captured["task.input"]))
	if err != nil {
		return m, errors.New("archived task is invalid")
	}
	names, err := exec.Command("git", "-C", repoTop, "diff", "--name-only", "-z", m.SourceHead, m.AcceptedCommit, "--").Output()
	if err != nil {
		return m, err
	}
	changed := strings.Split(strings.TrimSuffix(string(names), "\x00"), "\x00")
	if len(changed) != 2 || changed[0] == changed[1] {
		return m, errors.New("archive accepted commit scope mismatch")
	}
	expectedNames := map[string]bool{}
	for _, w := range doc.Workers {
		if len(w.Changed) != 1 || !isCodingProductPath(w.Changed[0]) {
			return m, errors.New("archive changed-file identity invalid")
		}
		expectedNames[w.Changed[0]] = true
	}
	if len(expectedNames) != 2 || !expectedNames[changed[0]] || !expectedNames[changed[1]] {
		return m, errors.New("archive accepted commit scope mismatch")
	}
	parents, err := gitCodingOutput(repoTop, "rev-list", "--parents", "-n", "1", m.AcceptedCommit)
	if err != nil || parents != m.AcceptedCommit+" "+m.SourceHead {
		return m, errors.New("archive accepted commit parent mismatch")
	}
	attemptStep := ""
	for i, w := range doc.Workers {
		prefix := fmt.Sprintf("worker-%d.", i+1)
		microRaw, err := json.Marshal(parsed.Workers[i])
		if err != nil {
			return m, err
		}
		if w.Status != "CODE_WRITTEN_IN_OWN_WORKTREE" || w.Index != i+1 || w.File != parsed.Workers[i].File || w.DoneWhen != parsed.Workers[i].DoneWhen || w.MicrostepSHA256 != shaCodingBytes(microRaw) || len(w.Changed) != 1 || ack.JobIDs[i] != w.JobID || ack.PatchSHA256[i] != w.PatchSHA256 || shaCodingBytes(captured[prefix+"patch"]) != w.PatchSHA256 {
			return m, errors.New("archive worker binding mismatch")
		}
		var receipt jobReceipt
		if json.Unmarshal(captured[prefix+"receipt"], &receipt) != nil || receipt.JobID != w.JobID || receipt.SourceHead != m.SourceHead || receipt.Status != jobStatusDone || !receipt.ProcessStarted || receipt.ExitCode == nil || *receipt.ExitCode != 0 || receipt.OutputSHA256 != shaCodingBytes(captured[prefix+"output"]) || receipt.OutputBytes != int64(len(captured[prefix+"output"])) || receipt.StdinSHA256 == "" || receipt.StdinSHA256 != shaCodingBytes(captured[prefix+"stdin"]) {
			return m, errors.New("archive receipt or output mismatch")
		}
		diff, err := exec.Command("git", "-C", repoTop, "diff", "--no-ext-diff", "--binary", m.SourceHead, m.AcceptedCommit, "--", w.Changed[0]).Output()
		if err != nil || shaCodingBytes(diff) != w.PatchSHA256 {
			return m, errors.New("archive accepted patch mismatch")
		}
		version := codingReceiptVersion(receipt.Operation, m.TaskSHA256, ack.Principal)
		originalStep := fmt.Sprintf("%s-coding-worker-%d", ack.Step, i+1)
		retryStep := fmt.Sprintf("%s-retry-1-coding-worker-%d", ack.Step, i+1)
		if i == 0 {
			if receipt.Step == originalStep {
				attemptStep = "original"
			} else if receipt.Step == retryStep {
				attemptStep = "retry"
			}
		}
		if version == "" || receipt.Principal != ack.Principal || receipt.Session != ack.SessionKey || (attemptStep == "original" && receipt.Step != originalStep) || (attemptStep == "retry" && receipt.Step != retryStep) || attemptStep == "" {
			return m, errors.New("archive receipt identity mismatch")
		}
		fullTask := codingWorkerTaskVersion(parsed, parsed.Workers[i], m.TaskSHA256, m.SourceHead, version)
		if string(captured[prefix+"task"]) != fullTask || !bytes.Contains(captured[prefix+"stdin"], []byte(codingWorkerPrompt(filepath.Join(w.Product, ".woody", "TASK-CODE-WORKER.md"), fullTask))) {
			return m, errors.New("archive worker task mismatch")
		}
		blob, err := exec.Command("git", "-C", repoTop, "show", m.AcceptedCommit+":"+w.Changed[0]).Output()
		if err != nil || !bytes.Equal(blob, captured[prefix+"changed"]) {
			return m, errors.New("archive restored file differs from accepted commit")
		}
	}
	return m, nil
}

// loadVerifiedCodingArchive never authorizes deletion. It checks the prepared
// snapshot against the still-owned ACK/handoff, task and reachable Git history.
func loadVerifiedCodingArchive(base, productRoot, taskSHA, sourceHead string) (codingArchiveManifest, error) {
	var empty codingArchiveManifest
	raw, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "archive-seal.json"), 8192)
	if err != nil || rejectDuplicateJSONKeys(raw) != nil {
		return empty, errors.New("archive seal missing or invalid")
	}
	var seal codingArchiveSeal
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&seal) != nil || decoder.Decode(new(any)) != io.EOF || seal.Schema != "air-worker.coding-archive-seal/v1" || seal.SourceHead != sourceHead || seal.TaskSHA256 != taskSHA || len(seal.ManifestSHA256) != 64 {
		return empty, errors.New("archive seal scope mismatch")
	}
	ack, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "coordinator-integration.json"), 8192)
	if err != nil || shaCodingBytes(ack) != seal.AckSHA256 {
		return empty, errors.New("original acknowledgement differs from archive seal")
	}
	handoff, err := readOwnedCodingArchiveInput(base, filepath.Join(base, "handoff.json"), 1<<20)
	if err != nil || shaCodingBytes(handoff) != seal.HandoffSHA256 {
		return empty, errors.New("original handoff differs from archive seal")
	}
	top, err := gitCodingOutput(productRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return empty, err
	}
	head, err := gitCodingOutput(top, "rev-parse", "HEAD")
	if err != nil {
		return empty, err
	}
	if err := exec.Command("git", "-C", top, "merge-base", "--is-ancestor", seal.AcceptedCommit, head).Run(); err != nil {
		return empty, errors.New("archive accepted commit is not reachable")
	}
	m, err := verifyCodingArchive(filepath.Join(base, "archive-candidate"), seal.ManifestSHA256, top)
	if err != nil || m.SourceHead != seal.SourceHead || m.TaskSHA256 != seal.TaskSHA256 || m.AcceptedCommit != seal.AcceptedCommit || m.AckSHA256 != seal.AckSHA256 || m.HandoffSHA256 != seal.HandoffSHA256 {
		return empty, errors.New("archive and seal differ")
	}
	after, err := gitCodingOutput(top, "rev-parse", "HEAD")
	if err != nil || after != head {
		return empty, errors.New("source changed during archive readback")
	}
	return m, nil
}

func codingCheckoutIsRemoved(repoTop, workerTop string) (bool, error) {
	if _, err := os.Lstat(workerTop); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	raw, err := exec.Command("git", "-C", repoTop, "worktree", "list", "--porcelain", "-z").Output()
	if err != nil {
		return false, err
	}
	for _, field := range strings.Split(string(raw), "\x00") {
		if strings.HasPrefix(field, "worktree ") && sameCodingPath(strings.TrimPrefix(field, "worktree "), workerTop) {
			return false, errors.New("missing checkout remains registered")
		}
	}
	return true, nil
}
