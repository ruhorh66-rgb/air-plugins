package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSemanticVerdictStrict(t *testing.T) {
	good := `{"verdict":"PASS","step_done":"yes","drift":"none","evidence":["tests pass"],"next":"close step"}`
	v, err := parseSemanticVerdict(good)
	if err != nil {
		t.Fatalf("valid verdict rejected: %v", err)
	}
	if v.Verdict != "PASS" || v.StepDone != "yes" || v.Drift != "none" {
		t.Fatalf("unexpected verdict: %+v", v)
	}

	bad := []string{
		"```json\n" + good + "\n```",
		good + " trailing",
		`{"verdict":"PASS","step_done":"yes","drift":"none","evidence":[],"next":"x","extra":1}`,
		`{"verdict":"PASS","step_done":"partial","drift":"none","evidence":[],"next":"x"}`,
		`{"verdict":"DRIFT","step_done":"no","drift":"none","evidence":[],"next":"x"}`,
	}
	for _, raw := range bad {
		if _, err := parseSemanticVerdict(raw); err == nil {
			t.Fatalf("invalid contract accepted: %q", raw)
		}
	}
}
func TestSemanticReviewerOppositeVendor(t *testing.T) {
	r, err := oppositeSemanticReviewer(runnerSpec{Kind: "claude"})
	if err != nil || r.Kind != "codex" {
		t.Fatalf("claude reviewer = %+v, %v", r, err)
	}
	r, err = oppositeSemanticReviewer(runnerSpec{Kind: "codex", Model: "gpt-6-sol"})
	if err != nil || r.Kind != "claude" || r.Model != "claude-sonnet-5-5" || r.Effort != "low" {
		t.Fatalf("codex reviewer = %+v, %v", r, err)
	}
	r, err = oppositeSemanticReviewer(runnerSpec{Kind: "codex", Model: "gpt-6-astra"})
	if err != nil || r.Model != "claude-opus-5-5" || r.Effort != "low" {
		t.Fatalf("astra reviewer = %+v, %v", r, err)
	}
	if _, err := oppositeSemanticReviewer(runnerSpec{Kind: "script"}); err == nil {
		t.Fatal("script must not get semantic model reviewer")
	}
}

func TestSemanticCodexIsReadOnly(t *testing.T) {
	args := semanticCodexArgs(`C:\product`, runnerSpec{Kind: "codex", Effort: "high"}, `C:\tmp\schema.json`)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-s read-only") {
		t.Fatalf("codex semantic sandbox is not read-only: %v", args)
	}
	if strings.Contains(joined, "workspace-write") {
		t.Fatalf("semantic reviewer received write sandbox: %v", args)
	}
	if !strings.Contains(joined, `--output-schema C:\tmp\schema.json`) {
		t.Fatalf("codex semantic output schema missing: %v", args)
	}
}

func TestCodexReviewerStreamRejectsNonzeroExitAfterCompletion(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"{ }\"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, errors.New("exit status 9"), "semantic review")
	if err == nil || !strings.Contains(err.Error(), "exit status 9") {
		t.Fatalf("nonzero process exit was accepted after turn.completed: %v", err)
	}
}

func TestCodexReviewerStreamAcceptsCleanCompletion(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.started\",\"item\":{\"type\":\"agent_message\"}}\n" +
		"{\"type\":\"item.updated\",\"item\":{\"type\":\"agent_message\",\"text\":\"PARTIAL\"}}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"EARLIER VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	text, session, _, turns, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err != nil || text != "VERDICT" || session != "review-1" || turns == nil || *turns != 2 {
		t.Fatalf("clean completion parse: text=%q session=%q turns=%v err=%v", text, session, turns, err)
	}
}

func TestCodexReviewerStreamRejectsIncompleteLatestTurn(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"EARLIER VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"INCOMPLETE FINAL RESPONSE\"}}\n")
	_, _, _, turns, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "without successful terminal event") || turns == nil || *turns != 2 {
		t.Fatalf("incomplete latest turn was accepted: turns=%v err=%v", turns, err)
	}
}

func TestCodexReviewerStreamRejectsPreviousMessageWhenFinalTurnHasNone(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"EARLIER VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"turn.completed\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "no agent_message") {
		t.Fatalf("previous turn's message was accepted for the empty final turn: %v", err)
	}
}

func TestCodexReviewerStreamRejectsMalformedTrailingEvent(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"{\"type\":\"turn.completed\" broken}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("malformed trailing event was ignored: %v", err)
	}
}

func TestCodexReviewerStreamRejectsPlainTextTrailingLine(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"unexpected trailing output\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "invalid JSON") {
		t.Fatalf("plain-text trailing output was ignored: %v", err)
	}
}

func TestCodexReviewerStreamRejectsErrorItem(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"error\",\"message\":\"review transport failed\"}}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "review transport failed") {
		t.Fatalf("reviewer error item was accepted: %v", err)
	}
}

func TestCodexReviewerStreamRejectsMessageAfterTurnCompletion(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"LATE MESSAGE\"}}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "after terminal event") {
		t.Fatalf("message after turn completion was accepted: %v", err)
	}
}

func TestCodexReviewerStreamRejectsCompletionWithoutStartedTurn(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "without an active turn") {
		t.Fatalf("turn without a start event was accepted: %v", err)
	}
}

func TestCodexReviewerStreamRejectsUnknownEventType(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"{\"type\":\"future.event\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "unsupported event type") {
		t.Fatalf("unsupported event type was ignored: %v", err)
	}
}

func TestCodexReviewerStreamRejectsTurnWithoutThreadStart(t *testing.T) {
	stream := []byte("{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "without thread.started") {
		t.Fatalf("turn without thread.started was accepted: %v", err)
	}
}

func TestCodexReviewerStreamRejectsLateThreadStart(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n" +
		"{\"type\":\"thread.started\",\"thread_id\":\"review-2\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "thread.started out of order") {
		t.Fatalf("late thread.started was accepted: %v", err)
	}
}

func TestCodexReviewerStreamRejectsMissingThreadID(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "missing thread_id") {
		t.Fatalf("review without a thread id was accepted: %v", err)
	}
}

func TestCodexReviewerStreamRejectsMalformedItemEvents(t *testing.T) {
	for _, eventType := range []string{"item.started", "item.updated", "item.completed"} {
		t.Run(eventType, func(t *testing.T) {
			stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
				"{\"type\":\"turn.started\"}\n" +
				"{\"type\":\"" + eventType + "\",\"item\":null}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
				"{\"type\":\"turn.completed\"}\n")
			_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
			if err == nil || !strings.Contains(err.Error(), "without a valid item") {
				t.Fatalf("malformed %s was accepted: %v", eventType, err)
			}
		})
	}
}

func TestCodexReviewerStreamRejectsEmptyAgentMessage(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"  \"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err == nil || !strings.Contains(err.Error(), "empty agent_message") {
		t.Fatalf("empty agent_message after a verdict was accepted: %v", err)
	}
}

func TestCodexReviewerStreamRejectsDuplicateJSONKeys(t *testing.T) {
	cases := []struct {
		name  string
		event string
	}{
		{"type", `{"type":"future.event","type":"item.completed","item":{"type":"agent_message","text":"VERDICT"}}`},
		{"item", `{"type":"item.completed","item":{"type":"error","message":"hidden error"},"item":{"type":"agent_message","text":"VERDICT"}}`},
		{"type casing", `{"type":"future.event","Type":"item.completed","item":{"type":"agent_message","text":"VERDICT"}}`},
		{"nested item type casing", `{"type":"item.completed","item":{"type":"error","Type":"agent_message","message":"hidden error","text":"VERDICT"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
				"{\"type\":\"turn.started\"}\n" + tc.event + "\n" +
				"{\"type\":\"turn.completed\"}\n")
			_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
			if err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
				t.Fatalf("duplicate JSON key was accepted: %v", err)
			}
		})
	}
}

func TestCodexReviewerStreamRejectsUnsupportedItemType(t *testing.T) {
	for _, eventType := range []string{"item.started", "item.updated", "item.completed"} {
		t.Run(eventType, func(t *testing.T) {
			stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
				"{\"type\":\"turn.started\"}\n" +
				"{\"type\":\"" + eventType + "\",\"item\":{\"type\":\"future_error\"}}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
				"{\"type\":\"turn.completed\"}\n")
			_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
			if err == nil || !strings.Contains(err.Error(), "unsupported item type") {
				t.Fatalf("unsupported %s item was accepted: %v", eventType, err)
			}
		})
	}
}

func TestCodexReviewerStreamRejectsErrorItemsBeforeCompletion(t *testing.T) {
	for _, eventType := range []string{"item.started", "item.updated"} {
		t.Run(eventType, func(t *testing.T) {
			stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
				"{\"type\":\"turn.started\"}\n" +
				"{\"type\":\"" + eventType + "\",\"item\":{\"type\":\"error\",\"message\":\"incomplete error item\"}}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
				"{\"type\":\"turn.completed\"}\n")
			_, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
			if err == nil || !strings.Contains(err.Error(), "incomplete error item") {
				t.Fatalf("error item in %s was ignored: %v", eventType, err)
			}
		})
	}
}

func TestCodexReviewerStreamAcceptsKnownNonVerdictItems(t *testing.T) {
	itemTypes := []string{"reasoning", "command_execution", "file_change", "mcp_tool_call", "collab_tool_call", "web_search", "todo_list"}
	for _, itemType := range itemTypes {
		t.Run(itemType, func(t *testing.T) {
			stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
				"{\"type\":\"turn.started\"}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"" + itemType + "\"}}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"VERDICT\"}}\n" +
				"{\"type\":\"turn.completed\"}\n")
			message, _, _, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
			if err != nil {
				t.Fatalf("known %s item was rejected: %v", itemType, err)
			}
			if message != "VERDICT" {
				t.Fatalf("message = %q, want VERDICT", message)
			}
		})
	}
}

func TestCodexReviewerStreamResetsCostForFinalTurn(t *testing.T) {
	stream := []byte("{\"type\":\"thread.started\",\"thread_id\":\"review-1\"}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"EARLIER\"}}\n" +
		"{\"type\":\"turn.completed\",\"usage\":{\"cost_usd\":0.42}}\n" +
		"{\"type\":\"turn.started\"}\n" +
		"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"FINAL\"}}\n" +
		"{\"type\":\"turn.completed\"}\n")
	message, _, cost, _, err := parseCodexReviewerStream(stream, nil, "semantic review")
	if err != nil {
		t.Fatalf("valid multi-turn stream failed: %v", err)
	}
	if message != "FINAL" {
		t.Fatalf("message = %q, want FINAL", message)
	}
	if cost != nil {
		t.Fatalf("cost = %v, want nil for final turn without usage", *cost)
	}
}

func TestCriterion56SemanticCodexUsesStdin(t *testing.T) {
	withPonytailFixture(t, "VENDOR_CONTEXT")
	prompt := strings.Repeat("semantic-packet-", 4096)
	cmd := semanticCodexCommand("codex.cmd", `C:\product`, prompt, runnerSpec{Kind: "codex", Effort: "high"}, `C:\tmp\schema.json`)
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "semantic-packet-") {
		t.Fatalf("semantic packet leaked into argv: %d chars", len(joined))
	}
	if !strings.HasSuffix(joined, " -") {
		t.Fatalf("codex stdin sentinel missing: %v", cmd.Args)
	}
	raw, err := io.ReadAll(cmd.Stdin)
	body := string(raw)
	if err != nil || !strings.Contains(body, "VENDOR_CONTEXT") || !strings.Contains(body, "AIRWORKER TASK:\n"+prompt) {
		t.Fatalf("semantic stdin lost Ponytail or task: len=%d err=%v", len(raw), err)
	}
}

func TestSemanticClaudeUsesStdin(t *testing.T) {
	prompt := strings.Repeat("semantic-packet-", 4096)
	cmd := semanticClaudeCommand("claude.cmd", `C:\product`, prompt, runnerSpec{Kind: "claude", Model: "sonnet"})
	joined := strings.Join(cmd.Args, " ")
	if strings.Contains(joined, "semantic-packet-") {
		t.Fatalf("semantic packet leaked into argv: %d chars", len(joined))
	}
	raw, err := io.ReadAll(cmd.Stdin)
	if err != nil || string(raw) != prompt {
		t.Fatalf("semantic stdin mismatch: len=%d err=%v", len(raw), err)
	}
}

func TestSemanticReviewerRegistersReceipt(t *testing.T) {
	oldCommand := semanticCommand
	t.Cleanup(func() { semanticCommand = oldCommand })
	t.Setenv("AW_SEMANTIC_HELPER", "1")
	semanticCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=TestSemanticReviewerHelperProcess", "--")
	}

	root := t.TempDir()
	scope := legacyScope(root)
	reviewer := runnerSpec{Kind: "codex", Model: "test", Effort: "low"}
	resultCh := make(chan error, 1)
	go func() {
		_, _, _, _, err := invokeSemanticCodex(scope, os.Args[0], "test prompt", reviewer, "5")
		resultCh <- err
	}()

	receiptPath, _ := jobReceiptPaths(scope, "5", "semantic-reviewer")
	deadline := time.Now().Add(2 * time.Second)
	var receipt *jobReceipt
	for time.Now().Before(deadline) {
		if candidate, err := readJobReceipt(receiptPath); err == nil && candidate.Status == jobStatusRunning && candidate.ProcessStarted && candidate.PID > 0 {
			receipt = candidate
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if receipt == nil {
		t.Fatalf("semantic receipt did not expose a running PID: %s", receiptPath)
	}
	if receipt.Operation != "semantic-reviewer" || receipt.Role != "semantic-reviewer" {
		t.Fatalf("semantic receipt metadata = %+v", receipt)
	}
	if !receipt.ProcessStarted || receipt.PID <= 0 {
		t.Fatalf("semantic receipt does not identify a running process: %+v", receipt)
	}

	if err := <-resultCh; err != nil {
		t.Fatalf("invokeSemanticCodex: %v", err)
	}
	finished := pollReceiptStatus(t, receiptPath, jobStatusDone, 2*time.Second)
	if finished.PID != receipt.PID || !finished.ProcessStarted {
		t.Fatalf("semantic receipt lost process identity: running=%+v done=%+v", receipt, finished)
	}
}

func TestSemanticReviewerHelperProcess(t *testing.T) {
	if os.Getenv("AW_SEMANTIC_HELPER") != "1" {
		return
	}
	time.Sleep(500 * time.Millisecond)
	println(`{"type":"thread.started","thread_id":"semantic-helper"}`)
	println(`{"type":"turn.started"}`)
	println(`{"type":"item.completed","item":{"type":"agent_message","text":"{\"verdict\":\"PASS\",\"step_done\":\"yes\",\"drift\":\"none\",\"evidence\":[],\"next\":\"done\"}"}}`)
	println(`{"type":"turn.completed"}`)
	os.Exit(0)
}

func TestCombinedAcceptanceMatrix(t *testing.T) {
	pass := semanticVerdict{Verdict: "PASS", StepDone: "yes", Drift: "none"}
	drift := semanticVerdict{Verdict: "DRIFT", StepDone: "partial", Drift: "major"}
	np := semanticVerdict{Verdict: "NOT_PROVEN", StepDone: "partial", Drift: "none"}
	if got := combinedAcceptance(1, pass); got != "FAIL" {
		t.Fatalf("factual fail overridden: %s", got)
	}
	if got := combinedAcceptance(2, pass); got != "NOT_PROVEN" {
		t.Fatalf("factual unknown became failure/pass: %s", got)
	}
	if got := combinedAcceptance(0, pass); got != "PASS" {
		t.Fatalf("pass matrix: %s", got)
	}
	if got := combinedAcceptance(0, drift); got != "DRIFT" {
		t.Fatalf("drift matrix: %s", got)
	}
	if got := combinedAcceptance(0, np); got != "NOT_PROVEN" {
		t.Fatalf("not-proven matrix: %s", got)
	}
}
func TestPublishSemanticWritesLatestAndHistory(t *testing.T) {
	root := t.TempDir()
	v := semanticVerdict{Verdict: "PASS", StepDone: "yes", Drift: "none", Next: "done"}
	rec := semanticRecord{At: "now", Subject: "step", Step: "65", ReviewerVendor: "codex", Verdict: &v}
	publishSemantic(root, rec)
	publishSemantic(root, rec)

	latest := filepath.Join(root, ".woody", "semantic-verdict.json")
	if _, err := os.Stat(latest); err != nil {
		t.Fatalf("latest verdict missing: %v", err)
	}
	history := filepath.Join(root, ".woody", "semantic-history.jsonl")
	raw, err := os.ReadFile(history)
	if err != nil {
		t.Fatalf("history missing: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("history is not append-only: %d lines", len(lines))
	}
}

func TestReleaseProfileUsesOpenAIExecutorsAndAnthropicJudges(t *testing.T) {
	var cfg runConfig
	if err := readJSON(filepath.Join("..", "run-config.json"), &cfg); err != nil {
		t.Fatalf("read release run-config: %v", err)
	}
	if err := validateModelPolicy(cfg); err != nil {
		t.Fatalf("release model policy: %v", err)
	}
	for _, tier := range cfg.Ladder {
		r := resolveRunner(cfg, tier)
		if r.Kind == "script" {
			continue
		}
		if r.Kind != "codex" {
			t.Fatalf("release executor at %s must be OpenAI/Codex: %+v", tier, r)
		}
		reviewer, err := oppositeSemanticReviewer(r)
		if err != nil || reviewer.Kind != "claude" || reviewer.Effort != "low" {
			t.Fatalf("executor at %s must start with Anthropic low-effort judge: %+v, %v", tier, reviewer, err)
		}
	}
}

func TestExplicitSemanticJudgePolicySelectsCodexSolMedium(t *testing.T) {
	var cfg runConfig
	raw, err := os.ReadFile(filepath.Join("..", "run-config.json"))
	if err != nil {
		t.Fatalf("read active run-config.json: %v", err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal active run-config.json: %v", err)
	}
	got, explicit, err := cfg.SemanticJudge.Resolve(runnerSpec{Kind: "codex", Model: "gpt-6-luna"}, "0.11.9")
	if err != nil {
		t.Fatalf("resolve semantic reviewer: %v", err)
	}
	if got.Kind != "codex" || got.Model != "gpt-6-sol" || got.Effort != "medium" || !explicit {
		t.Fatalf("explicit semantic reviewer = %+v (explicit=%v), want Codex gpt-6-sol medium", got, explicit)
	}
	if err := (semanticJudgePolicy{}).Validate(); err != nil {
		t.Fatalf("empty semantic_judge should preserve the default opposite-vendor policy: %v", err)
	}
	defaultReviewer, defaultOverride, err := (semanticJudgePolicy{}).Resolve(runnerSpec{Kind: "codex", Model: "gpt-6-luna"}, "0.11.9")
	if err != nil || defaultReviewer.Kind != "claude" || defaultOverride {
		t.Fatalf("default semantic reviewer = %+v (override=%v), %v", defaultReviewer, defaultOverride, err)
	}
	futureReviewer, futureOverride, err := cfg.SemanticJudge.Resolve(runnerSpec{Kind: "codex", Model: "gpt-6-luna"}, "0.11.10")
	if err != nil || futureReviewer.Kind != "claude" || futureOverride {
		t.Fatalf("release-specific reviewer leaked to later release: %+v (override=%v), %v", futureReviewer, futureOverride, err)
	}
	if err := (semanticJudgePolicy{Kind: "codex", Model: "gpt-6-sol", Effort: "medium"}).Validate(); err == nil {
		t.Fatal("unscoped explicit semantic override must be rejected")
	}
	if err := (semanticJudgePolicy{ReleaseVersion: "0.11.9", Kind: "codex", Model: "gpt-6-sol", Effort: "xhigh"}).Validate(); err == nil {
		t.Fatal("unsupported semantic judge effort must be rejected")
	}
}
