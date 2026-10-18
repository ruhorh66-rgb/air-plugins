package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// The model may propose any Markdown; only compiled release-owned actions
// have machine meaning. This registry grants no shell, file write or approval
// privileges. A review/APPLIED ledger and exact target@SHA are additionally
// required by findAppliedPreflightRule before the native handler can run.
const nativeLearnedActionSchema = "air-worker.native-learned-action/v1"

type nativeLearnedAction struct {
	Schema        string `json:"schema"`
	ID            string `json:"id"`
	Version       int    `json:"version"`
	NativeCommand string `json:"native_command"`
	Precondition  string `json:"precondition"`
	Operation     string `json:"operation"`
	Outcome       string `json:"outcome"`
	Effect        string `json:"effect"`
}

// A new typed action must be registered with an independent compiled
// handler, deterministic preconditions and negative tests. A new SHA or
// reviewer prose is NEVER enough to add a supported action.
func supportedNativePreflightAction() nativeLearnedAction {
	return nativeLearnedAction{
		Schema: nativeLearnedActionSchema,
		ID:     "validate.canonical-config-preflight", Version: 1,
		NativeCommand: "validate",
		Precondition:  "validate-run-config-missing",
		Operation:     "check-missing-config-and-defer",
		Outcome:       "deferred_missing_configuration",
		Effect:        "avoided_repeat_validation_with_missing_config",
	}
}

// Exact compatibility grammar for an already APPLIED, real-world 2026-10-18
// reviewer output. This is not a new-SHA exception: the bytes contain
// exactly the audited safe recipe; a changed instruction is UNSUPPORTED.
// Future generative outputs may use the machine-only typed envelope below.
// A natural-language procedure with extra/free-form steps is never executable.
const n093ReviewedSafePreflightText = "# Validate the Canonical Product Configuration Before Retrying\n\n" +
	"## When to apply\n\n" +
	"When a native AirWorker CLI validation fails because the product configuration is missing or cannot be found.\n\n" +
	"## Procedure\n\n" +
	"1. Locate the canonical product root.\n" +
	"2. Verify that the expected existing configuration file is present and valid before retrying validation.\n" +
	"3. If it is absent, stop and report the missing precondition; do not invent, silently replace, or claim repair of the configuration.\n\n" +
	"## Pitfalls\n\n" +
	"- Treating the product directory’s existence as proof that its configuration is available.\n" +
	"- Retrying validation without resolving the missing configuration precondition.\n" +
	"- Claiming a successful repair or procedure effect without evidence.\n"

// A second production Codex review on an independent signed native failure
// produced the same audited read-only action with different safe wording.
// This source-owned compatibility template is exact CONTENT, never a SHA
// exception: arbitrary Markdown still cannot select an action. New native
// operations require distinct compiled handlers and independent negative QA.
const n093ReviewedSafePreflightSecondReal = "# Verify the canonical product configuration before retrying validation\n\n" +
	"## When to apply\n\n" +
	"When native AirWorker CLI validation fails because the product directory exists but its expected configuration file is missing.\n\n" +
	"## Procedure\n\n" +
	"1. Locate the canonical product root used by the run.\n" +
	"2. Verify that the expected existing configuration, including `run-config.json`, is present and valid.\n" +
	"3. Retry validation only after confirming the canonical configuration; do not invent or silently replace configuration files.\n\n" +
	"## Pitfalls\n\n" +
	"- Treating the first matching product directory as canonical.\n" +
	"- Creating or replacing configuration merely to make validation proceed.\n" +
	"- Claiming successful repair or validation without evidence.\n"

// This grammar supports multiple distinct, cryptographically indexed skills
// without pre-registering their SHA. The only allowed contents are machine
// data for a compiled operation, not additional instructions from the model.
const nativeLearnedActionEnvelopePrefix = "# AirWorker Native Action\n\n~~~air-worker-action\n"
const nativeLearnedActionEnvelopeSuffix = "\n~~~\n"

// The published air-modules v0.1.0 only applies procedures with Markdown
// headings. This exact wrapper is source-owned; the payload may vary in JSON
// formatting, but any new instruction/operation remains UNSUPPORTED.
const nativeLearnedActionMarkdownPrefix = "# AirWorker Native Action\n\n" +
	"## When to apply\n\nAfter an authenticated native validate failure because canonical run-config.json is missing.\n\n" +
	"## Procedure\n\n~~~air-worker-action\n"
const nativeLearnedActionMarkdownSuffix = "\n~~~\n\n## Pitfalls\n\n- This read-only action neither repairs files nor claims that validation passed.\n"

func resolveNativeLearnedAction(body []byte) (nativeLearnedAction, error) {
	if len(body) == 0 || len(body) > 4096 || !utf8.Valid(body) || bytes.ContainsRune(body, '\r') {
		return nativeLearnedAction{}, errors.New("UNSUPPORTED: invalid or oversized native action")
	}
	audited := supportedNativePreflightAction()
	// Preserve the N118 signed old procedure while not elevating other text
	// to executable policy.
	if learnSHA(body) == n118ApprovedPreflightDeferralSHA {
		return audited, nil
	}
	if string(body) == n093ReviewedSafePreflightText || string(body) == n093ReviewedSafePreflightSecondReal {
		return audited, nil
	}
	var raw []byte
	switch {
	case bytes.HasPrefix(body, []byte(nativeLearnedActionEnvelopePrefix)) &&
		bytes.HasSuffix(body, []byte(nativeLearnedActionEnvelopeSuffix)):
		// Legacy machine-only envelope. It cannot pass the published
		// learning module's procedure schema and is retained for older tests.
		raw = body[len(nativeLearnedActionEnvelopePrefix) : len(body)-len(nativeLearnedActionEnvelopeSuffix)]
	case bytes.HasPrefix(body, []byte(nativeLearnedActionMarkdownPrefix)) &&
		bytes.HasSuffix(body, []byte(nativeLearnedActionMarkdownSuffix)):
		// The only accepted Markdown capsule has an exact audited wrapper.
		// Arbitrary reviewer prose cannot modify preconditions or effects.
		raw = body[len(nativeLearnedActionMarkdownPrefix) : len(body)-len(nativeLearnedActionMarkdownSuffix)]
	default:
		return nativeLearnedAction{}, errors.New("UNSUPPORTED: no compiled native action grammar")
	}
	if len(raw) == 0 || bytes.Contains(raw, []byte{0}) {
		return nativeLearnedAction{}, errors.New("UNSUPPORTED: empty native action payload")
	}
	// A plain json.Decoder would silently accept duplicate keys. Refuse them
	// even if the later value equals a registered action.
	keys := json.NewDecoder(bytes.NewReader(raw))
	start, err := keys.Token()
	if err != nil || start != json.Delim('{') {
		return nativeLearnedAction{}, errors.New("UNSUPPORTED: native action must be an object")
	}
	seen := make(map[string]bool)
	for keys.More() {
		keyToken, err := keys.Token()
		if err != nil {
			return nativeLearnedAction{}, fmt.Errorf("UNSUPPORTED: malformed action key: %w", err)
		}
		key, ok := keyToken.(string)
		if !ok || seen[key] {
			return nativeLearnedAction{}, fmt.Errorf("UNSUPPORTED: repeated/invalid action key %q", key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := keys.Decode(&value); err != nil {
			return nativeLearnedAction{}, fmt.Errorf("UNSUPPORTED: malformed action value: %w", err)
		}
	}
	end, err := keys.Token()
	if err != nil || end != json.Delim('}') {
		return nativeLearnedAction{}, errors.New("UNSUPPORTED: invalid native action termination")
	}
	if _, err := keys.Token(); err != io.EOF {
		return nativeLearnedAction{}, errors.New("UNSUPPORTED: extra native action payload")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var action nativeLearnedAction
	if err := decoder.Decode(&action); err != nil {
		return nativeLearnedAction{}, fmt.Errorf("UNSUPPORTED: malformed typed action: %w", err)
	}
	if action != audited || len(seen) != 8 || !strings.HasPrefix(action.ID, "validate.") {
		return nativeLearnedAction{}, errors.New("UNSUPPORTED: action is not registered or has changed semantics")
	}
	return action, nil
}
