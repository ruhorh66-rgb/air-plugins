package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func seedCuratorProduct(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "run-config.json"), []byte("{\"plan\":\"PLAN.md\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := "**Ц1.** release safely\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | build core | `script` | K1: go test |\n" +
		"| 2 | LPR release gate | гейт | ЛПР |\n" +
		"| 3 | post-gate smoke | `script` | K2: smoke |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCuratorSnapshotFindsNearestMilestoneAndPendingLPR(t *testing.T) {
	root := seedCuratorProduct(t)
	seedLearnProposal(t, root, "LP-pending", "release-proof", "Attach machine evidence to release claims.")

	snapshot, err := buildCuratorSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.NextStep == nil || snapshot.NextStep.Num != "1" {
		t.Fatalf("next step=%#v", snapshot.NextStep)
	}
	if snapshot.NextGate == nil || snapshot.NextGate.Num != "2" || !snapshot.NextGate.Gate {
		t.Fatalf("next gate=%#v", snapshot.NextGate)
	}
	if snapshot.StepsToGate != 1 {
		t.Fatalf("steps to gate=%d want 1", snapshot.StepsToGate)
	}
	if snapshot.OpenSteps != 2 {
		t.Fatalf("open executable steps=%d want 2", snapshot.OpenSteps)
	}
	if len(snapshot.PendingLPR) != 1 || snapshot.PendingLPR[0].ID != "LP-pending" {
		t.Fatalf("pending=%#v", snapshot.PendingLPR)
	}
}

func TestCuratorSnapshotReportsLearningEffect(t *testing.T) {
	root := seedCuratorProduct(t)
	id := "LP-effect"
	seedLearnProposal(t, root, id, "scope", "Stay inside the assigned product.")
	grantLearnProposal(t, root, id)
	if code := cmdLearnApply([]string{"-product", root, "-id", id}); code != 0 {
		t.Fatalf("apply failed: %d", code)
	}
	proposals, err := readLearnProposals(learnPaths(root).Proposals)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := time.Parse(time.RFC3339Nano, proposals[0].AppliedAt)
	if err != nil {
		t.Fatal(err)
	}
	repeat := learnJournalRecord{
		Schema: learnSchemaVersion, ID: "LR-repeat",
		CreatedAt: applied.Add(time.Second).Format(time.RFC3339Nano),
		Class:     "scope", Observed: "repeated", Kind: "lesson", Source: "worker",
	}
	if err := appendLearnJSON(learnPaths(root).Journal, repeat); err != nil {
		t.Fatal(err)
	}

	snapshot, err := buildCuratorSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Learning) != 1 {
		t.Fatalf("learning=%#v", snapshot.Learning)
	}
	got := snapshot.Learning[0]
	if got.ProposalID != id || got.RepeatsAfter != 1 || got.Verdict != "не сработало" {
		t.Fatalf("effect=%#v", got)
	}
}

func TestCuratorSnapshotJSONContract(t *testing.T) {
	root := seedCuratorProduct(t)
	snapshot, err := buildCuratorSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["schema"] != curatorSchemaVersion {
		t.Fatalf("schema=%v", decoded["schema"])
	}
	if decoded["product"] != root {
		t.Fatalf("product=%v", decoded["product"])
	}
}

func TestCuratorSnapshotWithControlAggregatesPeerAssignmentDecisionAndWake(t *testing.T) {
	root := seedCuratorProduct(t)
	stateDir := t.TempDir()
	if code := cmdCuratorPeerRegister([]string{
		"-id", "astra", "-provider", "claude", "-model", "sonnet", "-max-active", "1", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("register code=%d", code)
	}
	if code := cmdCuratorAssign([]string{
		"-peer", "astra", "-profile", "p1", "-session", "s1", "-run", "r1",
		"-product", root, "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("assign code=%d", code)
	}
	if code := cmdCuratorDecisionRecord([]string{
		"-principal", "user", "-profile", "p1", "-session", "s1", "-run", "r1",
		"-kind", "lpr-note", "-subject", "release", "-decision", "hold", "-state-dir", stateDir,
	}); code != 0 {
		t.Fatalf("decision code=%d", code)
	}
	wakeDir := filepath.Join(root, ".woody")
	if err := os.MkdirAll(wakeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	wakeRaw, _ := json.Marshal(orchestrationWakeState{
		Phase: "gate", StopReason: "gate", WakeTarget: "lpr",
	})
	if err := os.WriteFile(filepath.Join(wakeDir, "orchestration.state.json"), wakeRaw, 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 9, 28, 4, 30, 0, 0, time.UTC)
	snapshot, err := buildCuratorSnapshotWithControl(root, stateDir, now)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.GeneratedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("generated_at=%q", snapshot.GeneratedAt)
	}
	if len(snapshot.Peers) != 1 || snapshot.Peers[0].ID != "astra" {
		t.Fatalf("peers=%#v", snapshot.Peers)
	}
	if len(snapshot.Assignments) != 1 || snapshot.Assignments[0].Product != root {
		t.Fatalf("assignments=%#v", snapshot.Assignments)
	}
	if snapshot.DecisionCount != 1 {
		t.Fatalf("decision_count=%d", snapshot.DecisionCount)
	}
	if snapshot.Wake == nil || snapshot.Wake.WakeTarget != "lpr" || snapshot.Wake.Action != "card_only" {
		t.Fatalf("wake=%#v", snapshot.Wake)
	}
}
