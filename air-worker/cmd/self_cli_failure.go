package main

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

const ownCLIFailureSchema = "air-worker.self-learning.cli-failure/v3"

type ownCLIFailureReceipt struct {
	Schema   string `json:"schema"`
	RunID    string `json:"run_id"`
	Action   string `json:"action"`
	ExitCode int    `json:"exit_code"`
	// Exact argv identity without storing raw arguments, paths, or secrets.
	InvocationMAC string `json:"invocation_hmac_sha256"`
	CauseCode     string `json:"cause_code,omitempty"`
	Observed      string `json:"observed"`
	ReceiptMAC    string `json:"receipt_hmac_sha256"`
	Source        string `json:"source"`
	Principal     string `json:"principal"`
	Version       string `json:"version"`
	ProcessID     int    `json:"process_id"`
	CapturedAt    string `json:"captured_at_utc"`
}

// Only native AirWorker command CLASS is recorded, never positional
// arguments, shell text, environment values, credentials or user files.
// Internal reviewer/adapter/hook commands are excluded to prevent loops.
func ownCLIFailureAction(argv []string) (string, bool) {
	if len(argv) == 0 {
		return "", false
	}
	switch argv[0] {
	case "plan":
		if planHelpOrUnsupportedSyntax(argv[1:]) {
			// A local help/unsupported request is not a product failure.
			// Capturing it would itself dispatch a paid learning reviewer.
			return "", false
		}
		if len(argv) < 2 {
			return "plan", true
		}
		if argv[1] == "node" {
			if len(argv) < 3 {
				return "plan node", true
			}
			switch argv[2] {
			case "new", "close", "reparent", "list":
				return "plan node " + argv[2], true
			default:
				return "plan node invalid-action", true
			}
		}
		return "plan", true
	case "executor":
		return "executor", true
	case "judge":
		return "judge", true
	case "validate":
		return "validate", true
	case "plan-lint":
		return "plan-lint", true
	case "feedback":
		return "feedback", true
	case "tool":
		return "tool", true
	case "selfcheck":
		return "selfcheck", true
	default:
		return "", false
	}
}

func autoCaptureOwnCLIExit(argv []string, rc int) error {
	// A validated, measured native-preflight defer is already an immutable
	// procedure_used result, not a new generic CLI defect to learn twice.
	if nativePreflightDeferred.Swap(false) {
		return nil
	}
	action, ok := ownCLIFailureAction(argv)
	if rc == 0 || !ok {
		return nil
	}
	runID := "AW-CLI-" + learnSHA([]byte(action + "|" + strconv.Itoa(os.Getpid()) + "|" + time.Now().UTC().Format(time.RFC3339Nano)))[:24]
	start := startSharedLearningReviewBatchProcess
	if shouldDeferOwnCLIReviewForR02(argv, rc) {
		start = nil
	}
	return autoCaptureOwnCLIExitWithRunID(argv, rc, runID, start)
}

func autoCaptureOwnCLIExitWithRunID(argv []string, rc int, runID string, start func([]sharedDeferredReviewJob) error) error {
	action, ok := ownCLIFailureAction(argv)
	if rc == 0 || !ok {
		return nil
	}
	if rc < 0 || rc > 255 || len(runID) < 10 || len(runID) > 128 ||
		strings.IndexFunc(runID, func(c rune) bool { return c == 32 || c == 13 || c == 10 || c == 47 || c == 92 }) >= 0 {
		return errors.New("self-learning CLI failure requires a bounded native exit and stable safe run_id")
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !on {
		return nil
	}
	lock, locked := acquireLock(lockName("self-cli-failure", owner.Settings.RuntimeRoot))
	if !locked {
		return errors.New("another own CLI learning event is already being recorded")
	}
	defer lock.release()
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	invocationMAC := ownCLIInvocationMAC(key, argv)
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()

	recPath := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", runID+".json")
	var rec ownCLIFailureReceipt
	previous, readErr := readLearningBounded(recPath, 32*1024)
	replay := readErr == nil
	if readErr == nil {
		if err := json.Unmarshal(previous, &rec); err != nil {
			return err
		}
		if rec.Schema != ownCLIFailureSchema || rec.RunID != runID ||
			rec.ExitCode != rc || rec.Action != action || rec.InvocationMAC != invocationMAC || rec.Version != version || rec.Source != "air-worker-cli" ||
			rec.Principal != "air-worker" || rec.Observed == "" ||
			!hmac.Equal([]byte(rec.ReceiptMAC), []byte(ownCLIReceiptMAC(key, rec))) {
			return errors.New("owned CLI failure run_id has conflicting immutable evidence")
		}
	} else {
		if !errors.Is(readErr, os.ErrNotExist) {
			return readErr
		}
		rec = ownCLIFailureReceipt{
			Schema: ownCLIFailureSchema, RunID: runID, Action: action, ExitCode: rc, InvocationMAC: invocationMAC,
			Source: "air-worker-cli", Principal: "air-worker", Version: version,
			ProcessID: os.Getpid(), CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		rec.CauseCode = ownCLITrustedFailureCause(argv, rc)
		rec.Observed = ownCLIFailureObservation(action, rc, rec.CauseCode)
		rec.ReceiptMAC = ownCLIReceiptMAC(key, rec)
		raw, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(recPath), 0700); err != nil {
			return err
		}
		if err := writeFileAtomicDurable(recPath, append(raw, byte(10))); err != nil {
			return err
		}
		previous, err = readLearningBounded(recPath, 32*1024)
		if err != nil {
			return err
		}
		var again ownCLIFailureReceipt
		if err := json.Unmarshal(previous, &again); err != nil {
			return err
		}
		if again != rec {
			return errors.New("own CLI failure machine receipt readback changed")
		}
	}
	digest := learnSHA(previous)
	evidence := filepath.ToSlash(recPath) + "#sha256=" + digest
	class := "air-worker-cli-failure"
	if rec.CauseCode == "validate-run-config-missing" || rec.CauseCode == "validate-plan-missing" {
		class = "air-worker-cli-preflight"
	}
	result, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "observe", map[string]any{
		"run_id": runID, "kind": "run_completed", "observed": rec.Observed,
		"class": class, "source": "air-worker-cli",
		"principal": "air-worker", "session": runID,
		"outcome_ref": evidence, "defer_review": true,
	})
	if errors.Is(err, learning.ErrConflict) && result.Status == "duplicate" {
		err = nil
	}
	if err != nil {
		return err
	}
	if result.Status != "recorded" && result.Status != "duplicate" {
		return fmt.Errorf("own CLI event was not durably recorded: %s", result.Status)
	}
	if start != nil && !replay {
		if err := start([]sharedDeferredReviewJob{{Product: owner.Selector.ProductRoot, RunID: runID}}); err != nil {
			// The native event is already durable. Recovery may dispatch
			// pending reviews later; never rewrite this run or hide its exit.
			return fmt.Errorf("own CLI event recorded but reviewer dispatch failed: %w", err)
		}
	}
	return nil
}
