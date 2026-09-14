//go:build windows

package main

import (
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

// PATH ПОЛЬЗОВАТЕЛЯ — ЧТОБЫ ПРОДУКТ МОЖНО БЫЛО ПОЗВАТЬ ПО ИМЕНИ.
//
// Зачем это понадобилось, дословно из разбора 14.09.2026. AIR-ENV-002 спросила, как
// продукту, который НЕ везёт бинарник внутри себя, сослаться на судью и двигатель, и
// назвала три своих варианта: абсолютный путь в клон, junction как псевдоним,
// переменная окружения. Все три плохи, и каждый — по своей причине:
//
//	абсолютный путь в клон — привязан к машине; на второй он ведёт в пустоту;
//	адрес плагина — ВЕРСИОННЫЙ (в кэше рядом лежат 0.7.1 и 0.8.0), и ломается на
//	  каждом обновлении;
//	junction — псевдоним, который заводят руками, а его пропажа не видна ничем.
//	  Замер того же дня: junction на скил был снят, и это обнаружилось только когда
//	  страж хода отклонил ход по другой причине.
//
// Правильный ответ — тот, который даёт УСТАНОВКА: стабильный адрес без версии, и он
// должен находиться по имени. Поэтому install дописывает свой bin в PATH ПОЛЬЗОВАТЕЛЯ,
// а продукты пишут в постановке просто `air-worker judge -product .`.
//
// ТРИ ПРАВИЛА БЕЗОПАСНОСТИ, потому что PATH — общий ресурс, и испортить его дорого:
//
//  1. ЧИТАЕМ ПЕРЕД ЗАПИСЬЮ И РАЗЛИЧАЕМ «ключа нет» ОТ «прочитать не смогли». Первое —
//     нормальное состояние (пользовательский PATH пуст), и создать его можно. Второе —
//     отказ, и запись в этом случае ЗАПРЕЩЕНА: она заменила бы чужой PATH нашим одним
//     каталогом, и это не чинится ничем, кроме памяти человека.
//  2. ТИП ЗНАЧЕНИЯ СОХРАНЯЕТСЯ. Пользовательский PATH почти всегда REG_EXPAND_SZ —
//     в нём живут %USERPROFILE% и подобное. Переписать его как REG_SZ значит превратить
//     проценты в буквы, и половина путей перестанет существовать.
//  3. ИДЕМПОТЕНТНОСТЬ. Повторная установка не добавляет второй копии: PATH, растущий на
//     каждый прогон, однажды упирается в предел длины и обрезается молча.

const (
	regExpandSZType = 2
	hwndBroadcast   = 0xffff
	wmSettingChange = 0x001A
	smtoAbortIfHung = 0x0002
)

var (
	procRegQueryValueEx2   = advapi32Install.NewProc("RegQueryValueExW")
	procSendMessageTimeout = user32Install.NewProc("SendMessageTimeoutW")
)

// readUserPath возвращает значение, его тип и флаг «значение существует».
// Ошибка означает «прочитать не смогли» — это НЕ то же самое, что «значения нет»,
// и вызывающий обязан различать: см. правило 1 в шапке.
func readUserPath() (value string, typ uint32, exists bool, err error) {
	sub, e := syscall.UTF16PtrFromString(`Environment`)
	if e != nil {
		return "", 0, false, e
	}
	var h syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
		0, uintptr(keyRead), uintptr(unsafe.Pointer(&h)))
	if r == 2 {
		// Ветви нет вовсе — у пользователя нет собственного окружения. Создать можно.
		return "", regExpandSZType, false, nil
	}
	if r != 0 {
		return "", 0, false, fmt.Errorf("ветвь окружения не открылась, код %d", r)
	}
	defer procRegCloseKey.Call(uintptr(h))

	name, e := syscall.UTF16PtrFromString("Path")
	if e != nil {
		return "", 0, false, e
	}
	var size uint32
	r, _, _ = procRegQueryValueEx2.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if r == 2 {
		return "", regExpandSZType, false, nil // значения нет — это нормально
	}
	if r != 0 {
		return "", 0, false, fmt.Errorf("длина Path не прочиталась, код %d", r)
	}
	if size == 0 {
		return "", typ, true, nil
	}
	buf := make([]uint16, size/2+1)
	r, _, _ = procRegQueryValueEx2.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
	if r != 0 {
		return "", 0, false, fmt.Errorf("Path не прочитался, код %d", r)
	}
	return syscall.UTF16ToString(buf), typ, true, nil
}

func writeUserPath(value string, typ uint32) error {
	sub, err := syscall.UTF16PtrFromString(`Environment`)
	if err != nil {
		return err
	}
	var h syscall.Handle
	var disp uint32
	r, _, _ := procRegCreateKeyExW.Call(uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
		0, 0, 0, uintptr(keyWrite), 0, uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&disp)))
	if r != 0 {
		return fmt.Errorf("ветвь окружения не открылась на запись, код %d", r)
	}
	defer procRegCloseKey.Call(uintptr(h))

	name, err := syscall.UTF16PtrFromString("Path")
	if err != nil {
		return err
	}
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	if typ == 0 {
		typ = regExpandSZType
	}
	r, _, _ = procRegSetValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, uintptr(typ),
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r != 0 {
		return fmt.Errorf("Path не записан, код %d", r)
	}

	// Оповещение об изменении окружения. Без него новые процессы увидят новый PATH
	// только после перезахода пользователя, и установка выглядела бы не сработавшей.
	// Уже запущенные процессы не увидят его НИКОГДА — своё окружение они держат копией;
	// ровно этот факт стоил нам прогона петли 13.09.2026.
	env, _ := syscall.UTF16PtrFromString("Environment")
	procSendMessageTimeout.Call(hwndBroadcast, wmSettingChange, 0,
		uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 3000, 0)
	return nil
}

func pathHas(pathValue, dir string) bool {
	want := strings.ToLower(strings.TrimRight(dir, `\`))
	for _, part := range strings.Split(pathValue, ";") {
		if strings.ToLower(strings.TrimRight(strings.TrimSpace(part), `\`)) == want {
			return true
		}
	}
	return false
}

// addToUserPath дописывает каталог. Возвращает «изменили ли» и ошибку.
func addToUserPath(dir string) (bool, error) {
	cur, typ, _, err := readUserPath()
	if err != nil {
		// Правило 1: не прочитали — не пишем.
		return false, err
	}
	if pathHas(cur, dir) {
		return false, nil
	}
	next := dir
	if strings.TrimSpace(cur) != "" {
		next = strings.TrimRight(cur, "; ") + ";" + dir
	}
	if err := writeUserPath(next, typ); err != nil {
		return false, err
	}
	return true, nil
}

func removeFromUserPath(dir string) (bool, error) {
	cur, typ, exists, err := readUserPath()
	if err != nil {
		return false, err
	}
	if !exists || !pathHas(cur, dir) {
		return false, nil
	}
	want := strings.ToLower(strings.TrimRight(dir, `\`))
	var kept []string
	for _, part := range strings.Split(cur, ";") {
		if strings.ToLower(strings.TrimRight(strings.TrimSpace(part), `\`)) == want {
			continue
		}
		if strings.TrimSpace(part) == "" {
			continue
		}
		kept = append(kept, part)
	}
	if err := writeUserPath(strings.Join(kept, ";"), typ); err != nil {
		return false, err
	}
	return true, nil
}
