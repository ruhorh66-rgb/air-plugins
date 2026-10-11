package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// An intentionally deferred native validate is a genuine nonzero CLI exit,
// but NOT a second defect: the module-owned procedure_used receipt already
// explains the prevention. Keep main() from generating a duplicate review.
var nativePreflightDeferred atomic.Bool

type nativePreflightGuardResult struct {
	Deferred    bool
	RuleID      string
	ReceiptPath string
}

type nativePreflightUseReceipt struct {
	Schema                   string `json:"schema"`
	Status                   string `json:"status"`
	RunID                    string `json:"run_id"`
	OriginalRunID            string `json:"original_run_id"`
	OriginalEventID          string `json:"original_event_id"`
	PriorSignedReceiptSHA    string `json:"prior_signed_receipt_sha256"`
	InvocationMAC            string `json:"invocation_hmac_sha256"`
	RuleID                   string `json:"rule_id"`
	ProcedureSHA             string `json:"procedure_sha256"`
	NativeValidationExecuted bool   `json:"native_validation_executed"`
	Precondition             string `json:"precondition"`
	Outcome                  string `json:"outcome"`
	Effect                   string `json:"effect"`
	CreatedUTC               string `json:"created_utc"`
}

// This guard is called only from the real native cmdValidate before the
// validator. It can never grant permissions or apply executable rules.
// Missing configuration is checked against OS state, never model text.
func nativePreflightGuard(argv []string) (nativePreflightGuardResult, error) {
	var out nativePreflightGuardResult
	if len(argv) < 2 || argv[0] != "validate" {
		return out, nil
	}
	// This is a pure read-only precondition predicate; passing rc=2 is NOT
	// an assertion that the current validation has actually been executed.
	if ownCLITrustedFailureCause(argv, 2) != "validate-run-config-missing" {
		return out, nil
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return out, err
	}
	if !on {
		return out, nil
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, argv)
	if err != nil {
		return out, err
	}
	if !found {
		return out, nil
	}
	rule, eventID, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil {
		return out, err
	}
	if !eligible {
		return out, nil
	}
	lock, locked := acquireLock(lockName("native-preflight-use", owner.Settings.RuntimeRoot))
	if !locked {
		return out, errors.New("concurrent self-learning preflight receipt is owned by another process")
	}
	defer lock.release()
	// Rebind all owner and source identities after acquiring the lock.
	again, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return out, err
	}
	if !enabled || !sameLearningPath(again.Settings.RuntimeRoot, owner.Settings.RuntimeRoot) ||
		!sameLearningPath(again.Selector.ProductRoot, owner.Selector.ProductRoot) ||
		!strings.EqualFold(again.Selector.ConfigSHA256, owner.Selector.ConfigSHA256) {
		return out, errors.New("self-learning owner changed during native preflight")
	}
	if ownCLITrustedFailureCause(argv, 2) != "validate-run-config-missing" {
		return out, nil
	}
	if !currentSelfLearningRuleBytes(again, rule) {
		return out, errors.New("selected reviewed preflight procedure changed after selection")
	}
	return recordNativePreflightPreventedRetry(again, prior, rule, eventID)
}

func recordNativePreflightPreventedRetry(owner selfLearningOwner, prior ownCLIFailureReceipt,
	rule selfLearningLoadedRule, sourceEvent string) (nativePreflightGuardResult, error) {
	var out nativePreflightGuardResult
	ruleID := rule.Target + "@" + rule.SHA256
	identity := learnSHA([]byte(prior.RunID + "\x00" + sourceEvent + "\x00" + ruleID))
	loadID := "N118-LOADED-" + identity[:24]
	usedID := "N118-USED-" + identity[:24]
	loaded, err := nativeSelfSkillLoaded(owner.Settings.RuntimeRoot, loadID, rule)
	if err != nil {
		return out, err
	}
	if !loaded {
		response, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "load", map[string]string{
			"run_id": loadID, "target": rule.Target, "expected_sha256": rule.SHA256})
		if err != nil || response.Status != "loaded" {
			return out, fmt.Errorf("native preflight safe procedure load was not confirmed: %w", err)
		}
		loaded, err = nativeSelfSkillLoaded(owner.Settings.RuntimeRoot, loadID, rule)
		if err != nil || !loaded {
			return out, errors.New("module did not preserve exact native skill_loaded identity")
		}
	}
	failurePath := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", prior.RunID+".json")
	failureRaw, err := readLearningBounded(failurePath, 32*1024)
	if err != nil {
		return out, err
	}
	receipt := nativePreflightUseReceipt{
		Schema: "air-worker.n118.native-preflight-use/v1",
		Status: "native_preflight_retry_prepared", RunID: usedID,
		OriginalRunID: prior.RunID, OriginalEventID: sourceEvent,
		PriorSignedReceiptSHA: learnSHA(failureRaw), InvocationMAC: prior.InvocationMAC,
		RuleID: ruleID, ProcedureSHA: rule.SHA256,
		NativeValidationExecuted: false, Precondition: "validated_run_config_missing",
		Outcome: "pending", Effect: "pending_correlated_native_use_event",
		CreatedUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	dir := filepath.Join(owner.Settings.RuntimeRoot, "native-preflight-uses")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return out, err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return out, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return out, errors.New("native preflight use receipt directory is not a plain directory")
	}
	if _, err := os.Readlink(dir); err == nil {
		return out, errors.New("native preflight receipt directory cannot be a reparse point")
	}
	path := filepath.Join(dir, usedID+".json")
	previous, err := readLearningBounded(path, 32*1024)
	if errors.Is(err, os.ErrNotExist) {
		bytes, marshalErr := json.MarshalIndent(receipt, "", "  ")
		if marshalErr != nil {
			return out, marshalErr
		}
		if err := writeFileAtomicDurable(path, append(bytes, '\n')); err != nil {
			return out, err
		}
		previous, err = readLearningBounded(path, 32*1024)
	}
	if err != nil {
		return out, err
	}
	var persisted nativePreflightUseReceipt
	if err := json.Unmarshal(previous, &persisted); err != nil {
		return out, err
	}
	// A repeated identical native retry must not replace the first durable
	// receipt nor claim a second procedure_used event.
	originalCreated := receipt.CreatedUTC
	receipt.CreatedUTC = persisted.CreatedUTC
	if persisted != receipt || persisted.CreatedUTC == "" {
		return out, fmt.Errorf("native preflight immutable receipt conflict (original time %s)", originalCreated)
	}
	effectRef := filepath.ToSlash(path) + "#sha256=" + learnSHA(previous)
	expectedObserved := "Native AirWorker preflight selected the exact SHA-managed reviewer procedure, verified that run-config.json is still absent, and deferred redundant validate before executing its validator. The learned protective action succeeded; full validation did NOT run."
	var usageFound bool
	err = scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "events.jsonl"), func(raw []byte) error {
		var e struct {
			Schema     string `json:"schema"`
			RunID      string `json:"run_id"`
			Kind       string `json:"kind"`
			Class      string `json:"class"`
			Source     string `json:"source"`
			Principal  string `json:"principal"`
			Session    string `json:"session"`
			RuleID     string `json:"rule_id"`
			Outcome    string `json:"outcome"`
			OutcomeRef string `json:"outcome_ref"`
			Observed   string `json:"observed"`
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.RunID != usedID {
			return nil
		}
		if usageFound || e.Schema != "air.learning.event/v1" ||
			e.Kind != "procedure_used" || e.Class != "native-preflight-retry" ||
			e.Source != "native-preflight-validate" || e.Principal != "air-worker" ||
			e.Session != prior.RunID || e.RuleID != ruleID || e.Outcome != "pass" ||
			e.OutcomeRef != effectRef || e.Observed != expectedObserved {
			return errors.New("native preflight already has conflicting usage identity")
		}
		usageFound = true
		return nil
	})
	if err != nil {
		return out, err
	}
	if !usageFound {
		response, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "observe", map[string]string{
			"run_id": usedID, "kind": "procedure_used", "class": "native-preflight-retry",
			"source": "native-preflight-validate", "principal": "air-worker",
			"session": prior.RunID, "rule_id": ruleID, "outcome": "pass",
			"outcome_ref": effectRef, "observed": expectedObserved,
		})
		if err != nil || response.Status != "recorded" || response.ID == "" {
			return out, fmt.Errorf("native preflight measured use not recorded: %w", err)
		}
	}
	out.Deferred = true
	out.RuleID = ruleID
	out.ReceiptPath = path
	return out, nil
}
