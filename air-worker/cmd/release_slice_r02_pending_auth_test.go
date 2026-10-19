package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func r02SignedPendingFixture(t *testing.T) (string, selfLearningOwner, []byte, string, string, r02PendingAttempt) {
	t.Helper()
	product, planRaw, _ := releaseSliceTransitionFixture(t)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(owner.Selector.ProductRoot, product) {
		t.Fatalf("R02 pending fixture owner not enabled: %v", err)
	}
	scratch := filepath.Join(t.TempDir(), "isolated-capture-scratch")
	if err := os.Mkdir(scratch, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMPDIR", scratch)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	binSHA := learnSHA(binary)
	attempt, found, err := loadOrCreateR02Pending(owner, learnSHA(planRaw), binSHA, scratch)
	if err != nil || found {
		t.Fatalf("cannot create first native signed R02 pending attempt: found=%v err=%v", found, err)
	}
	if attempt.Schema != r02PendingSchema || attempt.ProductID != "air-worker" ||
		attempt.PendingHMAC == "" || attempt.CandidateBinarySHA256 != binSHA {
		t.Fatalf("R02 pending fixture is not signed by current native binary: %+v", attempt)
	}
	path := r02PendingPath(owner.Settings.RuntimeRoot)
	if _, read, err := readR02Pending(path); err != nil || !read {
		t.Fatalf("signed native pending marker not readable: %v", err)
	}
	return product, owner, planRaw, binSHA, path, attempt
}

func TestR02SignedPendingRecoverSameSourceOnceAndCleanup(t *testing.T) {
	_, owner, planRaw, binarySHA, path, initial := r02SignedPendingFixture(t)
	recovered, found, err := loadOrCreateR02Pending(owner, learnSHA(planRaw), binarySHA, filepath.Dir(initial.TargetRoot))
	if err != nil || !found {
		t.Fatalf("same-source signed pending marker not recoverable: found=%v err=%v", found, err)
	}
	if recovered.PendingHMAC != initial.PendingHMAC || recovered.TargetRoot != initial.TargetRoot {
		t.Fatal("pending recovery created a different release attempt")
	}
	if _, _, captured, err := r02PendingNewReceipt(owner, recovered); err != nil || captured {
		t.Fatalf("pending recovery falsely credits a not-yet-run validate: captured=%v err=%v", captured, err)
	}
	if err := cleanupR02Pending(owner, recovered); err != nil {
		t.Fatalf("signed pending cleanup failed: %v", err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("cleaned pending marker still exists: %v", err)
	}
	if _, err := os.Lstat(initial.TargetRoot); !os.IsNotExist(err) {
		t.Fatalf("cleaned disposable pending target still exists: %v", err)
	}
}

func TestR02ChangedInterruptedPendingCannotCreditStaleCandidate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*r02PendingAttempt)
	}{
		{"unsigned-old-schema", func(p *r02PendingAttempt) { p.Schema = "air-worker.release-slice.r02-pending/v1"; p.PendingHMAC = "" }},
		{"different-binary", func(p *r02PendingAttempt) { p.CandidateBinarySHA256 = strings.Repeat("a", 64) }},
		{"different-plan", func(p *r02PendingAttempt) { p.PlanBeforeSHA256 = strings.Repeat("b", 64) }},
		{"changed-baseline-inventory", func(p *r02PendingAttempt) { p.BaselineReceipts["AW-CLI-old-capture.json"] = strings.Repeat("c", 64) }},
		{"substituted-product", func(p *r02PendingAttempt) { p.ProductRoot += "-other" }},
		{"stale-valid-timestamp", func(p *r02PendingAttempt) { p.StartedAtUTC = "2020-01-01T00:00:00Z" }},
		{"empty-MAC", func(p *r02PendingAttempt) { p.PendingHMAC = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			product, owner, planRaw, binSHA, path, original := r02SignedPendingFixture(t)
			tc.mutate(&original)
			modified, err := json.MarshalIndent(original, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, append(modified, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if _, found, err := loadOrCreateR02Pending(owner, learnSHA(planRaw), binSHA, filepath.Dir(original.TargetRoot)); err == nil {
				t.Fatalf("stale or edited pending marker was credited (found=%v)", found)
			}
			// Untrusted recovery and cleanup cannot erase a target or accept
			// the old capture as a newly authorized release operation.
			cleanupR02PendingBestEffort(owner)
			if _, err := os.Stat(path); err != nil {
				t.Fatal("invalid marker removed instead of rejected")
			}
			if _, err := os.Stat(filepath.Join(product, "plan", "release-slices.json")); err != nil {
				t.Fatal("invalid pending recovery touched original plan")
			}
			if _, err := os.Stat(filepath.Join(product, "docs", "receipts", "RELEASE_0_11_9_02.json")); !os.IsNotExist(err) {
				t.Fatalf("invalid pending marker published a release receipt: %v", err)
			}
		})
	}
}

func TestR02PendingRejectsExternalHardlinkAndDuplicateJSONKey(t *testing.T) {
	t.Run("external-hardlink", func(t *testing.T) {
		_, owner, planRaw, binSHA, path, attempt := r02SignedPendingFixture(t)
		outside := filepath.Join(t.TempDir(), "external-linked-pending.json")
		if err := os.Link(path, outside); err != nil {
			t.Skipf("NTFS hardlink creation unavailable without privilege: %v", err)
		}
		if _, found, err := loadOrCreateR02Pending(owner, learnSHA(planRaw), binSHA, filepath.Dir(attempt.TargetRoot)); err == nil {
			t.Fatalf("external hardlink could be used as signed pending: found=%v", found)
		}
	})
	t.Run("duplicate-JSON-key", func(t *testing.T) {
		_, owner, planRaw, binSHA, path, attempt := r02SignedPendingFixture(t)
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		mutated := append([]byte("{\"schema\":\"air-worker.release-slice.r02-pending/v1\",\n"), body[1:]...)
		if err := os.WriteFile(path, mutated, 0600); err != nil {
			t.Fatal(err)
		}
		if _, found, err := loadOrCreateR02Pending(owner, learnSHA(planRaw), binSHA, filepath.Dir(attempt.TargetRoot)); err == nil {
			t.Fatalf("duplicated pending identity was accepted: found=%v", found)
		}
	})
}
