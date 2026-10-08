package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Schema             string                 `json:"schema"`
	Principal          string                 `json:"principal"`
	Session            string                 `json:"session"`
	RunID              string                 `json:"run_id"`
	Kind               string                 `json:"kind"`
	Rule               selfLearningLoadedRule `json:"rule"`
	ToolName           string                 `json:"tool_name"`
	ToolInputSHA       string                 `json:"tool_input_sha256,omitempty"`
	OperationKey       string                 `json:"operation_key,omitempty"`
	Node               string                 `json:"node,omitempty"`
	ProcessIDs         []int64                `json:"process_ids,omitempty"`
	RequestIDs         []string               `json:"request_ids,omitempty"`
	ArtifactPathSHA256 []string               `json:"artifact_path_sha256,omitempty"`
	ResponseSHA        string                 `json:"tool_response_sha256"`
	CreatedAt          string                 `json:"created_at"`
	ConsumedAt         string                 `json:"consumed_at,omitempty"`
	InvalidatedAt      string                 `json:"invalidated_at,omitempty"`
	InvalidationReason string                 `json:"invalidation_reason,omitempty"`
	OutcomeRef         string                 `json:"outcome_ref,omitempty"`
	DiagnosticTool     string                 `json:"diagnostic_tool,omitempty"`
	DiagnosticEvidence string                 `json:"diagnostic_evidence,omitempty"`
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

func captureAirWorkerSelfLearningContextReceipt(owner selfLearningOwner, in hookInput, skills []sharedLearningCatalogSkill) error {
	byTarget := map[string]selfLearningLoadedRule{}
	for _, skill := range skills {
		target := strings.TrimSpace(skill.Target)
		sha := strings.ToLower(strings.TrimSpace(skill.SHA256))
		if target == "" || len(sha) != 64 {
			return errors.New("current self-learning delivery has invalid target/SHA identity")
		}
		if _, err := hex.DecodeString(sha); err != nil {
			return errors.New("current self-learning delivery has invalid SHA")
		}
		key := strings.ToLower(filepath.ToSlash(target))
		if previous, exists := byTarget[key]; exists && !strings.EqualFold(previous.SHA256, sha) {
			return fmt.Errorf("current self-learning delivery contains multiple SHAs for %s", target)
		}
		byTarget[key] = selfLearningLoadedRule{Target: target, SHA256: sha}
	}
	loaded := make([]selfLearningLoadedRule, 0, len(byTarget))
	for _, rule := range byTarget {
		loaded = append(loaded, rule)
	}
	sort.Slice(loaded, func(i, j int) bool {
		return strings.ToLower(filepath.ToSlash(loaded[i].Target)) < strings.ToLower(filepath.ToSlash(loaded[j].Target))
	})
	path, err := selfLearningContextReceiptPath(in)
	if err != nil {
		return err
	}
	rec := selfLearningContextReceipt{
		Schema: selfLearningContextReceiptSchema, Principal: hookPrincipal(in), Session: in.SessionID,
		RunID: selfLearningRunID(in), ProductRoot: owner.Selector.ProductRoot, RuntimeRoot: owner.Settings.RuntimeRoot,
		Loaded: loaded, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	// This file is the exact CURRENT delivery snapshot for the run. Never carry
	// used/outcome fields across a later context refresh; durable historical use
	// remains in the shared module event journal.
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

var processIDPattern = regexp.MustCompile(`(?i)\b(?:pid|process[_ ]?id)\s*[:=#]?\s*(\d+)\b`)

func decodeJSONValue(raw []byte) (any, error) {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, errors.New("invalid JSON")
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func toolInputObject(raw json.RawMessage) map[string]any {
	value, err := decodeJSONValue(raw)
	if err != nil {
		return nil
	}
	obj, _ := value.(map[string]any)
	return obj
}

func startProcessOperationIdentity(raw json.RawMessage) (key, node string) {
	obj := toolInputObject(raw)
	if obj == nil {
		return "", ""
	}
	node, _ = obj["node"].(string)
	command, _ := obj["command"].(string)
	shell, _ := obj["shell"].(string)
	node = strings.TrimSpace(node)
	command = strings.TrimSpace(command)
	shell = strings.TrimSpace(shell)
	if command == "" {
		return "", node
	}
	canonical, _ := json.Marshal(map[string]string{
		"node": strings.ToLower(node), "command": command, "shell": strings.ToLower(shell),
	})
	return learnSHA(canonical), node
}

func normalizedEvidencePathHash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || !filepath.IsAbs(value) {
		return ""
	}
	return learnSHA([]byte(strings.ToLower(filepath.ToSlash(filepath.Clean(value)))))
}

func collectCorrelationRefs(value any, pids map[int64]bool, requests map[string]bool, paths map[string]bool) {
	switch x := value.(type) {
	case map[string]any:
		for key, item := range x {
			low := strings.ToLower(strings.TrimSpace(key))
			switch low {
			case "pid", "process_id", "processid":
				if pid, ok := numericExitCode(item); ok && pid > 0 {
					pids[pid] = true
				}
			case "request_id", "requestid":
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					requests[strings.TrimSpace(text)] = true
				}
			case "path", "file", "file_path", "artifact", "artifact_path", "receipt", "receipt_path", "output_path":
				if text, ok := item.(string); ok {
					if digest := normalizedEvidencePathHash(text); digest != "" {
						paths[digest] = true
					}
				}
			}
			collectCorrelationRefs(item, pids, requests, paths)
		}
	case []any:
		for _, item := range x {
			collectCorrelationRefs(item, pids, requests, paths)
		}
	case string:
		for _, match := range processIDPattern.FindAllStringSubmatch(x, -1) {
			if len(match) == 2 {
				if pid, err := strconv.ParseInt(match[1], 10, 64); err == nil && pid > 0 {
					pids[pid] = true
				}
			}
		}
	}
}

func correlationRefs(raw []byte) ([]int64, []string, []string) {
	value, err := decodeJSONValue(raw)
	if err != nil {
		return nil, nil, nil
	}
	pidSet := map[int64]bool{}
	requestSet := map[string]bool{}
	pathSet := map[string]bool{}
	collectCorrelationRefs(value, pidSet, requestSet, pathSet)
	pids := make([]int64, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	sort.Slice(pids, func(i, j int) bool { return pids[i] < pids[j] })
	requests := make([]string, 0, len(requestSet))
	for request := range requestSet {
		requests = append(requests, request)
	}
	sort.Strings(requests)
	paths := make([]string, 0, len(pathSet))
	for digest := range pathSet {
		paths = append(paths, digest)
	}
	sort.Strings(paths)
	return pids, requests, paths
}

func sameStringFold(list []string, value string) bool {
	for _, item := range list {
		if strings.EqualFold(item, value) {
			return true
		}
	}
	return false
}

func sameInt64(list []int64, value int64) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func toolInputPaths(obj map[string]any) []string {
	if obj == nil {
		return nil
	}
	var out []string
	for _, key := range []string{"path", "file", "file_path", "artifact", "artifact_path", "receipt", "receipt_path", "output_path"} {
		if value, ok := obj[key].(string); ok {
			if digest := normalizedEvidencePathHash(value); digest != "" {
				out = append(out, digest)
			}
		}
	}
	if values, ok := obj["paths"].([]any); ok {
		for _, value := range values {
			if text, ok := value.(string); ok {
				if digest := normalizedEvidencePathHash(text); digest != "" {
					out = append(out, digest)
				}
			}
		}
	}
	return out
}

func correlatedExecutionUnknownDiagnostic(in hookInput, trigger selfLearningTriggerReceipt) (string, bool) {
	obj := toolInputObject(in.ToolInput)
	if obj == nil {
		return "", false
	}
	node, _ := obj["node"].(string)
	if strings.TrimSpace(trigger.Node) == "" || !strings.EqualFold(strings.TrimSpace(node), strings.TrimSpace(trigger.Node)) {
		return "", false
	}
	lowTool := strings.ToLower(strings.TrimSpace(in.ToolName))
	if strings.Contains(lowTool, "read_process_output") {
		pid, ok := numericExitCode(obj["pid"])
		if !ok || !sameInt64(trigger.ProcessIDs, pid) {
			return "", false
		}
		return fmt.Sprintf("node=%s pid=%d", trigger.Node, pid), true
	}
	for _, token := range []string{"get_file_info", "read_file", "read_multiple_files", "list_directory"} {
		if !strings.Contains(lowTool, token) {
			continue
		}
		for _, digest := range toolInputPaths(obj) {
			if sameStringFold(trigger.ArtifactPathSHA256, digest) {
				return "node=" + trigger.Node + " artifact_sha256=" + digest, true
			}
		}
		return "", false
	}
	return "", false
}

func observableTextOutcome(text string) (bool, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return false, false
	}
	low := strings.ToLower(text)
	for _, token := range []string{
		"execution_unknown", "access denied", "permission denied", "target_offline",
		"target_timeout", "timed out", "timeout", " failed", "error:",
	} {
		if strings.Contains(low, token) {
			return false, true
		}
	}
	if strings.Contains(low, "process completed with exit code 0") ||
		strings.Contains(low, "\"status\":\"online\"") ||
		strings.Contains(low, "\"status\": \"online\"") {
		return true, true
	}
	return false, false
}

// Recursively collect positive and negative facts. A parent exit_code=0 or
// isError=false cannot override a failure inside MCP content. Plain text
// without recognizable evidence never counts as success.
func observableToolEvidence(v any, depth int) (positive, negative bool) {
	if depth > 12 {
		return false, true
	}
	switch x := v.(type) {
	case map[string]any:
		for _, key := range []string{"isError", "is_error"} {
			if value, present := x[key]; present {
				flag, ok := value.(bool)
				if !ok || flag {
					negative = true
				}
			}
		}
		if value, ok := x["error"]; ok && value != nil {
			if s, isString := value.(string); !isString || strings.TrimSpace(s) != "" {
				negative = true
			}
		}
		for _, key := range []string{"exit_code", "exitCode", "code"} {
			if value, present := x[key]; present {
				if n, ok := numericExitCode(value); ok {
					if n == 0 {
						positive = true
					} else {
						negative = true
					}
				} else {
					negative = true
				}
			}
		}
		if value, exists := x["status"]; exists {
			status, ok := value.(string)
			if !ok {
				negative = true
			} else {
				switch strings.ToLower(strings.TrimSpace(status)) {
				case "ok", "success", "passed", "pass", "online", "completed", "ready", "recorded":
					positive = true
				case "error", "failed", "fail", "offline", "blocked", "timeout", "execution_unknown":
					negative = true
				}
			}
		}
		for _, key := range []string{"result", "data", "content", "output", "text", "nodes", "results", "files"} {
			if value, exists := x[key]; exists {
				pos, neg := observableToolEvidence(value, depth+1)
				positive = positive || pos
				negative = negative || neg
			}
		}
	case []any:
		for _, item := range x {
			pos, neg := observableToolEvidence(item, depth+1)
			positive = positive || pos
			negative = negative || neg
		}
	case string:
		text := strings.TrimSpace(x)
		if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
			// A malformed embedded JSON document is not positive evidence.
			if !json.Valid([]byte(text)) {
				return false, true
			}
			parsed, err := decodeJSONValue([]byte(text))
			if err != nil {
				return false, true
			}
			return observableToolEvidence(parsed, depth+1)
		}
		if success, known := observableTextOutcome(text); known {
			if success {
				positive = true
			} else {
				negative = true
			}
		}
	}
	return positive, negative
}

func observableToolSuccessValue(v any) (bool, bool) {
	positive, negative := observableToolEvidence(v, 0)
	if negative {
		return false, true
	}
	return positive, positive
}

func observableToolSuccess(raw []byte) bool {
	value, err := decodeJSONValue(raw)
	if err != nil {
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
	trigger, _, triggerFound, err := readSelfLearningTriggerReceipt(in)
	if err != nil {
		return hookResult{}, err
	}

	if startProcessTool(in.ToolName) {
		opKey, node := startProcessOperationIdentity(in.ToolInput)
		if triggerFound && trigger.ConsumedAt == "" && trigger.InvalidatedAt == "" &&
			opKey != "" && trigger.OperationKey == opKey {
			trigger.InvalidatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			trigger.InvalidationReason = "same start_process operation was retried before correlated diagnostic evidence"
			if err := writeSelfLearningTriggerReceipt(triggerPath, trigger); err != nil {
				return hookResult{}, err
			}
			return hookResult{}, nil
		}
		if executionUnknownResponse(rawResponse) && !triggerFound {
			pids, requestIDs, paths := correlationRefs(rawResponse)
			trigger = selfLearningTriggerReceipt{
				Schema: selfLearningTriggerReceiptSchema, Principal: hookPrincipal(in), Session: in.SessionID,
				RunID: selfLearningRunID(in), Kind: "execution_unknown", Rule: rule, ToolName: in.ToolName,
				ToolInputSHA: learnSHA(in.ToolInput), OperationKey: opKey, Node: node,
				ProcessIDs: pids, RequestIDs: requestIDs, ArtifactPathSHA256: paths,
				ResponseSHA: learnSHA(rawResponse), CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			if err := writeSelfLearningTriggerReceipt(triggerPath, trigger); err != nil {
				return hookResult{}, err
			}
		}
		return hookResult{}, nil
	}

	if !triggerFound || trigger.ConsumedAt != "" || trigger.InvalidatedAt != "" ||
		trigger.Kind != "execution_unknown" || trigger.RunID != selfLearningRunID(in) {
		return hookResult{}, nil
	}
	if trigger.Rule.Target != rule.Target || !strings.EqualFold(trigger.Rule.SHA256, rule.SHA256) {
		return hookResult{}, errors.New("self-learning trigger rule does not match the exact current loaded rule")
	}
	if !observableToolSuccess(rawResponse) {
		return hookResult{}, nil
	}
	correlation, ok := correlatedExecutionUnknownDiagnostic(in, trigger)
	if !ok {
		return hookResult{}, nil
	}

	responseSHA := learnSHA(rawResponse)
	outcomeRef := filepath.ToSlash(contextPath) + "#tool_response_sha256=" + responseSHA
	ruleID := rule.Target + "@" + strings.ToLower(rule.SHA256)
	usageRunID := selfLearningRunID(in) + ":used:" + learnSHA([]byte(trigger.CreatedAt + "\n" + in.ToolName + "\n" + correlation + "\n" + responseSHA))[:20]
	res, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "observe", map[string]string{
		"run_id": usageRunID, "kind": "procedure_used",
		"observed": "After EXECUTION_UNKNOWN, correlated machine evidence was inspected before any retry of the same operation.",
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
	trigger.DiagnosticEvidence = correlation
	if err := writeSelfLearningTriggerReceipt(triggerPath, trigger); err != nil {
		return hookResult{}, err
	}
	return hookResult{Context: "AIRWORKER SELF-LEARNING: procedure_used " + ruleID + " outcome=pass"}, nil
}
