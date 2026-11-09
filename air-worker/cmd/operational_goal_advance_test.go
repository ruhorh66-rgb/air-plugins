package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
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

func TestOperationalGoalMutationRejectsInvalidMutex(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("native mutex handles are Windows-specific")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := operationalGoalPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("unchanged selector\n")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ProgramData", t.TempDir())
	state := sessionStateDir()
	id := sessionIdentity{Principal: "chatgpt", SessionKey: "goal-lock-fixture"}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	oldAdvance, oldActivate := operationalGoalAdvanceAcquireLock, operationalGoalActivateAcquireLock
	t.Cleanup(func() {
		operationalGoalAdvanceAcquireLock, operationalGoalActivateAcquireLock = oldAdvance, oldActivate
	})
	for _, invalid := range []struct {
		name string
		lock *osLock
	}{{"nil", nil}, {"zero", &osLock{}}} {
		for _, target := range []string{"activate", "goal", "writer"} {
			t.Run(invalid.name+"/"+target, func(t *testing.T) {
				calls := 0
				var held *osLock
				acquire := func(name string) (*osLock, bool) {
					calls++
					if target == "writer" && calls == 1 {
						var ok bool
						held, ok = acquireLock(name)
						if !ok || !validAutoSelfDeliveryLock(held) {
							t.Fatal("could not acquire real goal mutex")
						}
						return held, true
					}
					return invalid.lock, true
				}
				operationalGoalAdvanceAcquireLock = acquire
				operationalGoalActivateAcquireLock = acquire
				var output string
				var code int
				want := "operational goal mutex is unavailable\n"
				if target == "activate" {
					output, code = captureStderr(t, func() int { return cmdOperationalGoal([]string{"activate", "-product", root}) })
				} else {
					want = "operational-goal advance: goal mutex unavailable\n"
					if target == "writer" {
						want = "operational-goal advance: N139 writer mutex unavailable\n"
					}
					output, code = captureStderr(t, func() int {
						return cmdOperationalGoalAdvance([]string{"-product", root, "-principal", id.Principal, "-session-key", id.SessionKey})
					})
				}
				if code != 2 || strings.ReplaceAll(output, "\r\n", "\n") != want {
					t.Fatalf("code=%d stderr=%q, want 2 and %q", code, output, want)
				}
				wantCalls := 1
				if target == "writer" {
					wantCalls = 2
				}
				if calls != wantCalls {
					t.Fatalf("lock calls=%d, want %d", calls, wantCalls)
				}
				if held != nil && validAutoSelfDeliveryLock(held) {
					t.Fatal("real goal mutex was not released")
				}
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, original) {
					t.Fatalf("selector changed: %q, %v", got, err)
				}
			})
		}
	}
}
