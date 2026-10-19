package main

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// finalizeN093TypedPreflightResult never treats a pending native intent as
// successful use. It requires exactly one matching event read back from the
// module-owned journal and then publishes a distinct immutable signed result.
// It is called under the native-preflight-use lock by the real CLI path.
func finalizeN093TypedPreflightResult(owner selfLearningOwner, prior ownCLIFailureReceipt,
	rule selfLearningLoadedRule, sourceEvent, intentPath string, key []byte) (string, bool, error) {
	action := supportedNativePreflightAction()
	rt := owner.Settings.RuntimeRoot
	ruleID := rule.Target + "@" + rule.SHA256
	identity := learnSHA([]byte(prior.RunID + "\x00" + sourceEvent + "\x00" + ruleID))
	usedID := "N118-USED-" + identity[:24]
	loadID := "N118-LOADED-" + identity[:24]
	expectedIntentPath := filepath.Join(rt, "native-preflight-uses", usedID+".json")
	if !sameLearningPath(intentPath, expectedIntentPath) || prior.RunID == "" ||
		sourceEvent == "" || len(key) < 32 || rule.SHA256 == n118ApprovedPreflightDeferralSHA {
		return "", false, errors.New("N093: invalid signed intent identity")
	}
	if err := n093CheckOrdinaryDir(filepath.Dir(expectedIntentPath)); err != nil {
		return "", false, err
	}
	intentRaw, err := n093ReadOrdinaryReceipt(expectedIntentPath)
	if err != nil {
		return "", false, err
	}
	var intent nativePreflightUseReceipt
	if err := json.Unmarshal(intentRaw, &intent); err != nil {
		return "", false, err
	}
	intentMAC, err := nativeTypedPreflightReceiptMAC(key, intent)
	if err != nil {
		return "", false, err
	}
	if intent.Schema != "air-worker.n093.typed-preflight-intent/v1" ||
		intent.Status != "pending_procedure_used" ||
		intent.Outcome != "pending" || intent.Effect != "NOT_PROVEN" ||
		intent.ReceiptMAC == "" || !hmac.Equal([]byte(intent.ReceiptMAC), []byte(intentMAC)) ||
		intent.RunID != usedID || intent.OriginalRunID != prior.RunID ||
		intent.OriginalEventID != sourceEvent ||
		intent.RuleID != ruleID || intent.ProcedureSHA != rule.SHA256 ||
		intent.InvocationMAC != prior.InvocationMAC ||
		intent.Principal != "air-worker" || intent.Session != prior.RunID ||
		intent.ProcessID <= 0 || intent.Nonce != identity[:24] ||
		intent.NativeValidationExecuted || intent.Precondition != "validated_run_config_missing" ||
		intent.ActionSchema != action.Schema || intent.ActionID != action.ID ||
		intent.ActionVersion != action.Version || intent.NativeCommand != action.NativeCommand ||
		intent.IntentSHA != "" || intent.ConfirmedEventID != "" ||
		intent.PlanFingerprint == "" {
		return "", false, errors.New("N093: pending intent was forged or mismatched")
	}
	if _, err := time.Parse(time.RFC3339Nano, intent.CreatedUTC); err != nil {
		return "", false, errors.New("N093: invalid native intent timestamp")
	}
	priorPath := filepath.Join(rt, "cli-failures", prior.RunID+".json")
	priorRaw, err := n093ReadOrdinaryReceipt(priorPath)
	if err != nil {
		return "", false, err
	}
	if intent.PriorSignedReceiptSHA != learnSHA(priorRaw) ||
		prior.Schema != ownCLIFailureSchema || prior.CauseCode != action.Precondition ||
		prior.Source != "air-worker-cli" || prior.Principal != "air-worker" ||
		!hmac.Equal([]byte(prior.ReceiptMAC), []byte(ownCLIReceiptMAC(key, prior))) {
		return "", false, errors.New("N093: original signed CLI provenance invalid")
	}
	var priorNow ownCLIFailureReceipt
	if json.Unmarshal(priorRaw, &priorNow) != nil || priorNow != prior {
		return "", false, errors.New("N093: original native failure changed")
	}
	if !currentSelfLearningRuleBytes(owner, rule) {
		return "", false, errors.New("N093: loaded learned action changed before confirmation")
	}
	freshRule, freshEvent, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible || freshRule != rule || freshEvent != sourceEvent {
		return "", false, errors.New("N093: reviewer/APPLIED ledger/index no longer matches current action")
	}
	loaded, err := nativeSelfSkillLoaded(rt, loadID, rule)
	if err != nil || !loaded {
		return "", false, errors.New("N093: no exact module-owned skill_loaded readback")
	}
	// This is a read-only native OS precondition, not a model/tool-output claim.
	// Any reparse point, permission failure or newly created configuration
	// disqualifies a pending missing-config outcome.
	configPath := filepath.Join(owner.Selector.ProductRoot, "run-config.json")
	if _, err := os.Lstat(configPath); !errors.Is(err, os.ErrNotExist) {
		return "", false, errors.New("N093: missing-configuration precondition no longer proven")
	}
	currentPlan, err := n093NativePlanFingerprint(owner.Selector.ProductRoot)
	if err != nil || currentPlan != intent.PlanFingerprint {
		return "", false, errors.New("N093: PLAN state changed since signed native intent")
	}

	expectedRef := filepath.ToSlash(expectedIntentPath) + "#sha256=" + learnSHA(intentRaw)
	expectedObserved := "Native AirWorker preflight selected the exact SHA-managed reviewer procedure, verified that run-config.json is still absent, and deferred redundant validate before executing its validator. The learned protective action succeeded; full validation did NOT run."
	confirmedID := ""
	err = n093ScanRuntimeEvidenceJSONL(rt, "events.jsonl", func(raw []byte) error {
		var event struct {
			Schema     string `json:"schema"`
			EventID    string `json:"event_id"`
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
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		if event.RunID != usedID {
			return nil
		}
		if confirmedID != "" || event.Schema != "air.learning.event/v1" ||
			event.Kind != "procedure_used" || event.Class != "native-preflight-retry" ||
			event.Source != "native-preflight-validate" || event.Principal != "air-worker" ||
			event.Session != prior.RunID || event.RuleID != ruleID ||
			event.Outcome != "pass" || event.OutcomeRef != expectedRef ||
			event.Observed != expectedObserved || !n093SafeEventID(event.EventID) {
			return errors.New("N093: module-owned use event is missing, duplicate or mismatched")
		}
		confirmedID = event.EventID
		return nil
	})
	if err != nil {
		return "", false, err
	}
	if confirmedID == "" {
		return "", false, nil
	}

	result := intent
	result.Schema = "air-worker.n093.typed-preflight-use/v1"
	result.Status = action.Outcome
	result.Outcome = action.Outcome
	result.Effect = action.Effect
	result.IntentSHA = learnSHA(intentRaw)
	result.ConfirmedEventID = confirmedID
	result.CreatedUTC = time.Now().UTC().Format(time.RFC3339Nano)
	result.ReceiptMAC, err = nativeTypedPreflightReceiptMAC(key, result)
	if err != nil {
		return "", false, err
	}
	dir := filepath.Join(rt, "native-preflight-results")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", false, err
	}
	if err := n093CheckOrdinaryDir(dir); err != nil {
		return "", false, err
	}
	path := filepath.Join(dir, usedID+".json")
	savedBytes, readErr := n093ReadOrdinaryReceipt(path)
	if errors.Is(readErr, os.ErrNotExist) {
		b, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return "", false, err
		}
		if err := writeFileAtomicDurable(path, append(b, '\n')); err != nil {
			return "", false, err
		}
		savedBytes, readErr = n093ReadOrdinaryReceipt(path)
	}
	if readErr != nil {
		return "", false, readErr
	}
	var saved nativePreflightUseReceipt
	if err := json.Unmarshal(savedBytes, &saved); err != nil {
		return "", false, err
	}
	verifiedMAC, err := nativeTypedPreflightReceiptMAC(key, saved)
	if err != nil || saved.ReceiptMAC == "" ||
		!hmac.Equal([]byte(saved.ReceiptMAC), []byte(verifiedMAC)) {
		return "", false, errors.New("N093: final result signature invalid")
	}
	result.CreatedUTC = saved.CreatedUTC
	result.ReceiptMAC, err = nativeTypedPreflightReceiptMAC(key, result)
	if err != nil || result != saved {
		return "", false, fmt.Errorf("N093: immutable final result conflict: %v", err)
	}
	return path, true, nil
}

func n093CheckOrdinaryDir(path string) error {
	stat, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !stat.IsDir() || stat.Mode()&os.ModeSymlink != 0 {
		return errors.New("N093: receipt directory is not an ordinary directory")
	}
	if _, err := os.Readlink(path); err == nil {
		return errors.New("N093: receipt directory cannot be a reparse link")
	}
	return nil
}

func n093ReadOrdinaryReceipt(path string) ([]byte, error) {
	stat, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() || stat.Size() > 32*1024 ||
		stat.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("N093: receipt is not a bounded regular file")
	}
	if _, err := os.Readlink(path); err == nil {
		return nil, errors.New("N093: receipt cannot be a reparse link")
	}
	return readLearningBounded(path, 32*1024)
}

func n093SafeEventID(id string) bool {
	if len(id) < 4 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' ||
			c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}

// n093NativePlanFingerprint binds the observed read-only action to the
// canonical PLAN.md state, including the fact that it was absent. The special
// absent marker cannot be confused with an existing file SHA.
func n093NativePlanFingerprint(root string) (string, error) {
	info, err := os.Lstat(root)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("N093: product root is not a plain directory")
	}
	if _, err := os.Readlink(root); err == nil {
		return "", errors.New("N093: product root cannot be reparse")
	}
	plan := filepath.Join(root, "PLAN.md")
	info, err = os.Lstat(plan)
	if errors.Is(err, os.ErrNotExist) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 4*1024*1024 ||
		info.Mode()&os.ModeSymlink != 0 {
		return "", errors.New("N093: canonical PLAN is not a bounded ordinary file")
	}
	if _, err := os.Readlink(plan); err == nil {
		return "", errors.New("N093: canonical PLAN is a reparse point")
	}
	raw, err := readSharedContextSkill(root, "PLAN.md", 4*1024*1024)
	if err != nil {
		return "", err
	}
	return "sha256:" + learnSHA(raw), nil
}
