package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const learningReviewerOutputSchema = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["apply", "content"],
  "properties": {
    "apply": {"type": "boolean"},
    "content": {"type": "string", "maxLength": 65536}
  }
}`

type learningReviewerAnswer struct {
	Apply   bool   `json:"apply"`
	Content string `json:"content"`
}

var learningReviewerInvoke = invokeLearningReviewerCodex

func cmdLearningAdapter(argv []string) int {
	if len(argv) == 0 {
		fmt.Fprintln(os.Stderr, "usage: air-worker learning-adapter reviewer")
		return 2
	}
	switch argv[0] {
	case "reviewer":
		return cmdLearningReviewerAdapter(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown learning adapter %q\n", argv[0])
		return 2
	}
}

func cmdLearningReviewerAdapter(argv []string) int {
	fs := flag.NewFlagSet("learning-adapter reviewer", flag.ContinueOnError)
	productFlag := fs.String("product", "", "managed product root; normally supplied by the parent learning runtime")
	model := fs.String("model", "gpt-5.6-luna", "approved Codex reviewer model")
	effort := fs.String("effort", "medium", "reviewer reasoning effort")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	product := strings.TrimSpace(*productFlag)
	if product == "" {
		product = strings.TrimSpace(os.Getenv("AIR_WORKER_LEARNING_PRODUCT"))
	}
	if product == "" {
		fmt.Fprintln(os.Stderr, "learning reviewer: product root is not supplied")
		return 2
	}
	root, err := normalizeLearnProduct(product)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learning reviewer:", err)
		return 2
	}
	settings, on, err := readSharedLearningSettings(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learning reviewer:", err)
		return 2
	}
	if !on {
		fmt.Fprintln(os.Stderr, "learning reviewer: shared learning is not enabled for product")
		return 2
	}

	input, err := ioReadAllBounded(os.Stdin, sharedLearningMaxBytes)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learning reviewer:", err)
		return 2
	}
	var packet map[string]any
	if err := json.Unmarshal(input, &packet); err != nil {
		fmt.Fprintln(os.Stderr, "learning reviewer: invalid JSON input")
		return 2
	}
	event, _ := packet["event"].(map[string]any)
	if event == nil {
		fmt.Fprintln(os.Stderr, "learning reviewer: event is required")
		return 2
	}
	class := strings.TrimSpace(jsonString(event["class"]))
	if class == "" {
		class = strings.TrimSpace(jsonString(event["kind"]))
	}
	if class == "" {
		class = "general"
	}
	// An existing per-class skill is a historical fact, not a mutable scratch
	// file. The 0.11.8 reviewer replaced useful CLI guidance with an unrelated
	// self-owner lesson because it shared the class "feedback-error".
	// Show the old guidance to the reviewer for deduplication, but NEVER
	// delegate an automatic overwrite of its managed target.
	classTarget := learningProcedureTarget(settings.ManagedSkillPrefix, class)
	before := []byte(nil)
	if b, readErr := readSharedContextSkill(root, classTarget, 64*1024); readErr == nil {
		before = b
	} else if !errors.Is(readErr, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "learning reviewer: cannot read existing class guidance:", readErr)
		return 2
	}
	packetJSON, _ := json.MarshalIndent(packet, "", "  ")
	prompt := buildLearningReviewerPrompt(classTarget, before, packetJSON)
	raw, err := learningReviewerInvoke(root, prompt, strings.TrimSpace(*model), strings.TrimSpace(*effort))
	if err != nil {
		fmt.Fprintln(os.Stderr, "learning reviewer:", err)
		return 2
	}
	var answer learningReviewerAnswer
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &answer); err != nil {
		fmt.Fprintln(os.Stderr, "learning reviewer: model output is not the required JSON object")
		return 2
	}
	if !answer.Apply {
		fmt.Println("{}")
		return 0
	}
	answer.Content = strings.TrimSpace(answer.Content) + "\n"
	if !validLearningProcedureMarkdown(answer.Content) {
		fmt.Fprintln(os.Stderr, "learning reviewer: proposed procedure is missing required Markdown sections")
		return 2
	}
	answer.Content, err = bindNativeFeedbackProof(class, answer.Content)
	if err != nil {
		fmt.Fprintln(os.Stderr, "learning reviewer:", err)
		return 2
	}
	// A distinct exact-content target avoids semantic overwrite and stays
	// deterministic for duplicate reviewer callbacks. The shared module still
	// enforces product ownership, managed-target limits and atomic ledger writes.
	contentSHA := learnSHA([]byte(answer.Content))
	target := strings.TrimSuffix(classTarget, ".md") + "-" + contentSHA[:16] + ".md"
	existing, readErr := readSharedContextSkill(root, target, 64*1024)
	switch {
	case readErr == nil:
		if learnSHA(existing) != contentSHA {
			fmt.Fprintln(os.Stderr, "learning reviewer: managed digest target has conflicting bytes")
			return 2
		}
		// Already present. Never rewrite it or synthesize a second applied
		// transaction for the same procedure.
		fmt.Println("{}")
		return 0
	case !errors.Is(readErr, os.ErrNotExist):
		fmt.Fprintln(os.Stderr, "learning reviewer: cannot inspect generated target:", readErr)
		return 2
	}
	out := map[string]string{
		"kind":       "procedure",
		"target":     target,
		"pre_sha256": "",
		"content":    answer.Content,
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		return 2
	}
	return 0
}

func ioReadAllBounded(f *os.File, max int) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > max {
		return nil, errors.New("learning reviewer input exceeds byte limit")
	}
	return b, nil
}

func jsonString(v any) string {
	s, _ := v.(string)
	return s
}

func learningProcedureTarget(prefix, class string) string {
	prefix = strings.TrimSuffix(filepath.ToSlash(strings.TrimSpace(prefix)), "/")
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(class) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if b.Len() > 0 && !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "lesson"
	}
	hash := learnSHA([]byte(strings.ToLower(strings.TrimSpace(class))))
	return prefix + "/" + slug + "-" + hash[:8] + ".md"
}

func buildLearningReviewerPrompt(target string, before, packet []byte) string {
	existing := "(new file)"
	if before != nil {
		existing = string(before)
	}
	return "You are AirWorker's production learning reviewer.\n" +
		"Decide whether this completed run contains a durable, reusable PROCEDURE lesson.\n" +
		"Do not propose code changes, permissions, grants, executable blocking rules, policy changes, secrets, or case-specific facts.\n" +
		"If there is no reusable procedural lesson, return apply=false and empty content.\n" +
		"If apply=true, return a concise, distinct Markdown procedure. It must contain: a # title, ## When to apply, ## Procedure, and ## Pitfalls.\n" +
		"Existing guidance is immutable: NEVER overwrite or rephrase an unrelated existing lesson. If the run teaches nothing new, apply=false. A new content-hash target is allocated after review; do not invent evidence.\n" +
		"If a new lesson genuinely teaches reporting a defect by the native air-worker feedback add command with stable -run-id, include that as an explicit positive numbered Procedure step. The binary alone decides whether this step can have a limited machine-verified use contract. Do not assert generic use, effect, remediation, or any blocking approval.\n" +
		"Return only JSON matching the provided schema.\n\n" +
		"Managed target: " + target + "\n\nExisting target bytes:\n---\n" + existing + "\n---\n\nRun evidence:\n" + string(packet)
}

func validLearningProcedureMarkdown(content string) bool {
	t := strings.TrimSpace(content)
	if !strings.HasPrefix(t, "# ") {
		return false
	}
	for _, section := range []string{"## When to apply", "## Procedure", "## Pitfalls"} {
		if !strings.Contains(t, section) {
			return false
		}
	}
	return len([]byte(content)) <= 64*1024
}

func invokeLearningReviewerCodex(product, prompt, model, effort string) (string, error) {
	if err := requirePonytailSkill(); err != nil {
		return "", fmt.Errorf("Ponytail: %w", err)
	}
	exePath, err := resolveRunnerTool("codex")
	if err != nil {
		return "", err
	}
	schema, err := os.CreateTemp("", "air-worker-learning-review-schema-*.json")
	if err != nil {
		return "", err
	}
	schemaPath := schema.Name()
	defer os.Remove(schemaPath)
	if _, err := schema.WriteString(learningReviewerOutputSchema); err != nil {
		_ = schema.Close()
		return "", err
	}
	if err := schema.Close(); err != nil {
		return "", err
	}
	reviewer := runnerSpec{Kind: "codex", Model: model, Effort: effort}
	cmd := semanticCodexCommand(exePath, product, prompt, reviewer, schemaPath)
	out, runErr := runReceiptedWithMeta(context.Background(), legacyScope(product), "learning", "learning-reviewer", cmd, jobReceiptMeta{
		Runner: reviewer.Kind, Provider: reviewer.Kind, Model: reviewer.Model, Effort: reviewer.Effort,
		Role: "learning-reviewer", Sandbox: "read-only",
	})
	completed, failed := false, false
	var messages, failures []string
	for _, line := range strings.Split(strings.ReplaceAll(decodeOutput(out), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "{") {
			continue
		}
		var e codexEvent
		if json.Unmarshal([]byte(t), &e) != nil {
			continue
		}
		switch e.Type {
		case "turn.completed":
			completed = true
		case "turn.failed", "error":
			failed = true
			if e.Error != nil && strings.TrimSpace(e.Error.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Error.Message))
			}
			if strings.TrimSpace(e.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Message))
			}
		case "item.completed":
			if e.Item != nil && e.Item.Type == "agent_message" && strings.TrimSpace(e.Item.Text) != "" {
				messages = append(messages, strings.TrimSpace(e.Item.Text))
			}
			if e.Item != nil && e.Item.Type == "error" && strings.TrimSpace(e.Item.Message) != "" {
				failures = append(failures, strings.TrimSpace(e.Item.Message))
			}
		}
	}
	if runErr != nil && !completed {
		failures = append(failures, runErr.Error())
	}
	if failed || !completed {
		if len(failures) == 0 {
			failures = append(failures, "Codex learning reviewer ended without a successful terminal event")
		}
		return "", errors.New(strings.Join(failures, "; "))
	}
	if len(messages) == 0 {
		return "", errors.New("Codex learning reviewer returned no agent_message")
	}
	return messages[len(messages)-1], nil
}
