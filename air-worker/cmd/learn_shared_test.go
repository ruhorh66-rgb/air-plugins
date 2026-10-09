package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const sharedFixtureProcedure = "# Verify evidence\n\n## When to apply\nWhen checking runner success.\n\n## Procedure\n1. Inspect the produced artifact.\n2. Compare it with the requested result.\n\n## Pitfalls\nAn exit code alone is not evidence of the requested change.\n"

func learningAdapterFixture(t *testing.T, mode string) *learningProcessAdapter {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	return &learningProcessAdapter{Executable: exe, Args: []string{"-test.run=^TestLearningProcessHelper$", "--", mode}, SHA256: hex.EncodeToString(h.Sum(nil)), TimeoutMS: 5000}
}
func sharedProductFixture(t *testing.T, withAdapters bool) (string, sharedLearningSettings) {
	t.Helper()
	root := t.TempDir()
	product := filepath.Join(root, "product")
	if err := os.MkdirAll(product, 0700); err != nil {
		t.Fatal(err)
	}
	s := sharedLearningSettings{Schema: "air-worker.shared-learning/v1", ProductID: "AirWorker-fixture", RuntimeRoot: filepath.Join(root, "runtime"), ManagedSkillPrefix: "skills/learned", TimeoutMS: 10000}
	if withAdapters {
		t.Setenv("AW_LEARNING_ADAPTER_HELPER", "1")
		s.Judge = learningAdapterFixture(t, "judge")
		s.Reviewer = learningAdapterFixture(t, "review")
		s.DeliverSummary = learningAdapterFixture(t, "delivery")
	}
	b, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(product, sharedLearningConfigFile), b, 0600); err != nil {
		t.Fatal(err)
	}
	return product, s
}
func TestLearningProcessHelper(t *testing.T) {
	if os.Getenv("AW_LEARNING_ADAPTER_HELPER") != "1" {
		t.Skip("isolated child only")
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	mode := os.Args[len(os.Args)-1]
	switch mode {
	case "judge-log-failure":
		// Fault injection is confined to the test executable and t.TempDir.
		root := os.Getenv("AW_LEARNING_TEST_BLOCK_LOG_ROOT")
		if root == "" {
			os.Exit(2)
		}
		logs := filepath.Join(root, "logs")
		saved := filepath.Join(root, "fixture-saved-logs")
		if _, err := os.Stat(saved); os.IsNotExist(err) {
			if err := os.Rename(logs, saved); err != nil {
				os.Exit(2)
			}
			if err := os.WriteFile(logs, []byte("fixture obstruction after event persistence"), 0600); err != nil {
				os.Exit(2)
			}
		}
		os.Stdout.Write([]byte(`{"status":"fail","reason":"fixture found unverified output","check_ref":"fixture-verifier"}`))
	case "judge":
		os.Stdout.Write([]byte(`{"status":"fail","reason":"fixture found unverified output","check_ref":"fixture-verifier"}`))
	case "echo-product":
		json.NewEncoder(os.Stdout).Encode(map[string]string{"product": os.Getenv("AIR_WORKER_LEARNING_PRODUCT")})
	case "review":
		if !strings.Contains(string(b), "artifact") {
			os.Stdout.Write([]byte(`{}`))
			break
		}
		json.NewEncoder(os.Stdout).Encode(map[string]string{"kind": "procedure", "target": "skills/learned/evidence.md", "pre_sha256": "", "content": sharedFixtureProcedure})
	case "review-native-feedback":
		json.NewEncoder(os.Stdout).Encode(map[string]string{
			"kind":       "procedure",
			"target":     "skills/learned/native-feedback-lesson-n104.md",
			"pre_sha256": "",
			"content":    n104SafeFeedbackProcedure,
		})
	case "delivery":
		h := sha256.Sum256(b)
		json.NewEncoder(os.Stdout).Encode(map[string]string{"sha256": hex.EncodeToString(h[:]), "channel_ref": "fixture/loopback", "message_id": "fixture-message"})
	case "launch-marker":
		marker := os.Getenv("AW_LEARNING_START_MARKER")
		if marker == "" || os.WriteFile(marker, []byte("started"), 0600) != nil {
			os.Exit(2)
		}
		h := sha256.Sum256(b)
		json.NewEncoder(os.Stdout).Encode(map[string]string{"sha256": hex.EncodeToString(h[:]), "channel_ref": "fixture/loopback", "message_id": "fixture-marker"})
	case "timeout":
		time.Sleep(10 * time.Second)
	case "oversize":
		os.Stdout.Write([]byte(`{"padding":"` + strings.Repeat("x", sharedLearningMaxBytes) + `"}`))
	default:
		os.Stdout.Write([]byte("not json"))
	}
	os.Exit(0)
}

func TestSharedModuleEventUsesOnlyNewJournal(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	if err := os.MkdirAll(filepath.Join(product, "learn"), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(product, "learn/events.jsonl")
	before := []byte("historical journal must remain byte-identical\n")
	os.WriteFile(legacy, before, 0600)
	res, err := sharedLearningEvent(product, s, "r-event", "run_completed", "inspect artifact", "evidence", "feedback", "gpt", "s", "", "")
	if err != nil || res.Status != "recorded" {
		t.Fatalf("%+v %v", res, err)
	}
	after, _ := os.ReadFile(legacy)
	if string(after) != string(before) {
		t.Fatal("legacy journal changed")
	}
	b, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if err != nil || !strings.Contains(string(b), "inspect artifact") {
		t.Fatalf("canonical event missing: %v", err)
	}
	reviews, _ := filepath.Glob(filepath.Join(s.RuntimeRoot, "reviews/RV-*.json"))
	if len(reviews) != 1 {
		t.Fatal(reviews)
	}
	rv, _ := os.ReadFile(reviews[0])
	if !strings.Contains(string(rv), "reviewer not configured") {
		t.Fatal(string(rv))
	}
}

func TestSharedModuleProcessReviewNextContextAndSummary(t *testing.T) {
	product, s := sharedProductFixture(t, true)
	res, err := sharedLearningEvent(product, s, "review-run", "run_completed", "Check the artifact, not only exit zero", "evidence", "feedback", "gpt", "s1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res.Data), "candidate") {
		t.Fatal(string(res.Data))
	}
	on, ctx, err := sharedLearningContext(product, hookInput{SessionID: "next-session", RunID: "next-run"})
	if !on || err != nil || !strings.Contains(ctx.Context, sharedFixtureProcedure) {
		t.Fatalf("next run did not receive learned bytes: %v %s", err, ctx.Context)
	}
	b, _ := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if !strings.Contains(string(b), "skill_loaded") || !strings.Contains(string(b), "next-run") {
		t.Fatal("load receipt missing")
	}
	delivered, err := executeSharedLearning(product, s, "summary", nil)
	if err != nil || delivered.Status != "delivered" {
		t.Fatalf("fixture delivery: %+v %v", delivered, err)
	}
	if !strings.Contains(string(delivered.Data), "fixture/loopback") {
		t.Fatal("receipt hid that this was fixture delivery")
	}
}

func TestSharedFinalizeDoesNotRequireClaudeTranscript(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	handled, _, err := sharedLearningStop(product, hookInput{Principal: "gpt", SessionID: "s1", RunID: "native-gpt-run", LastAssistantMessage: "artifact checked"})
	if !handled || err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if !strings.Contains(string(b), "native-gpt-run") {
		t.Fatal("GPT event lost")
	}
	if handled, _, err := sharedLearningStop(product, hookInput{Principal: "gpt", SessionID: "s1"}); !handled || err == nil {
		t.Fatal("missing run identity was silently accepted")
	}
}

func TestSharedPlanCloseDispatchesReview(t *testing.T) {
	product, s := sharedProductFixture(t, true)
	err := appendPlanNodeEvents(product, []map[string]any{{"id": "close-N-001", "observed": "closed N-001 artifact verification", "actor": "gpt", "evidence": "fixture/receipt"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(product, "skills/learned/evidence.md")); err != nil {
		t.Fatal("plan close did not trigger process reviewer")
	}
	if _, err := os.Stat(filepath.Join(product, "learn/events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("parallel legacy journal created")
	}
	b, _ := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if !strings.Contains(string(b), "plan-node") {
		t.Fatal("provenance lost")
	}
}

func TestSharedProcessAdapterRejectsBadOutputAndTimeout(t *testing.T) {
	t.Setenv("AW_LEARNING_ADAPTER_HELPER", "1")
	for _, mode := range []string{"malformed", "oversize", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			a := learningAdapterFixture(t, mode)
			if mode == "timeout" {
				a.TimeoutMS = 30
			}
			started := time.Now()
			if _, err := runLearningProcess(context.Background(), "", a, json.RawMessage(`{}`)); err == nil {
				t.Fatal("invalid adapter result accepted")
			}
			if mode == "timeout" && time.Since(started) > 3*time.Second {
				t.Fatal("subprocess timeout not enforced")
			}
		})
	}
	a := learningAdapterFixture(t, "review")
	a.SHA256 = strings.Repeat("0", 64)
	if _, err := runLearningProcess(context.Background(), "", a, json.RawMessage(`{}`)); err == nil {
		t.Fatal("untrusted adapter binary ran")
	}
}

func TestSharedConfigFailureNeverFallsThroughToLegacy(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	os.WriteFile(filepath.Join(product, sharedLearningConfigFile), []byte(`{"schema":"broken"}`), 0600)
	handled, code := routeSharedLearn([]string{"event", "-product", product, "-observed", "artifact", "-class", "evidence", "-actor", "gpt", "-actor-kind", "gpt-window"})
	if !handled || code == 0 {
		t.Fatal("broken shared config fell back to legacy")
	}
	if _, err := os.Stat(filepath.Join(product, "learn/events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("legacy writer ran")
	}
}

func TestSharedModeCannotSilentlyDisableLegacyRules(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	path := filepath.Join(product, ".air-worker/learn/RULES.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	original := []byte("# Existing approved restriction\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	_, on, err := readSharedLearningSettings(product)
	if !on || err == nil {
		t.Fatal("switch silently disabled prior restrictions")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != string(original) {
		t.Fatal("legacy restriction changed")
	}
}
func TestSharedModeRefusesInternalLegacyWriter(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	for _, path := range []string{filepath.Join(product, "learn/events.jsonl"), filepath.Join(product, ".air-worker/learn/ledger.jsonl")} {
		if err := appendLearnJSON(path, map[string]string{"kind": "should-not-write"}); err == nil {
			t.Fatalf("legacy writer ran: %s", path)
		}
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("second journal created: %s", path)
		}
	}
}

func TestSharedDataFileRetainsProposalFields(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	payload := filepath.Join(t.TempDir(), "proposal.json")
	data := map[string]string{"proposal_id": "LP-json", "kind": "procedure", "target": "skills/learned/json.md", "pre_sha256": "", "content": sharedFixtureProcedure}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(payload, b, 0600); err != nil {
		t.Fatal(err)
	}
	handled, code := routeSharedLearn([]string{"propose", "-product", product, "-data-file", payload})
	if !handled || code != 0 {
		t.Fatalf("data-file overwritten by absent flags: handled=%v code=%d", handled, code)
	}
	if got, err := os.ReadFile(filepath.Join(product, "skills/learned/json.md")); err != nil || string(got) != sharedFixtureProcedure {
		t.Fatal("JSON procedure not applied")
	}
}
func TestSharedDataFileNullReturnsErrorInsteadOfPanic(t *testing.T) {
	product, _ := sharedProductFixture(t, false)
	payload := filepath.Join(t.TempDir(), "null.json")
	if err := os.WriteFile(payload, []byte("null"), 0600); err != nil {
		t.Fatal(err)
	}
	handled, code := routeSharedLearn([]string{"propose", "-product", product, "-data-file", payload})
	if !handled || code == 0 {
		t.Fatal("null data-file accepted")
	}
}

func TestSharedProcessAdapterSelfBinding(t *testing.T) {
	t.Setenv("AW_LEARNING_ADAPTER_HELPER", "1")
	product := filepath.Join(t.TempDir(), "product")
	if err := os.MkdirAll(product, 0700); err != nil {
		t.Fatal(err)
	}
	a := &learningProcessAdapter{
		Executable: "@self",
		SHA256:     "@self",
		Args:       []string{"-test.run=^TestLearningProcessHelper$", "--", "echo-product"},
		TimeoutMS:  5000,
	}
	raw, err := runLearningProcess(context.Background(), product, a, json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if json.Unmarshal(raw, &got) != nil || got["product"] != product {
		t.Fatalf("self adapter product binding lost: %s", raw)
	}
	a.SHA256 = strings.Repeat("0", 64)
	if _, err := runLearningProcess(context.Background(), product, a, json.RawMessage(`{}`)); err == nil {
		t.Fatal("@self adapter accepted a non-self SHA contract")
	}
}
