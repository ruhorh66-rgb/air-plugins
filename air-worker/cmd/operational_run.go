package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

var n139CoordinatorWait = func(c *loopCtx, step workStep) error {
	deadline := time.Now().Add(30 * time.Minute)
	for {
		ack, err := readCodingIntegrationAck(c.Root, c.scope(), step.Num, step.CodingTask)
		if err != nil {
			return err
		}
		switch ack.Status {
		case "ACK_RECORDED":
			return nil
		case "WAIT_COORDINATOR_INTEGRATION":
			if !time.Now().Before(deadline) {
				return errors.New("coordinator acknowledgement timed out")
			}
			time.Sleep(time.Second)
		default:
			return fmt.Errorf("invalid coordinator acknowledgement status: %q", ack.Status)
		}
	}
}

var n139CoordinatorVerify = readCodingIntegrationAck
var n139CommittedSourceCheck = sourceReviewGitState
var n139CommitWait = waitN139Commit
var n139AdvanceOwnedGoal = advanceN139OwnedGoal
var n139BoundSourceReview = reviewN139BoundSource

func reviewN139BoundSource(c *loopCtx, goal operationalGoal, task string) error {
	check := func() error {
		current, err := readOperationalGoal(c.Root)
		if err != nil || current != goal {
			return errors.New("operational goal changed or invalid")
		}
		head, err := sourceReviewGitState(c.Root)
		if err != nil || head != goal.SourceHead {
			return errors.New("source is dirty or HEAD changed")
		}
		selected, err := selectN139CodingTask(c.Root, goal)
		if err != nil || task == "" || selected != task {
			return errors.New("original coding task changed or invalid")
		}
		return nil
	}
	if err := check(); err != nil {
		return err
	}
	node, err := sourceReviewNode(c.Root, goal.NodeID)
	if err != nil || node.ID != goal.NodeID || strings.TrimSpace(node.DoneWhen) == "" {
		return errors.New("source review node or done_when invalid")
	}
	reviewer, err := judgeLaneRunner(c.Cfg, "default")
	if err == nil {
		err = enforceIndependentAnthropicReviewer(c.Cfg, runnerSpec{Kind: "codex"}, reviewer)
	}
	if err != nil {
		return err
	}
	step := "source-review-" + node.ID + "-" + goal.SourceHead[:12]
	receipt, output := jobReceiptPaths(c.scope(), step, "semantic-reviewer")
	for _, path := range []string{receipt, output} {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			return errors.New("source review receipt or output already exists or is unreadable")
		}
	}
	taskSHA := sha256Hex([]byte(task))
	prompt := sourceReviewPrompt("NATIVE_NODE_ID: "+node.ID+"\nNATIVE_NODE_DONE_WHEN: "+node.DoneWhen+"\n"+task, goal.SourceHead, taskSHA)
	reply, _, err := invokeNativeSourceReview(c.scope(), prompt, reviewer, step)
	if err != nil {
		return err
	}
	if err := check(); err != nil {
		return err
	}
	verdict, err := parseSourceReviewVerdict(reply, c.Root, goal.SourceHead, taskSHA)
	if err != nil {
		return err
	}
	if verdict.Verdict != "PASS_SOURCE_ONLY" {
		return fmt.Errorf("source review did not pass: %s", verdict.Verdict)
	}
	return nil
}

func waitN139Commit(c *loopCtx, step workStep, ack codingIntegrationAck) (string, error) {
	deadline := time.Now().Add(30 * time.Minute)
	for {
		head, err := gitCodingOutput(c.Root, "rev-parse", "HEAD")
		if err != nil {
			return "", err
		}
		if head != ack.SourceHead {
			return verifyCodingCommitted(c.Root, c.scope(), step.Num, step.CodingTask, ack)
		}
		if !time.Now().Before(deadline) {
			return "", errors.New("coordinator commit timed out")
		}
		time.Sleep(time.Second)
	}
}

func advanceN139OwnedGoal(root string, prior operationalGoal, nextHead string, writer *osLock) (operationalGoal, error) {
	var empty operationalGoal
	if !validAutoSelfDeliveryLock(writer) {
		return empty, errors.New("invalid N139 writer lock")
	}
	goalLock, ok := acquireLock(lockName("operational-goal", root))
	if !ok || !validAutoSelfDeliveryLock(goalLock) {
		return empty, errors.New("operational goal lock unavailable or invalid")
	}
	defer goalLock.release()
	if head, err := sourceReviewGitState(root); err != nil || head != nextHead {
		return empty, errors.New("source is dirty or HEAD changed")
	}
	path := operationalGoalPath(root)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return empty, errors.New("prior selector absent or nonregular")
	}
	oldRaw, err := os.ReadFile(path)
	if err != nil {
		return empty, err
	}
	current, err := decodeOperationalGoalPinned(oldRaw)
	if err != nil || current != prior {
		return empty, errors.New("prior selector changed or invalid")
	}
	next, err := validateOperationalGoalAdvance(root, prior, prior.NodeID, prior.SourceHead, nextHead)
	if err != nil {
		return empty, err
	}
	nextRaw, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return empty, err
	}
	check, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(check, oldRaw) {
		return empty, errors.New("prior selector changed during transition")
	}
	if !validAutoSelfDeliveryLock(writer) || !validAutoSelfDeliveryLock(goalLock) {
		return empty, errors.New("transition lock invalid")
	}
	if head, err := sourceReviewGitState(root); err != nil || head != nextHead {
		return empty, errors.New("source changed during transition")
	}
	check, err = os.ReadFile(path)
	if err != nil || !bytes.Equal(check, oldRaw) {
		return empty, errors.New("prior selector changed before write")
	}
	if err := writeFileAtomic(path, append(nextRaw, '\n')); err != nil {
		return empty, err
	}
	verified, err := readOperationalGoal(root)
	if err != nil || verified != next {
		_ = writeFileAtomic(path, oldRaw)
		return empty, errors.New("selector postwrite verification failed")
	}
	return verified, nil
}

// N139 is deliberately one source step. Its full done_when includes independent
// acceptance beyond this bootstrap, so this runner never closes the node.
func runN139Operational(root string, cfg runConfig, cfgPath string, goal operationalGoal, principal, sessionKey string) int {
	// The code-editing job and independent semantic job must belong to the
	// exact declared ChatGPT/Codex session, never to a shared legacy scope.
	if _, err := executorScopeForSession(root, principal, sessionKey, ""); err != nil {
		line("NOT_PROVEN: SESSION_UNDECLARED: " + err.Error())
		return 2
	}
	// Different sessions share this source tree. Native session jobs remain
	// individually scoped, but only one N139 writer may work on the product
	// at a time even when another session has a distinct job namespace.
	ownerLock, free := acquireLock(lockName("operational-n139", root))
	if !free || (cfg.Orchestration.Enabled && cfg.Orchestration.RequireNativeSubagents && !validAutoSelfDeliveryLock(ownerLock)) {
		line("NOT_PROVEN: N139_PRODUCT_WRITER_ACTIVE; do not start another coding job")
		return 2
	}
	if validAutoSelfDeliveryLock(ownerLock) {
		defer ownerLock.release()
	}
	c := &loopCtx{Root: root, Cfg: cfg, CfgPath: cfgPath,
		MaxTurns:  orInt(cfg.Budget.TurnsPerIteration, 60),
		Principal: principal, SessionKey: sessionKey,
		Orchestrate: cfg.Orchestration.Enabled,
		Subagents:   cfg.Orchestration.Subagents}
	return runN139Step(c, goal, operationalGoalPreview, c.runModelStep, c.semanticJudge, ownerLock)
}

func runN139Step(c *loopCtx, goal operationalGoal, verify func(string, operationalGoal) error,
	model func(workStep, string, string, runnerSpec) stepResult,
	review func(workStep, runnerSpec, semanticFactualPacket, string) semanticRun, writers ...*osLock) int {
	managed := c.Orchestrate && c.Cfg.Orchestration.RequireNativeSubagents
	if managed && (len(writers) != 1 || !validAutoSelfDeliveryLock(writers[0])) {
		line("EXECUTION_UNKNOWN: N139_WRITER_LOCK_INVALID")
		return 2
	}
	if err := verify(c.Root, goal); err != nil {
		line("NOT_PROVEN: SELECTOR_INVALID: " + err.Error())
		return 2
	}
	if goal.NodeID != "N-139_n136-native-operational-goal-selector-for-real-a" {
		line("NOT_PROVEN: WRONG_NODE")
		return 2
	}
	const rung = "gpt6-luna:low"
	if !resolveLadderTier(c.Cfg.Ladder, rung).Found {
		line("NOT_PROVEN: CODING_RUNG_ABSENT")
		return 2
	}
	runner := resolveRunner(c.Cfg, rung)
	if runner.Kind != "codex" || len(runner.AddDirs) != 0 {
		line("NOT_PROVEN: CODING_ROUTE_INVALID")
		return 2
	}
	step := workStep{Num: goal.NodeID, Title: "N139 fix terminal output integrity; FIRST read docs/tasks/DEVWORK29_N139_AUTONOMOUS_TAMPER_FIX_20261107.md and cmd/job_receipt.go and cmd/job_status.go; edit only cmd non-test Go source", Tier: rung, Judge: "N139 terminal output tamper RED to GREEN with genuine cmd source edit, bounded factual Go RC0, independent Anthropic PASS"}
	operation := runnerReceiptOperation("executor-codex", runner, 0)
	receipt, output := jobReceiptPaths(c.scope(), step.Num, operation)
	if _, err := os.Lstat(receipt); err == nil {
		state, inspectErr := inspectOwnedJob(c.scope(), step.Num, operation)
		if inspectErr != nil {
			line("NOT_PROVEN: JOB_RECEIPT_INVALID: " + inspectErr.Error())
		} else {
			line("NOT_PROVEN: JOB_ALREADY_" + state.Status + " (" + state.JobID + "); inspect receipt, do not redispatch")
		}
		return 2
	} else if !os.IsNotExist(err) {
		line("NOT_PROVEN: JOB_RECEIPT_UNREADABLE: " + err.Error())
		return 2
	}
	if _, err := os.Lstat(output); err == nil || !os.IsNotExist(err) {
		line("NOT_PROVEN: ORPHAN_JOB_OUTPUT; do not redispatch")
		return 2
	}
	if c.Orchestrate && c.Cfg.Orchestration.RequireNativeSubagents {
		task, err := selectN139CodingTask(c.Root, goal)
		if err != nil {
			line("NOT_PROVEN: N139_CODING_TASK_UNBOUND: " + err.Error())
			return 2
		}
		step.CodingTask = task
		step.Title = "N139 execute the source-bound two-worker coding task"
		step.Judge = "ChatGPT coordinates two isolated code writers; factual and semantic acceptance remain independent"
		status, statusErr := codingStatus(c.Root, c.scope(), step.Num, task)
		if statusErr != nil || status.Status != "NOT_FOUND" {
			var jobs []string
			for _, worker := range status.Workers {
				if worker.JobID != "" {
					jobs = append(jobs, fmt.Sprintf("worker%d=%s", worker.Index, worker.JobID))
				}
			}
			line(fmt.Sprintf("EXECUTION_UNKNOWN: N139_CODING_WORK_EXISTS: status=%s jobs=%v error=%v", status.Status, jobs, statusErr))
			return 2
		}
	}
	before, err := n139SourceDigest(c.Root)
	if err != nil {
		line("NOT_PROVEN: SOURCE_BEFORE: " + err.Error())
		return 2
	}
	guard, err := snapshotJudgeFiles(c.Root, c.Cfg, c.CfgPath)
	if err != nil {
		line("NOT_PROVEN: JUDGE_SNAPSHOT: " + err.Error())
		return 2
	}
	nodePath, err := planNodePathByID(c.Root, goal.NodeID)
	if err != nil {
		line("NOT_PROVEN: NODE_PATH: " + err.Error())
		return 2
	}
	protected := []string{filepath.Join(c.Root, "PLAN.md"), nodePath, filepath.Join(c.Root, "plan", "release-slices.json"), operationalGoalPath(c.Root), filepath.Join(c.Root, "goal", "goal.json"), filepath.Join(c.Root, "learn", "events.jsonl")}
	nodes, err := filepath.Glob(filepath.Join(c.Root, "plan", "N-*.md"))
	if err != nil {
		line("NOT_PROVEN: NODE_INVENTORY: " + err.Error())
		return 2
	}
	protected = append(protected, nodes...)
	tests, err := filepath.Glob(filepath.Join(c.Root, "cmd", "*_test.go"))
	if err != nil {
		line("NOT_PROVEN: TEST_INVENTORY: " + err.Error())
		return 2
	}
	protected = append(protected, tests...)
	var absent []string
	for _, path := range protected {
		raw, readErr := os.ReadFile(path)
		if readErr == nil {
			guard.files[path] = raw
		} else if os.IsNotExist(readErr) {
			absent = append(absent, path)
		} else {
			line("NOT_PROVEN: PROTECTED_SNAPSHOT: " + readErr.Error())
			return 2
		}
	}
	if step.CodingTask != "" {
		if err := verify(c.Root, goal); err != nil {
			line("NOT_PROVEN: SELECTOR_CHANGED: " + err.Error())
			return 2
		}
		task, err := selectN139CodingTask(c.Root, goal)
		if err != nil || task != step.CodingTask {
			line(fmt.Sprintf("NOT_PROVEN: N139_CODING_TASK_CHANGED: %v", err))
			return 2
		}
	}
	if managed {
		if !validAutoSelfDeliveryLock(writers[0]) {
			line("EXECUTION_UNKNOWN: N139_WRITER_LOCK_INVALID")
			return 2
		}
		goalLock, ok := acquireLock(lockName("operational-goal", c.Root))
		if !ok || !validAutoSelfDeliveryLock(goalLock) {
			line("EXECUTION_UNKNOWN: GOAL_LOCK_INVALID")
			return 2
		}
		goalLock.release()
	}
	result := model(step, rung, "", runner)
	changed, restoreErr := restoreJudgeFiles(guard)
	if restoreErr != nil || len(changed) != 0 {
		line(fmt.Sprintf("NOT_PROVEN: PROTECTED_FILE_TAMPER: restored=%v error=%v", changed, restoreErr))
		return 2
	}
	for _, path := range absent {
		if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
			line("NOT_PROVEN: PROTECTED_FILE_CREATED: " + path)
			return 2
		}
	}
	currentNodes, err := filepath.Glob(filepath.Join(c.Root, "plan", "N-*.md"))
	if err != nil || len(currentNodes) != len(nodes) {
		line("NOT_PROVEN: NODE_INVENTORY_CHANGED")
		return 2
	}
	currentTests, err := filepath.Glob(filepath.Join(c.Root, "cmd", "*_test.go"))
	if err != nil || len(currentTests) != len(tests) {
		line("NOT_PROVEN: TEST_INVENTORY_CHANGED")
		return 2
	}
	coordinatorIntegrated := false
	if !result.Ok && result.Subtype == "coordinator_handoff" {
		line("WAIT_COORDINATOR_INTEGRATION: " + result.Detail)
		if step.CodingTask == "" {
			return 2
		}
		if err := n139CoordinatorWait(c, step); err != nil {
			line("EXECUTION_UNKNOWN: COORDINATOR_WAIT: " + err.Error())
			return 2
		}
		ack, err := n139CoordinatorVerify(c.Root, c.scope(), step.Num, step.CodingTask)
		if err != nil || ack.Status != "ACK_RECORDED" {
			line(fmt.Sprintf("EXECUTION_UNKNOWN: COORDINATOR_ACK_UNVERIFIED: status=%q error=%v", ack.Status, err))
			return 2
		}
		for path, before := range guard.files {
			after, readErr := os.ReadFile(path)
			if readErr != nil || !bytes.Equal(before, after) {
				line("NOT_PROVEN: PROTECTED_FILE_TAMPER: " + path)
				return 2
			}
		}
		for _, path := range absent {
			if _, err := os.Lstat(path); err == nil || !os.IsNotExist(err) {
				line("NOT_PROVEN: PROTECTED_FILE_CREATED: " + path)
				return 2
			}
		}
		for dir, before := range guard.dirs {
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != len(before) {
				line("NOT_PROVEN: JUDGE_INVENTORY_CHANGED: " + dir)
				return 2
			}
			for _, entry := range entries {
				if !before[entry.Name()] {
					line("NOT_PROVEN: JUDGE_INVENTORY_CHANGED: " + dir)
					return 2
				}
			}
		}
		currentNodes, nodeErr := filepath.Glob(filepath.Join(c.Root, "plan", "N-*.md"))
		currentTests, testErr := filepath.Glob(filepath.Join(c.Root, "cmd", "*_test.go"))
		if nodeErr != nil || testErr != nil || !slices.Equal(currentNodes, nodes) || !slices.Equal(currentTests, tests) {
			line("NOT_PROVEN: PROTECTED_INVENTORY_CHANGED")
			return 2
		}
		line("WAIT_COORDINATOR_COMMIT")
		head, err := n139CommitWait(c, step, ack)
		if err != nil {
			line("EXECUTION_UNKNOWN: COORDINATOR_COMMIT: " + err.Error())
			return 2
		}
		goal, err = n139AdvanceOwnedGoal(c.Root, goal, head, writers[0])
		if err != nil {
			line("EXECUTION_UNKNOWN: SELECTOR_ADVANCE: " + err.Error())
			return 2
		}
		if current, err := n139CommittedSourceCheck(c.Root); err != nil || current != head || goal.SourceHead != head {
			line("EXECUTION_UNKNOWN: COMMITTED_SOURCE_CHANGED")
			return 2
		}
		if err := verify(c.Root, goal); err != nil {
			line("EXECUTION_UNKNOWN: SELECTOR_CHANGED: " + err.Error())
			return 2
		}
		coordinatorIntegrated = true
	}
	if !result.Ok && !coordinatorIntegrated {
		line("NOT_PROVEN: CODEX_" + result.Subtype + ": " + result.Detail)
		return 2
	}
	after, err := n139SourceDigest(c.Root)
	if err != nil || bytes.Equal(before, after) {
		if coordinatorIntegrated {
			line(fmt.Sprintf("EXECUTION_UNKNOWN: SOURCE_WRITE_NOT_PROVEN: %v", err))
		} else {
			line(fmt.Sprintf("NOT_PROVEN: SOURCE_WRITE_NOT_PROVEN: %v", err))
		}
		return 2
	}
	if err := verify(c.Root, goal); err != nil {
		if coordinatorIntegrated {
			line("EXECUTION_UNKNOWN: SELECTOR_CHANGED: " + err.Error())
		} else {
			line("NOT_PROVEN: SELECTOR_CHANGED: " + err.Error())
		}
		return 2
	}
	if coordinatorIntegrated {
		if head, err := n139CommittedSourceCheck(c.Root); err != nil || head != goal.SourceHead {
			line("EXECUTION_UNKNOWN: COMMITTED_SOURCE_CHANGED")
			return 2
		}
		if err := verify(c.Root, goal); err != nil {
			line("EXECUTION_UNKNOWN: SELECTOR_CHANGED: " + err.Error())
			return 2
		}
	}
	// Factual Go is the same owned, resumable native job family as the Codex
	// executor, not an untracked CombinedOutput subprocess. An interrupted
	// caller can read this exact scoped receipt/output without launching Go again.
	factual, err := n139FactualJudge(c, step)
	if err != nil {
		line("NOT_PROVEN: FACTUAL_GO_UNPROVEN: " + err.Error())
		return 2
	}
	if coordinatorIntegrated {
		if err := n139BoundSourceReview(c, goal, step.CodingTask); err != nil {
			line("NOT_PROVEN: SOURCE_REVIEW_UNPROVEN: " + err.Error())
			return 2
		}
	} else {
		semantic := review(step, runner, factual, result.Detail)
		if semantic.Error != "" || semantic.Reviewer.Kind != "claude" || combinedAcceptance(0, semantic.Verdict) != "PASS" {
			line("NOT_PROVEN: ANTHROPIC_NOT_PASS: " + semantic.Error + " " + semantic.Verdict.Verdict)
			return 2
		}
	}
	line("NOT_PROVEN: N139_FULL_DONE_WHEN_UNPROVEN: source write, factual Go and review PASS are evidence; native node remains open")
	return 2
}

var n139TaskMarker = regexp.MustCompile(`(?m)^<!-- air-worker-coding-task: (docs/tasks/[A-Za-z0-9_][A-Za-z0-9_.-]*\.json) sha256=([0-9a-f]{64}) -->\r?$`)

// selectN139CodingTask accepts only the live bytes of regular files tracked at the selected HEAD.
func selectN139CodingTask(root string, goal operationalGoal) (string, error) {
	if goal.NodeID != "N-139_n136-native-operational-goal-selector-for-real-a" {
		return "", errors.New("invalid N139 node")
	}
	nodePath, err := planNodePathByID(root, goal.NodeID)
	if err != nil {
		return "", err
	}
	if err := verifyPlanRephaseControlledPaths(root, nodePath); err != nil {
		return "", err
	}
	node, err := os.ReadFile(nodePath)
	if err != nil || len(node) > 1<<20 {
		return "", errors.New("node missing or oversized")
	}
	matches := n139TaskMarker.FindAllSubmatch(node, -1)
	markerCount := bytes.Count(node, []byte("air-worker-coding-task:"))
	const bindingPath = "docs/tasks/N139_ACTIVE_BINDING.json"
	bindingFile := filepath.Join(root, filepath.FromSlash(bindingPath))
	_, bindingErr := os.Lstat(bindingFile)
	trackedBinding, err := gitCodingOutput(root, "ls-tree", "HEAD", "--", bindingPath)
	if err != nil || (bindingErr != nil && !os.IsNotExist(bindingErr)) {
		return "", errors.New("binding inventory unavailable")
	}
	var binding []byte
	var relTask, taskSHA string
	if markerCount != 0 {
		if len(matches) != 1 || markerCount != 1 || bindingErr == nil || trackedBinding != "" {
			return "", errors.New("ambiguous or invalid N139 coding task binding")
		}
		relTask, taskSHA = string(matches[0][1]), string(matches[0][2])
	} else {
		if err := verifyPlanRephaseControlledPaths(root, filepath.Join(root, "docs"), filepath.Join(root, "docs", "tasks"), bindingFile); err != nil {
			return "", err
		}
		info, err := os.Lstat(bindingFile)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
			return "", errors.New("binding missing, non-regular, or oversized")
		}
		binding, err = os.ReadFile(bindingFile)
		if err != nil || len(binding) > 4096 || !json.Valid(binding) || codingJSONValue(json.NewDecoder(bytes.NewReader(binding))) != nil {
			return "", errors.New("invalid binding JSON")
		}
		var fields map[string]string
		if json.Unmarshal(binding, &fields) != nil || len(fields) != 3 || fields["node_id"] != goal.NodeID {
			return "", errors.New("invalid binding schema or node")
		}
		relTask, taskSHA = fields["task_path"], fields["task_sha256"]
		bindingMarker := n139TaskMarker.FindStringSubmatch("<!-- air-worker-coding-task: " + relTask + " sha256=" + taskSHA + " -->")
		if len(bindingMarker) != 3 || bindingMarker[1] != relTask || bindingMarker[2] != taskSHA {
			return "", errors.New("invalid binding task path or SHA256")
		}
	}
	taskPath := filepath.Join(root, filepath.FromSlash(relTask))
	if err := verifyPlanRephaseControlledPaths(root, filepath.Join(root, "docs"), filepath.Join(root, "docs", "tasks"), taskPath); err != nil {
		return "", err
	}
	info, err := os.Lstat(taskPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 48*1024 {
		return "", errors.New("task missing, non-regular, or oversized")
	}
	task, err := os.ReadFile(taskPath)
	if err != nil || len(task) > 48*1024 {
		return "", errors.New("task unreadable or oversized")
	}
	if fmt.Sprintf("%x", sha256.Sum256(task)) != taskSHA {
		return "", errors.New("task SHA256 mismatch")
	}
	if _, err := parseCodingMicrosteps(string(task)); err != nil {
		return "", err
	}
	head, err := gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil || head != goal.SourceHead {
		return "", errors.New("source HEAD changed")
	}
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", errors.New("Git worktree root unavailable")
	}
	physicalRoot, rootErr := filepath.EvalSymlinks(root)
	physicalTop, topErr := filepath.EvalSymlinks(top)
	if rootErr != nil || topErr != nil {
		return "", errors.New("Git worktree paths unavailable")
	}
	prefix, err := filepath.Rel(physicalTop, physicalRoot)
	if err != nil || filepath.IsAbs(prefix) || prefix == ".." || strings.HasPrefix(prefix, ".."+string(filepath.Separator)) {
		return "", errors.New("product root is outside Git worktree")
	}
	items := []struct {
		path string
		live []byte
	}{{filepath.ToSlash(filepath.Join("plan", filepath.Base(nodePath))), node}, {relTask, task}}
	if binding != nil {
		items = append(items, struct {
			path string
			live []byte
		}{bindingPath, binding})
	}
	for _, item := range items {
		state, stateErr := gitCodingOutput(root, "status", "--porcelain=v1", "--untracked-files=all", "--", item.path)
		if stateErr != nil || state != "" {
			return "", fmt.Errorf("tracked file is not clean: %s", item.path)
		}
		mode, err := gitCodingOutput(root, "ls-tree", "HEAD", "--", item.path)
		if err != nil || (!strings.HasPrefix(mode, "100644 blob ") && !strings.HasPrefix(mode, "100755 blob ")) || !strings.HasSuffix(mode, "\t"+item.path) {
			return "", fmt.Errorf("file is not a regular tracked HEAD blob: %s", item.path)
		}
		blobPath := filepath.ToSlash(filepath.Join(prefix, filepath.FromSlash(item.path)))
		size, err := gitCodingOutput(root, "cat-file", "-s", "HEAD:"+blobPath)
		if err != nil || size != fmt.Sprint(len(item.live)) {
			return "", fmt.Errorf("HEAD blob size differs from live file: %s", item.path)
		}
		blob := exec.Command("git", "-C", root, "show", "HEAD:"+blobPath)
		committed, err := blob.Output()
		if err != nil || !bytes.Equal(committed, item.live) {
			return "", fmt.Errorf("live file differs from HEAD: %s", item.path)
		}
	}
	if binding == nil {
		if _, err := os.Lstat(bindingFile); err == nil || !os.IsNotExist(err) {
			return "", errors.New("binding appeared during task selection")
		}
	}
	head, err = gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil || head != goal.SourceHead {
		return "", errors.New("source HEAD changed during task selection")
	}
	return string(task), nil
}

func n139SourceDigest(root string) ([]byte, error) {
	files, err := filepath.Glob(filepath.Join(root, "cmd", "*.go"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("cmd source inventory unavailable: %v", err)
	}
	h := sha256.New()
	for _, path := range files {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		_, _ = h.Write([]byte(filepath.Base(path)))
		_, _ = h.Write(raw)
	}
	return h.Sum(nil), nil
}

// Narrow test seam: production always binds to the native receipted factual Go.
var n139FactualJudge = runN139FactualGo

const n139GoSelector = "^(TestOperationalGoal|TestN139|TestN123|TestCriterion42Pending)"

// runN139FactualGo makes the bounded Go fact-check a native durable job.
// Its receipt/output are deterministic for this scoped step. It can be
// inspected after connection loss, and an existing terminal/orphan receipt
// refuses duplicate execution rather than being silently overwritten.
func configureN139OwnedGoCommand(cmd *exec.Cmd) {
	cmd.Cancel = func() error { return terminateLearningProcessTree(cmd) }
	cmd.WaitDelay = 3 * time.Second
}

func runN139FactualGo(c *loopCtx, step workStep) (semanticFactualPacket, error) {
	var factual semanticFactualPacket
	if step.Num != "N-139_n136-native-operational-goal-selector-for-real-a" {
		return factual, errors.New("factual step is not the native N139 scope")
	}
	tmpRoot := filepath.Join(c.Root, ".woody", "tmp", "n139")
	goCache := filepath.Join(tmpRoot, "gocache")
	goTmp := filepath.Join(tmpRoot, "gotmp")
	for _, dir := range []string{goCache, goTmp} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return factual, fmt.Errorf("prepare owned Go temp: %w", err)
		}
		physicalRoot, err := filepath.EvalSymlinks(c.Root)
		if err != nil {
			return factual, err
		}
		physical, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return factual, err
		}
		rel, err := filepath.Rel(physicalRoot, physical)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return factual, errors.New("Go cache/temp must remain inside the owned product root")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	// Fixed literal selector and arguments only. No plan text or model output
	// becomes an executable command. Timeout only affects this owned child.
	cmd := exec.CommandContext(ctx, "go", "-C", "cmd", "test", "-run", n139GoSelector, "-count=1", ".")
	configureN139OwnedGoCommand(cmd)
	cmd.Dir = c.Root
	cmd.Env = append(os.Environ(), "GOCACHE="+goCache, "GOTMPDIR="+goTmp, "TEMP="+goTmp, "TMP="+goTmp)
	operation := n123GoJudgeOperation(c.Root, "factual-go-test", cmd)
	out, err := runReceiptedWithMeta(ctx, c.scope(), step.Num, operation, cmd, jobReceiptMeta{
		Runner: "script", Provider: "local", Model: "go-test", Effort: "none",
		Role: "factual-judge", Sandbox: "owned-product",
	})
	if err != nil {
		if errors.Is(err, errJobStillRunning) {
			return factual, fmt.Errorf("factual Go remains RUNNING/timeout: recover same job, never redispatch")
		}
		detail := strings.TrimSpace(decodeOutput(out))
		if len(detail) > 2048 {
			detail = detail[:2048]
		}
		return factual, fmt.Errorf("factual Go failed: %w; output=%s", err, detail)
	}
	snapshot, err := inspectOwnedJob(c.scope(), step.Num, operation)
	if err != nil {
		return factual, fmt.Errorf("factual Go receipt/output unverified: %w", err)
	}
	receipt, _ := readJobReceipt(snapshot.ReceiptPath)
	if snapshot.Status != jobStatusDone || !snapshot.Terminal || receipt == nil ||
		!receipt.ProcessStarted || receipt.PID <= 0 ||
		snapshot.OutputSHA256 == "" || snapshot.OutputBytes != int64(len(out)) {
		return factual, errors.New("factual Go lacks accepted terminal job/output proof")
	}
	factual = semanticFactualPacket{
		Scope: "step", Code: 0,
		Text:   "fixed Go selector RC0; native job_id=" + snapshot.JobID + "; output_sha256=" + snapshot.OutputSHA256,
		Passed: []string{"literal targeted Go selector", "cmd source changed", "terminal native factual job receipt"},
	}
	return factual, nil
}
