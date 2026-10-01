//go:build windows

package main

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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
	procCreateMutexWLock           = kernel32Lock.NewProc("CreateMutexW")
	procOpenMutexW                 = kernel32Lock.NewProc("OpenMutexW")
	procReleaseMutex               = kernel32Lock.NewProc("ReleaseMutex")
	procCloseHandleLock            = kernel32Lock.NewProc("CloseHandle")
	procSetFileInformationByHandle = kernel32Lock.NewProc("SetFileInformationByHandle")
)

var kernel32Lock = syscall.NewLazyDLL("kernel32.dll")

var (
	writeFileAtomicRetryTimeout = 2 * time.Second
	writeFileAtomicRetryDelay   = 10 * time.Millisecond
)

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
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	deadline := time.Now().Add(writeFileAtomicRetryTimeout)
	delay := writeFileAtomicRetryDelay
	posixSupported := true
	for {
		var err error
		if posixSupported {
			err = renamePosixWindows(tmp, path)
			if errors.Is(err, syscall.Errno(1)) || errors.Is(err, syscall.Errno(3)) ||
				errors.Is(err, syscall.Errno(50)) || errors.Is(err, syscall.Errno(87)) ||
				errors.Is(err, syscall.Errno(120)) || errors.Is(err, syscall.Errno(123)) {
				posixSupported = false
			}
		}
		if !posixSupported {
			err = os.Rename(tmp, path)
		}
		if err == nil {
			return nil
		}
		if !retryableRenameError(err) {
			return fmt.Errorf("atomic replace %q: %w", path, err)
		}

		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("atomic replace %q timed out; target may be held without FILE_SHARE_DELETE (holder PID unknown): %w", path, err)
		}
		if delay > remaining {
			delay = remaining
		}
		time.Sleep(delay)
		if delay < 100*time.Millisecond {
			delay *= 2
			if delay > 100*time.Millisecond {
				delay = 100 * time.Millisecond
			}
		}
	}
}

type fileRenameInfo struct {
	Flags          uint32
	RootDirectory  syscall.Handle
	FileNameLength uint32
	FileName       [1]uint16
}

func renamePosixWindows(from, to string) error {
	absolute, err := filepath.Abs(to)
	if err != nil {
		return err
	}
	name, err := syscall.UTF16FromString(absolute)
	if err != nil {
		return err
	}
	offset := int(unsafe.Offsetof(fileRenameInfo{}.FileName))
	buf := make([]byte, offset+len(name)*2)
	info := (*fileRenameInfo)(unsafe.Pointer(&buf[0]))
	info.Flags = 0x1 | 0x2 // REPLACE_IF_EXISTS | POSIX_SEMANTICS
	info.FileNameLength = uint32((len(name) - 1) * 2)
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&buf[offset])), len(name)), name)
	source, err := syscall.UTF16PtrFromString(from)
	if err != nil {
		return err
	}
	h, err := syscall.CreateFile(source, 0x00010000, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0) // DELETE access
	if err != nil {
		return &os.LinkError{Op: "open rename source", Old: from, New: to, Err: err}
	}
	defer syscall.CloseHandle(h)
	r1, _, callErr := procSetFileInformationByHandle.Call(uintptr(h), 22, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))) // FileRenameInfoEx
	if r1 == 0 {
		return &os.LinkError{Op: "set FileRenameInfoEx", Old: from, New: to, Err: callErr}
	}
	return nil
}

func retryableRenameError(err error) bool {
	return errors.Is(err, syscall.Errno(5)) ||
		errors.Is(err, syscall.Errno(32)) ||
		errors.Is(err, syscall.Errno(33))
}
