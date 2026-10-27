package main

import (
	"encoding/json"
	"errors"
	"fmt"
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
	rule, _, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible {
		return "", false, err
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
