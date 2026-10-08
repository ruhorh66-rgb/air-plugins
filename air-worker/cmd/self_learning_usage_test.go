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

func TestAirWorkerSelfLearningExecutionUnknownUseNeedsCorrelatedDiagnosticOutcome(t *testing.T) {
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
	unknown.ToolInput = json.RawMessage(`{"node":"SRVLM01","command":"release mutation","shell":"powershell"}`)
	unknown.ToolResponse = json.RawMessage(`{"error":"EXECUTION_UNKNOWN","request_id":"fixture-unknown","pid":4242}`)
	if res, err := handlePostToolUseSelfLearning(unknown); err != nil || strings.TrimSpace(res.Context) != "" {
		t.Fatalf("trigger should not claim use: res=%+v err=%v", res, err)
	}

	for name, toolInput := range map[string]string{
		"wrong-pid":  `{"node":"SRVLM01","pid":9999}`,
		"wrong-node": `{"node":"AIR-ENV-002","pid":4242}`,
	} {
		t.Run(name, func(t *testing.T) {
			diagnostic := in
			diagnostic.HookEventName = "PostToolUse"
			diagnostic.ToolName = "mcp__AIR_Commander_Test__read_process_output"
			diagnostic.ToolInput = json.RawMessage(toolInput)
			diagnostic.ToolResponse = json.RawMessage(`{"status":"completed","exit_code":0}`)
			if res, err := handlePostToolUseSelfLearning(diagnostic); err != nil || strings.TrimSpace(res.Context) != "" {
				t.Fatalf("uncorrelated diagnostic claimed use: res=%+v err=%v", res, err)
			}
		})
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("uncorrelated diagnostics created %d used events", got)
	}

	diagnostic := in
	diagnostic.HookEventName = "PostToolUse"
	diagnostic.ToolName = "mcp__AIR_Commander_Test__read_process_output"
	diagnostic.ToolInput = json.RawMessage(`{"node":"SRVLM01","pid":4242}`)
	diagnostic.ToolResponse = json.RawMessage(`{"status":"completed","exit_code":0,"pid":4242}`)
	res, err := handlePostToolUseSelfLearning(diagnostic)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Context, "procedure_used") || !strings.Contains(res.Context, ruleID) {
		t.Fatalf("usage context missing exact rule id: %+v", res)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("correlated diagnostic outcome created %d used events", got)
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
	if err != nil || !found || trigger.ConsumedAt == "" ||
		trigger.DiagnosticTool != diagnostic.ToolName ||
		!strings.Contains(trigger.DiagnosticEvidence, "pid=4242") {
		t.Fatalf("trigger was not consumed by correlated evidence: found=%v err=%v trigger=%+v", found, err, trigger)
	}
}

func TestAirWorkerSelfLearningBlindRetryInvalidatesUsageProof(t *testing.T) {
	_, settings, stateDir := selfLearningFixture(t)
	target := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "blind-retry", target)
	in.RunID = "release-run-retry"
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	unknown := in
	unknown.HookEventName = "PostToolUse"
	unknown.ToolName = "mcp__AIR_Commander_Test__start_process"
	unknown.ToolInput = json.RawMessage(`{"node":"SRVLM01","command":"same mutation","shell":"powershell"}`)
	unknown.ToolResponse = json.RawMessage(`{"error":"EXECUTION_UNKNOWN","pid":4242}`)
	if _, err := handlePostToolUseSelfLearning(unknown); err != nil {
		t.Fatal(err)
	}

	retry := unknown
	retry.ToolResponse = json.RawMessage(`{"status":"completed","exit_code":0,"pid":4242}`)
	if _, err := handlePostToolUseSelfLearning(retry); err != nil {
		t.Fatal(err)
	}
	trigger, _, found, err := readSelfLearningTriggerReceipt(in)
	if err != nil || !found || trigger.InvalidatedAt == "" || trigger.InvalidationReason == "" {
		t.Fatalf("blind retry did not invalidate usage proof: found=%v err=%v trigger=%+v", found, err, trigger)
	}

	diagnostic := in
	diagnostic.HookEventName = "PostToolUse"
	diagnostic.ToolName = "mcp__AIR_Commander_Test__read_process_output"
	diagnostic.ToolInput = json.RawMessage(`{"node":"SRVLM01","pid":4242}`)
	diagnostic.ToolResponse = json.RawMessage(`{"status":"completed","exit_code":0,"pid":4242}`)
	if res, err := handlePostToolUseSelfLearning(diagnostic); err != nil || strings.TrimSpace(res.Context) != "" {
		t.Fatalf("invalidated trigger produced use: res=%+v err=%v", res, err)
	}
	if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("blind retry followed by diagnostic created %d used events", got)
	}
}

func TestAirWorkerSelfLearningContextReceiptUsesCurrentDeliverySnapshot(t *testing.T) {
	selfProduct, settings, stateDir := selfLearningFixture(t)
	targetProduct := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(targetProduct, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "snapshot-refresh", targetProduct)
	in.RunID = "snapshot-run"
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	before, _, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || len(before.Loaded) != 1 {
		t.Fatalf("initial snapshot missing: found=%v err=%v rec=%+v", found, err, before)
	}
	old := before.Loaded[0]
	updated := `# Updated execution reconciliation

## When to apply
When execution state is ambiguous.

## Procedure
1. Inspect the exact machine evidence for the current operation.

## Pitfalls
Do not reuse stale procedure identities.
`
	res, err := executeSharedLearning(selfProduct, settings, "propose", map[string]string{
		"proposal_id": "LP-snapshot-refresh",
		"kind":        "procedure",
		"target":      old.Target,
		"pre_sha256":  old.SHA256,
		"content":     updated,
	})
	if err != nil || res.Status != "applied" {
		t.Fatalf("procedure update failed: %+v %v", res, err)
	}
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	after, _, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found || len(after.Loaded) != 1 {
		t.Fatalf("refreshed snapshot missing: found=%v err=%v rec=%+v", found, err, after)
	}
	if strings.EqualFold(after.Loaded[0].SHA256, old.SHA256) {
		t.Fatalf("stale SHA survived current delivery snapshot: old=%s after=%s", old.SHA256, after.Loaded[0].SHA256)
	}
	if after.UsedAt != "" || after.UsedRuleID != "" || after.Outcome != "" {
		t.Fatalf("context refresh carried historical use into new delivery: %+v", after)
	}
}

func TestAirWorkerSelfLearningObservableToolOutcome(t *testing.T) {
	if !observableToolSuccess(json.RawMessage(`{"status":"online"}`)) {
		t.Fatal("structured success was not observable")
	}
	for name, raw := range map[string]json.RawMessage{
		"unknown":        json.RawMessage(`{"error":"EXECUTION_UNKNOWN"}`),
		"nonzero":        json.RawMessage(`{"exit_code":1}`),
		"camel-error":    json.RawMessage(`{"isError":true,"content":[{"type":"text","text":"Access denied"}]}`),
		"snake-error":    json.RawMessage(`{"is_error":true,"content":[{"type":"text","text":"failed"}]}`),
		"array-no-proof": json.RawMessage(`{"content":[{"type":"text","text":"some output"}]}`),
	} {
		t.Run(name, func(t *testing.T) {
			if observableToolSuccess(raw) {
				t.Fatalf("%s was treated as observable success: %s", name, raw)
			}
		})
	}
	if !observableToolSuccess(json.RawMessage(`{"exit_code":0}`)) {
		t.Fatal("zero exit was not treated as success")
	}
	if !observableToolSuccess(json.RawMessage(`{"isError":false,"content":[{"type":"text","text":"process completed with exit code 0"}]}`)) {
		t.Fatal("successful MCP envelope was not observable")
	}
}
