package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOperationalGoalRejectsBadHeadAndScope(t *testing.T) {
	root := t.TempDir()
	if _, err := operationalGoalInputs(root, "N-139_valid", "bad"); err == nil || !strings.Contains(err.Error(), "source-head") {
		t.Fatalf("want SHA refusal, got %v", err)
	}
	if _, err := operationalGoalInputs(root, "N-139_valid", strings.Repeat("a", 40)); err == nil || !strings.Contains(err.Error(), "source root") {
		t.Fatalf("want scope refusal, got %v", err)
	}
}

func TestOperationalGoalVerifiesRealN139ReadOnly(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	head, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	goal, err := operationalGoalInputs(root, "N-139_n136-native-operational-goal-selector-for-real-a", head)
	if err != nil {
		t.Fatal(err)
	}
	if goal.NodeID == "" || goal.ReleaseSHA256 == "" {
		t.Fatalf("incomplete goal: %+v", goal)
	}
}

func TestOperationalGoalExclusiveCreateNeverOverwritesForeignState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operational-goal.json")
	foreign := []byte("{\"foreign\":true}\n")
	if err := os.WriteFile(path, foreign, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeNewOperationalGoal(path, []byte("{\"owned\":true}\n")); err == nil {
		t.Fatal("exclusive native selector must refuse replacing an existing file")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, foreign) {
		t.Fatalf("foreign state changed during attempted activation: %q", got)
	}
}

func TestOperationalGoalExclusiveCreateReadbackAndNoBlindRepeat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "operational-goal.json")
	want := []byte("{\"schema\":\"air-worker.operational-goal/v1\"}\n")
	if err := writeNewOperationalGoal(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("first exclusive create readback failed: err=%v data=%q", err, got)
	}
	if err := writeNewOperationalGoal(path, []byte("duplicate")); err == nil {
		t.Fatal("duplicate blind create must be refused")
	}
	got, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("duplicate attempt modified the active state: err=%v data=%q", err, got)
	}
}

func TestOperationalGoalDoesNotMutateHistoricalReleaseSlices(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	history := filepath.Join(root, "plan", "release-slices.json")
	before, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	head, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	_, err = operationalGoalInputs(root, "N-139_n136-native-operational-goal-selector-for-real-a", head)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(history)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("operational selection modified historical release slices: %v", err)
	}
}

func TestOperationalGoalPreviewRejectsMissingSelector(t *testing.T) {
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	head, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	goal, err := operationalGoalInputs(root, "N-139_n136-native-operational-goal-selector-for-real-a", head)
	if err != nil {
		t.Fatal(err)
	}
	if err := operationalGoalPreview(t.TempDir(), goal); err == nil || !strings.Contains(err.Error(), "selector") {
		t.Fatalf("absent selector must refuse preview: %v", err)
	}
}
