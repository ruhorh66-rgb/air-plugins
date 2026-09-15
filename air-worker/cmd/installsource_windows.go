//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

// ПОВЫШЕНЫ ЛИ ПРАВА ПРОЦЕССА. Штатная установка НЕ требует повышения (файлы идут в
// пользовательскую область — так и было до шага 66). Повышение здесь значит другое:
// это ЛПР запустил бинарник с правами руками, и тогда прямая переустановка разрешена
// из любого источника и с понижением версии. Отличить один случай от другого можно
// только по токену процесса — форма команды или путь этого не доказывают.
//
// Читается тем же классом API ОС, что и остальное окружение (userenv_windows.go,
// userpath_windows.go): OpenProcessToken + GetTokenInformation(TokenElevation). Свою
// таблицу групп не держим — вопрос «поднят ли токен» ОС отвечает одним полем.
//
// x/sys не подключаем намеренно: модуль собирается из стандартной библиотеки без сети
// и go.sum, поэтому вызовы идут через syscall.LazyDLL, как в соседних файлах.

var (
	advapi32Src             = syscall.NewLazyDLL("advapi32.dll")
	procOpenProcessTokenSrc = advapi32Src.NewProc("OpenProcessToken")
	procGetTokenInformSrc   = advapi32Src.NewProc("GetTokenInformation")
)

const (
	tokenQuery          = 0x0008
	tokenElevationClass = 20 // TokenElevation
)

func isElevatedProcess() bool {
	proc, err := syscall.GetCurrentProcess()
	if err != nil {
		return false
	}
	var tok syscall.Handle
	r, _, _ := procOpenProcessTokenSrc.Call(uintptr(proc), tokenQuery, uintptr(unsafe.Pointer(&tok)))
	if r == 0 {
		return false
	}
	defer syscall.CloseHandle(tok)

	var elevated uint32
	var retLen uint32
	r, _, _ = procGetTokenInformSrc.Call(uintptr(tok), tokenElevationClass,
		uintptr(unsafe.Pointer(&elevated)), unsafe.Sizeof(elevated), uintptr(unsafe.Pointer(&retLen)))
	if r == 0 {
		return false
	}
	return elevated != 0
}
