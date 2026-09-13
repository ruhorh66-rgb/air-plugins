//go:build windows

package main

import (
	"syscall"
	"unicode/utf8"
	"unsafe"
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procSetConsoleOutCP  = kernel32.NewProc("SetConsoleOutputCP")
	procMultiByteToWide  = kernel32.NewProc("MultiByteToWideChar")
	procGetConsoleOutCP  = kernel32.NewProc("GetConsoleOutputCP")
)

const cpUTF8 = 65001

// setConsoleUTF8 переводит консоль в UTF-8 на время работы.
//
// Нужно ровно для одного случая — когда человек смотрит вывод глазами в окне. Весь
// машинный обмен (файлы, перенаправленный вывод) и так UTF-8, потому что в Go иначе не
// бывает. Ошибку вызова глотаем намеренно: перенаправленный вывод консоли не имеет, и
// отказ здесь ничего не означает.
func setConsoleUTF8() {
	procSetConsoleOutCP.Call(uintptr(cpUTF8))
}

// decodeOutput возвращает текст вывода дочернего процесса.
//
// ПОЧЕМУ НЕ ПРОСТО string(b). Проверки продукта — чужие программы, и пишут они в
// кодировке консоли, а не в нашей. Скрипты мы зовём с преамбулой, ставящей им UTF-8, но
// произвольная команда такой преамбулы не имеет: её вывод придёт в OEM-кодировке, и
// кириллица в причине отказа превратится в мусор. Причина, дожившая до человека
// нечитаемой, равна её отсутствию — это уже стоило нам одного прогона 12.09.2026.
//
// Порядок: сперва UTF-8, и только если байты в нём не разбираются — кодовая страница
// консоли средствами самой ОС. Своей таблицы перекодировки не держим: она устареет
// молча, а MultiByteToWideChar знает то же, что и сама консоль.
func decodeOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if utf8.Valid(b) {
		return string(b)
	}
	cp, _, _ := procGetConsoleOutCP.Call()
	if cp == 0 {
		cp = 866 // OEM-кириллица: разумное умолчание, когда консоли нет вовсе
	}
	n, _, _ := procMultiByteToWide.Call(cp, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return string(b) // не разобралось — отдаём как есть, а не теряем
	}
	buf := make([]uint16, n)
	procMultiByteToWide.Call(cp, 0, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
		uintptr(unsafe.Pointer(&buf[0])), n)
	return syscall.UTF16ToString(buf)
}
