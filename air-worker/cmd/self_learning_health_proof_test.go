package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const n100AutoHealthSkill = "# Handle Native Health-Probe Timeouts in Release Runs\n\n" +
	"## When to apply\n\nWhen a release validation command exits nonzero because its native health check times out or cannot receive response headers.\n\n" +
	"## Procedure\n\n1. Preserve the exact command, exit code, and native output.\n" +
	"2. Record the machine receipt or other run evidence reference.\n" +
	"3. Mark the release-ladder criterion as unmet; do not treat an unjudged run as successful.\n" +
	"4. Record whether any service or application was changed.\n\n" +
	"## Pitfalls\n\n- Do not convert a health-check timeout into a passing result.\n"

func n100Fixture(t *testing.T) (string, sharedLearningSettings, hookInput, selfLearningLoadedRule) {
	t.Helper()
	root, settings, state := selfLearningFixture(t)
	// Same shared module path as a production reviewer, but no LLM or LEARN
	// injection in the test; the result is written by native propose/apply.
	target := "skills/learned/a-fresh-procedural-lesson-20261009.md"
	res, err := executeSharedLearning(root, settings, "propose", map[string]string{
		"proposal_id": "LP-N100-fresh-" + strings.ReplaceAll(t.Name(), "/", "-"),
		"kind":        "procedure", "target": target, "pre_sha256": "",
		"content": n100AutoHealthSkill,
	})
	if err != nil || res.Status != "applied" {
		t.Fatalf("native safe procedure apply failed: %+v %v", res, err)
	}
	other := filepath.Join(t.TempDir(), "unrelated-product")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, state, "codex", "n100-scenario", other)
	in.RunID = "real-machine-cycle"
	if _, err := handleLearningContext(in); err != nil {
		t.Fatal(err)
	}
	rec, _, found, err := readSelfLearningContextReceipt(in)
	if err != nil || !found {
		t.Fatalf("loaded receipt missing: %v found=%v", err, found)
	}
	owner, active, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !active {
		t.Fatalf("self owner inactive: %v", err)
	}
	rule, supported := healthPreservationRule(owner, rec)
	if !supported || rule.Target != target {
		t.Fatalf("newly generated safe procedure not selected: %+v %+v", rule, rec.Loaded)
	}
	return root, settings, in, rule
}

func n100Post(in hookInput, tool string, args any, response any) hookInput {
	b, _ := json.Marshal(args)
	r, _ := json.Marshal(response)
	in.HookEventName, in.ToolName, in.ToolInput, in.ToolResponse = "PostToolUse", tool, b, r
	return in
}

func n100Probe(in hookInput) hookInput {
	return n100Post(in, "mcp__AIR_Commander_Test__start_process",
		map[string]any{"node": "SRVLM01", "command": "air-worker tool -which headroom"},
		map[string]any{"status": "started", "pid": 4242, "request_id": "acmd-n100"})
}

func n100Diagnostic(in hookInput, node string, pid int, text string) hookInput {
	return n100Post(in, "mcp__AIR_Commander_Test__read_process_output",
		map[string]any{"node": node, "pid": pid},
		map[string]any{"isError": false, "content": []map[string]string{{"type": "text", "text": text}}})
}

const n100Failure = "headroom health: Get http://localhost:8787/health: context deadline exceeded\nProcess completed with exit code 2"

func n100ReceiptRead(t *testing.T, in hookInput, alter func(string) string) hookInput {
	t.Helper()
	path, err := healthProofPath(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data := string(b)
	if alter != nil {
		data = alter(data)
	}
	return n100Post(in, "mcp__AIR_Commander_Test__read_file",
		map[string]any{"node": "SRVLM01", "path": path},
		map[string]any{"isError": false, "content": []map[string]string{{"type": "text", "text": "[SRVLM01] [Reading] " + data}}})
}

func TestN100NewSafeProcedureActualFailureReadbackUse(t *testing.T) {
	_, settings, in, rule := n100Fixture(t)
	if r, err := handlePostToolUseSelfLearning(n100Probe(in)); err != nil || r.Context != "" {
		t.Fatalf("probe start claimed use: %+v %v", r, err)
	}
	started, _, startedFound, startedErr := readHealthProof(in)
	if startedErr != nil || !startedFound || started.Stage != "probe_started" {
		t.Fatalf("native trigger absent: found=%v proof=%+v err=%v", startedFound, started, startedErr)
	}
	bad := n100Diagnostic(in, "AIR-ENV-002", 4242, n100Failure)
	if r, err := handlePostToolUseSelfLearning(bad); err != nil || r.Context != "" {
		t.Fatalf("wrong node claimed use: %+v %v", r, err)
	}
	if n := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); n != 0 {
		t.Fatalf("premature used=%d", n)
	}
	verified := n100Diagnostic(in, "SRVLM01", 4242, n100Failure)
	code, measured := verifiedHeadroomHealthFailure(verified.ToolResponse)
	if !measured || code != 2 {
		t.Fatalf("fixture failure not recognized: code=%d measured=%v raw=%s", code, measured, string(verified.ToolResponse))
	}
	res, err := handlePostToolUseSelfLearning(verified)
	if err != nil || !strings.Contains(res.Context, "FAILED (not recovered)") {
		t.Fatalf("fail-preservation instruction missing: %+v %v", res, err)
	}
	proof, _, found, err := readHealthProof(in)
	if err != nil || !found || proof.Stage != "verified_failure" || proof.ExitCode != 2 {
		t.Fatalf("failure proof not persisted: %+v %v", proof, err)
	}
	if n := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); n != 0 {
		t.Fatalf("failure alone claimed procedure use: %d", n)
	}
	consulted := n100ReceiptRead(t, in, nil)
	res, err = handlePostToolUseSelfLearning(consulted)
	ruleID := rule.Target + "@" + rule.SHA256
	if err != nil || !strings.Contains(res.Context, "procedure_used "+ruleID) {
		t.Fatalf("validated machine read did not produce use: %+v %v", res, err)
	}
	if n := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); n != 1 {
		t.Fatalf("positive machine effect missing: %d", n)
	}
	context, _, _, err := readSelfLearningContextReceipt(in)
	if err != nil || context.UsedRuleID != ruleID || context.Outcome != "pass" ||
		context.OutcomeRef == "" || context.ObservedTool != consulted.ToolName {
		t.Fatalf("exact use/outcome missing: %+v %v", context, err)
	}
	proof, _, _, err = readHealthProof(in)
	if err != nil || proof.Stage != "consulted" || proof.ConsultedAt == "" {
		t.Fatalf("consulted proof did not persist: %+v %v", proof, err)
	}
	if _, err := handlePostToolUseSelfLearning(consulted); err != nil {
		t.Fatal(err)
	}
	if n := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); n != 1 {
		t.Fatalf("duplicate use after repeated host callback: %d", n)
	}
}

func TestN100NeverUsesUnrelatedOrSpoofedEvidence(t *testing.T) {
	cases := []struct {
		name           string
		stopBeforeRead bool
		diagnosticNode string
		diagnosticPID  int
		diagnosticText string
		modifyRule     bool
		spoofRead      bool
		blindRetry     bool
	}{
		{name: "wrong-pid", stopBeforeRead: true, diagnosticNode: "SRVLM01", diagnosticPID: 9999, diagnosticText: n100Failure},
		{name: "success-is-not-failure", stopBeforeRead: true, diagnosticNode: "SRVLM01", diagnosticPID: 4242, diagnosticText: "headroom health: OK\nProcess completed with exit code 0"},
		{name: "generic-positive-text", stopBeforeRead: true, diagnosticNode: "SRVLM01", diagnosticPID: 4242, diagnosticText: "process completed with exit code 0"},
		{name: "not-health-timeout", stopBeforeRead: true, diagnosticNode: "SRVLM01", diagnosticPID: 4242, diagnosticText: "other tool failed\nProcess completed with exit code 2"},
		{name: "stale-target-sha", diagnosticNode: "SRVLM01", diagnosticPID: 4242, diagnosticText: n100Failure, modifyRule: true},
		{name: "spoofed-read-content", diagnosticNode: "SRVLM01", diagnosticPID: 4242, diagnosticText: n100Failure, spoofRead: true},
		{name: "blind-retry", diagnosticNode: "SRVLM01", diagnosticPID: 4242, diagnosticText: n100Failure, blindRetry: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, settings, in, rule := n100Fixture(t)
			if _, err := handlePostToolUseSelfLearning(n100Probe(in)); err != nil {
				t.Fatal(err)
			}
			if c.blindRetry {
				if _, err := handlePostToolUseSelfLearning(n100Probe(in)); err != nil {
					t.Fatal(err)
				}
			}
			response := n100Diagnostic(in, c.diagnosticNode, c.diagnosticPID, c.diagnosticText)
			if _, err := handlePostToolUseSelfLearning(response); err != nil {
				t.Fatal(err)
			}
			if c.modifyRule {
				path := filepath.Join(root, filepath.FromSlash(rule.Target))
				if err := os.WriteFile(path, []byte(n100AutoHealthSkill+"\nTampered\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if !c.stopBeforeRead {
				read := n100ReceiptRead(t, in, nil)
				if c.spoofRead {
					read = n100ReceiptRead(t, in, func(s string) string { return strings.ReplaceAll(s, "verified_failure", "successful") })
				}
				if _, err := handlePostToolUseSelfLearning(read); err != nil {
					t.Fatal(err)
				}
			}
			if got := countSelfLearningEvent(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
				t.Fatalf("%s incorrectly credited %d uses", c.name, got)
			}
		})
	}
}

func TestN100RuleMatcherDoesNotAcceptLegacyOrUnrelatedSkill(t *testing.T) {
	_, _, in, rule := n100Fixture(t)
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatal(err)
	}
	rec, _, _, err := readSelfLearningContextReceipt(in)
	if err != nil {
		t.Fatal(err)
	}
	filtered := rec
	filtered.Loaded = []selfLearningLoadedRule{{Target: "skills/learned/execution-unknown-no-blind-retry-6b72186f.md", SHA256: rec.Loaded[0].SHA256}}
	if got, ok := healthPreservationRule(owner, filtered); ok {
		t.Fatalf("unrelated legacy rule selected: %+v", got)
	}
	other := in
	other.SessionID = "another-session"
	if _, _, exists, err := readHealthProof(other); err != nil || exists {
		t.Fatalf("cross-session proof found: %v %v", exists, err)
	}
	if rule.Target == "" {
		t.Fatal("missing genuine new profile")
	}
}
