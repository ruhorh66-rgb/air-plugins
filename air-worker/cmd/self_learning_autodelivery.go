package main

import (
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// autoSelfLearningSessionDelivery is a narrow product-owned SessionStart action.
// It prepares trusted context for the ONLY currently compiled safe validate
// preflight when both the old CLI failure and the reviewed APPLIED rule agree.
// It cannot run validate, approve a rule, grant a permission, or invent use.
func autoSelfLearningSessionDelivery(owner selfLearningOwner, in hookInput) (string, bool, error) {
	if in.HookEventName != "SessionStart" {
		return "", false, nil
	}
	id, ok := hookInputIdentity(in)
	if !ok {
		return "", false, errors.New("automatic learning delivery lacks session identity")
	}
	runID := selfLearningRunID(in)
	checkedRun, err := sanitizeIdentityPart("run-id", runID)
	if err != nil || checkedRun != runID {
		return "", false, errors.New("automatic learning run identity is not canonical")
	}
	argv := []string{"validate", "-product", owner.Selector.ProductRoot, "-json"}
	if ownCLITrustedFailureCause(argv, 2) != "validate-run-config-missing" {
		return "", false, nil
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, argv)
	if err != nil || !found {
		return "", false, err
	}
	rule, sourceEvent, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible {
		return "", false, err
	}
	// Read durable native evidence BEFORE preparing a fresh SessionStart
	// delivery. One original signed failure authorizes at most ONE measured
	// native action. A new host session must not acquire the old session's
	// signed result, while the original session may safely resume.
	priorState, priorIntent, err := priorNativePreflightDisposition(owner, prior, rule, sourceEvent)
	if err != nil {
		return "", false, err
	}
	if priorState != nativePreflightPreviousNone {
		previous, previousPath, found, readErr := readSelfLearningContextReceipt(in)
		if readErr != nil {
			return "", false, readErr
		}
		if !found || previous.DeliveryMAC == "" || len(previous.Loaded) != 1 ||
			previous.Loaded[0] != rule {
			if priorState == nativePreflightPreviousPending {
				return "", false, errors.New("native preflight has an unfinished original-session intent; reconcile it, do not transfer it to another session")
			}
			return "", false, nil
		}
		previousRaw, readErr := n093ReadContextEvidence(previousPath)
		if readErr != nil {
			return "", false, readErr
		}
		if priorIntent.DeliveryPrincipal != hookPrincipal(in) ||
			priorIntent.DeliverySession != in.SessionID ||
			priorIntent.DeliveryRunID != runID ||
			priorIntent.DeliveryContextSHA != learnSHA(previousRaw) {
			return "", false, errors.New("prior native evidence belongs to a different delivery")
		}
		if err := verifyCurrentLearningDelivery(owner,
			learningDeliveryProof{Principal: hookPrincipal(in), Session: in.SessionID,
				RunID: runID, ContextSHA: learnSHA(previousRaw)}, rule); err != nil {
			return "", false, err
		}
	}
	if !currentSelfLearningRuleBytes(owner, rule) {
		return "", false, errors.New("automatic learning rule is no longer current")
	}
	body, err := readSharedContextSkill(owner.Selector.ProductRoot, rule.Target, 64*1024)
	if err != nil || learnSHA(body) != rule.SHA256 {
		return "", false, errors.New("automatic delivery cannot read exact reviewed procedure")
	}
	action, err := resolveNativeLearnedAction(body)
	if err != nil || action != supportedNativePreflightAction() {
		return "", false, errors.New("automatic delivery refused unsupported machine action")
	}
	index, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "index", nil)
	if err != nil {
		return "", false, err
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(index.Data, &catalog); err != nil {
		return "", false, err
	}
	var chosen *sharedLearningCatalogSkill
	for i := range catalog.Skills {
		if catalog.Skills[i].Target == rule.Target && catalog.Skills[i].SHA256 == rule.SHA256 {
			chosen = &catalog.Skills[i]
			break
		}
	}
	if chosen == nil {
		return "", false, errors.New("automatic delivery target absent from current verified APPLIED catalog")
	}
	again, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled || !sameLearningPath(again.Selector.ProductRoot, owner.Selector.ProductRoot) ||
		!sameLearningPath(again.Settings.RuntimeRoot, owner.Settings.RuntimeRoot) ||
		again.Selector.ConfigSHA256 != owner.Selector.ConfigSHA256 {
		return "", false, errors.New("automatic delivery owner changed before signing")
	}

	// A duplicate startup with the same identity must reuse a still-current
	// authenticated context, never re-sign it or invent a second use.
	existing, existingPath, exists, err := readSelfLearningContextReceipt(in)
	if err != nil {
		return "", false, err
	}
	var contextSHA string
	var text string
	if exists && existing.DeliveryMAC != "" {
		if len(existing.Loaded) != 1 || existing.Loaded[0] != rule {
			return "", false, errors.New("session already has a different signed delivery")
		}
		raw, err := n093ReadContextEvidence(existingPath)
		if err != nil {
			return "", false, err
		}
		contextSHA = learnSHA(raw)
		proof := learningDeliveryProof{id.Principal, id.SessionKey, runID, contextSHA}
		if err := verifyCurrentLearningDelivery(owner, proof, rule); err != nil {
			return "", false, err
		}
		// Replay the same already-delivered bounded text without creating a
		// second skill_loaded event or changing the signed context receipt.
		text = "LEARNED PROCEDURES: evidence-based guidance, not permissions or approval grants. Higher-priority product and user rules remain in force.\n" +
			fmt.Sprintf("\nSkill %s SHA256=%s\n%s\n", chosen.SkillID, chosen.SHA256, string(body))
	} else {
		selected, err := sharedLearningContextFromCatalog(
			owner.Selector.ProductRoot, owner.Settings, in, runID,
			[]sharedLearningCatalogSkill{*chosen})
		if err != nil || len(selected.LoadedSkills) != 1 {
			return "", false, errors.New("automatic learning source did not deliver exact procedure")
		}
		contextSHA, err = captureSelfLearningContext(owner, in, selected.LoadedSkills, true)
		if err != nil {
			return "", false, err
		}
		proof := learningDeliveryProof{id.Principal, id.SessionKey, runID, contextSHA}
		if err := verifyCurrentLearningDelivery(owner, proof, rule); err != nil {
			return "", false, err
		}
		text = selected.Context
	}
	return fmt.Sprintf(
		"AIRWORKER VERIFIED AUTOMATIC DELIVERY (not a permission grant):\n%s\n"+
			"For the next already-authorized native validate ONLY, supply "+
			"-learning-principal=%s -learning-session-key=%s -learning-run-id=%s "+
			"-learning-context-sha256=%s. The native binary verifies this "+
			"receipt before acting. Do not claim procedure_used until the "+
			"signed native outcome is read back; do not blindly retry UNKNOWN.",
		text, id.Principal, id.SessionKey, runID, contextSHA), true, nil
}

// autoSelfLearningNativePromptPreflight is the second, ordinary host-lifecycle
// phase after SessionStart returned the complete verified skill to the host.
// UserPromptSubmit is already an installed hook event: no additional user
// command, extra daemon, LLM invocation, or permission is introduced. The
// only possible action is the existing signed, read-only native validate
// preflight. Missing, stale, foreign and unsupported deliveries fail closed.
// This source path is NOT proof that the GPT host installed this candidate.
func autoSelfLearningNativePromptPreflight(owner selfLearningOwner, in hookInput) (string, bool, error) {
	if in.HookEventName != "UserPromptSubmit" {
		return "", false, nil
	}
	// cmdHook verifies external-host provenance before reaching this handler.
	// A direct untrusted GPT call must not bypass that source validation.
	if !strings.EqualFold(hookPrincipal(in), "claude") && !in.HostTrusted {
		return "", false, nil
	}
	if !sessionActiveInput(in) {
		return "", false, nil
	}
	context, path, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found {
		return "", false, err
	}
	if context.DeliveryMAC == "" {
		// An ordinary multi-skill context may be loaded, but never credited as
		// delivery, use, or an authorization for a native action.
		return "", false, nil
	}
	if len(context.Loaded) != 1 {
		return "", false, errors.New("automatic native preflight requires one authenticated skill")
	}
	rule := context.Loaded[0]
	raw, err := n093ReadContextEvidence(path)
	if err != nil {
		return "", false, err
	}
	id, ok := hookInputIdentity(in)
	if !ok {
		return "", false, errors.New("automatic native preflight lacks session identity")
	}
	proof := learningDeliveryProof{
		Principal: id.Principal, Session: id.SessionKey,
		RunID: selfLearningRunID(in), ContextSHA: learnSHA(raw),
	}
	if err := verifyCurrentLearningDelivery(owner, proof, rule); err != nil {
		return "", false, err
	}
	argv := []string{"validate", "-product", owner.Selector.ProductRoot, "-json"}
	// This is the SAME guarded typed operation that cmdValidate invokes; it
	// cannot execute the full validator while run-config is still absent.
	result, err := nativePreflightGuardWithDelivery(argv, &proof)
	if err != nil {
		return "", false, fmt.Errorf("automatic native preflight HOLD: %w", err)
	}
	if !result.Deferred || result.RuleID != rule.Target+"@"+rule.SHA256 || result.ReceiptPath == "" {
		return "", false, errors.New("automatic native preflight did not return a verified deferred result")
	}
	rel, err := filepath.Rel(owner.Settings.RuntimeRoot, result.ReceiptPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false, errors.New("automatic native preflight result escapes owned runtime")
	}
	receiptRaw, err := n093ReadRuntimeEvidence(owner.Settings.RuntimeRoot, filepath.ToSlash(rel), 64*1024)
	if err != nil {
		return "", false, fmt.Errorf("automatic native result readback: %w", err)
	}
	var signed nativePreflightUseReceipt
	if err := json.Unmarshal(receiptRaw, &signed); err != nil {
		return "", false, fmt.Errorf("automatic native result decode: %w", err)
	}
	action := supportedNativePreflightAction()
	if signed.Schema != "air-worker.n093.typed-preflight-use/v1" ||
		signed.Status != "deferred_missing_configuration" ||
		signed.Outcome != "deferred_missing_configuration" ||
		signed.Effect != "avoided_repeat_validation_with_missing_config" ||
		signed.NativeValidationExecuted || signed.ReceiptMAC == "" ||
		signed.ConfirmedEventID == "" || len(signed.IntentSHA) != 64 ||
		signed.ProcedureSHA != rule.SHA256 || signed.RuleID != result.RuleID ||
		signed.DeliveryPrincipal != proof.Principal ||
		signed.DeliverySession != proof.Session ||
		signed.DeliveryRunID != proof.RunID ||
		signed.DeliveryContextSHA != proof.ContextSHA ||
		signed.ActionSchema != action.Schema || signed.ActionID != action.ID ||
		signed.ActionVersion != action.Version {
		return "", false, errors.New("automatic native preflight result is not the exact signed permitted action")
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return "", false, err
	}
	mac, macErr := nativeTypedPreflightReceiptMAC(key, signed)
	for i := range key {
		key[i] = 0
	}
	if macErr != nil || !hmac.Equal([]byte(mac), []byte(signed.ReceiptMAC)) {
		return "", false, errors.New("automatic native preflight result authentication failed")
	}
	return fmt.Sprintf(
		"AIRWORKER AUTOMATIC NATIVE PREFLIGHT (read-only, not a grant): "+
			"session=%s delivery_sha256=%s rule=%s status=%s "+
			"effect=%s native_validator_executed=false signed_receipt=%s "+
			"receipt_sha256=%s. Full validation has NOT passed.",
		id.SessionKey, proof.ContextSHA, result.RuleID, signed.Status,
		signed.Effect, result.ReceiptPath, learnSHA(receiptRaw)), true, nil
}

const (
	nativePreflightPreviousNone     = ""
	nativePreflightPreviousPending  = "pending"
	nativePreflightPreviousTerminal = "terminal"
)

// priorNativePreflightDisposition performs read-only reconciliation of the
// single original signed failure's durable native intent and terminal result.
// The action ID is derived from the ORIGINAL run, review event and procedure,
// not the new GPT session. A completed original action cannot be relabeled as
// a second session's fresh procedure_used. Partial intents stay owned by the
// original delivery; unknown bytes are HOLD, never treated as absent.
func priorNativePreflightDisposition(owner selfLearningOwner, prior ownCLIFailureReceipt,
	rule selfLearningLoadedRule, sourceEvent string) (string, *nativePreflightUseReceipt, error) {
	if prior.RunID == "" || sourceEvent == "" || rule.Target == "" || len(rule.SHA256) != 64 {
		return "", nil, errors.New("native preflight prior identity incomplete")
	}
	identity := learnSHA([]byte(prior.RunID + "\x00" + sourceEvent + "\x00" +
		rule.Target + "@" + rule.SHA256))
	id := "N118-USED-" + identity[:24]
	runtime := owner.Settings.RuntimeRoot
	intentRel := "native-preflight-uses/" + id + ".json"
	resultRel := "native-preflight-results/" + id + ".json"
	intentRaw, intentErr := n093ReadRuntimeEvidence(runtime, intentRel, 32*1024)
	resultRaw, resultErr := n093ReadRuntimeEvidence(runtime, resultRel, 32*1024)
	if intentErr != nil && !errors.Is(intentErr, os.ErrNotExist) {
		return "", nil, fmt.Errorf("read prior native intent: %w", intentErr)
	}
	if resultErr != nil && !errors.Is(resultErr, os.ErrNotExist) {
		return "", nil, fmt.Errorf("read prior native result: %w", resultErr)
	}
	if errors.Is(intentErr, os.ErrNotExist) {
		if resultErr == nil {
			return "", nil, errors.New("terminal native result has no durable prior intent")
		}
		return nativePreflightPreviousNone, nil, nil
	}
	var intent nativePreflightUseReceipt
	if err := json.Unmarshal(intentRaw, &intent); err != nil {
		return "", nil, fmt.Errorf("prior native intent is invalid JSON: %w", err)
	}
	key, err := ownCLIPrivateKey(runtime)
	if err != nil {
		return "", nil, err
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	mac, err := nativeTypedPreflightReceiptMAC(key, intent)
	// Existing 0.11.10 can persist an independently authorized typed native
	// preflight without GPT delivery. Such an authenticated terminal use
	// must block new attribution, not block ordinary catalog loading.
	legacyDelivery := intent.DeliveryContextSHA == "" && intent.DeliveryPrincipal == "" &&
		intent.DeliverySession == "" && intent.DeliveryRunID == ""
	explicitDelivery := intent.DeliveryContextSHA != "" && intent.DeliveryPrincipal != "" &&
		intent.DeliverySession != "" && intent.DeliveryRunID != ""
	if !legacyDelivery && !explicitDelivery {
		return "", nil, errors.New("prior native intent has partial delivery identity")
	}
	if err != nil || intent.ReceiptMAC == "" ||
		!hmac.Equal([]byte(intent.ReceiptMAC), []byte(mac)) ||
		intent.Schema != "air-worker.n093.typed-preflight-intent/v1" ||
		intent.Status != "pending_procedure_used" || intent.RunID != id ||
		intent.OriginalRunID != prior.RunID || intent.OriginalEventID != sourceEvent ||
		intent.RuleID != rule.Target+"@"+rule.SHA256 ||
		intent.ProcedureSHA != rule.SHA256 || intent.Outcome != "pending" ||
		intent.Effect != "NOT_PROVEN" || intent.NativeValidationExecuted {
		return "", nil, errors.New("prior native intent is not an authenticated exact delivery")
	}
	if errors.Is(resultErr, os.ErrNotExist) {
		return nativePreflightPreviousPending, &intent, nil
	}
	var outcome nativePreflightUseReceipt
	if err := json.Unmarshal(resultRaw, &outcome); err != nil {
		return "", nil, fmt.Errorf("prior native result is invalid JSON: %w", err)
	}
	expectedMAC, err := nativeTypedPreflightReceiptMAC(key, outcome)
	if err != nil || outcome.ReceiptMAC == "" ||
		!hmac.Equal([]byte(outcome.ReceiptMAC), []byte(expectedMAC)) ||
		outcome.Schema != "air-worker.n093.typed-preflight-use/v1" ||
		outcome.RunID != intent.RunID || outcome.OriginalRunID != intent.OriginalRunID ||
		outcome.OriginalEventID != intent.OriginalEventID ||
		outcome.RuleID != intent.RuleID || outcome.ProcedureSHA != intent.ProcedureSHA ||
		outcome.DeliveryPrincipal != intent.DeliveryPrincipal ||
		outcome.DeliverySession != intent.DeliverySession ||
		outcome.DeliveryRunID != intent.DeliveryRunID ||
		outcome.DeliveryContextSHA != intent.DeliveryContextSHA ||
		outcome.IntentSHA != learnSHA(intentRaw) ||
		outcome.Status != "deferred_missing_configuration" ||
		outcome.Outcome != "deferred_missing_configuration" ||
		outcome.Effect != "avoided_repeat_validation_with_missing_config" ||
		outcome.NativeValidationExecuted || outcome.ConfirmedEventID == "" {
		return "", nil, errors.New("prior native terminal result is not an authenticated exact outcome")
	}
	return nativePreflightPreviousTerminal, &intent, nil
}
