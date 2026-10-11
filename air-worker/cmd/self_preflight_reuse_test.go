package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The reuse path is release-owned native evidence, not a hook-response or
// an LLM assertion. Matching a trusted prior failure is necessary but is
// deliberately NOT yet proof of procedure_used/outcome/effect.
const n118SafePreflightProcedure = "# Preflight the Canonical Product Configuration\n\n" +
	"## When to apply\nBefore retrying native AirWorker validate after run-config.json was missing.\n\n" +
	"## Procedure\n1. Verify the canonical product root and expected existing configuration.\n" +
	"2. Defer validation while run-config.json is still missing; do not invent a substitute.\n\n" +
	"## Pitfalls\nDo not claim validation succeeded from a deferred retry.\n"

func TestN118ExactPriorFailureBindingAndPrivacy(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	args := []string{"validate", "-product", product, "-json"}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("no isolated owner: %v", err)
	}
	if got, found, err := findPriorSignedPreflightFailure(owner, args); err != nil || found || got.RunID != "" {
		t.Fatalf("no prior failure must remain absent: %+v %v %v", got, found, err)
	}
	run := "AW-CLI-N118-PRIOR-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	rec, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found || rec.RunID != run || rec.CauseCode != "validate-run-config-missing" {
		t.Fatalf("missing verified prior failure: %+v %v %v", rec, found, err)
	}
	if got, found, err := findPriorSignedPreflightFailure(owner,
		[]string{"validate", "-product", product, "-json", "-private=DIFFERENT"}); err != nil || found || got.RunID != "" {
		t.Fatalf("a changed argv was falsely attributed: %+v %v %v", got, found, err)
	}
	path := filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), product) || strings.Contains(string(before), "DIFFERENT") {
		t.Fatal("raw private argv was persisted")
	}
	var forged ownCLIFailureReceipt
	if err := json.Unmarshal(before, &forged); err != nil {
		t.Fatal(err)
	}
	forged.CauseCode = "validate-plan-missing"
	edited, _ := json.Marshal(forged)
	if err := os.WriteFile(path, edited, 0600); err != nil {
		t.Fatal(err)
	}
	if _, found, err := findPriorSignedPreflightFailure(owner, args); err == nil || found {
		t.Fatalf("forged cause was accepted: found=%v err=%v", found, err)
	}
}

func TestN118OnlyActualReviewedAppliedPreflightIsEligible(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("selector absent: %v", err)
	}
	sel.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("owner identity changed: on=%v err=%v", on, err)
	}
	args := []string{"validate", "-product", product, "-json"}
	run := "AW-CLI-N118-REVIEW-TEST-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("signed original failure absent: %v %v", found, err)
	}
	if _, _, eligible, err := findAppliedPreflightRule(owner, prior); err != nil || eligible {
		t.Fatalf("prior run without independent reviewer cannot use skill: %v %v", eligible, err)
	}
	review, err := executeSharedLearning(product, settings, "review", map[string]string{"run_id": run})
	if err != nil || review.Status != "candidate" {
		t.Fatalf("native reviewer did not generate candidate: %+v %v", review, err)
	}
	rule, eventID, eligible, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !eligible || eventID == "" ||
		rule.Target != "skills/learned/air-worker-cli-preflight-n118-test.md" ||
		rule.SHA256 != learnSHA([]byte(n118SafePreflightProcedure)) {
		t.Fatalf("no exact native applied preflight rule: %+v event=%s eligible=%v err=%v", rule, eventID, eligible, err)
	}
	path := filepath.Join(product, filepath.FromSlash(rule.Target))
	if err := os.WriteFile(path, []byte(n118SafePreflightProcedure+"\nUNREVIEWED EXTRA"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, eligible, err := findAppliedPreflightRule(owner, prior); err == nil || eligible {
		t.Fatalf("changed reviewed skill was silently accepted: %v %v", eligible, err)
	}
}
