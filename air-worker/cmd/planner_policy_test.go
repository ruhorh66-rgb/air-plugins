package main

import (
	"strings"
	"testing"
)

func TestPlannerPromptRequiresScriptFirst(t *testing.T) {
	prompt := buildPlannerPrompt("goal", "{}", "task", "body", []string{"script", "sonnet"}, []string{"tests"})
	required := []string{
		"СНАЧАЛА проверь, можно ли получить и проверить точный результат скриптом",
		"Если да — назначь script; вызов любой модели для такого шага запрещён",
		"бинарник отклоняет эту команду на model tier",
		"детерминированное с проверяемым точным выходом -> script",
	}
	for _, phrase := range required {
		if !strings.Contains(prompt, phrase) {
			t.Fatalf("planner prompt lost script-first contract %q", phrase)
		}
	}
}
