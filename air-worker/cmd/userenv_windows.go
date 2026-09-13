//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// ПЕРЕМЕННАЯ ОКРУЖЕНИЯ ПОЛЬЗОВАТЕЛЯ — ИЗ РЕЕСТРА, А НЕ ИЗ СВОЕГО ПРОЦЕССА.
//
// Найдено живым прогоном 13.09.2026, и это ЧЕТВЁРТЫЙ раз, когда этот класс кусает
// продукт, и ВТОРОЙ, когда я вношу его переносом.
//
// Симптом: петля сделала настоящую итерацию, claude вернул один ход и ноль расхода,
// судья не сдвинулся. Выглядело как «модель не справилась». На деле дочерний процесс
// отвечал «Not logged in» — токен был задан на уровне ПОЛЬЗОВАТЕЛЯ, а процесс петли
// стартовал раньше, чем переменную поставили, и своего окружения не перечитывает. Новые
// переменные пользователя видят только процессы, стартовавшие после.
//
// Ровно эта починка уже есть в питоновской обёртке продукта (claude_judge_run.py,
// _environment_with_token). Перенося петлю в Go, я взяла код и не взяла его окружение —
// формулировка AIR-ENV-002 того же дня: перенос теряет не код, а то, что было ВОКРУГ него.
//
// ЧИТАЕТСЯ ЧЕРЕЗ API ОС, А НЕ ПОДПРОЦЕССОМ. Вызвать powershell и прочитать значение из
// его вывода было бы короче, но протащило бы секрет через трубу и командную строку.
// Контракт продукта исключает секреты из протокола; значение здесь не печатается, не
// логируется и не сравнивается — только подставляется дочернему процессу.

var (
	advapi32          = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValue = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey   = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	regSZ           = 1
	regExpandSZ     = 2
)

// userEnvVar возвращает значение переменной из HKCU\Environment либо пустую строку.
// Пустая строка и отсутствие ключа здесь неразличимы намеренно: и то и другое означает
// «нечем подставить», а разница ни на что не влияет.
func userEnvVar(name string) string {
	sub, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return ""
	}
	var h syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
		0, uintptr(keyRead), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return ""
	}
	defer procRegCloseKey.Call(uintptr(h))

	val, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	var typ uint32
	var size uint32
	r, _, _ = procRegQueryValue.Call(uintptr(h), uintptr(unsafe.Pointer(val)), 0,
		uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if r != 0 || size == 0 || (typ != regSZ && typ != regExpandSZ) {
		return ""
	}
	buf := make([]uint16, size/2+1)
	r, _, _ = procRegQueryValue.Call(uintptr(h), uintptr(unsafe.Pointer(val)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}
