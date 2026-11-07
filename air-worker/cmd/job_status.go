package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// job status is a read-only recovery path for a deterministic owned job identity.
// It does not restart or finalize a RUNNING job and does not infer success from
// transport timeout or from the mere existence of an output file.
type jobStatusSnapshot struct {
	Schema       string `json:"schema"`
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
	Terminal     bool   `json:"terminal"`
	Product      string `json:"product"`
	Principal    string `json:"principal"`
	SessionKey   string `json:"session_key"`
	Step         string `json:"step"`
	Operation    string `json:"operation"`
	ReceiptPath  string `json:"receipt_path"`
	OutputPath   string `json:"output_path"`
	OutputSHA256 string `json:"output_sha256,omitempty"`
	OutputBytes  int64  `json:"output_bytes,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

func jobOwnedEvidencePath(root, path string) error {
	cleanRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	// Reject links and reparse ancestors that resolve outside this product.
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(cleanRoot, real)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("job evidence escapes product root")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("job evidence must be an ordinary file")
	}
	return nil
}

func inspectOwnedJob(scope sessionScope, step, operation string) (jobStatusSnapshot, error) {
	s := jobStatusSnapshot{Schema: "air-worker.job-status/v1", Product: scope.Root,
		Principal: scope.ID.Principal, SessionKey: scope.ID.SessionKey,
		Step: step, Operation: operation}
	if len(step) < 1 || len(step) > 200 || !reIdentityPart.MatchString(step) ||
		len(operation) < 1 || len(operation) > 200 || !reIdentityPart.MatchString(operation) {
		return s, errors.New("step and operation must use bounded safe native identifiers")
	}
	receiptPath, outputPath := jobReceiptPaths(scope, step, operation)
	s.ReceiptPath, s.OutputPath = receiptPath, outputPath
	if err := jobOwnedEvidencePath(scope.Root, receiptPath); err != nil {
		return s, fmt.Errorf("receipt not verified: %w", err)
	}
	r, err := readJobReceipt(receiptPath)
	if err != nil {
		return s, fmt.Errorf("receipt unreadable: %w", err)
	}
	if r.JobID == "" || r.Product != scope.Root || r.Principal != scope.ID.Principal ||
		r.Session != scope.ID.SessionKey || r.Step != step || r.Operation != operation ||
		filepath.Clean(r.OutputPath) != filepath.Clean(outputPath) {
		return s, errors.New("receipt identity does not match requested owned job")
	}
	if r.Status != jobStatusRunning && r.Status != jobStatusDone && r.Status != jobStatusFailed {
		return s, errors.New("unknown job receipt status")
	}
	s.JobID, s.Status = r.JobID, r.Status
	s.Terminal = r.Status == jobStatusDone || r.Status == jobStatusFailed
	if !s.Terminal {
		s.Reason = "RUNNING is not terminal; read this identity again later, do not redispatch"
		return s, nil
	}
	if err := jobOwnedEvidencePath(scope.Root, outputPath); err != nil {
		return s, fmt.Errorf("terminal output not verified: %w", err)
	}
	f, err := os.Open(outputPath)
	if err != nil {
		return s, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > (32<<20) {
		return s, errors.New("terminal output is absent, not regular or exceeds 32MiB")
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil || n != info.Size() {
		return s, errors.New("terminal output could not be read completely")
	}
	s.OutputSHA256, s.OutputBytes = hex.EncodeToString(h.Sum(nil)), n
	if r.OutputSHA256 == "" {
		s.OutputSHA256, s.OutputBytes = "", 0
		s.Reason = "terminal output has no completion seal; historical result is unproven"
		return s, nil
	}
	if s.OutputSHA256 != r.OutputSHA256 || s.OutputBytes != r.OutputBytes {
		return s, errors.New("terminal output changed after completion seal")
	}
	return s, nil
}

func cmdJob(argv []string) int {
	if len(argv) == 0 || argv[0] != "status" {
		fmt.Fprintln(os.Stderr, "usage: air-worker job status -product <root> -principal <p> -session-key <k> -step <id> -operation <id> [-json]")
		return 2
	}
	fs := flag.NewFlagSet("job status", flag.ContinueOnError)
	product := fs.String("product", ".", "product root")
	principal := fs.String("principal", "", "declared principal")
	sessionKey := fs.String("session-key", "", "declared session key")
	step := fs.String("step", "", "exact native step")
	operation := fs.String("operation", "", "exact native operation")
	asJSON := fs.Bool("json", false, "machine-readable JSON")
	if err := fs.Parse(argv[1:]); err != nil || fs.NArg() != 0 {
		return 2
	}
	root, err := normalizePlanNodeProduct(*product)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	scope, err := executorScopeForSession(root, *principal, *sessionKey, "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	snapshot, err := inspectOwnedJob(scope, *step, *operation)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(snapshot)
	} else {
		fmt.Printf("job %s status=%s terminal=%t log_sha256=%s%s", snapshot.JobID, snapshot.Status,
			snapshot.Terminal, snapshot.OutputSHA256, lineEnding)
	}
	return 0
}
