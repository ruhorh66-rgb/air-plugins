package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func sharedEventRows(t *testing.T, s sharedLearningSettings) []map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(s.RuntimeRoot, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var row map[string]any
		if err := json.Unmarshal([]byte(line), &row); err != nil {
			t.Fatal(err)
		}
		if row["schema"] == "air.learning.event/v1" {
			rows = append(rows, row)
		}
	}
	return rows
}

func TestSharedDataFileEventPayloadAndRetry(t *testing.T) {
	for _, op := range []string{"event", "add", "finalize"} {
		t.Run(op, func(t *testing.T) {
			product, s := sharedProductFixture(t, false)
			data := map[string]any{"run_id": "json-run", "observed": "original evidence text", "class": "evidence", "source": "json-source", "actor": "json-worker", "actor_kind": "gpt-window", "session": "json-session", "evidence": "receipt/json", "outcome": "fail"}
			b, _ := json.Marshal(data)
			payload := filepath.Join(t.TempDir(), "event.json")
			if err := os.WriteFile(payload, b, 0600); err != nil {
				t.Fatal(err)
			}
			args := []string{op, "-product", product, "-data-file", payload, "-actor", "json-worker"}
			if handled, code := routeSharedLearn(args); !handled || code != 0 {
				t.Fatalf("first call: %v %d", handled, code)
			}
			rows := sharedEventRows(t, s)
			if len(rows) != 1 {
				t.Fatalf("events=%d", len(rows))
			}
			for key, want := range map[string]string{"run_id": "json-run", "observed": "original evidence text", "class": "evidence", "source": "json-source", "principal": "gpt-window:json-worker", "session": "json-session", "outcome_ref": "receipt/json", "outcome": "fail"} {
				if rows[0][key] != want {
					t.Errorf("lost %s: got=%v want=%s", key, rows[0][key], want)
				}
			}
			if handled, code := routeSharedLearn(args); !handled || code != 3 {
				t.Fatalf("duplicate must be explicit: %v %d", handled, code)
			}
			if rows := sharedEventRows(t, s); len(rows) != 1 {
				t.Fatalf("retry duplicated event: %d", len(rows))
			}
		})
	}
}

func TestSharedDataFileExplicitFlagsOverrideIncludingEmpty(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	payload := filepath.Join(t.TempDir(), "event.json")
	data := map[string]string{"run_id": "payload-run", "observed": "payload text", "actor": "payload-worker", "source": "payload-source", "outcome_ref": "payload-ref", "outcome": "payload-outcome", "session": "payload-session"}
	b, _ := json.Marshal(data)
	if err := os.WriteFile(payload, b, 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{"finalize", "-product", product, "-data-file", payload, "-run-id", "flag-run", "-observed", "flag text", "-actor", "flag-worker", "-source", "", "-evidence", "", "-outcome", "", "-session", "flag-session"}
	if on, code := routeSharedLearn(args); !on || code != 0 {
		t.Fatalf("%v %d", on, code)
	}
	rows := sharedEventRows(t, s)
	for key, want := range map[string]string{"run_id": "flag-run", "observed": "flag text", "principal": "gpt-window:flag-worker", "outcome_ref": "", "outcome": "", "session": "flag-session"} {
		if rows[0][key] != want {
			t.Errorf("flag precedence %s: %v", key, rows[0][key])
		}
	}
	if value, exists := rows[0]["source"]; exists && value != "" {
		t.Errorf("empty explicit source did not clear payload: %v", value)
	}
}

func TestSharedDataFileRejectsNonStringEventIdentityBeforeMutation(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	payload := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(payload, []byte(`{"run_id":42,"observed":"must not write","actor":"worker"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if handled, code := routeSharedLearn([]string{"event", "-product", product, "-data-file", payload, "-actor", "worker"}); !handled || code == 0 {
		t.Fatal("non-string identity accepted")
	}
	if _, err := os.Stat(filepath.Join(s.RuntimeRoot, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("invalid payload mutated state")
	}
}

func TestSharedUnreadableLegacyDirectoryRefusesCutover(t *testing.T) {
	product, s := sharedProductFixture(t, false)
	path := filepath.Join(product, "learn", "rules")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not a directory; inspection must fail"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, on, err := readSharedLearningSettings(product); !on || err == nil {
		t.Fatal("ReadDir error counted as no legacy rules")
	}
	if on, code := routeSharedLearn([]string{"event", "-product", product, "-actor", "worker", "-observed", "must not switch"}); !on || code == 0 {
		t.Fatal("failed inspection allowed mutation")
	}
	if _, err := os.Stat(filepath.Join(s.RuntimeRoot, "events.jsonl")); !os.IsNotExist(err) {
		t.Fatal("failed cutover created journal")
	}
}

func prepareSharedPlanClose(t *testing.T) (string, sharedLearningSettings, planNode) {
	t.Helper()
	product := seedPlanNodeProduct(t)
	if code := cmdPlanNodeNew([]string{"-product", product, "-title", "artifact verification", "-parent", "test", "-owner", "worker", "-done-when", "fixture verification"}); code != 0 {
		t.Fatal("new node failed")
	}
	nodes, err := listPlanNodes(product)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes=%v err=%v", nodes, err)
	}
	_, s := sharedProductFixture(t, true)
	b, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(product, sharedLearningConfigFile), b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(s.RuntimeRoot, 0700); err != nil {
		t.Fatal(err)
	}
	return product, s, nodes[0]
}

func TestSharedCloseRetryAfterEventPersistence(t *testing.T) {
	product, s, n := prepareSharedPlanClose(t)
	// This is a real filesystem failure AFTER the module commits the event and RV:
	// its deferred operation trace cannot create the logs directory.
	blocked := filepath.Join(s.RuntimeRoot, "logs")
	injectSharedPostEventLogFailure(t, product, s)
	args := []string{n.ID, "-product", product, "-receipt", "fixture/PASS.json", "-actor-kind", "gpt-window", "-actor", "first-worker", "-json"}
	if code := cmdPlanNodeClose(args); code == 0 {
		t.Fatal("partial log failure falsely succeeded")
	}
	rows := sharedEventRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("completion events=%d", len(rows))
	}
	firstID := rows[0]["run_id"]
	firstNode, err := readPlanNode(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if firstNode.Status != "closed" {
		t.Errorf("committed shared completion was rolled back to %s", firstNode.Status)
	}
	firstBody, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	oldNow := planNodeNow
	defer func() { planNodeNow = oldNow }()
	planNodeNow = func() time.Time { return time.Now().UTC().Add(time.Hour) }
	args[len(args)-2] = "second-worker"
	if code := cmdPlanNodeClose(args); code != 0 {
		t.Fatalf("retry failed: %d", code)
	}
	rows = sharedEventRows(t, s)
	if len(rows) != 1 || rows[0]["run_id"] != firstID {
		t.Fatalf("retry changed completion identity: %v", rows)
	}
	secondBody, _ := os.ReadFile(n.Path)
	if string(secondBody) != string(firstBody) {
		t.Error("retry changed original completion/receipt/timestamp")
	}
	reviews, _ := filepath.Glob(filepath.Join(s.RuntimeRoot, "reviews", "RV-*.json"))
	if len(reviews) != 1 {
		t.Fatalf("review duplicated: %d", len(reviews))
	}
	code, out := captureLoopOutput(t, func() int { return cmdPlanNodeClose(args) })
	if code != 0 || !json.Valid([]byte(strings.TrimSpace(out))) {
		t.Fatalf("completed replay must be JSON: %d %q", code, out)
	}
}

func TestSharedPendingCloseCannotChangeReceiptOrOverwriteNode(t *testing.T) {
	product, s, n := prepareSharedPlanClose(t)
	blocked := filepath.Join(s.RuntimeRoot, "logs")
	injectSharedPostEventLogFailure(t, product, s)
	args := []string{n.ID, "-product", product, "-receipt", "fixture/original.json", "-actor", "worker", "-actor-kind", "gpt-window"}
	if code := cmdPlanNodeClose(args); code == 0 {
		t.Fatal("expected partial failure")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	bad := append([]string(nil), args...)
	bad[4] = "fixture/different.json"
	if code := cmdPlanNodeClose(bad); code == 0 {
		t.Error("pending completion accepted a different receipt")
	}
	after, _ := os.ReadFile(n.Path)
	if string(after) != string(before) {
		t.Fatal("conflicting receipt changed node")
	}
	changed := append(append([]byte(nil), before...), []byte("\nOutside edit must survive.\n")...)
	if err := os.WriteFile(n.Path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if code := cmdPlanNodeClose(args); code == 0 {
		t.Error("pending completion silently accepted externally changed node")
	}
	after, _ = os.ReadFile(n.Path)
	if string(after) != string(changed) {
		t.Fatal("external node edit overwritten")
	}
}
