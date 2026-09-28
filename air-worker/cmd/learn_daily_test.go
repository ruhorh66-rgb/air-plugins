package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDailyPartnerRunsImmediatelyThenWaitsTwentyFourHours(t *testing.T) {
	product := t.TempDir()
	stateDir := t.TempDir()
	session := "daily-partner"
	seedActiveLearningSession(t, product, stateDir, session)
	t.Setenv("AIR_WORKER_LEARN_INTERVAL", "")
	transcript := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(transcript, []byte("daily evidence\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	oldSpawn := spawnLearnReviewProcess
	defer func() { spawnLearnReviewProcess = oldSpawn }()
	calls := 0
	spawnLearnReviewProcess = func(product, transcript, session, reviewID string) error {
		calls++
		return nil
	}

	in := hookInput{SessionID: session, TranscriptPath: transcript, HookEventName: "Stop"}
	if _, err := handleStopLearning(in); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("first daily partner review must start immediately, calls=%d", calls)
	}

	statePath := learnReviewStatePath(product, session)
	state := readLearnReviewState(statePath)
	state.Running = ""
	state.LastReviewAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeLearnReviewState(statePath, state); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStopLearning(in); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("partner reran before 24h, calls=%d", calls)
	}

	state = readLearnReviewState(statePath)
	state.LastReviewAt = time.Now().UTC().Add(-25 * time.Hour).Format(time.RFC3339Nano)
	if err := writeLearnReviewState(statePath, state); err != nil {
		t.Fatal(err)
	}
	if _, err := handleStopLearning(in); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("partner did not rerun after 24h, calls=%d", calls)
	}
}

func TestReviewPacketContainsOnlyRecentCanonicalEventsAndClasses(t *testing.T) {
	product := t.TempDir()
	paths := learnPaths(product)
	now := time.Now().UTC()
	recent := learnJournalRecord{
		Schema: learnSchemaVersion, ID: "LR-recent", CreatedAt: now.Add(-time.Hour).Format(time.RFC3339Nano),
		Class: "recent-class", Observed: "recent observation", Kind: "lesson", Source: "test",
	}
	old := learnJournalRecord{
		Schema: learnSchemaVersion, ID: "LR-old", CreatedAt: now.Add(-25 * time.Hour).Format(time.RFC3339Nano),
		Class: "old-class", Observed: "old observation", Kind: "lesson", Source: "test",
	}
	if err := appendLearnJSON(paths.Journal, old); err != nil {
		t.Fatal(err)
	}
	if err := appendLearnJSON(paths.Journal, recent); err != nil {
		t.Fatal(err)
	}

	packet, err := buildLearnReviewPacket(product, []byte("transcript fragment"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(packet)
	for _, want := range []string{"## EVENTS LAST 24H", "recent-class", "recent observation", "## EVENT CLASSES", "transcript fragment"} {
		if !strings.Contains(text, want) {
			t.Fatalf("review packet missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "old-class") || strings.Contains(text, "old observation") {
		t.Fatalf("review packet leaked event older than 24h:\n%s", text)
	}
}

func TestGPTStopRunsDailyReviewWithoutTranscript(t *testing.T) {
	product := t.TempDir()
	session := "gpt-daily-no-transcript"
	hookTestStateForPrincipal(t, "chatgpt", session, product)

	oldSpawn := spawnLearnReviewProcess
	defer func() { spawnLearnReviewProcess = oldSpawn }()
	calls := 0
	gotTranscript := "not-called"
	spawnLearnReviewProcess = func(gotProduct, transcript, gotSession, reviewID string) error {
		calls++
		gotTranscript = transcript
		if gotProduct != product || gotSession != session {
			t.Fatalf("wrong GPT review identity: product=%q session=%q", gotProduct, gotSession)
		}
		return nil
	}

	in := hookInput{Principal: "chatgpt", SessionID: session, HookEventName: "Stop"}
	if _, err := handleStopLearning(in); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || gotTranscript != "" {
		t.Fatalf("GPT daily review should start from canonical events without transcript: calls=%d transcript=%q", calls, gotTranscript)
	}
}
