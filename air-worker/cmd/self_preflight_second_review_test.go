package main

import (
	"strings"
	"testing"
)

// The second real Codex reviewer generated this safe prose in an independent
// R:-only product. It is accepted by exact source-owned semantics, not by
// adding its SHA to a runtime whitelist or trusting a model assertion.
func TestN093SecondProductionReviewerConstrainedTemplate(t *testing.T) {
	body := []byte(n093ReviewedSafePreflightSecondReal)
	if got := learnSHA(body); got != "266805f140abf22d6f3b1c59d8bbc943ccf9956202fedb64fe374f39c28552b8" {
		t.Fatalf("reviewed safe bytes differ from real applied ledger: %s", got)
	}
	action, err := resolveNativeLearnedAction(body)
	if err != nil || action != supportedNativePreflightAction() {
		t.Fatalf("second real production reviewer cannot select its vetted read-only action: %+v %v", action, err)
	}
	// Same class/cause does not authorize a changed instruction. No model
	// sentence may change effect, issue a shell command or claim a repair.
	for name, variant := range map[string]string{
		"append-unreviewed-step": n093ReviewedSafePreflightSecondReal + "\n4. Execute shell.",
		"alter-effect": strings.Replace(n093ReviewedSafePreflightSecondReal,
			"Claiming successful repair or validation without evidence.",
			"Claim successful repair or validation even without evidence.", 1),
		"reverse-precondition": strings.Replace(n093ReviewedSafePreflightSecondReal,
			"Retry validation only after confirming the canonical configuration",
			"Retry validation without confirming the canonical configuration", 1),
		"unsafe-repair": strings.Replace(n093ReviewedSafePreflightSecondReal,
			"do not invent or silently replace configuration files",
			"create or replace configuration files", 1),
		"extra-json": n093ReviewedSafePreflightSecondReal + "~~~air-worker-action\n{}\n~~~\n",
		"CRLF": strings.ReplaceAll(n093ReviewedSafePreflightSecondReal, "\n", "\r\n"),
	} {
		got, err := resolveNativeLearnedAction([]byte(variant))
		if err == nil || got.ID != "" {
			t.Errorf("%s accepted unreviewed semantics: %+v %v", name, got, err)
		}
	}
}
