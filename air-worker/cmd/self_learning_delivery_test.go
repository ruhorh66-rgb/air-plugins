package main

import (
	"bytes"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExplicitDeliveryFreshProcessHelper(t *testing.T) {
	if os.Getenv("AW_DELIVERY_CHILD") != "1" {
		return
	}
	os.Exit(cmdValidate([]string{"-product", os.Getenv("AW_DELIVERY_PRODUCT"), "-json", "-learning-principal", "gpt", "-learning-session-key", "delivery", "-learning-run-id", "child-run", "-learning-context-sha256", os.Getenv("AW_DELIVERY_SHA")}))
}

func TestExplicitDeliveryFreshProcess(t *testing.T) {
	product, settings, _, rule := n093NativeTypedFixture(t)
	var skill sharedLearningCatalogSkill
	for _, candidate := range selfLoadCatalog(t, product, settings) {
		if candidate.Target == rule.Target && candidate.SHA256 == rule.SHA256 {
			skill = candidate
		}
	}
	code, body := captureSelfLoad(t, selfLoadArgs(skill, "child-run"))
	if code != 0 {
		t.Fatalf("load=%d %s", code, body)
	}
	var response struct {
		SHA string `json:"context_receipt_sha256"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestExplicitDeliveryFreshProcessHelper$")
	child.Env = append(os.Environ(), "AW_DELIVERY_CHILD=1", "AW_DELIVERY_PRODUCT="+product, "AW_DELIVERY_SHA="+response.SHA)
	output, err := child.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(output), "deferred_missing_configuration") {
		t.Fatalf("child: %v %s", err, output)
	}
	if n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("child did not produce one native use")
	}
}

func TestValidateLearningProofParsingPreservesOrdinaryArgs(t *testing.T) {
	sha := strings.Repeat("a", 64)
	argv := []string{"-json", "-learning-principal=gpt", "-product", "scratch", "-learning-session-key", "delivery", "-learning-run-id", "run", "-learning-context-sha256", sha}
	ordinary, proof, err := splitValidateLearningProof(argv)
	if err != nil || proof == nil || strings.Join(ordinary, "|") != "-json|-product|scratch" {
		t.Fatalf("ordinary argv changed: %v %+v %v", ordinary, proof, err)
	}
	for _, native := range [][]string{{"--product", "scratch", "-json"}, {"--product=scratch", "-json"}} {
		input := append(append([]string{}, native...), "-learning-principal", "gpt", "-learning-session-key", "delivery", "-learning-run-id", "run", "-learning-context-sha256", sha)
		got, linked, err := splitValidateLearningProof(input)
		if err != nil || linked == nil || !reflect.DeepEqual(got, native) {
			t.Fatalf("double-dash native argv changed: %v => %v %+v %v", input, got, linked, err)
		}
	}
	for _, input := range [][]string{
		{"-product", "-learning-principal", "-json"},
		{"--product", "-learning-principal", "-json"},
		{"-product=-learning-principal", "-json"},
		{"-json", "--", "plain"},
		{"-json", "plain", "-unknown"},
	} {
		got, proof, err := splitValidateLearningProof(input)
		if err != nil || proof != nil || !reflect.DeepEqual(got, input) {
			t.Fatalf("ordinary argv changed: %v => %v %+v %v", input, got, proof, err)
		}
	}
	for _, bad := range [][]string{
		{"-learning-principal", "gpt"},
		append(append([]string{}, argv...), "-learning-principal", "gpt"),
		{"-learning-principal", "gpt", "-learning-session-key", "delivery", "-learning-run-id", "run", "-learning-context-sha256", "ABC"},
		{"--learning-principal", "gpt"},
		append(append([]string{"--"}, argv...), "-json"),
		append([]string{"plain"}, argv...),
	} {
		if _, _, err := splitValidateLearningProof(bad); err == nil {
			t.Fatalf("accepted malformed proof: %v", bad)
		}
	}
}

func TestConfirmedSelfLearningContextSHARejectsReplacedSnapshot(t *testing.T) {
	product, settings, _ := selfLearningFixture(t)
	code, body := captureSelfLoad(t, selfLoadArgs(selfLoadCatalog(t, product, settings)[0], "replace-run"))
	if code != 0 {
		t.Fatalf("load=%d %s", code, body)
	}
	var response struct {
		Path string `json:"context_receipt_path"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	written, err := n093ReadContextEvidence(response.Path)
	if err != nil {
		t.Fatal(err)
	}
	if sha, err := confirmedSelfLearningContextSHA(response.Path, written); err != nil || sha != learnSHA(written) {
		t.Fatalf("original snapshot: %s %v", sha, err)
	}
	if err := writeFileAtomicDurable(response.Path, []byte("replaced\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := confirmedSelfLearningContextSHA(response.Path, written); err == nil {
		t.Fatal("accepted another publisher's snapshot")
	}
}

func deliveryNativeReceipts(t *testing.T, runtime string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for _, dir := range []string{"native-preflight-uses", "native-preflight-results"} {
		entries, err := os.ReadDir(filepath.Join(runtime, dir))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			path := filepath.Join(runtime, dir, entry.Name())
			out[path], err = os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	return out
}

func rejectLinkedValidate(t *testing.T, settings sharedLearningSettings, args []string) {
	t.Helper()
	before := deliveryNativeReceipts(t, settings.RuntimeRoot)
	uses := n104Events(t, settings.RuntimeRoot, "procedure_used")
	code, output := captureLoopOutput(t, func() int { return cmdValidate(args) })
	if code != 2 || strings.Contains(output, "deferred_missing_configuration") ||
		strings.Contains(output, "procedure_used") || strings.Contains(output, "outcome_receipt_path") ||
		!reflect.DeepEqual(before, deliveryNativeReceipts(t, settings.RuntimeRoot)) ||
		n104Events(t, settings.RuntimeRoot, "procedure_used") != uses {
		t.Fatalf("invalid proof acquired linked outcome: rc=%d output=%s", code, output)
	}
}

func TestExplicitDeliveryBindsActualNativePreflight(t *testing.T) {
	product, settings, args, rule := n093NativeTypedFixture(t)
	skills := selfLoadCatalog(t, product, settings)
	var skill sharedLearningCatalogSkill
	for _, candidate := range skills {
		if candidate.Target == rule.Target && candidate.SHA256 == rule.SHA256 {
			skill = candidate
		}
	}
	if skill.Target == "" {
		t.Fatal("reviewed rule absent from catalog")
	}
	load := func() string {
		t.Helper()
		code, body := captureSelfLoad(t, selfLoadArgs(skill, "causal-run"))
		if code != 0 {
			t.Fatalf("load=%d %s", code, body)
		}
		var response struct {
			ContextSHA string `json:"context_receipt_sha256"`
			Path       string `json:"context_receipt_path"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatal(err)
		}
		raw, err := n093ReadContextEvidence(response.Path)
		if err != nil || response.ContextSHA != learnSHA(raw) {
			t.Fatalf("delivery hash: %v", err)
		}
		return response.ContextSHA
	}
	sha := load()
	contextPath, err := selfLearningContextReceiptPath(hookInput{Principal: "gpt", SessionID: "delivery", RunID: "causal-run"})
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 0 {
		t.Fatalf("load used procedure: %d", got)
	}
	flags := []string{"-learning-principal", "gpt", "-learning-session-key", "delivery", "-learning-run-id", "causal-run", "-learning-context-sha256", sha}
	validate := func(proof []string) []string { return append(append([]string{}, args[1:]...), proof...) }
	var edited selfLearningContextReceipt
	if err := json.Unmarshal(original, &edited); err != nil {
		t.Fatal(err)
	}
	edited.DeliveryMAC = ""
	if err := writeSelfLearningContextReceipt(contextPath, edited); err != nil {
		t.Fatal(err)
	}
	unsigned, err := n093ReadContextEvidence(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	unsignedFlags := append([]string{}, flags...)
	unsignedFlags[len(unsignedFlags)-1] = learnSHA(unsigned)
	rejectLinkedValidate(t, settings, validate(unsignedFlags))
	edited.DeliveryMAC = "wrong"
	if err := writeSelfLearningContextReceipt(contextPath, edited); err != nil {
		t.Fatal(err)
	}
	tampered, err := n093ReadContextEvidence(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	tamperedFlags := append([]string{}, flags...)
	tamperedFlags[len(tamperedFlags)-1] = learnSHA(tampered)
	rejectLinkedValidate(t, settings, validate(tamperedFlags))
	if err := writeFileAtomicDurable(contextPath, original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(original, &edited); err != nil {
		t.Fatal(err)
	}
	edited.UsedAt = "later"
	edited.Outcome = "claimed"
	if err := writeSelfLearningContextReceipt(contextPath, edited); err != nil {
		t.Fatal(err)
	}
	mutated, err := n093ReadContextEvidence(contextPath)
	if err != nil {
		t.Fatal(err)
	}
	mutatedFlags := append([]string{}, flags...)
	mutatedFlags[len(mutatedFlags)-1] = learnSHA(mutated)
	rejectLinkedValidate(t, settings, validate(mutatedFlags))
	if err := writeFileAtomicDurable(contextPath, original); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{flags[:6], append(append([]string{}, flags...), "-learning-principal", "gpt"),
		{"-learning-principal", "other", "-learning-session-key", "delivery", "-learning-run-id", "causal-run", "-learning-context-sha256", sha},
		{"-learning-principal", "gpt", "-learning-session-key", "wrong", "-learning-run-id", "causal-run", "-learning-context-sha256", sha},
		{"-learning-principal", "gpt", "-learning-session-key", "delivery", "-learning-run-id", "wrong", "-learning-context-sha256", sha},
		{"-learning-principal", "gpt", "-learning-session-key", "delivery", "-learning-run-id", "causal-run", "-learning-context-sha256", strings.Repeat("b", 64)},
	} {
		rejectLinkedValidate(t, settings, validate(bad))
	}
	for _, bad := range [][]string{
		append([]string{"--"}, flags...), append([]string{"positional"}, flags...),
		{"--learning-principal", "gpt"},
	} {
		rejectLinkedValidate(t, settings, validate(bad))
	}
	if err := os.Remove(contextPath); err != nil {
		t.Fatal(err)
	}
	rejectLinkedValidate(t, settings, validate(flags))
	if err := writeFileAtomicDurable(contextPath, original); err != nil {
		t.Fatal(err)
	}
	sel, found, err := readAirWorkerSelfLearningSelector()
	if err != nil || !found {
		t.Fatal(err)
	}
	sel.Enabled = false
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	rejectLinkedValidate(t, settings, validate(flags))
	sel.Enabled = true
	sel.ConfigSHA256 = strings.Repeat("a", 64)
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	rejectLinkedValidate(t, settings, validate(flags))
	sel.ConfigSHA256, err = selfLearningConfigSHA(product)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeAirWorkerSelfLearningSelector(sel); err != nil {
		t.Fatal(err)
	}
	otherProduct := filepath.Join(t.TempDir(), "other")
	if err := os.MkdirAll(otherProduct, 0700); err != nil {
		t.Fatal(err)
	}
	rejectLinkedValidate(t, settings, append([]string{"-product", otherProduct, "-json"}, flags...))
	if err := os.WriteFile(filepath.Join(product, "run-config.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	rejectLinkedValidate(t, settings, validate(flags))
	if err := os.Remove(filepath.Join(product, "run-config.json")); err != nil {
		t.Fatal(err)
	}
	rulePath := filepath.Join(product, filepath.FromSlash(rule.Target))
	ruleBytes, err := os.ReadFile(rulePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rulePath, append(append([]byte{}, ruleBytes...), '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	rejectLinkedValidate(t, settings, validate(flags))
	if err := os.WriteFile(rulePath, ruleBytes, 0600); err != nil {
		t.Fatal(err)
	}
	priorPath := filepath.Join(settings.RuntimeRoot, "cli-failures", "AW-CLI-N093-TYPED-USE-TEST.json")
	priorBytes, err := os.ReadFile(priorPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(priorPath); err != nil {
		t.Fatal(err)
	}
	rejectLinkedValidate(t, settings, validate(flags))
	if err := writeFileAtomicDurable(priorPath, priorBytes); err != nil {
		t.Fatal(err)
	}
	call := func() string {
		t.Helper()
		code, body := captureLoopOutput(t, func() int { return cmdValidate(append(append([]string{}, args[1:]...), flags...)) })
		if code != 2 || !strings.Contains(body, "deferred_missing_configuration") {
			t.Fatalf("validate=%d %s", code, body)
		}
		var response struct {
			Path string `json:"outcome_receipt_path"`
			SHA  string `json:"delivery_context_sha256"`
		}
		if err := json.Unmarshal([]byte(body), &response); err != nil || response.SHA != sha {
			t.Fatalf("response: %v %s", err, body)
		}
		return response.Path
	}
	path := call()
	if got := n104Events(t, settings.RuntimeRoot, "procedure_used"); got != 1 {
		t.Fatalf("use count: %d", got)
	}
	result, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt nativePreflightUseReceipt
	if err := json.Unmarshal(result, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.DeliveryContextSHA != sha || receipt.DeliveryPrincipal != "gpt" || receipt.DeliverySession != "delivery" || receipt.DeliveryRunID != "causal-run" || receipt.Principal != "air-worker" || receipt.ActionID == "" || receipt.NativeValidationExecuted {
		t.Fatalf("bad linked result: %+v", receipt)
	}
	intentPath := filepath.Join(settings.RuntimeRoot, "native-preflight-uses", filepath.Base(path))
	intentRaw, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatal(err)
	}
	var intent nativePreflightUseReceipt
	if err := json.Unmarshal(intentRaw, &intent); err != nil {
		t.Fatal(err)
	}
	key, err := ownCLIPrivateKey(settings.RuntimeRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, signed := range []nativePreflightUseReceipt{intent, receipt} {
		mac, err := nativeTypedPreflightReceiptMAC(key, signed)
		if err != nil || !hmac.Equal([]byte(mac), []byte(signed.ReceiptMAC)) || signed.DeliveryContextSHA != sha {
			t.Fatal("delivery is absent from signed native receipt")
		}
	}
	for i := range key {
		key[i] = 0
	}
	if receipt.IntentSHA != learnSHA(intentRaw) || receipt.ConfirmedEventID == "" {
		t.Fatal("final result does not reference signed intent and event")
	}
	if call() != path || n104Events(t, settings.RuntimeRoot, "procedure_used") != 1 {
		t.Fatal("linked retry changed result or use count")
	}
	for _, item := range []struct {
		path string
		raw  []byte
	}{{intentPath, intentRaw}, {path, result}} {
		for _, change := range []func(*nativePreflightUseReceipt){
			func(r *nativePreflightUseReceipt) { r.DeliveryPrincipal = "forged" },
			func(r *nativePreflightUseReceipt) { r.DeliverySession = "forged" },
			func(r *nativePreflightUseReceipt) { r.DeliveryRunID = "forged" },
			func(r *nativePreflightUseReceipt) { r.DeliveryContextSHA = strings.Repeat("b", 64) },
		} {
			var edited nativePreflightUseReceipt
			if err := json.Unmarshal(item.raw, &edited); err != nil {
				t.Fatal(err)
			}
			change(&edited)
			bad, err := json.Marshal(edited)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeFileAtomicDurable(item.path, bad); err != nil {
				t.Fatal(err)
			}
			rejectLinkedValidate(t, settings, validate(flags))
			if err := writeFileAtomicDurable(item.path, item.raw); err != nil {
				t.Fatal(err)
			}
		}
	}
	if finalRaw, err := os.ReadFile(path); err != nil || !bytes.Equal(finalRaw, result) {
		t.Fatal("final result changed")
	}
	if next := load(); next == sha {
		t.Fatal("refresh did not replace delivery")
	}
	rejectLinkedValidate(t, settings, validate(flags))
	flags[len(flags)-1] = load()
	rejectLinkedValidate(t, settings, validate(flags))
}

func TestExplicitDeliveryCannotRelabelIndependentUse(t *testing.T) {
	product, settings, args, rule := n093NativeTypedFixture(t)
	if code, _ := captureLoopOutput(t, func() int { return cmdValidate(args[1:]) }); code != 2 {
		t.Fatal("independent preflight did not defer")
	}
	before := deliveryNativeReceipts(t, settings.RuntimeRoot)
	for _, raw := range before {
		var rec nativePreflightUseReceipt
		if err := json.Unmarshal(raw, &rec); err != nil || rec.DeliveryContextSHA != "" || rec.DeliveryPrincipal != "" || rec.DeliverySession != "" || rec.DeliveryRunID != "" {
			t.Fatal("independent action has delivery fields")
		}
	}
	var skill sharedLearningCatalogSkill
	for _, candidate := range selfLoadCatalog(t, product, settings) {
		if candidate.Target == rule.Target && candidate.SHA256 == rule.SHA256 {
			skill = candidate
		}
	}
	code, body := captureSelfLoad(t, selfLoadArgs(skill, "later-run"))
	if code != 0 {
		t.Fatalf("load=%d %s", code, body)
	}
	var response struct {
		SHA string `json:"context_receipt_sha256"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		t.Fatal(err)
	}
	linked := append(append([]string{}, args[1:]...), "-learning-principal", "gpt", "-learning-session-key", "delivery", "-learning-run-id", "later-run", "-learning-context-sha256", response.SHA)
	rejectLinkedValidate(t, settings, linked)
}
