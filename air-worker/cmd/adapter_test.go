package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"
)

func writeAdapterFixture(t *testing.T, root, plan string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte(`{"plan":"PLAN.md"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeAdapterReceipt(t *testing.T, root, name string, r *jobReceipt) string {
	t.Helper()
	dir := filepath.Join(root, ".woody", "jobs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name+".receipt.json")
	if err := writeJobReceipt(path, r); err != nil {
		t.Fatal(err)
	}
	return path
}

func runAdapterForTest(t *testing.T, args ...string) (int, adapterEnvelope) {
	t.Helper()
	var out bytes.Buffer
	code := cmdAdapterTo(args, &out)
	var result adapterEnvelope
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid adapter JSON %q: %v", out.String(), err)
	}
	return code, result
}

func adapterTreeSnapshot(t *testing.T, root string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if info.IsDir() {
			files = append(files, rel+"/")
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, rel+":"+string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func TestAdapterStatusIsReadOnlyAndReportsProgress(t *testing.T) {
	root := t.TempDir()
	writeAdapterFixture(t, root, "- [x] script first\n- [ ] sonnet second\n")
	before := adapterTreeSnapshot(t, root)

	code, got := runAdapterForTest(t, "-action", "status", "-product", root)
	if code != 0 || got.ExitCode != 0 || got.Outcome != "needs_action" {
		t.Fatalf("unexpected result: code=%d result=%+v", code, got)
	}
	if got.Progress != (adapterProgress{Closed: 1, Total: 2}) || got.CurrentStep != "2" || got.NextAction != "run_loop" || got.StopReason != "no_live_worker" {
		t.Fatalf("unexpected progress/transition: %+v", got)
	}
	if got.Receipts == nil || got.Workers == nil || len(got.Receipts) != 0 || len(got.Workers) != 0 {
		t.Fatalf("arrays must be present and empty: receipts=%#v workers=%#v", got.Receipts, got.Workers)
	}
	if after := adapterTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("status mutated product tree\nbefore: %#v\nafter:  %#v", before, after)
	}
	if _, err := os.Stat(filepath.Join(root, ".woody")); !os.IsNotExist(err) {
		t.Fatalf("status created state: %v", err)
	}
}

func TestAdapterStatusReportsOnlyVerifiedLiveCurrentWorker(t *testing.T) {
	root := t.TempDir()
	writeAdapterFixture(t, root, "- [ ] sonnet work\n")
	outputPath := filepath.Join(root, ".woody", "jobs", "job.out")
	receiptPath := writeAdapterReceipt(t, root, "job", &jobReceipt{
		JobID: "job-1", PID: os.Getpid(), StartedAt: time.Now().UTC(), Status: jobStatusRunning,
		OutputPath: outputPath, Product: root, Step: "1", Operation: "executor",
		Runner: "codex", Principal: "alice", Session: "session-1", ProcessStarted: true,
	})
	before := adapterTreeSnapshot(t, root)

	code, got := runAdapterForTest(t, "-action", "status", "-product", root, "-principal", "alice", "-session-key", "session-1")
	if code != 0 || got.Outcome != "running" || got.NextAction != "wait" || got.StopReason != "" {
		t.Fatalf("unexpected running result: code=%d result=%+v", code, got)
	}
	if len(got.Receipts) != 1 || got.Receipts[0].Path != receiptPath || got.Receipts[0].SessionKey != "session-1" {
		t.Fatalf("receipts = %#v", got.Receipts)
	}
	if len(got.Workers) != 1 || got.Workers[0].PID != os.Getpid() || got.Workers[0].Principal != "alice" {
		t.Fatalf("workers = %#v", got.Workers)
	}
	if got.DetailPath != outputPath {
		t.Fatalf("detail_path = %q", got.DetailPath)
	}
	if after := adapterTreeSnapshot(t, root); !reflect.DeepEqual(before, after) {
		t.Fatalf("status mutated receipt tree\nbefore: %#v\nafter:  %#v", before, after)
	}
}

func TestAdapterHelperExit(t *testing.T) {
	if os.Getenv("AIR_WORKER_ADAPTER_HELPER_EXIT") == "1" {
		os.Exit(0)
	}
}

func deadAdapterPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=TestAdapterHelperExit")
	cmd.Env = append(os.Environ(), "AIR_WORKER_ADAPTER_HELPER_EXIT=1")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestAdapterStatusRejectsUnverifiedStaleAndUnrelatedRunningReceipts(t *testing.T) {
	deadPID := deadAdapterPID(t)
	tests := []struct {
		name           string
		expectReceipts int
		edit           func(root string, r *jobReceipt)
	}{
		{name: "process not started", expectReceipts: 1, edit: func(_ string, r *jobReceipt) { r.ProcessStarted = false }},
		{name: "dead pid", expectReceipts: 1, edit: func(_ string, r *jobReceipt) { r.PID = deadPID }},
		{name: "wrong product", edit: func(_ string, r *jobReceipt) { r.Product = t.TempDir() }},
		{name: "wrong step", edit: func(_ string, r *jobReceipt) { r.Step = "99" }},
		{name: "wrong principal", edit: func(_ string, r *jobReceipt) { r.Principal = "other" }},
		{name: "wrong session", edit: func(_ string, r *jobReceipt) { r.Session = "other" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeAdapterFixture(t, root, "- [ ] sonnet work\n")
			r := &jobReceipt{JobID: "stale", PID: os.Getpid(), StartedAt: time.Now().UTC(), Status: jobStatusRunning,
				Product: root, Principal: "alice", Session: "session-1", Step: "1", Operation: "executor", ProcessStarted: true}
			tt.edit(root, r)
			writeAdapterReceipt(t, root, "stale", r)
			_, got := runAdapterForTest(t, "-action", "status", "-product", root, "-principal", "alice", "-session-key", "session-1")
			if got.Outcome != "needs_action" || got.StopReason != "no_live_worker" || len(got.Workers) != 0 {
				t.Fatalf("stale/unrelated receipt reported running: %+v", got)
			}
			if len(got.Receipts) != tt.expectReceipts {
				t.Fatalf("receipt correlation mismatch: got %d receipts, want %d: %+v", len(got.Receipts), tt.expectReceipts, got)
			}
		})
	}
}

func TestAdapterReceiptsAreBoundedRelevantFirstAndNewestFirst(t *testing.T) {
	root := t.TempDir()
	writeAdapterFixture(t, root, "- [ ] sonnet work\n")
	base := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		writeAdapterReceipt(t, root, fmt.Sprintf("job-%02d", i), &jobReceipt{
			JobID: fmt.Sprintf("job-%02d", i), StartedAt: base.Add(time.Duration(i) * time.Minute),
			Status: jobStatusDone, Product: root, Step: "1", Operation: "executor",
		})
	}
	_, got := runAdapterForTest(t, "-action", "status", "-product", root)
	if len(got.Receipts) != adapterReceiptLimit || got.ReceiptsOmitted != 4 {
		t.Fatalf("receipt bound = %d omitted=%d", len(got.Receipts), got.ReceiptsOmitted)
	}
	if got.Receipts[0].JobID != "job-11" || got.Receipts[1].JobID != "job-10" || got.Receipts[7].JobID != "job-04" {
		t.Fatalf("receipts are not newest-first: %#v", got.Receipts)
	}
}

func TestAdapterCompletedStatusRetainsLatestIdentityBoundReceipt(t *testing.T) {
	root := t.TempDir()
	writeAdapterFixture(t, root, "- [x] script done\n- [x] verify done\n")
	older := time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
	writeAdapterReceipt(t, root, "older", &jobReceipt{
		JobID: "older", StartedAt: older, Status: jobStatusDone, Product: root,
		Step: "1", Operation: "executor", Principal: "hermes", Session: "session-1",
	})
	latestPath := writeAdapterReceipt(t, root, "latest", &jobReceipt{
		JobID: "latest", StartedAt: older.Add(time.Minute), Status: jobStatusDone, Product: root,
		Step: "2", Operation: "judge", Principal: "hermes", Session: "session-1",
	})
	writeAdapterReceipt(t, root, "other-session", &jobReceipt{
		JobID: "other-session", StartedAt: older.Add(2 * time.Minute), Status: jobStatusDone, Product: root,
		Step: "2", Operation: "judge", Principal: "hermes", Session: "session-2",
	})

	code, got := runAdapterForTest(t, "-action", "status", "-product", root, "-principal", "hermes", "-session-key", "session-1")
	if code != 0 || got.Outcome != "completed" || got.CurrentStep != "" {
		t.Fatalf("unexpected completed result: code=%d result=%+v", code, got)
	}
	if len(got.Receipts) != 2 || got.Receipts[0].Path != latestPath || got.Receipts[0].Step != "2" {
		t.Fatalf("completed receipts lost or misordered: %#v", got.Receipts)
	}
}

func TestAdapterRelativeConfigResolvesAgainstProductRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "configs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ALT.md"), []byte("- [x] script done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "configs", "custom.json"), []byte(`{"plan":"ALT.md"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Chdir(outside); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(old) }()
	code, got := runAdapterForTest(t, "-action", "status", "-product", root, "-config", filepath.Join("configs", "custom.json"))
	if code != 0 || got.Outcome != "completed" || got.Progress != (adapterProgress{Closed: 1, Total: 1}) {
		t.Fatalf("relative config did not resolve from product: code=%d result=%+v", code, got)
	}
}

func TestAdapterStatusGateCompleteAndErrors(t *testing.T) {
	root := t.TempDir()
	writeAdapterFixture(t, root, "| 1 | approval | - | human approval |\n")
	code, got := runAdapterForTest(t, "-action", "status", "-product", root)
	if code != 0 || got.Outcome != "waiting" || got.NextAction != "approve_lpr" {
		t.Fatalf("unexpected gate result: code=%d result=%+v", code, got)
	}

	root = t.TempDir()
	writeAdapterFixture(t, root, "- [x] script done\n")
	code, got = runAdapterForTest(t, "-action", "status", "-product", root)
	if code != 0 || got.Outcome != "completed" || got.StopReason != "plan_complete" {
		t.Fatalf("unexpected complete result: code=%d result=%+v", code, got)
	}

	code, got = runAdapterForTest(t, "-action", "execute", "-product", root)
	if code != 2 || got.Outcome != "error" || got.StopReason != "unsupported_action" {
		t.Fatalf("unexpected unsupported-action result: code=%d result=%+v", code, got)
	}
}
