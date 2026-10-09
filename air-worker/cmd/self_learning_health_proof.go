package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

// The learned procedure remains guidance, never an executable permission.
// Only a correlated machine readback can attest a safe procedural effect.
const healthProofSchema = "air-worker.self-learning.health-preservation/v1"

type healthProofReceipt struct {
	Schema        string                 `json:"schema"`
	Principal     string                 `json:"principal"`
	Session       string                 `json:"session"`
	RunID         string                 `json:"run_id"`
	Rule          selfLearningLoadedRule `json:"rule"`
	Node          string                 `json:"node"`
	ProcessID     int64                  `json:"process_id"`
	OperationKey  string                 `json:"operation_key"`
	InputSHA      string                 `json:"tool_input_sha256"`
	StartedAt     string                 `json:"started_at"`
	Stage         string                 `json:"stage"`
	ExitCode      int64                  `json:"exit_code,omitempty"`
	FailureSHA    string                 `json:"failure_sha256,omitempty"`
	VerifiedAt    string                 `json:"verified_at,omitempty"`
	InvalidatedAt string                 `json:"invalidated_at,omitempty"`
	ConsultedAt   string                 `json:"consulted_at,omitempty"`
	OutcomeRef    string                 `json:"outcome_ref,omitempty"`
}

// Match the actual safe procedure sections, not its filename or an LLM claim.
// Unsupported or ambiguous procedures remain loaded-only (NOT_PROVEN).
func healthPreservationRule(owner selfLearningOwner, rec selfLearningContextReceipt) (selfLearningLoadedRule, bool) {
	if !sameLearningPath(owner.Selector.ProductRoot, rec.ProductRoot) ||
		!sameLearningPath(owner.Settings.RuntimeRoot, rec.RuntimeRoot) {
		return selfLearningLoadedRule{}, false
	}
	var found selfLearningLoadedRule
	for _, rule := range rec.Loaded {
		if !currentSelfLearningRuleBytes(owner, rule) {
			continue
		}
		b, err := readLearningBounded(filepath.Join(owner.Selector.ProductRoot, filepath.FromSlash(rule.Target)), 64*1024)
		if err != nil {
			continue
		}
		s := strings.ToLower(string(b))
		before, after, ok := strings.Cut(s, "## when to apply")
		if !ok || !strings.Contains(before, "# ") {
			continue
		}
		when, after, ok := strings.Cut(after, "## procedure")
		if !ok {
			continue
		}
		procedure, _, ok := strings.Cut(after, "## pitfalls")
		if !ok {
			continue
		}
		if !strings.Contains(when, "release validation") ||
			!strings.Contains(when, "health check") ||
			!strings.Contains(when, "nonzero") ||
			!(strings.Contains(when, "times out") || strings.Contains(when, "timeout")) ||
			!strings.Contains(procedure, "preserve") ||
			!strings.Contains(procedure, "receipt") ||
			!strings.Contains(procedure, "unmet") {
			continue
		}
		// Never attribute one observed operation to two possible procedures.
		if found.Target != "" {
			return selfLearningLoadedRule{}, false
		}
		found = rule
	}
	return found, found.Target != ""
}

func healthProofPath(in hookInput) (string, error) {
	dir, err := selfLearningSessionDir(in)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "health-preservation-"+learnSHA([]byte(selfLearningRunID(in)))[:24]+".json"), nil
}

func readHealthProof(in hookInput) (healthProofReceipt, string, bool, error) {
	path, err := healthProofPath(in)
	if err != nil {
		return healthProofReceipt{}, "", false, err
	}
	var proof healthProofReceipt
	err = readJSON(path, &proof)
	if errors.Is(err, os.ErrNotExist) {
		return proof, path, false, nil
	}
	if err != nil {
		return proof, path, true, err
	}
	if proof.Schema != healthProofSchema ||
		proof.Principal != hookPrincipal(in) ||
		proof.Session != in.SessionID ||
		proof.RunID != selfLearningRunID(in) ||
		proof.Rule.Target == "" || len(proof.Rule.SHA256) != 64 ||
		proof.OperationKey == "" || proof.Node == "" || proof.ProcessID <= 0 {
		return proof, path, true, errors.New("invalid machine health preservation proof identity")
	}
	return proof, path, true, nil
}

func writeHealthProof(path string, proof healthProofReceipt) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomicDurable(path, append(b, '\n'))
}

var nativeHeadroomProbe = regexp.MustCompile(`(?i)\bair-worker(?:\.exe)?\b[^\r\n;]*\btool\s+-which\s+headroom\b`)

func headroomProbeCommand(in hookInput) (string, string, bool) {
	if !startProcessTool(in.ToolName) {
		return "", "", false
	}
	key, node := startProcessOperationIdentity(in.ToolInput)
	obj := toolInputObject(in.ToolInput)
	if key == "" || node == "" || obj == nil {
		return "", "", false
	}
	command, _ := obj["command"].(string)
	if !nativeHeadroomProbe.MatchString(command) {
		return "", "", false
	}
	return key, node, true
}

// Decode host result envelopes before inspecting child output. Applying a
// regexp to raw JSON misses the word boundary in an escaped "\nProcess".
func healthProcessTexts(value any, depth int) ([]string, bool) {
	if depth > 10 {
		return nil, false
	}
	switch x := value.(type) {
	case map[string]any:
		for _, key := range []string{"isError", "is_error"} {
			if v, ok := x[key]; ok && v != false {
				return nil, false
			}
		}
		if e, exists := x["error"]; exists && e != nil && e != "" {
			return nil, false
		}
		var texts []string
		for _, key := range []string{"text", "content", "output", "stdout", "result", "data"} {
			if child, exists := x[key]; exists {
				chunk, ok := healthProcessTexts(child, depth+1)
				if !ok {
					return nil, false
				}
				texts = append(texts, chunk...)
			}
		}
		return texts, true
	case []any:
		var texts []string
		for _, part := range x {
			chunk, ok := healthProcessTexts(part, depth+1)
			if !ok {
				return nil, false
			}
			texts = append(texts, chunk...)
		}
		return texts, true
	case string:
		trimmed := strings.TrimSpace(x)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			decoded, err := decodeJSONValue([]byte(trimmed))
			if err != nil {
				return nil, false
			}
			return healthProcessTexts(decoded, depth+1)
		}
		return []string{x}, true
	default:
		return nil, true
	}
}

// Transport code 0 never overrides a nonzero child completion. A health
// timeout AND one consistent nonzero completion in a real text field are
// required; no status field or generic positive text can attest use.
func verifiedHeadroomHealthFailure(raw []byte) (int64, bool) {
	decoded, err := decodeJSONValue(raw)
	if err != nil {
		return 0, false
	}
	texts, ok := healthProcessTexts(decoded, 0)
	if !ok || len(texts) == 0 {
		return 0, false
	}
	s := strings.ToLower(strings.Join(texts, "\n"))
	if !strings.Contains(s, "headroom health") ||
		!(strings.Contains(s, "context deadline exceeded") ||
			strings.Contains(s, "timed out") || strings.Contains(s, "timeout")) {
		return 0, false
	}
	found := completedProcessExitCode.FindAllStringSubmatch(s, -1)
	if len(found) == 0 {
		return 0, false
	}
	var code int64
	for i, match := range found {
		v, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || v == 0 || (i > 0 && v != code) {
			return 0, false
		}
		code = v
	}
	return code, true
}

// An actual read_file must return this exact current machine document,
// not merely an unrelated success, a copied positive string or an error.
func validMachineReadback(raw []byte, proof healthProofReceipt, fileBytes []byte) bool {
	var env map[string]json.RawMessage
	if !json.Valid(raw) || json.Unmarshal(raw, &env) != nil {
		return false
	}
	for _, name := range []string{"isError", "is_error"} {
		if v, ok := env[name]; ok && strings.TrimSpace(string(v)) != "false" {
			return false
		}
	}
	if v, ok := env["error"]; ok {
		s := strings.TrimSpace(string(v))
		if s != "null" && s != `""` {
			return false
		}
	}
	var content string
	for _, key := range []string{"text", "content"} {
		if v, ok := env[key]; ok {
			content += string(v)
		}
	}
	if content == "" {
		return false
	}
	for _, token := range []string{
		proof.OperationKey, proof.FailureSHA, proof.Rule.SHA256,
		healthProofSchema, `"stage"`, `"verified_failure"`,
	} {
		if !strings.Contains(content, token) &&
			!strings.Contains(content, strings.ReplaceAll(token, `"`, `\"`)) {
			return false
		}
	}
	var onDisk healthProofReceipt
	return json.Unmarshal(fileBytes, &onDisk) == nil && onDisk == proof
}

// Only host lifecycle evidence, not words in a model's answer, may reach
// procedure_used. The new observer never mutates LEARN journals directly.
func handleHealthProcedureEvidence(owner selfLearningOwner, rec selfLearningContextReceipt, contextPath string, in hookInput) (hookResult, bool, error) {
	rule, supported := healthPreservationRule(owner, rec)
	if !supported {
		return hookResult{}, false, nil
	}
	proof, proofPath, exists, err := readHealthProof(in)
	if err != nil {
		return hookResult{}, true, err
	}
	raw := hookResponseBytes(in)
	if len(raw) == 0 {
		return hookResult{}, false, nil
	}
	if key, node, isProbe := headroomProbeCommand(in); isProbe {
		if exists && proof.OperationKey == key && proof.InvalidatedAt == "" && proof.ConsultedAt == "" {
			proof.InvalidatedAt = time.Now().UTC().Format(time.RFC3339Nano)
			return hookResult{}, true, writeHealthProof(proofPath, proof)
		}
		if exists {
			return hookResult{}, false, nil
		}
		pids, _, _ := correlationRefs(raw)
		if len(pids) != 1 {
			return hookResult{}, true, nil
		}
		proof = healthProofReceipt{
			Schema: healthProofSchema, Principal: hookPrincipal(in), Session: in.SessionID,
			RunID: selfLearningRunID(in), Rule: rule, Node: node, ProcessID: pids[0],
			OperationKey: key, InputSHA: learnSHA(in.ToolInput),
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano), Stage: "probe_started",
		}
		return hookResult{}, true, writeHealthProof(proofPath, proof)
	}
	if !exists || proof.InvalidatedAt != "" || proof.ConsultedAt != "" ||
		proof.Rule != rule || !currentSelfLearningRuleBytes(owner, rule) {
		return hookResult{}, false, nil
	}
	if strings.Contains(strings.ToLower(in.ToolName), "read_process_output") &&
		proof.Stage == "probe_started" {
		obj := toolInputObject(in.ToolInput)
		pid, ok := numericExitCode(obj["pid"])
		node, _ := obj["node"].(string)
		if !ok || pid != proof.ProcessID || !strings.EqualFold(node, proof.Node) {
			return hookResult{}, true, nil
		}
		code, measured := verifiedHeadroomHealthFailure(raw)
		if !measured {
			return hookResult{}, true, nil
		}
		proof.Stage, proof.ExitCode, proof.FailureSHA = "verified_failure", code, learnSHA(raw)
		proof.VerifiedAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := writeHealthProof(proofPath, proof); err != nil {
			return hookResult{}, true, err
		}
		return hookResult{Context: "AIRWORKER LEARNED PROCEDURE: health check FAILED (not recovered). " +
			"Preserve nonzero verdict; before any release claim read the machine receipt using " +
			"read_file on the same node: " + proofPath + ". Current procedure " +
			rule.Target + "@" + rule.SHA256}, true, nil
	}
	if strings.Contains(strings.ToLower(in.ToolName), "read_file") && proof.Stage == "verified_failure" {
		obj := toolInputObject(in.ToolInput)
		if obj == nil {
			return hookResult{}, true, nil
		}
		node, _ := obj["node"].(string)
		path, _ := obj["path"].(string)
		if !strings.EqualFold(node, proof.Node) || !sameLearningPath(path, proofPath) {
			return hookResult{}, true, nil
		}
		fileBytes, err := readLearningBounded(proofPath, 64*1024)
		if err != nil || !validMachineReadback(raw, proof, fileBytes) {
			return hookResult{}, true, nil
		}
		outcomeRef := filepath.ToSlash(proofPath) + "#failure_sha256=" + proof.FailureSHA
		ruleID := rule.Target + "@" + strings.ToLower(rule.SHA256)
		useRun := selfLearningRunID(in) + ":used-health:" +
			learnSHA([]byte(proof.OperationKey + "\n" + proof.FailureSHA + "\n" + ruleID))[:20]
		result, err := executeSharedLearning(owner.Selector.ProductRoot, owner.Settings, "observe", map[string]string{
			"run_id": useRun, "kind": "procedure_used", "class": "release-health-timeout-preserved",
			"source": "host-post-tool", "principal": hookPrincipal(in), "session": in.SessionID,
			"rule_id": ruleID, "outcome": "pass", "outcome_ref": outcomeRef,
			"observed": "Health timeout retained as failed release criterion; no service repair or successful health claimed.",
		})
		if err != nil && !(errors.Is(err, learning.ErrConflict) && result.Status == "duplicate") {
			return hookResult{}, true, err
		}
		proof.Stage, proof.ConsultedAt, proof.OutcomeRef =
			"consulted", time.Now().UTC().Format(time.RFC3339Nano), outcomeRef
		if err := writeHealthProof(proofPath, proof); err != nil {
			return hookResult{}, true, err
		}
		rec.UsedAt, rec.UsedRuleID = proof.ConsultedAt, ruleID
		rec.Outcome, rec.OutcomeRef, rec.ObservedTool = "pass", outcomeRef, in.ToolName
		if err := writeSelfLearningContextReceipt(contextPath, rec); err != nil {
			return hookResult{}, true, err
		}
		return hookResult{Context: fmt.Sprintf("AIRWORKER SELF-LEARNING: procedure_used %s outcome=pass (health FAILURE preserved, not repaired)", ruleID)}, true, nil
	}
	return hookResult{}, false, nil
}
