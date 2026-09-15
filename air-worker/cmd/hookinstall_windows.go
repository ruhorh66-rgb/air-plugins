//go:build windows

package main

// hookInstallHome — каталог установки air-worker для стража обхода (classifyBypass,
// handlePreToolUseBypassGuard): та же величина, что install_windows.go уже вычисляет для
// установки самого продукта. Второго вычисления пути установки не заводится.
func hookInstallHome() string {
	return installHome()
}
