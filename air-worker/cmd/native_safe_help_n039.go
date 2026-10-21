package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// N-039 deliberately authorizes ONE real, independently reviewed safe
// Markdown procedure. The code, not a reviewer, binds it to this read-only
// native action. There is no general "claim use" interface.
const (
	n039Action      = "air-worker.cli.plan-help/v1"
	n039Target      = "skills/learned/feedback-error-a0309df4-b8d17b719c88287a.md"
	n039SHA         = "b8d17b719c88287a818d5fe0da6e7478036a583569e9c455506288ed3957e3c0"
	n039OriginRun   = "AW-FB-PLANRELEASE-HELP-20261020-01"
	n039OriginEvent = "EV-a683ddb01530f642f32dd217"
	n039Proposal    = "LP-EV-a683ddb01530f642f32dd217"
	n039Observed    = "Compiled exact plan help completed successfully in a separate process. Effect: safe local syntax discovery. This does not assert a saved paid model call or repair of the original parser defect."
)

var n039ID = regexp.MustCompile("^[A-Za-z0-9][A-Za-z0-9_-]{5,79}$")

// Injection exists solely for finite process tests. The production boundary
// always invokes THIS executable with the literal command "plan help".
var n039HelpRunner = n039BoundedHelp

type n039BoundedBuffer struct {
	bytes.Buffer
	max int
}

func (b *n039BoundedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		return 0, errors.New("native help output exceeded bound")
	}
	return b.Buffer.Write(p)
}

func n039BoundedHelp(ctx context.Context) ([]byte, error) {
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, exe, "plan", "help")
	child.Stdin = nil
	stdout := &n039BoundedBuffer{max: 4096}
	stderr := &n039BoundedBuffer{max: 4096}
	child.Stdout, child.Stderr = stdout, stderr
	if err := child.Run(); err != nil {
		return nil, fmt.Errorf("compiled plan help failed closed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := stdout.Bytes()
	if stderr.Len() != 0 || !bytes.HasPrefix(out, []byte("Usage: air-worker plan ")) ||
		len(out) > 4096 {
		return nil, errors.New("native help response is not the supported read-only result")
	}
	return append([]byte(nil), out...), nil
}

func n039RuleIdentity(owner selfLearningOwner) (selfLearningLoadedRule, string, error) {
	rule := selfLearningLoadedRule{Target: n039Target, SHA256: n039SHA}
	if owner.Settings.ProductID != "air-worker" || !currentSelfLearningRuleBytes(owner, rule) {
		return rule, "", errors.New("N039 target@SHA is not current and exact")
	}
	content, err := readSharedContextSkill(owner.Selector.ProductRoot, rule.Target, 64*1024)
	if err != nil {
		return rule, "", err
	}
	if learnSHA(content) != rule.SHA256 || !validLearningProcedureMarkdown(string(content)) ||
		!strings.Contains(string(content), "Verify that help or unsupported-command requests complete locally") ||
		strings.Contains(string(content), "Machine-verifiable action") {
		return rule, "", errors.New("N039 safe-help candidate has an unsupported action or different reviewed content")
	}
	index, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "index", nil)
	if err != nil {
		return rule, "", err
	}
	var catalog struct{ Skills []sharedLearningCatalogSkill }
	if err := json.Unmarshal(index.Data, &catalog); err != nil {
		return rule, "", err
	}
	active := 0
	for _, x := range catalog.Skills {
		if x.Target == rule.Target && x.SHA256 == rule.SHA256 && x.SkillID == n039Proposal {
			active++
		}
	}
	if active != 1 {
		return rule, "", errors.New("N039 has no unique active module index entry")
	}
	ledgers := 0
	if err := scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "ledger.jsonl"), func(b []byte) error {
		var x map[string]any
		if err := json.Unmarshal(b, &x); err != nil {
			return err
		}
		if x["schema"] == "air.learning.ledger/v1" && x["proposal_id"] == n039Proposal &&
			x["target"] == rule.Target && x["post_sha256"] == rule.SHA256 && x["status"] == "applied" {
			ledgers++
		}
		return nil
	}); err != nil {
		return rule, "", err
	}
	if ledgers != 1 {
		return rule, "", errors.New("N039 missing unique reviewed applied ledger")
	}
	reviewPath := filepath.Join(owner.Settings.RuntimeRoot, "reviews", n039OriginEvent+".json")
	// Only the exact reviewed candidate bytes match the applied target.
	var rv map[string]any
	if err := readJSON(reviewPath, &rv); err != nil {
		return rule, "", err
	}
	candidate, ok := rv["candidate"].(map[string]any)
	if !ok || rv["schema"] != "air.learning.review/v1" ||
		rv["event_id"] != n039OriginEvent || rv["phase"] != "complete" ||
		rv["outcome"] != "candidate" || rv["proposal_id"] != n039Proposal ||
		candidate["kind"] != "procedure" || candidate["target"] != rule.Target ||
		candidate["content"] != string(content) {
		return rule, "", errors.New("N039 applied bytes are not the exact real auto-review candidate")
	}
	principal := ""
	matches := 0
	err = scanLearnJSONL(filepath.Join(owner.Settings.RuntimeRoot, "events.jsonl"), func(b []byte) error {
		var ev map[string]any
		if err := json.Unmarshal(b, &ev); err != nil {
			return err
		}
		// Verdict rows reference the same origin event_id; only genuine
		// run_completed events are eligible for origin identity matching.
		if ev["schema"] != "air.learning.event/v1" || ev["event_id"] != n039OriginEvent {
			return nil
		}
		if ev["kind"] != "run_completed" ||
			ev["run_id"] != n039OriginRun || ev["class"] != "feedback-error" ||
			ev["source"] != "feedback" || ev["product_id"] != owner.Settings.ProductID {
			return errors.New("N039 origin does not match the real feedback opportunity")
		}
		observed, ok := ev["observed"].(string)
		if !ok || !strings.Contains(observed, "air-worker plan release -help") ||
			!strings.Contains(observed, "gpt-5.6-sol") {
			return errors.New("N039 origin lacks the recorded unsafe help opportunity")
		}
		principal, _ = ev["principal"].(string)
		matches++
		return nil
	})
	if err != nil {
		return rule, "", err
	}
	if matches != 1 || principal == "" {
		return rule, "", errors.New("N039 origin not uniquely authenticated by the native journal")
	}
	return rule, principal, nil
}

func n039MAC(key []byte, fields map[string]string) string {
	copyFields := make(map[string]string, len(fields))
	for k, v := range fields {
		if k != "mac" {
			copyFields[k] = v
		}
	}
	raw, _ := json.Marshal(copyFields)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte("air-worker.n039.safe-help-result/v1\x00"))
	_, _ = mac.Write(raw)
	return hex.EncodeToString(mac.Sum(nil))
}

func n039ReceiptPath(owner selfLearningOwner, runID string) string {
	return filepath.Join(owner.Settings.RuntimeRoot, "n039-safe-help", runID+".json")
}

func n039Attest(ctx context.Context, owner selfLearningOwner, receipt map[string]string, payload json.RawMessage) (json.RawMessage, error) {
	var p nativeN118VerifyPacket
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, err
	}
	if p.Schema != "air.learning.verify-native-outcome/v1" ||
		p.ProductID != owner.Settings.ProductID || p.RunID != receipt["used_run"] ||
		p.OriginalRunID != n039OriginRun || p.OriginEventID != n039OriginEvent ||
		p.ProposalID != n039Proposal || p.Target != n039Target || p.SHA256 != n039SHA ||
		p.RuleID != n039Target+"@"+n039SHA || p.RequiredOutcome != "pass" ||
		p.Source != "native-plan-help" || p.Class != "native-plan-help-use" ||
		p.Principal != receipt["principal"] || p.Session != n039OriginRun ||
		p.ObservedSHA256 != learnSHA([]byte(receipt["observed"])) {
		return nil, errors.New("N039 module attestation requested for a different event")
	}
	ownerNow, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on || !sameLearningPath(owner.Selector.ProductRoot, ownerNow.Selector.ProductRoot) ||
		!sameLearningPath(owner.Settings.RuntimeRoot, ownerNow.Settings.RuntimeRoot) ||
		owner.Selector.ConfigSHA256 != ownerNow.Selector.ConfigSHA256 {
		return nil, errors.New("N039 self-learning owner changed")
	}
	if !currentSelfLearningRuleBytes(ownerNow, selfLearningLoadedRule{Target: n039Target, SHA256: n039SHA}) {
		return nil, errors.New("N039 reviewed rule changed before native attestation")
	}
	path := n039ReceiptPath(owner, receipt["run_id"])
	data, err := readLearningBounded(path, 64*1024)
	if err != nil {
		return nil, err
	}
	if p.OutcomeRef != filepath.ToSlash(path)+"#sha256="+learnSHA(data) {
		return nil, errors.New("N039 outcome ref is stale")
	}
	var stored map[string]string
	if err = json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return nil, err
	}
	signed := n039MAC(key, stored)
	for i := range key {
		key[i] = 0
	}
	if !hmac.Equal([]byte(stored["mac"]), []byte(signed)) ||
		len(stored) != len(receipt) {
		return nil, errors.New("N039 signed readback differs")
	}
	for k, v := range receipt {
		if stored[k] != v {
			return nil, errors.New("N039 signed result mutated: " + k)
		}
	}
	if stored["status"] != "native_safe_help_pass" || stored["exit_code"] != "0" ||
		stored["argv"] != "plan help" || stored["action"] != n039Action {
		return nil, errors.New("N039 invalid typed action")
	}
	if opportunityID := stored["opportunity_id"]; opportunityID != "" {
		prior, priorSHA, verifyErr := n040ReadOpportunityForUse(ownerNow, opportunityID)
		if verifyErr != nil || priorSHA != stored["opportunity_sha256"] ||
			prior["binary_sha256"] != stored["binary_sha256"] ||
			prior["process_instance_id"] == stored["current_process_instance_id"] ||
			prior["current_actor_sid"] != stored["current_actor_sid"] ||
			stored["original_feedback_principal"] != p.Principal ||
			stored["observed"] != n040ObservedUse(opportunityID, priorSHA) {
			return nil, errors.New("N040 signed opportunity differs in native actor or process generation")
		}
		currentSID, actorErr := n040CurrentActorSID()
		currentInstance, instanceErr := n040CurrentProcessInstance()
		if actorErr != nil || instanceErr != nil ||
			stored["current_actor_sid"] != currentSID ||
			stored["current_process_instance_id"] != currentInstance {
			return nil, errors.New("N040 purported current actor/process differs from this running compiled verifier")
		}
		attemptAt, timeErr := time.Parse(time.RFC3339Nano, prior["observed_at_utc"])
		resultAt, resultErr := time.Parse(time.RFC3339Nano, stored["completed_at"])
		if timeErr != nil || resultErr != nil || !resultAt.After(attemptAt) {
			return nil, errors.New("N040 later help result does not follow the signed earlier syntax attempt")
		}
	} else if stored["observed"] != n039Observed {
		return nil, errors.New("N039 ordinary source-only help changed its measured observation")
	}
	// A new actual compiled execution, not a witness file, independently
	// rechecks what the native CLI does under the module mutation lock.
	output, err := n039HelpRunner(ctx)
	if err != nil || learnSHA(output) != stored["stdout_sha256"] {
		return nil, errors.New("N039 independent bounded native help check failed")
	}
	return json.Marshal(map[string]any{
		"schema":     "air.learning.native-outcome-attestation/v1",
		"product_id": p.ProductID, "run_id": p.RunID,
		"origin_event_id": p.OriginEventID, "proposal_id": p.ProposalID,
		"target": p.Target, "sha256": p.SHA256, "outcome_ref": p.OutcomeRef,
		"source": p.Source, "class": p.Class, "principal": p.Principal,
		"session": p.Session, "observed_sha256": p.ObservedSHA256,
		"native_validation_executed": true, "validation_exit_code": 0,
		"outcome": "pass", "check_ref": "compiled:" + n039Action + ":" + stored["binary_sha256"],
	})
}

func nativeN039SafeHelp(owner selfLearningOwner, runID string) (map[string]any, error) {
	return nativeN039SafeHelpWithOpportunity(owner, runID, "")
}

// N040 requires an additional different-process HMAC-signed native syntax
// attempt before using the already reviewed exact N039 safe-help procedure.
func nativeN039SafeHelpWithOpportunity(owner selfLearningOwner, runID, opportunityID string) (map[string]any, error) {
	if !n039ID.MatchString(runID) {
		return nil, errors.New("N039 requires a safe unique run-id")
	}
	observed := n039Observed
	opportunitySHA := ""
	currentActorSID := ""
	currentProcessInstance := ""
	if opportunityID != "" {
		if !n040IDValid(opportunityID) {
			return nil, errors.New("N040 untrusted opportunity identifier")
		}
		gate, locked := acquireLock(lockName("n040-help-opportunity", filepath.Join(owner.Settings.RuntimeRoot, opportunityID)))
		if !locked {
			return nil, errors.New("N040 another worker owns the original help opportunity")
		}
		defer gate.release()
		prior, verifiedSHA, evidenceErr := n040ReadOpportunityForUse(owner, opportunityID)
		if evidenceErr != nil {
			return nil, fmt.Errorf("N040 genuine native help opportunity not verified: %w", evidenceErr)
		}
		opportunitySHA = verifiedSHA
		currentActorSID, evidenceErr = n040CurrentActorSID()
		if evidenceErr != nil || prior["current_actor_sid"] != currentActorSID {
			return nil, errors.New("N040 current native OS actor differs from original request")
		}
		currentProcessInstance, evidenceErr = n040CurrentProcessInstance()
		if evidenceErr != nil || prior["process_instance_id"] == currentProcessInstance {
			return nil, errors.New("N040 use must execute in a different native process instance")
		}
		if err := n040PreviouslyUsedOrPending(owner, opportunityID); err != nil {
			return nil, err
		}
		observed = n040ObservedUse(opportunityID, opportunitySHA)
	}
	rule, principal, err := n039RuleIdentity(owner)
	if err != nil {
		return nil, err
	}
	path := n039ReceiptPath(owner, runID)
	if _, err := os.Lstat(path); err == nil {
		return nil, errors.New("N039 run already has a native result; inspect it without replay")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	loadID := "N039-LOADED-" + learnSHA([]byte(runID + "\n" + n039SHA))[:24]
	usedID := "N039-USED-" + learnSHA([]byte(runID + "\n" + n039SHA))[:24]
	if usedID == loadID || runID == n039OriginRun {
		return nil, errors.New("N039 run ids collide")
	}
	loaded, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "load", map[string]string{
		"run_id": loadID, "target": rule.Target, "expected_sha256": rule.SHA256,
	})
	if err != nil || loaded.Status != "loaded" || loaded.ID == "" {
		return nil, fmt.Errorf("N039 exact module load failed: %v (%s)", err, loaded.Status)
	}
	output, err := n039HelpRunner(context.Background())
	if err != nil {
		return nil, err
	}
	if len(output) == 0 || len(output) > 4096 || !bytes.HasPrefix(output, []byte("Usage: air-worker plan ")) {
		return nil, errors.New("N039 native help did not return supported CLI output")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	bin, err := os.ReadFile(exe)
	if err != nil {
		return nil, err
	}
	signed := map[string]string{
		"schema": "air-worker.n039.native-safe-help/v1",
		"status": "native_safe_help_pass",
		"action": n039Action, "argv": "plan help",
		"run_id": runID, "used_run": usedID, "load_run": loadID,
		"principal": principal, "target": rule.Target, "sha256": rule.SHA256,
		"origin_event_id": n039OriginEvent, "origin_run_id": n039OriginRun,
		"proposal_id": n039Proposal, "product_id": owner.Settings.ProductID,
		"product_root":  owner.Selector.ProductRoot,
		"runtime_root":  owner.Settings.RuntimeRoot,
		"binary_sha256": learnSHA(bin),
		"process_id":    fmt.Sprint(os.Getpid()),
		"stdout_sha256": learnSHA(output),
		"exit_code":     "0",
		"observed":      observed,
		"completed_at":  time.Now().UTC().Format(time.RFC3339Nano),
	}
	if opportunityID != "" {
		signed["opportunity_id"] = opportunityID
		signed["opportunity_sha256"] = opportunitySHA
		// The shared Module Learning v0.1.0 principal field binds the
		// original feedback author; the separate OS SID is the actual
		// current native invocation actor. Neither is caller-supplied.
		signed["original_feedback_principal"] = principal
		signed["current_actor_sid"] = currentActorSID
		signed["current_process_instance_id"] = currentProcessInstance
	}
	key, err := ownCLIPrivateKey(owner.Settings.RuntimeRoot)
	if err != nil {
		return nil, err
	}
	signed["mac"] = n039MAC(key, signed)
	for i := range key {
		key[i] = 0
	}
	raw, err := json.Marshal(signed)
	if err != nil {
		return nil, err
	}
	if err := writeReleaseReceiptNoReplace(path, append(raw, '\n')); err != nil {
		return nil, err
	}
	ref := filepath.ToSlash(path) + "#sha256=" + learnSHA(append(raw, '\n'))
	request := map[string]string{
		"run_id": usedID, "kind": "procedure_used",
		"original_run_id": n039OriginRun, "origin_event_id": n039OriginEvent,
		"proposal_id": n039Proposal, "load_run_id": loadID,
		"target": rule.Target, "expected_sha256": rule.SHA256,
		"rule_id": rule.Target + "@" + rule.SHA256,
		"source":  "native-plan-help", "class": "native-plan-help-use",
		"principal": principal, "session": n039OriginRun,
		"outcome": "pass", "outcome_ref": ref, "observed": observed,
	}
	data, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	config := sharedLearningConfig(owner.Selector.ProductRoot, owner.Settings)
	config.VerifyNativeOutcome = func(ctx context.Context, packet json.RawMessage) (json.RawMessage, error) {
		return n039Attest(ctx, owner, signed, packet)
	}
	event, err := learning.Execute(context.Background(), config, learning.Request{
		Operation: "observe_verified_use", Version: learning.Version, Data: data,
	})
	if err != nil || event.Status != "recorded" || event.ID == "" {
		return nil, fmt.Errorf("N039 native help ran but signed learning use NOT PROVEN: %v (%s)", err, event.Status)
	}
	effect := "safe_read_only_plan_help_delivered; paid-call-prevention-counterfactual_not_claimed"
	if opportunityID != "" {
		effect = "signed_native_unsupported_help_attempt_followed_by_safe_documented_help; paid-call-savings_NOT_PROVEN"
	}
	return map[string]any{
		"schema": "air-worker.n039.evidence/v1",
		"status": "PASS_NATIVE_ACTION_AND_MODULE_USE",
		"run_id": runID, "module_event_id": event.ID, "module_version": learning.Version,
		"rule_id": rule.Target + "@" + rule.SHA256, "receipt": ref,
		"effect": effect, "opportunity_id": opportunityID, "opportunity_sha256": opportunitySHA,
	}, nil
}

func cmdNativeN039SafeHelp(argv []string) int {
	fs := flag.NewFlagSet("learn safe-help", flag.ContinueOnError)
	product := fs.String("product", "", "exact self-learning owner root")
	runID := fs.String("run-id", "", "new unique observed run")
	opportunityID := fs.String("opportunity-id", "", "exact signed native unsupported-help attempt; optional")
	fs.Bool("json", true, "machine JSON")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if fs.NArg() != 0 || *product == "" || *runID == "" {
		fmt.Fprintln(os.Stderr, "learn safe-help requires -product and -run-id")
		return 2
	}
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil || !on {
		fmt.Fprintln(os.Stderr, "N039 self-learning owner unavailable:", err)
		return 2
	}
	if !sameLearningPath(*product, owner.Selector.ProductRoot) {
		fmt.Fprintln(os.Stderr, "N039 wrong product root")
		return 2
	}
	result, err := nativeN039SafeHelpWithOpportunity(owner, *runID, *opportunityID)
	if err != nil {
		fmt.Fprintln(os.Stderr, "N039:", err)
		return 1
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		return 2
	}
	return 0
}
