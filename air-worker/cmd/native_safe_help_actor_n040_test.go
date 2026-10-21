package main

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func n040IsolatedOwner(t *testing.T) selfLearningOwner {
	t.Helper()
	root := t.TempDir()
	runtimeRoot := t.TempDir()
	return selfLearningOwner{
		Selector: airWorkerSelfLearningSelector{
			ProductRoot: root, RuntimeRoot: runtimeRoot,
			ConfigSHA256: strings.Repeat("a", 64),
		},
		Settings: sharedLearningSettings{
			ProductID: "air-worker", RuntimeRoot: runtimeRoot,
		},
	}
}

func TestN040CurrentOSActorAndProcessInstanceStable(t *testing.T) {
	firstSID, err := n040CurrentActorSID()
	if err != nil || firstSID == "" {
		t.Fatalf("current OS actor unavailable: %v", err)
	}
	secondSID, err := n040CurrentActorSID()
	if err != nil || firstSID != secondSID {
		t.Fatalf("OS actor identity changed within one process: %v", err)
	}
	firstID, err := n040CurrentProcessInstance()
	if err != nil || len(firstID) != 32 {
		t.Fatalf("secure process instance ID unavailable: %v", err)
	}
	if _, err := hex.DecodeString(firstID); err != nil {
		t.Fatalf("process nonce not valid hex: %v", err)
	}
	secondID, err := n040CurrentProcessInstance()
	if err != nil || firstID != secondID {
		t.Fatalf("native process instance changed in one process: %v", err)
	}
}

func TestN040SignedActorTamperRejectedByVerifier(t *testing.T) {
	owner := n040IsolatedOwner(t)
	record, _, err := n040RuntimeOpportunity(owner, n040HelpArgs(owner.Selector.ProductRoot), 2)
	if err != nil {
		t.Fatal(err)
	}
	path, err := n040OpportunityPath(owner, record["id"])
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := n040VerifyOpportunity(owner, record["id"], learnSHA(original)); err != nil {
		t.Fatalf("valid native actor not accepted: %v", err)
	}
	actor := record["current_actor_sid"]
	record["current_actor_sid"] = actor + "-forged"
	forged, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	forged = append(forged, '\n')
	if err := os.WriteFile(path, forged, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := n040VerifyOpportunity(owner, record["id"], learnSHA(forged)); err == nil {
		t.Fatal("rehashed false OS actor bypassed signed native identity")
	}
}

func TestN040SameNativeProcessCannotConsumeItsOwnOpportunity(t *testing.T) {
	owner := n040IsolatedOwner(t)
	record, _, err := n040RuntimeOpportunity(owner, n040HelpArgs(owner.Selector.ProductRoot), 2)
	if err != nil {
		t.Fatal(err)
	}
	_, err = nativeN039SafeHelpWithOpportunity(owner, "N040-SAME-PROCESS-01", record["id"])
	if err == nil || !strings.Contains(err.Error(), "different native process instance") {
		t.Fatalf("same process claimed a fresh later learning opportunity: %v", err)
	}
	if _, err := os.Stat(filepath.Join(owner.Settings.RuntimeRoot, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("rejected same-process opportunity wrote learning use: %v", err)
	}
}

func TestN040HardlinkedSignedOpportunityCannotBeReused(t *testing.T) {
	owner := n040IsolatedOwner(t)
	record, _, err := n040RuntimeOpportunity(owner, n040HelpArgs(owner.Selector.ProductRoot), 2)
	if err != nil {
		t.Fatal(err)
	}
	path, err := n040OpportunityPath(owner, record["id"])
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "external-alias.json")
	if err := os.Link(path, outside); err != nil {
		t.Skipf("filesystem does not permit NTFS hardlink fixture: %v", err)
	}
	if _, err := n040VerifyOpportunity(owner, record["id"], learnSHA(data)); err == nil {
		t.Fatal("external NTFS hardlink was accepted as a signed native opportunity")
	}
	if _, _, err := n040ReadOpportunityForUse(owner, record["id"]); err == nil {
		t.Fatal("hardlinked proof was trusted for later learned use")
	}
	if err := os.Remove(outside); err != nil {
		t.Fatal(err)
	}
	if _, err := n040VerifyOpportunity(owner, record["id"], learnSHA(data)); err != nil {
		t.Fatalf("restored ordinary signed receipt rejected: %v", err)
	}
}
