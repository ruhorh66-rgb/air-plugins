package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

type hlp03Case struct {
	ID         string `json:"id"`
	Procedures []struct {
		Target string `json:"target"`
		Body   string `json:"body"`
	} `json:"procedures"`
	Event struct {
		Kind     string `json:"kind"`
		Class    string `json:"class"`
		Observed string `json:"observed"`
		Outcome  string `json:"outcome"`
	} `json:"event"`
}

type hlp03Record struct {
	ID              string                       `json:"id"`
	Status          string                       `json:"status"`
	Error           string                       `json:"error,omitempty"`
	ElapsedMS       int64                        `json:"elapsed_ms"`
	PromptSHA       string                       `json:"prompt_sha256,omitempty"`
	NativePromptSHA string                       `json:"native_prompt_sha256,omitempty"`
	ContextSHA      string                       `json:"context_sha256,omitempty"`
	Settings        sharedLearningSettings       `json:"settings"`
	Adapter         learningProcessAdapter       `json:"adapter"`
	ConfigSHA       string                       `json:"config_sha256,omitempty"`
	Raw             json.RawMessage              `json:"adapter_raw,omitempty"`
	Candidate       map[string]string            `json:"candidate,omitempty"`
	Before          []sharedLearningCatalogSkill `json:"before,omitempty"`
	After           []sharedLearningCatalogSkill `json:"after,omitempty"`
	Module          string                       `json:"module_status,omitempty"`
	Lifecycle       string                       `json:"lifecycle_status,omitempty"`
	StateBefore     map[string]string            `json:"state_before,omitempty"`
	StateAfter      map[string]string            `json:"state_after,omitempty"`
	StateRollback   map[string]string            `json:"state_after_rollback,omitempty"`
	Backup          bool                         `json:"backup_verified,omitempty"`
	Rollback        string                       `json:"rollback_status,omitempty"`
	Cleanup         string                       `json:"cleanup_status"`
	Native          hlp03Native                  `json:"native"`
}

type hlp03Native struct {
	Usage        any               `json:"usage"`
	Model        string            `json:"model,omitempty"`
	OutputModel  string            `json:"output_model,omitempty"`
	Terminal     string            `json:"terminal"`
	ToolCalls    map[string]int    `json:"tool_calls"`
	Contaminated bool              `json:"contaminated"`
	RawToolCalls []json.RawMessage `json:"raw_tool_calls,omitempty"`
}

func hlp03NativeEvidence(raw []byte, fixtureRoot string, forbidden ...string) hlp03Native {
	e := hlp03Native{Terminal: "unknown", ToolCalls: map[string]int{}}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		var row map[string]json.RawMessage
		if json.Unmarshal(line, &row) != nil {
			continue
		}
		var typ string
		_ = json.Unmarshal(row["type"], &typ)
		if typ == "turn.completed" || typ == "turn.failed" {
			e.Terminal = typ
		}
		if len(row["usage"]) > 0 {
			e.Usage = json.RawMessage(row["usage"])
		}
		if len(row["model"]) > 0 {
			_ = json.Unmarshal(row["model"], &e.Model)
		}
		var item struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(row["item"], &item)
		if item.Type != "" && item.Type != "agent_message" && item.Type != "reasoning" {
			var decoded any
			if json.Unmarshal(row["item"], &decoded) == nil && hlp03ContainsForbidden(decoded, fixtureRoot, forbidden) {
				e.Contaminated = true
			}
			if typ == "item.started" {
				e.ToolCalls[item.Type]++
				e.RawToolCalls = append(e.RawToolCalls, append(json.RawMessage(nil), row["item"]...))
			}
		}
	}
	return e
}

func hlp03NormalPath(s string) string {
	s = strings.ToLower(strings.ReplaceAll(s, `\`, "/"))
	for strings.Contains(s, "//") {
		s = strings.ReplaceAll(s, "//", "/")
	}
	return s
}

func hlp03ContainsForbidden(v any, fixtureRoot string, forbidden []string) bool {
	switch x := v.(type) {
	case string:
		x = hlp03NormalPath(x)
		allowed := hlp03NormalPath(fixtureRoot)
		// Fixture roots live under the checkout's owned qa-temp. Exempt only
		// their exact path boundary, never a similarly prefixed source path.
		if x == allowed {
			x = "<fixture>"
		}
		for _, boundary := range []string{"/", "'", "\"", " ", "\n", "\r", "\t"} {
			x = strings.ReplaceAll(x, allowed+boundary, "<fixture>"+boundary)
		}
		for _, path := range forbidden {
			if path != "" && strings.Contains(x, hlp03NormalPath(path)) {
				return true
			}
		}
	case []any:
		for _, item := range x {
			if hlp03ContainsForbidden(item, fixtureRoot, forbidden) {
				return true
			}
		}
	case map[string]any:
		for key, item := range x {
			// Relative parent traversal in a tool input needs manual adjudication;
			// it cannot establish a clean, fixture-only observation.
			if key != "aggregated_output" && key != "output" {
				if text, ok := item.(string); ok && strings.Contains(hlp03NormalPath(text), "../") {
					return true
				}
			}
			if hlp03ContainsForbidden(item, fixtureRoot, forbidden) {
				return true
			}
		}
	}
	return false
}

func hlp03Write(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0600)
}

func hlp03SHA(b []byte) string { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }

func hlp03State(root string) (map[string]string, error) {
	state := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if len(state) >= 256 {
			return fmt.Errorf("fixture state exceeds 256 files")
		}
		b, err := readLearningBounded(path, 1024*1024)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		state[filepath.ToSlash(rel)] = hlp03SHA(b)
		return nil
	})
	return state, err
}

func TestHLP03RealReviewerCorpus(t *testing.T) {
	if os.Getenv("AIR_WORKER_HLP03_LIVE") != "1" {
		t.Skip("opt-in live reviewer evaluation")
	}
	t.Setenv(hookStateDirEnv, t.TempDir())
	deadline, ok := t.Deadline()
	if !ok || time.Until(deadline) <= 10*time.Second {
		t.Fatal("test needs a deadline with at least 10 seconds remaining")
	}
	ctx, cancel := context.WithDeadline(context.Background(), deadline.Add(-10*time.Second))
	defer cancel()
	need := func(name string) string {
		v := strings.TrimSpace(os.Getenv(name))
		if v == "" {
			t.Fatalf("%s is required", name)
		}
		return v
	}
	corpusPath, output := need("AIR_WORKER_HLP03_CORPUS"), need("AIR_WORKER_HLP03_OUTPUT")
	binary, binarySHA := need("AIR_WORKER_HLP03_BINARY"), need("AIR_WORKER_HLP03_BINARY_SHA256")
	model, effort := need("AIR_WORKER_HLP03_MODEL"), need("AIR_WORKER_HLP03_EFFORT")
	if model != "gpt-5.6-luna" || effort != "medium" {
		t.Fatal("reviewer model/effort differs from installed configuration")
	}
	if !filepath.IsAbs(binary) || !filepath.IsAbs(output) || !filepath.IsAbs(corpusPath) {
		t.Fatal("corpus, output and binary must be absolute paths")
	}
	if _, err := os.Stat(output); err == nil {
		t.Fatal("output already exists")
	}
	corpusBytes, err := readLearningBounded(corpusPath, 256*1024)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Schema string      `json:"schema"`
		Cases  []hlp03Case `json:"cases"`
	}
	dec := json.NewDecoder(bytes.NewReader(corpusBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	if dec.Decode(new(any)) != io.EOF {
		t.Fatal("trailing corpus JSON")
	}
	if corpus.Schema != "air-worker.hlp03-corpus-input/v1" || len(corpus.Cases) == 0 || len(corpus.Cases) > 24 {
		t.Fatal("invalid corpus schema or case count")
	}
	if len(corpus.Cases) != 12 {
		t.Fatal("HLP-03 requires all 12 fixed cases")
	}
	ids := map[string]bool{}
	validID := regexp.MustCompile(`^C[0-9]{2}$`)
	for _, c := range corpus.Cases {
		if !validID.MatchString(c.ID) || ids[c.ID] || len(c.Procedures) > 32 || c.Event.Kind == "" || c.Event.Class == "" {
			t.Fatal("invalid case identity, seed count or event")
		}
		ids[c.ID] = true
		for _, p := range c.Procedures {
			if len(p.Body) > 64*1024 || !validLearningProcedureMarkdown(p.Body) || !strings.HasPrefix(p.Target, "skills/learned/") {
				t.Fatalf("invalid seed in %s", c.ID)
			}
		}
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil || !strings.EqualFold(hlp03SHA(binaryBytes), binarySHA) {
		t.Fatal("source binary SHA mismatch or unreadable binary")
	}
	if err := os.Mkdir(output, 0700); err != nil {
		t.Fatal(err)
	}
	config := learningProcessAdapter{Executable: binary, SHA256: binarySHA, TimeoutMS: 240000}
	config.Args = []string{"learning-adapter", "reviewer", "-product", "<per-case fixture>", "-model", model, "-effort", effort}
	configBytes, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	sourceRoot = filepath.Dir(sourceRoot)
	head, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	harnessBytes, err := readLearningBounded(filepath.Join(sourceRoot, "cmd", "learn_adapter_live_eval_test.go"), 1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	summary := struct {
		Schema     string        `json:"schema"`
		CorpusSHA  string        `json:"corpus_sha256"`
		BinarySHA  string        `json:"binary_sha256"`
		ConfigSHA  string        `json:"config_template_sha256"`
		SourceHEAD string        `json:"source_head"`
		HarnessSHA string        `json:"harness_sha256"`
		Model      string        `json:"model"`
		Effort     string        `json:"effort"`
		Cases      []hlp03Record `json:"cases"`
	}{"air-worker.hlp03-eval-output/v1", hlp03SHA(corpusBytes), hlp03SHA(binaryBytes), hlp03SHA(configBytes), strings.TrimSpace(string(head)), hlp03SHA(harnessBytes), model, effort, nil}
	identity := map[string]any{"corpus_sha256": summary.CorpusSHA, "binary_sha256": summary.BinarySHA, "config_template_sha256": summary.ConfigSHA, "adapter_config_template": config, "model": model, "effort": effort, "source_head": summary.SourceHEAD, "harness_sha256": summary.HarnessSHA}
	if err := hlp03Write(filepath.Join(output, "identity.json"), identity); err != nil {
		t.Fatal(err)
	}
	if err := hlp03Write(filepath.Join(output, "summary.json"), summary); err != nil {
		t.Fatal(err)
	}
	for _, c := range corpus.Cases {
		c := c
		t.Run(c.ID, func(t *testing.T) {
			caseDir := filepath.Join(output, c.ID)
			if err := os.Mkdir(caseDir, 0700); err != nil {
				t.Fatal(err)
			}
			r := hlp03Record{ID: c.ID, Status: "incomplete", Cleanup: "unverified"}
			var ownedRoot string
			t.Cleanup(func() {
				if ownedRoot != "" {
					_, err := os.Stat(ownedRoot)
					if os.IsNotExist(err) {
						r.Cleanup = "verified_removed"
					} else {
						r.Cleanup = fmt.Sprintf("cleanup_failed: %v", err)
						t.Errorf("fixture cleanup: %s", r.Cleanup)
					}
				}
				if err := hlp03Write(filepath.Join(caseDir, "result.json"), r); err != nil {
					t.Errorf("result evidence: %v", err)
				}
				summary.Cases = append(summary.Cases, r)
				if err := hlp03Write(filepath.Join(output, "summary.json"), summary); err != nil {
					t.Errorf("summary evidence: %v", err)
				}
			})
			// Register verification before the first TempDir: Go shares one
			// cleanup for every TempDir in this subtest and runs cleanups LIFO.
			t.Setenv(hookStateDirEnv, t.TempDir())
			product, settings := sharedProductFixture(t, false)
			ownedRoot = filepath.Dir(product)
			r.Settings = settings
			write := func(name string, value any) bool {
				if err := hlp03Write(filepath.Join(caseDir, name), value); err != nil {
					r.Status, r.Error = "infrastructure_error", err.Error()
					t.Errorf("%s: %v", name, err)
					return false
				}
				return true
			}
			writeBytes := func(name string, b []byte) bool {
				if err := os.WriteFile(filepath.Join(caseDir, name), b, 0600); err != nil {
					r.Status, r.Error = "infrastructure_error", err.Error()
					t.Errorf("%s: %v", name, err)
					return false
				}
				return true
			}
			for i, p := range c.Procedures {
				reuseSeed(t, product, settings, fmt.Sprintf("LP-HLP03-%s-%02d", c.ID, i), p.Target, p.Body)
			}
			r.Before = reuseIndex(t, product, settings)
			r.StateBefore, err = hlp03State(settings.RuntimeRoot)
			if err != nil {
				r.Status, r.Error = "infrastructure_error", err.Error()
				t.Error(err)
				return
			}
			classTarget := learningProcedureTarget(settings.ManagedSkillPrefix, c.Event.Class)
			skills, err := reviewerCatalog(product, settings, classTarget)
			if err != nil {
				r.Status, r.Error = "setup_error", err.Error()
				return
			}
			packet, err := json.Marshal(map[string]any{"event": c.Event})
			if err != nil {
				t.Fatal(err)
			}
			var packetObject map[string]any
			if err := json.Unmarshal(packet, &packetObject); err != nil {
				t.Fatal(err)
			}
			packetIndented, err := json.MarshalIndent(packetObject, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			prompt := buildLearningReviewerPromptWithSettings(classTarget, skills, packetIndented, settings)
			contextBytes, err := json.Marshal(skills)
			if err != nil {
				t.Fatal(err)
			}
			r.PromptSHA, r.ContextSHA = hlp03SHA([]byte(prompt)), hlp03SHA(contextBytes)
			r.NativePromptSHA = hlp03SHA([]byte(ponytailPrompt(prompt)))
			if !writeBytes("prompt.txt", []byte(prompt)) || !writeBytes("native-prompt.txt", []byte(ponytailPrompt(prompt))) || !write("seed-catalogue.json", skills) || !write("packet.json", packetObject) || !write("settings.json", settings) {
				return
			}
			config.Args = []string{"learning-adapter", "reviewer", "-product", product, "-model", model, "-effort", effort}
			r.Adapter = config
			actualConfig, marshalErr := json.Marshal(config)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			r.ConfigSHA = hlp03SHA(actualConfig)
			if !write("adapter-config.json", config) {
				return
			}
			start := time.Now()
			r.Raw, err = runLearningProcess(ctx, product, &config, packet)
			r.ElapsedMS = time.Since(start).Milliseconds()
			// Native receipts and JSONL are copied while the fixture still exists.
			jobs, globErr := filepath.Glob(filepath.Join(product, ".woody", "jobs", "*"))
			if globErr != nil {
				r.Status, r.Error = "infrastructure_error", globErr.Error()
				t.Error(globErr)
				return
			}
			var outCount, receiptCount int
			for _, path := range jobs {
				if !strings.HasSuffix(path, ".out") && !strings.HasSuffix(path, ".receipt.json") {
					continue
				}
				b, readErr := readLearningBounded(path, 1024*1024)
				if readErr != nil {
					r.Status, r.Error = "infrastructure_error", fmt.Sprintf("native evidence %s: %v", path, readErr)
					t.Error(r.Error)
					return
				}
				if !writeBytes(filepath.Base(path), b) {
					return
				}
				if strings.HasSuffix(path, ".out") {
					outCount++
					native := hlp03NativeEvidence(b, ownedRoot, corpusPath, sourceRoot, "HLP03_REVIEWER_GOLD_20261024", "HLP03_CORPUS_FREEZE_20261024")
					r.Native.Usage, r.Native.Terminal, r.Native.ToolCalls, r.Native.Contaminated, r.Native.RawToolCalls = native.Usage, native.Terminal, native.ToolCalls, native.Contaminated, native.RawToolCalls
					r.Native.OutputModel = native.Model
				}
				if strings.HasSuffix(path, ".receipt.json") {
					receiptCount++
					var receipt jobReceipt
					if json.Unmarshal(b, &receipt) != nil {
						r.Status, r.Error = "infrastructure_error", "invalid native receipt"
						t.Error(r.Error)
						return
					}
					r.Native.Model = receipt.Model
				}
			}
			if len(r.Raw) > 0 && !writeBytes("adapter-raw.json", r.Raw) {
				return
			}
			if err != nil {
				r.Status, r.Error = "provider_or_adapter_error", err.Error()
				if strings.Contains(strings.ToLower(err.Error()), "deadline exceeded") {
					r.Status = "timeout"
				}
				return
			}
			if outCount != 1 || receiptCount != 1 || r.Native.Terminal != "turn.completed" || r.Native.Contaminated {
				r.Status, r.Error = "evidence_error", fmt.Sprintf("native out=%d receipt=%d terminal=%s contaminated=%t", outCount, receiptCount, r.Native.Terminal, r.Native.Contaminated)
				t.Error(r.Error)
				return
			}
			if bytes.Equal(bytes.TrimSpace(r.Raw), []byte("{}")) {
				r.Status = "no_candidate"
				r.After = reuseIndex(t, product, settings)
				if !reflect.DeepEqual(r.Before, r.After) {
					r.Lifecycle = "catalogue_changed"
					t.Error("no_candidate changed catalogue")
				} else {
					r.Lifecycle = "unchanged"
				}
				for _, p := range c.Procedures {
					got, readErr := os.ReadFile(filepath.Join(product, filepath.FromSlash(p.Target)))
					if readErr != nil || string(got) != p.Body {
						r.Lifecycle = "seed_changed"
						t.Errorf("no_candidate changed seed %s: %v", p.Target, readErr)
					}
				}
				r.StateAfter, err = hlp03State(settings.RuntimeRoot)
				if err != nil {
					r.Lifecycle = "state_error"
					t.Error(err)
				}
				return
			}
			if err := json.Unmarshal(r.Raw, &r.Candidate); err != nil || r.Candidate["kind"] != "procedure" || r.Candidate["target"] == "" || r.Candidate["content"] == "" {
				r.Status = "malformed"
				return
			}
			old := map[string]string{}
			for _, p := range c.Procedures {
				old[p.Target] = p.Body
			}
			pre := r.Candidate["pre_sha256"]
			if pre != "" {
				body, ok := old[r.Candidate["target"]]
				if !ok || !strings.EqualFold(pre, hlp03SHA([]byte(body))) {
					r.Status = "wrong_update_identity"
					return
				}
				r.Status = "update"
			} else {
				r.Status = "create"
			}
			proposalID := "LP-HLP03-REVIEW-" + c.ID
			res, applyErr := executeSharedLearning(product, settings, "propose", map[string]string{"proposal_id": proposalID, "kind": "procedure", "target": r.Candidate["target"], "pre_sha256": pre, "content": r.Candidate["content"]})
			r.Module = res.Status
			r.After = reuseIndex(t, product, settings)
			r.StateAfter, err = hlp03State(settings.RuntimeRoot)
			if err != nil {
				r.Lifecycle = "state_error"
				t.Error(err)
				return
			}
			if applyErr != nil || res.Status != "applied" {
				r.Lifecycle, r.Error = "module_error", fmt.Sprint(applyErr)
				return
			}
			r.Lifecycle = "applied"
			wantCount := len(r.Before) + 1
			if pre != "" {
				wantCount = len(r.Before)
			}
			if len(r.After) != wantCount {
				r.Lifecycle = "catalogue_mismatch"
				return
			}
			found := false
			for _, skill := range r.After {
				if skill.Target == r.Candidate["target"] && strings.EqualFold(skill.SHA256, hlp03SHA([]byte(r.Candidate["content"]))) {
					found = true
				}
			}
			if !found {
				r.Lifecycle = "catalogue_mismatch"
				return
			}
			for _, before := range r.Before {
				if before.Target == r.Candidate["target"] && pre != "" {
					continue
				}
				unchanged := false
				for _, after := range r.After {
					if reflect.DeepEqual(before, after) {
						unchanged = true
						break
					}
				}
				if !unchanged {
					r.Lifecycle = "catalogue_mismatch"
					return
				}
			}
			for target, body := range old {
				if target == r.Candidate["target"] && pre != "" {
					continue
				}
				got, readErr := os.ReadFile(filepath.Join(product, filepath.FromSlash(target)))
				if readErr != nil || string(got) != body {
					r.Lifecycle = "seed_changed"
					return
				}
			}
			if pre != "" {
				backup, backupErr := os.ReadFile(filepath.Join(settings.RuntimeRoot, "backups", pre))
				r.Backup = backupErr == nil && string(backup) == old[r.Candidate["target"]]
				roll, rollErr := executeSharedLearning(product, settings, "rollback", map[string]string{"proposal_id": proposalID})
				r.Rollback = roll.Status
				if !r.Backup || rollErr != nil || roll.Status != "rolled_back" {
					r.Lifecycle = "rollback_error"
				} else if restored, readErr := os.ReadFile(filepath.Join(product, filepath.FromSlash(r.Candidate["target"]))); readErr != nil || string(restored) != old[r.Candidate["target"]] {
					r.Lifecycle = "rollback_error"
				} else {
					r.Lifecycle = "rolled_back"
				}
				r.StateRollback, err = hlp03State(settings.RuntimeRoot)
				if err != nil {
					r.Lifecycle = "state_error"
					t.Error(err)
				}
			}
		})
	}
}

func TestHLP03NativeEvidencePaths(t *testing.T) {
	fixture := `R:\source\.air-worker\qa-temp\fixture`
	source := `R:\source`
	for _, tc := range []struct {
		name, command string
		contaminated  bool
	}{
		{"fixture", "Get-Content '" + fixture + `\product\skills\learned\p1.md'`, false},
		{"gold escaped case", `Get-Content 'r:\\SOURCE\\docs\\HLP03_REVIEWER_GOLD_20261024.json'`, true},
		{"source", `Get-Content 'R:\source\cmd\learn_adapter.go'`, true},
		{"adjacent", "Get-Content '" + fixture + `-other\file'`, true},
		{"mixed", "Get-Content '" + fixture + `\product\p1.md'; Get-Content 'R:\source\docs\HLP03_REVIEWER_GOLD_20261024.json'`, true},
		{"parent traversal", "Get-Content '" + fixture + `\..\..\..\cmd\learn_adapter.go'`, true},
		{"vendor skill", `Get-Content 'C:\vendor\ponytail\SKILL.md'`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			line, err := json.Marshal(map[string]any{"type": "item.started", "item": map[string]any{"type": "command_execution", "command": tc.command}})
			if err != nil {
				t.Fatal(err)
			}
			raw := append(line, []byte("\n"+`{"type":"turn.completed","usage":{"input_tokens":123,"output_tokens":7}}`)...)
			got := hlp03NativeEvidence(raw, fixture, source, "HLP03_REVIEWER_GOLD_20261024", "HLP03_CORPUS_FREEZE_20261024")
			if got.Contaminated != tc.contaminated || got.Terminal != "turn.completed" || got.Usage == nil || got.ToolCalls["command_execution"] != 1 || len(got.RawToolCalls) != 1 {
				t.Fatalf("native evidence mismatch: %+v", got)
			}
		})
	}
}
