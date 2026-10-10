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
)

// A small typed precondition classifier can safely provide evidence missing
// from a bare nonzero exit code. It does not parse stderr/model prose or claim
// a causal repair; it observes the filesystem after the native CLI returns.
func ownCLITrustedFailureCause(argv []string, rc int) string {
	if rc != 2 || len(argv) < 2 || argv[0] != "validate" {
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
	plan := cfg.Plan
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
