package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestN139BoundReviewRejectsChangedTaskBeforeDispatch(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "air-worker")
	for _, dir := range []string{"cmd", "plan", "goal", "docs/tasks"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(dir)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	task := codingMicrostepTask(t)
	const taskPath = "docs/tasks/N139.json"
	const bindingPath = "docs/tasks/N139_ACTIVE_BINDING.json"
	binding, err := json.Marshal(map[string]string{"node_id": n139ID, "task_path": taskPath, "task_sha256": sha256Hex([]byte(task))})
	if err != nil {
		t.Fatal(err)
	}
	release, err := os.ReadFile(filepath.Join("..", "plan", "release-slices.json"))
	if err != nil {
		t.Fatal(err)
	}
	node := fmt.Sprintf("---\nid: %q\ntitle: \"N139\"\nparent: %q\ntrigger: \"test\"\nowner: \"test\"\ndone_when: \"test\"\nstatus: \"open\"\nreturn_to: \"test\"\ncreated_at: \"2026-11-07T03:07:15Z\"\nupdated_at: \"2026-11-07T03:07:15Z\"\nreceipts:\n---\n", n139ID, "N-136_prove-autonomous-plan-coding-factual-anthropic-c")
	files := map[string][]byte{
		".gitignore":                          []byte("air-worker/.woody/\nair-worker/goal/operational-goal.json\n"),
		"air-worker/cmd/go.mod":               []byte("module airos/air-worker\n\ngo 1.26\n"),
		"air-worker/PLAN.md":                  []byte("[[" + n139ID + "]]\n"),
		"air-worker/plan/" + n139ID + ".md":   []byte(node),
		"air-worker/plan/release-slices.json": release,
		"air-worker/" + taskPath:              []byte(task),
		"air-worker/" + bindingPath:           binding,
	}
	for path, data := range files {
		if err := os.WriteFile(filepath.Join(repo, filepath.FromSlash(path)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	n124Git(t, repo, "init")
	n124Git(t, repo, "config", "user.name", "Test")
	n124Git(t, repo, "config", "user.email", "test@example.invalid")
	n124Git(t, repo, "add", ".")
	n124Git(t, repo, "commit", "-m", "bound fixture")
	goal, err := operationalGoalInputs(root, n139ID, n124Git(t, repo, "rev-parse", "HEAD"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(goal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(operationalGoalPath(root), raw, 0600); err != nil {
		t.Fatal(err)
	}
	c := &loopCtx{Root: root, Cfg: runConfig{ModelPolicy: modelPolicySpec{Judges: map[string]modelJudgeLane{
		"default": {Model: "claude-sonnet-5-5", Efforts: []string{"low"}},
	}}}}
	c.Cfg.Orchestration.RequireAnthropicReviewer = true
	selected, err := selectN139CodingTask(root, goal)
	if err != nil || selected != task {
		t.Fatalf("initial selection: %v", err)
	}
	step := "source-review-" + goal.NodeID + "-" + goal.SourceHead[:12]
	receipt, _ := jobReceiptPaths(c.scope(), step, "semantic-reviewer")
	if err := os.MkdirAll(filepath.Dir(receipt), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(receipt, []byte("collision"), 0600); err != nil {
		t.Fatal(err)
	}
	const preflight = "source review receipt or output already exists or is unreadable"
	if err := reviewN139BoundSource(c, goal, task); err == nil || err.Error() != preflight {
		t.Fatalf("control did not reach reviewer preflight: %v", err)
	}
	if err := reviewN139BoundSource(c, goal, task+" "); err == nil || err.Error() != "original coding task changed or invalid" {
		t.Fatalf("changed bound task argument: %v", err)
	}
	for _, path := range []string{taskPath, bindingPath} {
		t.Run(path, func(t *testing.T) {
			full := filepath.Join(root, filepath.FromSlash(path))
			original, err := os.ReadFile(full)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, append(append([]byte(nil), original...), ' '), 0600); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.WriteFile(full, original, 0600); err != nil {
					t.Error(err)
				}
			}()
			err = reviewN139BoundSource(c, goal, task)
			if err == nil || (!strings.Contains(err.Error(), "source is dirty") && err.Error() != "original coding task changed or invalid") {
				t.Fatalf("changed %s reached reviewer preflight or was accepted: %v", path, err)
			}
		})
	}
}
