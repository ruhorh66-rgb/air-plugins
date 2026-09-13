//go:build !windows

package main

// Заглушки для не-Windows. Механизм живёт на Windows, но собираться и ТЕСТИРОВАТЬСЯ он
// обязан везде: тест, который нельзя прогнать на другой машине, проверяет машину, а не
// код. На не-Windows кодировка вывода — UTF-8 по умолчанию, делать нечего.

func setConsoleUTF8() {}

func decodeOutput(b []byte) string { return string(b) }
