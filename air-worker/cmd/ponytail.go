package main

import "strings"

const ponytailSkillInvocation = "@ponytail full"

// ponytailPrompt activates the already installed Codex Ponytail skill per invocation.
// This keeps the AirWorker contract independent of interactive Codex session state.
func ponytailPrompt(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if strings.HasPrefix(prompt, ponytailSkillInvocation) {
		return prompt
	}
	if prompt == "" {
		return ponytailSkillInvocation
	}
	return ponytailSkillInvocation + "\n\n" + prompt
}
