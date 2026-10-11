package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// A small typed precondition classifier can safely provide evidence missing
// from a bare nonzero exit code. It does not parse stderr/model prose or claim
// a causal repair; it observes the filesystem after the native CLI returns.
func ownCLITrustedFailureCause(argv []string, rc int) string {
	if rc != 2 || len(argv) < 2 {
		return ""
	}
	if argv[0] == "judge" {
		return ownCLITrustedJudgeFailureCause(argv)
	}
	if argv[0] != "validate" {
		return ""
	}
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	product := fs.String("product", ".", "")
	_ = fs.Bool("json", false, "")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 {
		return ""
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		return ""
	}
	st, err := os.Lstat(root)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	config := filepath.Join(root, "run-config.json")
	st, err = os.Lstat(config)
	if os.IsNotExist(err) {
		return "validate-run-config-missing"
	}
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	raw, err := readLearningBounded(config, 256*1024)
	if err != nil {
		return ""
	}
	var cfg struct {
		Plan string `json:"plan"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	// Match the canonical nativePlanPath normalization before comparing
	// filesystem facts. A whitespace-only plan resolves to PLAN.md.
	plan := strings.TrimSpace(cfg.Plan)
	if plan == "" {
		plan = "PLAN.md"
	}
	if !filepath.IsLocal(plan) {
		return ""
	}
	st, err = os.Lstat(filepath.Join(root, plan))
	if os.IsNotExist(err) {
		return "validate-plan-missing"
	}
	if err != nil || !st.Mode().IsRegular() {
		return ""
	}
	return ""
}

func ownCLIFailureObservation(action string, rc int, cause string) string {
	text := fmt.Sprintf("Native AirWorker CLI action=%s ended with exit_code=%d.", action, rc)
	switch cause {
	case "validate-run-config-missing":
		text += " Trusted read-only precondition at native capture: the product directory exists but run-config.json is missing. Before reattempting validation, locate the canonical product root and verify its existing configuration; never invent or silently replace it."
	case "validate-plan-missing":
		text += " Trusted read-only precondition at native capture: a readable run-config.json exists but the configured PLAN file is missing. Before reattempting validation, locate the canonical plan and preserve actual machine evidence; never create a fabricated plan."
	case "judge-run-config-missing":
		text += " Trusted read-only precondition at native judge capture: the product directory exists but run-config.json is missing. Before reattempting judgment, locate the canonical existing configuration; never invent or silently replace it."
	case "judge-plan-missing":
		text += " Trusted read-only precondition at native judge capture: run-config.json exists but the configured PLAN file is missing. Before reattempting judgment, locate the verified canonical plan; never create a fabricated plan."
	default:
		text += " No specific precondition is independently verified by the native code."
	}
	return text + " No successful repair, procedure use or effect is claimed."
}

// A distinct domain binds the entire factual receipt, including its safe
// observation and original native failure classification. Replays may not
// change those fields even when the underlying product later changes.
func ownCLIReceiptMAC(key []byte, rec ownCLIFailureReceipt) string {
	rec.ReceiptMAC = ""
	raw, _ := json.Marshal(rec)
	h := hmac.New(sha256.New, key)
	_, _ = h.Write([]byte("air-worker.cli.failure-receipt/v3\x00"))
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil))
}

// ownCLITrustedJudgeFailureCause is deliberately independent of model text
// and stderr. Native cmdJudge returns rc2 when canonical run-config cannot be
// read or the PLAN contract is unavailable. A snapshot of an OS-verified
// precondition is NOT a claim that this is the sole root cause or a repair.
//
// An explicitly overridden -config can point outside the product, so such
// calls get NO typed trusted cause. Any unknown option/stray argv, symlink,
// reparse point, unresolved parent, or untrusted path fails closed.
func ownCLITrustedJudgeFailureCause(argv []string) string {
	if len(argv) < 2 || len(argv) > 32 || argv[0] != "judge" {
		return ""
	}
	fs := flag.NewFlagSet("judge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	product := fs.String("product", ".", "")
	custom := fs.String("config", "", "")
	_ = fs.Int("min-facts", -1, "")
	_ = fs.Bool("json", false, "")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 || *custom != "" {
		return ""
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		return ""
	}
	ordinaryRoot, err := os.Lstat(root)
	if err != nil || !ordinaryRoot.IsDir() || ordinaryRoot.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	if _, err := os.Readlink(root); err == nil {
		return ""
	}
	configPath := filepath.Join(root, "run-config.json")
	configInfo, err := os.Lstat(configPath)
	if os.IsNotExist(err) {
		return "judge-run-config-missing"
	}
	if err != nil || !configInfo.Mode().IsRegular() ||
		configInfo.Mode()&os.ModeSymlink != 0 {
		return ""
	}
	if _, err := os.Readlink(configPath); err == nil {
		return ""
	}
	raw, err := readLearningBounded(configPath, 256*1024)
	if err != nil {
		return ""
	}
	var cfg runConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ""
	}
	// cmdJudge calls requirePlan -> planFilePath; that path uses exactly
	// cfg.Plan (NOT the whitespace-trimming nativePlanPath helper).
	plan := cfg.Plan
	if plan == "" {
		plan = "PLAN.md"
	}
	if strings.TrimSpace(plan) == "" || !filepath.IsLocal(plan) {
		return ""
	}
	plan = filepath.Clean(plan)
	if !filepath.IsLocal(plan) {
		return ""
	}
	// Check each intermediate directory so a configured nested plan does
	// not silently follow a symlink/junction outside the trusted root.
	elements := strings.Split(plan, string(filepath.Separator))
	current := root
	for idx, element := range elements {
		if element == "" || element == "." || element == ".." {
			return ""
		}
		current = filepath.Join(current, element)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if idx == len(elements)-1 {
				return "judge-plan-missing"
			}
			return ""
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return ""
		}
		if _, err := os.Readlink(current); err == nil {
			return ""
		}
		if idx < len(elements)-1 && !info.IsDir() {
			return ""
		}
		if idx == len(elements)-1 && !info.Mode().IsRegular() {
			return ""
		}
	}
	return ""
}
