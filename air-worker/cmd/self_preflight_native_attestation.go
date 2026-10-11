package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// This is the TRUSTED, compiled AirWorker consumer of the shared atomic
// operation. It is never serialized to learning-module.json or exposed as a
// reviewer callback. The actual native validator is run again against the
// exact pinned inputs while the module holds its normal rollback/apply locks.
type nativeN118VerifyPacket struct {
	Schema          string `json:"schema"`
	ProductID       string `json:"product_id"`
	RunID           string `json:"run_id"`
	OriginalRunID   string `json:"original_run_id"`
	OriginEventID   string `json:"origin_event_id"`
	ProposalID      string `json:"proposal_id"`
	Target          string `json:"target"`
	SHA256          string `json:"sha256"`
	RuleID          string `json:"rule_id"`
	OutcomeRef      string `json:"outcome_ref"`
	RequiredOutcome string `json:"required_outcome"`
	Source          string `json:"source"`
	Class           string `json:"class"`
	Principal       string `json:"principal"`
	Session         string `json:"session"`
	ObservedSHA256  string `json:"observed_sha256"`
}

func executeVerifiedNativePreflightUse(
	owner selfLearningOwner, candidate *nativePreflightRecoveredCandidate,
	original productValidation, expectedReceipt nativePreflightRecoveredReceipt,
	outcomeRef, observed string,
) (learning.Response, error) {
	if candidate == nil || candidate.UsedID == "" || candidate.SourceEvent == "" {
		return learning.Response{}, errors.New("positive native validation has no pinned source context")
	}
	// A reviewer candidate's proposal ID is set by shared learning core from
	// its source event ID, not by model output. The module verifies the actual
	// reviewer, proposal, ledger and active index again under its lock.
	proposalID := "LP-" + candidate.SourceEvent
	ruleID := candidate.Rule.Target + "@" + candidate.Rule.SHA256
	reqData := map[string]string{
		"run_id":          candidate.UsedID,
		"kind":            "procedure_used",
		"original_run_id": candidate.Prior.RunID,
		"origin_event_id": candidate.SourceEvent,
		"proposal_id":     proposalID,
		"load_run_id":     candidate.LoadID,
		"target":          candidate.Rule.Target,
		"expected_sha256": candidate.Rule.SHA256,
		"rule_id":         ruleID,
		"source":          "native-validate-recovered",
		"class":           "native-preflight-validated",
		"principal":       "air-worker",
		"session":         candidate.Prior.RunID,
		"outcome":         "pass",
		"outcome_ref":     outcomeRef,
		"observed":        observed,
	}
	raw, err := json.Marshal(reqData)
	if err != nil {
		return learning.Response{}, err
	}
	config := sharedLearningConfig(owner.Selector.ProductRoot, owner.Settings)
	config.VerifyNativeOutcome = func(ctx context.Context, packet json.RawMessage) (json.RawMessage, error) {
		return attestNativePreflightValidPass(ctx, owner, candidate, original,
			expectedReceipt, outcomeRef, observed, reqData, packet)
	}
	return learning.Execute(context.Background(), config, learning.Request{
		Operation: "observe_verified_use", Version: learning.Version, Data: raw,
	})
}

func attestNativePreflightValidPass(
	ctx context.Context, owner selfLearningOwner,
	c *nativePreflightRecoveredCandidate, original productValidation,
	expectedReceipt nativePreflightRecoveredReceipt, outcomeRef, observed string,
	request map[string]string, payload json.RawMessage,
) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(payload) == 0 || len(payload) > 64*1024 {
		return nil, errors.New("native positive attestation packet out of bounds")
	}
	var p nativeN118VerifyPacket
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if p.Schema != "air.learning.verify-native-outcome/v1" ||
		p.ProductID != owner.Settings.ProductID ||
		p.RunID != c.UsedID ||
		p.OriginalRunID != c.Prior.RunID ||
		p.OriginEventID != c.SourceEvent ||
		p.ProposalID != "LP-"+c.SourceEvent ||
		p.Target != c.Rule.Target ||
		p.SHA256 != c.Rule.SHA256 ||
		p.RuleID != c.Rule.Target+"@"+c.Rule.SHA256 ||
		p.OutcomeRef != outcomeRef ||
		p.RequiredOutcome != "pass" ||
		p.Source != request["source"] ||
		p.Class != request["class"] ||
		p.Principal != "air-worker" ||
		p.Session != c.Prior.RunID ||
		p.ObservedSHA256 != learnSHA([]byte(observed)) {
		return nil, errors.New("native verification packet does not bind to immutable AirWorker evidence")
	}
	if !sameLearningPath(owner.Selector.ProductRoot, c.Owner.Selector.ProductRoot) ||
		!sameLearningPath(owner.Settings.RuntimeRoot, c.Owner.Settings.RuntimeRoot) ||
		!strings.EqualFold(owner.Selector.ConfigSHA256, c.Owner.Selector.ConfigSHA256) {
		return nil, errors.New("native learning owner differs from prepared source context")
	}
	if !currentSelfLearningRuleBytes(owner, c.Rule) {
		return nil, errors.New("reviewed procedure SHA changed before actual native attestation")
	}
	path := filepath.Join(owner.Settings.RuntimeRoot, "native-preflight-recovered", c.UsedID+".json")
	b, err := readLearningBounded(path, 32*1024)
	if err != nil {
		return nil, err
	}
	expectedRef := filepath.ToSlash(path) + "#sha256=" + learnSHA(b)
	if p.OutcomeRef != expectedRef || outcomeRef != expectedRef {
		return nil, errors.New("native positive-use receipt SHA or path mismatch")
	}
	var saved nativePreflightRecoveredReceipt
	if err := json.Unmarshal(b, &saved); err != nil {
		return nil, err
	}
	if saved != expectedReceipt ||
		saved.Schema != "air-worker.n118.native-preflight-recovered/v1" ||
		saved.Status != "validated_pass" ||
		saved.NativeValidationExecuted != true ||
		saved.ValidationExitCode != 0 ||
		saved.Outcome != "pass" ||
		saved.RunID != c.UsedID ||
		saved.OriginalRunID != c.Prior.RunID ||
		saved.OriginalEventID != c.SourceEvent ||
		saved.RuleID != p.RuleID ||
		saved.ProcedureSHA != c.Rule.SHA256 ||
		saved.ConfigSHA != c.ConfigSHA ||
		saved.PlanSHA != c.PlanSHA ||
		saved.Goals != original.Goals ||
		saved.Criteria != original.Criteria ||
		saved.Effect != "restored_configuration_followed_by_actual_native_validate_pass" {
		return nil, errors.New("native positive-use receipt is not the exact previously validated result")
	}
	priorPath := filepath.Join(owner.Settings.RuntimeRoot, "cli-failures", c.Prior.RunID+".json")
	prior, err := readLearningBounded(priorPath, 32*1024)
	if err != nil {
		return nil, err
	}
	var signed ownCLIFailureReceipt
	if err := json.Unmarshal(prior, &signed); err != nil {
		return nil, err
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return nil, err
	}
	signedOK := signed == c.Prior && signed.ReceiptMAC == ownCLIReceiptMAC(key, signed)
	for i := range key {
		key[i] = 0
	}
	if !signedOK || saved.OriginalSignedSHA != learnSHA(prior) {
		return nil, errors.New("original native failure signature changed before attestation")
	}
	checkInputs := func() error {
		configPath, planPath, configSHA, planSHA, err := readNativePreflightConfigAndPlan(c.Root)
		if err != nil {
			return err
		}
		if !sameLearningPath(configPath, c.ConfigPath) ||
			!sameLearningPath(planPath, c.PlanPath) ||
			configSHA != c.ConfigSHA || planSHA != c.PlanSHA ||
			learnSHA(c.PinnedConfig) != c.ConfigSHA ||
			learnSHA(c.PinnedPlan) != c.PlanSHA {
			return errors.New("native validator input bytes differ from exact secure preflight snapshot")
		}
		return nil
	}
	if err := checkInputs(); err != nil {
		return nil, err
	}
	// This is a new REAL invocation of the compiled native validator, on
	// exact prior pinned config+PLAN bytes, independent of the caller-authored
	// pass JSON and the previously captured productValidation report.
	verified, rc := validateProductUsingPinnedInputs(c.Root, &nativeValidatePinnedBytes{
		Config: c.PinnedConfig, Plan: c.PinnedPlan,
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rc != 0 || len(verified.Problems) != 0 ||
		verified.Goals <= 0 || verified.Criteria <= 0 ||
		!reflect.DeepEqual(verified, original) {
		return nil, fmt.Errorf("independent native validation failed or differs: exit=%d", rc)
	}
	if err := checkInputs(); err != nil {
		return nil, err
	}
	// The callback is in trusted compiled AirWorker code, cannot be supplied
	// through sidecar JSON and does not call learning.Execute recursively.
	attestation := map[string]any{
		"schema":                     "air.learning.native-outcome-attestation/v1",
		"product_id":                 p.ProductID,
		"run_id":                     p.RunID,
		"origin_event_id":            p.OriginEventID,
		"proposal_id":                p.ProposalID,
		"target":                     p.Target,
		"sha256":                     p.SHA256,
		"outcome_ref":                p.OutcomeRef,
		"source":                     p.Source,
		"class":                      p.Class,
		"principal":                  p.Principal,
		"session":                    p.Session,
		"observed_sha256":            p.ObservedSHA256,
		"native_validation_executed": true,
		"validation_exit_code":       0,
		"outcome":                    "pass",
		"check_ref":                  "native-validator-pinned-snapshot:" + c.ConfigSHA + ":" + c.PlanSHA,
	}
	return json.Marshal(attestation)
}
