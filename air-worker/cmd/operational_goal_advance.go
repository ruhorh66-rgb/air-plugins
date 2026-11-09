package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
)

var operationalGoalAdvanceAcquireLock = acquireLock

// validateOperationalGoalAdvance verifies a previously owned pinned goal
// against the exact current Git HEAD. The old selector cannot use the usual
// readOperationalGoal reader after a source commit, because its HEAD pin is
// expected to be stale. This routine never weakens the regular loop read gate.
func validateOperationalGoalAdvance(root string, prior operationalGoal, nodeID, fromHead, toHead string) (operationalGoal, error) {
	var empty operationalGoal
	if !operationalSHA.MatchString(fromHead) || !operationalSHA.MatchString(toHead) || fromHead == toHead {
		return empty, errors.New("goal advance requires two distinct exact Git SHA values")
	}
	if prior.Schema != operationalGoalSchema || prior.SourceHead != fromHead || prior.NodeID != nodeID {
		return empty, errors.New("prior selector identity/source HEAD mismatch")
	}
	next, err := operationalGoalInputs(root, nodeID, toHead)
	if err != nil {
		return empty, err
	}
	if prior.Owner != next.Owner || prior.Trigger != next.Trigger ||
		prior.DoneWhen != next.DoneWhen || prior.ReleaseSHA256 != next.ReleaseSHA256 {
		return empty, errors.New("prior goal owner/criteria/release digest differs from current N-node")
	}
	// Require forward history from the exact old source; no force pushes,
	// unrelated branches, rebases or arbitrary manual retargeting.
	command := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", fromHead, toHead)
	if output, err := command.CombinedOutput(); err != nil {
		return empty, fmt.Errorf("goal source ancestry not proven: %s: %w", bytes.TrimSpace(output), err)
	}
	return next, nil
}

func decodeOperationalGoalPinned(raw []byte) (operationalGoal, error) {
	var prior operationalGoal
	if len(raw) == 0 || len(raw) > (1<<20) {
		return prior, errors.New("prior goal has invalid size")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return prior, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&prior); err != nil {
		return prior, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return prior, errors.New("prior goal has trailing content")
	}
	return prior, nil
}

func cmdOperationalGoalAdvance(argv []string) int {
	fs := flag.NewFlagSet("operational-goal advance", flag.ContinueOnError)
	product := fs.String("product", ".", "AirWorker product root")
	nodeID := fs.String("node", "", "full existing open native N-node ID")
	fromHead := fs.String("from-head", "", "expected previous source SHA")
	toHead := fs.String("source-head", "", "exact current source HEAD")
	principal := fs.String("principal", "", "declared native session principal")
	sessionKey := fs.String("session-key", "", "declared native session key")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, "operational-goal advance:", err)
		return 2
	}
	if _, err := executorScopeForSession(root, *principal, *sessionKey, ""); err != nil {
		fmt.Fprintln(os.Stderr, "operational-goal advance: session:", err)
		return 2
	}
	goalLock, ok := operationalGoalAdvanceAcquireLock(lockName("operational-goal", root))
	if !ok {
		fmt.Fprintln(os.Stderr, "operational-goal advance: goal owner is active")
		return 2
	}
	if runtime.GOOS == "windows" && !validAutoSelfDeliveryLock(goalLock) {
		if goalLock != nil {
			goalLock.release()
		}
		fmt.Fprintln(os.Stderr, "operational-goal advance: goal mutex unavailable")
		return 2
	}
	defer goalLock.release()
	writerLock, ok := operationalGoalAdvanceAcquireLock(lockName("operational-n139", root))
	if !ok {
		fmt.Fprintln(os.Stderr, "operational-goal advance: N139 writer is active")
		return 2
	}
	if runtime.GOOS == "windows" && !validAutoSelfDeliveryLock(writerLock) {
		if writerLock != nil {
			writerLock.release()
		}
		fmt.Fprintln(os.Stderr, "operational-goal advance: N139 writer mutex unavailable")
		return 2
	}
	defer writerLock.release()
	// Source clean gate is checked before mutation and again after readback;
	// the selector is never used to bypass the historical release guard.
	currentHead, err := sourceReviewGitState(root)
	if err != nil || currentHead != *toHead {
		fmt.Fprintln(os.Stderr, "operational-goal advance: current source is dirty or unverified")
		return 2
	}
	path := operationalGoalPath(root)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		fmt.Fprintln(os.Stderr, "operational-goal advance: prior selector is absent or nonregular")
		return 2
	}
	oldRaw, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "operational-goal advance: read prior:", err)
		return 2
	}
	prior, err := decodeOperationalGoalPinned(oldRaw)
	if err != nil {
		fmt.Fprintln(os.Stderr, "operational-goal advance: decode prior:", err)
		return 2
	}
	next, err := validateOperationalGoalAdvance(root, prior, *nodeID, *fromHead, *toHead)
	if err != nil {
		fmt.Fprintln(os.Stderr, "operational-goal advance:", err)
		return 2
	}
	// Re-read old bytes under both native locks to refuse stale/foreign edits.
	check, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(check, oldRaw) {
		fmt.Fprintln(os.Stderr, "operational-goal advance: prior selector changed during transition")
		return 2
	}
	nextRaw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := writeFileAtomic(path, append(nextRaw, '\n')); err != nil {
		fmt.Fprintln(os.Stderr, "operational-goal advance: write:", err)
		return 2
	}
	verified, err := readOperationalGoal(root)
	if err != nil || verified != next {
		// This is a host-local selector, not a plan file. A failed readback
		// restores only the known original state under the same native locks.
		_ = writeFileAtomic(path, oldRaw)
		fmt.Fprintln(os.Stderr, "operational-goal advance: postwrite verification failed")
		return 2
	}
	fmt.Printf("{\"schema\":\"air-worker.operational-goal.advance/v1\",\"from_head\":%q,\"to_head\":%q,\"node_id\":%q,\"release_sha256\":%q}\n",
		*fromHead, *toHead, verified.NodeID, verified.ReleaseSHA256)
	return 0
}

// The core keeps its only goal file in goal/; this is not a new planner.
