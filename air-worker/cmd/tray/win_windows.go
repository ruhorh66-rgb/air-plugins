//go:build windows

package main

import (
	"syscall"
)

// ЗНАЧОК В ТРЕЕ — ПРЯМЫМИ ВЫЗОВАМИ ОС, БЕЗ БИБЛИОТЕК.
//
// Указание ЛПР 14.09.2026: «хочу, чтобы уже в трее висел наш air-worker, чтобы он уже
// работал, чтобы там значок висел». Отдельный бинарник, а не режим CLI: подсистема у
// него GUI (-H windowsgui), иначе при автозапуске мигало бы окно консоли.
//
// ПОЧЕМУ НЕ getlantern/systray, которым сделан трей ASW. Замер: systray тянет
// github.com/lxn/walk, а рабочая версия walk на этой машине — ПРАВЛЕНАЯ КОПИЯ внутри
// ASW (third_party/walk, там устранён отказ регистрации подсказки). Взять systray
// значило бы связать два продукта через чужой форк: air-worker перестал бы собираться
// без каталога ASW, а правка в ASW молча меняла бы поведение механизма. Цена отказа —
// примерно триста строк связываний ниже; цена согласия — зависимость продукта от
// чужого дерева, которую нечем проверить.
//
// Своих зависимостей у бинарника по-прежнему НЕТ: он ставится копированием одного
// файла и потому не требует повышения прав.

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW     = user32.NewProc("RegisterClassExW")
	procCreateWindowExW      = user32.NewProc("CreateWindowExW")
	procDefWindowProcW       = user32.NewProc("DefWindowProcW")
	procDestroyWindow        = user32.NewProc("DestroyWindow")
	procGetMessageW          = user32.NewProc("GetMessageW")
	procTranslateMessage     = user32.NewProc("TranslateMessage")
	procDispatchMessageW     = user32.NewProc("DispatchMessageW")
	procPostQuitMessage      = user32.NewProc("PostQuitMessage")
	procCreatePopupMenu      = user32.NewProc("CreatePopupMenu")
	procDestroyMenu          = user32.NewProc("DestroyMenu")
	procAppendMenuW          = user32.NewProc("AppendMenuW")
	procTrackPopupMenu       = user32.NewProc("TrackPopupMenu")
	procGetCursorPos         = user32.NewProc("GetCursorPos")
	procSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	procPostMessageW         = user32.NewProc("PostMessageW")
	procSetTimer             = user32.NewProc("SetTimer")
	procRegisterWindowMsgW   = user32.NewProc("RegisterWindowMessageW")
	procCreateIconFromResEx  = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon          = user32.NewProc("DestroyIcon")
	procShellNotifyIconW     = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW        = shell32.NewProc("ShellExecuteW")
	procGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW         = kernel32.NewProc("CreateMutexW")
	procGetLastErrorKernel32 = kernel32.NewProc("GetLastError")
)

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmCommand     = 0x0111
	wmTimer       = 0x0113
	wmRBUTTONUP   = 0x0205
	wmLBUTTONUP   = 0x0202
	wmLBUTTONDBL  = 0x0203
	wmUserTrayMsg = 0x0400 + 1 // WM_APP-подобное: своё сообщение значка

	nimAdd    = 0x00000000
	nimModify = 0x00000001
	nimDelete = 0x00000002

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004

	mfString    = 0x00000000
	mfSeparator = 0x00000800
	mfGrayed    = 0x00000001

	tpmLeftAlign   = 0x0000
	tpmRightButton = 0x0002

	errAlreadyExists = 183
)

type point struct{ X, Y int32 }

type msg struct {
	HWnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

// notifyIconData — ТОЛЬКО поля до szTip включительно, и размер объявляется
// соответствующий (NOTIFYICONDATA_V2_SIZE, 488 байт на 64-битной Windows).
// Объявить полную структуру и не заполнить хвост значило бы отдать ОС мусор в полях,
// о которых мы ничего не утверждаем.
type notifyIconData struct {
	Size             uint32
	Wnd              syscall.Handle
	ID               uint32
	Flags            uint32
	CallbackMessage  uint32
	Icon             syscall.Handle
	Tip              [128]uint16
	State            uint32
	StateMask        uint32
	Info             [256]uint16
	TimeoutOrVersion uint32
	InfoTitle        [64]uint16
	InfoFlags        uint32
}

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		// Строка с нулевым байтом внутри — наша ошибка, а не пользователя.
		// Подсказка теряется, значок остаётся: молчание здесь лучше падения.
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

// copyTip кладёт подсказку в массив фиксированной длины с ОБРЕЗКОЙ ПО ГРАНИЦЕ
// кодовой единицы и обязательным нулём. Win32 читает до нуля; массив без нуля —
// чтение за пределами, то есть мусор в подсказке или падение проводника.
func copyTip(dst *[128]uint16, s string) {
	r := syscall.StringToUTF16(s)
	n := len(r)
	if n > len(dst)-1 {
		n = len(dst) - 1
	}
	copy(dst[:n], r[:n])
	dst[n] = 0
}
