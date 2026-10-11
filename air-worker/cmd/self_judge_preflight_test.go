package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestN120TrustedJudgeFailureCauseOnlyFromNativeFilesystem(t *testing.T) {
	root := t.TempDir()
	args := []string{"judge", "-product", root, "-json"}
	_, nativeRC := captureStdout(t, func() int { return cmdJudge(args[1:]) })
	if nativeRC != 2 {
		t.Fatalf("actual native judge missing config must be rc2, got %d", nativeRC)
	}
	if cause := ownCLITrustedFailureCause(args, nativeRC); cause != "judge-run-config-missing" {
		t.Fatalf("native judge missing run-config cause=%q", cause)
	}
	for _, rc := range []int{0, 1} {
		if got := ownCLITrustedFailureCause(args, rc); got != "" {
			t.Fatalf("judge rc%d invented a failure cause: %q", rc, got)
		}
	}
	badArgs := [][]string{
		{"judge", "-product", root, "-config", "foreign-private.json", "-json"},
		{"judge", "-product", root, "-not-native-option"},
		{"judge", "-product", root, "-json", "stray-private-argument"},
		{"judge", "-product", root, "-min-facts", "not-an-integer"},
	}
	for _, argv := range badArgs {
		if cause := ownCLITrustedFailureCause(argv, 2); cause != "" {
			t.Fatalf("unknown or custom native judge argv granted typed cause %q: %q", argv[1:], cause)
		}
	}
	configPath := filepath.Join(root, "run-config.json")
	if err := os.WriteFile(configPath, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	_, nativeRC = captureStdout(t, func() int { return cmdJudge(args[1:]) })
	if nativeRC != 2 || ownCLITrustedFailureCause(args, nativeRC) != "judge-plan-missing" {
		t.Fatalf("judge without PLAN needs native rc2 and exact plan-missing cause: rc=%d cause=%q",
			nativeRC, ownCLITrustedFailureCause(args, nativeRC))
	}
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte("# synthetic file\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ownCLITrustedFailureCause(args, 2); got != "" {
		t.Fatalf("mere judge rc2 with both files present falsely claimed precondition %q", got)
	}
	if err := os.WriteFile(configPath, []byte(`{"plan":"   "}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ownCLITrustedFailureCause(args, 2); got != "" {
		t.Fatalf("whitespace-only noncanonical judge plan should not be trusted: %q", got)
	}
	if err := os.WriteFile(configPath, []byte(`{"plan":"../foreign/PLAN.md"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := ownCLITrustedFailureCause(args, 2); got != "" {
		t.Fatalf("parent-traversing judge plan must fail closed: %q", got)
	}
}

func TestN120RealNativeJudgeFailureEmitsOnePrivateSignedPreflightEvent(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	argv := []string{"judge", "-product", product, "-json"}
	_, nativeRC := captureStdout(t, func() int { return cmdJudge(argv[1:]) })
	if nativeRC != 2 {
		t.Fatalf("native judge missing configuration did not return rc2: %d", nativeRC)
	}
	runID := "AW-CLI-N120-JUDGE-MISSING-001"
	calls := 0
	trigger := func(jobs []sharedDeferredReviewJob) error {
		calls++
		if len(jobs) != 1 || jobs[0].RunID != runID || jobs[0].Product != product {
			t.Fatalf("reviewer dispatched before proper durable native origin: %+v", jobs)
		}
		count, _ := n110CountFailureEvents(t, settings.RuntimeRoot, runID)
		if count != 1 {
			t.Fatalf("reviewer saw %d origin events instead of one", count)
		}
		return nil
	}
	if err := autoCaptureOwnCLIExitWithRunID(argv, nativeRC, runID, trigger); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("new native judge defect should dispatch reviewer once, got %d", calls)
	}
	receiptPath := filepath.Join(settings.RuntimeRoot, "cli-failures", runID+".json")
	raw, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatal(err)
	}
	var receipt ownCLIFailureReceipt
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.CauseCode != "judge-run-config-missing" ||
		receipt.Action != "judge" || receipt.ExitCode != 2 ||
		!strings.Contains(receipt.Observed, "run-config.json is missing") ||
		strings.Contains(receipt.Observed, product) || strings.Contains(string(raw), product) ||
		receipt.ReceiptMAC == "" {
		t.Fatalf("unsafe or unsupported native judge failure receipt: %+v", receipt)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	trusted := ownCLIReceiptMAC(key, receipt)
	for i := range key {
		key[i] = 0
	}
	if receipt.ReceiptMAC != trusted {
		t.Fatal("native judge receipt not signed by own per-user runtime key")
	}
	count, event := n110CountFailureEvents(t, settings.RuntimeRoot, runID)
	if count != 1 || event["class"] != "air-worker-cli-preflight" ||
		event["source"] != "air-worker-cli" || event["observed"] != receipt.Observed ||
		event["outcome"] == "pass" {
		t.Fatalf("not a single truthful new native judge failure event: %+v", event)
	}
	if err := autoCaptureOwnCLIExitWithRunID(argv, nativeRC, runID, trigger); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("idempotent same run relaunched native reviewer")
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("failure capture credited %d unearned procedure uses", got)
	}
	if err := autoCaptureOwnCLIExitWithRunID([]string{"judge", "-product", product, "-json", "-config", "other"}, nativeRC, runID, nil); err == nil {
		t.Fatal("same run with modified private argv reused original signed receipt")
	}
}

func TestN120RealJudgeFailureAutomaticallyReviewsAndAppliesSafeSkill(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	settings.Reviewer = learningAdapterFixture(t, "review-native-preflight")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	selector, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("persistent isolated AirWorker self-owner missing: %v", err)
	}
	selector.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(selector); err != nil {
		t.Fatal(err)
	}
	argv := []string{"judge", "-product", product, "-json"}
	_, actualRC := captureStdout(t, func() int { return cmdJudge(argv[1:]) })
	if actualRC != 2 {
		t.Fatalf("judge must actually fail on missing run-config, got %d", actualRC)
	}
	runID := "AW-CLI-N120-AUTO-REVIEW-001"
	reviews := 0
	startReviewer := func(jobs []sharedDeferredReviewJob) error {
		reviews++
		if len(jobs) != 1 || jobs[0].Product != product || jobs[0].RunID != runID {
			t.Fatalf("automatic review request not bound to native judge defect: %+v", jobs)
		}
		// This deterministic fake production-reviewer process is launched by
		// the native dispatch callback, never via a hand-written LEARN event.
		result, err := executeSharedLearning(product, settings, "review",
			map[string]string{"run_id": runID})
		if err != nil || result.Status != "candidate" {
			t.Fatalf("native dispatch could not safely propose procedure: %+v err=%v", result, err)
		}
		return nil
	}
	if err := autoCaptureOwnCLIExitWithRunID(argv, actualRC, runID, startReviewer); err != nil {
		t.Fatal(err)
	}
	if reviews != 1 {
		t.Fatalf("real judge failure did not launch one reviewer: %d", reviews)
	}
	const target = "skills/learned/air-worker-cli-preflight-n118-test.md"
	file := filepath.Join(product, filepath.FromSlash(target))
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("native module did not auto-apply new safe Markdown lesson: %v", err)
	}
	if string(raw) != n118SafePreflightProcedure {
		t.Fatal("reviewer accepted a changed procedure instead of its safe candidate")
	}
	sha := learnSHA(raw)
	idx, err := executeSharedLearning(product, settings, "index", nil)
	if err != nil || idx.Status != "ok" {
		t.Fatalf("applied module skill index not available: %+v %v", idx, err)
	}
	var cat struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err := json.Unmarshal(idx.Data, &cat); err != nil {
		t.Fatal(err)
	}
	active := 0
	for _, skill := range cat.Skills {
		if skill.Target == target && skill.SHA256 == sha {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("native module has %d active exact learned procedures, expected one", active)
	}
	applied := 0
	err = scanLearnJSONL(filepath.Join(settings.RuntimeRoot, "ledger.jsonl"), func(line []byte) error {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			return err
		}
		if rec["status"] == "applied" && rec["target"] == target &&
			rec["post_sha256"] == sha {
			applied++
		}
		return nil
	})
	if err != nil || applied != 1 {
		t.Fatalf("module did not atomically apply a unique safe procedure: applied=%d err=%v", applied, err)
	}
	events, event := n110CountFailureEvents(t, settings.RuntimeRoot, runID)
	if events != 1 || event["class"] != "air-worker-cli-preflight" ||
		event["source"] != "air-worker-cli" || event["outcome"] == "pass" {
		t.Fatalf("review chain no longer anchored to truthful native judge failure: %+v", event)
	}
	if got := sharedUsageCount(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("applying new procedure was falsely counted as %d uses", got)
	}
	if err := autoCaptureOwnCLIExitWithRunID(argv, actualRC, runID, startReviewer); err != nil {
		t.Fatal(err)
	}
	if reviews != 1 || sharedUsageCount(t, settings.RuntimeRoot, "procedure_used") != 0 {
		t.Fatal("repeated native run dispatched duplicate reviewer or false procedure use")
	}
}

func TestN120JudgeRejectsAncestorJunctionToForeignProduct(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows junction regression")
	}
	base := t.TempDir()
	real := filepath.Join(base, "real-root")
	alias := filepath.Join(base, "alias-root")
	product := filepath.Join(real, "nested-product")
	if err := os.MkdirAll(product, 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := exec.Command("cmd", "/c", "mklink", "/J", alias, real).CombinedOutput()
	if err != nil {
		t.Skipf("junction could not be created in isolated fixture: %v: %s", err, raw)
	}
	viaAlias := filepath.Join(alias, "nested-product")
	info, err := os.Lstat(viaAlias)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("fixture leaf is not an ordinary directory, cannot test parent-only bypass: %v %v", info, err)
	}
	args := []string{"judge", "-product", viaAlias, "-json"}
	if got := ownCLITrustedFailureCause(args, 2); got != "" {
		t.Fatalf("parent junction to foreign product created trusted native judge missing config cause: %q", got)
	}
	// A real root with the same missing run-config remains diagnosable.
	safeArgs := []string{"judge", "-product", product, "-json"}
	if got := ownCLITrustedFailureCause(safeArgs, 2); got != "judge-run-config-missing" {
		t.Fatalf("no alias real native root was refused, cause=%q", got)
	}
}
