package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	selfLearningContextReceiptSchema = "air-worker.self-learning.context/v1"
	selfLearningTriggerReceiptSchema = "air-worker.self-learning.trigger/v1"
)

type selfLearningLoadedRule struct {
	Target string `json:"target"`
	SHA256 string `json:"sha256"`
}

type selfLearningContextReceipt struct {
	Schema       string                   `json:"schema"`
	Principal    string                   `json:"principal"`
	Session      string                   `json:"session"`
	RunID        string                   `json:"run_id"`
	ProductRoot  string                   `json:"product_root"`
	RuntimeRoot  string                   `json:"runtime_root"`
	Loaded       []selfLearningLoadedRule `json:"loaded"`
	CreatedAt    string                   `json:"created_at"`
	UsedAt       string                   `json:"used_at,omitempty"`
	UsedRuleID   string                   `json:"used_rule_id,omitempty"`
	Outcome      string                   `json:"outcome,omitempty"`
	OutcomeRef   string                   `json:"outcome_ref,omitempty"`
	ObservedTool string                   `json:"observed_tool,omitempty"`
}

type selfLearningTriggerReceipt struct {
	Schema         string                 `json:"schema"`
	Principal      string                 `json:"principal"`
	Session        string                 `json:"session"`
	RunID          string                 `json:"run_id"`
	Kind           string                 `json:"kind"`
	Rule           selfLearningLoadedRule `json:"rule"`
	ToolName       string                 `json:"tool_name"`
	ToolInputSHA   string                 `json:"tool_input_sha256,omitempty"`
	ResponseSHA    string                 `json:"tool_response_sha256"`
	CreatedAt      string                 `json:"created_at"`
	ConsumedAt     string                 `json:"consumed_at,omitempty"`
	OutcomeRef     string                 `json:"outcome_ref,omitempty"`
	DiagnosticTool string                 `json:"diagnostic_tool,omitempty"`
}

func selfLearningRunID(in hookInput) string {
	if run := strings.TrimSpace(in.RunID); run != "" {
		return run
	}
	return "context-" + strings.TrimSpace(in.SessionID)
}

func selfLearningSessionDir(in hookInput) (string, error) {
	id, ok := hookInputIdentity(in)
	if !ok {
		return "", errors.New("self-learning receipt requires valid principal/session identity")
	}
	return filepath.Join(hookStateDir(), "air-worker-self-learning", id.namespace()), nil
}

func selfLearningContextReceiptPath(in hookInput) (string, error) {
	dir, err := selfLearningSessionDir(in)
	if err != nil {
		return "", err
	}
	key := learnSHA([]byte(selfLearningRunID(in)))[:24]
	return filepath.Join(dir, "context-"+key+".json"), nil
}

func selfLearningTriggerReceiptPath(in hookInput) (string, error) {
	dir, err := selfLearningSessionDir(in)
	if err != nil {
		return "", err
	}
	key := learnSHA([]byte(selfLearningRunID(in)))[:24]
	return filepath.Join(dir, "execution-unknown-trigger-"+key+".json"), nil
}

func writeSelfLearningContextReceipt(path string, rec selfLearningContextReceipt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(path, append(raw, '\n'))
}

func readSelfLearningContextReceipt(in hookInput) (selfLearningContextReceipt, string, bool, error) {
	path, err := selfLearningContextReceiptPath(in)
	if err != nil {
		return selfLearningContextReceipt{}, "", false, err
	}
	var rec selfLearningContextReceipt
	if err := readJSON(path, &rec); errors.Is(err, os.ErrNotExist) {
		return rec, path, false, nil
	} else if err != nil {
		return rec, path, true, err
	}
	if rec.Schema != selfLearningContextReceiptSchema ||
		rec.Principal != hookPrincipal(in) ||
		rec.Session != in.SessionID ||
		rec.RunID != selfLearningRunID(in) ||
		!filepath.IsAbs(rec.ProductRoot) ||
		!filepath.IsAbs(rec.RuntimeRoot) {
		return rec, path, true, errors.New("invalid AirWorker self-learning context receipt identity")
	}
	for _, rule := range rec.Loaded {
		if strings.TrimSpace(rule.Target) == "" || len(rule.SHA256) != 64 {
			return rec, path, true, errors.New("invalid AirWorker self-learning loaded rule identity")
		}
		if _, err := hex.DecodeString(rule.SHA256); err != nil {
			return rec, path, true, errors.New("invalid AirWorker self-learning loaded rule SHA")
		}
	}
	return rec, path, true, nil
}

func loadedRulesFromObserved(observed string) []selfLearningLoadedRule {
	seen := map[string]selfLearningLoadedRule{}
	for _, item := range strings.Split(observed, ";") {
		item = strings.TrimSpace(item)
		at := strings.LastIndex(item, "@")
		if at <= 0 || at+1 >= len(item) {
			continue
		}
		target := strings.TrimSpace(item[:at])
		sha := strings.ToLower(strings.TrimSpace(item[at+1:]))
		if target == "" || len(sha) != 64 {
			continue
		}
		if _, err := hex.DecodeString(sha); err != nil {
			continue
		}
		seen[strings.ToLower(target)+"@"+sha] = selfLearningLoadedRule{Target: target, SHA256: sha}
	}
	out := make([]selfLearningLoadedRule, 0, len(seen))
	for _, rule := range seen {
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Target == out[j].Target {
			return out[i].SHA256 < out[j].SHA256
		}
		return out[i].Target < out[j].Target
	})
	return out
}

func captureAirWorkerSelfLearningContextReceipt(owner selfLearningOwner, in hookInput) error {
	res, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "events", map[string]string{"source": "host-context"})
	if err != nil {
		return err
	}
	var rows []map[string]any
	if err := json.Unmarshal(res.Data, &rows); err != nil {
		return err
	}
	baseRun := selfLearningRunID(in)
	prefix := baseRun + ":context-loaded:context:"
	byID := map[string]selfLearningLoadedRule{}
	for _, row := range rows {
		kind, _ := row["kind"].(string)
		runID, _ := row["run_id"].(string)
		session, _ := row["session"].(string)
		if kind != "skill_loaded" || session != in.SessionID || !strings.HasPrefix(runID, prefix) {
			continue
		}
		observed, _ := row["observed"].(string)
		for _, rule := range loadedRulesFromObserved(observed) {
			byID[strings.ToLower(rule.Target)+"@"+rule.SHA256] = rule
		}
	}
	loaded := make([]selfLearningLoadedRule, 0, len(byID))
	for _, rule := range byID {
		loaded = append(loaded, rule)
	}
	sort.Slice(loaded, func(i, j int) bool {
		if loaded[i].Target == loaded[j].Target {
			return loaded[i].SHA256 < loaded[j].SHA256
		}
		return loaded[i].Target < loaded[j].Target
	})
	path, err := selfLearningContextReceiptPath(in)
	if err != nil {
		return err
	}
	rec := selfLearningContextReceipt{
		Schema: selfLearningContextReceiptSchema, Principal: hookPrincipal(in), Session: in.SessionID,
		RunID: baseRun, ProductRoot: owner.Selector.ProductRoot, RuntimeRoot: owner.Settings.RuntimeRoot,
		Loaded: loaded, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	if previous, _, found, readErr := readSelfLearningContextReceipt(in); readErr == nil && found {
		rec.UsedAt = previous.UsedAt
		rec.UsedRuleID = previous.UsedRuleID
		rec.Outcome = previous.Outcome
		rec.OutcomeRef = previous.OutcomeRef
		rec.ObservedTool = previous.ObservedTool
	}
	return writeSelfLearningContextReceipt(path, rec)
}

func executionUnknownLoadedRule(rec selfLearningContextReceipt) (selfLearningLoadedRule, bool) {
	for _, rule := range rec.Loaded {
		if strings.Contains(strings.ToLower(filepath.ToSlash(rule.Target)), "execution-unknown-no-blind-retry") {
			return rule, true
		}
	}
	return selfLearningLoadedRule{}, false
}

func hookResponseBytes(in hookInput) []byte {
	if len(in.ToolResponse) > 0 {
		return in.ToolResponse
	}
	return in.ToolResult
}

func executionUnknownResponse(raw []byte) bool {
	return strings.Contains(strings.ToUpper(string(raw)), "EXECUTION_UNKNOWN")
}

func startProcessTool(name string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(name)), "start_process")
}

func executionUnknownDiagnosticTool(name string) bool {
	low := strings.ToLower(strings.TrimSpace(name))
	for _, token := range []string{
		"list_nodes", "read_process_output", "get_file_info", "list_directory",
		"read_file", "read_multiple_files", "list_searches", "start_search",
		"get_more_search_results",
	} {
		if strings.Contains(low, token) {
			return true
		}
	}
	return false
}

func numericExitCode(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		if x != float64(int64(x)) {
			return 0, false
		}
		return int64(x), true
	case json.Number:
		n, err := x.Int64()
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}

func observableToolSuccessValue(v any) (bool, bool) {
	switch x := v.(type) {
	case map[string]any:
		if flag, ok := x["is_error"].(bool); ok {
			return !flag, true
		}
		if value, ok := x["error"]; ok && value != nil {
			if s, isString := value.(string); !isString || strings.TrimSpace(s) != "" {
				return false, true
			}
		}
		for _, key := range []string{"exit_code", "exitCode", "code"} {
			if value, ok := x[key]; ok {
				if n, valid := numericExitCode(value); valid {
					return n == 0, true
				}
			}
		}
		if status, ok := x["status"].(string); ok {
			switch strings.ToLower(strings.TrimSpace(status)) {
			case "ok", "success", "passed", "pass", "online", "completed", "ready", "recorded":
				return true, true
			case "error", "failed", "fail", "offline", "blocked":
				return false, true
			}
		}
		for _, key := range []string{"result", "data", "content", "output", "text"} {
			if value, ok := x[key]; ok {
				if success, known := observableToolSuccessValue(value); known {
					return success, true
				}
			}
		}
		for _, key := range []string{"nodes", "results", "files"} {
			if value, ok := x[key]; ok {
				if arr, ok := value.([]any); ok {
					return len(arr) > 0, true
				}
			}
		}
	case []any:
		if len(x) > 0 {
			return true, true
		}
	case string:
		text := strings.TrimSpace(x)
		if text == "" {
			return false, false
		}
		upper := strings.ToUpper(text)
		if strings.Contains(upper, "EXECUTION_UNKNOWN") {
			return false, true
		}
		if strings.Contains(strings.ToLower(text), "process completed with exit code 0") ||
			strings.Contains(strings.ToLower(text), "\"status\":\"online\"") {
			return true, true
		}
	}
	return false, false
}

func observableToolSuccess(raw []byte) bool {
	if len(raw) == 0 || !json.Valid(raw) {
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var value any
	if dec.Decode(&value) != nil {
		return false
	}
	success, known := observableToolSuccessValue(value)
	return known && success
}

func writeSelfLearningTriggerReceipt(path string, rec selfLearningTriggerReceipt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(path, append(raw, '\n'))
}

func readSelfLearningTriggerReceipt(in hookInput) (selfLearningTriggerReceipt, string, bool, error) {
	path, err := selfLearningTriggerReceiptPath(in)
	if err != nil {
		return selfLearningTriggerReceipt{}, "", false, err
	}
	var rec selfLearningTriggerReceipt
	if err := readJSON(path, &rec); errors.Is(err, os.ErrNotExist) {
		return rec, path, false, nil
	} else if err != nil {
		return rec, path, true, err
	}
	if rec.Schema != selfLearningTriggerReceiptSchema || rec.Session != in.SessionID || rec.Principal != hookPrincipal(in) {
		return rec, path, true, errors.New("invalid AirWorker self-learning trigger receipt identity")
	}
	return rec, path, true, nil
}

func handlePostToolUseSelfLearning(in hookInput) (hookResult, error) {
	owner, on, err := activeAirWorkerSelfLearningOwner()
	if err != nil {
		return hookResult{}, sharedHookFailure("self-selector", strings.TrimSpace(in.RunID), err)
	}
	if !on {
		return hookResult{}, nil
	}
	contextRec, contextPath, found, err := readSelfLearningContextReceipt(in)
	if err != nil {
		return hookResult{}, sharedHookFailure("self-context-receipt", selfLearningRunID(in), err)
	}
	if !found {
		return hookResult{}, nil
	}
	rule, hasRule := executionUnknownLoadedRule(contextRec)
	if !hasRule {
		return hookResult{}, nil
	}
	rawResponse := hookResponseBytes(in)
	if len(rawResponse) == 0 {
		return hookResult{}, nil
	}
	triggerPath, err := selfLearningTriggerReceiptPath(in)
	if err != nil {
		return hookResult{}, err
	}
	if startProcessTool(in.ToolName) && executionUnknownResponse(rawResponse) {
		trigger := selfLearningTriggerReceipt{
			Schema: selfLearningTriggerReceiptSchema, Principal: hookPrincipal(in), Session: in.SessionID,
			RunID: selfLearningRunID(in), Kind: "execution_unknown", Rule: rule, ToolName: in.ToolName,
			ToolInputSHA: learnSHA(in.ToolInput), ResponseSHA: learnSHA(rawResponse),
			CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err := writeSelfLearningTriggerReceipt(triggerPath, trigger); err != nil {
			return hookResult{}, err
		}
		return hookResult{}, nil
	}

	trigger, _, triggerFound, err := readSelfLearningTriggerReceipt(in)
	if err != nil {
		return hookResult{}, err
	}
	if !triggerFound || trigger.ConsumedAt != "" || trigger.Kind != "execution_unknown" || trigger.RunID != selfLearningRunID(in) {
		return hookResult{}, nil
	}
	if !executionUnknownDiagnosticTool(in.ToolName) || !observableToolSuccess(rawResponse) {
		return hookResult{}, nil
	}
	if trigger.Rule.Target != rule.Target || !strings.EqualFold(trigger.Rule.SHA256, rule.SHA256) {
		return hookResult{}, errors.New("self-learning trigger rule does not match the exact loaded rule")
	}
	responseSHA := learnSHA(rawResponse)
	outcomeRef := filepath.ToSlash(contextPath) + "#tool_response_sha256=" + responseSHA
	ruleID := rule.Target + "@" + strings.ToLower(rule.SHA256)
	usageRunID := selfLearningRunID(in) + ":used:" + learnSHA([]byte(trigger.CreatedAt + "\n" + in.ToolName + "\n" + responseSHA))[:20]
	res, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "observe", map[string]string{
		"run_id": usageRunID, "kind": "procedure_used",
		"observed": "After EXECUTION_UNKNOWN, a diagnostic tool completed before any repeated start was accepted.",
		"class":    "execution-unknown-no-blind-retry", "source": "host-post-tool",
		"principal": hookPrincipal(in), "session": in.SessionID,
		"rule_id": ruleID, "outcome": "pass", "outcome_ref": outcomeRef,
	})
	if err != nil {
		return hookResult{}, err
	}
	if res.Status != "recorded" && res.Status != "duplicate" {
		return hookResult{}, fmt.Errorf("unexpected self-learning usage status %q", res.Status)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	contextRec.UsedAt = now
	contextRec.UsedRuleID = ruleID
	contextRec.Outcome = "pass"
	contextRec.OutcomeRef = outcomeRef
	contextRec.ObservedTool = in.ToolName
	if err := writeSelfLearningContextReceipt(contextPath, contextRec); err != nil {
		return hookResult{}, err
	}
	trigger.ConsumedAt = now
	trigger.OutcomeRef = outcomeRef
	trigger.DiagnosticTool = in.ToolName
	if err := writeSelfLearningTriggerReceipt(triggerPath, trigger); err != nil {
		return hookResult{}, err
	}
	return hookResult{Context: "AIRWORKER SELF-LEARNING: procedure_used " + ruleID + " outcome=pass"}, nil
}
