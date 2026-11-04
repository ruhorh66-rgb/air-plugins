package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Claude route diagnostics are intentionally read-only. A bare "claude auth status"
// in the host shell is not proof that AirWorker's Claude subprocess lacks OAuth:
// runnerEnv also loads CLAUDE_CODE_OAUTH_TOKEN from the user's environment.
type claudeRoutePreflight struct {
	Schema          string   `json:"schema"`
	ToolResolved    bool     `json:"tool_resolved"`
	ToolRule        string   `json:"tool_rule,omitempty"`
	ProfileKnown    bool     `json:"profile_known"`
	OAuthSource     string   `json:"oauth_source"`
	HeadroomReady   bool     `json:"headroom_ready"`
	HeadroomVersion string   `json:"headroom_version,omitempty"`
	ReviewerModel   string   `json:"reviewer_model"`
	ReviewerEffort  string   `json:"reviewer_effort"`
	ModelCall       string   `json:"model_call"`
	Status          string   `json:"status"`
	Problems        []string `json:"problems,omitempty"`
}

func inspectClaudeRoute() (claudeRoutePreflight, int) {
	out := claudeRoutePreflight{
		Schema:         "air-worker.claude-route-preflight/v1",
		OAuthSource:    "none_observed",
		ReviewerModel:  "claude-sonnet-5-5",
		ReviewerEffort: "low",
		ModelCall:      "NOT_RUN",
		Status:         "HOLD_NOT_PROVEN",
	}
	if _, rule, err := resolveRunnerToolWhy("claude"); err != nil {
		out.Problems = append(out.Problems, "claude executable not resolved")
	} else {
		out.ToolResolved, out.ToolRule = true, rule
	}
	if dir, err := activeClaudeConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		out.ProfileKnown = true
	} else {
		out.Problems = append(out.Problems, "active Claude profile not resolved")
	}
	if os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		out.OAuthSource = "process_env"
	} else if userEnvVar("CLAUDE_CODE_OAUTH_TOKEN") != "" {
		out.OAuthSource = "user_env"
	} else {
		out.Problems = append(out.Problems, "OAuth not observed in runner environment; credential store not assessed")
	}
	if h, err := headroomPreflight(); err != nil {
		out.Problems = append(out.Problems, "Headroom health not proven (may be transient)")
	} else {
		out.HeadroomReady, out.HeadroomVersion = true, h.Version
	}
	return evaluateClaudeRouteReadiness(out)
}

// Readiness is deliberately distinct from a successful model invocation.
// Never claim a subscription was revoked from a bare shell auth status.
func evaluateClaudeRouteReadiness(out claudeRoutePreflight) (claudeRoutePreflight, int) {
	if out.ToolResolved && out.ProfileKnown && out.OAuthSource != "none_observed" && out.HeadroomReady {
		out.Status = "READY_TO_TRY_NATIVE_REVIEW"
		return out, 0
	}
	out.Status = "HOLD_NOT_PROVEN"
	return out, 2
}

func cmdClaudeRoutePreflight(asJSON bool) int {
	doc, code := inspectClaudeRoute()
	if asJSON {
		_ = json.NewEncoder(os.Stdout).Encode(doc)
		return code
	}
	fmt.Printf("Claude native route: %s · tool=%t profile=%t oauth=%s headroom=%t (%s) · reviewer=%s:%s · model_call=%s"+lineEnding,
		doc.Status, doc.ToolResolved, doc.ProfileKnown, doc.OAuthSource,
		doc.HeadroomReady, doc.HeadroomVersion, doc.ReviewerModel, doc.ReviewerEffort, doc.ModelCall)
	for _, problem := range doc.Problems {
		fmt.Printf("  %s"+lineEnding, problem)
	}
	return code
}
