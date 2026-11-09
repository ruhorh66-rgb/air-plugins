//go:build windows

package main

import (
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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
		return true
	}
	h, _, callErr := procOpenMutexW.Call(mutexAllAccess, 0, uintptr(unsafe.Pointer(n)))
	if h == 0 {
		if errno, ok := callErr.(syscall.Errno); ok && errno == syscall.Errno(2) {
			return false
		}
		return true
	}
	procCloseHandleLock.Call(h)
	return true
}

func atomicTempPattern(path string) string {
	base := filepath.Base(path)
	if len(base) > 220 {
		sum := sha256.Sum256([]byte(base))
		return ".aw-" + hex.EncodeToString(sum[:8]) + "-*.tmp"
	}
	return "." + base + ".*.tmp"
}

// writeFileAtomic — запись через временный файл и перенос. Перенос в пределах одного
// тома неделим, и читатель никогда не увидит полузаписанного файла. Применяется к
// вердикту: его читают и страж, и двигатель, и соседние сессии.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, atomicTempPattern(path))
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
	return replaceFileAtomicWindows(tmp, path)
}

// writeFileAtomicDurable is used for recovery intents. The temporary file is
// flushed before its atomic publication, then the published target is flushed
// again. NTFS rename metadata durability across sudden hardware power loss is an
// OS/filesystem guarantee outside Go's testable surface; we record that limit in
// the release receipt rather than claiming a synthetic power-loss proof.
func writeFileAtomicDurable(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, atomicTempPattern(path))
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := replaceFileAtomicWindows(tmp, path); err != nil {
		return err
	}
	published, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("reopen durable intent after publication: %w", err)
	}
	syncErr := published.Sync()
	closeErr := published.Close()
	if syncErr != nil {
		return fmt.Errorf("flush durable intent after publication: %w", syncErr)
	}
	if closeErr != nil {
		return closeErr
	}
	return nil
}

func replaceFileAtomicWindows(tmp, path string) error {
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

// acquireReportLoopGate coordinates only processes using this gate; rc2 and non-Windows processes do not participate.
func acquireReportLoopGate(root string, exclusive bool) (func(), error) {
	if root == "" || len(root) > 32700 || !filepath.IsAbs(root) || filepath.Clean(root) != root || filepath.Dir(root) == root {
		return nil, errors.New("report gate: root must be a bounded absolute product directory")
	}
	volume := filepath.VolumeName(root)
	if !(len(volume) == 2 && volume[1] == ':' || strings.HasPrefix(strings.ToLower(volume), `\\?\volume{`) && strings.HasSuffix(volume, `}`)) {
		return nil, errors.New("report gate: unsupported root volume")
	}
	for _, part := range strings.Split(strings.TrimPrefix(root, volume), `\`) {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.Contains(part, ":") {
			return nil, errors.New("report gate: unsupported path component")
		}
	}
	open := func(path string, directory bool, create bool) (*os.File, syscall.ByHandleFileInformation, error) {
		var info syscall.ByHandleFileInformation
		name, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return nil, info, err
		}
		access, disposition := uint32(0), uint32(syscall.OPEN_EXISTING)
		if !directory {
			access = syscall.GENERIC_READ | syscall.GENERIC_WRITE
		}
		if create {
			disposition = syscall.OPEN_ALWAYS
		}
		h, err := syscall.CreateFile(name, access, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, disposition,
			syscall.FILE_FLAG_BACKUP_SEMANTICS|syscall.FILE_FLAG_OPEN_REPARSE_POINT, 0)
		if err != nil {
			return nil, info, err
		}
		f := os.NewFile(uintptr(h), path)
		err = syscall.GetFileInformationByHandle(h, &info)
		if err == nil && (info.VolumeSerialNumber == 0 || (info.FileIndexHigh == 0 && info.FileIndexLow == 0)) {
			err = errors.New("report gate: unavailable file identity")
		}
		if err == nil && (info.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 || (info.FileAttributes&syscall.FILE_ATTRIBUTE_DIRECTORY != 0) != directory) {
			err = errors.New("report gate: unexpected file type")
		}
		if err == nil && !directory {
			kind, kindErr := syscall.GetFileType(h)
			if kindErr != nil {
				err = kindErr
			} else if kind != syscall.FILE_TYPE_DISK || info.NumberOfLinks != 1 {
				err = errors.New("report gate: unsupported gate file")
			}
		}
		if err != nil {
			_ = f.Close()
			return nil, info, err
		}
		return f, info, nil
	}
	same := func(a, b syscall.ByHandleFileInformation) bool {
		return a.VolumeSerialNumber == b.VolumeSerialNumber && a.FileIndexHigh == b.FileIndexHigh && a.FileIndexLow == b.FileIndexLow
	}
	var pins []*os.File
	var resolvedRelease func()
	var gate *os.File
	var locked bool
	ov := new(syscall.Overlapped)
	var once sync.Once
	release := func() {
		once.Do(func() {
			if gate != nil {
				if locked {
					bootstrapUnlockFile.Call(gate.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(ov)))
				}
				_ = gate.Close()
			}
			if resolvedRelease != nil {
				resolvedRelease()
			}
			for i := len(pins) - 1; i >= 0; i-- {
				_ = pins[i].Close()
			}
		})
	}
	fail := func(err error) (func(), error) { release(); return nil, fmt.Errorf("report gate: %w", err) }
	firstRoot, rootID, err := open(root, true, false)
	if err != nil {
		return fail(err)
	}
	pins = append(pins, firstRoot)
	var fsName [32]uint16
	getVolumeInfo := kernel32Lock.NewProc("GetVolumeInformationByHandleW")
	volumeOK, _, volumeErr := getVolumeInfo.Call(firstRoot.Fd(), 0, 0, 0, 0, 0, uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)))
	if volumeOK == 0 {
		return fail(volumeErr)
	}
	if syscall.UTF16ToString(fsName[:]) != "NTFS" {
		return fail(errors.New("unsupported filesystem"))
	}
	woody := filepath.Join(root, ".woody")
	firstWoody, woodyID, err := open(woody, true, false)
	if err != nil {
		return fail(err)
	}
	pins = append(pins, firstWoody)
	physical, err := openedFileFinalPath(firstRoot)
	if err != nil {
		return fail(err)
	}
	if len(filepath.VolumeName(physical)) != 2 || filepath.VolumeName(physical)[1] != ':' || !filepath.IsAbs(physical) {
		return fail(errors.New("unsupported resolved root"))
	}
	for current := woody; ; current = filepath.Dir(current) {
		f, _, err := open(current, true, false)
		if err != nil {
			return fail(err)
		}
		pins = append(pins, f)
		if filepath.Dir(current) == current {
			break
		}
	}
	resolvedRelease, err = releaseR04HoldSignedScratchUntilChild(filepath.Join(physical, ".woody"))
	if err != nil {
		return fail(err)
	}
	check := func(path string, expected syscall.ByHandleFileInformation, directory bool) error {
		f, info, err := open(path, directory, false)
		if err != nil {
			return err
		}
		defer f.Close()
		if !same(expected, info) {
			return errors.New("path identity changed")
		}
		return nil
	}
	for _, item := range []struct {
		path string
		id   syscall.ByHandleFileInformation
	}{{root, rootID}, {woody, woodyID}, {physical, rootID}, {filepath.Join(physical, ".woody"), woodyID}} {
		if err := check(item.path, item.id, true); err != nil {
			return fail(err)
		}
	}
	gatePath := filepath.Join(woody, ".report-loop.lock")
	var gateID syscall.ByHandleFileInformation
	gate, gateID, err = open(gatePath, false, true)
	if err != nil {
		return fail(err)
	}
	if err := check(gatePath, gateID, false); err != nil {
		return fail(err)
	}
	flags := uintptr(1) // LOCKFILE_FAIL_IMMEDIATELY
	if exclusive {
		flags |= 2
	}
	ok, _, callErr := bootstrapLockFile.Call(gate.Fd(), flags, 0, 1, 0, uintptr(unsafe.Pointer(ov)))
	if ok == 0 {
		return fail(callErr)
	}
	locked = true
	for _, item := range []struct {
		path string
		id   syscall.ByHandleFileInformation
		dir  bool
	}{{root, rootID, true}, {woody, woodyID, true}, {physical, rootID, true}, {filepath.Join(physical, ".woody"), woodyID, true}, {gatePath, gateID, false}, {filepath.Join(physical, ".woody", ".report-loop.lock"), gateID, false}} {
		if err := check(item.path, item.id, item.dir); err != nil {
			return fail(err)
		}
	}
	return release, nil
}
