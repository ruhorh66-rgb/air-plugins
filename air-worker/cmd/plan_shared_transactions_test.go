package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type sharedPlanChildRequest struct {
	Action string
	Args   []string
}

func TestSharedPlanConsumerChild(t *testing.T) {
	if os.Getenv("AW_SHARED_PLAN_CHILD") != "1" {
		t.Skip("separate fixture consumer only")
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, 65537))
	if err != nil || len(b) > 65536 {
		os.Exit(2)
	}
	var req sharedPlanChildRequest
	if json.Unmarshal(b, &req) != nil {
		os.Exit(2)
	}
	if os.Getenv("AW_SHARED_PLAN_CREATE_LOG_FAULT") == "1" {
		// New/migrate emit observations, not completed runs; their judge must not
		// be invoked just to trigger this fixture. Fail the operation trace after
		// its initial successful preflight, so Observe can persist the first event.
		original := sharedPlanPreflight
		sharedPlanPreflight = func(root string, settings sharedLearningSettings) error {
			if err := original(root, settings); err != nil {
				return err
			}
			logs := filepath.Join(settings.RuntimeRoot, "logs")
			saved := filepath.Join(settings.RuntimeRoot, "fixture-saved-logs")
			if _, err := os.Stat(saved); os.IsNotExist(err) {
				if err := os.Rename(logs, saved); err != nil {
					return err
				}
				return os.WriteFile(logs, []byte("fixture trace obstruction"), 0600)
			}
			return nil
		}
	}
	code := 2
	switch req.Action {
	case "new":
		code = cmdPlanNodeNew(req.Args)
	case "migrate":
		code = cmdPlanMigrate(req.Args)
	case "close":
		code = cmdPlanNodeClose(req.Args)
	}
	os.Exit(code)
}

func runSharedPlanChild(t *testing.T, action string, args []string) (int, []byte) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "-test.run=^TestSharedPlanConsumerChild$")
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(), "AW_SHARED_PLAN_CHILD=1")
	b, _ := json.Marshal(sharedPlanChildRequest{Action: action, Args: args})
	cmd.Stdin = bytes.NewReader(b)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("consumer process exceeded bound: %v", ctx.Err())
	}
	if err == nil {
		return 0, out
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode(), out
	}
	t.Fatalf("consumer start: %v", err)
	return 2, out
}

func prepareSharedPlanCreateFixture(t *testing.T) (string, sharedLearningSettings) {
	t.Helper()
	root := seedPlanNodeProduct(t)
	_, s := sharedProductFixture(t, true)
	b, _ := json.Marshal(s)
	if err := os.WriteFile(filepath.Join(root, sharedLearningConfigFile), b, 0600); err != nil {
		t.Fatal(err)
	}
	return root, s
}

func sharedPlanCreateArgs(action, root, key string) []string {
	args := []string{"-product", root, "-owner", "fixture-owner", "-actor", "first-consumer", "-actor-kind", "gpt-window", "-request-id", key, "-trigger", "fixture retry contract", "-json"}
	if action == "new" {
		args = append(args, "-title", "fixture task", "-parent", "fixture stage", "-done-when", "verified fixture outcome")
	}
	return args
}

func TestSharedPlanNewAndMigrationRetryAcrossConsumerProcesses(t *testing.T) {
	for _, action := range []string{"new", "migrate"} {
		t.Run(action, func(t *testing.T) {
			root, s := prepareSharedPlanCreateFixture(t)
			want := 1
			if action == "migrate" {
				f, err := os.OpenFile(filepath.Join(root, "PLAN.md"), os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = f.WriteString("\n## first section\nFirst body is retained.\n\n## second section\nSecond body is retained.\n")
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
				want = 2
			}
			t.Setenv("AW_SHARED_PLAN_CREATE_LOG_FAULT", "1")
			args := sharedPlanCreateArgs(action, root, "same-request")
			code, out := runSharedPlanChild(t, action, args)
			if code == 0 {
				t.Fatalf("post-event failure returned success: %s", out)
			}
			rows := sharedEventRows(t, s)
			if len(rows) != 1 {
				t.Fatalf("expected first partial event, got %d: %s", len(rows), out)
			}
			firstID := rows[0]["run_id"]
			nodes, err := listPlanNodes(root)
			if err != nil || len(nodes) != want {
				t.Fatalf("pending nodes lost: %d %v %s", len(nodes), err, out)
			}
			before := map[string]string{}
			for _, n := range nodes {
				b, err := os.ReadFile(n.Path)
				if err != nil {
					t.Fatal(err)
				}
				before[n.ID] = learnSHA(b)
			}
			if err := os.Remove(filepath.Join(s.RuntimeRoot, "logs")); err != nil {
				t.Fatal(err)
			}
			for i, arg := range args {
				if arg == "first-consumer" {
					args[i] = "restarted-consumer"
				}
			}
			code, out = runSharedPlanChild(t, action, args)
			if code != 0 || !json.Valid(bytes.TrimSpace(out)) {
				t.Fatalf("restart: %d %s", code, out)
			}
			rows = sharedEventRows(t, s)
			if len(rows) != want || rows[0]["run_id"] != firstID {
				t.Fatalf("batch duplicated or replaced: %v", rows)
			}
			for _, row := range rows {
				if row["principal"] != "gpt-window:first-consumer" {
					t.Errorf("original actor lost: %v", row)
				}
			}
			nodes, err = listPlanNodes(root)
			if err != nil || len(nodes) != want {
				t.Fatalf("restart nodes: %d %v", len(nodes), err)
			}
			for _, n := range nodes {
				b, _ := os.ReadFile(n.Path)
				if before[n.ID] != learnSHA(b) {
					t.Errorf("node snapshot changed on retry: %s", n.ID)
				}
			}
			code, out = runSharedPlanChild(t, action, args)
			if code != 0 || !json.Valid(bytes.TrimSpace(out)) {
				t.Fatalf("completed replay: %d %s", code, out)
			}
			if rows := sharedEventRows(t, s); len(rows) != want {
				t.Fatal("completed replay emitted another event")
			}
			rv, _ := filepath.Glob(filepath.Join(s.RuntimeRoot, "reviews", "RV-*.json"))
			if len(rv) != 0 {
				t.Fatalf("review count=%d", len(rv))
			}
			// Reusing an idempotency key with different content is never a new request.
			for i, arg := range args {
				if arg == "fixture retry contract" {
					args[i] = "different trigger"
				}
			}
			if code, out := runSharedPlanChild(t, action, args); code == 0 {
				t.Fatalf("conflicting payload accepted: %s", out)
			}
		})
	}
}

func TestSharedPlanCloseRetryAcrossConsumerProcesses(t *testing.T) {
	root, s, n := prepareSharedPlanClose(t)
	injectSharedPostEventLogFailure(t, root, s)
	args := []string{n.ID, "-product", root, "-receipt", "fixture/pass.json", "-actor", "first-consumer", "-actor-kind", "gpt-window", "-json"}
	if code, out := runSharedPlanChild(t, "close", args); code == 0 {
		t.Fatalf("expected post-event error: %s", out)
	}
	first := sharedEventRows(t, s)
	before, _ := os.ReadFile(n.Path)
	if err := os.Remove(filepath.Join(s.RuntimeRoot, "logs")); err != nil {
		t.Fatal(err)
	}
	args[6] = "restarted-consumer"
	if code, out := runSharedPlanChild(t, "close", args); code != 0 || !json.Valid(bytes.TrimSpace(out)) {
		t.Fatalf("restart close: %d %s", code, out)
	}
	rows := sharedEventRows(t, s)
	after, _ := os.ReadFile(n.Path)
	if len(rows) != 1 || rows[0]["run_id"] != first[0]["run_id"] || !bytes.Equal(before, after) {
		t.Fatal("close identity did not survive a process restart")
	}
}

func TestSharedPlanCloseKeepsEditMadeDuringPreflight(t *testing.T) {
	root, _, n := prepareSharedPlanClose(t)
	before, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	changed := append(append([]byte(nil), before...), []byte("\nEdit made while preflight was running.\n")...)
	original := sharedPlanPreflight
	defer func() { sharedPlanPreflight = original }()
	sharedPlanPreflight = func(root string, s sharedLearningSettings) error {
		if err := os.WriteFile(n.Path, changed, 0600); err != nil {
			return err
		}
		return original(root, s)
	}
	code := cmdPlanNodeClose([]string{n.ID, "-product", root, "-receipt", "fixture/pass", "-actor", "worker", "-actor-kind", "gpt-window"})
	if code == 0 {
		t.Fatal("changed snapshot accepted")
	}
	after, err := os.ReadFile(n.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, changed) {
		t.Fatal("edit during preflight was overwritten")
	}
	got, err := readPlanNode(n.Path)
	if err != nil || got.Status != "open" {
		t.Fatalf("node changed despite stale snapshot: %v %v", got, err)
	}
}

func TestSharedPlanCreateRequiresStableRequestBeforeMutation(t *testing.T) {
	for _, action := range []string{"new", "migrate"} {
		t.Run(action, func(t *testing.T) {
			root, s := prepareSharedPlanCreateFixture(t)
			before, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
			args := sharedPlanCreateArgs(action, root, "")
			if code, _ := runSharedPlanChild(t, action, args); code == 0 {
				t.Fatal("missing request id accepted")
			}
			after, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
			if !bytes.Equal(before, after) {
				t.Fatal("missing request id changed PLAN")
			}
			nodes, err := listPlanNodes(root)
			if err != nil || len(nodes) != 0 {
				t.Fatalf("missing request id created nodes: %v %v", nodes, err)
			}
			if _, err := os.Stat(filepath.Join(s.RuntimeRoot, "events.jsonl")); !os.IsNotExist(err) {
				t.Fatal("missing request id changed journal")
			}
		})
	}
}

func TestSharedPlanMigrationRetainsOutsideEditOnResume(t *testing.T) {
	root, s := prepareSharedPlanCreateFixture(t)
	f, err := os.OpenFile(filepath.Join(root, "PLAN.md"), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("\n## one\nRetained content.\n")
	f.Close()
	t.Setenv("AW_SHARED_PLAN_CREATE_LOG_FAULT", "1")
	args := sharedPlanCreateArgs("migrate", root, "migration-request")
	if code, _ := runSharedPlanChild(t, "migrate", args); code == 0 {
		t.Fatal("expected partial mutation")
	}
	if err := os.Remove(filepath.Join(s.RuntimeRoot, "logs")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "PLAN.md")
	before, _ := os.ReadFile(path)
	changed := append(append([]byte(nil), before...), []byte("\nExternal edit must survive.\n")...)
	if err := os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	if code, out := runSharedPlanChild(t, "migrate", args); code == 0 {
		t.Fatalf("external migration edit accepted: %s", out)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, changed) {
		t.Fatal("migration overwrote outside edit")
	}
}

func TestSharedNewPlanPublisherNeverReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.md")
	if err := publishNewPlanNode(path, []byte("first owner")); err != nil {
		t.Fatal(err)
	}
	if err := publishNewPlanNode(path, []byte("replacement")); err == nil {
		t.Fatal("existing file replaced")
	}
	b, _ := os.ReadFile(path)
	if strings.TrimSpace(string(b)) != "first owner" {
		t.Fatal("existing bytes changed")
	}
}
