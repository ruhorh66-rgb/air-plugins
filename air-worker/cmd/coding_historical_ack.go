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
	"reflect"
	"strings"
)

// verifyHistoricalCodingAck verifies recorded integration against a unique
// direct-child commit reachable from the current clean product HEAD. It does
// not declare that a human accepted the code or that worktrees may be removed.
func verifyHistoricalCodingAck(root string, scope sessionScope, step, task string) (codingIntegrationAck, string, error) {
	var empty codingIntegrationAck
	fail := func(s string) (codingIntegrationAck, string, error) { return empty, "", errors.New(s) }
	if step == "N-139_n136-native-operational-goal-selector-for-real-a" {
		return fail("historical N139 acknowledgement requires its own goal verification")
	}
	doc, expected, err := verifiedCodingWorkerEvidence(root, scope, step, task)
	if err != nil || len(expected) != 2 || len(doc.Workers) != 2 {
		return fail("original worker evidence is not verified")
	}
	dir := filepath.Dir(doc.Report)
	if filepath.Base(doc.Report) != "handoff.json" || jobOwnedEvidencePath(dir, doc.Report) != nil {
		return fail("unsafe original handoff")
	}
	ackPath := filepath.Join(dir, "coordinator-integration.json")
	if jobOwnedEvidencePath(dir, ackPath) != nil {
		return fail("unsafe original acknowledgement")
	}
	before, err := os.Lstat(ackPath)
	if err != nil || !before.Mode().IsRegular() || before.Size() == 0 || before.Size() > 8192 {
		return fail("invalid acknowledgement file")
	}
	owner, err := os.OpenRoot(dir)
	if err != nil {
		return empty, "", err
	}
	defer owner.Close()
	f, err := owner.Open("coordinator-integration.json")
	if err != nil {
		return empty, "", err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, 8193))
	opened, statErr := f.Stat()
	closeErr := f.Close()
	after, afterErr := os.Lstat(ackPath)
	if readErr != nil || statErr != nil || closeErr != nil || afterErr != nil || !os.SameFile(before, opened) || !os.SameFile(before, after) || len(raw) == 0 || len(raw) > 8192 || int64(len(raw)) != before.Size() || rejectDuplicateJSONKeys(raw) != nil {
		return fail("acknowledgement identity or bytes changed")
	}
	var ack codingIntegrationAck
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ack) != nil || decoder.Decode(new(any)) != io.EOF || ack.Schema != "air-worker.coordinator-integration/v1" || ack.Status != "ACK_RECORDED" || ack.Step != step || ack.Principal != scope.ID.Principal || ack.SessionKey != scope.ID.SessionKey || ack.TaskSHA256 != shaCodingBytes([]byte(task)) || ack.SourceHead != doc.SourceHead {
		return fail("acknowledgement scope or schema mismatch")
	}
	for i, w := range doc.Workers {
		if ack.JobIDs[i] == "" || ack.PatchSHA256[i] == "" || ack.JobIDs[i] != w.JobID || ack.PatchSHA256[i] != w.PatchSHA256 {
			return fail("worker acknowledgement mismatch")
		}
	}
	handoff, err := os.ReadFile(doc.Report)
	if err != nil || len(handoff) == 0 || len(handoff) > 1024*1024 || shaCodingBytes(handoff) != ack.HandoffSHA256 {
		return fail("handoff acknowledgement mismatch")
	}
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return empty, "", err
	}
	head, err := gitCodingOutput(top, "rev-parse", "HEAD")
	if err != nil || !operationalSHA.MatchString(head) || !operationalSHA.MatchString(ack.SourceHead) {
		return fail("invalid source history identity")
	}
	rel, err := filepath.Rel(top, root)
	if err != nil || (rel != "." && !isCodingProductPath(rel)) {
		return fail("product outside repository")
	}
	clean := func() bool {
		out, e := exec.Command("git", "-C", top, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", filepath.ToSlash(rel)).Output()
		return e == nil && len(out) == 0
	}
	if !clean() {
		return fail("current product source is dirty")
	}
	if err := exec.Command("git", "-C", top, "merge-base", "--is-ancestor", ack.SourceHead, head).Run(); err != nil {
		return fail("original source is not an ancestor of current HEAD")
	}
	list, err := exec.Command("git", "-C", top, "rev-list", "--ancestry-path", "--parents", "--max-count=10001", ack.SourceHead+".."+head).Output()
	if err != nil {
		return empty, "", err
	}
	lines := strings.Split(strings.TrimSpace(string(list)), "\n")
	if len(lines) > 10000 {
		return fail("historical search exceeds bound")
	}
	matches := make([]string, 0, 1)
	for _, line := range lines {
		parts := strings.Fields(line)
		if len(parts) != 2 || parts[1] != ack.SourceHead || !operationalSHA.MatchString(parts[0]) {
			continue
		}
		candidate := parts[0]
		names, err := exec.Command("git", "-C", top, "diff", "--name-only", "-z", ack.SourceHead, candidate, "--").Output()
		if err != nil {
			return empty, "", err
		}
		seen := map[string]bool{}
		valid := true
		for _, name := range strings.Split(strings.TrimSuffix(string(names), "\x00"), "\x00") {
			if _, ok := expected[name]; !ok || seen[name] {
				valid = false
				break
			}
			seen[name] = true
		}
		if !valid || len(seen) != len(expected) {
			continue
		}
		for name, hash := range expected {
			diff, err := exec.Command("git", "-C", top, "diff", "--no-ext-diff", "--binary", ack.SourceHead, candidate, "--", name).Output()
			if err != nil || len(diff) == 0 || len(diff) > 256*1024 || shaCodingBytes(diff) != hash {
				valid = false
				break
			}
		}
		if valid {
			matches = append(matches, candidate)
		}
	}
	if len(matches) != 1 {
		return fail(fmt.Sprintf("expected one matching reachable direct-child commit, found %d", len(matches)))
	}
	again, err := gitCodingOutput(top, "rev-parse", "HEAD")
	if err != nil || again != head || !clean() {
		return fail("source changed during historical verification")
	}
	if jobOwnedEvidencePath(dir, ackPath) != nil {
		return fail("acknowledgement path changed")
	}
	lastFile, err := owner.Open("coordinator-integration.json")
	if err != nil {
		return fail("acknowledgement cannot be reread")
	}
	last, lastReadErr := io.ReadAll(io.LimitReader(lastFile, 8193))
	lastInfo, lastStatErr := lastFile.Stat()
	lastCloseErr := lastFile.Close()
	lastPathInfo, lastPathErr := os.Lstat(ackPath)
	if lastReadErr != nil || lastStatErr != nil || lastCloseErr != nil || lastPathErr != nil ||
		!os.SameFile(before, lastInfo) || !os.SameFile(before, lastPathInfo) || shaCodingBytes(last) != shaCodingBytes(raw) {
		return fail("acknowledgement changed during verification")
	}
	fresh, freshExpected, err := verifiedCodingWorkerEvidence(root, scope, step, task)
	if err != nil || fresh.Report != doc.Report || fresh.SourceHead != doc.SourceHead || !reflect.DeepEqual(freshExpected, expected) {
		return fail("worker evidence changed during historical verification")
	}
	freshHandoff, err := os.ReadFile(doc.Report)
	if err != nil || shaCodingBytes(freshHandoff) != ack.HandoffSHA256 {
		return fail("handoff changed during historical verification")
	}
	ack.AckSHA256 = shaCodingBytes(raw)
	return ack, matches[0], nil
}
