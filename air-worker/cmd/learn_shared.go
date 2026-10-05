package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ruhorh66-rgb/air-modules/learning"
)

const sharedLearningConfigFile = "learning-module.json"
const sharedLearningMaxBytes = 256 * 1024

// This configuration selects ONE writer for a product. Its presence never migrates
// historical state or switches an unrelated installed product to the new module.
type sharedLearningSettings struct {
	Schema             string                  `json:"schema"`
	ProductID          string                  `json:"product_id"`
	RuntimeRoot        string                  `json:"runtime_root"`
	ManagedSkillPrefix string                  `json:"managed_skill_prefix"`
	ProtectedTargets   []string                `json:"protected_targets,omitempty"`
	TimeoutMS          int                     `json:"timeout_ms,omitempty"`
	Reviewer           *learningProcessAdapter `json:"reviewer,omitempty"`
	Judge              *learningProcessAdapter `json:"judge,omitempty"`
	VerifyGrant        *learningProcessAdapter `json:"verify_grant,omitempty"`
	DeliverSummary     *learningProcessAdapter `json:"deliver_summary,omitempty"`
}

type learningProcessAdapter struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
	SHA256     string   `json:"sha256"`
	TimeoutMS  int      `json:"timeout_ms,omitempty"`
}

func readSharedLearningSettings(product string) (sharedLearningSettings, bool, error) {
	var s sharedLearningSettings
	b, err := readLearningBounded(filepath.Join(product, sharedLearningConfigFile), sharedLearningMaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return s, false, nil
	}
	if err != nil {
		return s, true, err
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, true, err
	}
	if s.Schema != "air-worker.shared-learning/v1" || s.ProductID == "" || !filepath.IsAbs(s.RuntimeRoot) || !strings.HasPrefix(s.ManagedSkillPrefix, "skills/") {
		return s, true, fmt.Errorf("invalid %s: explicit product identity, absolute runtime root and managed skill subtree required", sharedLearningConfigFile)
	}
	// A configuration file is not approval to disable previously active controls.
	if err := inspectLegacyLearningRules(product, os.Lstat, os.ReadDir); err != nil {
		return s, true, err
	}
	return s, true, nil
}

// Injected filesystem functions let tests exercise permission/I/O failures on
// Windows without changing the host ACLs. Production always uses the OS calls.
func inspectLegacyLearningRules(product string, stat func(string) (os.FileInfo, error), readDir func(string) ([]os.DirEntry, error)) error {
	info, err := inspectLegacyLearningPath(product, filepath.Join(product, ".air-worker", "learn", "RULES.md"), stat)
	if err != nil {
		return fmt.Errorf("cannot inspect legacy RULES.md: %w", err)
	}
	if info != nil && (!info.Mode().IsRegular() || info.Size() > 0) {
		return errors.New("active or non-regular legacy learned rules require explicit reconciliation before shared-mode cutover")
	}
	dir := filepath.Join(product, "learn", "rules")
	info, err = inspectLegacyLearningPath(product, dir, stat)
	if err != nil {
		return fmt.Errorf("cannot inspect legacy executable-rule directory: %w", err)
	}
	if info == nil {
		return nil
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("legacy executable-rule path is not an ordinary directory")
	}
	entries, err := readDir(dir)
	// Once Stat proved presence, even an ErrNotExist race is not proof of a
	// reconciled empty directory. In particular Windows can report NOT_FOUND for
	// ReadDir on a regular file or a path obstructed by a non-directory ancestor.
	if err != nil {
		return fmt.Errorf("cannot read legacy executable rules: %w", err)
	}
	if len(entries) > 0 {
		return errors.New("legacy executable-rule directory requires explicit reconciliation before shared-mode cutover")
	}
	return nil
}

func inspectLegacyLearningPath(root, path string, stat func(string) (os.FileInfo, error)) (os.FileInfo, error) {
	root = filepath.Clean(root)
	var target os.FileInfo
	for p := path; ; p = filepath.Dir(p) {
		info, err := stat(p)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if err == nil {
			if p == path {
				target = info
			} else if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("legacy path has non-directory or linked ancestor: %s", p)
			}
		}
		if p == root {
			return target, nil
		}
		if filepath.Dir(p) == p {
			return nil, errors.New("legacy path is outside product root")
		}
	}
}

func readLearningBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, fmt.Errorf("learning input is not a bounded regular file: %s", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("learning input exceeds byte limit")
	}
	return b, nil
}

type learningBoundedOutput struct {
	buffer   bytes.Buffer
	overflow bool
}

func (b *learningBoundedOutput) Len() int      { return b.buffer.Len() }
func (b *learningBoundedOutput) Bytes() []byte { return b.buffer.Bytes() }

func (b *learningBoundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := sharedLearningMaxBytes - b.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	if len(p) > 0 {
		_, _ = b.buffer.Write(p)
	}
	return n, nil
}

// Adapters consume evidence as JSON stdin, never as a shell command. The configured
// executable is release-owned and hash-pinned; a model's packet cannot select it.
func runLearningProcess(ctx context.Context, a *learningProcessAdapter, input json.RawMessage) (json.RawMessage, error) {
	if len(input) > sharedLearningMaxBytes {
		return nil, errors.New("learning adapter input exceeds byte limit")
	}
	if a == nil {
		return nil, errors.New("learning adapter not configured")
	}
	if !filepath.IsAbs(a.Executable) || len(a.SHA256) != 64 {
		return nil, errors.New("learning adapter must have an absolute executable and SHA-256")
	}
	f, err := os.Open(a.Executable)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, hashErr := io.Copy(h, f)
	closeErr := f.Close()
	if hashErr != nil {
		return nil, hashErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), a.SHA256) {
		return nil, errors.New("learning adapter executable SHA mismatch")
	}
	ms := a.TimeoutMS
	if ms <= 0 || ms > 120000 {
		ms = 120000
	}
	childCtx, cancel := context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
	defer cancel()
	cmd := exec.CommandContext(childCtx, a.Executable, a.Args...)
	cmd.Stdin = bytes.NewReader(input)
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr learningBoundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if childCtx.Err() != nil {
		return nil, childCtx.Err()
	}
	if err != nil {
		return nil, fmt.Errorf("learning adapter failed: %w", err)
	}
	if stdout.overflow || stderr.overflow {
		return nil, errors.New("learning adapter output exceeds byte limit")
	}
	b := bytes.TrimSpace(stdout.Bytes())
	if !json.Valid(b) {
		return nil, errors.New("learning adapter returned invalid JSON")
	}
	return append(json.RawMessage(nil), b...), nil
}

func sharedLearningConfig(product string, s sharedLearningSettings) learning.Config {
	c := learning.Config{ProductID: s.ProductID, RuntimeRoot: s.RuntimeRoot, GitRoot: product, ManagedSkillPrefix: s.ManagedSkillPrefix, ProtectedTargets: s.ProtectedTargets, ReviewTimeoutMS: s.TimeoutMS}
	if s.Judge != nil {
		c.Judge = func(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
			return runLearningProcess(ctx, s.Judge, b)
		}
	}
	if s.Reviewer != nil {
		c.Reviewer = func(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
			return runLearningProcess(ctx, s.Reviewer, b)
		}
	}
	if s.VerifyGrant != nil {
		c.VerifyGrant = func(ctx context.Context, b json.RawMessage) error {
			answer, err := runLearningProcess(ctx, s.VerifyGrant, b)
			if err != nil {
				return err
			}
			var requested, checked map[string]any
			if json.Unmarshal(b, &requested) != nil || json.Unmarshal(answer, &checked) != nil || checked["valid"] != true {
				return learning.ErrInvalidGrant
			}
			for _, key := range []string{"proposal_id", "diff_sha256", "decision", "channel_ref"} {
				if requested[key] == nil || checked[key] != requested[key] {
					return learning.ErrInvalidGrant
				}
			}
			return nil
		}
	}
	if s.DeliverSummary != nil {
		c.DeliverSummary = func(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
			answer, err := runLearningProcess(ctx, s.DeliverSummary, b)
			if err != nil {
				return nil, err
			}
			var ack struct {
				ChannelRef string `json:"channel_ref"`
				MessageID  string `json:"message_id"`
			}
			if json.Unmarshal(answer, &ack) != nil || ack.ChannelRef == "" || ack.MessageID == "" {
				return nil, errors.New("summary delivery lacks channel/message readback")
			}
			return answer, nil
		}
	}
	return c
}

func executeSharedLearning(product string, s sharedLearningSettings, op string, data any) (learning.Response, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return learning.Response{}, err
	}
	return learning.Execute(context.Background(), sharedLearningConfig(product, s), learning.Request{Operation: op, Data: b, Version: learning.Version})
}

func sharedLearningEvent(product string, s sharedLearningSettings, runID, kind, observed, class, source, principal, session, evidence, outcome string) (learning.Response, error) {
	if runID == "" {
		return learning.Response{}, errors.New("stable run_id is required")
	}
	return executeSharedLearning(product, s, "observe", map[string]string{"run_id": runID, "kind": kind, "observed": observed, "class": class, "source": source, "principal": principal, "session": session, "outcome_ref": evidence, "outcome": outcome})
}

func appendSharedPlanEvents(product string, rows []map[string]any) (bool, error) {
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on {
		return on, err
	}
	for _, entry := range rows {
		text, _ := entry["observed"].(string)
		id, _ := entry["id"].(string)
		actor, _ := entry["actor"].(string)
		evidence, _ := entry["evidence"].(string)
		kind := "observation"
		if strings.HasPrefix(text, "closed ") {
			kind = "run_completed"
		}
		res, err := sharedLearningEvent(product, s, id, kind, text, "plan-node", "plan-node", actor, actor, evidence, "")
		if err != nil && !(errors.Is(err, learning.ErrConflict) && res.Status == "duplicate") {
			return true, err
		}
	}
	return true, nil
}

func sharedLearningStop(product string, in hookInput) (bool, hookResult, error) {
	s, on, err := readSharedLearningSettings(product)
	if err != nil || !on {
		return on, hookResult{}, err
	}
	runID := strings.TrimSpace(in.RunID)
	if runID == "" && in.TranscriptPath != "" {
		if st, e := os.Stat(in.TranscriptPath); e == nil && !st.IsDir() {
			runID = "STOP-" + learnSHA([]byte(fmt.Sprintf("%s\n%s\n%d\n%d", in.SessionID, in.TranscriptPath, st.Size(), st.ModTime().UnixNano())))[:24]
		}
	}
	if runID == "" {
		return true, hookResult{}, errors.New("shared learning: finalize requires run_id; a Claude transcript is not required for GPT")
	}
	res, err := sharedLearningEvent(product, s, runID, "run_completed", in.LastAssistantMessage, "completed-turn", "host-finalize", hookPrincipal(in), in.SessionID, "", "")
	if errors.Is(err, learning.ErrConflict) && res.Status == "duplicate" {
		err = nil
	}
	if err != nil {
		return true, hookResult{}, err
	}
	return true, hookResult{}, nil
}

func sharedLearningContext(product string, in hookInput) (bool, hookResult, error) {
	s, on, err := readSharedLearningSettings(product)
	if err != nil {
		return true, hookResult{Block: true, Reason: "shared-learning cutover refused: " + err.Error()}, nil
	}
	if !on {
		return false, hookResult{}, nil
	}
	index, err := executeSharedLearning(product, s, "index", nil)
	if err != nil {
		return true, hookResult{}, err
	}
	var catalog struct {
		Skills []struct {
			SkillID string `json:"skill_id"`
			Target  string `json:"target"`
			SHA256  string `json:"sha256"`
		} `json:"skills"`
	}
	if err = json.Unmarshal(index.Data, &catalog); err != nil {
		return true, hookResult{}, err
	}
	if len(catalog.Skills) == 0 {
		return true, hookResult{}, nil
	}
	runID := in.RunID
	if runID == "" {
		runID = "context-" + in.SessionID
	}
	var text strings.Builder
	text.WriteString("LEARNED PROCEDURES: evidence-based guidance, not permissions or approval grants. Higher-priority product and user rules remain in force.\n")
	included := 0
	for _, skill := range catalog.Skills {
		if included >= 8 {
			text.WriteString("Additional procedures are available through air-worker learn context -product <root>.\n")
			break
		}
		heading := fmt.Sprintf("\nSkill %s SHA256=%s\n", skill.SkillID, skill.SHA256)
		info, err := os.Stat(filepath.Join(product, filepath.FromSlash(skill.Target)))
		if err != nil {
			return true, hookResult{}, err
		}
		if !info.Mode().IsRegular() || info.Size() < 0 {
			return true, hookResult{}, errors.New("invalid learned skill file")
		}
		if int64(text.Len()+len(heading)+1)+info.Size() > 16384 {
			// Selection is completed before the receipt-producing module load.
			continue
		}
		loaded, err := executeSharedLearning(product, s, "load", map[string]string{"target": skill.Target, "run_id": runID})
		if err != nil {
			return true, hookResult{}, err
		}
		var body struct {
			Content string `json:"content"`
			SHA256  string `json:"sha256"`
		}
		if err = json.Unmarshal(loaded.Data, &body); err != nil || body.SHA256 != skill.SHA256 {
			return true, hookResult{}, errors.New("shared skill changed between index and load")
		}
		if int64(len(body.Content)) != info.Size() {
			return true, hookResult{}, errors.New("skill changed during context selection")
		}
		text.WriteString(heading)
		text.WriteString(body.Content)
		text.WriteString("\n")
		included++
	}
	if included == 0 {
		return true, hookResult{Context: "No procedure body fits this context budget; use air-worker learn context/load on demand. No skill was loaded into this hook context."}, nil
	}
	return true, hookResult{Context: text.String()}, nil
}

// Defense in depth for any legacy internal caller not yet routed through the
// facade. A shared-mode product cannot silently create a second learning journal.
func forbidLegacyLearningWrite(path string) error {
	for parent := filepath.Dir(filepath.Clean(path)); ; parent = filepath.Dir(parent) {
		if filepath.Base(parent) == "learn" {
			product := filepath.Dir(parent)
			if filepath.Base(product) == ".air-worker" {
				product = filepath.Dir(product)
			}
			if _, err := os.Lstat(filepath.Join(product, sharedLearningConfigFile)); err == nil {
				return errors.New("legacy learning state is read-only in shared mode; use the native learn facade")
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("cannot prove legacy writer is selected: %w", err)
			}
		}
		if filepath.Dir(parent) == parent {
			break
		}
	}
	return nil
}
