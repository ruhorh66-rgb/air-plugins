//go:build !windows

package main

// На не-Windows отдельного «окружения пользователя» нет: переменные приходят процессу
// обычным путём, и подставлять нечего.
func userEnvVar(string) string                  { return "" }
func userEnvVarIn(string, string) string        { return "" }
func userDWORDIn(string, string) (uint32, bool) { return 0, false }
