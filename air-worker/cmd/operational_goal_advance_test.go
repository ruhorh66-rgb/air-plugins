package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOperationalGoalAdvanceRecordedN124Cancellation(t *testing.T) {
	const oldHead = "8c96ffd01a9b036f39b85141876804b258b1c842"
	const newHead = "eccce10cc3e665755afc2995a0832fd58866e160"
	const oldDigest = "811a37794842d8ed575f478d0a84665579daee5f5afe94e5e4ad6e9e7627ab9a"
	const newDigest = "6bbe0fdb1c21fb5449f5b5f03a7d9b286461954e8fac2a1e449b7010cc38a22c"
	const id = "N-139_n136-native-operational-goal-selector-for-real-a"
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	root := filepath.Join(repo, "air-worker")
	if out, err := exec.Command("git", "clone", "--shared", "--no-checkout", source, repo).CombinedOutput(); err != nil {
		t.Fatalf("clone: %s: %v", out, err)
	}
	if out, err := exec.Command("git", "-C", repo, "-c", "core.autocrlf=false", "checkout", "--detach", newHead).CombinedOutput(); err != nil {
		t.Fatalf("checkout: %s: %v", out, err)
	}
	historical, err := exec.Command("git", "-C", root, "show", oldHead+":air-worker/plan/release-slices.json").Output()
	if err != nil || fmt.Sprintf("%x", sha256.Sum256(bytes.ReplaceAll(historical, []byte("\n"), []byte("\r\n")))) != oldDigest {
		t.Fatalf("historical release digest mismatch: %v", err)
	}
	next, err := operationalGoalInputs(root, id, newHead)
	if err != nil || next.ReleaseSHA256 != newDigest {
		t.Fatalf("current goal or digest mismatch: %+v %v", next, err)
	}
	prior := next
	prior.SourceHead, prior.ReleaseSHA256 = oldHead, oldDigest
	if got, err := validateOperationalGoalAdvance(root, prior, id, oldHead, newHead); err != nil || got != next {
		t.Fatalf("recorded cancellation rejected: %+v %v", got, err)
	}
	t.Run("CRLF plan", func(t *testing.T) {
		path := filepath.Join(root, "plan", "release-slices.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Error(err)
			}
		})
		crlf := bytes.ReplaceAll(bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n")), []byte("\n"), []byte("\r\n"))
		if got := fmt.Sprintf("%x", sha256.Sum256(crlf)); got != "600a062f120d63f986d92ea962a8c629a3b99bb502dd028f112b70637dae0fd6" {
			t.Fatalf("CRLF digest mismatch: %s", got)
		}
		if err := os.WriteFile(path, crlf, 0600); err != nil {
			t.Fatal(err)
		}
		crlfNext, err := operationalGoalInputs(root, id, newHead)
		if err != nil || crlfNext.ReleaseSHA256 != "600a062f120d63f986d92ea962a8c629a3b99bb502dd028f112b70637dae0fd6" {
			t.Fatalf("CRLF goal or digest mismatch: %+v %v", crlfNext, err)
		}
		if got, err := validateOperationalGoalAdvance(root, prior, id, oldHead, newHead); err != nil || got != crlfNext {
			t.Fatalf("CRLF cancellation rejected: %+v %v", got, err)
		}
		wrongPrior := prior
		wrongPrior.ReleaseSHA256 = strings.Repeat("a", 64)
		if _, err := validateOperationalGoalAdvance(root, wrongPrior, id, oldHead, newHead); err == nil {
			t.Fatal("CRLF plan accepted wrong old digest")
		}
		replaceN124File(t, root, "plan/release-slices.json", "0.11.9", "0.11.8")
		if _, err := validateOperationalGoalAdvance(root, prior, id, oldHead, newHead); err == nil {
			t.Fatal("altered CRLF plan accepted")
		}
	})
	for _, tc := range []struct {
		name, from, to string
		prior          operationalGoal
	}{
		{"wrong old digest", oldHead, newHead, func() operationalGoal { p := prior; p.ReleaseSHA256 = strings.Repeat("a", 64); return p }()},
		{"changed criteria", oldHead, newHead, func() operationalGoal { p := prior; p.DoneWhen = "changed"; return p }()},
		{"unrelated source", strings.Repeat("f", 40), newHead, func() operationalGoal { p := prior; p.SourceHead = strings.Repeat("f", 40); return p }()},
		{"stale head", oldHead, oldHead, prior},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := validateOperationalGoalAdvance(root, tc.prior, id, tc.from, tc.to); err == nil {
				t.Fatal("unverified transition accepted")
			}
		})
	}
	for _, tc := range []struct{ name, path, before, after string }{
		{"wrong new digest", "plan/release-slices.json", "0.11.9", "0.11.8"},
		{"altered receipt", n124ReceiptPath, "CANCELLED_BY_LPR", "NOT_CANCELLED"},
		{"altered node", n124NodePath, `status: "closed"`, `status: "open"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(root, filepath.FromSlash(tc.path))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.WriteFile(path, raw, 0600); err != nil {
					t.Error(err)
				}
			})
			replaceN124File(t, root, tc.path, tc.before, tc.after)
			if _, err := validateOperationalGoalAdvance(root, prior, id, oldHead, newHead); err == nil {
				t.Fatal("altered evidence accepted")
			}
		})
	}
	t.Run("absent receipt", func(t *testing.T) {
		removeN124File(t, root, n124ReceiptPath)
		if _, err := validateOperationalGoalAdvance(root, prior, id, oldHead, newHead); err == nil {
			t.Fatal("missing evidence accepted")
		}
	})
}

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

func TestOperationalGoalAdvanceRechecksSource(t *testing.T) {
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(t.TempDir(), "repo")
	if out, err := exec.Command("git", "clone", "--shared", "--no-checkout", source, repo).CombinedOutput(); err != nil {
		t.Fatalf("clone: %s: %v", out, err)
	}
	if out, err := exec.Command("git", "-C", repo, "-c", "core.autocrlf=false", "checkout", "--detach", "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("checkout: %s: %v", out, err)
	}
	root := filepath.Join(repo, "air-worker")
	head, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	previous, err := planRephaseGit(root, "rev-parse", "HEAD^")
	if err != nil {
		t.Fatal(err)
	}
	const node = "N-139_n136-native-operational-goal-selector-for-real-a"
	next, err := operationalGoalInputs(root, node, head)
	if err != nil {
		t.Fatal(err)
	}
	prior := next
	prior.SourceHead = previous
	oldRaw, err := json.MarshalIndent(prior, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	oldRaw = append(oldRaw, '\n')
	path := operationalGoalPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ProgramData", t.TempDir())
	state := sessionStateDir()
	id := sessionIdentity{Principal: "chatgpt", SessionKey: "goal-recheck-fixture"}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root, Principal: id.Principal, SessionKey: id.SessionKey}); err != nil {
		t.Fatal(err)
	}
	originalGitState := operationalGoalAdvanceGitState
	t.Cleanup(func() { operationalGoalAdvanceGitState = originalGitState })
	foreign := []byte("foreign selector\n")
	for _, tc := range []struct {
		name    string
		dirtyAt int
		foreign bool
	}{
		{"dirty before write", 2, false},
		{"dirty after write", 3, false},
		{"foreign edit after write", 3, true},
		{"clean", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, oldRaw, 0o600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			operationalGoalAdvanceGitState = func(gotRoot string) (string, error) {
				calls++
				if gotRoot != root {
					t.Errorf("source check root=%q, want %q", gotRoot, root)
				}
				if calls == tc.dirtyAt {
					got, err := os.ReadFile(path)
					if err != nil || (calls == 2 && !bytes.Equal(got, oldRaw)) || (calls == 3 && bytes.Equal(got, oldRaw)) {
						t.Errorf("selector at check %d: %q, %v", calls, got, err)
					}
					if tc.foreign {
						if err := os.WriteFile(path, foreign, 0o600); err != nil {
							t.Fatal(err)
						}
					}
					return "", fmt.Errorf("fixture source is dirty")
				}
				return head, nil
			}
			var stderr string
			stdout, code := captureStdout(t, func() int {
				var code int
				stderr, code = captureStderr(t, func() int {
					return cmdOperationalGoalAdvance([]string{"-product", root, "-node", node, "-from-head", previous, "-source-head", head, "-principal", id.Principal, "-session-key", id.SessionKey})
				})
				return code
			})
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.dirtyAt != 0 {
				want := oldRaw
				if tc.foreign {
					want = foreign
				}
				if code == 0 || stdout != "" || stderr == "" || calls != tc.dirtyAt || !bytes.Equal(got, want) {
					t.Fatalf("code=%d stdout=%q stderr=%q checks=%d selector=%q", code, stdout, stderr, calls, got)
				}
			} else {
				verified, err := decodeOperationalGoalPinned(got)
				if code != 0 || stderr != "" || !strings.Contains(stdout, `"schema":"air-worker.operational-goal.advance/v1"`) || calls != 3 || err != nil || verified != next {
					t.Fatalf("code=%d stdout=%q stderr=%q checks=%d selector=%q decode=%v", code, stdout, stderr, calls, got, err)
				}
			}
		})
	}
}
