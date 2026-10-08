package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLegacyOperationalRule(t *testing.T, product, proposalID, class, checkSpec string) {
	t.Helper()
	dir := filepath.Join(product, "learn", "rules")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rule := learnRuleRecord{
		Schema: learnRuleSchemaVersion, ProposalID: proposalID, Class: class,
		Rule: "Inspect the original evidence before retrying.", Trigger: "ambiguous execution result",
		CheckType: "gate", CheckSpec: checkSpec,
		TestCase: "ambiguous result -> inspect original evidence before retry",
		Status:   learnApplied, ApprovedAt: now, Approval: "grant:test",
		VerifiedAt: now, VerificationReceipt: "fixture:pass",
	}
	b, _ := json.MarshalIndent(rule, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, proposalID+".json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInitSharedLearningMigratesOperationalRule(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-test-operational", "execution-unknown-no-blind-retry", "operational-procedure-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	report, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "initialized" || len(report.MigratedRules) != 1 || report.Reviewer != "@self" {
		t.Fatalf("unexpected init report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-test-operational.json")); err != nil {
		t.Fatalf("Git seed rule did not survive cutover: %v", err)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "legacy-rules", "LP-test-operational.json")); err != nil {
		t.Fatal(err)
	}
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on {
		t.Fatalf("shared settings unavailable: on=%v err=%v", on, err)
	}
	index, err := executeSharedLearning(product, s, "index", nil)
	if err != nil || index.Status != "ok" || !strings.Contains(string(index.Data), "skills/learned/") {
		t.Fatalf("migrated procedure missing from shared index: %+v %v", index, err)
	}
	status, err := executeSharedLearning(product, s, "status", nil)
	if err != nil || status.Status != "ok" {
		t.Fatalf("shared status failed: %+v %v", status, err)
	}
	again, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil || again.Status != "already_initialized" {
		t.Fatalf("init is not idempotent: %+v %v", again, err)
	}
}

func TestInitSharedLearningRehydratesPackagedManagedProcedure(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-packaged-operational", "execution-unknown-no-blind-retry", "operational-procedure-v1")
	rules, sourcePaths, err := loadLegacyOperationalProcedures(product)
	if err != nil || len(rules) != 1 {
		t.Fatalf("legacy seed discovery: rules=%d err=%v", len(rules), err)
	}
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	intent, err := buildSharedLearningBootstrapIntent(product, runtimeRoot, "air-worker-test", rules, sourcePaths)
	if err != nil || len(intent.Rules) != 1 {
		t.Fatalf("bootstrap intent: %+v err=%v", intent, err)
	}
	procedure := legacyRuleProcedure(rules[0])
	target := filepath.Join(product, filepath.FromSlash(intent.Rules[0].Target))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte(procedure), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := initSharedLearning(product, runtimeRoot, "air-worker-test")
	if err != nil || report.Status != "initialized" {
		t.Fatalf("prepackaged managed procedure did not rehydrate runtime: %+v %v", report, err)
	}
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on {
		t.Fatalf("shared settings after rehydrate: on=%v err=%v", on, err)
	}
	if err := verifySharedBootstrapMigration(product, s, intent.Rules[0]); err != nil {
		t.Fatalf("rehydrated runtime lacks migration provenance: %v", err)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != procedure {
		t.Fatalf("packaged managed procedure changed: %v %q", err, got)
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-packaged-operational.json")); err != nil {
		t.Fatalf("Git seed rule lost during rehydrate: %v", err)
	}
}

func gitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}

func TestLearnedProceduresCheckoutLFWithAutoCRLF(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	repo := t.TempDir()
	gitFixture(t, repo, "init")
	gitFixture(t, repo, "config", "user.email", "fixture@example.invalid")
	gitFixture(t, repo, "config", "user.name", "fixture")
	attrs, err := os.ReadFile(filepath.Join("..", ".gitattributes"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".gitattributes"), attrs, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(repo, "skills", "learned", "fixture.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	lf := []byte(`# Fixture

## When to apply
Always.

## Procedure
1. Check.

## Pitfalls
None.
`)
	if err := os.WriteFile(target, lf, 0o644); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, repo, "add", ".")
	gitFixture(t, repo, "commit", "-m", "fixture")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, repo, "-c", "core.autocrlf=true", "checkout", "--", "skills/learned/fixture.md")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(got, []byte{'\r', '\n'}) || !bytes.Equal(got, lf) {
		t.Fatalf("learned procedure checkout changed LF bytes: %q", got)
	}
}

func TestSharedBootstrapSeedAllowsManagedUpdateAndRollback(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-seed-evolution", "execution-unknown-no-blind-retry", "operational-procedure-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if _, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err != nil {
		t.Fatal(err)
	}
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on {
		t.Fatalf("shared settings after init: on=%v err=%v", on, err)
	}
	intent, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
	if err != nil || !found || len(intent.Rules) != 1 {
		t.Fatalf("bootstrap intent missing: %+v found=%v err=%v", intent, found, err)
	}
	target := intent.Rules[0].Target
	before, err := os.ReadFile(filepath.Join(product, filepath.FromSlash(target)))
	if err != nil {
		t.Fatal(err)
	}
	updated := `# Updated execution reconciliation

## When to apply
When execution state is ambiguous.

## Procedure
1. Correlate the original operation with machine evidence before retrying.

## Pitfalls
Do not use unrelated diagnostics as proof.
`
	res, err := executeSharedLearning(product, s, "propose", map[string]string{
		"proposal_id": "LP-seed-update",
		"kind":        "procedure",
		"target":      target,
		"pre_sha256":  learnSHA(before),
		"content":     updated,
	})
	if err != nil || res.Status != "applied" {
		t.Fatalf("managed update failed: %+v %v", res, err)
	}
	if _, on, err := readSharedLearningSettings(product); err != nil || !on {
		t.Fatalf("seed overlap rejected supported update: on=%v err=%v", on, err)
	}
	res, err = executeSharedLearning(product, s, "rollback", map[string]string{"proposal_id": "LP-seed-update"})
	if err != nil || res.Status != "rolled_back" {
		t.Fatalf("managed rollback failed: %+v %v", res, err)
	}
	if _, on, err := readSharedLearningSettings(product); err != nil || !on {
		t.Fatalf("seed overlap rejected supported rollback: on=%v err=%v", on, err)
	}
}

func TestInitSharedLearningRejectsUnsupportedLegacyRule(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLegacyOperationalRule(t, product, "LP-test-block", "blocking-rule", "custom-block-v1")
	runtimeRoot := filepath.Join(t.TempDir(), "runtime")
	if _, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err == nil {
		t.Fatal("unsupported legacy executable rule was migrated silently")
	}
	if _, err := os.Stat(filepath.Join(product, sharedLearningConfigFile)); !os.IsNotExist(err) {
		t.Fatal("failed init left shared selector behind")
	}
	if _, err := os.Stat(filepath.Join(product, "learn", "rules", "LP-test-block.json")); err != nil {
		t.Fatal("failed init lost legacy rule")
	}
}

func TestRunLearningProcessSelfBound(t *testing.T) {
	t.Setenv("AW_LEARNING_ADAPTER_HELPER", "1")
	product := t.TempDir()
	a := &learningProcessAdapter{
		Executable: "@self", SHA256: "@self",
		Args:      []string{"-test.run=^TestLearningProcessHelper$", "--", "review"},
		TimeoutMS: 5000,
	}
	out, err := runLearningProcess(context.Background(), product, a, json.RawMessage(`{"artifact":"x"}`))
	if err != nil || !strings.Contains(string(out), "kind") {
		t.Fatalf("self-bound adapter failed: %s %v", out, err)
	}
}

func TestN083InterruptedManagedProcedureRecoversBeforeSeedVerification(t *testing.T) {
	for _, mode := range []string{"update", "rollback"} {
		t.Run(mode, func(t *testing.T) {
			product := filepath.Join(t.TempDir(), "product")
			if err := os.MkdirAll(product, 0700); err != nil {
				t.Fatal(err)
			}
			writeLegacyOperationalRule(t, product, "LP-n083-seed", "execution-unknown-no-blind-retry", "operational-procedure-v1")
			runtimeRoot := filepath.Join(t.TempDir(), "runtime")
			if _, err := initSharedLearning(product, runtimeRoot, "air-worker-test"); err != nil {
				t.Fatal(err)
			}
			intent, found, err := readSharedLearningBootstrapIntent(runtimeRoot)
			if err != nil || !found || len(intent.Rules) != 1 {
				t.Fatalf("intent=%+v err=%v", intent, err)
			}
			target := intent.Rules[0].Target
			path := filepath.Join(product, filepath.FromSlash(target))
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after := []byte("# Updated evidence check\n\n## When to apply\nAmbiguous execution.\n\n## Procedure\n1. Inspect exact machine evidence.\n\n## Pitfalls\nNever retry blindly.\n")
			// Real producer order: durable proposal + content/diff, then
			// an apply or rollback WAL. A synthetic WAL without a proposal
			// is correctly rejected by the hardened module.
			proposalID := "custom-update-" + mode
			proposalDir := filepath.Join(runtimeRoot, "proposals")
			if err := os.MkdirAll(proposalDir, 0700); err != nil {
				t.Fatal(err)
			}
			diff := n083FixtureDiff(target, before, after)
			if err := os.WriteFile(filepath.Join(proposalDir, proposalID+".content"), after, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(proposalDir, proposalID+".diff"), diff, 0600); err != nil {
				t.Fatal(err)
			}
			proposal := map[string]any{
				"schema": "air.learning.proposal/v1", "proposal_id": proposalID,
				"product_id": "air-worker-test", "kind": "procedure", "status": "PENDING_LPR",
				"target": target, "before_exists": true,
				"pre_sha256": learnSHA(before), "post_sha256": learnSHA(after),
				"content_ref": "proposals/" + proposalID + ".content", "diff_ref": "proposals/" + proposalID + ".diff",
				"diff_sha256": learnSHA(diff), "at": time.Now().UTC().Format(time.RFC3339Nano),
			}
			proposalRaw, err := json.Marshal(proposal)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(runtimeRoot, "proposals.jsonl"), append(proposalRaw, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			mutationBefore, mutationAfter := before, after
			if mode == "rollback" {
				mutationBefore, mutationAfter = after, before
			}
			txID := "TX-n083-" + mode
			status := "applied"
			if mode == "rollback" {
				status = "rolled_back"
			}
			// Synthetic interrupted transaction must satisfy the exact module
			// WAL contract; otherwise preflight must reject it as poisoned.
			ledger := map[string]any{"schema": "air.learning.ledger/v1", "transaction_id": txID,
				"proposal_id": proposalID, "product_id": "air-worker-test",
				"at":     time.Now().UTC().Format(time.RFC3339Nano),
				"target": target, "kind": "procedure", "status": status,
				"before_exists": true, "after_exists": true,
				"pre_sha256": learnSHA(mutationBefore), "post_sha256": learnSHA(mutationAfter)}
			if mode == "rollback" {
				// A rollback requires an earlier committed apply of the same
				// proposal. Seed history remains intact and immutable.
				prior := map[string]any{
					"schema": "air.learning.ledger/v1", "transaction_id": "TX-n083-prior",
					"product_id": "air-worker-test", "proposal_id": proposalID,
					"target": target, "kind": "procedure", "status": "applied",
					"at":            time.Now().UTC().Format(time.RFC3339Nano),
					"before_exists": true, "after_exists": true,
					"pre_sha256": learnSHA(before), "post_sha256": learnSHA(after),
				}
				priorRaw, err := json.Marshal(prior)
				if err != nil {
					t.Fatal(err)
				}
				ledgerFile, err := os.OpenFile(filepath.Join(runtimeRoot, "ledger.jsonl"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := ledgerFile.Write(append(priorRaw, '\n'))
				closeErr := ledgerFile.Close()
				if writeErr != nil {
					t.Fatal(writeErr)
				}
				if closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			backupRel := "backups/" + learnSHA(mutationBefore)
			ledger["backup_ref"] = backupRel
			backup := filepath.Join(runtimeRoot, filepath.FromSlash(backupRel))
			if err := os.MkdirAll(filepath.Dir(backup), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backup, mutationBefore, 0600); err != nil {
				t.Fatal(err)
			}
			transactionDir := filepath.Join(runtimeRoot, "transactions")
			if err := os.MkdirAll(transactionDir, 0700); err != nil {
				t.Fatal(err)
			}
			wal := map[string]any{"schema": "air.learning.transaction/v1", "product_id": "air-worker-test", "ledger": ledger}
			raw, err := json.Marshal(wal)
			if err != nil {
				t.Fatal(err)
			}
			walPath := filepath.Join(transactionDir, txID+".json")
			if err := os.WriteFile(walPath, raw, 0600); err != nil {
				t.Fatal(err)
			}
			// Emulate the crash after the target was written but before
			// its terminal ledger append, preserving the pre-transaction backup.
			if err := os.WriteFile(path, mutationAfter, 0600); err != nil {
				t.Fatal(err)
			}
			got, on, err := readSharedLearningSettings(product)
			if err != nil || !on || got.RuntimeRoot != runtimeRoot {
				t.Fatalf("recovery blocked settings: on=%v err=%v", on, err)
			}
			restored, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(restored, mutationBefore) {
				t.Fatalf("recovery bytes=%q err=%v", restored, err)
			}
			if _, err := os.Stat(walPath); !os.IsNotExist(err) {
				t.Fatalf("pending WAL after recovery: %v", err)
			}
		})
	}
}

func n083FixtureDiff(target string, before, after []byte) []byte {
	lines := func(b []byte) []string {
		if len(b) == 0 {
			return nil
		}
		s := strings.Split(string(b), "\n")
		if s[len(s)-1] == "" {
			s = s[:len(s)-1]
		}
		return s
	}
	a, z := lines(before), lines(after)
	lineStart := func(n int) int {
		if n == 0 {
			return 0
		}
		return 1
	}
	var out strings.Builder
	fmt.Fprintf(&out, "--- a/%s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n", target, target, lineStart(len(a)), len(a), lineStart(len(z)), len(z))
	for _, line := range a {
		out.WriteString("-" + line + "\n")
	}
	if len(before) > 0 && before[len(before)-1] != '\n' {
		out.WriteString("\\ No newline at end of file\n")
	}
	for _, line := range z {
		out.WriteString("+" + line + "\n")
	}
	if len(after) > 0 && after[len(after)-1] != '\n' {
		out.WriteString("\\ No newline at end of file\n")
	}
	return []byte(out.String())
}
