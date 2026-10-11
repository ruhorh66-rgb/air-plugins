package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCodeReviewRejectArguments(t *testing.T) {
	for _, args := range [][]string{{}, {"-tier", "gpt6-sol:low"}, {"-model", "gpt-6-sol"}, {"-sandbox", "read-only"}, {"extra"}, {"-source-sha", "bad"}} {
		if got := cmdCodeReview(args); got != 2 {
			t.Fatalf("%v: exit %d", args, got)
		}
	}
}

func TestNativeReviewerPinnedArgs(t *testing.T) {
	args := semanticCodexArgs("product", codeReviewer(), "schema")
	got := strings.Join(args, " ")
	want := []string{"exec", "--json", "--skip-git-repo-check", "-s", "read-only", "-C", "product", "--output-schema", "schema", "-m", "gpt-6-sol", "-c", "model_reasoning_effort=low", "-"}
	if strings.Join(args, "\x00") != strings.Join(want, "\x00") || strings.Contains(got, "workspace-write") {
		t.Fatal(args)
	}
}

func TestCodeReviewEvents(t *testing.T) {
	sha := strings.Repeat("a", 40)
	message := `{"verdict":"PASS","source_head":"` + sha + `","p0_open":0,"p1_open":0,"p2_open":0,"findings":[],"next_gate":"independent acceptance"}`
	stream := codeReviewTestStream(message)
	if _, _, err := parseCodeReview(stream, sha); err != nil {
		t.Fatal(err)
	}
	progress, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": "I am checking the diff."}})
	final, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": message}})
	realistic := "{\"type\":\"thread.started\",\"thread_id\":\"mock-session\"}\n" + string(progress) + "\n" + string(final) + "\n{\"type\":\"turn.completed\"}"
	if _, _, err := parseCodeReview(realistic, sha); err != nil {
		t.Fatalf("progress then final verdict rejected: %v", err)
	}
	if _, _, err := parseCodeReview(realistic+"\nwarning: stderr noise", sha); err != nil {
		t.Fatalf("non-JSON warning rejected completed job: %v", err)
	}
	secondFinal := "{\"type\":\"thread.started\",\"thread_id\":\"mock-session\"}\n" + string(final) + "\n" + finalMessageEvent(strings.Replace(message, "PASS", "NOT_PROVEN", 1)) + "\n{\"type\":\"turn.completed\"}"
	if _, _, err := parseCodeReview(secondFinal, sha); err == nil {
		t.Fatal("accepted multiple structured verdicts")
	}
	commentAfter := "{\"type\":\"thread.started\",\"thread_id\":\"mock-session\"}\n" + string(final) + "\n" + string(progress) + "\n{\"type\":\"turn.completed\"}"
	if _, _, err := parseCodeReview(commentAfter, sha); err == nil {
		t.Fatal("accepted commentary after final JSON")
	}
	for _, bad := range []string{strings.Replace(stream, "turn.completed", "turn.failed", 1), stream + "\n" + `{"type":"error"}`, strings.Replace(stream, sha, strings.Repeat("b", 40), 1), strings.Replace(stream, `\"p1_open\":0`, `\"p1_open\":1`, 1), strings.Replace(stream, "turn.completed", "turn.started", 1)} {
		if _, _, err := parseCodeReview(bad, sha); err == nil {
			t.Fatal("accepted invalid stream", bad)
		}
	}
}

func finalMessageEvent(message string) string {
	b, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": message}})
	return string(b)
}

func codeReviewTestStream(message string) string {
	b, _ := json.Marshal(map[string]any{"type": "item.completed", "item": map[string]string{"type": "agent_message", "text": message}})
	return "{\"type\":\"thread.started\",\"thread_id\":\"mock-session\"}\n" + string(b) + "\n{\"type\":\"turn.completed\"}"
}

func codeReviewFixture(t *testing.T) (string, []string, string) {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	cfg, _ := json.Marshal(executorTestConfig())
	for name, content := range map[string][]byte{"run-config.json": cfg, "PLAN.md": []byte("unchanged plan"), "TASK.md": []byte("review source"), ".gitignore": []byte(".woody/\n")} {
		if err := os.WriteFile(filepath.Join(root, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "fixture"}} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git: %s: %v", out, err)
		}
	}
	sha, err := codeReviewGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	id := sessionIdentity{Principal: "codex", SessionKey: "review-test"}
	if err := writeStateJSON(sessionModePath(state, id), sessionModeState{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionProductPath(state, id), sessionProductState{Path: root}); err != nil {
		t.Fatal(err)
	}
	oldRemote, oldResolve, oldRun, oldPonytail := codeReviewRemote, codeReviewResolve, codeReviewRun, loadPonytailSkill
	t.Cleanup(func() {
		codeReviewRemote = oldRemote
		codeReviewResolve = oldResolve
		codeReviewRun = oldRun
		loadPonytailSkill = oldPonytail
	})
	codeReviewRemote = func(string) (string, error) { return sha, nil }
	codeReviewResolve = func(tool string) (string, error) {
		if tool != "codex" {
			t.Fatal(tool)
		}
		return "mock-codex", nil
	}
	loadPonytailSkill = func() (ponytailSkill, error) {
		return ponytailSkill{Version: "5.1.0", Path: "fixture/SKILL.md", Body: "vendor"}, nil
	}
	codeReviewRun = func(context.Context, sessionScope, string, string, *exec.Cmd, jobReceiptMeta) ([]byte, error) {
		t.Fatal("unexpected model launch")
		return nil, nil
	}
	return root, []string{"-product", root, "-source-sha", sha, "-task-file", "TASK.md", "-principal", id.Principal, "-session-key", id.SessionKey, "-state-dir", state, "-json"}, sha
}

func TestCodeReviewPreflightGuards(t *testing.T) {
	for _, name := range []string{"valid", "identity", "product", "dirty", "stale", "head", "outside", "large", "symlink", "junction", "policy", "replay", "claim"} {
		t.Run(name, func(t *testing.T) {
			root, args, sha := codeReviewFixture(t)
			want := 2
			switch name {
			case "valid":
				want = 0
			case "identity":
				args[9] = "wrong"
			case "product":
				if err := writeStateJSON(sessionProductPath(args[11], sessionIdentity{Principal: "codex", SessionKey: "review-test"}), sessionProductState{Path: t.TempDir()}); err != nil {
					t.Fatal(err)
				}
			case "dirty":
				if err := os.WriteFile(filepath.Join(root, "untracked"), []byte("dirty"), 0600); err != nil {
					t.Fatal(err)
				}
			case "stale":
				codeReviewRemote = func(string) (string, error) { return strings.Repeat("b", 40), nil }
			case "head":
				args[3] = strings.Repeat("b", 40)
			case "outside":
				args[5] = filepath.Join(t.TempDir(), "task")
			case "large":
				if err := os.WriteFile(filepath.Join(root, "TASK.md"), make([]byte, codeReviewMaxTask+1), 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				link := filepath.Join(root, "link")
				if err := os.Symlink(root, link); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				args[5] = filepath.Join(link, "TASK.md")
			case "junction":
				if runtime.GOOS != "windows" {
					t.Skip("Windows junction")
				}
				link := filepath.Join(root, "junction")
				// Environment values keep paths out of PowerShell source text.
				cmd := exec.Command("powershell", "-NoProfile", "-Command", `New-Item -ItemType Junction -Path $env:AIR_REVIEW_TEST_LINK -Target $env:AIR_REVIEW_TEST_TARGET -ErrorAction Stop | Out-Null`)
				cmd.Env = append(os.Environ(), "AIR_REVIEW_TEST_LINK="+link, "AIR_REVIEW_TEST_TARGET="+root)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("junction: %s: %v", out, err)
				}
				t.Cleanup(func() { os.Remove(link) })
				args[5] = filepath.Join(link, "TASK.md")
			case "policy":
				cfg := executorTestConfig()
				cfg.Runners["gpt6-sol"] = runnerSpec{Kind: "codex", Model: "wrong"}
				if err := writeStateJSON(filepath.Join(root, "run-config.json"), cfg); err != nil {
					t.Fatal(err)
				}
			case "replay", "claim":
				scope := newScope(root, sessionIdentity{Principal: "codex", SessionKey: "review-test"})
				receipt, _ := jobReceiptPaths(scope, "code-"+sha+"-"+codeReviewDigest([]byte("review source")), codeReviewOperation)
				if err := os.MkdirAll(filepath.Dir(receipt), 0755); err != nil {
					t.Fatal(err)
				}
				if name == "claim" {
					receipt += ".claim"
				}
				if err := os.WriteFile(receipt, []byte("RUNNING"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if got := cmdCodeReview(append(args, "-preflight")); got != want {
				t.Fatalf("got %d want %d", got, want)
			}
		})
	}
}

func TestCodeReviewMockJob(t *testing.T) {
	for _, mode := range []string{"pass", "failed", "error", "stale", "swap", "dirty", "running", "changes", "not-proven"} {
		t.Run(mode, func(t *testing.T) {
			root, args, sha := codeReviewFixture(t)
			calls := 0
			codeReviewRun = func(_ context.Context, scope sessionScope, step, operation string, cmd *exec.Cmd, meta jobReceiptMeta) ([]byte, error) {
				calls++
				if meta.Model != "gpt-6-sol" || meta.Effort != "low" || meta.Sandbox != "read-only" || strings.Contains(strings.Join(cmd.Args, " "), "workspace-write") {
					t.Fatal(meta, cmd.Args)
				}
				receipt, _ := jobReceiptPaths(scope, step, operation)
				wantArgs := append([]string{"mock-codex"}, semanticCodexArgs(root, codeReviewer(), receipt+".schema.json")...)
				if strings.Join(cmd.Args, "\x00") != strings.Join(wantArgs, "\x00") {
					t.Fatalf("unexpected judge argv: %#v", cmd.Args)
				}
				status := jobStatusDone
				if mode == "running" {
					status = jobStatusRunning
				}
				if err := writeJobReceipt(receipt, &jobReceipt{Status: status, ProcessStarted: true}); err != nil {
					t.Fatal(err)
				}
				v := codeReviewVerdict{Verdict: "PASS", SourceHead: sha, Findings: []codeReviewFinding{}, NextGate: "LPR acceptance"}
				if mode == "changes" {
					v.Verdict = "CHANGES_REQUIRED"
					v.P2 = 1
					v.Findings = []codeReviewFinding{{Severity: "P2", Location: "a:1", Issue: "issue", MinimalFix: "fix"}}
				}
				if mode == "not-proven" {
					v.Verdict = "NOT_PROVEN"
				}
				b, _ := json.Marshal(v)
				stream := codeReviewTestStream(string(b))
				if mode == "error" {
					stream = strings.Replace(stream, "turn.completed", "error", 1)
				}
				if mode == "stale" {
					codeReviewRemote = func(string) (string, error) { return strings.Repeat("b", 40), nil }
				}
				if mode == "swap" {
					path := filepath.Join(root, "TASK.md")
					if err := os.Rename(path, path+".old"); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("review source"), 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Remove(path + ".old"); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "dirty" {
					if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("changed"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "failed" {
					return []byte(stream), fmt.Errorf("exit 1")
				}
				return []byte(stream), nil
			}
			want := 2
			if mode == "pass" {
				want = 0
			}
			if mode == "changes" {
				want = 1
			}
			if got := cmdCodeReview(args); got != want {
				t.Fatalf("got %d want %d", got, want)
			}
			if got := cmdCodeReview(args); got != 2 || calls != 1 {
				t.Fatalf("replay exit=%d calls=%d", got, calls)
			}
			scope := newScope(root, sessionIdentity{Principal: "codex", SessionKey: "review-test"})
			path, _ := jobReceiptPaths(scope, "code-"+sha+"-"+codeReviewDigest([]byte("review source")), codeReviewOperation)
			var doc codeReviewReceipt
			if err := readJSON(path+".review.json", &doc); err != nil {
				t.Fatal(err)
			}
			if doc.OutputSHA == "" || doc.TaskDigest == "" || doc.PlanSHA == "" || doc.SessionID != "mock-session" {
				t.Fatalf("missing evidence: %+v", doc)
			}
			if want == 2 && doc.Verdict != "NOT_PROVEN" {
				t.Fatal(doc)
			}
		})
	}
}

func TestCodeReviewStrictJSON(t *testing.T) {
	sha := strings.Repeat("a", 40)
	valid := `{"verdict":"PASS","source_head":"` + sha + `","p0_open":0,"p1_open":0,"p2_open":0,"findings":[],"next_gate":"gate"}`
	for _, message := range []string{
		strings.Replace(valid, `"p0_open":0,`, "", 1),
		strings.Replace(valid, `"p0_open":0`, `"p0_open":null`, 1),
		strings.Replace(valid, `"verdict":"PASS"`, `"verdict":"NOT_PROVEN","verdict":"PASS"`, 1),
		strings.Replace(valid, `"p0_open":0`, `"p0_open":0,"extra":1`, 1),
		valid + valid,
		strings.Replace(valid, `"findings":[]`, `"findings":null`, 1),
		strings.Replace(valid, `"p2_open":0,"findings":[]`, `"p2_open":1,"findings":[{"severity":"P2","location":"a","issue":"x","minimal_fix":"y"}]`, 1),
	} {
		if _, _, err := parseCodeReview(codeReviewTestStream(message), sha); err == nil {
			t.Fatalf("accepted %s", message)
		}
	}
}

func TestCodeReviewTaskPath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "task")
	if err := os.WriteFile(path, []byte("task"), 0600); err != nil {
		t.Fatal(err)
	}
	data, _, err := readCodeReviewTask(root, path)
	if err != nil || string(data) != "task" {
		t.Fatal(err)
	}
	if _, _, err := readCodeReviewTask(root, root); err == nil {
		t.Fatal("accepted directory")
	}
	if err := os.WriteFile(path, make([]byte, codeReviewMaxTask+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readCodeReviewTask(root, path); err == nil {
		t.Fatal("accepted oversized task")
	}
}

func TestCodeReviewWholeRepository(t *testing.T) {
	root, _, sha := codeReviewFixture(t)
	product := filepath.Join(root, ".woody", "nested-product")
	if err := os.MkdirAll(product, 0755); err != nil {
		t.Fatal(err)
	}
	if err := codeReviewSource(product, sha); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside-product"), []byte("dirty"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := codeReviewSource(product, sha); err == nil {
		t.Fatal("accepted dirt outside product")
	}
}

func TestCodeReviewUnknownRemote(t *testing.T) {
	root, _, _ := codeReviewFixture(t)
	bare := filepath.Join(t.TempDir(), "origin.git")
	if out, err := exec.Command("git", "init", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("bare: %s: %v", out, err)
	}
	for _, args := range [][]string{{"config", "remote.origin.url", "https://github.com/test/repo.git"}, {"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"}} {
		if _, err := codeReviewGit(root, args...); err != nil {
			t.Fatal(err)
		}
	}
	oldLookup := codeReviewLookupRemote
	oldURL, oldRemotes := codeReviewRemoteURL, codeReviewRemotes
	t.Cleanup(func() {
		codeReviewLookupRemote = oldLookup
		codeReviewRemoteURL = oldURL
		codeReviewRemotes = oldRemotes
	})
	codeReviewRemotes = func(root string) ([]string, error) {
		remotes, err := codeReviewGit(root, "remote")
		if err != nil || remotes == "" {
			return nil, err
		}
		return strings.Split(remotes, "\n"), nil
	}
	codeReviewLookupRemote = func(_, remote, ref string) (string, error) {
		if remote != "github" {
			return "", fmt.Errorf("unexpected remote %q", remote)
		}
		return codeReviewGit(bare, "ls-remote", "--exit-code", "--refs", bare, ref)
	}
	codeReviewRemoteURL = func(_, remote string) (string, error) {
		if remote == "github" {
			return "https://github.com/test/repo.git", nil
		}
		return bare, nil
	}
	if _, err := codeReviewGit(root, "remote", "add", "github", "https://github.com/test/repo.git"); err != nil {
		t.Fatal(err)
	}
	// The branch is deliberately created without tracking metadata, as in production worktrees.
	if out, err := exec.Command("git", "-C", root, "checkout", "-b", "review-no-track", "--no-track", "HEAD").CombinedOutput(); err != nil {
		t.Fatalf("no-track: %s: %v", out, err)
	}
	if out, err := exec.Command("git", "-C", root, "push", bare, "HEAD:refs/heads/review-no-track").CombinedOutput(); err != nil {
		t.Fatalf("push fixture: %s: %v", out, err)
	}
	sha, err := codeReviewGit(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := codeReviewRemoteHead(root); err != nil || got != sha {
		t.Fatalf("no-track published ref got %q, %v want %q", got, err, sha)
	}
	if _, err := codeReviewGit(root, "config", "branch.review-no-track.merge", "refs/heads/other"); err != nil {
		t.Fatal(err)
	}
	if _, err := codeReviewGit(root, "config", "branch.review-no-track.remote", "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := codeReviewRemoteHead(root); err == nil {
		t.Fatal("accepted mismatched tracking branch")
	}
	for _, name := range []string{"extra"} {
		if _, err := codeReviewGit(root, "remote", "add", name, "https://github.com/test/other.git"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := codeReviewRemoteHead(root); err == nil {
		t.Fatal("accepted multiple GitHub remotes")
	}
	if _, err := codeReviewGit(root, "remote", "remove", "extra"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"branch.review-no-track.remote", "branch.review-no-track.merge"} {
		if _, err := codeReviewGit(root, "config", "--unset-all", key); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := codeReviewRemoteHead(root); err != nil || got != sha {
		t.Fatalf("single remote published ref got %q, %v want %q", got, err, sha)
	}
	if _, err := codeReviewGit(root, "remote", "remove", "github"); err != nil {
		t.Fatal(err)
	}
	if _, err := codeReviewRemoteHead(root); err == nil {
		t.Fatal("accepted zero GitHub remotes")
	}
	if _, err := codeReviewGit(root, "remote", "add", "github", "https://github.com/test/repo.git"); err != nil {
		t.Fatal(err)
	}
	codeReviewRemoteURL = oldURL
	if _, err := codeReviewGit(root, "remote", "set-url", "github", "https://github.com.evil.test/test/repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := codeReviewRemoteHead(root); err == nil {
		t.Fatal("accepted foreign domain")
	}
	if _, err := codeReviewGit(root, "remote", "set-url", "github", "https://github.com/test/repo.git"); err != nil {
		t.Fatal(err)
	}
	if _, err := codeReviewGit(root, "config", "branch.review-no-track.remote", "origin"); err != nil {
		t.Fatal(err)
	}
	if _, err := codeReviewGit(root, "config", "branch.review-no-track.merge", "refs/heads/review-no-track"); err != nil {
		t.Fatal(err)
	}
	codeReviewRemoteURL = func(_, remote string) (string, error) { return bare, nil }
	if _, err := codeReviewRemoteHead(root); err == nil {
		t.Fatal("accepted local configured tracking remote")
	}
	if _, err := codeReviewGit(root, "checkout", "--detach", "HEAD"); err != nil {
		t.Fatal(err)
	}
	if _, err := codeReviewRemoteHead(root); err == nil {
		t.Fatal("accepted detached HEAD")
	}
}

func TestCodeReviewPrelaunchSwap(t *testing.T) {
	root, args, _ := codeReviewFixture(t)
	codeReviewResolve = func(string) (string, error) {
		path := filepath.Join(root, "TASK.md")
		if err := os.Rename(path, path+".old"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("review source"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path + ".old"); err != nil {
			t.Fatal(err)
		}
		return "mock-codex", nil
	}
	if got := cmdCodeReview(append(args, "-preflight")); got != 2 {
		t.Fatal(got)
	}
}
