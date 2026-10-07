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

type sharedLearningHookError struct {
	Phase string
	RunID string
	Err   error
}

func (e *sharedLearningHookError) Error() string {
	if e == nil {
		return "shared learning hook failure"
	}
	if e.RunID != "" {
		return fmt.Sprintf("shared learning %s failed (run_id=%s): %v", e.Phase, e.RunID, e.Err)
	}
	return fmt.Sprintf("shared learning %s failed: %v", e.Phase, e.Err)
}

func (e *sharedLearningHookError) Unwrap() error { return e.Err }

func sharedHookFailure(phase, runID string, err error) error {
	if err == nil {
		return nil
	}
	return &sharedLearningHookError{Phase: phase, RunID: strings.TrimSpace(runID), Err: err}
}

func readSharedLearningSettings(product string) (sharedLearningSettings, bool, error) {
	var s sharedLearningSettings
	path := filepath.Join(product, sharedLearningConfigFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, false, nil
	}
	if err != nil {
		return s, true, fmt.Errorf("cannot inspect %s: %w", sharedLearningConfigFile, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return s, true, fmt.Errorf("%s exists but is not an ordinary regular file", sharedLearningConfigFile)
	}
	// On the supported Windows host a junction/reparse entry can be readable by
	// Readlink even when ModeSymlink is absent. Presence of such an entry is a
	// configuration error, never evidence that shared mode is absent.
	if target, linkErr := os.Readlink(path); linkErr == nil {
		return s, true, fmt.Errorf("%s must not be a link/reparse entry (target %q)", sharedLearningConfigFile, target)
	}
	b, err := readLearningBounded(path, sharedLearningMaxBytes)
	if err != nil {
		// Once Lstat proved the selector entry exists, a read-time NOT_FOUND is
		// a shared-mode configuration failure (race/dangling entry), never proof
		// that legacy mode was selected.
		return s, true, fmt.Errorf("cannot read existing %s: %w", sharedLearningConfigFile, err)
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
func runLearningProcess(ctx context.Context, product string, a *learningProcessAdapter, input json.RawMessage) (json.RawMessage, error) {
	if len(input) > sharedLearningMaxBytes {
		return nil, errors.New("learning adapter input exceeds byte limit")
	}
	if a == nil {
		return nil, errors.New("learning adapter not configured")
	}
	exePath := strings.TrimSpace(a.Executable)
	expectedSHA := strings.TrimSpace(a.SHA256)
	selfBound := exePath == "@self"
	if selfBound {
		if expectedSHA != "@self" {
			return nil, errors.New("learning @self adapter requires sha256=@self")
		}
		var err error
		exePath, err = os.Executable()
		if err != nil {
			return nil, fmt.Errorf("resolve learning @self adapter: %w", err)
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return nil, fmt.Errorf("resolve learning @self adapter absolute path: %w", err)
		}
	} else {
		if !filepath.IsAbs(exePath) || len(expectedSHA) != 64 {
			return nil, errors.New("learning adapter must have an absolute executable and SHA-256")
		}
		f, err := os.Open(exePath)
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
		if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), expectedSHA) {
			return nil, errors.New("learning adapter executable SHA mismatch")
		}
	}
	ms := a.TimeoutMS
	if ms <= 0 || ms > 240000 {
		ms = 240000
	}
	childCtx, cancel := context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
	defer cancel()
	cmd := exec.Command(exePath, a.Args...)
	cmd.Stdin = bytes.NewReader(input)
	prepareLearningProcessTree(cmd)
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		name := item
		if i := strings.IndexByte(item, '='); i >= 0 {
			name = item[:i]
		}
		if strings.EqualFold(name, "AIR_WORKER_LEARNING_PRODUCT") {
			continue
		}
		env = append(env, item)
	}
	cmd.Env = append(env, "AIR_WORKER_LEARNING_PRODUCT="+product)
	var stdout, stderr learningBoundedOutput
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("learning adapter start failed: %w", err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitCh:
	case <-childCtx.Done():
		killErr := terminateLearningProcessTree(cmd)
		select {
		case <-waitCh:
		case <-time.After(3 * time.Second):
		}
		if killErr != nil {
			return nil, fmt.Errorf("%w; learning adapter process-tree cleanup failed: %v", childCtx.Err(), killErr)
		}
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
			return runLearningProcess(ctx, product, s.Judge, b)
		}
	}
	if s.Reviewer != nil {
		c.Reviewer = func(ctx context.Context, b json.RawMessage) (json.RawMessage, error) {
			return runLearningProcess(ctx, product, s.Reviewer, b)
		}
	}
	if s.VerifyGrant != nil {
		c.VerifyGrant = func(ctx context.Context, b json.RawMessage) error {
			answer, err := runLearningProcess(ctx, product, s.VerifyGrant, b)
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
			answer, err := runLearningProcess(ctx, product, s.DeliverSummary, b)
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
	runID := strings.TrimSpace(in.RunID)
	if err != nil {
		return on, hookResult{}, sharedHookFailure("finalize-config", runID, err)
	}
	if !on {
		return false, hookResult{}, nil
	}
	if runID == "" && in.TranscriptPath != "" {
		if st, e := os.Stat(in.TranscriptPath); e == nil && !st.IsDir() {
			runID = "STOP-" + learnSHA([]byte(fmt.Sprintf("%s\n%s\n%d\n%d", in.SessionID, in.TranscriptPath, st.Size(), st.ModTime().UnixNano())))[:24]
		}
	}
	if runID == "" {
		return true, hookResult{}, sharedHookFailure("finalize", "", errors.New("finalize requires run_id; a Claude transcript is not required for GPT"))
	}
	res, err := sharedLearningEvent(product, s, runID, "run_completed", in.LastAssistantMessage, "completed-turn", "host-finalize", hookPrincipal(in), in.SessionID, "", "")
	if errors.Is(err, learning.ErrConflict) && res.Status == "duplicate" {
		err = nil
	}
	if err != nil {
		return true, hookResult{}, sharedHookFailure("finalize", runID, err)
	}
	return true, hookResult{}, nil
}

type sharedLearningCatalogSkill struct {
	SkillID string `json:"skill_id"`
	Target  string `json:"target"`
	SHA256  string `json:"sha256"`
}

func recordSharedContextEvent(product string, s sharedLearningSettings, runID, kind, observed, class, outcome string, in hookInput) error {
	identity := learnSHA([]byte(kind + "\n" + observed + "\n" + class + "\n" + outcome))
	eventRunID := strings.TrimSpace(runID) + ":context:" + identity[:20]
	res, err := sharedLearningEvent(product, s, eventRunID, kind, observed, class, "host-context", hookPrincipal(in), in.SessionID, "", outcome)
	if errors.Is(err, learning.ErrConflict) && res.Status == "duplicate" {
		return nil
	}
	// For non-run_completed context receipts, Status=recorded means the primary
	// event was durably appended. The only later module step is its diagnostic
	// trace. A trace-write failure must not turn a truthful loaded receipt into
	// an aborted delivery: return the prepared context so the receipt matches
	// what the host receives.
	if err != nil && res.Status == "recorded" && res.ID != "" {
		return nil
	}
	return err
}

func readSharedContextSkill(product, target string, limit int64) ([]byte, error) {
	return readSharedContextSkillWithHook(product, target, limit, nil)
}

// readSharedContextSkillWithHook keeps the race regression deterministic without
// weakening production behavior. Production passes nil; tests can swap a parent after
// ancestor validation and restore it after os.Open to prove confinement follows the
// opened handle rather than the pathname.
func readSharedContextSkillWithHook(product, target string, limit int64, hook func(string) error) ([]byte, error) {
	rel := filepath.Clean(filepath.FromSlash(target))
	if filepath.IsAbs(rel) || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return nil, errors.New("learned skill target escaped product root")
	}
	abs := filepath.Join(product, rel)
	if !pathWithinRoot(product, abs) {
		return nil, errors.New("learned skill target escaped product root")
	}

	// Reject linked/reparse ancestors before opening. The opened handle is then
	// checked again below, so a parent swap cannot redirect this read outside
	// the resolved product root without detection.
	for cur := filepath.Dir(abs); ; cur = filepath.Dir(cur) {
		relToRoot, relErr := filepath.Rel(product, cur)
		if relErr == nil && relToRoot == "." {
			break
		}
		if relErr != nil || relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.Dir(cur) == cur {
			return nil, errors.New("learned skill ancestor escaped product root")
		}
		info, err := os.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return nil, fmt.Errorf("learned skill has linked/reparse ancestor: %s", cur)
		}
	}

	if hook != nil {
		if err := hook("before-open"); err != nil {
			return nil, err
		}
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if hook != nil {
		if err := hook("after-open"); err != nil {
			return nil, err
		}
	}
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() < 0 || st.Size() > limit {
		return nil, errors.New("learned skill is not a bounded regular file")
	}
	resolvedRoot, err := filepath.EvalSymlinks(product)
	if err != nil {
		return nil, err
	}
	finalPath, err := openedFileFinalPath(f)
	if err != nil {
		return nil, err
	}
	if !pathWithinRoot(resolvedRoot, finalPath) {
		return nil, fmt.Errorf("opened learned skill escaped product root: %s", finalPath)
	}
	body, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, errors.New("learned skill exceeds byte limit")
	}
	return body, nil
}

func sharedLearningContextFromCatalog(product string, s sharedLearningSettings, in hookInput, runID string, skills []sharedLearningCatalogSkill) (hookResult, error) {
	return sharedLearningContextFromCatalogWithReader(product, s, in, runID, skills, readSharedContextSkill)
}

func sharedLearningContextFromCatalogWithReader(product string, s sharedLearningSettings, in hookInput, runID string, skills []sharedLearningCatalogSkill, reader func(string, string, int64) ([]byte, error)) (hookResult, error) {
	var text strings.Builder
	text.WriteString("LEARNED PROCEDURES: evidence-based guidance, not permissions or approval grants. Higher-priority product and user rules remain in force.\n")
	var loadedFacts, failedFacts []string
	included := 0

	for _, skill := range skills {
		if included >= 8 {
			text.WriteString("Additional procedures are available through air-worker learn context -product <root>.\n")
			break
		}
		body, readErr := reader(product, skill.Target, sharedLearningMaxBytes)
		if readErr != nil {
			failedFacts = append(failedFacts, skill.Target+"@expected="+skill.SHA256+": "+readErr.Error())
			continue
		}
		actualSHA := learnSHA(body)
		if !strings.EqualFold(actualSHA, skill.SHA256) {
			failedFacts = append(failedFacts, skill.Target+"@expected="+skill.SHA256+" actual="+actualSHA+": SHA changed after verified index")
			continue
		}
		heading := fmt.Sprintf("\nSkill %s SHA256=%s\n", skill.SkillID, skill.SHA256)
		if text.Len()+len(heading)+len(body)+1 > 16384 {
			continue
		}
		text.WriteString(heading)
		text.Write(body)
		text.WriteString("\n")
		loadedFacts = append(loadedFacts, skill.Target+"@"+actualSHA)
		included++
	}

	if len(failedFacts) > 0 {
		if err := recordSharedContextEvent(product, s, runID+":context-failed", "skill_delivery_failed", strings.Join(failedFacts, "; "), "delivery-error", "failed", in); err != nil {
			return hookResult{}, sharedHookFailure("context-failure-receipt", runID, err)
		}
	}
	if len(loadedFacts) > 0 {
		if err := recordSharedContextEvent(product, s, runID+":context-loaded", "skill_loaded", strings.Join(loadedFacts, "; "), "procedure-context", "loaded", in); err != nil {
			return hookResult{}, sharedHookFailure("context-load-receipt", runID, err)
		}
	}

	if included == 0 {
		if len(failedFacts) > 0 {
			return hookResult{}, sharedHookFailure("context-load", runID, errors.New(strings.Join(failedFacts, "; ")))
		}
		return hookResult{Context: "No procedure body fits this context budget; use air-worker learn context/load on demand. No skill was loaded into this hook context."}, nil
	}
	if len(failedFacts) > 0 {
		text.WriteString("\nSome learned procedures were not delivered; the failure was recorded as skill_delivery_failed.\n")
	}
	return hookResult{Context: text.String()}, nil
}

func sharedLearningContext(product string, in hookInput) (bool, hookResult, error) {
	s, on, err := readSharedLearningSettings(product)
	runID := strings.TrimSpace(in.RunID)
	if runID == "" {
		runID = "context-" + in.SessionID
	}
	if err != nil {
		return true, hookResult{}, sharedHookFailure("context-config", runID, err)
	}
	if !on {
		return false, hookResult{}, nil
	}
	index, err := executeSharedLearning(product, s, "index", nil)
	if err != nil {
		return true, hookResult{}, sharedHookFailure("context-index", runID, err)
	}
	var catalog struct {
		Skills []sharedLearningCatalogSkill `json:"skills"`
	}
	if err = json.Unmarshal(index.Data, &catalog); err != nil {
		return true, hookResult{}, sharedHookFailure("context-index-decode", runID, err)
	}
	if len(catalog.Skills) == 0 {
		return true, hookResult{}, nil
	}
	res, err := sharedLearningContextFromCatalog(product, s, in, runID, catalog.Skills)
	return true, res, err
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
