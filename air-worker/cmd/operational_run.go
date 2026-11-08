package main

import (
	"bytes"
	"context"
	"crypto/sha256"
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
	if !free {
		line("NOT_PROVEN: N139_PRODUCT_WRITER_ACTIVE; do not start another coding job")
		return 2
	}
	defer ownerLock.release()
	c := &loopCtx{Root: root, Cfg: cfg, CfgPath: cfgPath,
		MaxTurns:  orInt(cfg.Budget.TurnsPerIteration, 60),
		Principal: principal, SessionKey: sessionKey,
		Orchestrate: cfg.Orchestration.Enabled,
		Subagents:   cfg.Orchestration.Subagents}
	return runN139Step(c, goal, operationalGoalPreview, c.runModelStep, c.semanticJudge)
}

func runN139Step(c *loopCtx, goal operationalGoal, verify func(string, operationalGoal) error,
	model func(workStep, string, string, runnerSpec) stepResult,
	review func(workStep, runnerSpec, semanticFactualPacket, string) semanticRun) int {
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
			line("NOT_PROVEN: COORDINATOR_WAIT: " + err.Error())
			return 2
		}
		ack, err := n139CoordinatorVerify(c.Root, c.scope(), step.Num, step.CodingTask)
		if err != nil || ack.Status != "ACK_RECORDED" {
			line(fmt.Sprintf("NOT_PROVEN: COORDINATOR_ACK_UNVERIFIED: status=%q error=%v", ack.Status, err))
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
		coordinatorIntegrated = true
	}
	if !result.Ok && !coordinatorIntegrated {
		line("NOT_PROVEN: CODEX_" + result.Subtype + ": " + result.Detail)
		return 2
	}
	after, err := n139SourceDigest(c.Root)
	if err != nil || bytes.Equal(before, after) {
		line(fmt.Sprintf("NOT_PROVEN: SOURCE_WRITE_NOT_PROVEN: %v", err))
		return 2
	}
	if err := verify(c.Root, goal); err != nil {
		line("NOT_PROVEN: SELECTOR_CHANGED: " + err.Error())
		return 2
	}
	// Factual Go is the same owned, resumable native job family as the Codex
	// executor, not an untracked CombinedOutput subprocess. An interrupted
	// caller can read this exact scoped receipt/output without launching Go again.
	factual, err := n139FactualJudge(c, step)
	if err != nil {
		line("NOT_PROVEN: FACTUAL_GO_UNPROVEN: " + err.Error())
		return 2
	}
	semantic := review(step, runner, factual, result.Detail)
	if semantic.Error != "" || semantic.Reviewer.Kind != "claude" || combinedAcceptance(0, semantic.Verdict) != "PASS" {
		line("NOT_PROVEN: ANTHROPIC_NOT_PASS: " + semantic.Error + " " + semantic.Verdict.Verdict)
		return 2
	}
	line("NOT_PROVEN: N139_FULL_DONE_WHEN_UNPROVEN: source write, factual Go and semantic PASS are evidence; native node remains open")
	return 2
}

var n139TaskMarker = regexp.MustCompile(`(?m)^<!-- air-worker-coding-task: (docs/tasks/[A-Za-z0-9_][A-Za-z0-9_.-]*\.json) sha256=([0-9a-f]{64}) -->\r?$`)

// selectN139CodingTask accepts only the live bytes of regular files tracked at the selected HEAD.
func selectN139CodingTask(root string, goal operationalGoal) (string, error) {
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
	if len(matches) != 1 || bytes.Count(node, []byte("air-worker-coding-task:")) != 1 {
		return "", errors.New("exactly one valid N139 coding task marker required")
	}
	relTask := string(matches[0][1])
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
	if fmt.Sprintf("%x", sha256.Sum256(task)) != string(matches[0][2]) {
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
	for _, item := range []struct {
		path string
		live []byte
	}{{filepath.ToSlash(filepath.Join("plan", filepath.Base(nodePath))), node}, {relTask, task}} {
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
