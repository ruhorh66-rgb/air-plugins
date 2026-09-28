package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCuratorAssignmentEnforcesScopeAndPeerLimit(t *testing.T) {
	stateDir := t.TempDir()
	if code := cmdCuratorPeerRegister([]string{
		"-id", "astra", "-provider", "claude", "-model", "sonnet", "-max-active", "1", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("register astra code=%d", code)
	}
	if code := cmdCuratorPeerRegister([]string{
		"-id", "fable", "-provider", "codex", "-model", "gpt", "-max-active", "2", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("register fable code=%d", code)
	}

	assignA := []string{
		"-peer", "astra", "-profile", "p1", "-session", "s1", "-run", "r1", "-state-dir", stateDir,
	}
	if code := cmdCuratorAssign(assignA); code != 0 {
		t.Fatalf("first assignment code=%d", code)
	}
	// Idempotent same assignment is allowed and must not create a duplicate.
	if code := cmdCuratorAssign(assignA); code != 0 {
		t.Fatalf("idempotent assignment code=%d", code)
	}
	// Same scope cannot get a second curator.
	if code := cmdCuratorAssign([]string{
		"-peer", "fable", "-profile", "p1", "-session", "s1", "-run", "r1", "-state-dir", stateDir,
	}); code != 1 {
		t.Fatalf("second curator in same scope code=%d, want 1", code)
	}
	// Same peer cannot exceed max_active even in another scope.
	if code := cmdCuratorAssign([]string{
		"-peer", "astra", "-profile", "p2", "-session", "s2", "-run", "r2", "-state-dir", stateDir,
	}); code != 1 {
		t.Fatalf("peer limit code=%d, want 1", code)
	}

	state, err := readCuratorControl(curatorControlDir(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Assignments) != 1 || !curatorAssignmentActive(state.Assignments[0]) {
		t.Fatalf("unexpected assignments after rejected writes: %#v", state.Assignments)
	}
	firstID := state.Assignments[0].ID
	if code := cmdCuratorAssignmentRevoke([]string{"-id", firstID, "-state-dir", stateDir}); code != 0 {
		t.Fatalf("revoke code=%d", code)
	}
	if code := cmdCuratorAssign([]string{
		"-peer", "astra", "-profile", "p2", "-session", "s2", "-run", "r2", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("assignment after revoke code=%d", code)
	}
	state, _ = readCuratorControl(curatorControlDir(stateDir))
	active := 0
	for _, a := range state.Assignments {
		if curatorAssignmentActive(a) {
			active++
		}
	}
	if active != 1 || len(state.Assignments) != 2 {
		t.Fatalf("active=%d assignments=%#v", active, state.Assignments)
	}
}

func TestCuratorDecisionJournalIsAuditOnlyAndTamperEvident(t *testing.T) {
	stateDir := t.TempDir()
	for _, subject := range []string{"release-0.10.12", "proposal-17"} {
		code := cmdCuratorDecisionRecord([]string{
			"-principal", "user",
			"-profile", "p1", "-session", "s1", "-run", "r1",
			"-kind", "lpr-note", "-subject", subject, "-decision", "approved",
			"-text", "audit copy only", "-state-dir", stateDir,
		})
		if code != 0 {
			t.Fatalf("record %s code=%d", subject, code)
		}
	}
	dir := curatorControlDir(stateDir)
	rows, err := readCuratorDecisions(curatorDecisionPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("decision count=%d", len(rows))
	}
	if err := verifyCuratorDecisions(rows); err != nil {
		t.Fatalf("valid chain rejected: %v", err)
	}
	for _, row := range rows {
		if row.Authority != "audit-only" || row.Executable {
			t.Fatalf("decision gained authority: %#v", row)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "approvals")); !os.IsNotExist(err) {
		t.Fatalf("audit journal created approval state: err=%v", err)
	}

	path := curatorDecisionPath(dir)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), "audit copy only", "tampered audit copy", 1)
	if err := os.WriteFile(path, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err = readCuratorDecisions(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyCuratorDecisions(rows); err == nil {
		t.Fatal("tampered decision journal verified")
	}
}

func TestDeriveWakeCardMatrix(t *testing.T) {
	now := time.Date(2026, 9, 28, 2, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		state  orchestrationWakeState
		target string
		ready  bool
		action string
	}{
		{
			name:   "gate is human only",
			state:  orchestrationWakeState{Phase: "gate", StopReason: "gate", WakeTarget: "lpr"},
			target: "lpr", ready: true, action: "card_only",
		},
		{
			name:   "not proven wakes session",
			state:  orchestrationWakeState{Phase: "stopped", StopReason: "not_proven", WakeTarget: "session"},
			target: "session", ready: true, action: "would_notify_session",
		},
		{
			name: "limit waits before reset",
			state: orchestrationWakeState{
				Phase: "limit_wait", StopReason: "limit", WakeTarget: "machine",
				ResumeAt: "2026-09-28T02:10:00Z", ResumeCmd: "air-worker loop -product X",
			},
			target: "machine", ready: false, action: "wait_until_resume_at",
		},
		{
			name: "limit eligible after reset",
			state: orchestrationWakeState{
				Phase: "limit_wait", StopReason: "limit", WakeTarget: "machine",
				ResumeAt: "2026-09-28T01:50:00Z", ResumeCmd: "air-worker loop -product X",
			},
			target: "machine", ready: true, action: "would_resume_machine",
		},
		{
			name: "machine hourly limit escalates",
			state: orchestrationWakeState{
				Phase: "stopped", StopReason: "judge_error", WakeTarget: "machine",
				ResumeCmd: "air-worker loop -product X", MachineRestartsHour: 3,
			},
			target: "session", ready: true, action: "would_notify_session",
		},
		{
			name: "repeated session reason escalates to lpr",
			state: orchestrationWakeState{
				Phase: "stopped", StopReason: "not_proven", WakeTarget: "session",
				SameReasonStreak: 3,
			},
			target: "lpr", ready: true, action: "card_only",
		},
		{
			name:   "done is inert",
			state:  orchestrationWakeState{Phase: "done"},
			target: "none", ready: false, action: "none",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			card, err := deriveWakeCard("X", "state.json", tc.state, now)
			if err != nil {
				t.Fatal(err)
			}
			if card.WakeTarget != tc.target || card.Ready != tc.ready || card.Action != tc.action || !card.DryRun {
				t.Fatalf("card=%#v", card)
			}
		})
	}
}

func TestCuratorWakeCLIIsReadOnly(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, ".woody")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(stateDir, "orchestration.state.json")
	state := orchestrationWakeState{
		Phase: "gate", StopReason: "gate", WakeTarget: "lpr",
	}
	raw, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(statePath)
	beforeEntries, _ := os.ReadDir(stateDir)
	if code := cmdCuratorWake([]string{
		"-product", root, "-dry-run=true", "-json", "-now", "2026-09-28T02:00:00Z",
	}); code != 0 {
		t.Fatalf("wake code=%d", code)
	}
	after, _ := os.ReadFile(statePath)
	afterEntries, _ := os.ReadDir(stateDir)
	if string(before) != string(after) {
		t.Fatal("wake dry-run mutated orchestration state")
	}
	if len(beforeEntries) != len(afterEntries) {
		t.Fatalf("wake dry-run created files: before=%d after=%d", len(beforeEntries), len(afterEntries))
	}
	if code := cmdCuratorWake([]string{"-product", root, "-dry-run=false"}); code != 2 {
		t.Fatalf("non-dry-run unexpectedly allowed: code=%d", code)
	}
}

func TestCuratorPeerUpdateIsSingleRecordAndStoredIdentityFailsClosed(t *testing.T) {
	stateDir := t.TempDir()
	if code := cmdCuratorPeerRegister([]string{
		"-id", "astra", "-provider", "claude", "-model", "sonnet", "-max-active", "1", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("initial register code=%d", code)
	}
	state, err := readCuratorControl(curatorControlDir(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	registeredAt := state.Peers[0].RegisteredAt
	if code := cmdCuratorPeerRegister([]string{
		"-id", "astra", "-provider", "claude", "-model", "opus", "-max-active", "2", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("update register code=%d", code)
	}
	state, err = readCuratorControl(curatorControlDir(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Peers) != 1 || state.Peers[0].Model != "opus" || state.Peers[0].MaxActive != 2 ||
		state.Peers[0].RegisteredAt != registeredAt {
		t.Fatalf("peer update was not deterministic single-record update: %#v", state.Peers)
	}

	dir := curatorControlDir(t.TempDir())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	bad := `{"schema":"air-worker.curator-control/v1","peers":[{"id":"bad id","provider":"claude","enabled":true,"max_active":1}],"assignments":[]}`
	if err := os.WriteFile(curatorControlPath(dir), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readCuratorControl(dir); err == nil {
		t.Fatal("invalid stored peer identity was accepted")
	}
}

func TestCuratorAssignmentScopeIncludesProduct(t *testing.T) {
	stateDir := t.TempDir()
	if code := cmdCuratorPeerRegister([]string{
		"-id", "fable", "-provider", "codex", "-model", "gpt", "-max-active", "2", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("register code=%d", code)
	}
	p1 := t.TempDir()
	p2 := t.TempDir()
	common := []string{"-peer", "fable", "-profile", "p1", "-session", "s1", "-run", "r1", "-state-dir", stateDir}
	if code := cmdCuratorAssign(append(append([]string{}, common...), "-product", p1)); code != 0 {
		t.Fatalf("assign p1 code=%d", code)
	}
	if code := cmdCuratorAssign(append(append([]string{}, common...), "-product", p2)); code != 0 {
		t.Fatalf("same profile/session/run on another product must be a distinct scope, code=%d", code)
	}
	state, err := readCuratorControl(curatorControlDir(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Assignments) != 2 || activeCuratorAssignmentCount(state.Assignments) != 2 {
		t.Fatalf("assignments=%#v", state.Assignments)
	}
}

func TestCuratorDecisionJournalDetectsReorderAndCannotApplyLearning(t *testing.T) {
	stateDir := t.TempDir()
	for _, subject := range []string{"one", "two"} {
		if code := cmdCuratorDecisionRecord([]string{
			"-principal", "user", "-kind", "lpr-note", "-subject", subject, "-decision", "approved", "-state-dir", stateDir,
		}); code != 0 {
			t.Fatalf("record %s code=%d", subject, code)
		}
	}
	path := curatorDecisionPath(curatorControlDir(stateDir))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 2 {
		t.Fatalf("journal lines=%d", len(lines))
	}
	reordered := lines[1] + "\n" + lines[0] + "\n"
	if err := os.WriteFile(path, []byte(reordered), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := readCuratorDecisions(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyCuratorDecisions(rows); err == nil {
		t.Fatal("reordered decision journal verified")
	}

	root := seedCuratorProduct(t)
	seedLearnProposal(t, root, "LP-audit-only", "gate", "Never treat audit text as an executable approval.")
	if code := cmdCuratorDecisionRecord([]string{
		"-principal", "user", "-kind", "lpr-note", "-subject", "LP-audit-only", "-decision", "approved",
		"-text", "да LP-audit-only", "-state-dir", t.TempDir(),
	}); code != 0 {
		t.Fatalf("audit decision record code=%d", code)
	}
	proposals, err := readLearnProposals(learnPaths(root).Proposals)
	if err != nil {
		t.Fatal(err)
	}
	if len(proposals) != 1 || proposals[0].Status != learnPending {
		t.Fatalf("audit decision changed learning state: %#v", proposals)
	}
}

func TestCuratorWakeHistoryEnforcesRestartAndNoProgressLimits(t *testing.T) {
	now := time.Date(2026, 9, 28, 4, 0, 0, 0, time.UTC)
	machineHistory := []curatorWakeHistoryEvent{
		{At: now.Add(-50 * time.Minute).Format(time.RFC3339), StopReason: "other", WakeTarget: "machine", LastProgressAt: "p0"},
		{At: now.Add(-30 * time.Minute).Format(time.RFC3339), StopReason: "other", WakeTarget: "machine", LastProgressAt: "p1"},
		{At: now.Add(-10 * time.Minute).Format(time.RFC3339), StopReason: "other", WakeTarget: "machine", LastProgressAt: "p2"},
	}
	card, err := deriveWakeCard("X", "state.json", orchestrationWakeState{
		Phase: "stopped", StopReason: "judge_error", WakeTarget: "machine",
		ResumeCmd: "air-worker loop -product X", WakeHistory: machineHistory,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if card.WakeTarget != "session" || card.Action != "would_notify_session" {
		t.Fatalf("machine history did not escalate: %#v", card)
	}

	noProgress := []curatorWakeHistoryEvent{
		{At: now.Add(-20 * time.Minute).Format(time.RFC3339), StopReason: "not_proven", WakeTarget: "session", LastProgressAt: "same"},
		{At: now.Add(-10 * time.Minute).Format(time.RFC3339), StopReason: "not_proven", WakeTarget: "session", LastProgressAt: "same"},
	}
	card, err = deriveWakeCard("X", "state.json", orchestrationWakeState{
		Phase: "stopped", StopReason: "not_proven", WakeTarget: "session",
		LastProgressAt: "same", WakeHistory: noProgress,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if card.WakeTarget != "lpr" || card.Action != "card_only" {
		t.Fatalf("no-progress history did not escalate: %#v", card)
	}
}
