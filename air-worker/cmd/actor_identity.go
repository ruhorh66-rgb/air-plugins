package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	actorKindGPTWindow     = "gpt-window"
	actorKindClaudeSession = "claude-session"
)

func resolveMutationActor(kind, name, fallback string) (string, error) {
	kind = strings.TrimSpace(kind)
	name = strings.TrimSpace(name)
	if kind == "" {
		kind = strings.TrimSpace(os.Getenv("AIR_WORKER_ACTOR_KIND"))
	}
	if name == "" {
		name = strings.TrimSpace(os.Getenv("AIR_WORKER_ACTOR_NAME"))
	}
	if kind == "" && name == "" {
		return strings.TrimSpace(fallback), nil
	}
	if kind == "" || name == "" {
		return "", fmt.Errorf("actor identity requires both kind and name")
	}
	switch kind {
	case actorKindGPTWindow, actorKindClaudeSession:
	default:
		return "", fmt.Errorf("unsupported actor kind %q (want %s or %s)", kind, actorKindGPTWindow, actorKindClaudeSession)
	}
	if strings.ContainsAny(name, "\r\n\x00") {
		return "", fmt.Errorf("actor name contains a control character")
	}
	return kind + ":" + name, nil
}
