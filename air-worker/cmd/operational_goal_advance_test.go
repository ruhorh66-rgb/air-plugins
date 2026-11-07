package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func operationalAdvanceRealFixture(t *testing.T) (string, operationalGoal, string, string, string) {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	head, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := planRephaseGit(root, "rev-parse", "HEAD^")
	if err != nil {
		t.Fatal(err)
	}
	const id = "N-139_n136-native-operational-goal-selector-for-real-a"
	current, err := operationalGoalInputs(root, id, head)
	if err != nil {
		t.Fatal(err)
	}
	prior := current
	prior.SourceHead = previous
	return root, prior, id, previous, head
}

func TestOperationalGoalAdvanceExistingN139OnlyByGitAncestry(t *testing.T) {
	root, prior, id, previous, head := operationalAdvanceRealFixture(t)
	next, err := validateOperationalGoalAdvance(root, prior, id, previous, head)
	if err != nil {
		t.Fatal(err)
	}
	if next.NodeID != id || next.SourceHead != head || next.ReleaseSHA256 != prior.ReleaseSHA256 {
		t.Fatalf("advance changed identity or history: %+v", next)
	}
}

func TestOperationalGoalAdvanceRejectsStaleForeignOrUnrelatedHistory(t *testing.T) {
	root, prior, id, previous, head := operationalAdvanceRealFixture(t)
	for _, tc := range []struct {
		name  string
		from  string
		to    string
		prior operationalGoal
	}{
		{"changed-owner", previous, head, func() operationalGoal { x := prior; x.Owner = "foreign"; return x }()},
		{"changed-release-hash", previous, head, func() operationalGoal { x := prior; x.ReleaseSHA256 = "wrong"; return x }()},
		{"changed-node-criteria", previous, head, func() operationalGoal { x := prior; x.DoneWhen = "relaxed"; return x }()},
		{"stale-from-head", head, head, prior},
		{"unrelated-history", strings.Repeat("f", 40), head, func() operationalGoal { x := prior; x.SourceHead = strings.Repeat("f", 40); return x }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateOperationalGoalAdvance(root, tc.prior, id, tc.from, tc.to); err == nil {
				t.Fatal("unverified selector advance accepted")
			}
		})
	}
}

func TestOperationalGoalAdvanceDecodeRejectsDuplicateAndUnknownFields(t *testing.T) {
	_, goal, _, _, _ := operationalAdvanceRealFixture(t)
	raw, err := json.Marshal(goal)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := decodeOperationalGoalPinned(raw)
	if err != nil || valid != goal {
		t.Fatalf("valid prior selector decode failed: %v %+v", err, valid)
	}
	duplicate := bytes.Replace(raw, []byte("{"), []byte(`{"schema":"duplicate",`), 1)
	if _, err := decodeOperationalGoalPinned(duplicate); err == nil {
		t.Fatal("duplicate JSON key not refused")
	}
	unknown := bytes.Replace(raw, []byte("{"), []byte(`{"foreign_key":1,`), 1)
	if _, err := decodeOperationalGoalPinned(unknown); err == nil {
		t.Fatal("unknown prior selector field not refused")
	}
	if _, err := decodeOperationalGoalPinned(append(raw, []byte("{}")...)); err == nil {
		t.Fatal("trailing JSON not refused")
	}
}

func TestOperationalGoalAdvanceAtomicStateReplacement(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "operational-goal.json")
	old := []byte("{\"schema\":\"prior\"}\n")
	next := []byte("{\"schema\":\"updated\"}\n")
	if err := os.WriteFile(path, old, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, next); err != nil {
		t.Fatalf("native goal transition cannot atomically replace its prior state: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, next) {
		t.Fatalf("native atomic state readback failed: %v bytes=%q", err, got)
	}
}
