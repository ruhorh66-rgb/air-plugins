//go:build windows

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// НЕСКОЛЬКО СЕССИЙ НА ОДНОЙ МАШИНЕ — ЭТО НОРМА, А НЕ ИСКЛЮЧЕНИЕ.
//
// Указание ЛПР 14.09.2026: «будет несколько сессий... надо реализовать, чтобы не было
// конфликтов, несколько сессий, которые будут работать с бинарником».
//
// Вводная поздняя, но она не про удобство. Разбор показал ТРИ места, где две сессии
// портят работу друг другу, и все три — молча:
//
//  1. УСТАНОВКА. Две одновременные `install` останавливают значок, копируют файлы и
//     запускают его вперемешку. Итог — половина: новый CLI и старый значок либо
//     наоборот. Отказ «Access is denied» при замене .exe мы уже ловили, и он приходит
//     не всегда: иногда копирование успевает, и расхождение остаётся незамеченным.
//
//  2. ПЕТЛЯ. Два исполнителя, правящих одно дерево, — худшее из всего. Каждый видит
//     чужие изменения как свои, судья меряет смесь, а расстояние до цели перестаёт
//     что-либо значить. Механизм, который врёт про расстояние, хуже отсутствующего.
//
//  3. ВЕРДИКТ. `os.WriteFile` НЕ АТОМАРЕН: читатель может застать файл усечённым, и
//     «вердикта нет» станет неотличимо от «вердикт пуст». Здесь замок не нужен вовсе —
//     нужна атомарная запись, и это дешевле и надёжнее любого замка.
//
// ЗАМОК — ИМЕНОВАННЫЙ МЬЮТЕКС ОС, а не файл-флажок. Файл переживает смерть процесса и
// остаётся висеть навсегда; мьютекс ОС снимается операционной системой, когда владелец
// умирает, как бы он ни умер. Это и есть разница между «замок» и «записка о замке».
//
// ОБЛАСТЬ `Local\` — сеанс Windows, а не вся машина. Так и нужно: значок живёт на
// рабочем столе сеанса, и две разные сессии Windows — это два разных рабочих стола.

var (
	// CreateMutexW живёт в kernel32, а не в advapi32. Первая редакция брала её из
	// advapi32 (рядом лежат функции реестра), и продукт ПАДАЛ на первом же вызове:
	// ленивая загрузка проверяет наличие процедуры только в момент обращения, поэтому
	// сборка и vet молчали, а установка рухнула панике на живой машине.
	//
	// Урок ровно тот же, что механизм ловит у продуктов: резолв не доказывает наличия,
	// доказывает только ОТВЕТ. Здесь его доказал прогон, и никакая проверка формы
	// доказать не могла.
	procCreateMutexWLock = kernel32Lock.NewProc("CreateMutexW")
	procOpenMutexW       = kernel32Lock.NewProc("OpenMutexW")
	procReleaseMutex     = kernel32Lock.NewProc("ReleaseMutex")
	procCloseHandleLock  = kernel32Lock.NewProc("CloseHandle")
)

var kernel32Lock = syscall.NewLazyDLL("kernel32.dll")

const (
	errAlreadyExistsLock = 183
	mutexAllAccess       = 0x1F0001
)

// lockName делает имя мьютекса из пути продукта. Путь в имя не годится: в именах
// объектов ядра запрещена обратная косая черта, и «F:\-7-\air-worker» превратилось бы
// в подобъект несуществующего пространства имён. Хэш решает это и заодно уравнивает
// длину — предел имени объекта короче, чем бывают пути.
func lockName(kind, path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	sum := sha1.Sum([]byte(strings.ToLower(filepath.Clean(abs))))
	return `Local\air-worker-` + kind + "-" + hex.EncodeToString(sum[:8])
}

type osLock struct{ h syscall.Handle }

// acquireLock берёт замок БЕЗ ОЖИДАНИЯ. Ждать здесь неправильно: вызывающий должен
// узнать, что работа уже идёт, и сказать это человеку, а не молча простоять минуту и
// сделать то же самое вторым.
func acquireLock(name string) (*osLock, bool) {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		// Имя не собралось — это наша ошибка, и она не повод остановить работу.
		// Возвращаем «взяли», но с пустым дескриптором: хуже незамкнутой работы была
		// бы работа, отменённая из-за дефекта самого замка.
		return &osLock{}, true
	}
	// ОШИБКА БЕРЁТСЯ ТРЕТЬИМ ЗНАЧЕНИЕМ ВЫЗОВА, а не отдельным GetLastError.
	// Отдельный вызов — это второй переход в ядро, и между ними рантайм Go вправе
	// увести горутину на другой поток ОС; код последней ошибки живёт в ПОТОКЕ, и
	// прочитан был бы чужой. Классическая ловушка, и молчаливая: замок изредка
	// «свободен», когда он занят.
	h, _, callErr := procCreateMutexWLock.Call(0, 1, uintptr(unsafe.Pointer(n)))
	if h == 0 {
		return &osLock{}, true
	}
	if errno, ok := callErr.(syscall.Errno); ok && uintptr(errno) == errAlreadyExistsLock {
		procCloseHandleLock.Call(h)
		return nil, false
	}
	return &osLock{h: syscall.Handle(h)}, true
}

func (l *osLock) release() {
	if l == nil || l.h == 0 {
		return
	}
	procReleaseMutex.Call(uintptr(l.h))
	procCloseHandleLock.Call(uintptr(l.h))
	l.h = 0
}

// lockHeld отвечает, держит ли замок КТО-ТО ДРУГОЙ. Не берёт его и не меняет ничего —
// нужен там, где мы только спрашиваем «уже работает?», например перед запуском значка.
func lockHeld(name string) bool {
	n, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	h, _, _ := procOpenMutexW.Call(mutexAllAccess, 0, uintptr(unsafe.Pointer(n)))
	if h == 0 {
		return false
	}
	procCloseHandleLock.Call(h)
	return true
}

// writeFileAtomic — запись через временный файл и перенос. Перенос в пределах одного
// тома неделим, и читатель никогда не увидит полузаписанного файла. Применяется к
// вердикту: его читают и страж, и двигатель, и соседние сессии.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp := filepath.Join(dir, fmt.Sprintf(".%s.%d.tmp", filepath.Base(path), os.Getpid()))
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
