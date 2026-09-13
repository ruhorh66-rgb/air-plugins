//go:build !windows

package main

// На не-Windows отдельного «окружения пользователя» нет: переменные приходят процессу
// обычным путём, и подставлять нечего.
func userEnvVar(string) string { return "" }
