//go:build !windows

package main

// На не-Windows установки со значком нет (cmdInstall — заглушка в install_other.go),
// и понятие «повышенные права процесса» тут не используется. Заглушка держит символ
// определённым для сборки под другой ОС.
func isElevatedProcess() bool { return false }
