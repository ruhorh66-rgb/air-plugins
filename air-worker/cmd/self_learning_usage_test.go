package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func countSelfLearningEvent(t *testing.T, runtimeRoot, kind string) int {
	t.Helper()
	count := 0
	err := scanLearnJSONL(filepath.Join(runtimeRoot, "events.jsonl"), func(raw []byte) error {
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row["kind"] == kind {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAirWorkerSelfLearningLoadedReceiptDoesNotClaimUse(t *testing.T) {
	_, settings, stateDir := selfLearningFixture(t)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "loaded-only", target)
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	rec, _, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found {
		t.Fatalf("context receipt missing: found=%v err=%v", found, err)
	}
	if len(rec.Loaded) != 1 || !strings.Contains(rec.Loaded[0].Target, "execution-unknown-no-blind-retry") || len(rec.Loaded[0].SHA256) != 64 {
		t.Fatalf("exact loaded identity missing: %+v", rec.Loaded)
	}
	if rec.UsedAt != "" || rec.UsedRuleID != "" || rec.Outcome != "" {
		t.Fatalf("loaded was falsely promoted to used: %+v", rec)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("loaded-only context created %d used events", got)
	}
}

func TestAirWorkerSelfLearningExecutionUnknownUseNeedsDiagnosticOutcome(t *testing.T) {
	_, settings, stateDir := selfLearningFixture(t)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "execution-unknown", target)
	in.RunID = "release-run-1"
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	contextRec, contextPath, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || len(contextRec.Loaded) != 1 {
		t.Fatalf("loaded receipt: found=%v err=%v rec=%+v", found, err, contextRec)
	}
	ruleID := contextRec.Loaded[0].Target + "@" + contextRec.Loaded[0].SHA256

	unknown := in
	unknown.HookEventName = "PostToolUse"
	unknown.ToolName = "mcp__AIR_Commander_Test__start_process"
	unknown.ToolInput = json.RawMessage(`{"command":"release mutation"}`)
	unknown.ToolResponse = json.RawMessage(`{"error":"EXECUTION_UNKNOWN","request_id":"fixture-unknown"}`)
	if res, err := handlePostToolUseSelfLearning(unknown); err != nil || strings.TrimSpace(res.Context) != "" {
		t.Fatalf("trigger should not claim use: res=%+v err=%v", res, err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("trigger alone created %d used events", got)
	}

	blindRetry := unknown
	blindRetry.ToolInput = json.RawMessage(`{"command":"same release mutation"}`)
	blindRetry.ToolResponse = json.RawMessage(`{"status":"completed","exit_code":0}`)
	if _, err := handlePostToolUseSelfLearning(blindRetry); err != nil {
		t.Fatal(err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("a repeated start_process was falsely accepted as procedure use: %d", got)
	}

	diagnostic := in
	diagnostic.HookEventName = "PostToolUse"
	diagnostic.ToolName = "mcp__AIR_Commander_Test__list_nodes"
	diagnostic.ToolResponse = json.RawMessage(`{"nodes":[{"node":"SRVLM01","status":"online"}]}`)
	res, err := handlePostToolUseSelfLearning(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Context, "procedure_used") || !strings.Contains(res.Context, ruleID) {
		t.Fatalf("usage context missing exact rule id: %+v", res)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("diagnostic outcome created %d used events", got)
	}

	used, gotPath, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || gotPath != contextPath {
		t.Fatalf("used receipt readback: found=%v path=%s err=%v", found, gotPath, err)
	}
	if used.UsedRuleID != ruleID || used.Outcome != "pass" || used.UsedAt == "" ||
		!strings.Contains(used.OutcomeRef, "tool_response_sha256=") ||
		used.ObservedTool != diagnostic.ToolName {
		t.Fatalf("usage evidence incomplete: %+v", used)
	}

	trigger, _, found, err := readSelfLearningTriggerReceipt(in)
	if err != nil || !found || trigger.ConsumedAt == "" || trigger.DiagnosticTool != diagnostic.ToolName {
		t.Fatalf("trigger was not consumed by diagnostic evidence: found=%v err=%v trigger=%+v", found, err, trigger)
	}

	if _, err := handlePostToolUseSelfLearning(diagnostic); err != nil {
		t.Fatal(err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("consumed trigger produced duplicate usage: %d", got)
	}

	var usage map[string]any
	err = scanLearnJSONL(filepath.Join(settings.RuntimeRoot, "events.jsonl"), func(raw []byte) error {
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row["kind"] == "procedure_used" {
			usage = row
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if usage == nil || usage["rule_id"] != ruleID || usage["outcome"] != "pass" ||
		usage["source"] != "host-post-tool" {
		t.Fatalf("module usage event mismatch: %#v", usage)
	}
}

func TestAirWorkerSelfLearningObservableToolOutcome(t *testing.T) {
	if !observableToolSuccess(json.RawMessage(`{"status":"online"}`)) {
		t.Fatal("structured success was not observable")
	}
	if observableToolSuccess(json.RawMessage(`{"error":"EXECUTION_UNKNOWN"}`)) {
		t.Fatal("unknown execution was treated as success")
	}
	if observableToolSuccess(json.RawMessage(`{"exit_code":1}`)) {
		t.Fatal("nonzero exit was treated as success")
	}
	if !observableToolSuccess(json.RawMessage(`{"exit_code":0}`)) {
		t.Fatal("zero exit was not treated as success")
	}
}
