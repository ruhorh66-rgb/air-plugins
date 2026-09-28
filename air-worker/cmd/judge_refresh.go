package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const judgeRefreshSchema = "air-worker.judge.refresh/v1"

type judgeRefreshView struct {
	Schema           string `json:"schema"`
	Refreshed        bool   `json:"refreshed"`
	Reason           string `json:"reason"`
	Code             int    `json:"code"`
	At               string `json:"at,omitempty"`
	GitHead          string `json:"git_head,omitempty"`
	InputFingerprint string `json:"input_fingerprint,omitempty"`
}

var judgeRefreshNow = time.Now

func cachedVerdictNeedsRefresh(root string, cfg runConfig, now time.Time) (machineVerdict, string) {
	var mv machineVerdict
	if err := readJSON(filepath.Join(root, ".goal-verdict.json"), &mv); err != nil {
		return mv, "machine verdict missing"
	}
	if err := machineVerdictFreshness(root, mv, now); err != nil {
		return mv, err.Error()
	}
	current := judgeInputFingerprint(root, cfg, filepath.Join(root, "run-config.json"), nativePlanPath(root, cfg))
	if strings.TrimSpace(mv.InputFingerprint) == "" {
		return mv, "machine verdict has no input_fingerprint"
	}
	if !strings.EqualFold(strings.TrimSpace(mv.InputFingerprint), current) {
		return mv, fmt.Sprintf("machine verdict fingerprint %s != current %s", mv.InputFingerprint, current)
	}
	return mv, ""
}

func ensureFreshMachineVerdict(root string) (judgeRefreshView, error) {
	view := judgeRefreshView{Schema: judgeRefreshSchema, Code: 2}
	var cfg runConfig
	cfgPath := filepath.Join(root, "run-config.json")
	if err := readJSON(cfgPath, &cfg); err != nil {
		view.Reason = "run-config unavailable: " + err.Error()
		return view, nil
	}
	req := checkPlanRequirement(root, cfg)
	if !req.OK() {
		view.Reason = "plan is not measurable: " + strings.Join(req.Problems, "; ")
		return view, nil
	}
	now := judgeRefreshNow()
	mv, reason := cachedVerdictNeedsRefresh(root, cfg, now)
	if reason == "" {
		view.Code = mv.Code
		view.At = mv.At
		view.GitHead = mv.GitHead
		view.InputFingerprint = mv.InputFingerprint
		view.Reason = "fresh"
		return view, nil
	}

	res := runJudge(root, cfg, -1, legacyScope(root))
	res.InputFingerprint = judgeInputFingerprint(root, cfg, cfgPath, nativePlanPath(root, cfg))
	code, text := verdict(res)
	mv = publishVerdict(root, code, text, res)
	var stored machineVerdict
	if err := readJSON(filepath.Join(root, ".goal-verdict.json"), &stored); err != nil {
		return view, fmt.Errorf("publish refreshed verdict: %w", err)
	}
	view.Refreshed = true
	view.Reason = reason
	view.Code = stored.Code
	view.At = stored.At
	view.GitHead = stored.GitHead
	view.InputFingerprint = stored.InputFingerprint
	return view, nil
}

var gitCommitCommandRE = regexp.MustCompile(`(?i)(^|[;&|]\s*)git(?:\.exe)?(?:\s+-C\s+(?:"[^"]+"|'[^']+'|\S+))?\s+commit(?:\s|$)`)

func hookShellCommand(in hookInput) (string, bool) {
	tool := strings.ToLower(strings.TrimSpace(in.ToolName))
	if tool != "bash" && tool != "powershell" {
		return "", false
	}
	var body struct {
		Command string `json:"command"`
	}
	if len(in.ToolInput) == 0 || json.Unmarshal(in.ToolInput, &body) != nil {
		return "", false
	}
	return strings.TrimSpace(body.Command), true
}

func handlePostToolUseJudgeRefresh(in hookInput) (hookResult, error) {
	command, ok := hookShellCommand(in)
	if !ok || !gitCommitCommandRE.MatchString(command) {
		return hookResult{}, nil
	}
	product, ok := productForLearningHookInput(in)
	if !ok {
		return hookResult{}, nil
	}
	view, err := ensureFreshMachineVerdict(product)
	if err != nil {
		return hookResult{}, err
	}
	return hookResult{Context: fmt.Sprintf("AIRWORKER FACTUAL JUDGE: refreshed=%t code=%d reason=%s", view.Refreshed, view.Code, view.Reason)}, nil
}

func mergeHookContexts(a, b hookResult) hookResult {
	var parts []string
	for _, value := range []string{a.Context, b.Context} {
		if strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	return hookResult{Block: a.Block || b.Block, Reason: strings.TrimSpace(strings.Join([]string{a.Reason, b.Reason}, " ")), Context: strings.Join(parts, "\n")}
}

func handlePostToolUse(in hookInput) (hookResult, error) {
	lint, err := handlePostToolUsePlanLint(in)
	if err != nil {
		return hookResult{}, err
	}
	refresh, err := handlePostToolUseJudgeRefresh(in)
	if err != nil {
		return hookResult{}, err
	}
	return mergeHookContexts(lint, refresh), nil
}
