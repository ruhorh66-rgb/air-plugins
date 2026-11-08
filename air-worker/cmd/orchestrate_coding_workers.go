package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// codingWorkerEvidence is a durable pointer to ACTUAL source edits in a private
// worktree. It is not a model assertion or an accepted product change.
type codingWorkerEvidence struct {
	Index       int
	Workspace   string
	Product     string
	Patch       string
	PatchSHA256 string
	Receipt     string
	JobID       string
	Model       string
	Status      string
	Changed     []string
	Error       string
}

type codingWorkersHandoff struct {
	Schema        string
	Status        string
	SourceHead    string
	SourceProduct string
	Coordinator   string
	Model         string
	WorkerCount   int
	Workers       []codingWorkerEvidence
	Report        string
	Error         string
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
// controlling ChatGPT session is coordinator; there is NO third Codex leader.
// The original shared repository stays unchanged. Neither source changes nor
// a final judge can be marked PASS until the coordinator inspects/merges them.
func (c *loopCtx) runIsolatedCodingWorkers(exePath, task, step string, runner runnerSpec) codingWorkersHandoff {
	doc := codingWorkersHandoff{Schema: "air-worker.chatgpt-coordinated-codex-workers/v1",
		Status: "NOT_PROVEN", SourceProduct: c.Root, Coordinator: "ChatGPT/current-conversation",
		Model: runner.Model, WorkerCount: 2}
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
			work := codingWorkerEvidence{Index: i, Product: dirs[i-1],
				Workspace: filepath.Dir(dirs[i-1]), Model: runner.Model, Status: "NOT_PROVEN"}
			workFolder := filepath.Join(dirs[i-1], ".woody")
			if err := os.MkdirAll(workFolder, 0700); err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			taskPath := filepath.Join(workFolder, "TASK-CODE-WORKER.md")
			fullTask := fmt.Sprintf("CODING WORKER %d OF 2. You are a CODE WRITER in your OWN isolated Git worktree. You MUST read source files and physically EDIT the assigned source files in this workspace. Do not just return an example diff. Do not modify other workspaces, shared source, PLAN, Git metadata or credentials. Do not commit. The ChatGPT conversation is your coordinator; it will inspect and merge the real Git edits. There is NO third Codex leader.\n\nCOORDINATOR TASK:\n%s\n", i, task)
			if err := os.WriteFile(taskPath, []byte(fullTask), 0600); err != nil {
				work.Error = err.Error()
				ch <- outcome{work}
				return
			}
			prompt := "READ THIS WORKER TASK FILE FIRST: " + taskPath +
				"\nEdit the actual files inside YOUR worktree, run bounded checks if needed, and give a concise report. Your actual Git diff, not your prose, is the evidence."
			cmd := codexCommandForSandbox(exePath, dirs[i-1], prompt, runner, "workspace-write")
			agentStep := fmt.Sprintf("%s-coding-worker-%d", step, i)
			operation := runnerReceiptOperation("executor-codex-subagent", runner, c.iter)
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

// codingStatus reads only the original scope's durable evidence; it never starts a job.
func codingStatus(root string, scope sessionScope, step string) (codingWorkersHandoff, error) {
	doc := codingWorkersHandoff{Schema: "air-worker.chatgpt-coordinated-codex-workers/v1", Status: "NOT_FOUND", SourceProduct: root, WorkerCount: 2}
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
	for i := 1; i <= 2; i++ {
		w := &doc.Workers[i-1]
		workerProduct := filepath.Join(base, fmt.Sprintf("worker-%d", i), rel)
		if real, e := filepath.EvalSymlinks(workerProduct); e == nil && strings.EqualFold(filepath.Clean(real), filepath.Clean(workerProduct)) {
			if head, e := gitCodingOutput(workerProduct, "rev-parse", "HEAD"); e == nil {
				if doc.SourceHead != "" && doc.SourceHead != head {
					return doc, errors.New("worker source mismatch")
				}
				doc.SourceHead = head
			}
		}
		if w.Index != i {
			return doc, errors.New("handoff worker order mismatch")
		}
		workerStep := fmt.Sprintf("%s-coding-worker-%d", step, i)
		prefix := reNonWord.ReplaceAllString(scope.ID.namespace()+"__"+workerStep+"__executor-codex-subagent", "-")
		var snap jobStatusSnapshot
		found := false
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, prefix+"-") || !strings.HasSuffix(name, ".receipt.json") {
				continue
			}
			path := filepath.Join(root, ".woody", "jobs", name)
			if err := jobOwnedEvidencePath(root, path); err != nil {
				return doc, err
			}
			r, err := readJobReceipt(path)
			if err != nil {
				return doc, err
			}
			if r.Step != workerStep || !strings.HasPrefix(r.Operation, "executor-codex-subagent-") {
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
		}
		if !found {
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
		if snap.Terminal && snap.OutputSHA256 == "" {
			w.Status = "EXECUTION_UNKNOWN"
		}
		if doc.SourceHead == "" {
			doc.SourceHead = snap.SourceHead
		} else if snap.SourceHead != "" && doc.SourceHead != snap.SourceHead {
			return doc, errors.New("receipt source mismatch")
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
	}
	return doc, nil
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
	if err == nil {
		doc, err = codingStatus(root, scope, "coord-"+filepath.Base(taskPath))
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
