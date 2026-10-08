package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

const selfLearningProcedure = "# Self evidence reconciliation\n\n## When to apply\nWhen an AirWorker operation has an ambiguous execution result.\n\n## Procedure\n1. Reconcile process, filesystem, and receipt evidence before repeating the operation.\n\n## Pitfalls\nDo not convert an unknown execution state into a failure assumption.\n"

const productLearningProcedure = "# Product-specific check\n\n## When to apply\nWhen the target product needs its own procedure.\n\n## Procedure\n1. Inspect the target product evidence independently from AirWorker self-learning.\n\n## Pitfalls\nDo not merge product and AirWorker learning owners.\n"

func selfLearningFixture(t *testing.T) (product string, settings sharedLearningSettings, stateDir string) {
	t.Helper()
	root := t.TempDir()
	product = filepath.Join(root, "air-worker")
	if err := os.MkdirAll(product, 0o700); err != nil {
		t.Fatal(err)
	}
	stateDir = filepath.Join(root, "state")
	t.Setenv(hookStateDirEnv, stateDir)
	runtime := filepath.Join(root, "runtime")
	if code := cmdLearnSelf([]string{"enable", "-product", product, "-runtime-root", runtime, "-product-id", "air-worker"}); code != 0 {
		t.Fatalf("self enable code=%d", code)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		t.Fatalf("self owner not active: on=%v err=%v", on, err)
	}
	// Production binds Reviewer to @self. Under go test that would recursively launch
	// the test executable, so the fixture swaps only the reviewer adapter for the
	// existing bounded helper and refreshes the selector's exact config SHA.
	t.Setenv("AW_LEARNING_ADAPTER_HELPER", "1")
	settings = owner.Settings
	settings.Reviewer = learningAdapterFixture(t, "review")
	if err := writeSharedLearningSettings(product, settings); err != nil {
		t.Fatal(err)
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatalf("selector missing after enable: found=%v err=%v", found, err)
	}
	sel.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	if _, err := executeSharedLearning(product, settings, "propose", map[string]string{
		"proposal_id": "LP-self-fixture",
		"kind":        "procedure",
		"target":      "skills/learned/execution-unknown-no-blind-retry-6b72186f.md",
		"pre_sha256":  "",
		"content":     selfLearningProcedure,
	}); err != nil {
		t.Fatal(err)
	}
	return product, settings, stateDir
}

func declareLearningTestSession(t *testing.T, stateDir, principal, session, product string) hookInput {
	t.Helper()
	t.Setenv(hookStateDirEnv, stateDir)
	id, err := parseIdentity(principal, session)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeStateJSON(sessionModePath(stateDir, id), sessionModeState{
		Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey,
	}); err != nil {
		t.Fatal(err)
	}
	if product != "" {
		if err := writeStateJSON(sessionProductPath(stateDir, id), sessionProductState{
			Path: product, Principal: id.Principal, SessionKey: id.SessionKey,
		}); err != nil {
			t.Fatal(err)
		}
	}
	return hookInput{Principal: principal, SessionID: session, RunID: "run-" + session}
}

func sharedUsageCount(t *testing.T, runtimeRoot, kind string) int {
	t.Helper()
	count := 0
	err := scanLearnJSONL(filepath.Join(runtimeRoot, "events.jsonl"), func(raw []byte) error {
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return err
		}
		if row["kind"] == kind {
			count++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return count
}

func TestAirWorkerSelfLearningPersistsOutsideTargetProduct(t *testing.T) {
	selfProduct, selfSettings, stateDir := selfLearningFixture(t)
	target := filepath.Join(t.TempDir(), "target-product")
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "self-independent", target)
	res, err := handleLearningContext(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Context, "AIRWORKER SELF-LEARNING") || !strings.Contains(res.Context, "Self evidence reconciliation") {
		t.Fatalf("self context missing while target has no selector: %s", res.Context)
	}
	if strings.Contains(res.Context, target) {
		t.Fatalf("target path leaked into self learning context: %s", res.Context)
	}
	if got := sharedUsageCount(t, selfSettings.RuntimeRoot, "skill_loaded"); got != 1 {
		t.Fatalf("self skill_loaded count=%d", got)
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(owner.Selector.ProductRoot, selfProduct) {
		t.Fatalf("persistent owner lost: on=%v owner=%+v err=%v", on, owner.Selector, err)
	}
}

func TestAirWorkerSelfLearningDualContextAndDeduplicatesOwner(t *testing.T) {
	selfProduct, selfSettings, stateDir := selfLearningFixture(t)
	targetProduct, targetSettings := sharedProductFixture(t, false)
	if _, err := executeSharedLearning(targetProduct, targetSettings, "propose", map[string]string{
		"proposal_id": "LP-target-fixture",
		"kind":        "procedure",
		"target":      "skills/learned/target-fixture.md",
		"pre_sha256":  "",
		"content":     productLearningProcedure,
	}); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "dual-owner", targetProduct)
	res, err := handleLearningContext(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Self evidence reconciliation", "Product-specific check"} {
		if !strings.Contains(res.Context, want) {
			t.Fatalf("dual context missing %q: %s", want, res.Context)
		}
	}
	if got := sharedUsageCount(t, selfSettings.RuntimeRoot, "skill_loaded"); got != 1 {
		t.Fatalf("self loaded=%d", got)
	}
	if got := sharedUsageCount(t, targetSettings.RuntimeRoot, "skill_loaded"); got != 1 {
		t.Fatalf("target loaded=%d", got)
	}

	before := sharedUsageCount(t, selfSettings.RuntimeRoot, "skill_loaded")
	same := declareLearningTestSession(t, stateDir, "claude", "dedupe-owner", selfProduct)
	if _, err := handleLearningContext(same); err != nil {
		t.Fatal(err)
	}
	after := sharedUsageCount(t, selfSettings.RuntimeRoot, "skill_loaded")
	if after != before+1 {
		t.Fatalf("same owner was not deduplicated: before=%d after=%d", before, after)
	}
}

func TestAirWorkerSelfLearningSelectorFailsClosedOnDriftAndRollbackDisables(t *testing.T) {
	selfProduct, _, stateDir := selfLearningFixture(t)
	config := filepath.Join(selfProduct, sharedLearningConfigFile)
	raw, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if json.Unmarshal(raw, &settings) != nil {
		t.Fatal("fixture config did not decode")
	}
	settings["timeout_ms"] = float64(12345)
	changed, _ := json.MarshalIndent(settings, "", "  ")
	if err := os.WriteFile(config, append(changed, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "config-drift", filepath.Join(t.TempDir(), "unrelated"))
	if _, err := handleLearningContext(in); err == nil {
		t.Fatal("configuration SHA drift silently disabled AirWorker self-learning")
	} else {
		var sharedErr *sharedLearningHookError
		if !errors.As(err, &sharedErr) || sharedErr.Phase != "self-selector" {
			t.Fatalf("wrong fail-closed error: %v", err)
		}
	}

	// Rollback is deliberately non-destructive: it disables host selection without
	// deleting the shared config/runtime, so 0.11.8 behavior can be restored.
	if code := cmdLearnSelf([]string{"rollback"}); code != 0 {
		t.Fatalf("rollback code=%d", code)
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found || sel.Enabled || sel.DisableCause != "rollback-to-0.11.8-behavior" {
		t.Fatalf("rollback selector=%+v found=%v err=%v", sel, found, err)
	}
	if _, err := os.Stat(config); err != nil {
		t.Fatalf("rollback removed shared config: %v", err)
	}
	if _, on, err := activeAirWorkerSelfLearningOwner(); err != nil || on {
		t.Fatalf("rollback did not disable owner: on=%v err=%v", on, err)
	}
}

func TestAirWorkerSelfLearningMalformedSelectorIsNotTreatedAsAbsent(t *testing.T) {
	stateDir := t.TempDir()
	t.Setenv(hookStateDirEnv, stateDir)
	if err := os.WriteFile(filepath.Join(stateDir, airWorkerSelfLearningFile), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := declareLearningTestSession(t, stateDir, "claude", "malformed-selector", filepath.Join(t.TempDir(), "target"))
	if _, err := handleLearningContext(in); err == nil {
		t.Fatal("malformed self-learning selector was treated as absent")
	}
}

func TestAirWorkerSelfLearningSelectorRequiresUniqueExplicitEnabled(t *testing.T) {
	base := airWorkerSelfLearningSelector{
		Schema: airWorkerSelfLearningSchema, Enabled: true,
		ProductRoot: t.TempDir(), ProductID: "air-worker", RuntimeRoot: t.TempDir(),
		ConfigSHA256: strings.Repeat("a", 64), UpdatedAt: "2026-10-08T00:00:00Z",
	}
	raw, err := json.MarshalIndent(base, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	delete(doc, "enabled")
	missing, _ := json.Marshal(doc)
	nullEnabled := []byte(strings.Replace(string(raw), `"enabled": true`, `"enabled": null`, 1))
	duplicate := []byte(strings.Replace(string(raw), `"enabled": true`, `"enabled": true, "enabled": false`, 1))
	caseAlias := []byte(strings.Replace(string(raw), `"enabled": true`, `"enabled": true, "Enabled": false`, 1))
	caseOnly := []byte(strings.Replace(string(raw), `"enabled": true`, `"Enabled": false`, 1))
	rootAlias := []byte(strings.Replace(string(raw), `"product_root":`, `"Product_root":`, 1))
	for name, candidate := range map[string][]byte{
		"missing": missing, "null": nullEnabled, "duplicate": duplicate,
		"case_alias_enabled": caseAlias, "case_only_enabled": caseOnly, "noncanonical_root": rootAlias,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeAirWorkerSelfLearningSelector(candidate); err == nil {
				t.Fatalf("%s enabled selector was accepted: %s", name, candidate)
			}
		})
	}
	if got, err := decodeAirWorkerSelfLearningSelector(raw); err != nil || !got.Enabled {
		t.Fatalf("valid explicit enabled selector rejected: %+v %v", got, err)
	}
}

func TestAirWorkerSelfLearningStopRunsBesideTargetOwner(t *testing.T) {
	selfProduct, selfSettings, stateDir := selfLearningFixture(t)
	targetProduct, targetSettings := sharedProductFixture(t, false)
	in := declareLearningTestSession(t, stateDir, "gpt", "dual-stop", targetProduct)
	in.RunID = "dual-stop-run"
	in.LastAssistantMessage = "completed after artifact reconciliation"
	var started []string
	starter := func(product, runID string) error {
		// Both owners must be durable before the first review starts.
		for _, runtime := range []string{selfSettings.RuntimeRoot, targetSettings.RuntimeRoot} {
			if got := sharedUsageCount(t, runtime, "run_completed"); got != 1 {
				t.Fatalf("launched review before both completions: %s count=%d", runtime, got)
			}
		}
		if runID != in.RunID {
			t.Fatalf("review run_id=%q want=%q", runID, in.RunID)
		}
		started = append(started, product)
		return nil
	}
	if _, err := handleStopLearningWithReviewStarter(in, starter); err != nil {
		t.Fatal(err)
	}
	if len(started) != 2 || started[0] != selfProduct || started[1] != targetProduct {
		t.Fatalf("review owners=%v; want self then target", started)
	}
	for name, runtime := range map[string]string{"self": selfSettings.RuntimeRoot, "target": targetSettings.RuntimeRoot} {
		rows := 0
		err := scanLearnJSONL(filepath.Join(runtime, "events.jsonl"), func(raw []byte) error {
			var row map[string]any
			if err := json.Unmarshal(raw, &row); err != nil {
				return err
			}
			if row["kind"] == "run_completed" && row["run_id"] == "dual-stop-run" {
				rows++
			}
			return nil
		})
		if err != nil || rows != 1 {
			raw, _ := os.ReadFile(filepath.Join(runtime, "events.jsonl"))
			t.Fatalf("%s run_completed rows=%d err=%v events=%s", name, rows, err, raw)
		}
	}
	if sameLearningPath(selfProduct, targetProduct) {
		t.Fatal("fixture roots unexpectedly identical")
	}
}

func TestAirWorkerSelfLearningBlockingProposalStaysPendingLPR(t *testing.T) {
	selfProduct, settings, _ := selfLearningFixture(t)
	target := "rules/self-learning-block.json"
	res, err := executeSharedLearning(selfProduct, settings, "propose", map[string]string{
		"proposal_id": "LP-self-blocking",
		"kind":        "check_spec",
		"target":      target,
		"pre_sha256":  "",
		"content":     `{"action":"delete","context":"protected"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "PENDING_LPR" {
		t.Fatalf("blocking proposal status=%q", res.Status)
	}
	if _, err := os.Stat(filepath.Join(selfProduct, filepath.FromSlash(target))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocking proposal changed target before LPR grant: %v", err)
	}
	if _, err := executeSharedLearning(selfProduct, settings, "apply", map[string]string{"proposal_id": "LP-self-blocking"}); err == nil {
		t.Fatal("blocking proposal applied without trusted LPR grant")
	}
}

func TestAirWorkerSelfLearningSelectorSurvivesFreshProcessAndCacheRefresh(t *testing.T) {
	_, _, stateDir := selfLearningFixture(t)
	cmd := exec.Command(os.Args[0], "-test.run=^TestAirWorkerSelfLearningFreshProcessHelper$")
	cmd.Env = append(os.Environ(),
		"AW_SELF_LEARNING_FRESH_PROCESS=1",
		hookStateDirEnv+"="+stateDir,
		"AIR_WORKER_HOME="+filepath.Join(t.TempDir(), "different-cache-home"),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fresh process status failed: %v %s", err, out)
	}
	text := string(out)
	if !strings.Contains(text, `"status":"enabled"`) || !strings.Contains(text, `"configured":true`) {
		t.Fatalf("fresh process did not resolve persistent selector: %s", text)
	}
}

func TestAirWorkerSelfLearningFreshProcessHelper(t *testing.T) {
	if os.Getenv("AW_SELF_LEARNING_FRESH_PROCESS") != "1" {
		return
	}
	if code := cmdLearnSelf([]string{"status", "-json"}); code != 0 {
		t.Fatalf("self status code=%d", code)
	}
}

var _ = learning.Version
