package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const operationalGoalSchema = "air-worker.operational-goal/v1"

var operationalSHA = regexp.MustCompile(`^[a-f0-9]{40}$`)

type operationalGoal struct {
	Schema        string `json:"schema"`
	SourceHead    string `json:"source_head"`
	NodeID        string `json:"node_id"`
	Owner         string `json:"owner"`
	Trigger       string `json:"trigger"`
	DoneWhen      string `json:"done_when"`
	ReleaseSHA256 string `json:"release_sha256"`
}

func operationalGoalPath(root string) string {
	return filepath.Join(root, "goal", "operational-goal.json")
}

func operationalGoalInputs(root, id, head string) (operationalGoal, error) {
	var empty operationalGoal
	if !operationalSHA.MatchString(head) {
		return empty, errors.New("-source-head must be an exact 40-character lowercase Git SHA")
	}
	module, err := os.ReadFile(filepath.Join(root, "cmd", "go.mod"))
	if err != nil || !strings.HasPrefix(strings.ReplaceAll(string(module), "\r\n", "\n"), "module airos/air-worker\n") {
		return empty, errors.New("product is not the AirWorker source root")
	}
	top, err := planRephaseGit(root, "rev-parse", "--show-toplevel")
	if err != nil || !sameLearningPath(top, filepath.Dir(root)) || filepath.Base(root) != "air-worker" {
		return empty, errors.New("product is not the Git worktree root")
	}
	actual, err := planRephaseGit(root, "rev-parse", "HEAD")
	if err != nil || actual != head {
		return empty, fmt.Errorf("source HEAD mismatch: expected %s, actual %s", head, actual)
	}
	if !planNodeIDRE.MatchString(id) {
		return empty, fmt.Errorf("full node ID required: %q", id)
	}
	path, err := planNodePathByID(root, id)
	if err != nil {
		return empty, err
	}
	releasePath := filepath.Join(root, "plan", "release-slices.json")
	controlled := []string{filepath.Join(root, "PLAN.md"), path, releasePath, filepath.Join(root, "goal")}
	if _, err := os.Lstat(operationalGoalPath(root)); err == nil {
		controlled = append(controlled, operationalGoalPath(root))
	} else if !os.IsNotExist(err) {
		return empty, err
	}
	if err := verifyPlanRephaseControlledPaths(root, controlled...); err != nil {
		return empty, err
	}
	node, err := readPlanNode(path)
	if err != nil {
		return empty, err
	}
	if node.ID != id || node.Parent != "N-136_prove-autonomous-plan-coding-factual-anthropic-c" || node.Status != "open" || strings.TrimSpace(node.Owner) == "" ||
		!strings.Contains(readPlanText(root), "[["+id+"]]") {
		return empty, errors.New("node must be an open N-136 child linked from PLAN.md with an owner")
	}
	state := inspectReleaseSlices(root)
	if !state.Found || len(state.Problems) != 0 || state.ActiveVersion != "0.11.9" || state.NextStep != "R0.11.9-05" {
		return empty, errors.New("historical 0.11.9 R05 release slices are missing or invalid")
	}
	release, err := os.ReadFile(releasePath)
	if err != nil {
		return empty, err
	}
	return operationalGoal{operationalGoalSchema, head, id, node.Owner, node.Trigger, node.DoneWhen, fmt.Sprintf("%x", sha256.Sum256(release))}, nil
}

func readPlanText(root string) string {
	raw, _ := os.ReadFile(filepath.Join(root, "PLAN.md"))
	return string(raw)
}

func readOperationalGoal(root string) (operationalGoal, error) {
	var saved operationalGoal
	path := operationalGoalPath(root)
	if err := verifyPlanRephaseControlledPaths(root, filepath.Join(root, "PLAN.md"), filepath.Join(root, "plan", "release-slices.json"), path); err != nil {
		return saved, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return saved, err
	}
	if len(raw) == 0 || len(raw) > 1<<20 {
		return saved, errors.New("operational goal size is invalid")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return saved, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&saved); err != nil {
		return saved, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return saved, errors.New("trailing operational goal JSON")
	}
	want, err := operationalGoalInputs(root, saved.NodeID, saved.SourceHead)
	if err != nil {
		return saved, err
	}
	if saved != want {
		return saved, errors.New("operational goal no longer matches node or historical release slices")
	}
	return saved, nil
}

// writeNewOperationalGoal creates one host-local selector without replacing a raced foreign file.
// A torn write remains invalid and fail-closed; only the native owner may reconcile it.
func writeNewOperationalGoal(path string, raw []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func cmdOperationalGoal(argv []string) int {
	if len(argv) == 0 || (argv[0] != "activate" && argv[0] != "status") {
		fmt.Fprintln(os.Stderr, "usage: air-worker operational-goal activate|status -product <root> [-node <full-id> -source-head <sha>]")
		return 2
	}
	action := argv[0]
	fs := flag.NewFlagSet("operational-goal "+action, flag.ContinueOnError)
	product := fs.String("product", ".", "AirWorker source root")
	id := fs.String("node", "", "full existing node ID")
	head := fs.String("source-head", "", "expected Git HEAD")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	var goal operationalGoal
	if action == "status" {
		goal, err = readOperationalGoal(root)
	} else {
		lock, ok := acquireLock(lockName("operational-goal", root))
		if !ok {
			fmt.Fprintln(os.Stderr, "operational goal transition is already running")
			return 2
		}
		defer lock.release()
		goal, err = operationalGoalInputs(root, *id, *head)
		if err == nil {
			var dirty string
			dirty, err = planRephaseGit(root, "status", "--porcelain")
			if err == nil && dirty != "" {
				err = errors.New("source must be clean before operational goal activation")
			}
		}
		if err == nil {
			path := operationalGoalPath(root)
			if _, readErr := os.Lstat(path); readErr == nil {
				var current operationalGoal
				current, err = readOperationalGoal(root)
				if err == nil && current != goal {
					err = errors.New("different operational goal already active")
				}
			} else if !os.IsNotExist(readErr) {
				err = readErr
			} else {
				raw, marshalErr := json.MarshalIndent(goal, "", "  ")
				if marshalErr != nil {
					err = marshalErr
				} else {
					err = writeNewOperationalGoal(path, append(raw, '\n'))
				}
			}
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "operational goal:", err)
		return 2
	}
	if err := json.NewEncoder(os.Stdout).Encode(goal); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	return 0
}
