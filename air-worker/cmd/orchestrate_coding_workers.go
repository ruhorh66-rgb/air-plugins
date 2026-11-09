package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// codingWorkerEvidence is a durable pointer to ACTUAL source edits in a private
// worktree. It is not a model assertion or an accepted product change.
type codingWorkerEvidence struct {
	Index           int
	File            string
	DoneWhen        string
	MicrostepSHA256 string
	Workspace       string
	Product         string
	Patch           string
	PatchSHA256     string
	Receipt         string
	JobID           string
	Model           string
	Status          string
	Changed         []string
	Error           string
	Measurement     string `json:"measurement,omitempty"`
}

type codingWorkersHandoff struct {
	Schema        string
	Status        string
	SourceHead    string
	TaskSHA256    string
	Retry         *codingWorkersHandoff `json:"retry,omitempty"`
	SourceProduct string
	Coordinator   string
	Model         string
	WorkerCount   int
	Workers       []codingWorkerEvidence
	Report        string
	Error         string
}

type codingMicrostep struct {
	Index        int    `json:"index"`
	Title        string `json:"title"`
	File         string `json:"file"`
	DoneWhen     string `json:"done_when"`
	Instructions string `json:"instructions"`
}
type codingMicrosteps struct {
	Schema  string            `json:"schema"`
	Context string            `json:"context"`
	Workers []codingMicrostep `json:"workers"`
}

// Reject duplicate keys before decoding: encoding/json otherwise accepts the last value.
func codingJSONValue(d *json.Decoder) error {
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			k, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := k.(string)
			if !ok || seen[key] {
				return errors.New("duplicate or invalid JSON key")
			}
			seen[key] = true
			if err := codingJSONValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	case '[':
		for d.More() {
			if err := codingJSONValue(d); err != nil {
				return err
			}
		}
		_, err = d.Token()
		return err
	default:
		return errors.New("invalid JSON value")
	}
}

var codingScopePart = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]*$`)

func parseCodingMicrosteps(task string) (codingMicrosteps, error) {
	var result codingMicrosteps
	fail := func() (codingMicrosteps, error) {
		return codingMicrosteps{}, errors.New("invalid two-worker microsteps task")
	}
	if len(task) == 0 || len(task) > 48*1024 {
		return fail()
	}
	check := json.NewDecoder(strings.NewReader(task))
	if codingJSONValue(check) != nil {
		return fail()
	}
	if _, err := check.Token(); err != io.EOF {
		return fail()
	}
	decoder := json.NewDecoder(strings.NewReader(task))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Schema != "air-worker.coding-microsteps/v1" || len(strings.TrimSpace(result.Context)) == 0 || len(result.Context) > 8192 || len(result.Workers) != 2 {
		return fail()
	}
	seen := map[string]bool{}
	for _, w := range result.Workers {
		if w.Index < 1 || w.Index > 2 || seen[fmt.Sprint(w.Index)] {
			return fail()
		}
		seen[fmt.Sprint(w.Index)] = true
		if len(strings.TrimSpace(w.Title)) < 3 || len(w.Title) > 256 || len(strings.TrimSpace(w.DoneWhen)) < 10 || len(w.DoneWhen) > 4096 || len(strings.TrimSpace(w.Instructions)) < 10 || len(w.Instructions) > 8192 {
			return fail()
		}
		if w.File == "" || len(w.File) > 240 || strings.Contains(w.File, "\\") || path.Clean(w.File) != w.File || strings.HasPrefix(w.File, "/") {
			return fail()
		}
		for _, part := range strings.Split(w.File, "/") {
			lower := strings.ToLower(part)
			if !codingScopePart.MatchString(part) || strings.HasPrefix(part, ".") || lower == "plan" || strings.HasPrefix(lower, "plan.") || strings.Contains(lower, "secret") || strings.Contains(lower, "credential") || strings.Contains(lower, "password") || strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
				return fail()
			}
		}
		key := strings.ToLower(w.File)
		if seen[key] {
			return fail()
		}
		seen[key] = true
	}
	if strings.EqualFold(result.Workers[0].File, result.Workers[1].File) || strings.HasPrefix(strings.ToLower(result.Workers[0].File), strings.ToLower(result.Workers[1].File)+"/") || strings.HasPrefix(strings.ToLower(result.Workers[1].File), strings.ToLower(result.Workers[0].File)+"/") {
		return fail()
	}
	if result.Workers[0].Index == 2 {
		result.Workers[0], result.Workers[1] = result.Workers[1], result.Workers[0]
	}
	return result, nil
}

func codingWorkerTask(task codingMicrosteps, w codingMicrostep, taskSHA, head string) string {
	raw, _ := json.MarshalIndent(struct {
		Context    string          `json:"context"`
		Worker     codingMicrostep `json:"worker"`
		TaskSHA256 string          `json:"task_sha256"`
		SourceHead string          `json:"source_head"`
	}{task.Context, w, taskSHA, head}, "", "  ")
	return fmt.Sprintf("CODING WORKER %d OF 2. You are a CODE WRITER in your OWN isolated Git worktree. Edit only your assigned file. Do not modify other workspaces, shared source, PLAN, Git metadata or credentials. Do not commit. The ChatGPT conversation is your coordinator.\n\nCOORDINATOR TASK:\n%s\n", w.Index, raw)
}

func codingWorkerTaskVersion(task codingMicrosteps, w codingMicrostep, taskSHA, head, version string) string {
	fullTask := codingWorkerTask(task, w, taskSHA, head)
	if version == "v3" {
		fullTask = strings.Replace(fullTask, "The ChatGPT conversation is your coordinator.", "The Codex conversation is your coordinator.", 1)
	}
	return fullTask
}

func codingReceiptVersion(operation, taskSHA, principal string) string {
	if strings.HasSuffix(operation, "-v2-task-"+taskSHA) {
		return "v2"
	}
	if principal == "codex" && strings.HasSuffix(operation, "-v3-task-"+taskSHA) {
		return "v3"
	}
	return ""
}

func codingWorkerPrompt(taskPath, fullTask string) string {
	return "AUTHORITATIVE WORKER TASK (also saved at " + taskPath + " for audit):\n" + fullTask +
		"\nEdit the actual files inside YOUR worktree, run bounded checks if needed, and give a concise report. Your actual Git diff, not your prose, is the evidence."
}

func gitCodingOutput(root string, args ...string) (string, error) {
	argv := append([]string{"-C", root}, args...)
	cmd := exec.Command("git", argv...)
	// Git on Windows may print LF/CRLF notices on stderr even on successful
	// operations. Never mix stderr warnings into machine path/HEAD evidence.
	out, err := cmd.Output()
	if err != nil {
		reason := ""
		if e, ok := err.(*exec.ExitError); ok {
			reason = strings.TrimSpace(string(e.Stderr))
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, reason)
	}
	return strings.TrimSpace(string(out)), nil
}

func shaCodingBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isCodingProductPath(relative string) bool {
	relative = filepath.ToSlash(strings.TrimSpace(relative))
	return relative != "" && relative != "." && !strings.HasPrefix(relative, "../") &&
		!strings.Contains(relative, "/../") && !strings.HasPrefix(relative, "/") &&
		!strings.Contains(relative, ":")
}

func prepareCodingWorktrees(root string, scope sessionScope, step, sourceHead string) (string, [2]string, error) {
	var productDirs [2]string
	repoTop, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", productDirs, err
	}
	repoTop, err = filepath.Abs(repoTop)
	if err != nil {
		return "", productDirs, err
	}
	relativeProduct, err := filepath.Rel(repoTop, root)
	if err != nil || (relativeProduct != "." && !isCodingProductPath(relativeProduct)) {
		return "", productDirs, errors.New("product is outside its verified Git repository")
	}
	// Check only our product: shared monorepositories contain unrelated workers.
	selection := "."
	if relativeProduct != "." {
		selection = filepath.ToSlash(relativeProduct)
	}
	status, err := gitCodingOutput(repoTop, "status", "--porcelain", "--", selection)
	if err != nil || status != "" {
		return "", productDirs, errors.New("product source is not clean; do not clone a drifting source task")
	}
	head, err := gitCodingOutput(repoTop, "rev-parse", "HEAD")
	if err != nil || head != sourceHead {
		return "", productDirs, errors.New("Git source HEAD changed before two-worker launch")
	}
	id := shaCodingBytes([]byte(repoTop + "|" + scope.key() + "|" + step))[:18]
	base := filepath.Join(filepath.Dir(repoTop), ".airworker-coding-workspaces", filepath.Base(repoTop), id)
	if _, err := os.Lstat(base); err == nil {
		return "", productDirs, fmt.Errorf("worker workspace %s already exists; use receipt readback, never rerun", base)
	} else if !os.IsNotExist(err) {
		return "", productDirs, err
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return "", productDirs, err
	}
	for index := 1; index <= 2; index++ {
		workerRepo := filepath.Join(base, fmt.Sprintf("worker-%d", index))
		if _, err := gitCodingOutput(repoTop, "worktree", "add", "--detach", workerRepo, sourceHead); err != nil {
			return "", productDirs, fmt.Errorf("cannot create independent Git worktree %d: %w", index, err)
		}
		directory := workerRepo
		if relativeProduct != "." {
			directory = filepath.Join(workerRepo, relativeProduct)
		}
		if info, err := os.Stat(directory); err != nil || !info.IsDir() {
			return "", productDirs, fmt.Errorf("worker product %d missing after checkout: %v", index, err)
		}
		productDirs[index-1] = directory
	}
	return base, productDirs, nil
}

func codingChangedFiles(root string) ([]string, []byte, error) {
	// Changes are inspected against HEAD, not from an LLM text response.
	before, err := gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil {
		return nil, nil, err
	}
	_ = before
	// Include new source files without staging their contents or committing.
	untracked, err := gitCodingOutput(root, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, nil, err
	}
	for _, name := range strings.Split(untracked, "\n") {
		name = strings.TrimSpace(name)
		if name == "" || name == ".woody" || strings.HasPrefix(filepath.ToSlash(name), ".woody/") {
			continue
		}
		if !isCodingProductPath(name) {
			return nil, nil, fmt.Errorf("invalid untracked path %q", name)
		}
		if _, err := gitCodingOutput(root, "add", "-N", "--", name); err != nil {
			return nil, nil, err
		}
	}
	files, err := gitCodingOutput(root, "diff", "--name-only", "HEAD", "--", ".")
	if err != nil {
		return nil, nil, err
	}
	if files == "" {
		return nil, nil, errors.New("worker wrote no source files; textual patch is not coding")
	}
	names := strings.Split(files, "\n")
	for _, name := range names {
		if !isCodingProductPath(name) || strings.HasPrefix(filepath.ToSlash(name), ".woody/") {
			return nil, nil, fmt.Errorf("unsafe changed path %q", name)
		}
	}
	cmd := exec.Command("git", "-C", root, "diff", "--no-ext-diff", "--binary", "HEAD", "--", ".")
	raw, err := cmd.Output()
	if err != nil {
		return nil, nil, fmt.Errorf("read exact worker Git diff: %w", err)
	}
	if len(raw) == 0 || len(raw) > 256*1024 || strings.Contains(string(raw), "GIT binary patch") {
		return nil, nil, errors.New("worker Git diff missing, too large or binary")
	}
	if _, err := gitCodingOutput(root, "diff", "--check", "HEAD", "--", "."); err != nil {
		return nil, nil, err
	}
	return names, raw, nil
}

// Exactly two real Codex coders WRITE in two separate Git worktrees. The
// controlling conversation is coordinator; there is NO third worker leader.
// The original shared repository stays unchanged. Neither source changes nor
// a final judge can be marked PASS until the coordinator inspects/merges them.
func (c *loopCtx) runIsolatedCodingWorkers(exePath, task, step string, runner runnerSpec) codingWorkersHandoff {
	doc := c.runIsolatedCodingWorkersOnce(exePath, task, step, runner)
	if doc.Status != "NOT_PROVEN" || doc.Report == "" || len(doc.Workers) != 2 || doc.Workers[0].JobID == "" || doc.Workers[1].JobID == "" || doc.Workers[0].JobID == doc.Workers[1].JobID {
		return doc
	}
	scope := c.scope()
	lock, ok := acquireLock(scopedLockName("coding-escalate:"+step, scope))
	if !ok || !validAutoSelfDeliveryLock(lock) {
		if lock != nil {
			lock.release()
		}
		return doc
	}
	defer lock.release()
	trace, next, exe, err := validateCodingEscalation(c.Root, c.Cfg, scope, step, []byte(task))
	if err != nil || trace.Attempt2.Status != "NOT_FOUND" || trace.Attempt1.SourceHead != doc.SourceHead || trace.Attempt1.Report != doc.Report || len(trace.Attempt1.Workers) != 2 || trace.Attempt1.Workers[0].JobID != doc.Workers[0].JobID || trace.Attempt1.Workers[1].JobID != doc.Workers[1].JobID {
		return doc
	}
	retry := c.runIsolatedCodingWorkersOnce(exe, task, step+"-retry-1", next)
	if retry.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" {
		return retry
	}
	verified, physical, err := effectiveCodingEvidence(c.Root, scope, step, task)
	if err != nil || physical != step+"-retry-1" {
		retry.Status = "NOT_PROVEN"
		retry.Error = "automatic retry evidence could not be verified"
		return retry
	}
	return verified
}

func (c *loopCtx) runIsolatedCodingWorkersOnce(exePath, task, step string, runner runnerSpec) codingWorkersHandoff {
	doc := codingWorkersHandoff{Schema: "air-worker.chatgpt-coordinated-codex-workers/v1",
		Status: "NOT_PROVEN", SourceProduct: c.Root, Coordinator: "ChatGPT/current-conversation",
		Model: runner.Model, WorkerCount: 2, TaskSHA256: shaCodingBytes([]byte(task))}
	if c.Principal == "codex" {
		doc.Coordinator = "Codex/current-conversation"
	}
	parsed, err := parseCodingMicrosteps(task)
	if err != nil {
		doc.Error = err.Error()
		return doc
	}
	if c.Subagents != 2 || !c.Cfg.Orchestration.RequireNativeSubagents {
		doc.Error = "canonical two-Codex-worker mode is not enabled"
		return doc
	}
	if err := validateCodexAddDirs(runner.AddDirs); err != nil {
		doc.Error = err.Error()
		return doc
	}
	if err := requirePonytailSkill(); err != nil {
		doc.Error = "Ponytail: " + err.Error()
		return doc
	}
	originalHead, err := gitCodingOutput(c.Root, "rev-parse", "HEAD")
	if err != nil {
		doc.Error = err.Error()
		return doc
	}
	doc.SourceHead = originalHead
	base, dirs, err := prepareCodingWorktrees(c.Root, c.scope(), step, originalHead)
	if err != nil {
		doc.Error = err.Error()
		return doc
	}
	type outcome struct {
		worker codingWorkerEvidence
	}
	ch := make(chan outcome, 2)
	var wg sync.WaitGroup
	for i := 1; i <= 2; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			microstep := parsed.Workers[i-1]
			microRaw, _ := json.Marshal(microstep)
			work := codingWorkerEvidence{Index: i, File: microstep.File, DoneWhen: microstep.DoneWhen, MicrostepSHA256: shaCodingBytes(microRaw), Product: dirs[i-1],
				Workspace: filepath.Dir(dirs[i-1]), Model: runner.Model, Status: "NOT_PROVEN"}
			workFolder := filepath.Join(dirs[i-1], ".woody")
			if err := os.MkdirAll(workFolder, 0700); err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			taskPath := filepath.Join(workFolder, "TASK-CODE-WORKER.md")
			version := "v2"
			if c.Principal == "codex" {
				version = "v3"
			}
			fullTask := codingWorkerTaskVersion(parsed, microstep, doc.TaskSHA256, originalHead, version)
			if err := os.WriteFile(taskPath, []byte(fullTask), 0600); err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			prompt := codingWorkerPrompt(taskPath, fullTask)
			cmd := codexCommandForSandbox(exePath, dirs[i-1], prompt, runner, "workspace-write")
			agentStep := fmt.Sprintf("%s-coding-worker-%d", step, i)
			operation := runnerReceiptOperation("executor-codex-subagent", runner, c.iter) + "-" + version + "-task-" + shaCodingBytes([]byte(task))
			output, runErr := runReceiptedWithMeta(context.Background(), c.scope(), agentStep, operation,
				cmd, codexReceiptMeta(runner, "coding-subagent/worker", "workspace-write"))
			receiptPath, _ := jobReceiptPaths(c.scope(), agentStep, operation)
			work.Receipt = receiptPath
			receipt, _ := readJobReceipt(receiptPath)
			if receipt != nil {
				work.JobID = receipt.JobID
			}
			modelResult := parseCodexResult(decodeOutput(output), runErr)
			if !modelResult.Ok || receipt == nil || !receipt.ProcessStarted || receipt.Status != jobStatusDone {
				work.Error = "native job terminal success missing: " + modelResult.Subtype
				ch <- outcome{work}
				return
			}
			if newHead, err := gitCodingOutput(dirs[i-1], "rev-parse", "HEAD"); err != nil || newHead != originalHead {
				work.Error = "worker changed base HEAD or Git revision is unreadable"
				ch <- outcome{work}
				return
			}
			names, patch, err := codingChangedFiles(dirs[i-1])
			if err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			top, err := gitCodingOutput(dirs[i-1], "rev-parse", "--show-toplevel")
			if err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			rel, err := filepath.Rel(top, dirs[i-1])
			if err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			prefix := filepath.ToSlash(rel)
			for _, name := range names {
				normalized := filepath.ToSlash(name)
				if prefix != "." {
					normalized = strings.TrimPrefix(normalized, prefix+"/")
				}
				if normalized != microstep.File {
					work.Error = "worker changed file outside declared scope: " + name
					ch <- outcome{work}
					return
				}
			}
			// Immutable by name in a unique run; never overwrite accepted evidence.
			patchPath := filepath.Join(base, fmt.Sprintf("worker-%d.patch", i))
			file, err := os.OpenFile(patchPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			_, writeErr := file.Write(patch)
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				work.Error = "cannot persist worker patch"
				ch <- outcome{work}
				return
			}
			work.Patch = patchPath
			work.PatchSHA256 = shaCodingBytes(patch)
			work.Changed = names
			work.Status = "CODE_WRITTEN_IN_OWN_WORKTREE"
			ch <- outcome{work}
		}()
	}
	wg.Wait()
	close(ch)
	workers := make([]codingWorkerEvidence, 2)
	allGood := true
	for done := range ch {
		workers[done.worker.Index-1] = done.worker
		if done.worker.Status != "CODE_WRITTEN_IN_OWN_WORKTREE" {
			allGood = false
		}
	}
	doc.Workers = workers
	if current, err := gitCodingOutput(c.Root, "rev-parse", "HEAD"); err != nil || current != originalHead {
		allGood = false
		doc.Error = "original source HEAD drifted during worker execution"
	}
	if allGood {
		doc.Status = "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION"
	} else if doc.Error == "" {
		doc.Error = "one or more workers did not produce factual code edits; no automatic integration"
	}
	doc.Report = filepath.Join(base, "handoff.json")
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err == nil {
		err = os.WriteFile(doc.Report, append(raw, '\n'), 0600)
	}
	if err != nil {
		doc.Report = ""
		doc.Status = "NOT_PROVEN"
		doc.Error = "cannot persist worker handoff: " + err.Error()
	}
	return doc
}

func cmdOrchestrateCode(argv []string) int {
	if len(argv) > 0 && argv[0] == "ack-status" {
		return cmdCodingAckStatus(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "ack-integration" {
		return cmdCodingAckIntegration(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "escalate" {
		return cmdCodingEscalate(argv[1:])
	}
	if len(argv) > 0 && argv[0] == "status" {
		return cmdCodingStatus(argv[1:])
	}
	fs := flag.NewFlagSet("orchestrate code", flag.ContinueOnError)
	product := fs.String("product", ".", "canonical product root")
	task := fs.String("task-file", "", "original bounded task file inside product")
	class := fs.String("class", "standard", "bulk|standard|complex|hardest")
	tier := fs.String("tier", "", "approved explicit Codex tier")
	reason := fs.String("reason", "", "required Astra reason")
	principal := fs.String("principal", "", "current ChatGPT session principal")
	sessionKey := fs.String("session-key", "", "current ChatGPT session key")
	stateDir := fs.String("state-dir", "", "native session state")
	jsonFlag := fs.Bool("json", false, "machine-readable report")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	emitErr := func(err error) int {
		if *jsonFlag {
			_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "air-worker.chatgpt-coordinated-codex-workers/v1", "status": "NOT_PROVEN", "error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, "NOT_PROVEN: "+err.Error())
		}
		return 2
	}
	root, cfg, err := loadExecutorConfig(*product, "")
	if err != nil {
		return emitErr(err)
	}
	if !cfg.Orchestration.Enabled || !cfg.Orchestration.RequireNativeSubagents ||
		!cfg.Orchestration.RequireAnthropicReviewer || cfg.Orchestration.Subagents != 2 {
		return emitErr(errors.New("managed product must mandate exactly two Codex workers and Anthropic final review"))
	}
	if err := requireNativeLoopInvocation(cfg, true, 2); err != nil {
		return emitErr(err)
	}
	scope, err := executorScopeForSession(root, *principal, *sessionKey, *stateDir)
	if err != nil {
		return emitErr(err)
	}
	taskPath, err := taskFileInsideProduct(root, *task)
	if err != nil {
		return emitErr(err)
	}
	taskBody, err := os.ReadFile(taskPath)
	if err != nil || len(taskBody) == 0 || len(taskBody) > 48*1024 {
		return emitErr(errors.New("task must be readable and bounded (1..49152 bytes)"))
	}
	selected := strings.TrimSpace(*tier)
	if selected == "" {
		selected, err = startTierForClass(cfg, strings.ToLower(strings.TrimSpace(*class)))
		if err != nil {
			return emitErr(err)
		}
	}
	if _, err := approvedExecutorTier(cfg, selected); err != nil {
		return emitErr(err)
	}
	if tierName(selected) == "gpt6-astra" && len(strings.TrimSpace(*reason)) < 16 {
		return emitErr(errors.New("Astra requires -reason at least 16 characters"))
	}
	runner := resolveRunner(cfg, selected)
	if runner.Kind != "codex" {
		return emitErr(errors.New("only native Codex coding workers supported"))
	}
	exePath, err := resolveRunnerTool("codex")
	if err != nil {
		return emitErr(err)
	}
	ctx := &loopCtx{Root: root, Cfg: cfg, Principal: scope.ID.Principal, SessionKey: scope.ID.SessionKey,
		Orchestrate: true, Subagents: 2}
	doc := ctx.runIsolatedCodingWorkers(exePath, string(taskBody), "coord-"+filepath.Base(taskPath), runner)
	if *jsonFlag {
		_ = json.NewEncoder(os.Stdout).Encode(doc)
	} else {
		fmt.Printf("%s source=%s model=%s workers=%d report=%s error=%s\n",
			doc.Status, doc.SourceHead, doc.Model, len(doc.Workers), doc.Report, doc.Error)
		for _, w := range doc.Workers {
			fmt.Printf("worker%d %s files=%v workspace=%s patch=%s receipt=%s\n",
				w.Index, w.Status, w.Changed, w.Product, w.Patch, w.Receipt)
		}
	}
	if doc.Status == "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" {
		return 0 // handoff prepared, NEVER a completed/factually judged product step
	}
	return 2
}

var errHashedOwnershipUnknown = errors.New("hashed worker receipt ownership unknown")

// codingStatus reads only the original scope's durable evidence; it never starts a job.
func codingStatus(root string, scope sessionScope, step, task string) (codingWorkersHandoff, error) {
	doc := codingWorkersHandoff{Schema: "air-worker.chatgpt-coordinated-codex-workers/v1", Status: "NOT_FOUND", SourceProduct: root, WorkerCount: 2}
	parsed, err := parseCodingMicrosteps(task)
	if err != nil {
		return doc, err
	}
	taskSHA := shaCodingBytes([]byte(task))
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return doc, err
	}
	top, err = filepath.Abs(top)
	if err != nil {
		return doc, err
	}
	rel, err := filepath.Rel(top, root)
	if err != nil || (rel != "." && !isCodingProductPath(rel)) {
		return doc, errors.New("product outside repository")
	}
	id := shaCodingBytes([]byte(top + "|" + scope.key() + "|" + step))[:18]
	base := filepath.Join(filepath.Dir(top), ".airworker-coding-workspaces", filepath.Base(top), id)
	_, baseErr := os.Lstat(base)
	if baseErr != nil && !os.IsNotExist(baseErr) {
		return doc, baseErr
	}
	if baseErr == nil {
		// An attacker-controlled workspace link must not redirect evidence reads.
		realBase, err := filepath.EvalSymlinks(base)
		if err != nil || !strings.EqualFold(filepath.Clean(realBase), filepath.Clean(base)) {
			return doc, errors.New("workspace path is not canonical")
		}
	}
	report := filepath.Join(base, "handoff.json")
	if _, err := os.Lstat(report); err == nil {
		if err := jobOwnedEvidencePath(base, report); err != nil {
			return doc, err
		}
		raw, err := os.ReadFile(report)
		if err != nil || len(raw) > 1024*1024 {
			return doc, errors.New("invalid handoff")
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			return doc, err
		}
		if doc.TaskSHA256 != taskSHA {
			return doc, errors.New("handoff task hash mismatch")
		}
		if doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" && doc.Status != "NOT_PROVEN" {
			return doc, errors.New("invalid handoff status")
		}
		if doc.Schema != "air-worker.chatgpt-coordinated-codex-workers/v1" || doc.SourceProduct != root || doc.WorkerCount != 2 || len(doc.Workers) != 2 || doc.SourceHead == "" || filepath.Clean(doc.Report) != report {
			return doc, errors.New("handoff identity mismatch")
		}
	} else if !os.IsNotExist(err) {
		return doc, err
	} else {
		doc.Report = report
		doc.Workers = []codingWorkerEvidence{{Index: 1, Status: "EXECUTION_UNKNOWN"}, {Index: 2, Status: "EXECUTION_UNKNOWN"}}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".woody", "jobs"))
	if err != nil && !os.IsNotExist(err) {
		return doc, err
	}
	anyReceipt := false
	failedProven := doc.TaskSHA256 == taskSHA && len(doc.Workers) == 2
	for i := 1; i <= 2; i++ {
		w := &doc.Workers[i-1]
		workerProduct := filepath.Join(base, fmt.Sprintf("worker-%d", i), rel)
		microRaw, _ := json.Marshal(parsed.Workers[i-1])
		if w.JobID == "" || w.Receipt == "" || w.Product != workerProduct || w.File != parsed.Workers[i-1].File || w.DoneWhen != parsed.Workers[i-1].DoneWhen || w.MicrostepSHA256 != shaCodingBytes(microRaw) {
			failedProven = false
		}
		if real, e := filepath.EvalSymlinks(workerProduct); e == nil && strings.EqualFold(filepath.Clean(real), filepath.Clean(workerProduct)) {
			if head, e := gitCodingOutput(workerProduct, "rev-parse", "HEAD"); e == nil {
				if doc.SourceHead != "" && doc.SourceHead != head {
					return doc, errors.New("worker source mismatch")
				}
				doc.SourceHead = head
			} else {
				failedProven = false
			}
		} else {
			failedProven = false
		}
		if w.Index != i {
			return doc, errors.New("handoff worker order mismatch")
		}
		workerStep := fmt.Sprintf("%s-coding-worker-%d", step, i)
		prefix := reNonWord.ReplaceAllString(scope.ID.namespace()+"__"+workerStep+"__executor-codex-subagent", "-")
		var snap jobStatusSnapshot
		var receipt *jobReceipt
		found := false
		unknownHashed := false
		for _, entry := range entries {
			name := entry.Name()
			legacy := strings.HasPrefix(name, prefix+"-") && strings.HasSuffix(name, ".receipt.json")
			hashed := strings.HasPrefix(name, "awjob-") && strings.HasSuffix(name, ".receipt.json") && len(name) == len("awjob-")+64+len(".receipt.json")
			if !legacy && !hashed {
				continue
			}
			if hashed {
				if _, err := hex.DecodeString(name[len("awjob-") : len("awjob-")+64]); err != nil {
					continue
				}
				info, err := entry.Info()
				if err != nil || info.Size() > 1024*1024 {
					if w.Receipt == filepath.Join(root, ".woody", "jobs", name) {
						return doc, errors.New("invalid owned hashed worker receipt")
					}
					unknownHashed = true
					continue
				}
			}
			path := filepath.Join(root, ".woody", "jobs", name)
			if err := jobOwnedEvidencePath(root, path); err != nil {
				if !hashed || w.Receipt == path {
					return doc, err
				}
				unknownHashed = true
				continue
			}
			r, err := readJobReceipt(path)
			if err != nil {
				if !hashed || w.Receipt == path {
					return doc, err
				}
				unknownHashed = true
				continue
			}
			if hashed && (r.Product != scope.Root || r.Principal != scope.ID.Principal || r.Session != scope.ID.SessionKey || r.Step != workerStep) {
				continue
			}
			if r.Step != workerStep || !strings.HasPrefix(r.Operation, "executor-codex-subagent-") || !strings.HasSuffix(r.Operation, "-task-"+taskSHA) {
				return doc, errors.New("receipt identity mismatch")
			}
			if found {
				return doc, errors.New("ambiguous worker receipts")
			}
			snap, err = inspectOwnedJob(scope, workerStep, r.Operation)
			if err != nil {
				return doc, err
			}
			if snap.ReceiptPath != path || r.Role != "coding-subagent/worker" {
				return doc, errors.New("foreign worker receipt")
			}
			found = true
			receipt = r
		}
		if unknownHashed && w.Receipt == "" {
			doc.Status = "NOT_PROVEN"
			w.Status = "EXECUTION_UNKNOWN"
			return doc, errHashedOwnershipUnknown
		}
		if unknownHashed {
			failedProven = false
		}
		if !found {
			failedProven = false
			if w.JobID != "" {
				return doc, errors.New("handoff receipt missing")
			}
			w.Status = "EXECUTION_UNKNOWN"
			continue
		}
		anyReceipt = true
		if w.JobID != "" && (w.JobID != snap.JobID || filepath.Clean(w.Receipt) != snap.ReceiptPath) {
			return doc, errors.New("handoff receipt mismatch")
		}
		w.JobID, w.Receipt = snap.JobID, snap.ReceiptPath
		w.Status = snap.Status
		if snap.Status != jobStatusRunning && !snap.Terminal {
			w.Status = "EXECUTION_UNKNOWN"
		}
		if snap.Terminal {
			_, sourceErr := hex.DecodeString(snap.SourceHead)
			_, argsErr := hex.DecodeString(snap.ArgsSHA256)
			sourceValid := (len(snap.SourceHead) == 40 || len(snap.SourceHead) == 64) && sourceErr == nil
			argsValid := len(snap.ArgsSHA256) == 64 && argsErr == nil
			recoveryValid := snap.JobID != "" && snap.RecoveryRef == filepath.ToSlash(snap.ReceiptPath)+"#job_id="+snap.JobID
			if !sourceValid || !argsValid || !recoveryValid {
				w.Status = "EXECUTION_UNKNOWN"
			}
		}
		if doc.SourceHead == "" {
			doc.SourceHead = snap.SourceHead
		} else if snap.SourceHead != "" && doc.SourceHead != snap.SourceHead {
			return doc, errors.New("receipt source mismatch")
		}
		version := codingReceiptVersion(receipt.Operation, taskSHA, scope.ID.Principal)
		if strings.HasSuffix(receipt.Operation, "-v2-task-"+taskSHA) || strings.HasSuffix(receipt.Operation, "-v3-task-"+taskSHA) || receipt.StdinSHA256 != "" {
			product := filepath.Join(base, fmt.Sprintf("worker-%d", i), rel)
			taskPath := filepath.Join(product, ".woody", "TASK-CODE-WORKER.md")
			fullTask := codingWorkerTaskVersion(parsed, parsed.Workers[i-1], taskSHA, snap.SourceHead, version)
			expected := shaCodingBytes([]byte(ponytailPrompt(codingWorkerPrompt(taskPath, fullTask))))
			ownedErr := jobOwnedEvidencePath(base, taskPath)
			var actual []byte
			var readErr error
			if ownedErr == nil {
				actual, readErr = os.ReadFile(taskPath)
			}
			if version == "" || ownedErr != nil || readErr != nil || string(actual) != fullTask || receipt.StdinSHA256 != expected {
				w.Status = "EXECUTION_UNKNOWN"
			} else {
				w.Measurement = "STDIN_SHA256_VERIFIED"
			}
		} else {
			w.Measurement = "LEGACY_UNMEASURED"
		}
		if !failedProven || w.Status != jobStatusFailed || !snap.Terminal || !receipt.ProcessStarted || snap.ExitCode == nil || *snap.ExitCode == 0 || len(snap.OutputSHA256) != 64 || w.Measurement != "STDIN_SHA256_VERIFIED" {
			failedProven = false
			if w.Status == jobStatusFailed {
				w.Status = "EXECUTION_UNKNOWN"
			}
		}
		if w.Patch != "" {
			product := filepath.Join(base, fmt.Sprintf("worker-%d", i), rel)
			realProduct, evalErr := filepath.EvalSymlinks(product)
			if evalErr != nil || !strings.EqualFold(filepath.Clean(realProduct), filepath.Clean(product)) {
				return doc, errors.New("unsafe worker product")
			}
			patch := filepath.Join(base, fmt.Sprintf("worker-%d.patch", i))
			if filepath.Clean(w.Product) != filepath.Clean(product) || filepath.Clean(w.Patch) != patch || jobOwnedEvidencePath(base, patch) != nil {
				return doc, errors.New("unsafe patch path")
			}
			data, err := os.ReadFile(patch)
			if err != nil || len(data) == 0 || len(data) > 256*1024 || shaCodingBytes(data) != w.PatchSHA256 {
				return doc, errors.New("patch hash mismatch")
			}
			if head, err := gitCodingOutput(product, "rev-parse", "HEAD"); err != nil || head != doc.SourceHead {
				return doc, errors.New("worker source mismatch")
			}
			untracked, err := gitCodingOutput(product, "ls-files", "--others", "--exclude-standard")
			if err != nil {
				return doc, err
			}
			for _, name := range strings.Split(untracked, "\n") {
				if name != "" && name != ".woody" && !strings.HasPrefix(filepath.ToSlash(name), ".woody/") {
					return doc, errors.New("untracked worker changes")
				}
			}
			files, err := gitCodingOutput(product, "diff", "--name-only", "HEAD", "--", ".")
			if err != nil || strings.Join(w.Changed, "\n") != files {
				return doc, errors.New("worker changed files mismatch")
			}
			cmd := exec.Command("git", "-C", product, "diff", "--no-ext-diff", "--binary", "HEAD", "--", ".")
			actual, err := cmd.Output()
			if err != nil || shaCodingBytes(actual) != w.PatchSHA256 {
				return doc, errors.New("worker patch mismatch")
			}
		} else if w.PatchSHA256 != "" || len(w.Changed) != 0 || doc.Status == "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" {
			return doc, errors.New("incomplete patch evidence")
		}
	}
	if !anyReceipt && os.IsNotExist(baseErr) {
		doc.Status = "NOT_FOUND"
		doc.Workers = nil
		return doc, nil
	}
	doc.Status = "EXECUTION_UNKNOWN"
	if len(doc.Workers) == 2 && doc.Workers[0].Status == jobStatusDone && doc.Workers[1].Status == jobStatusDone && doc.Workers[0].PatchSHA256 != "" && doc.Workers[1].PatchSHA256 != "" {
		doc.Status = "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION"
	} else if failedProven {
		doc.Status = jobStatusFailed
	}
	return doc, nil
}

// verifyCodingIntegrated compares verified worker patches with the current Git diff. Coordinator review and final
// product acceptance remain separate decisions.
func verifyCodingIntegrated(root string, scope sessionScope, step, task string) (codingWorkersHandoff, error) {
	doc, expected, err := verifiedCodingWorkerEvidence(root, scope, step, task)
	if err != nil {
		return doc, err
	}
	fail := func(reason string) (codingWorkersHandoff, error) { return doc, errors.New(reason) }
	head, err := gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil || head != doc.SourceHead {
		return fail("original source HEAD changed")
	}
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return doc, err
	}
	// Porcelain -z retains unusual path bytes and exposes untracked files. Only
	// unstaged edits to the two already tracked files are acceptable.
	status, err := exec.Command("git", "-C", top, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
	if err != nil {
		return doc, err
	}
	seenFiles := map[string]bool{}
	for _, entry := range strings.Split(strings.TrimSuffix(string(status), "\x00"), "\x00") {
		if len(entry) < 4 || entry[2] != ' ' || entry[:2] != " M" {
			return fail("source contains unrelated or unsupported changes")
		}
		name := entry[3:]
		if _, ok := expected[name]; !ok || seenFiles[name] {
			return fail("source changed file scope mismatch")
		}
		seenFiles[name] = true
	}
	if len(seenFiles) != 2 {
		return fail("source does not contain both worker edits")
	}
	for name, hash := range expected {
		if _, err := gitCodingOutput(top, "ls-files", "--error-unmatch", "--", name); err != nil {
			return fail("integrated file is not tracked")
		}
		file := filepath.Join(top, filepath.FromSlash(name))
		if err := jobOwnedEvidencePath(root, file); err != nil {
			return fail("unsafe integrated file")
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return fail("integrated path cannot be resolved")
		}
		diff, err := exec.Command("git", "-C", root, "diff", "--no-ext-diff", "--binary", "HEAD", "--", filepath.ToSlash(rel)).Output()
		if err != nil || len(diff) == 0 || len(diff) > 256*1024 || shaCodingBytes(diff) != hash {
			return fail("integrated bytes differ from worker edit")
		}
	}
	return doc, nil
}

func effectiveCodingEvidence(root string, scope sessionScope, step, task string) (codingWorkersHandoff, string, error) {
	original, err := codingStatus(root, scope, step, task)
	if err != nil || original.Status == "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" {
		return original, step, err
	}
	fail := func() (codingWorkersHandoff, string, error) {
		return original, step, errors.New("historical retry evidence is not verified")
	}
	if original.Status != jobStatusFailed || !operationalSHA.MatchString(original.SourceHead) {
		return fail()
	}
	physical := step + "-retry-1"
	retry, err := codingStatus(root, scope, physical, task)
	if err != nil || retry.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" || retry.SourceHead != original.SourceHead || retry.TaskSHA256 != original.TaskSHA256 || len(retry.Workers) != 2 {
		return fail()
	}
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return fail()
	}
	rel, err := filepath.Rel(top, root)
	if err != nil || (rel != "." && !isCodingProductPath(rel)) {
		return fail()
	}
	configPath := filepath.ToSlash(filepath.Join(rel, "run-config.json"))
	tree, err := gitCodingOutput(top, "ls-tree", original.SourceHead, "--", configPath)
	if err != nil || (!strings.HasPrefix(tree, "100644 blob ") && !strings.HasPrefix(tree, "100755 blob ")) || !strings.HasSuffix(tree, "\t"+configPath) {
		return fail()
	}
	object := original.SourceHead + ":" + configPath
	size, err := gitCodingOutput(top, "cat-file", "-s", object)
	n, sizeErr := strconv.Atoi(size)
	if err != nil || sizeErr != nil || n <= 0 || n > 1024*1024 {
		return fail()
	}
	show := exec.Command("git", "-C", top, "show", object)
	pipe, err := show.StdoutPipe()
	if err != nil || show.Start() != nil {
		return fail()
	}
	raw, readErr := io.ReadAll(io.LimitReader(pipe, 1024*1024+1))
	if len(raw) > 1024*1024 || readErr != nil {
		_ = show.Process.Kill()
	}
	err = show.Wait()
	var cfg runConfig
	if err != nil || readErr != nil || len(raw) != n || rejectDuplicateJSONKeys(raw) != nil || json.Unmarshal(raw, &cfg) != nil || validateModelPolicy(cfg) != nil {
		return fail()
	}
	trace, err := validateHistoricalCodingEscalation(root, cfg, scope, step, []byte(task))
	if err != nil {
		return fail()
	}
	next := resolveRunner(cfg, trace.ToTier)
	exe, err := resolveRunnerTool("codex")
	parsed, parseErr := parseCodingMicrosteps(task)
	if err != nil || parseErr != nil || next.Kind != "codex" {
		return fail()
	}
	seen := map[string]bool{}
	for _, w := range original.Workers {
		if w.JobID == "" || seen[w.JobID] {
			return fail()
		}
		seen[w.JobID] = true
	}
	for i, w := range retry.Workers {
		if w.JobID == "" || seen[w.JobID] {
			return fail()
		}
		seen[w.JobID] = true
		r, err := readJobReceipt(w.Receipt)
		if err != nil || r.Model != next.Model || r.Effort != next.Effort {
			return fail()
		}
		snap, err := inspectOwnedJob(scope, fmt.Sprintf("%s-coding-worker-%d", physical, i+1), r.Operation)
		version := codingReceiptVersion(r.Operation, retry.TaskSHA256, scope.ID.Principal)
		if err != nil || version == "" || !snap.Terminal || snap.Status != jobStatusDone || snap.JobID != w.JobID || snap.ReceiptPath != w.Receipt || snap.SourceHead != retry.SourceHead {
			return fail()
		}
		taskPath := filepath.Join(w.Product, ".woody", "TASK-CODE-WORKER.md")
		fullTask := codingWorkerTaskVersion(parsed, parsed.Workers[i], retry.TaskSHA256, retry.SourceHead, version)
		actual, err := os.ReadFile(taskPath)
		if err != nil || string(actual) != fullTask {
			return fail()
		}
		cmd := codexCommandForSandbox(exe, w.Product, codingWorkerPrompt(taskPath, fullTask), next, "workspace-write")
		stdin := jobStdinFingerprint(cmd)
		if stdin == "" || stdin != r.StdinSHA256 || jobCommandFingerprint(cmd) != snap.ArgsSHA256 {
			return fail()
		}
	}
	return retry, physical, nil
}

func verifiedCodingWorkerEvidence(root string, scope sessionScope, step, task string) (codingWorkersHandoff, map[string]string, error) {
	doc, physical, err := effectiveCodingEvidence(root, scope, step, task)
	if err != nil {
		return doc, nil, err
	}
	fail := func(reason string) (codingWorkersHandoff, map[string]string, error) {
		return doc, nil, errors.New(reason)
	}
	parsed, err := parseCodingMicrosteps(task)
	if err != nil {
		return doc, nil, err
	}
	if doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" || len(doc.Workers) != 2 || doc.TaskSHA256 != shaCodingBytes([]byte(task)) {
		return fail("two verified worker edits are not ready")
	}
	if err := jobOwnedEvidencePath(filepath.Dir(doc.Report), doc.Report); err != nil {
		return fail("unsafe original handoff")
	}
	var original codingWorkersHandoff
	raw, err := os.ReadFile(doc.Report)
	if err != nil || len(raw) == 0 || len(raw) > 1024*1024 || rejectDuplicateJSONKeys(raw) != nil || json.Unmarshal(raw, &original) != nil || original.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" ||
		original.Schema != doc.Schema || original.SourceHead != doc.SourceHead || original.TaskSHA256 != doc.TaskSHA256 ||
		original.SourceProduct != doc.SourceProduct || original.WorkerCount != 2 || original.Report != doc.Report || len(original.Workers) != 2 {
		return fail("original handoff was not ready")
	}
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return doc, nil, err
	}
	relRoot, err := filepath.Rel(top, root)
	if err != nil || relRoot == ".." || strings.HasPrefix(relRoot, ".."+string(filepath.Separator)) {
		return fail("product outside repository")
	}
	seenJobs := map[string]bool{}
	expected := map[string]string{}
	for i, w := range doc.Workers {
		pinned := original.Workers[i]
		microRaw, _ := json.Marshal(parsed.Workers[i])
		if pinned.Status != "CODE_WRITTEN_IN_OWN_WORKTREE" || pinned.Index != w.Index || pinned.File != w.File || pinned.DoneWhen != w.DoneWhen ||
			pinned.MicrostepSHA256 != w.MicrostepSHA256 || pinned.JobID != w.JobID || pinned.Receipt != w.Receipt ||
			pinned.Product != w.Product || pinned.Workspace != w.Workspace || pinned.Patch != w.Patch ||
			pinned.PatchSHA256 != w.PatchSHA256 || !reflect.DeepEqual(pinned.Changed, w.Changed) ||
			w.Index != i+1 || w.File != parsed.Workers[i].File || w.DoneWhen != parsed.Workers[i].DoneWhen ||
			w.MicrostepSHA256 != shaCodingBytes(microRaw) || w.Status != jobStatusDone ||
			w.Measurement != "STDIN_SHA256_VERIFIED" || w.JobID == "" || seenJobs[w.JobID] ||
			len(w.Changed) != 1 || w.Changed[0] != filepath.ToSlash(filepath.Join(relRoot, filepath.FromSlash(w.File))) {
			return fail("worker identity, scope or measured input mismatch")
		}
		seenJobs[w.JobID] = true
		receipt, err := readJobReceipt(w.Receipt)
		if err != nil || !receipt.ProcessStarted || receipt.Status != jobStatusDone ||
			codingReceiptVersion(receipt.Operation, doc.TaskSHA256, scope.ID.Principal) == "" || receipt.StdinSHA256 == "" {
			return fail("worker receipt is not sealed versioned evidence")
		}
		snap, err := inspectOwnedJob(scope, fmt.Sprintf("%s-coding-worker-%d", physical, i+1), receipt.Operation)
		if err != nil || !snap.Terminal || snap.Status != jobStatusDone || snap.JobID != w.JobID ||
			snap.ReceiptPath != w.Receipt || snap.SourceHead != doc.SourceHead || snap.ArgsSHA256 == "" ||
			snap.RecoveryRef != filepath.ToSlash(snap.ReceiptPath)+"#job_id="+snap.JobID {
			return fail("worker receipt is not sealed or owned")
		}
		workerFile := filepath.Join(w.Product, filepath.FromSlash(w.File))
		if err := jobOwnedEvidencePath(w.Product, workerFile); err != nil {
			return fail("unsafe worker file")
		}
		_, err = os.ReadFile(workerFile)
		if err != nil {
			return doc, nil, err
		}
		expected[filepath.ToSlash(filepath.Join(relRoot, filepath.FromSlash(w.File)))] = w.PatchSHA256
	}
	return doc, expected, nil
}

type codingIntegrationAck struct {
	AckSHA256     string    `json:"-"`
	Schema        string    `json:"schema"`
	Status        string    `json:"status"`
	Step          string    `json:"step"`
	Principal     string    `json:"principal"`
	SessionKey    string    `json:"session_key"`
	SourceHead    string    `json:"source_head"`
	TaskSHA256    string    `json:"task_sha256"`
	JobIDs        [2]string `json:"job_ids"`
	PatchSHA256   [2]string `json:"patch_sha256"`
	HandoffSHA256 string    `json:"handoff_sha256"`
}

// The caller must obtain original from readCodingIntegrationAck before the commit;
// caller-supplied ACK fields alone do not authorize a committed integration.
func verifyCodingCommitted(root string, scope sessionScope, step, task string, original codingIntegrationAck) (string, error) {
	fail := func(reason string) (string, error) { return "", errors.New(reason) }
	if (scope.ID.Principal != "gpt" && scope.ID.Principal != "codex") || !(strings.HasPrefix(step, "coord-") || step == "N-139_n136-native-operational-goal-selector-for-real-a") ||
		len(step) > 240 || !codingScopePart.MatchString(step) || strings.Contains(step, "..") ||
		original.Schema != "air-worker.coordinator-integration/v1" || original.Status != "ACK_RECORDED" ||
		original.Step != step || original.Principal != scope.ID.Principal || original.SessionKey != scope.ID.SessionKey ||
		original.TaskSHA256 != shaCodingBytes([]byte(task)) || !operationalSHA.MatchString(original.SourceHead) ||
		len(original.AckSHA256) != 64 {
		return fail("original acknowledgement identity is invalid")
	}
	if _, err := hex.DecodeString(original.AckSHA256); err != nil || strings.ToLower(original.AckSHA256) != original.AckSHA256 {
		return fail("original acknowledgement hash is invalid")
	}
	doc, expected, err := verifiedCodingWorkerEvidence(root, scope, step, task)
	if err != nil {
		return "", err
	}
	if doc.SourceHead != original.SourceHead || len(expected) != 2 {
		return fail("original source or worker file scope mismatch")
	}
	for i, w := range doc.Workers {
		if original.JobIDs[i] == "" || original.PatchSHA256[i] == "" || original.JobIDs[i] != w.JobID || original.PatchSHA256[i] != w.PatchSHA256 {
			return fail("original worker acknowledgement mismatch")
		}
	}
	handoff, err := os.ReadFile(doc.Report)
	if err != nil || len(handoff) == 0 || len(handoff) > 1024*1024 || shaCodingBytes(handoff) != original.HandoffSHA256 {
		return fail("original handoff acknowledgement mismatch")
	}
	dir := filepath.Dir(doc.Report)
	ackPath := filepath.Join(dir, "coordinator-integration.json")
	if filepath.Base(doc.Report) != "handoff.json" || jobOwnedEvidencePath(dir, ackPath) != nil {
		return fail("unsafe acknowledgement path")
	}
	info, err := os.Lstat(ackPath)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 8192 {
		return fail("original acknowledgement file is invalid")
	}
	owned, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer owned.Close()
	f, err := owned.Open("coordinator-integration.json")
	if err != nil {
		return "", err
	}
	raw, readErr := io.ReadAll(io.LimitReader(f, 8193))
	opened, statErr := f.Stat()
	closeErr := f.Close()
	after, afterErr := os.Lstat(ackPath)
	if readErr != nil || statErr != nil || closeErr != nil || afterErr != nil || !os.SameFile(info, opened) || !os.SameFile(info, after) ||
		len(raw) == 0 || len(raw) > 8192 || int64(len(raw)) != info.Size() || shaCodingBytes(raw) != original.AckSHA256 || rejectDuplicateJSONKeys(raw) != nil {
		return fail("original acknowledgement bytes changed")
	}
	var decoded codingIntegrationAck
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || decoder.Decode(new(any)) != io.EOF {
		return fail("original acknowledgement JSON is invalid")
	}
	decoded.AckSHA256 = original.AckSHA256
	if decoded != original {
		return fail("original acknowledgement fields changed")
	}
	head, err := gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil || !operationalSHA.MatchString(head) || head == original.SourceHead {
		return fail("committed HEAD is invalid")
	}
	parents, err := gitCodingOutput(root, "rev-list", "--parents", "-n", "1", head)
	if err != nil || parents != head+" "+original.SourceHead {
		return fail("commit is not a direct child of original source")
	}
	top, err := gitCodingOutput(root, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	clean := func() bool {
		status, err := exec.Command("git", "-C", top, "status", "--porcelain=v1", "-z", "--untracked-files=all").Output()
		return err == nil && len(status) == 0
	}
	if !clean() {
		return fail("committed source is dirty")
	}
	changed, err := exec.Command("git", "-C", top, "diff", "--name-only", "-z", original.SourceHead, head, "--").Output()
	if err != nil {
		return "", err
	}
	seen := map[string]bool{}
	for _, name := range strings.Split(strings.TrimSuffix(string(changed), "\x00"), "\x00") {
		if _, ok := expected[name]; !ok || seen[name] {
			return fail("commit changed file scope mismatch")
		}
		seen[name] = true
	}
	if len(seen) != 2 {
		return fail("commit does not contain exactly both worker files")
	}
	for name, hash := range expected {
		tracked, err := gitCodingOutput(top, "ls-tree", original.SourceHead, "--", name)
		if err != nil || (!strings.HasPrefix(tracked, "100644 blob ") && !strings.HasPrefix(tracked, "100755 blob ")) || !strings.HasSuffix(tracked, "\t"+name) {
			return fail("original worker file is not tracked")
		}
		file := filepath.Join(top, filepath.FromSlash(name))
		if jobOwnedEvidencePath(root, file) != nil {
			return fail("unsafe committed worker file")
		}
		diff, err := exec.Command("git", "-C", top, "diff", "--no-ext-diff", "--binary", original.SourceHead, head, "--", name).Output()
		if err != nil || len(diff) == 0 || len(diff) > 256*1024 || shaCodingBytes(diff) != hash {
			return fail("committed worker patch differs")
		}
	}
	if step == "N-139_n136-native-operational-goal-selector-for-real-a" {
		path := operationalGoalPath(root)
		if jobOwnedEvidencePath(root, path) != nil {
			return fail("unsafe original operational goal")
		}
		goalRaw, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		prior, err := decodeOperationalGoalPinned(goalRaw)
		if err != nil || prior.SourceHead != original.SourceHead || prior.NodeID != step {
			return fail("original operational goal is invalid")
		}
		prospective, err := validateOperationalGoalAdvance(root, prior, step, original.SourceHead, head)
		if err != nil {
			return "", err
		}
		bound, err := selectN139CodingTask(root, prospective)
		if err != nil || bound != task {
			return fail("N139 task is not bound to committed source")
		}
		again, err := os.ReadFile(path)
		if err != nil || !reflect.DeepEqual(again, goalRaw) {
			return fail("original operational goal changed")
		}
	}
	last, err := gitCodingOutput(root, "rev-parse", "HEAD")
	if err != nil || last != head || !clean() {
		return fail("committed source changed during verification")
	}
	return head, nil
}

// readCodingIntegrationAck only reports a recorded action while its original evidence still matches.
func readCodingIntegrationAck(root string, scope sessionScope, step, task string) (codingIntegrationAck, error) {
	waiting := codingIntegrationAck{Status: "WAIT_COORDINATOR_INTEGRATION"}
	if (scope.ID.Principal != "gpt" && scope.ID.Principal != "codex") || !(strings.HasPrefix(step, "coord-") || step == "N-139_n136-native-operational-goal-selector-for-real-a") ||
		len(step) > 240 || !codingScopePart.MatchString(step) || strings.Contains(step, "..") {
		return waiting, errors.New("invalid original coordinator step")
	}
	if step == "N-139_n136-native-operational-goal-selector-for-real-a" {
		goal, err := readOperationalGoal(root)
		if err != nil || goal.NodeID != step {
			return waiting, errors.New("N139 operational goal is not verified")
		}
		bound, err := selectN139CodingTask(root, goal)
		if err != nil || bound != task {
			return waiting, errors.New("N139 task is not plan-bound")
		}
	}
	doc, _, err := effectiveCodingEvidence(root, scope, step, task)
	if err != nil || doc.Status != "READY_FOR_CHATGPT_COORDINATOR_INTEGRATION" || len(doc.Workers) != 2 {
		return waiting, errors.New("original two-worker handoff is not verified")
	}
	dir := filepath.Dir(doc.Report)
	if filepath.Base(doc.Report) != "handoff.json" || jobOwnedEvidencePath(dir, doc.Report) != nil {
		return waiting, errors.New("unsafe original handoff path")
	}
	ackPath := filepath.Join(dir, "coordinator-integration.json")
	if err := jobOwnedEvidencePath(dir, ackPath); err != nil {
		if os.IsNotExist(err) {
			return waiting, nil
		}
		return waiting, err
	}
	before, err := os.Lstat(ackPath)
	if err != nil {
		if os.IsNotExist(err) {
			return waiting, nil
		}
		return waiting, err
	}
	if !before.Mode().IsRegular() || before.Size() == 0 || before.Size() > 8192 {
		return waiting, errors.New("invalid acknowledgement file")
	}
	owned, err := os.OpenRoot(dir)
	if err != nil {
		return waiting, err
	}
	defer owned.Close()
	f, err := owned.Open("coordinator-integration.json")
	if err != nil {
		return waiting, err
	}
	data, readErr := io.ReadAll(io.LimitReader(f, 8193))
	opened, statErr := f.Stat()
	closeErr := f.Close()
	after, afterErr := os.Lstat(ackPath)
	if readErr != nil || statErr != nil || closeErr != nil || afterErr != nil || !os.SameFile(before, opened) || !os.SameFile(before, after) ||
		len(data) == 0 || len(data) > 8192 || int64(len(data)) != before.Size() {
		return waiting, errors.New("acknowledgement identity or size changed")
	}
	check := json.NewDecoder(strings.NewReader(string(data)))
	if codingJSONValue(check) != nil {
		return waiting, errors.New("invalid acknowledgement JSON")
	}
	if _, err := check.Token(); err != io.EOF {
		return waiting, errors.New("trailing acknowledgement JSON")
	}
	var ack codingIntegrationAck
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&ack) != nil || ack.Schema != "air-worker.coordinator-integration/v1" || ack.Status != "ACK_RECORDED" ||
		ack.Step != step || ack.Principal != scope.ID.Principal || ack.SessionKey != scope.ID.SessionKey ||
		ack.TaskSHA256 != shaCodingBytes([]byte(task)) || ack.SourceHead != doc.SourceHead {
		return waiting, errors.New("acknowledgement scope or schema mismatch")
	}
	verified, err := verifyCodingIntegrated(root, scope, step, task)
	if err != nil || verified.Report != doc.Report || verified.SourceHead != ack.SourceHead || len(verified.Workers) != 2 {
		return waiting, errors.New("current integration is not verified")
	}
	for i, w := range verified.Workers {
		if ack.JobIDs[i] == "" || ack.PatchSHA256[i] == "" || ack.JobIDs[i] != w.JobID || ack.PatchSHA256[i] != w.PatchSHA256 {
			return waiting, errors.New("worker acknowledgement mismatch")
		}
	}
	handoff, err := os.ReadFile(verified.Report)
	if err != nil || len(handoff) == 0 || len(handoff) > 1024*1024 || ack.HandoffSHA256 != shaCodingBytes(handoff) {
		return waiting, errors.New("handoff acknowledgement mismatch")
	}
	ack.AckSHA256 = shaCodingBytes(data)
	return ack, nil
}

func cmdCodingAckStatus(argv []string) int {
	fs := flag.NewFlagSet("orchestrate code ack-status", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	task := fs.String("task-file", "", "original task file")
	step := fs.String("step", "", "original coordinator step")
	principal := fs.String("principal", "", "original principal")
	session := fs.String("session-key", "", "original session")
	state := fs.String("state-dir", "", "native session state")
	asJSON := fs.Bool("json", false, "machine output")
	if fs.Parse(argv) != nil || fs.NArg() != 0 {
		return 2
	}
	result := map[string]string{"status": "NOT_PROVEN"}
	err := func() error {
		root, cfg, err := loadExecutorConfig(*product, "")
		if err != nil {
			return err
		}
		if !cfg.Orchestration.Enabled || !cfg.Orchestration.RequireNativeSubagents || !cfg.Orchestration.RequireAnthropicReviewer || cfg.Orchestration.Subagents != 2 {
			return errors.New("managed two-worker configuration required")
		}
		scope, err := executorScopeForSession(root, *principal, *session, *state)
		if err != nil {
			return err
		}
		taskPath, err := taskFileInsideProduct(root, *task)
		if err != nil {
			return err
		}
		if *step != "coord-"+filepath.Base(taskPath) && *step != "N-139_n136-native-operational-goal-selector-for-real-a" {
			return errors.New("step does not match original task file")
		}
		body, err := os.ReadFile(taskPath)
		if err != nil || len(body) == 0 || len(body) > 48*1024 {
			return errors.New("invalid original task")
		}
		ack, err := readCodingIntegrationAck(root, scope, *step, string(body))
		if err != nil {
			return err
		}
		result["status"] = ack.Status
		if ack.Status == "ACK_RECORDED" {
			doc, _, err := effectiveCodingEvidence(root, scope, *step, string(body))
			if err != nil {
				return err
			}
			path := filepath.Join(filepath.Dir(doc.Report), "coordinator-integration.json")
			result["path"], result["ack_sha256"] = path, ack.AckSHA256
			result["source_head"], result["task_sha256"], result["handoff_sha256"] = ack.SourceHead, ack.TaskSHA256, ack.HandoffSHA256
			result["job_id_1"], result["job_id_2"] = ack.JobIDs[0], ack.JobIDs[1]
			result["patch_sha256_1"], result["patch_sha256_2"] = ack.PatchSHA256[0], ack.PatchSHA256[1]
		}
		return nil
	}()
	if err != nil {
		result = map[string]string{"status": "NOT_PROVEN", "error": err.Error()}
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(result)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "NOT_PROVEN: "+err.Error())
	} else {
		fmt.Printf("%s path=%s ack_sha256=%s\n", result["status"], result["path"], result["ack_sha256"])
	}
	if err != nil {
		return 2
	}
	return 0
}

// ackCodingIntegrated records a coordinator action; it does not certify human review or future source stability.
func ackCodingIntegrated(root string, scope sessionScope, step, task string) (string, error) {
	if (scope.ID.Principal != "gpt" && scope.ID.Principal != "codex") || !(strings.HasPrefix(step, "coord-") || step == "N-139_n136-native-operational-goal-selector-for-real-a") ||
		len(step) > 240 || !codingScopePart.MatchString(step) || strings.Contains(step, "..") {
		return "", errors.New("invalid original coordinator step")
	}
	if step == "N-139_n136-native-operational-goal-selector-for-real-a" {
		goal, err := readOperationalGoal(root)
		if err != nil || goal.NodeID != step {
			return "", errors.New("N139 operational goal is not verified")
		}
		bound, err := selectN139CodingTask(root, goal)
		if err != nil || bound != task {
			return "", errors.New("N139 task is not plan-bound")
		}
	}
	if _, err := parseCodingMicrosteps(task); err != nil {
		return "", err
	}
	doc, err := verifyCodingIntegrated(root, scope, step, task)
	if err != nil {
		return "", err
	}
	handoff, err := os.ReadFile(doc.Report)
	if err != nil || len(handoff) == 0 || len(handoff) > 1024*1024 {
		return "", errors.New("invalid verified handoff")
	}
	ack := codingIntegrationAck{Schema: "air-worker.coordinator-integration/v1", Status: "ACK_RECORDED",
		Step: step, Principal: scope.ID.Principal, SessionKey: scope.ID.SessionKey,
		SourceHead: doc.SourceHead, TaskSHA256: doc.TaskSHA256, HandoffSHA256: shaCodingBytes(handoff)}
	for i, w := range doc.Workers {
		ack.JobIDs[i], ack.PatchSHA256[i] = w.JobID, w.PatchSHA256
	}
	if ack.JobIDs[0] == "" || ack.JobIDs[1] == "" || ack.JobIDs[0] == ack.JobIDs[1] ||
		ack.PatchSHA256[0] == "" || ack.PatchSHA256[1] == "" {
		return "", errors.New("incomplete verified worker identity")
	}
	bytes, err := json.MarshalIndent(ack, "", "  ")
	if err != nil {
		return "", err
	}
	dir := filepath.Dir(doc.Report)
	if err := jobOwnedEvidencePath(dir, doc.Report); err != nil || filepath.Base(doc.Report) != "handoff.json" {
		return "", errors.New("unsafe verified handoff path")
	}
	owned, err := os.OpenRoot(dir)
	if err != nil {
		return "", err
	}
	defer owned.Close()
	file, err := owned.OpenFile("coordinator-integration.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "coordinator-integration.json")
	if n, err := file.Write(append(bytes, '\n')); err != nil || n != len(bytes)+1 {
		_ = file.Close()
		return "", errors.New("incomplete coordinator acknowledgement write")
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return "", err
	}
	if err := file.Close(); err != nil {
		return "", err
	}
	return path, nil
}

func cmdCodingAckIntegration(argv []string) int {
	fs := flag.NewFlagSet("orchestrate code ack-integration", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	task := fs.String("task-file", "", "original task file")
	step := fs.String("step", "", "original coordinator step")
	principal := fs.String("principal", "", "original principal")
	session := fs.String("session-key", "", "original session")
	state := fs.String("state-dir", "", "native session state")
	asJSON := fs.Bool("json", false, "machine output")
	if fs.Parse(argv) != nil || fs.NArg() != 0 {
		return 2
	}
	result := map[string]string{"status": "NOT_PROVEN"}
	err := func() error {
		root, cfg, err := loadExecutorConfig(*product, "")
		if err != nil {
			return err
		}
		if !cfg.Orchestration.Enabled || !cfg.Orchestration.RequireNativeSubagents ||
			!cfg.Orchestration.RequireAnthropicReviewer || cfg.Orchestration.Subagents != 2 {
			return errors.New("managed two-worker configuration required")
		}
		scope, err := executorScopeForSession(root, *principal, *session, *state)
		if err != nil {
			return err
		}
		taskPath, err := taskFileInsideProduct(root, *task)
		if err != nil {
			return err
		}
		if *step != "coord-"+filepath.Base(taskPath) && *step != "N-139_n136-native-operational-goal-selector-for-real-a" {
			return errors.New("step does not match original task file")
		}
		body, err := os.ReadFile(taskPath)
		if err != nil || len(body) == 0 || len(body) > 48*1024 {
			return errors.New("invalid original task")
		}
		path, err := ackCodingIntegrated(root, scope, *step, string(body))
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var ack codingIntegrationAck
		if err := json.Unmarshal(data, &ack); err != nil {
			return err
		}
		if len(data) == 0 || len(data) > 8192 || jobOwnedEvidencePath(filepath.Dir(path), path) != nil ||
			ack.Schema != "air-worker.coordinator-integration/v1" || ack.Status != "ACK_RECORDED" ||
			ack.Step != *step || ack.Principal != scope.ID.Principal || ack.SessionKey != scope.ID.SessionKey ||
			ack.TaskSHA256 != shaCodingBytes(body) {
			return errors.New("coordinator acknowledgement readback mismatch")
		}
		verified, err := verifyCodingIntegrated(root, scope, *step, string(body))
		if err != nil || verified.SourceHead != ack.SourceHead || len(verified.Workers) != 2 {
			return errors.New("integrated source changed during acknowledgement")
		}
		for i, worker := range verified.Workers {
			if ack.JobIDs[i] != worker.JobID || ack.PatchSHA256[i] != worker.PatchSHA256 {
				return errors.New("worker evidence changed during acknowledgement")
			}
		}
		freshHandoff, err := os.ReadFile(verified.Report)
		if err != nil || shaCodingBytes(freshHandoff) != ack.HandoffSHA256 {
			return errors.New("handoff changed during acknowledgement")
		}
		result = map[string]string{"status": ack.Status, "path": path, "source_head": ack.SourceHead,
			"task_sha256": ack.TaskSHA256, "handoff_sha256": ack.HandoffSHA256, "ack_sha256": shaCodingBytes(data),
			"job_id_1": ack.JobIDs[0], "job_id_2": ack.JobIDs[1],
			"patch_sha256_1": ack.PatchSHA256[0], "patch_sha256_2": ack.PatchSHA256[1]}
		return nil
	}()
	if err != nil {
		result["error"] = err.Error()
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(result)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "NOT_PROVEN: "+err.Error())
	} else {
		fmt.Printf("ACK_RECORDED path=%s source_head=%s task_sha256=%s handoff_sha256=%s ack_sha256=%s job_id_1=%s patch_sha256_1=%s job_id_2=%s patch_sha256_2=%s\n",
			result["path"], result["source_head"], result["task_sha256"], result["handoff_sha256"], result["ack_sha256"],
			result["job_id_1"], result["patch_sha256_1"], result["job_id_2"], result["patch_sha256_2"])
	}
	if err != nil {
		return 2
	}
	return 0
}

func cmdCodingStatus(argv []string) int {
	fs := flag.NewFlagSet("orchestrate code status", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	task := fs.String("task-file", "", "original task file")
	principal := fs.String("principal", "", "original principal")
	session := fs.String("session-key", "", "original session")
	state := fs.String("state-dir", "", "native session state")
	asJSON := fs.Bool("json", false, "machine output")
	if fs.Parse(argv) != nil {
		return 2
	}
	doc := codingWorkersHandoff{Schema: "air-worker.chatgpt-coordinated-codex-workers/v1", Status: "NOT_PROVEN"}
	root, cfg, err := loadExecutorConfig(*product, "")
	if err == nil && (!cfg.Orchestration.Enabled || !cfg.Orchestration.RequireNativeSubagents || !cfg.Orchestration.RequireAnthropicReviewer || cfg.Orchestration.Subagents != 2) {
		err = errors.New("managed two-worker configuration required")
	}
	var scope sessionScope
	if err == nil {
		scope, err = executorScopeForSession(root, *principal, *session, *state)
	}
	var taskPath string
	if err == nil {
		taskPath, err = taskFileInsideProduct(root, *task)
	}
	var taskBody []byte
	if err == nil {
		var f *os.File
		f, err = os.Open(taskPath)
		if err == nil {
			taskBody, err = io.ReadAll(io.LimitReader(f, 48*1024+1))
			_ = f.Close()
			if err == nil && (len(taskBody) == 0 || len(taskBody) > 48*1024) {
				err = errors.New("task must be readable and bounded (1..49152 bytes)")
			}
		}
	}
	if err == nil {
		doc, err = codingStatus(root, scope, "coord-"+filepath.Base(taskPath), string(taskBody))
		if err == nil {
			retry, retryErr := codingStatus(root, scope, "coord-"+filepath.Base(taskPath)+"-retry-1", string(taskBody))
			if retryErr != nil {
				if errors.Is(retryErr, errHashedOwnershipUnknown) {
					retry.Status = "EXECUTION_UNKNOWN"
					doc.Retry = &retry
				} else {
					err = retryErr
				}
			} else if retry.Status != "NOT_FOUND" {
				doc.Retry = &retry
			}
		}
	}
	if err != nil {
		doc.Status = "NOT_PROVEN"
		doc.Error = err.Error()
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(doc)
	} else {
		fmt.Printf("%s source=%s recovery=%s error=%s\n", doc.Status, doc.SourceHead, doc.Report, doc.Error)
		for _, w := range doc.Workers {
			fmt.Printf("worker%d %s job_id=%s receipt=%s patch_sha256=%s\n", w.Index, w.Status, w.JobID, w.Receipt, w.PatchSHA256)
		}
	}
	if err != nil {
		return 2
	}
	return 0
}

// codingEscalation is a readback of one deterministic transition, not a product PASS.
type codingEscalation struct {
	Status     string               `json:"status"`
	TaskSHA256 string               `json:"task_sha256,omitempty"`
	SourceHead string               `json:"source_head,omitempty"`
	Cause      string               `json:"cause,omitempty"`
	FromTier   string               `json:"from_tier,omitempty"`
	ToTier     string               `json:"to_tier,omitempty"`
	Attempt1   codingWorkersHandoff `json:"attempt_1"`
	Attempt2   codingWorkersHandoff `json:"attempt_2"`
	Error      string               `json:"error,omitempty"`
}

// Only a structured terminal error is evidence of a vendor limit; agent prose is not.
func codingVendorLimit(output []byte) bool {
	for _, line := range strings.Split(string(output), "\n") {
		var event struct {
			Type    string `json:"type"`
			Message string `json:"message"`
			Error   struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		if event.Type == "turn.failed" && isVendorLimit(event.Error.Message) {
			return true
		}
		if event.Type == "error" && isVendorLimit(event.Message) {
			return true
		}
	}
	return false
}

func validateCodingEscalation(root string, cfg runConfig, scope sessionScope, step string, body []byte) (codingEscalation, runnerSpec, string, error) {
	return verifyCodingEscalation(root, cfg, scope, step, body, false)
}

// validateHistoricalCodingEscalation verifies evidence only; it does not authorize a launch.
func validateHistoricalCodingEscalation(root string, cfg runConfig, scope sessionScope, step string, body []byte) (codingEscalation, error) {
	trace, _, _, err := verifyCodingEscalation(root, cfg, scope, step, body, true)
	return trace, err
}

func verifyCodingEscalation(root string, cfg runConfig, scope sessionScope, step string, body []byte, historical bool) (codingEscalation, runnerSpec, string, error) {
	trace := codingEscalation{Status: "NOT_PROVEN"}
	parsed, err := parseCodingMicrosteps(string(body))
	if err != nil {
		return trace, runnerSpec{}, "", err
	}
	trace.TaskSHA256 = shaCodingBytes(body)
	original, err := codingStatus(root, scope, step, string(body))
	if err != nil {
		return trace, runnerSpec{}, "", err
	}
	trace.Attempt1, trace.SourceHead = original, original.SourceHead
	if original.TaskSHA256 != trace.TaskSHA256 || original.SourceHead == "" || len(original.Workers) != 2 {
		return trace, runnerSpec{}, "", errors.New("original task or handoff identity not proven")
	}
	head := original.SourceHead
	if historical {
		_, headErr := hex.DecodeString(head)
		if len(head) != 40 || headErr != nil || original.Workers[0].JobID == "" || original.Workers[0].JobID == original.Workers[1].JobID || original.Workers[0].Receipt == original.Workers[1].Receipt || original.Workers[0].Status != jobStatusFailed || original.Workers[1].Status != jobStatusFailed {
			return trace, runnerSpec{}, "", errors.New("original failures are not distinct and sealed")
		}
	} else {
		head, err = gitCodingOutput(root, "rev-parse", "HEAD")
		if err != nil || head != original.SourceHead {
			return trace, runnerSpec{}, "", errors.New("source HEAD changed")
		}
		clean, err := gitCodingOutput(root, "status", "--porcelain", "--", ".")
		if err != nil || clean != "" {
			return trace, runnerSpec{}, "", errors.New("product source is not clean")
		}
	}
	exe, err := resolveRunnerTool("codex")
	if err != nil {
		return trace, runnerSpec{}, "", err
	}
	from := -1
	for i, tier := range cfg.Ladder {
		runner := resolveRunner(cfg, tier)
		if runner.Kind != "codex" || runner.Model != original.Model {
			continue
		}
		matches := true
		for _, w := range original.Workers {
			r, e := readJobReceipt(w.Receipt)
			if e != nil || r.Model != runner.Model || r.Effort != runner.Effort {
				matches = false
			}
		}
		if matches {
			if from != -1 {
				return trace, runnerSpec{}, "", errors.New("ambiguous original tier")
			}
			from = i
		}
	}
	if from < 0 || from+1 >= len(cfg.Ladder) {
		return trace, runnerSpec{}, "", errors.New("next approved tier unavailable")
	}
	trace.FromTier, trace.ToTier = cfg.Ladder[from], cfg.Ladder[from+1]
	if _, err := approvedExecutorTier(cfg, trace.ToTier); err != nil {
		return trace, runnerSpec{}, "", err
	}
	runner := resolveRunner(cfg, trace.FromTier)
	next := resolveRunner(cfg, trace.ToTier)
	if next.Kind != "codex" {
		return trace, runnerSpec{}, "", errors.New("immediate next tier is not Codex")
	}
	for i, w := range original.Workers {
		r, err := readJobReceipt(w.Receipt)
		if err != nil {
			return trace, runnerSpec{}, "", err
		}
		snap, err := inspectOwnedJob(scope, r.Step, r.Operation)
		if err != nil {
			return trace, runnerSpec{}, "", err
		}
		if !strings.HasSuffix(r.Operation, "-task-"+trace.TaskSHA256) || !snap.Terminal || snap.Status != jobStatusFailed || snap.ExitCode == nil || *snap.ExitCode == 0 || !r.ProcessStarted || snap.SourceHead != head || len(snap.ArgsSHA256) != 64 || len(snap.OutputSHA256) != 64 || snap.RecoveryRef != filepath.ToSlash(snap.ReceiptPath)+"#job_id="+snap.JobID {
			return trace, runnerSpec{}, "", errors.New("original terminal failure is not sealed")
		}
		workerRoot := w.Product
		top, topErr := gitCodingOutput(root, "rev-parse", "--show-toplevel")
		rel, relErr := filepath.Rel(top, root)
		expected := filepath.Join(filepath.Dir(original.Report), fmt.Sprintf("worker-%d", i+1), rel)
		real, realErr := filepath.EvalSymlinks(workerRoot)
		workerHead, headErr := gitCodingOutput(workerRoot, "rev-parse", "HEAD")
		if topErr != nil || relErr != nil || realErr != nil || headErr != nil || filepath.Clean(workerRoot) != filepath.Clean(expected) || !strings.EqualFold(filepath.Clean(real), filepath.Clean(expected)) || workerHead != head {
			return trace, runnerSpec{}, "", errors.New("original worker workspace identity mismatch")
		}
		if workerRoot == "" {
			return trace, runnerSpec{}, "", errors.New("original worker workspace missing")
		}
		version := codingReceiptVersion(r.Operation, trace.TaskSHA256, scope.ID.Principal)
		if version == "" || r.StdinSHA256 == "" {
			return trace, runnerSpec{}, "", errors.New("original measured stdin fingerprint mismatch")
		}
		fullTask := codingWorkerTaskVersion(parsed, parsed.Workers[i], trace.TaskSHA256, head, version)
		taskPath := filepath.Join(workerRoot, ".woody", "TASK-CODE-WORKER.md")
		actual, err := os.ReadFile(taskPath)
		if err != nil || string(actual) != fullTask {
			return trace, runnerSpec{}, "", errors.New("original worker task mismatch")
		}
		prompt := codingWorkerPrompt(taskPath, fullTask)
		expectedCmd := codexCommandForSandbox(exe, workerRoot, prompt, runner, "workspace-write")
		if jobCommandFingerprint(expectedCmd) != snap.ArgsSHA256 {
			return trace, runnerSpec{}, "", errors.New("original command fingerprint mismatch")
		}
		expectedStdin := jobStdinFingerprint(expectedCmd)
		if expectedStdin == "" || r.StdinSHA256 == "" || r.StdinSHA256 != expectedStdin {
			return trace, runnerSpec{}, "", errors.New("original measured stdin fingerprint mismatch")
		}
		changes, err := gitCodingOutput(workerRoot, "status", "--porcelain", "--", ".", ":(exclude).woody")
		if err != nil || changes != "" {
			return trace, runnerSpec{}, "", errors.New("original worker made source edits")
		}
		output, err := os.ReadFile(snap.OutputPath)
		if err != nil || !codingVendorLimit(output) {
			return trace, runnerSpec{}, "", errors.New("structured vendor limit not proven")
		}
	}
	trace.Cause = "vendor_limit"
	if historical {
		return trace, runnerSpec{}, "", nil
	}
	retry, err := codingStatus(root, scope, step+"-retry-1", string(body))
	if err != nil {
		return trace, runnerSpec{}, "", err
	}
	trace.Attempt2 = retry
	if retry.Status == "EXECUTION_UNKNOWN" || retry.Status == "RUNNING" || retry.Status == jobStatusFailed {
		return trace, runnerSpec{}, "", errors.New("retry status is unsafe to start new work")
	}
	return trace, next, exe, nil
}

func cmdCodingEscalate(argv []string) int {
	fs := flag.NewFlagSet("orchestrate code escalate", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	task := fs.String("task-file", "", "original task file")
	principal := fs.String("principal", "", "original principal")
	session := fs.String("session-key", "", "original session")
	state := fs.String("state-dir", "", "native session state")
	asJSON := fs.Bool("json", false, "machine output")
	if fs.Parse(argv) != nil || fs.NArg() != 0 {
		return 2
	}
	trace := codingEscalation{Status: "NOT_PROVEN"}
	err := func() error {
		root, cfg, err := loadExecutorConfig(*product, "")
		if err != nil {
			return err
		}
		if !cfg.Orchestration.Enabled || !cfg.Orchestration.RequireNativeSubagents || !cfg.Orchestration.RequireAnthropicReviewer || cfg.Orchestration.Subagents != 2 {
			return errors.New("managed two-worker configuration required")
		}
		if err := requireNativeLoopInvocation(cfg, true, 2); err != nil {
			return err
		}
		scope, err := executorScopeForSession(root, *principal, *session, *state)
		if err != nil {
			return err
		}
		path, err := taskFileInsideProduct(root, *task)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil || len(body) == 0 || len(body) > 48*1024 {
			return errors.New("invalid original task")
		}
		_, err = parseCodingMicrosteps(string(body))
		if err != nil {
			return err
		}
		trace.TaskSHA256 = shaCodingBytes(body)
		step := "coord-" + filepath.Base(path)
		lock, ok := acquireLock(scopedLockName("coding-escalate:"+step, scope))
		if !ok || !validAutoSelfDeliveryLock(lock) {
			if lock != nil {
				lock.release()
			}
			return errors.New("escalation already being inspected or launched")
		}
		defer lock.release()
		var next runnerSpec
		var exe string
		trace, next, exe, err = validateCodingEscalation(root, cfg, scope, step, body)
		if err != nil {
			return err
		}
		if trace.Attempt2.Status != "NOT_FOUND" {
			trace.Status = trace.Attempt2.Status
			return nil
		}
		retryStep := step + "-retry-1"
		ctx := &loopCtx{Root: root, Cfg: cfg, Principal: scope.ID.Principal, SessionKey: scope.ID.SessionKey, Orchestrate: true, Subagents: 2}
		trace.Attempt2 = ctx.runIsolatedCodingWorkersOnce(exe, string(body), retryStep, next)
		trace.Status = trace.Attempt2.Status
		return nil
	}()
	if err != nil {
		trace.Error = err.Error()
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(trace)
	} else {
		fmt.Printf("%s cause=%s from=%s to=%s error=%s\n", trace.Status, trace.Cause, trace.FromTier, trace.ToTier, trace.Error)
	}
	if err != nil || trace.Status == "NOT_PROVEN" {
		return 2
	}
	return 0
}
