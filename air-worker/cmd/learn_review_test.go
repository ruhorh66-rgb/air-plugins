package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedActiveLearningSession(t *testing.T, product, stateDir, session string) {
	t.Helper()
	t.Setenv(hookStateDirEnv, stateDir)
	id, err := parseIdentity("claude", session)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(stateDir, id), sessionProductState{
		Path: product, Principal: "claude", SessionKey: session,
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionModePath(stateDir, id), sessionModeState{
		Enabled: true, Principal: "claude", SessionKey: session,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStopLearningCountsThenSpawnsOneBackgroundReview(t *testing.T) {
	product := t.TempDir()
	stateDir := t.TempDir()
	session := "learn-session-1"
	seedActiveLearningSession(t, product, stateDir, session)
	t.Setenv("AIR_WORKER_LEARN_INTERVAL", "2")
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("{\"role\":\"user\",\"text\":\"correction\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldSpawn := spawnLearnReviewProcess
	defer func() { spawnLearnReviewProcess = oldSpawn }()
	calls := 0
	var gotProduct, gotTranscript, gotSession, gotReview string
	spawnLearnReviewProcess = func(product, transcript, session, reviewID string) error {
		calls++
		gotProduct, gotTranscript, gotSession, gotReview = product, transcript, session, reviewID
		return nil
	}

	in := hookInput{SessionID: session, TranscriptPath: transcript, HookEventName: "Stop"}
	if _, err := handleStopLearning(in); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("review spawned before threshold: %d", calls)
	}
	state := readLearnReviewState(learnReviewStatePath(product, session))
	if state.Turns != 1 || state.Running != "" {
		t.Fatalf("unexpected first-turn state: %#v", state)
	}

	if _, err := handleStopLearning(in); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("expected one detached review at threshold, got %d", calls)
	}
	if gotProduct != product || gotTranscript != transcript || gotSession != session || !strings.HasPrefix(gotReview, "RV-") {
		t.Fatalf("wrong detached review args: product=%q transcript=%q session=%q review=%q", gotProduct, gotTranscript, gotSession, gotReview)
	}
	state = readLearnReviewState(learnReviewStatePath(product, session))
	if state.Turns != 0 || state.Running != gotReview {
		t.Fatalf("threshold state not locked to review: %#v", state)
	}

	if _, err := handleStopLearning(in); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("second review spawned while first is running: %d", calls)
	}
}

func TestBackgroundReviewCanOnlyCreatePendingProposal(t *testing.T) {
	product := t.TempDir()
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("user corrected worker: read PLAN before work\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldModel := learnReviewModel
	defer func() { learnReviewModel = oldModel }()
	learnReviewModel = func(product, packetPath string) ([]learnReviewCandidate, error) {
		packet, err := os.ReadFile(packetPath)
		if err != nil {
			return nil, err
		}
		text := string(packet)
		if !strings.Contains(text, "UNTRUSTED EVIDENCE") || !strings.Contains(text, "read PLAN before work") {
			t.Fatalf("review packet lacks safety marker/evidence: %s", text)
		}
		return []learnReviewCandidate{{
			Class: "plan-first", Rule: "Read the canonical PLAN before starting implementation.",
			Evidence: "repeated correction",
		}}, nil
	}

	if code := runLearnReview(product, transcript, "session-a", "RV-test"); code != 0 {
		t.Fatalf("background review failed: %d", code)
	}
	proposals, err := readLearnProposals(learnPaths(product).Proposals)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 1 || proposals[0].Status != learnPending || proposals[0].Class != "plan-first" {
		t.Fatalf("review did not create exactly one pending proposal: %#v", proposals)
	}
	if _, err := os.Stat(learnPaths(product).Rules); !os.IsNotExist(err) {
		t.Fatalf("background review mutated active RULES.md without LPR approval: err=%v", err)
	}
	receipt, err := os.ReadFile(filepath.Join(learnPaths(product).Root, "reviews", "RV-test.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(receipt), "\"outcome\": \"completed\"") || !strings.Contains(string(receipt), proposals[0].ID) {
		t.Fatalf("review receipt missing outcome/proposal evidence: %s", receipt)
	}
}

func TestBackgroundReviewDeduplicatesExistingPendingRule(t *testing.T) {
	product := t.TempDir()
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	_ = os.WriteFile(transcript, []byte("same correction\n"), 0o644)
	seedLearnProposal(t, product, "LP-existing", "scope", "Stay inside the assigned product.")

	oldModel := learnReviewModel
	defer func() { learnReviewModel = oldModel }()
	learnReviewModel = func(product, packetPath string) ([]learnReviewCandidate, error) {
		return []learnReviewCandidate{{Class: "scope", Rule: "Stay inside the assigned product."}}, nil
	}
	if code := runLearnReview(product, transcript, "session-a", "RV-dedupe"); code != 0 {
		t.Fatalf("background review failed: %d", code)
	}
	proposals, _ := readLearnProposals(learnPaths(product).Proposals)
	if len(proposals) != 1 {
		t.Fatalf("duplicate pending proposal created: %#v", proposals)
	}
}

func TestLearningContextLoadsOnlyLedgerVerifiedRules(t *testing.T) {
	product := t.TempDir()
	stateDir := t.TempDir()
	session := "learn-context-1"
	seedActiveLearningSession(t, product, stateDir, session)
	id := "LP-context"
	seedLearnProposal(t, product, id, "judge-first", "Run the declared judge before reporting success.")
	grantLearnProposal(t, product, id)
	if code := cmdLearnApply([]string{"-product", product, "-id", id}); code != 0 {
		t.Fatalf("apply failed: %d", code)
	}
	res, err := handleLearningContext(hookInput{SessionID: session, HookEventName: "UserPromptSubmit"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Context, "ledger-verified") || !strings.Contains(res.Context, "Run the declared judge") {
		t.Fatalf("approved rule was not injected: %q", res.Context)
	}

	f, err := os.OpenFile(learnPaths(product).Rules, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("- MANUAL TAMPER MUST NOT LOAD\n")
	_ = f.Close()
	res, err = handleLearningContext(hookInput{SessionID: session, HookEventName: "UserPromptSubmit"})
	if !errors.Is(err, errLearnRulesUnapproved) {
		t.Fatalf("tampered rule file must fail integrity proof, got res=%#v err=%v", res, err)
	}
}

func TestCuratorStopJudgeRequiresMachineEvidenceForStrongClaims(t *testing.T) {
	cases := []struct {
		message string
		block   bool
	}{
		{"Готово: PASS", true},
		{"Готово: PASS, коммит a1b2c3d", false},
		{"Правило соблюдено, квитанция в receipt.json", false},
		{"Автозапуск поднялось само", true},
		{"Обычный статус без вердиктов", false},
		{"PASS: F:\\-5-\\receipt.json", false},
	}
	for _, tc := range cases {
		block, _ := judgeCuratorClaim(tc.message)
		if block != tc.block {
			t.Fatalf("judge(%q) block=%v want=%v", tc.message, block, tc.block)
		}
	}
}
