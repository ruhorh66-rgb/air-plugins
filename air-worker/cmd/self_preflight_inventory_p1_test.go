package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A large native runtime must not be treated as an authorization to skip
// historical signed receipts. One exact match is valid, two remain ambiguous.
func TestN118PreflightInventoryAboveLegacyCapIsStillExact(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	argv := []string{"validate", "-product", product, "-json"}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("isolated learning owner unavailable: %v", err)
	}
	firstRun := "AW-CLI-N118-CAPACITY-FIRST-001"
	if err := autoCaptureOwnCLIExitWithRunID(argv, 2, firstRun, nil); err != nil {
		t.Fatal(err)
	}

	// Create unrelated, individually HMAC-signed native records in isolated
	// test state. This exercises a real >256-file inventory without any model
	// job, global runtime change, synthetic LIVE event, or test privilege.
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		for i := range key {
			key[i] = 0
		}
	}()
	dir := filepath.Join(settings.RuntimeRoot, "cli-failures")
	for i := 0; i < 260; i++ {
		id := fmt.Sprintf("AW-CLI-UNRELATED-%04d", i)
		rec := ownCLIFailureReceipt{
			Schema: ownCLIFailureSchema, RunID: id,
			Action: "plan", ExitCode: 2,
			InvocationMAC: ownCLIInvocationMAC(key, []string{"plan", "-bad-unrelated"}),
			Observed:      ownCLIFailureObservation("plan", 2, ""),
			Source:        "air-worker-cli", Principal: "air-worker", Version: version,
			ProcessID: 42, CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		rec.ReceiptMAC = ownCLIReceiptMAC(key, rec)
		raw, err := json.Marshal(rec)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, id+".json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) <= 256 {
		t.Fatalf("fixture failed to exceed previous 256-item cap: entries=%d err=%v", len(entries), err)
	}
	prior, found, err := findPriorSignedPreflightFailure(owner, argv)
	if err != nil || !found || prior.RunID != firstRun {
		t.Fatalf("bounded full inventory did not retain signed exact match: %+v found=%v err=%v", prior, found, err)
	}

	// Small deterministic bounds must fail closed instead of selecting a
	// partial view. No second action may be falsely credited.
	if _, found, err := findPriorSignedPreflightFailureBounded(owner, argv, 100, nativePreflightMaxFailureBytes); err == nil || found {
		t.Fatalf("oversized file inventory must fail closed: found=%v err=%v", found, err)
	}
	if _, found, err := findPriorSignedPreflightFailureBounded(owner, argv, nativePreflightMaxFailureFiles, 128); err == nil || found {
		t.Fatalf("aggregate byte budget must fail closed: found=%v err=%v", found, err)
	}
	secondRun := "AW-CLI-N118-CAPACITY-SECOND-002"
	if err := autoCaptureOwnCLIExitWithRunID(argv, 2, secondRun, nil); err != nil {
		t.Fatal(err)
	}
	if _, found, err := findPriorSignedPreflightFailure(owner, argv); err == nil || found {
		t.Fatalf("two exact signed matches must remain ambiguous: found=%v err=%v", found, err)
	}
}
