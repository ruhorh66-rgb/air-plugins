package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// These tests exercise disposable module-owned fixtures only. The production
// reviewer and the previous B2 one-shot job are never called or replayed.
func n093SecurityReviewedFixture(t *testing.T) (string, sharedLearningSettings, []string, selfLearningOwner, ownCLIFailureReceipt, string, selfLearningLoadedRule) {
	t.Helper()
	product, settings, args, rule := n093NativeTypedFixture(t)
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("fixture owner: %v", err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, args)
	if err != nil || !found {
		t.Fatalf("fixture signed prior: %v", err)
	}
	_, eventID, good, err := findAppliedPreflightRule(owner, prior)
	if err != nil || !good {
		t.Fatalf("fixture approved review: %v %v", good, err)
	}
	return product, settings, args, owner, prior, eventID, rule
}

func n093EditReviewFixture(t *testing.T, file string, modify func(map[string]any)) {
	t.Helper()
	b, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(b, &data); err != nil {
		t.Fatal(err)
	}
	modify(data)
	out, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, out, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestN093ReviewIndexWrongRunRejectsNativeUse(t *testing.T) {
	_, settings, _, owner, prior, eventID, _ := n093SecurityReviewedFixture(t)
	path := filepath.Join(settings.RuntimeRoot, "reviews", eventID+".json")
	n093EditReviewFixture(t, path, func(r map[string]any) { r["run_id"] = "AW-CLI-OTHER-RUN" })
	if _, _, allowed, err := findAppliedPreflightRule(owner, prior); err == nil && allowed {
		t.Fatal("P1: forged event-index review RUN credited to signed source")
	}
}

func TestN093ReviewIndexWrongCandidateDigestRejectsNativeUse(t *testing.T) {
	_, settings, _, owner, prior, eventID, _ := n093SecurityReviewedFixture(t)
	path := filepath.Join(settings.RuntimeRoot, "reviews", eventID+".json")
	n093EditReviewFixture(t, path, func(r map[string]any) {
		r["candidate_sha256"] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	})
	if _, _, allowed, err := findAppliedPreflightRule(owner, prior); err == nil && allowed {
		t.Fatal("P1: forged event-index candidate digest credited to source")
	}
}

func TestN093ReviewCanonicalWrongRunRejectsNativeUse(t *testing.T) {
	_, settings, _, owner, prior, eventID, _ := n093SecurityReviewedFixture(t)
	var index struct {
		ReviewID string `json:"review_id"`
	}
	if err := readJSON(filepath.Join(settings.RuntimeRoot, "reviews", eventID+".json"), &index); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(settings.RuntimeRoot, "reviews", index.ReviewID+".json")
	n093EditReviewFixture(t, path, func(r map[string]any) { r["run_id"] = "AW-CLI-FORGED-OTHER" })
	if _, _, allowed, err := findAppliedPreflightRule(owner, prior); err == nil && allowed {
		t.Fatal("P1: changed canonical reviewer record not checked")
	}
}

func TestN093LinkedRuntimeProvenanceCannotAuthorizeUse(t *testing.T) {
	for _, which := range []string{"review-event", "review-canonical", "events", "ledger"} {
		t.Run(which, func(t *testing.T) {
			_, settings, _, owner, prior, eventID, _ := n093SecurityReviewedFixture(t)
			rt := settings.RuntimeRoot
			source := ""
			switch which {
			case "review-event":
				source = filepath.Join(rt, "reviews", eventID+".json")
			case "review-canonical":
				var row struct {
					ReviewID string `json:"review_id"`
				}
				if err := readJSON(filepath.Join(rt, "reviews", eventID+".json"), &row); err != nil {
					t.Fatal(err)
				}
				source = filepath.Join(rt, "reviews", row.ReviewID+".json")
			case "events":
				source = filepath.Join(rt, "events.jsonl")
			case "ledger":
				source = filepath.Join(rt, "ledger.jsonl")
			}
			original, err := os.ReadFile(source)
			if err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "external")
			if err := os.WriteFile(outside, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(source); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, source); err != nil {
				// Creating file symlinks requires a Windows privilege on some
				// hosts. Never request elevation merely for a negative test.
				t.Skipf("Windows file symlink unavailable without elevation: %v", err)
			}
			if _, _, allowed, err := findAppliedPreflightRule(owner, prior); err == nil && allowed {
				t.Fatalf("P1: external linked %s reviewer evidence wrongly authorized native use", which)
			}
		})
	}
}

func TestN093WindowsReviewJunctionCannotAuthorizeNativeUse(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows NTFS junction fixture only")
	}
	_, settings, args, owner, prior, _, _ := n093SecurityReviewedFixture(t)
	root := settings.RuntimeRoot
	originalDir := filepath.Join(root, "reviews")
	externalDir := filepath.Join(t.TempDir(), "off-root-review-data")
	if err := os.Rename(originalDir, externalDir); err != nil {
		t.Skipf("junction fixture cannot relocate disposable reviews: %v", err)
	}
	out, err := exec.Command("cmd", "/c", "mklink", "/J", originalDir, externalDir).CombinedOutput()
	if err != nil {
		t.Skipf("safe directory junction fixture unsupported: %v, %s", err, string(out))
	}
	if _, _, eligible, err := findAppliedPreflightRule(owner, prior); err == nil && eligible {
		t.Fatal("P1: external review junction was trusted as local module proof")
	}
	result, err := nativePreflightGuard(args)
	if err == nil && result.Deferred {
		t.Fatalf("junction-backed reviewer authorized actual native use: %+v", result)
	}
	if count := n104Events(t, settings.RuntimeRoot, "procedure_used"); count != 0 {
		t.Fatalf("review junction created a false native use event count=%d", count)
	}
}

func TestN093HardlinkedReviewerProvenanceCannotAuthorizeNativeUse(t *testing.T) {
	for _, which := range []string{"event-index", "canonical-RV", "events", "ledger"} {
		t.Run(which, func(t *testing.T) {
			_, settings, args, owner, prior, eventID, _ := n093SecurityReviewedFixture(t)
			rt := settings.RuntimeRoot
			path := ""
			switch which {
			case "event-index":
				path = filepath.Join(rt, "reviews", eventID+".json")
			case "canonical-RV":
				var index struct {
					ReviewID string `json:"review_id"`
				}
				if err := readJSON(filepath.Join(rt, "reviews", eventID+".json"), &index); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(rt, "reviews", index.ReviewID+".json")
			case "events":
				path = filepath.Join(rt, "events.jsonl")
			case "ledger":
				path = filepath.Join(rt, "ledger.jsonl")
			}
			external := filepath.Join(t.TempDir(), "out-of-runtime-hardlink")
			if err := os.Link(path, external); err != nil {
				t.Skipf("filesystem does not support external hardlink fixture: %v", err)
			}
			if _, _, eligible, err := findAppliedPreflightRule(owner, prior); err == nil && eligible {
				t.Fatal("NTFS hardlinked external evidence authorized native use")
			}
			res, err := nativePreflightGuard(args)
			if err == nil && res.Deferred {
				t.Fatalf("NTFS external evidence caused use: %+v", res)
			}
			if count := n104Events(t, settings.RuntimeRoot, "procedure_used"); count != 0 {
				t.Fatalf("hardlink emitted false use=%d", count)
			}
		})
	}
}

// The journal is redirected ONLY while the shared skill_loaded verifier runs.
// The genuine module journal is restored before any subsequent native check.
// This reproduces the Astra high trust-boundary finding without UAC or a
// production/runtime mutation: NTFS hardlinks permit the disposable fixture.
func TestN093ForgedExternalSkillLoadedOnlyDuringCheckCannotClaimUse(t *testing.T) {
	_, settings, _, _, prior, eventID, rule := n093SecurityReviewedFixture(t)
	runtimeRoot := settings.RuntimeRoot
	ruleID := rule.Target + "@" + rule.SHA256
	id := learnSHA([]byte(prior.RunID + "\x00" + eventID + "\x00" + ruleID))
	loadID := "N118-LOADED-" + id[:24]
	local := filepath.Join(runtimeRoot, "events.jsonl")
	original, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	if found, err := nativeSelfSkillLoaded(runtimeRoot, loadID, rule); err != nil || found {
		t.Fatalf("unloaded skill already credited: found=%v err=%v", found, err)
	}
	fake := map[string]any{
		"schema": "air.learning.usage/v1", "event_id": "EV-FORGED-LOAD-FROM-EXTERNAL-001",
		"run_id": loadID, "product_id": "air-worker", "kind": "skill_loaded",
		"target": rule.Target, "sha256": rule.SHA256, "outcome": "loaded",
	}
	line, err := json.Marshal(fake)
	if err != nil {
		t.Fatal(err)
	}
	copied := append([]byte(nil), original...)
	if len(copied) > 0 && copied[len(copied)-1] != '\n' {
		copied = append(copied, '\n')
	}
	copied = append(append(copied, line...), '\n')
	external := filepath.Join(t.TempDir(), "injected-external-events.jsonl")
	if err := os.WriteFile(external, copied, 0600); err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(runtimeRoot, ".events-original-snapshot-for-load-verification")
	if err := os.Rename(local, backup); err != nil {
		t.Fatal(err)
	}
	restored := false
	restore := func() {
		if restored {
			return
		}
		if _, err := os.Lstat(local); err == nil {
			if err := os.Remove(local); err != nil {
				t.Fatalf("cannot remove disposable hardlink: %v", err)
			}
		}
		if err := os.Rename(backup, local); err != nil {
			t.Fatalf("cannot restore original module journal: %v", err)
		}
		restored = true
	}
	defer restore()
	if err := os.Link(external, local); err != nil {
		restore()
		t.Skipf("disposable cross-directory hardlink unavailable: %v", err)
	}
	// In the old implementation scanLearnJSONL(os.Open) returned loaded=true,
	// even though its evidence came from an external hardlinked file.
	loaded, readErr := nativeSelfSkillLoaded(runtimeRoot, loadID, rule)
	restore()
	current, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}
	if learnSHA(current) != learnSHA(original) {
		t.Fatal("actual module journal was changed by the attack fixture")
	}
	if readErr == nil && loaded {
		t.Fatal("P1: external linked skill_loaded was accepted as a module-owned operation")
	}
	if uses := n104Events(t, runtimeRoot, "procedure_used"); uses != 0 {
		t.Fatalf("forged load led to %d false native use events", uses)
	}
	if _, err := os.Stat(filepath.Join(runtimeRoot, "native-preflight-results")); !os.IsNotExist(err) {
		t.Fatalf("forged load created a terminal native result: %v", err)
	}
}
