//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// A separate process keeps the destination handle open throughout the rename.
func TestAtomicReaderProcess(t *testing.T) {
	path := os.Getenv("AIR_WORKER_ATOMIC_READER_PATH")
	if path == "" {
		return
	}
	name, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		t.Fatal(err)
	}
	share := uint32(syscall.FILE_SHARE_READ | syscall.FILE_SHARE_WRITE)
	if os.Getenv("AIR_WORKER_ATOMIC_SHARE_DELETE") == "1" {
		share |= syscall.FILE_SHARE_DELETE
	}
	h, err := syscall.CreateFile(name, syscall.GENERIC_READ, share, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(h)
	fmt.Println("READY")
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestWriteFileAtomicWithExternalReader(t *testing.T) {
	oldTimeout, oldDelay := writeFileAtomicRetryTimeout, writeFileAtomicRetryDelay
	writeFileAtomicRetryTimeout, writeFileAtomicRetryDelay = 60*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		writeFileAtomicRetryTimeout, writeFileAtomicRetryDelay = oldTimeout, oldDelay
	})
	for _, tc := range []struct {
		name, shareDelete string
		wantSuccess       bool
	}{
		{"share-delete", "1", true},
		{"no-share-delete", "0", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "PLAN.md")
			if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
			child := exec.Command(os.Args[0], "-test.run=^TestAtomicReaderProcess$")
			child.Env = append(os.Environ(), "AIR_WORKER_ATOMIC_READER_PATH="+path, "AIR_WORKER_ATOMIC_SHARE_DELETE="+tc.shareDelete)
			stdin, err := child.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := child.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := child.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = stdin.Close()
				_ = child.Wait()
			})
			scanner := bufio.NewScanner(stdout)
			if !scanner.Scan() || scanner.Text() != "READY" {
				t.Fatalf("reader did not open PLAN.md: %q, %v", scanner.Text(), scanner.Err())
			}
			err = writeFileAtomic(path, []byte("new"))
			if (err == nil) != tc.wantSuccess {
				t.Fatalf("replace success=%v, want %v; err=%v", err == nil, tc.wantSuccess, err)
			}
			if !tc.wantSuccess && (!strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "FILE_SHARE_DELETE")) {
				t.Fatalf("failure does not explain blocked replacement: %v", err)
			}
			want := "old"
			if tc.wantSuccess {
				want = "new"
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != want {
				t.Fatalf("PLAN.md=%q, err=%v; want %q", got, readErr, want)
			}
			entries, readErr := os.ReadDir(filepath.Dir(path))
			if readErr != nil || len(entries) != 1 {
				t.Fatalf("atomic write left litter: %v, %v", entries, readErr)
			}
		})
	}
}

func TestWriteFileAtomicRepeatedReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "PLAN.md")
	for i := 0; i < 100; i++ {
		want := fmt.Sprintf("revision %d", i)
		if err := writeFileAtomic(path, []byte(want)); err != nil {
			t.Fatalf("revision %d: %v", i, err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != want {
			t.Fatalf("revision %d: got %q, err=%v", i, got, err)
		}
	}
}

func TestAtomicWritesLongReceiptName(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(string, []byte) error
	}{
		{"atomic", writeFileAtomic},
		{"durable", writeFileAtomicDurable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			name := strings.Repeat("n", 230) + ".receipt.json"
			if len(name) != 243 {
				t.Fatalf("filename length = %d, want 243", len(name))
			}
			path := filepath.Join(dir, name)
			if err := os.WriteFile(path, []byte("preflight"), 0o600); err != nil {
				t.Fatalf("NOT_PROVEN: filesystem cannot create the destination: %v", err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			defer func() {
				entries, err := os.ReadDir(dir)
				if err != nil {
					t.Error(err)
				} else if len(entries) != 1 || entries[0].Name() != name {
					t.Errorf("unexpected directory contents: %v", entries)
				}
			}()
			for _, want := range []string{"first receipt\x00", "replacement receipt\n"} {
				if err := tc.write(path, []byte(want)); err != nil {
					t.Fatalf("write: %v", err)
				}
				got, err := os.ReadFile(path)
				if err != nil || string(got) != want {
					t.Fatalf("read = %q, err = %v; want %q", got, err, want)
				}
			}
		})
	}
}

// ЭТИ ПРОВЕРКИ ЗОВУТ ЗАМОК, А НЕ СМОТРЯТ НА НЕГО.
//
// Написаны после отказа 14.09.2026: `CreateMutexW` была взята из advapi32, где её нет
// (она в kernel32), и продукт УПАЛ ПАНИКОЙ на первом же вызове у человека на машине.
// Сборка молчала, `go vet` молчал — ленивая загрузка проверяет наличие процедуры только
// в момент обращения.
//
// Урок тот же, что механизм ловит у чужих продуктов: РЕЗОЛВ НЕ ДОКАЗЫВАЕТ НАЛИЧИЯ,
// ДОКАЗЫВАЕТ ТОЛЬКО ОТВЕТ. Ни одна проверка формы этого поймать не могла — только вызов.

func TestLockAcquireAndRelease(t *testing.T) {
	name := `Local\air-worker-test-` + strings.ReplaceAll(t.Name(), "/", "-")

	// Сам вызов и есть предмет проверки: при неверной библиотеке здесь паника.
	l, ok := acquireLock(name)
	if !ok {
		t.Fatal("замок не взялся с первого раза, хотя его никто не держит")
	}
	if !lockHeld(name) {
		t.Error("замок взят, но lockHeld его не видит — снаружи он невидим")
	}

	// ВТОРОЙ ЗАХВАТ ОБЯЗАН ОТКАЗАТЬ, иначе замок не замок. Проверяется из того же
	// процесса намеренно: ОС отвечает «уже существует» независимо от того, кто владелец,
	// и это ровно то поведение, на которое опирается защита от второй петли.
	if _, ok2 := acquireLock(name); ok2 {
		t.Error("замок взялся ДВАЖДЫ: защита от одновременной работы не работает")
	}

	l.release()
	if lockHeld(name) {
		t.Error("после release замок всё ещё держится")
	}
	// После снятия он обязан браться снова — иначе один прогон запирал бы продукт
	// навсегда до перезагрузки.
	l2, ok3 := acquireLock(name)
	if !ok3 {
		t.Fatal("после release замок не берётся заново")
	}
	l2.release()
}

// Имя замка строится из пути продукта, и от этого зависит, мешают ли друг другу две
// петли. Ошибка здесь не видна ничем: либо продукты запирают друг друга, либо один
// продукт не запирается вовсе.
func TestLockNameIsPerProduct(t *testing.T) {
	a := lockName("loop", `F:\-7-\air-worker`)
	b := lockName("loop", `E:\-8-\asw`)
	if a == b {
		t.Fatal("разные продукты дали одно имя замка — они запрут друг друга")
	}

	// Тот же продукт, записанный иначе, — ТОТ ЖЕ замок. Иначе две петли по одному
	// дереву разойдутся по регистру буквы диска или по хвостовой косой черте.
	same := []string{`F:\-7-\air-worker`, `f:\-7-\air-worker`, `F:\-7-\air-worker\`, `F:\-7-\.\air-worker`}
	for _, v := range same {
		if got := lockName("loop", v); got != a {
			t.Errorf("путь %q дал другое имя замка (%s вместо %s): одно дерево не запрётся", v, got, a)
		}
	}

	// Обратная косая черта в имени объекта ядра запрещена — она делит пространство имён.
	// Имя, собранное из сырого пути, молча создавало бы объект не там.
	if strings.Count(a, `\`) != 1 {
		t.Errorf("в имени замка лишние разделители пространства имён: %q", a)
	}

	// Разные роды замков на одном продукте не должны совпадать: установка и петля —
	// разные вещи, и общий замок остановил бы одну из-за другой.
	if lockName("loop", `F:\-7-\air-worker`) == lockName("install", `F:\-7-\air-worker`) {
		t.Error("замки разного рода совпали на одном продукте")
	}
}

// Вердикт читают из разных процессов. Неатомарная запись даёт усечённый файл, и
// «вердикта нет» становится неотличимо от «вердикт пуст».
func TestWriteFileAtomicLeavesNoLitter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verdict.json")
	if err := writeFileAtomic(path, []byte(`{"code":0}`)); err != nil {
		t.Fatalf("запись не удалась: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл не читается: %v", err)
	}
	if string(got) != `{"code":0}` {
		t.Errorf("содержимое искажено: %q", got)
	}

	// Перезапись обязана работать: вердикт пишется каждым прогоном судьи.
	if err := writeFileAtomic(path, []byte(`{"code":1}`)); err != nil {
		t.Fatalf("перезапись не удалась: %v", err)
	}

	// Временных файлов остаться не должно: мусор рядом с вердиктом попадёт в счёт
	// изменённых файлов дерева и сместит замер.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("после записи остался временный файл %s", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("в каталоге %d файлов вместо одного", len(entries))
	}
}

func TestWriteFileAtomicRetriesRenameWhileReaderHoldsTarget(t *testing.T) {
	originalTimeout := writeFileAtomicRetryTimeout
	originalDelay := writeFileAtomicRetryDelay
	t.Cleanup(func() {
		writeFileAtomicRetryTimeout = originalTimeout
		writeFileAtomicRetryDelay = originalDelay
	})

	dir := t.TempDir()
	path := filepath.Join(dir, "verdict.json")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = reader.Close()
		close(closed)
	}()

	if err := writeFileAtomic(path, []byte("new")); err != nil {
		t.Fatalf("повтор замены не дождался освобождения файла: %v", err)
	}
	<-closed
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("содержимое не заменилось: %q", got)
	}

	writeFileAtomicRetryTimeout = 50 * time.Millisecond
	writeFileAtomicRetryDelay = 5 * time.Millisecond
	if err := os.WriteFile(path, []byte("old-again"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader, err = os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("never")); err == nil {
		t.Fatal("ожидалась ошибка после исчерпания времени повтора")
	}
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatalf("после ошибки остался временный файл %s", entry.Name())
		}
	}
}

// TestReportLoopGateChild holds the gate until the parent closes stdin.
func TestReportLoopGateChild(t *testing.T) {
	root := os.Getenv("AIR_WORKER_GATE_TEST_ROOT")
	if root == "" {
		return
	}
	release, err := acquireReportLoopGate(root, os.Getenv("AIR_WORKER_GATE_TEST_EXCLUSIVE") == "1")
	if err != nil {
		fmt.Println("DENIED")
	} else {
		defer release()
		fmt.Println("ACQUIRED")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

func TestReportLoopGateProcesses(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "parent")
	root := filepath.Join(parent, "product")
	other := filepath.Join(filepath.Dir(root), "other")
	for _, dir := range []string{root, other} {
		if err := os.MkdirAll(filepath.Join(dir, ".woody"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	start := func(path string, exclusive, want bool) func() {
		t.Helper()
		mode := "0"
		if exclusive {
			mode = "1"
		}
		child := exec.Command(os.Args[0], "-test.run=^TestReportLoopGateChild$")
		child.Env = append(os.Environ(), "AIR_WORKER_GATE_TEST_ROOT="+path, "AIR_WORKER_GATE_TEST_EXCLUSIVE="+mode)
		stdin, err := child.StdinPipe()
		if err != nil {
			t.Fatal(err)
		}
		stdout, err := child.StdoutPipe()
		if err != nil {
			t.Fatal(err)
		}
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- child.Wait() }()
		stopped := false
		stop := func() {
			if stopped {
				return
			}
			stopped = true
			_ = stdin.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("gate child: %v", err)
				}
			case <-time.After(5 * time.Second):
				_ = child.Process.Kill()
				<-done
				t.Error("gate child did not exit")
			}
		}
		t.Cleanup(stop)
		response := make(chan string, 1)
		go func() {
			scanner := bufio.NewScanner(stdout)
			if scanner.Scan() {
				response <- scanner.Text()
			} else {
				response <- ""
			}
		}()
		select {
		case got := <-response:
			expected := "DENIED"
			if want {
				expected = "ACQUIRED"
			}
			if got != expected {
				t.Fatalf("gate %q exclusive=%v: got %q, want %q", path, exclusive, got, expected)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("gate child did not respond")
		}
		return stop
	}

	shared1 := start(root, false, true)
	shared2 := start(root, false, true)
	denied := start(root, true, false)
	independent := start(other, true, true)
	independent()
	if err := os.Rename(root, root+"-moved"); err == nil {
		t.Fatal("product directory was renamed while gate was held")
	}
	if err := os.Rename(parent, parent+"-moved"); err == nil {
		t.Fatal("product parent was renamed while gate was held")
	}
	if err := os.Rename(filepath.Join(root, ".woody"), filepath.Join(root, ".woody-moved")); err == nil {
		t.Fatal("gate directory was renamed while gate was held")
	}
	shared1()
	start(root, true, false)()
	shared2()
	// The denied child is still alive: its failed acquisition must not pin root.
	if err := os.Rename(root, root+"-moved"); err != nil {
		t.Fatalf("failed acquisition left handles behind: %v", err)
	}
	if err := os.Rename(root+"-moved", root); err != nil {
		t.Fatal(err)
	}
	denied()
	exclusive := start(root, true, true)
	start(root, false, false)()
	exclusive()
	start(root, false, true)()

	t.Run("volume GUID alias", func(t *testing.T) {
		dir, err := os.Open(root)
		if err != nil {
			t.Fatal(err)
		}
		buf := make([]uint16, 32768)
		const volumeNameGUID = 0x1
		proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetFinalPathNameByHandleW")
		n, _, _ := proc.Call(dir.Fd(), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), volumeNameGUID)
		_ = dir.Close()
		if n == 0 || int(n) >= len(buf) {
			t.Skip("volume GUID alias is unavailable on this filesystem")
		}
		guidRoot := syscall.UTF16ToString(buf[:n])
		if !strings.HasPrefix(guidRoot, `\\?\Volume{`) {
			t.Skipf("volume GUID alias is unavailable: %q", guidRoot)
		}
		dosInfo, err := os.Stat(root)
		if err != nil {
			t.Fatal(err)
		}
		guidInfo, err := os.Stat(guidRoot)
		if err != nil {
			t.Skipf("volume GUID alias cannot be opened: %v", err)
		}
		if !os.SameFile(dosInfo, guidInfo) {
			t.Fatal("volume GUID alias does not identify the product root")
		}
		aliasHolder := start(root, false, true)
		start(guidRoot, true, false)()
		aliasHolder()
		aliasExclusive := start(guidRoot, true, true)
		if err := os.Rename(parent, parent+"-moved"); err == nil {
			t.Fatal("product parent was renamed while GUID gate was held")
		}
		start(root, false, false)()
		aliasExclusive()
	})
}
