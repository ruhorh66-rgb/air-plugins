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
	"strings"
	"time"
)

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
	if !result.Ok {
		if result.Subtype == "coordinator_handoff" {
			// Two real Codex workers have completed changes in independent
			// worktrees; ChatGPT is responsible for merge+factual+Anthropic.
			// This is not a Codex failure, must not auto-escalate/re-dispatch,
			// and must not close N139 by treating a handoff as source PASS.
			line("WAIT_COORDINATOR_INTEGRATION: " + result.Detail)
			return 2
		}
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
