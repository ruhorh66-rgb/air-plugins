package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveMutationActorRequiresTypedIdentity(t *testing.T) {
	if got, err := resolveMutationActor("gpt-window", "AC·DEV·AirWorker", "owner"); err != nil || got != "gpt-window:AC·DEV·AirWorker" {
		t.Fatalf("typed actor got=%q err=%v", got, err)
	}
	if _, err := resolveMutationActor("gpt-window", "", "owner"); err == nil {
		t.Fatal("missing actor name accepted")
	}
	if got, err := resolveMutationActor("", "", "owner"); err != nil || got != "owner" {
		t.Fatalf("fallback actor got=%q err=%v", got, err)
	}
}

func TestLearnEventGPTActorJSON(t *testing.T) {
	product := t.TempDir()
	code, out := captureLoopOutput(t, func() int {
		return cmdLearnEvent([]string{
			"-product", product,
			"-class", "gpt-smoke",
			"-observed", "GPT window wrote via core",
			"-actor-kind", "gpt-window",
			"-actor", "AC·DEV·AirWorker",
			"-json",
		})
	})
	if code != 0 {
		t.Fatalf("learn event code=%d out=%q", code, out)
	}
	var row learnJournalRecord
	if err := json.Unmarshal([]byte(out), &row); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if row.Actor != "gpt-window:AC·DEV·AirWorker" {
		t.Fatalf("actor=%q", row.Actor)
	}
	rows, err := readLearnJournal(learnPaths(product).Journal)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Actor != row.Actor {
		t.Fatalf("journal=%#v", rows)
	}
}

func TestPlanNodeNewGPTActorJSONDoesNotReplaceOwner(t *testing.T) {
	root := seedPlanNodeProduct(t)
	code, out := captureLoopOutput(t, func() int {
		return cmdPlanNodeNew([]string{
			"-product", root,
			"-title", "GPT core smoke",
			"-parent", "Stage",
			"-owner", "AirWorker",
			"-done-when", "smoke passes",
			"-actor-kind", "gpt-window",
			"-actor", "AC·DEV·AirWorker",
			"-json",
		})
	})
	if code != 0 {
		t.Fatalf("plan node new code=%d out=%q", code, out)
	}
	var doc struct {
		Actor string   `json:"actor"`
		Node  planNode `json:"node"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("json: %v out=%q", err, out)
	}
	if doc.Actor != "gpt-window:AC·DEV·AirWorker" || doc.Node.Owner != "AirWorker" {
		t.Fatalf("doc=%#v", doc)
	}
	raw, err := os.ReadFile(filepath.Join(root, "learn", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"actor":"gpt-window:AC·DEV·AirWorker"`) {
		t.Fatalf("event actor missing: %s", raw)
	}
}
