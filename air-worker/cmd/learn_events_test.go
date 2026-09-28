package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLearnPathsUseProductLearnDirectly(t *testing.T) {
	product := filepath.Join(t.TempDir(), "product")
	paths := learnPaths(product)
	want := filepath.Join(product, "learn")
	if paths.Durable != want || paths.Journal != filepath.Join(want, "events.jsonl") || paths.Proposals != filepath.Join(want, "proposals.jsonl") {
		t.Fatalf("durable paths=%#v want root %q", paths, want)
	}
	if strings.Contains(paths.Durable, filepath.Join("product", "product", "learn")) {
		t.Fatalf("nested product path returned: %s", paths.Durable)
	}
	if paths.Root != filepath.Join(product, ".air-worker", "learn") {
		t.Fatalf("runtime root moved: %s", paths.Root)
	}
	if paths.Rules != filepath.Join(paths.Root, "RULES.md") || paths.Ledger != filepath.Join(paths.Root, "ledger.jsonl") || paths.Blobs != filepath.Join(paths.Root, "blobs") {
		t.Fatalf("runtime learning paths moved: %#v", paths)
	}
}

func TestLearnAddAndEventWriteValidatedEvents(t *testing.T) {
	product := t.TempDir()
	if code := cmdLearnAdd([]string{"-product", product, "-class", "scope", "-observed", "first"}); code != 0 {
		t.Fatalf("learn add failed with %d", code)
	}
	if code := cmdLearnEvent([]string{"-product", product, "-class", "delivery", "-observed", "second", "-kind", "check", "-source", "judge"}); code != 0 {
		t.Fatalf("learn event failed with %d", code)
	}
	rows, err := readLearnJournal(learnPaths(product).Journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].Kind != "lesson" || rows[0].Source != "worker" || rows[1].Kind != "check" || rows[1].Source != "judge" {
		t.Fatalf("unexpected events: %#v", rows)
	}
	if code := cmdLearnEvent([]string{"-product", product, "-class", "bad", "-observed", "bad", "-kind", "unknown"}); code != 2 {
		t.Fatalf("invalid event exit=%d want 2", code)
	}
	rows, _ = readLearnJournal(learnPaths(product).Journal)
	if len(rows) != 2 {
		t.Fatalf("invalid event mutated journal: %#v", rows)
	}
}

func TestAirCuratorImportIsStableLosslessAndReadOnly(t *testing.T) {
	product := t.TempDir()
	source := filepath.Join(t.TempDir(), "journal.jsonl")
	line := `{"ts":"2026-09-25","situation":"miss","curator":"AirCurator","lpr":"LPR-1","class":"scope","lesson":"stay in scope"}`
	original := []byte(line + "\n" + line + "\n")
	if err := os.WriteFile(source, original, 0o644); err != nil {
		t.Fatal(err)
	}
	beforeHash := learnSHA(original)
	target := learnPaths(product).Journal
	added, err := importLegacyEvents(target, source, "aircurator")
	if err != nil || added != 2 {
		t.Fatalf("first import added=%d err=%v", added, err)
	}
	after, err := os.ReadFile(source)
	if err != nil || learnSHA(after) != beforeHash || string(after) != string(original) {
		t.Fatalf("source changed: err=%v before=%q after=%q", err, original, after)
	}
	rows, err := readLearnEventsNoMigration(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID == rows[1].ID || rows[0].CreatedAt != "2026-09-25T00:00:00Z" || rows[1].CreatedAt != "2026-09-25T00:00:00Z" {
		t.Fatalf("duplicate/date import lost identity: %#v", rows)
	}
	if added, err = importLegacyEvents(target, source, "aircurator"); err != nil || added != 0 {
		t.Fatalf("second import added=%d err=%v", added, err)
	}
	copyAtAnotherPath := filepath.Join(t.TempDir(), "same-journal.jsonl")
	if err := os.WriteFile(copyAtAnotherPath, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if added, err = importLegacyEvents(target, copyAtAnotherPath, "aircurator"); err != nil || added != 0 {
		t.Fatalf("source path changed import identity: added=%d err=%v", added, err)
	}
}

func TestLegacyImportFailsBeforeTargetMutation(t *testing.T) {
	product := t.TempDir()
	target := learnPaths(product).Journal
	seed := learnJournalRecord{Schema: learnSchemaVersion, ID: "LE-seed", CreatedAt: "2026-09-25T00:00:00Z", Class: "scope", Observed: "seed", Kind: "lesson", Source: "worker"}
	if err := appendLearnJSON(target, seed); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(target)
	if _, err := importLegacyEvents(target, filepath.Join(t.TempDir(), "missing.jsonl"), "aircurator"); err == nil {
		t.Fatal("missing source was accepted")
	}
	if _, err := importLegacyEvents(target, t.TempDir(), "aircurator"); err == nil {
		t.Fatal("directory source was accepted")
	}
	invalid := filepath.Join(t.TempDir(), "invalid.jsonl")
	data := []byte("{\"ts\":\"2026-09-25\",\"class\":\"scope\",\"lesson\":\"valid\"}\nnot-json\n")
	if err := os.WriteFile(invalid, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := importLegacyEvents(target, invalid, "aircurator"); err == nil {
		t.Fatal("invalid source was accepted")
	}
	after, _ := os.ReadFile(target)
	if string(after) != string(before) {
		t.Fatalf("failed import mutated target\nbefore=%q\nafter=%q", before, after)
	}
}

func TestProductJournalMigrationIsIdempotentAndLossless(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	legacy := filepath.Join(paths.Root, "journal.jsonl")
	old := learnJournalRecord{Schema: learnSchemaVersion, ID: "LR-old", CreatedAt: "2026-09-24T12:00:00Z", Class: "delivery", Observed: "verify delivery", Evidence: "receipt"}
	if err := appendLearnJSON(legacy, old); err != nil {
		t.Fatal(err)
	}
	if err := appendLearnJSON(legacy, old); err != nil {
		t.Fatal(err)
	}
	rows, err := readLearnJournal(paths.Journal)
	if err != nil || len(rows) != 2 || rows[0].ID == rows[1].ID || rows[0].Reference != old.ID || rows[0].Observed != old.Observed {
		t.Fatalf("migration rows=%#v err=%v", rows, err)
	}
	first, _ := os.ReadFile(paths.Journal)
	rows, err = readLearnJournal(paths.Journal)
	second, _ := os.ReadFile(paths.Journal)
	if err != nil || len(rows) != 2 || string(first) != string(second) {
		t.Fatalf("migration was not idempotent rows=%#v err=%v", rows, err)
	}
}

func TestLegacyProposalMigrationAndConflict(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	legacy := filepath.Join(paths.Root, "proposals.jsonl")
	old := learnProposal{Schema: learnSchemaVersion, ID: "LP-old", CreatedAt: "2026-09-25T00:00:00Z", Status: learnPending, Class: "scope", Rule: "keep scope"}
	if err := appendLearnJSON(legacy, old); err != nil {
		t.Fatal(err)
	}
	rows, err := readLearnProposals(paths.Proposals)
	if err != nil || len(rows) != 1 || rows[0].Status != learnPending || rows[0].Rule != old.Rule {
		t.Fatalf("proposal migration rows=%#v err=%v", rows, err)
	}
	first, _ := os.ReadFile(paths.Proposals)
	if _, err := readLearnProposals(paths.Proposals); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(paths.Proposals)
	if string(first) != string(second) {
		t.Fatal("proposal migration was not idempotent")
	}
	rows[0].Status = learnApplied
	rows[0].AppliedAt = "2026-09-26T00:00:00Z"
	advanced, _ := marshalLearnJSONL(rows)
	if err := writeLearnAtomic(paths.Proposals, advanced); err != nil {
		t.Fatal(err)
	}
	if rows, err = readLearnProposals(paths.Proposals); err != nil || rows[0].Status != learnApplied {
		t.Fatalf("stale legacy status blocked canonical lifecycle: rows=%#v err=%v", rows, err)
	}

	canonicalOldProduct := t.TempDir()
	canonicalOldPath := learnPaths(canonicalOldProduct).Proposals
	rolledBack := old
	rolledBack.Status = learnRolledBack
	if err := appendLearnJSON(canonicalOldPath, rolledBack); err != nil {
		t.Fatal(err)
	}
	canonicalOld, err := readLearnProposals(canonicalOldPath)
	if err != nil || len(canonicalOld) != 1 || canonicalOld[0].Status != learnRevoked {
		t.Fatalf("canonical ROLLED_BACK was not normalized: rows=%#v err=%v", canonicalOld, err)
	}

	conflictProduct := t.TempDir()
	conflictPaths := learnPaths(conflictProduct)
	canonical := learnProposal{Schema: learnSchemaVersion, ID: "LP-conflict", CreatedAt: old.CreatedAt, Status: learnPending, Class: "scope", Rule: "canonical"}
	legacyConflict := canonical
	legacyConflict.Rule = "legacy"
	if err := appendLearnJSON(conflictPaths.Proposals, canonical); err != nil {
		t.Fatal(err)
	}
	if err := appendLearnJSON(filepath.Join(conflictPaths.Root, "proposals.jsonl"), legacyConflict); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(conflictPaths.Proposals)
	if _, err := readLearnProposals(conflictPaths.Proposals); err == nil {
		t.Fatal("proposal ID conflict was accepted")
	}
	after, _ := os.ReadFile(conflictPaths.Proposals)
	if string(before) != string(after) {
		t.Fatal("proposal conflict mutated canonical registry")
	}
}
