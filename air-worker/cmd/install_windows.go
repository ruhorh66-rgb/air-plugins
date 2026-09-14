//go:build windows

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// УСТАНОВКА БЕЗ ПОВЫШЕНИЯ ПРАВ — ТРЕБОВАНИЕ ЛПР, А НЕ УДОБСТВО.
//
// Дословно 14.09.2026: «я хочу, чтобы он устанавливался в систему без повышения, чтобы
// по моему гейту вы самостоятельно могли его установить».
//
// Отсюда три решения, и каждое проверяется, а не обещается:
//
//  1. КУДА. %LOCALAPPDATA%\air-worker — пользовательская область. Program Files
//     потребовал бы администратора, и установка встала бы на запрос UAC, которого в
//     сессии некому нажать. Путь перекрывается переменной AIR_WORKER_HOME: решение о
//     томе принимает человек, механизм не выбирает за него.
//
//  2. ЧЕМ АВТОЗАПУСК. HKCU\...\CurrentVersion\Run — ветвь ТЕКУЩЕГО пользователя.
//     HKLM и «Запланированные задания» с правами «для всех» требуют повышения;
//     ярлык в папке «Автозагрузка» его не требует, но живёт файлом, который легко
//     потерять при переносе профиля, и его отсутствие ничем не заметно.
//
//  3. ЧЕМ ПИСАТЬ В РЕЕСТР. Тем же API ОС, что читает токен в userenv_windows.go, а не
//     вызовом reg.exe. Подпроцесс протащил бы путь через командную строку и вернул бы
//     результат текстом, который надо разбирать; API возвращает код.
//
// ПРОВЕРКА УСТАНОВКИ — ОТДЕЛЬНАЯ КОМАНДА И ОНА ЧЕСТНАЯ. `install -status` отвечает не
// «установлено», а тремя РАЗНЫМИ фактами: файлы на месте, автозапуск объявлен, значок
// живёт. Свести их в одно «да» значило бы повторить ошибку, за которую судье завели
// третий код: «не проверено» и «не прошло» — разные ответы.

var (
	advapi32Install     = syscall.NewLazyDLL("advapi32.dll")
	procRegCreateKeyExW = advapi32Install.NewProc("RegCreateKeyExW")
	procRegSetValueExW  = advapi32Install.NewProc("RegSetValueExW")
	procRegDeleteValueW = advapi32Install.NewProc("RegDeleteValueW")

	user32Install     = syscall.NewLazyDLL("user32.dll")
	procFindWindowW   = user32Install.NewProc("FindWindowW")
	procPostMessageWI = user32Install.NewProc("PostMessageW")
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "AIR Worker Tray"
	trayClass    = "AirWorkerTrayWnd"
	keyWrite     = 0x20006
	wmCloseMsg   = 0x0010
)

// installHome — куда ставим. Объявляется переменной окружения либо берётся умолчание
// в профиле пользователя: профиль входит в периметр резервного копирования контура.
func installHome() string {
	if v := strings.TrimSpace(os.Getenv("AIR_WORKER_HOME")); v != "" {
		return v
	}
	if v := strings.TrimSpace(userEnvVar("AIR_WORKER_HOME")); v != "" {
		return v
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		base = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Local")
	}
	return filepath.Join(base, "air-worker")
}

func setRunValue(value string) error {
	sub, err := syscall.UTF16PtrFromString(runKeyPath)
	if err != nil {
		return err
	}
	var h syscall.Handle
	var disp uint32
	r, _, _ := procRegCreateKeyExW.Call(uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
		0, 0, 0, uintptr(keyWrite), 0, uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&disp)))
	if r != 0 {
		return fmt.Errorf("ветвь автозапуска не открылась, код %d", r)
	}
	defer procRegCloseKey.Call(uintptr(h))

	name, err := syscall.UTF16PtrFromString(runValueName)
	if err != nil {
		return err
	}
	data, err := syscall.UTF16FromString(value)
	if err != nil {
		return err
	}
	r, _, _ = procRegSetValueExW.Call(uintptr(h), uintptr(unsafe.Pointer(name)), 0, regSZ,
		uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)*2))
	if r != 0 {
		return fmt.Errorf("значение автозапуска не записано, код %d", r)
	}
	return nil
}

func deleteRunValue() error {
	sub, err := syscall.UTF16PtrFromString(runKeyPath)
	if err != nil {
		return err
	}
	var h syscall.Handle
	var disp uint32
	r, _, _ := procRegCreateKeyExW.Call(uintptr(hkeyCurrentUser), uintptr(unsafe.Pointer(sub)),
		0, 0, 0, uintptr(keyWrite), 0, uintptr(unsafe.Pointer(&h)), uintptr(unsafe.Pointer(&disp)))
	if r != 0 {
		return fmt.Errorf("ветвь автозапуска не открылась, код %d", r)
	}
	defer procRegCloseKey.Call(uintptr(h))
	name, err := syscall.UTF16PtrFromString(runValueName)
	if err != nil {
		return err
	}
	// Код 2 — «значения нет». Это не отказ: снять то, чего нет, и есть нужный итог.
	if r, _, _ = procRegDeleteValueW.Call(uintptr(h), uintptr(unsafe.Pointer(name))); r != 0 && r != 2 {
		return fmt.Errorf("значение автозапуска не снято, код %d", r)
	}
	return nil
}

func runValue() string {
	return userEnvVarIn(runKeyPath, runValueName)
}

// trayWindow — дескриптор окна значка либо 0. Это ЕДИНСТВЕННОЕ доказательство, что
// значок живёт: наличие процесса доказывает лишь, что что-то запустилось.
func trayWindow() syscall.Handle {
	cls, err := syscall.UTF16PtrFromString(trayClass)
	if err != nil {
		return 0
	}
	h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
	return syscall.Handle(h)
}

// stopTray просит значок выйти И ДОЖИДАЕТСЯ, что он вышел.
//
// Ожидание добавлено не из осторожности, а по отказу: первая редакция посылала
// WM_CLOSE и сразу шла копировать файлы, а процесс ещё жил — замена его .exe падала с
// «Access is denied», и установка оставляла половину (CLI новый, значок старый).
// Поймано первым же прогоном установки 14.09.2026.
//
// Это тот же класс, что механизм ловит у продуктов: ПРОСЬБА ПРИНЯТА НЕ ЗНАЧИТ
// ДЕЙСТВИЕ СОВЕРШЕНО. PostMessage возвращает «сообщение поставлено в очередь», а не
// «окно закрылось», и разница здесь стоила установки.
//
// Второе возвращаемое значение отличает «вышел» от «не дождались»: ждать вечно нельзя
// (зависший значок остановил бы установку навсегда), а молча продолжить — значит
// вернуться к тому же отказу с другой стороны.
func stopTray() (asked bool, gone bool) {
	h := trayWindow()
	if h == 0 {
		return false, true
	}
	// Закрываем сообщением, а не убийством процесса: по WM_CLOSE трей снимает свой
	// значок сам. Убитый процесс оставляет в трее «призрак», который исчезает только
	// когда мышь пройдёт над ним.
	procPostMessageWI.Call(uintptr(h), wmCloseMsg, 0, 0)
	for i := 0; i < 50; i++ { // до пяти секунд
		time.Sleep(100 * time.Millisecond)
		if trayWindow() == 0 {
			// Окно исчезло. Процессу нужен ещё момент, чтобы отпустить свой .exe:
			// дескриптор образа закрывает ОС уже после выхода из main.
			time.Sleep(300 * time.Millisecond)
			return true, true
		}
	}
	return true, false
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	// Пишем во временный файл и переименовываем: замена работающего .exe иначе
	// упирается в «файл занят», и установка оставила бы половину.
	tmp := dst + ".new"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	out.Close()
	_ = os.Remove(dst)
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return nil
}

type trayProofRead struct {
	PID      int    `json:"pid"`
	At       string `json:"at"`
	Accepted bool   `json:"shell_accepted"`
	Tooltip  string `json:"tooltip"`
}

// readTrayProof — что трей записал о себе сам. nil означает «он ничего не говорил»,
// и это НЕ то же самое, что «сказал, что не смог».
func readTrayProof() *trayProofRead {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:` + string(os.PathSeparator) + `ProgramData`
	}
	raw, err := os.ReadFile(filepath.Join(pd, "AIR OS", "State", "air-worker-tray.json"))
	if err != nil {
		return nil
	}
	var p trayProofRead
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	return &p
}

func cmdInstall(argv []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	dir := fs.String("dir", "", "куда ставить (умолчание — %LOCALAPPDATA%\\air-worker)")
	noAuto := fs.Bool("no-autostart", false, "не объявлять автозапуск")
	noStart := fs.Bool("no-start", false, "не запускать значок сейчас")
	status := fs.Bool("status", false, "только показать состояние установки")
	remove := fs.Bool("uninstall", false, "снять автозапуск и остановить значок")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	home := *dir
	if home == "" {
		home = installHome()
	}
	binDir := filepath.Join(home, "bin")
	dstCLI := filepath.Join(binDir, "air-worker.exe")
	dstTray := filepath.Join(binDir, "air-worker-tray.exe")

	if *status {
		fmt.Printf("Каталог    : %s"+lineEnding, home)
		for _, f := range []struct{ name, path string }{{"CLI", dstCLI}, {"значок", dstTray}} {
			if st, err := os.Stat(f.path); err == nil {
				fmt.Printf("%-10s : на месте, %d КБ"+lineEnding, f.name, st.Size()/1024)
			} else {
				fmt.Printf("%-10s : НЕТ (%s)"+lineEnding, f.name, f.path)
			}
		}
		if v := runValue(); v != "" {
			fmt.Printf("Автозапуск : объявлен — %s"+lineEnding, v)
		} else {
			fmt.Print("Автозапуск : НЕ объявлен" + lineEnding)
		}
		// ТРИ РАЗНЫХ ФАКТА, а не один. Окно можно завести и не отдать значок
		// оболочке; файл доказательства можно оставить от умершего процесса. Каждое
		// утверждение печатается отдельно, и расхождение между ними видно сразу.
		win := trayWindow() != 0
		proof := readTrayProof()
		switch {
		case win && proof != nil && proof.Accepted:
			fmt.Printf("Значок     : ВИСИТ — оболочка приняла его %s, процесс %d"+lineEnding, proof.At, proof.PID)
			fmt.Printf("Подсказка  : %s"+lineEnding, proof.Tooltip)
		case win && proof != nil && !proof.Accepted:
			fmt.Print("Значок     : окно есть, но ОБОЛОЧКА ОТКАЗАЛА принять значок" + lineEnding)
		case win:
			fmt.Print("Значок     : окно есть, доказательства приёма нет (старая версия трея?)" + lineEnding)
		case proof != nil:
			fmt.Print("Значок     : окна нет, а файл доказательства остался — процесс умер, не убрав за собой" + lineEnding)
		default:
			fmt.Print("Значок     : не запущен" + lineEnding)
		}
		return 0
	}

	if *remove {
		if err := deleteRunValue(); err != nil {
			fmt.Printf("автозапуск не снят: %v"+lineEnding, err)
			return 1
		}
		fmt.Print("автозапуск снят" + lineEnding)
		switch asked, gone := stopTray(); {
		case !asked:
			fmt.Print("значок и так не запущен" + lineEnding)
		case gone:
			fmt.Print("значок остановлен" + lineEnding)
		default:
			fmt.Print("значок НЕ ОТВЕТИЛ на запрос выхода за пять секунд — сними его вручную" + lineEnding)
		}
		// ФАЙЛЫ НЕ УДАЛЯЮТСЯ. Снять автозапуск — обратимо; стереть каталог — нет, и
		// решение о безвозвратном принимает человек.
		fmt.Printf("файлы оставлены в %s — удали руками, если они больше не нужны"+lineEnding, home)
		return 0
	}

	self, err := os.Executable()
	if err != nil {
		fmt.Printf("не найден собственный путь: %v"+lineEnding, err)
		return 2
	}
	srcDir := filepath.Dir(self)
	srcCLI := filepath.Join(srcDir, "air-worker.exe")
	srcTray := filepath.Join(srcDir, "air-worker-tray.exe")
	if _, err := os.Stat(srcTray); err != nil {
		fmt.Printf("рядом нет air-worker-tray.exe (%s): собери его командой"+lineEnding, srcTray)
		fmt.Print("  go build -ldflags \"-H windowsgui\" -o bin/air-worker-tray.exe ./tray" + lineEnding)
		return 2
	}

	// Значок останавливается ДО копирования: работающий .exe заменить нельзя.
	// Не дождались — ОТКАЗ, а не «попробуем всё равно»: копирование поверх живого
	// процесса и есть тот отказ, ради которого здесь появилось ожидание.
	if asked, gone := stopTray(); asked {
		if !gone {
			fmt.Print("прежний значок не вышел за пять секунд: установка остановлена, чтобы не оставить половину" + lineEnding)
			fmt.Print("сними его из меню значка («Выход») и повтори" + lineEnding)
			return 1
		}
		fmt.Print("прежний значок остановлен" + lineEnding)
	}

	sameDir := strings.EqualFold(filepath.Clean(srcDir), filepath.Clean(binDir))
	if sameDir {
		fmt.Printf("Каталог    : %s (ставим на месте, копировать нечего)"+lineEnding, home)
	} else {
		for _, f := range [][2]string{{srcCLI, dstCLI}, {srcTray, dstTray}} {
			if err := copyFile(f[0], f[1]); err != nil {
				fmt.Printf("не скопирован %s: %v"+lineEnding, filepath.Base(f[0]), err)
				return 1
			}
		}
		fmt.Printf("Каталог    : %s"+lineEnding, home)
		fmt.Print("Файлы      : CLI и значок скопированы" + lineEnding)
	}

	if *noAuto {
		fmt.Print("Автозапуск : не объявлен (по флагу -no-autostart)" + lineEnding)
	} else if err := setRunValue("\"" + dstTray + "\""); err != nil {
		fmt.Printf("Автозапуск : НЕ объявлен — %v"+lineEnding, err)
		return 1
	} else {
		fmt.Print("Автозапуск : объявлен в ветви пользователя, повышение не потребовалось" + lineEnding)
	}

	if *noStart {
		fmt.Print("Значок     : не запускался (по флагу -no-start)" + lineEnding)
		return 0
	}
	cmd := exec.Command(dstTray)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		fmt.Printf("Значок     : не запустился — %v"+lineEnding, err)
		return 1
	}
	_ = cmd.Process.Release()
	fmt.Print("Значок     : запущен" + lineEnding)
	fmt.Print("Проверь замером: air-worker install -status" + lineEnding)
	return 0
}
