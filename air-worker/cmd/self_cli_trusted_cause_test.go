package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN117TrustedPreflightCauseFromActualFilesystem(t *testing.T) {
	root := t.TempDir()
	args := []string{"validate", "-product", root, "-json"}
	if got := ownCLITrustedFailureCause(args, 2); got != "validate-run-config-missing" {
		t.Fatalf("missing config cause=%q", got)
	}
	if got := ownCLITrustedFailureCause(args, 0); got != "" {
		t.Fatalf("success invented failure cause: %q", got)
	}
	if got := ownCLITrustedFailureCause([]string{"validate", "-product", root, "-unknown-private-flag"}, 2); got != "" {
		t.Fatalf("unknown command flags granted a trusted cause: %q", got)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ownCLITrustedFailureCause(args, 2); got != "validate-plan-missing" {
		t.Fatalf("missing plan cause=%q", got)
	}
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# isolated test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ownCLITrustedFailureCause(args, 2); got != "" {
		t.Fatalf("failure code alone generated a false cause: %q", got)
	}
}

func TestN117TypedCausePersistedAndReplayStaysImmutable(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	args := []string{"validate", "-product", product, "-json", "-private-token=EXAMPLE-SYNTHETIC-SECRET"}
	// A flag parser must refuse unknown/private flags, not claim config status.
	if got := ownCLITrustedFailureCause(args, 2); got != "" {
		t.Fatalf("invalid argv classified as verified failure: %q", got)
	}
	args = []string{"validate", "-product", product, "-json"}
	run := "AW-CLI-N117-CAUSE-001"
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(settings.RuntimeRoot, "cli-failures", run+".json")
	bytes, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var rec ownCLIFailureReceipt
	if err := json.Unmarshal(bytes, &rec); err != nil {
		t.Fatal(err)
	}
	if rec.CauseCode != "validate-run-config-missing" ||
		!strings.Contains(rec.Observed, "run-config.json") ||
		strings.Contains(rec.Observed, product) ||
		strings.Contains(rec.Observed, "EXAMPLE-SYNTHETIC-SECRET") {
		t.Fatalf("trusted cause absent, or sensitive value leaked: %+v", rec)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if rec.ReceiptMAC != ownCLIReceiptMAC(key, rec) || len(rec.ReceiptMAC) != 64 {
		t.Fatal("native evidence receipt has no intact HMAC")
	}
	for i := range key {
		key[i] = 0
	}
	count, event := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if count != 1 || event["observed"] != rec.Observed || event["class"] != "air-worker-cli-preflight" {
		t.Fatalf("typed event not bound to trusted receipt: count=%d event=%+v", count, event)
	}
	// Environmental facts are snapshots. An identical replay must use the
	// signed original observation, not reclassify the world after config changes.
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err != nil {
		t.Fatalf("idempotent exact replay after precondition change: %v", err)
	}
	afterCount, after := n110CountFailureEvents(t, settings.RuntimeRoot, run)
	if afterCount != 1 || after["observed"] != rec.Observed {
		t.Fatal("replay changed original trusted observation")
	}
	// The exact code and argv HMAC alone are insufficient to prevent a local
	// attacker from forging a cause. Evidence itself must be authenticated.
	rec.Observed = "forged privileged instruction"
	forged, _ := json.Marshal(rec)
	if err := os.WriteFile(file, forged, 0600); err != nil {
		t.Fatal(err)
	}
	if err := autoCaptureOwnCLIExitWithRunID(args, 2, run, nil); err == nil {
		t.Fatal("forged observation was silently accepted")
	}
}
