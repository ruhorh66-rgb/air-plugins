package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTransferAirWorkerSelfLearningOwnerPreservesRuntimeAndAudits(t *testing.T) {
	ownerProduct := filepath.Join(t.TempDir(), "owner")
	targetProduct := filepath.Join(t.TempDir(), "candidate")
	if err := os.MkdirAll(ownerProduct, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetProduct, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv(hookStateDirEnv, state)
	runtime := filepath.Join(t.TempDir(), "runtime")
	if code := cmdLearnSelf([]string{"enable", "-product", ownerProduct, "-runtime-root", runtime, "-product-id", "air-worker-test"}); code != 0 {
		t.Fatalf("owner enable=%d", code)
	}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled {
		t.Fatalf("initial self owner: enabled=%v err=%v", enabled, err)
	}
	if _, err := transferAirWorkerSelfLearningOwner(targetProduct); err == nil {
		t.Fatal("unverified target accepted")
	}
	after, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled || !sameLearningPath(after.Selector.ProductRoot, owner.Selector.ProductRoot) {
		t.Fatalf("owner changed: %+v %v %v", after.Selector, enabled, err)
	}
	if _, err := transferAirWorkerSelfLearningOwner(ownerProduct); err != nil {
		t.Fatalf("same-owner idempotency: %v", err)
	}
}

func TestTransferAirWorkerSelfLearningOwnerRejectsUnbootstrappedTarget(t *testing.T) {
	ownerProduct := filepath.Join(t.TempDir(), "owner")
	targetProduct := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(ownerProduct, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(targetProduct, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	t.Setenv(hookStateDirEnv, state)
	runtime := filepath.Join(t.TempDir(), "runtime")
	if code := cmdLearnSelf([]string{"enable", "-product", ownerProduct, "-runtime-root", runtime, "-product-id", "air-worker-test"}); code != 0 {
		t.Fatalf("owner enable=%d", code)
	}
	if _, err := transferAirWorkerSelfLearningOwner(targetProduct); err == nil {
		t.Fatal("unbootstrapped target was accepted")
	}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled || !sameLearningPath(owner.Selector.ProductRoot, ownerProduct) {
		t.Fatalf("owner changed after rejected transfer: %+v enabled=%v err=%v", owner.Selector, enabled, err)
	}
}

func TestTransferAirWorkerSelfLearningOwnerRejectsDifferentRuntime(t *testing.T) {
	ownerProduct, ownerSettings, _ := selfLearningFixture(t)
	targetProduct, _ := sharedProductFixture(t, false)
	if _, err := transferAirWorkerSelfLearningOwner(targetProduct); err == nil {
		t.Fatal("different runtime was accepted")
	}
	owner, enabled, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !enabled || !sameLearningPath(owner.Selector.ProductRoot, ownerProduct) || !sameLearningPath(owner.Selector.RuntimeRoot, ownerSettings.RuntimeRoot) {
		t.Fatalf("owner changed after rejected transfer: %+v enabled=%v err=%v", owner.Selector, enabled, err)
	}
}
