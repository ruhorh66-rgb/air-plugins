package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This is a narrowly scoped, native, post-recovery proof for N-118.
// It does NOT claim that the learned procedure itself repaired configuration.
// It proves the release-owned action ("check canonical files before retry")
// was performed before a REAL native validator which then returned PASS.
type nativePreflightRecoveredCandidate struct {
	Owner       selfLearningOwner
	Prior       ownCLIFailureReceipt
	Rule        selfLearningLoadedRule
	SourceEvent string
	Root        string
	ConfigPath  string
	PlanPath    string
	ConfigSHA   string
	PlanSHA     string
	// These exact byte slices are consumed by the native validator; reading
	// the paths again would permit replacement-and-restoration false proofs.
	PinnedConfig []byte
	PinnedPlan   []byte
	LoadID       string
	UsedID       string
}

type nativePreflightRecoveredReceipt struct {
	Schema                   string `json:"schema"`
	Status                   string `json:"status"`
	RunID                    string `json:"run_id"`
	OriginalRunID            string `json:"original_run_id"`
	OriginalEventID          string `json:"original_event_id"`
	OriginalSignedSHA        string `json:"original_signed_sha256"`
	RuleID                   string `json:"rule_id"`
	ProcedureSHA             string `json:"procedure_sha256"`
	ConfigSHA                string `json:"config_sha256"`
	PlanSHA                  string `json:"plan_sha256"`
	NativeValidationExecuted bool   `json:"native_validation_executed"`
	ValidationExitCode       int    `json:"validation_exit_code"`
	Goals                    int    `json:"goals"`
	Criteria                 int    `json:"criteria"`
	Outcome                  string `json:"outcome"`
	Effect                   string `json:"effect"`
	CreatedUTC               string `json:"created_utc"`
}

// These are actual existing artifact bytes (not an LLM response) with the
// same handle+resolved-root confinement used for approved learned content.
func readNativePreflightConfigAndPlan(root string) (string, string, string, string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return "", "", "", "", err
	}
	configPath := filepath.Join(root, "run-config.json")
	rawConfig, err := readSharedContextSkill(root, "run-config.json", 256*1024)
	if err != nil {
		return "", "", "", "", err
	}
	var cfg runConfig
	if err := json.Unmarshal(rawConfig, &cfg); err != nil {
		return "", "", "", "", err
	}
	planPath := nativePlanPath(root, cfg)
	if !pathWithinRoot(root, planPath) {
		return "", "", "", "", errors.New("native learning cannot credit a PLAN outside the current product root")
	}
	rel, err := filepath.Rel(root, planPath)
	if err != nil {
		return "", "", "", "", err
	}
	rawPlan, err := readSharedContextSkill(root, filepath.ToSlash(rel), 4*1024*1024)
	if err != nil {
		return "", "", "", "", err
	}
	return configPath, planPath, learnSHA(rawConfig), learnSHA(rawPlan), nil
}

func prepareNativePreflightRecoveredUse(argv []string, product string) (*nativePreflightRecoveredCandidate, error) {
	if len(argv) < 2 || argv[0] != "validate" {
		return nil, nil
	}
	// If config/PLAN are still missing, N-118 defer guard handles the earlier
	// lesson. Presence alone does not record a successful use.
	root, err := filepath.Abs(product)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(filepath.Join(root, "run-config.json")); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, nil
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, argv)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	rule, origin, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil {
		return nil, err
	}
	if !eligible {
		return nil, nil
	}
	configPath, planPath, configSHA, planSHA, err := readNativePreflightConfigAndPlan(root)
	if err != nil {
		// Let real validation truthfully report its original error; don't claim
		// a measured recovery action from unreadable/missing paths.
		return nil, nil
	}
	// Capture the same opened-handle-constrained bytes for the actual
	// validator. Matching SHA against the first preflight snapshot catches
	// any intervening replacement before pinning the semantic inputs.
	pinnedConfig, err := readSharedContextSkill(root, "run-config.json", 256*1024)
	if err != nil {
		return nil, nil
	}
	planRel, err := filepath.Rel(root, planPath)
	if err != nil {
		return nil, nil
	}
	pinnedPlan, err := readSharedContextSkill(root, filepath.ToSlash(planRel), 4*1024*1024)
	if err != nil || learnSHA(pinnedConfig) != configSHA || learnSHA(pinnedPlan) != planSHA {
		return nil, nil
	}
	identity := learnSHA([]byte("native-preflight-recovered/v1\x00" + prior.RunID + "\x00" + origin + "\x00" +
		rule.SHA256 + "\x00" + configSHA + "\x00" + planSHA))
	loadID := "N118-RECOVERED-LOADED-" + identity[:24]
	usedID := "N118-RECOVERED-USED-" + identity[:24]
	loaded, err := nativeSelfSkillLoaded(owner.Settings.RuntimeRoot, loadID, rule)
	if err != nil {
		return nil, err
	}
	if !loaded {
		result, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "load", map[string]string{
			"run_id": loadID, "target": rule.Target, "expected_sha256": rule.SHA256,
		})
		if err != nil || result.Status != "loaded" {
			return nil, fmt.Errorf("exact reviewed preflight rule load not proved: %w", err)
		}
		loaded, err = nativeSelfSkillLoaded(owner.Settings.RuntimeRoot, loadID, rule)
		if err != nil || !loaded {
			return nil, errors.New("native module did not persist the exact pre-validation skill load")
		}
	}
	return &nativePreflightRecoveredCandidate{Owner: owner, Prior: prior, Rule: rule, SourceEvent: origin,
		Root: root, ConfigPath: configPath, PlanPath: planPath, ConfigSHA: configSHA, PlanSHA: planSHA,
		PinnedConfig: pinnedConfig, PinnedPlan: pinnedPlan,
		LoadID: loadID, UsedID: usedID}, nil
}

func recordNativePreflightRecoveredPass(c *nativePreflightRecoveredCandidate, r productValidation, code int) error {
	if c == nil || code != 0 {
		return nil
	}
	if r.Schema != productValidationSchema || len(r.Problems) != 0 ||
		!sameLearningPath(r.Product, c.Root) ||
		!sameLearningPath(r.Config, c.ConfigPath) ||
		!sameLearningPath(r.Plan, c.PlanPath) {
		return errors.New("actual native validator report does not match preflight-read canonical paths")
	}
	lock, ok := acquireLock(lockName("native-preflight-recovered", c.Owner.Settings.RuntimeRoot))
	if !ok {
		return errors.New("native recovery outcome owned by another invocation")
	}
	defer lock.release()
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return err
	}
	if !on || !sameLearningPath(owner.Selector.ProductRoot, c.Owner.Selector.ProductRoot) ||
		!sameLearningPath(owner.Settings.RuntimeRoot, c.Owner.Settings.RuntimeRoot) ||
		!strings.EqualFold(owner.Selector.ConfigSHA256, c.Owner.Selector.ConfigSHA256) {
		return errors.New("self-learning owner changed during successful native validation")
	}
	if !currentSelfLearningRuleBytes(owner, c.Rule) {
		return errors.New("reviewed preflight rule changed before success evidence")
	}
	// Ledger/index and reviewer origin may change after preparation while the
	// native validator runs. Re-read the full applied provenance under the
	// recovery outcome lock; file SHA alone is NOT current approval.
	freshRule, freshEvent, freshEligible, err := findAppliedPreflightRule(owner, c.Prior)
	if err != nil {
		return err
	}
	if !freshEligible || freshRule != c.Rule || freshEvent != c.SourceEvent {
		return errors.New("reviewer, applied ledger or module index changed during native validation")
	}
	pathConfig, pathPlan, afterConfig, afterPlan, err := readNativePreflightConfigAndPlan(c.Root)
	if err != nil {
		return err
	}
	if !sameLearningPath(pathConfig, c.ConfigPath) || !sameLearningPath(pathPlan, c.PlanPath) ||
		afterConfig != c.ConfigSHA || afterPlan != c.PlanSHA {
		return errors.New("config or PLAN changed during native validation; no measured use")
	}
	priorPath := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", c.Prior.RunID+".json")
	raw, err := readLearningBounded(priorPath, 32*1024)
	if err != nil {
		return err
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return err
	}
	// The complete prior receipt was signed; no replay may replace its origin
	// between planning and recording the positive outcome.
	var signed ownCLIFailureReceipt
	if err := json.Unmarshal(raw, &signed); err != nil {
		return err
	}
	sameSigned := signed == c.Prior && signed.ReceiptMAC == ownCLIReceiptMAC(key, signed)
	for i := range key {
		key[i] = 0
	}
	if !sameSigned {
		return errors.New("signed original native failure changed during validation")
	}
	priorRef := learnSHA(raw)
	ruleID := c.Rule.Target + "@" + c.Rule.SHA256
	receipt := nativePreflightRecoveredReceipt{
		Schema: "air-worker.n118.native-preflight-recovered/v1", Status: "validated_pass",
		RunID: c.UsedID, OriginalRunID: c.Prior.RunID, OriginalEventID: c.SourceEvent,
		OriginalSignedSHA: priorRef, RuleID: ruleID, ProcedureSHA: c.Rule.SHA256,
		ConfigSHA: c.ConfigSHA, PlanSHA: c.PlanSHA, NativeValidationExecuted: true,
		ValidationExitCode: 0, Goals: r.Goals, Criteria: r.Criteria, Outcome: "pass",
		Effect:     "restored_configuration_followed_by_actual_native_validate_pass",
		CreatedUTC: time.Now().UTC().Format(time.RFC3339Nano),
	}
	dir := filepath.Join(owner.Settings.RuntimeRoot, "native-preflight-recovered")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return errors.New("native recovered-preflight evidence directory is linked/invalid")
	}
	if _, err := os.Readlink(dir); err == nil {
		return errors.New("native recovered-preflight evidence directory is a reparse point")
	}
	path := filepath.Join(dir, c.UsedID+".json")
	existing, err := readLearningBounded(path, 32*1024)
	if errors.Is(err, os.ErrNotExist) {
		bytes, marshalErr := json.MarshalIndent(receipt, "", "  ")
		if marshalErr != nil {
			return marshalErr
		}
		if err := writeFileAtomicDurable(path, append(bytes, '\n')); err != nil {
			return err
		}
		existing, err = readLearningBounded(path, 32*1024)
	}
	if err != nil {
		return err
	}
	var persisted nativePreflightRecoveredReceipt
	if err := json.Unmarshal(existing, &persisted); err != nil {
		return err
	}
	receipt.CreatedUTC = persisted.CreatedUTC
	if persisted != receipt || persisted.CreatedUTC == "" {
		return errors.New("native positive-validation receipt changed or conflicts with original source")
	}
	ref := filepath.ToSlash(path) + "#sha256=" + learnSHA(existing)
	observed := "Native AirWorker verified exact learned preflight procedure, verified unchanged canonical run-config.json and PLAN bytes before and after the real validator, and the actual native validate returned PASS. No claim that the procedure repaired files."
	var already bool
	err = scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var event struct {
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
		if err := json.Unmarshal(b, &event); err != nil {
			return err
		}
		if event.RunID != c.UsedID {
			return nil
		}
		if already || event.Schema != "air.learning.event/v1" ||
			event.Kind != "procedure_used" || event.Class != "native-preflight-validated" ||
			event.Source != "native-validate-recovered" || event.Principal != "air-worker" ||
			event.Session != c.Prior.RunID || event.RuleID != ruleID ||
			event.Outcome != "pass" || event.OutcomeRef != ref || event.Observed != observed {
			return errors.New("native positive-validation use identity conflicts with prior event")
		}
		already = true
		return nil
	})
	if err != nil {
		return err
	}
	if already {
		return nil
	}
	res, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "observe", map[string]string{
		"run_id": c.UsedID, "kind": "procedure_used", "class": "native-preflight-validated",
		"source": "native-validate-recovered", "principal": "air-worker",
		"session": c.Prior.RunID, "rule_id": ruleID, "outcome": "pass",
		"outcome_ref": ref, "observed": observed,
	})
	if err != nil || res.Status != "recorded" || res.ID == "" {
		return fmt.Errorf("native positive-validator measured use not recorded: %w", err)
	}
	return nil
}
