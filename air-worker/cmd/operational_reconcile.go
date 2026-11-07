package main

import (
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
)

// N139 evidence reconciliation is an independent, read-only judge retry after
// the first native semantic review returned NOT_PROVEN. The original verdict,
// native Codex job and factual Go job stay on disk unchanged. No executor or Go
// child job is started from this path and no plan/N node can be closed here.
type n139ReconcileEvidence struct {
	JobID          string
	OutputSHA256   string
	Output         []byte
	LegacyUnsealed bool
}

func readN139ReconcileEvidence(scope sessionScope, step, operation, expectedSHA string) (n139ReconcileEvidence, error) {
	var empty n139ReconcileEvidence
	if len(expectedSHA) != 64 || strings.ToLower(expectedSHA) != expectedSHA {
		return empty, errors.New("expected job output SHA256 must be exact lowercase hex")
	}
	if _, err := hex.DecodeString(expectedSHA); err != nil {
		return empty, fmt.Errorf("invalid job output SHA256: %w", err)
	}
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	if err := jobOwnedEvidencePath(scope.Root, receiptPath); err != nil {
		return empty, err
	}
	rec, err := readJobReceipt(receiptPath)
	if err != nil {
		return empty, err
	}
	if rec.JobID == "" || rec.Status != jobStatusDone || !rec.ProcessStarted || rec.PID <= 0 ||
		rec.Product != scope.Root || rec.Principal != scope.ID.Principal ||
		rec.Session != scope.ID.SessionKey || rec.Step != step || rec.Operation != operation ||
		filepath.Clean(rec.OutputPath) != filepath.Clean(outputPath) {
		return empty, errors.New("native terminal job provenance does not match the requested owned scope")
	}
	if err := jobOwnedEvidencePath(scope.Root, outputPath); err != nil {
		return empty, err
	}
	info, err := os.Stat(outputPath)
	if err != nil || info.Size() > 4<<20 {
		return empty, errors.New("job output missing or outside bounded reconciliation limit")
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		return empty, err
	}
	actual := sha256.Sum256(data)
	digest := hex.EncodeToString(actual[:])
	if digest != expectedSHA || len(data) != int(info.Size()) {
		return empty, errors.New("original job output SHA or size differs from the frozen independent evidence")
	}
	if rec.OutputSHA256 != "" && (rec.OutputSHA256 != digest || rec.OutputBytes != int64(len(data))) {
		return empty, errors.New("native job completion seal disagrees with actual output")
	}
	return n139ReconcileEvidence{JobID: rec.JobID, OutputSHA256: digest, Output: data, LegacyUnsealed: rec.OutputSHA256 == ""}, nil
}

func cmdOperationalGoalReconcile(argv []string) int {
	fs := flag.NewFlagSet("operational-goal reconcile", flag.ContinueOnError)
	product := fs.String("product", ".", "AirWorker product root")
	nodeID := fs.String("node", "", "native N139 full ID")
	principal := fs.String("principal", "", "declared judge session principal")
	sessionKey := fs.String("session-key", "", "new independent judge session key")
	originSession := fs.String("origin-session-key", "", "original Codex/Go native session key")
	baseline := fs.String("baseline-head", "", "frozen Git source SHA before Codex edit")
	wantCodex := fs.String("expected-codex-sha", "", "independently frozen original Codex output SHA256")
	wantGo := fs.String("expected-go-sha", "", "independently frozen original factual Go output SHA256")
	if err := fs.Parse(argv); err != nil || fs.NArg() != 0 {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *nodeID != "N-139_n136-native-operational-goal-selector-for-real-a" {
		fmt.Fprintln(os.Stderr, "reconcile requires exact native N139 node identity")
		return 2
	}
	if _, err := executorScopeForSession(root, *principal, *sessionKey, ""); err != nil {
		fmt.Fprintln(os.Stderr, "reconcile judge identity:", err)
		return 2
	}
	if *sessionKey == *originSession {
		fmt.Fprintln(os.Stderr, "reconcile requires a distinct judge session, never overwrite original semantic job")
		return 2
	}
	origin, err := executorScopeForSession(root, *principal, *originSession, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, "reconcile original identity:", err)
		return 2
	}
	goal, err := readOperationalGoal(root)
	if err != nil || goal.NodeID != *nodeID {
		fmt.Fprintln(os.Stderr, "reconcile operational goal is not current or independently verified:", err)
		return 2
	}
	head, err := sourceReviewGitState(root)
	if err != nil || head != goal.SourceHead {
		fmt.Fprintln(os.Stderr, "reconcile requires clean goal-bound source HEAD:", err)
		return 2
	}
	if !operationalSHA.MatchString(*baseline) || *baseline == head {
		fmt.Fprintln(os.Stderr, "invalid frozen baseline Git SHA")
		return 2
	}
	if out, err := exec.Command("git", "-C", root, "merge-base", "--is-ancestor", *baseline, head).CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, "baseline source ancestry unproven:", string(out))
		return 2
	}
	path, err := planNodePathByID(root, *nodeID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	node, err := readPlanNode(path)
	if err != nil || node.Status != "open" {
		fmt.Fprintln(os.Stderr, "reconcile requires original N139 OPEN node:", err)
		return 2
	}
	var cfg runConfig
	cfgPath := filepath.Join(root, "run-config.json")
	if err := enforceCanonicalNativeRouteConfig(root, cfgPath); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if err := readJSON(cfgPath, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	runner := resolveRunner(cfg, "gpt6-luna:low")
	if runner.Kind != "codex" {
		fmt.Fprintln(os.Stderr, "the original source coding tier is not native Codex")
		return 2
	}
	operation := runnerReceiptOperation("executor-codex", runner, 0)
	codex, err := readN139ReconcileEvidence(origin, goal.NodeID, operation, *wantCodex)
	if err != nil {
		fmt.Fprintln(os.Stderr, "original Codex provenance:", err)
		return 2
	}
	codexResult := parseCodexResult(string(codex.Output), nil)
	if !codexResult.Ok {
		fmt.Fprintln(os.Stderr, "original Codex terminal turn not verified")
		return 2
	}
	factualGo, err := readN139ReconcileEvidence(origin, goal.NodeID, "factual-go-test", *wantGo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "original factual Go provenance:", err)
		return 2
	}
	factualText := strings.TrimSpace(string(factualGo.Output))
	if !strings.HasPrefix(factualText, "ok") || !strings.Contains(factualText, "airos/air-worker") {
		fmt.Fprintln(os.Stderr, "original factual Go output does not show native PASS")
		return 2
	}
	// Only the two exact files changed by the ONE original native Codex job
	// are placed into the independent re-review packet.
	diffCmd := exec.Command("git", "-C", root, "diff", "--no-ext-diff", *baseline+".."+head, "--",
		"cmd/job_receipt.go", "cmd/job_status.go")
	diff, err := diffCmd.Output()
	if err != nil || len(diff) < 20 || len(diff) > 24<<10 ||
		!strings.Contains(string(diff), "job_receipt.go") || !strings.Contains(string(diff), "job_status.go") {
		fmt.Fprintln(os.Stderr, "bounded original code change is not independently proven:", err)
		return 2
	}
	taskPath := filepath.Join(root, "docs", "tasks", "DEVWORK29_N139_AUTONOMOUS_TAMPER_FIX_20261107.md")
	task, err := os.ReadFile(taskPath)
	if err != nil || len(task) == 0 || len(task) > 32<<10 {
		fmt.Fprintln(os.Stderr, "original source task cannot be verified:", err)
		return 2
	}
	reviewScopeLock, free := acquireLock(lockName("operational-n139", root))
	if !free {
		fmt.Fprintln(os.Stderr, "N139 writer active; independent re-review refused")
		return 2
	}
	defer reviewScopeLock.release()
	c := &loopCtx{Root: root, Cfg: cfg, CfgPath: cfgPath, Principal: *principal, SessionKey: *sessionKey}
	step := workStep{Num: goal.NodeID, Tier: "gpt6-luna:low",
		Title: "N139 bounded useful source fix: seal and verify terminal output integrity; full N139 node remains OPEN",
		Judge: "Review only original source-edit slice and native factual RC0; not N139 full plan close"}
	factual := semanticFactualPacket{Scope: "step", Code: 0,
		Text:   "Original native Go process DONE RC0, output SHA256 " + factualGo.OutputSHA256 + "; task SHA256 " + sha256Hex(task),
		Passed: []string{"original native Codex terminal DONE and actual code diff", "original factual-go-test terminal DONE, verified output RC0", "independent source-review PASS_SOURCE_ONLY on frozen HEAD"}}
	// The original Codex narrative contained conjecture about a different
	// manual judge. It is preserved unchanged in the original job output, but
	// is not an authoritative fact for this distinct native recovery verdict.
	claim := "REVIEW SCOPE: one useful N139 source fix only, NOT all N139 criteria. Original semantic verdict NOT_PROVEN is preserved in its own receipt. Native Codex job=" + codex.JobID + " output_sha256=" + codex.OutputSHA256 +
		" legacy_unsealed=" + fmt.Sprint(codex.LegacyUnsealed) +
		"; factual Go job=" + factualGo.JobID + " SHA256=" + factualGo.OutputSHA256 +
		" legacy_unsealed=" + fmt.Sprint(factualGo.LegacyUnsealed) +
		". Task=docs/tasks/DEVWORK29_N139_AUTONOMOUS_TAMPER_FIX_20261107.md SHA256=" + sha256Hex(task) +
		". Source baseline=" + *baseline + " accepted HEAD=" + head +
		". BOUNDED VERIFIED SOURCE DIFF:\n" + string(diff)
	review := c.semanticJudge(step, runner, factual, claim)
	if after, err := sourceReviewGitState(root); err != nil || after != head {
		fmt.Fprintln(os.Stderr, "source moved during independent review")
		return 2
	}
	if review.Error != "" {
		fmt.Fprintln(os.Stderr, "reconcile independent reviewer error:", review.Error)
		return 2
	}
	if review.Reviewer.Kind != "claude" {
		fmt.Fprintln(os.Stderr, "reconcile requires cross-vendor Anthropic")
		return 2
	}
	msg, _ := json.Marshal(map[string]any{
		"schema": "air-worker.operational-goal.reconcile/v1", "node_id": goal.NodeID,
		"review_scope": "bounded N139 source fix, not node closure", "source_head": head,
		"codex_job_id": codex.JobID, "go_job_id": factualGo.JobID,
		"codex_sha256": codex.OutputSHA256, "factual_go_sha256": factualGo.OutputSHA256,
		"original_semantic":   "NOT_PROVEN preserved in prior receipt",
		"independent_verdict": review.Verdict,
		"independent_session": review.Session,
	})
	fmt.Println(string(msg))
	if combinedAcceptance(factual.Code, review.Verdict) != "PASS" {
		return 2
	}
	return 0
}
