package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture-only substitute for the trusted production adapter and review-batch
// pair. Production never imports this file, and the helper never modifies
// the shared LEARN journal or invokes the historical one-shot Codex job.
func n093AttestFixtureReviewedProcedure(t *testing.T, product, runID, reviewID string) {
	t.Helper()
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(owner.Selector.ProductRoot, product) {
		t.Fatalf("fixture reviewer owner absent: %v", err)
	}
	var eventRaw []byte
	matches := 0
	err = n093ScanRuntimeEvidenceJSONL(owner.Settings.RuntimeRoot, "events.jsonl", func(raw []byte) error {
		var event struct {
			RunID string `json:"run_id"`
			Kind  string `json:"kind"`
			Class string `json:"class"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return err
		}
		if event.RunID == runID && event.Kind == "run_completed" && event.Class == "air-worker-cli-preflight" {
			matches++
			eventRaw = append([]byte(nil), raw...)
		}
		return nil
	})
	if err != nil || matches != 1 {
		t.Fatalf("fixture source event unavailable: %v count %d", err, matches)
	}
	var event struct {
		EventID string `json:"event_id"`
	}
	if err := json.Unmarshal(eventRaw, &event); err != nil {
		t.Fatal(err)
	}
	rv, _, err := n093ReviewCanonicalRecord(owner, event.EventID)
	if err != nil {
		t.Fatal(err)
	}
	candidate, ok := rv["candidate"].(map[string]any)
	if !ok {
		t.Fatal("fixture reviewer candidate not object")
	}
	target, _ := candidate["target"].(string)
	content, _ := candidate["content"].(string)
	packet := []byte(fmt.Sprintf(`{"event":%s}`, string(eventRaw)))
	if err := n093SealProductionReviewerAnswer(product, packet, target, content,
		learnSHA([]byte("test-fixture-real-reviewer-substitute:"+runID)),
		"test-native-reviewer", "medium"); err != nil {
		t.Fatalf("fixture review model-response HMAC failed: %v", err)
	}
	if err := n093SealCompletedNativeReview(product, runID, reviewID); err != nil {
		t.Fatalf("fixture canonical reviewer completion HMAC failed: %v", err)
	}
}

func TestN093NativeReviewerAbsentSignedAttestationCannotClaimNewUse(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight-n093")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	sel, exists, err := readAirWorkerSelfLearningSelector()
	if err != nil || !exists {
		t.Fatalf("selector %v", err)
	}
	sel.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	run := "AW-CLI-N093-NO-AUTHENTICATED-REVIEW-001"
	args := []string{"validate", "-product", product, "-json"}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	res, err := executeSharedLearning(product, settings, "review", map[string]string{"run_id": run})
	if err != nil || res.Status != "candidate" {
		t.Fatalf("native review fixture %v %+v", err, res)
	}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("owner %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("prior %v", err)
	}
	if _, _, ok, err := findAppliedPreflightRule(owner, prior); err == nil && ok {
		t.Fatal("unattested model-review candidate granted typed native use")
	}
}

func TestN093NativeReviewerAttestationTamperCannotAuthorizeUse(t *testing.T) {
	for _, kind := range []string{"response-run", "response-target", "complete-candidate", "complete-review"} {
		t.Run(kind, func(t *testing.T) {
			_, settings, args, owner, prior, eventID, _ := n093SecurityReviewedFixture(t)
			name := eventID + "-response.json"
			if kind == "complete-candidate" || kind == "complete-review" {
				name = eventID + "-complete.json"
			}
			path := filepath.Join(settings.RuntimeRoot, "review-attestations", name)
			n093EditReviewFixture(t, path, func(m map[string]any) {
				switch kind {
				case "response-run":
					m["source_run_id"] = "AW-CLI-FORGED-REVIEW-RUN"
				case "response-target":
					m["target"] = "skills/learned/air-worker-cli-preflight-forged.md"
				case "complete-candidate":
					m["candidate_sha256"] = strings.Repeat("a", 64)
				case "complete-review":
					m["review_id"] = "RV-FORGED-UNTRUSTED"
				}
			})
			if _, _, allowed, err := findAppliedPreflightRule(owner, prior); err == nil && allowed {
				t.Fatal("tampered native reviewer HMAC authorized learned use")
			}
			if result, err := nativePreflightGuard(args); err == nil && result.Deferred {
				t.Fatalf("tampered native reviewer HMAC led to actual use: %+v", result)
			}
			if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
				t.Fatalf("tampered reviewer recorded procedure_used=%d", got)
			}
		})
	}
}

func TestN093ReviewerResponseProofCannotBeReplacedByCaller(t *testing.T) {
	product, settings, args, _, prior, eventID, rule := n093SecurityReviewedFixture(t)
	packet := []byte("not a valid signed native source callback")
	err := n093SealProductionReviewerAnswer(product, packet, rule.Target,
		n093RealReviewedPreflight, learnSHA(packet), "test-native-reviewer", "medium")
	if err == nil {
		t.Fatal("untrusted caller replaced immutable production reviewer response")
	}
	if _, _, ok, err := func() (selfLearningLoadedRule, string, bool, error) {
		owner, on, err := activeAirWorkerSelfLearningOwner()
		if err != nil || !on {
			t.Fatalf("owner absent %v", err)
		}
		return findAppliedPreflightRule(owner, prior)
	}(); err != nil || !ok {
		t.Fatalf("rejected caller should preserve original authenticated reviewer state: %v %v", ok, err)
	}
	_ = settings
	_ = args
	_ = eventID
}
