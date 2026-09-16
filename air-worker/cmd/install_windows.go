//go:build windows

package main

import (
	"context"
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
	procFindWindowExW = user32Install.NewProc("FindWindowExW")
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
// trayWindows — ВСЕ окна значка, а не первое найденное.
//
// Добавлено после перезагрузки 14.09.2026, когда в трее встало три значка разом.
// FindWindowW возвращает одно окно, и остановка, закрывшая его, ждала исчезновения
// «окна значка» — а оставшиеся два отвечали «есть», и ожидание кончалось таймаутом.
// Инструмент снятия обязан справляться именно с тем состоянием, ради которого его зовут.
func trayWindows() []syscall.Handle {
	cls, err := syscall.UTF16PtrFromString(trayClass)
	if err != nil {
		return nil
	}
	var out []syscall.Handle
	var prev uintptr
	for i := 0; i < 64; i++ { // потолок: зацикленный перебор хуже неполного
		h, _, _ := procFindWindowExW.Call(0, prev, uintptr(unsafe.Pointer(cls)), 0)
		if h == 0 {
			break
		}
		out = append(out, syscall.Handle(h))
		prev = h
	}
	return out
}

// stopTray просит ВСЕ значки выйти И ДОЖИДАЕТСЯ, что вышли.
//
// Ожидание добавлено по отказу: первая редакция посылала WM_CLOSE и сразу шла
// копировать файлы, а процесс ещё жил — замена .exe падала с «Access is denied».
// Просьба принята не значит действие совершено: PostMessage возвращает «сообщение
// поставлено в очередь», а не «окно закрылось».
//
// Трогаются ТОЛЬКО окна класса AirWorkerTrayWnd. Имя класса уникально для этого
// продукта; значок AIR Kill Switch живёт в классе SystrayClass и сюда не попадает
// ни при каком раскладе — снятие не должно задевать чужие продукты даже случайно.
func stopTray() (asked bool, gone bool) {
	wins := trayWindows()
	if len(wins) == 0 {
		return false, true
	}
	// Закрываем сообщением, а не убийством процесса: по WM_CLOSE значок снимает себя
	// из трея сам. Убитый процесс оставляет «призрак», который исчезает только когда
	// мышь пройдёт над ним.
	for _, h := range wins {
		procPostMessageWI.Call(uintptr(h), wmCloseMsg, 0, 0)
	}
	for i := 0; i < 50; i++ { // до пяти секунд
		time.Sleep(100 * time.Millisecond)
		if len(trayWindows()) == 0 {
			// Окна исчезли. Процессам нужен ещё момент, чтобы отпустить свои .exe:
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

// binaryVersion — версия установленного файла ПО ИМЕНИ: запускает его `version` и читает
// последнее слово. Пустая строка — файла нет, он не ответил за 10 секунд, или ответил
// ошибкой (нечем понижать, нечем сверять). Значение подставляется в решение об установке
// и в сверку, но само по себе вердикта не выносит.
//
// ПРЕДЕЛ ВРЕМЕНИ — 10 секунд через exec.CommandContext. Без него зависший файл (не тот
// формат, битая копия, чужой процесс с тем же именем) повесил бы установку навсегда:
// вызов ничем не ограничен и ждёт ответа сколько понадобится. Не ответил вовремя —
// пустая версия, тот же исход, что и «не ответил» без таймаута.
func binaryVersion(path string) string {
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").Output()
	if err != nil {
		return ""
	}
	return parseVersionToken(decodeOutput(out))
}

type trayProofRead struct {
	PID      int    `json:"pid"`
	At       string `json:"at"`
	Accepted bool   `json:"shell_accepted"`
	Tooltip  string `json:"tooltip"`
}

// trayProofPath — где значок пишет доказательство приёма. Расчёт тот же, что stateDir()
// у самого значка: писатель и читатель обязаны смотреть в одно место.
func trayProofPath() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:` + string(os.PathSeparator) + `ProgramData`
	}
	return filepath.Join(pd, "AIR OS", "State", "air-worker-tray.json")
}

// readTrayProof — что трей записал о себе сам. nil означает «он ничего не говорил»,
// и это НЕ то же самое, что «сказал, что не смог».
func readTrayProof() *trayProofRead {
	p, _ := trayProofFrom(trayProofPath())
	return p
}

// trayProofFrom — доказательство значка и, если прочесть не вышло, ПОЧЕМУ.
//
// До 0.10 любая неудача давала nil, и статус печатал догадку «старая версия трея?».
// AIR-ENV-002 14.09.2026: файл на месте, без BOM, права в порядке, приём подтверждён, а
// подсказка всё равно про старую версию. Догадка вместо причины стоила двух кругов
// переписки между машинами, и причина так и осталась неизвестной. Теперь печатаются путь и
// то, что с ним на деле; BOM отрезается, как у остальных читателей состояния.
func trayProofFrom(path string) (*trayProofRead, string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "файла нет: " + path
		}
		return nil, fmt.Sprintf("файл не прочитан: %s — %v", path, err)
	}
	text := strings.TrimPrefix(string(raw), string(utf8BOM))
	if strings.TrimSpace(text) == "" {
		return nil, "файл пуст: " + path
	}
	var p trayProofRead
	if err := json.Unmarshal([]byte(text), &p); err != nil {
		return nil, fmt.Sprintf("файл не разобран: %s — %v", path, err)
	}
	return &p, ""
}

func cmdInstall(argv []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	dir := fs.String("dir", "", "куда ставить (умолчание — %LOCALAPPDATA%\\air-worker)")
	auto := fs.Bool("autostart", false, "дополнительно объявить автозапуск при входе пользователя")
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
		routerExe := filepath.Join(home, "tools", "opencode", "opencode.exe")
		if err := verifyOpenCodeBinary(routerExe); err != nil {
			fmt.Printf("OpenCode   : НЕ ГОТОВ — %v"+lineEnding, err)
		} else {
			fmt.Printf("OpenCode   : %s, SHA-256 подтверждён"+lineEnding, openCodeBundleVersion)
		}
		bridge := filepath.Join(home, "tools", "router_stream_bridge.py")
		if st, err := os.Stat(bridge); err == nil && !st.IsDir() {
			fmt.Printf("Router bridge: на месте, %d байт"+lineEnding, st.Size())
		} else {
			fmt.Printf("Router bridge: НЕТ (%s)"+lineEnding, bridge)
		}
		if v := runValue(); v != "" {
			fmt.Printf("Автозапуск : объявлен — %s"+lineEnding, v)
		} else {
			fmt.Print("Автозапуск : НЕ объявлен" + lineEnding)
		}
		if cur, _, _, err := readUserPath(); err != nil {
			fmt.Printf("PATH       : прочитать не удалось — %v"+lineEnding, err)
		} else if pathHas(cur, binDir) {
			fmt.Print("PATH       : каталог в PATH пользователя — продукт зовётся по имени" + lineEnding)
		} else {
			fmt.Print("PATH       : каталога НЕТ в PATH — по имени продукт не найдётся" + lineEnding)
		}
		// ТРИ РАЗНЫХ ФАКТА, а не один. Окно можно завести и не отдать значок
		// оболочке; файл доказательства можно оставить от умершего процесса. Каждое
		// утверждение печатается отдельно, и расхождение между ними видно сразу.
		win := trayWindow() != 0
		proof, proofWhy := trayProofFrom(trayProofPath())
		// НЕСКОЛЬКО ЗНАЧКОВ — ДЕФЕКТ, И ОН НАЗЫВАЕТСЯ ЧИСЛОМ. Три значка после
		// перезагрузки 14.09.2026 выглядели в трее «рабочими», и ни одна строка
		// состояния об этом не говорила: она проверяла «есть ли окно», а не «сколько».
		if n := len(trayWindows()); n > 1 {
			fmt.Printf("Значков    : %d — ДОЛЖЕН БЫТЬ ОДИН, это дефект единственности; снять: air-worker tray -stop"+lineEnding, n)
		}
		switch {
		case win && proof != nil && proof.Accepted:
			fmt.Printf("Значок     : ВИСИТ — оболочка приняла его %s, процесс %d"+lineEnding, proof.At, proof.PID)
			fmt.Printf("Подсказка  : %s"+lineEnding, proof.Tooltip)
		case win && proof != nil && !proof.Accepted:
			fmt.Print("Значок     : окно есть, но ОБОЛОЧКА ОТКАЗАЛА принять значок" + lineEnding)
		case win:
			fmt.Print("Значок     : окно есть, доказательства приёма нет — " + proofWhy + lineEnding)
		case proof != nil:
			fmt.Print("Значок     : окна нет, а файл доказательства остался — процесс умер, не убрав за собой" + lineEnding)
		default:
			fmt.Print("Значок     : не запущен" + lineEnding)
		}
		// ИСТОЧНИК И ШТАТНОСТЬ — отдельными строками. `install -status` называет источник
		// установленного бинарника и источник маркетплейса, зарегистрирован ли плагин и
		// какой версии, и пользовательскую копию скила, заслоняющую плагинный. Каждое
		// нарушение печатается отдельной строкой вместе с недостающей штатной командой.
		self, _ := os.Executable()
		lines, violations := installSourceStatusLinesForHosts(claudeConfigDir(), codexConfigDir(), dstCLI, self)
		for _, ln := range lines {
			fmt.Print(ln + lineEnding)
		}
		if len(violations) == 0 {
			fmt.Print("Штатность  : нарушений источника нет" + lineEnding)
		} else {
			for _, v := range violations {
				fmt.Print(v + lineEnding)
			}
		}
		return 0
	}

	if *remove {
		if err := deleteRunValue(); err != nil {
			fmt.Printf("автозапуск не снят: %v"+lineEnding, err)
			return 1
		}
		fmt.Print("автозапуск снят (если он был объявлен)" + lineEnding)
		switch asked, gone := stopTray(); {
		case !asked:
			fmt.Print("значок и так не запущен" + lineEnding)
		case gone:
			fmt.Print("значок остановлен" + lineEnding)
		default:
			fmt.Print("значок НЕ ОТВЕТИЛ на запрос выхода за пять секунд — сними его вручную" + lineEnding)
		}
		if changed, err := removeFromUserPath(binDir); err != nil {
			fmt.Printf("PATH       : не тронут — %v"+lineEnding, err)
		} else if changed {
			fmt.Print("PATH       : каталог убран из PATH пользователя" + lineEnding)
		} else {
			fmt.Print("PATH       : каталога в PATH и не было" + lineEnding)
		}
		// ФАЙЛЫ НЕ УДАЛЯЮТСЯ. Снять автозапуск — обратимо; стереть каталог — нет, и
		// решение о безвозвратном принимает человек.
		fmt.Printf("файлы оставлены в %s — удали руками, если они больше не нужны"+lineEnding, home)
		return 0
	}

	// УСТАНОВКА ИСКЛЮЧИТЕЛЬНА. Две одновременные останавливают значок, копируют файлы
	// и запускают его вперемешку, и итогом бывает половина: новый CLI со старым значком.
	// Отказ «Access is denied» приходит не всегда — иногда копирование успевает, и
	// расхождение остаётся незамеченным, что хуже честной ошибки.
	lock, ok := acquireLock(`Local\air-worker-install`)
	if !ok {
		fmt.Print("УСТАНОВКА УЖЕ ИДЁТ в другом процессе — эта остановлена, чтобы не оставить половину" + lineEnding)
		fmt.Print("дождись её окончания и проверь: air-worker install -status" + lineEnding)
		return 1
	}
	defer lock.release()

	self, err := os.Executable()
	if err != nil {
		fmt.Printf("не найден собственный путь: %v"+lineEnding, err)
		return 2
	}
	srcDir := filepath.Dir(self)
	srcCLI := filepath.Join(srcDir, "air-worker.exe")
	srcTray := filepath.Join(srcDir, "air-worker-tray.exe")

	// ИСТОЧНИК И ПОНИЖЕНИЕ ВЕРСИИ — до любого действия над машиной. Без повышенных прав
	// установка проходит только из кэша GitHub-маркетплейса air-plugins и только не вниз
	// по версии; с повышенными правами (это ЛПР) разрешена прямая переустановка из любого
	// источника и с понижением, но источник и обе версии при этом печатаются. Само
	// решение — в decideInstallGuard (чистая функция с тестами, installsource.go); здесь
	// подставлены реальные права процесса, версия self и версия уже установленного файла.
	guard := decideInstallGuard(installGuard{
		Elevated:   isElevatedProcess(),
		Self:       self,
		ConfigDir:  claudeConfigDir(),
		CodexDir:   codexConfigDir(),
		SrcVersion: version,
		DstVersion: binaryVersion(dstCLI),
	})
	for _, ln := range guard.Info {
		fmt.Print(ln + lineEnding)
	}
	if !guard.Allow {
		for _, ln := range guard.Reasons {
			fmt.Print(ln + lineEnding)
		}
		return guard.Code
	}

	if _, err := os.Stat(srcTray); err != nil {
		fmt.Printf("рядом нет air-worker-tray.exe (%s): собери его командой"+lineEnding, srcTray)
		fmt.Print("  go build -ldflags \"-H windowsgui\" -o bin/air-worker-tray.exe ./tray" + lineEnding)
		return 2
	}

	routerRuntime, err := stageRouterRuntimePayload(srcDir)
	if err != nil {
		fmt.Printf("Router runtime не прошёл preflight: %v"+lineEnding, err)
		return 2
	}
	defer routerRuntime.cleanup()
	fmt.Printf("Router runtime: OpenCode %s и compatibility bridge проверены до изменения установки"+lineEnding, openCodeBundleVersion)

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

	// СВЕРКА ПОСЛЕ УСТАНОВКИ — не «скопировано, значит установлено». Версия по имени
	// (`air-worker version` установленного файла) и его SHA-256 сверяются с исходным
	// файлом; расхождение — код 2, а не строка при коде 0: недокопированный или
	// подменённый файл выглядит установленным. При установке на месте сверять нечего —
	// источник и назначение это один файл.
	if err := installRouterRuntimePayload(routerRuntime, home); err != nil {
		fmt.Printf("Router runtime не установлен: %v"+lineEnding, err)
		return 2
	}
	fmt.Printf("Router runtime: OpenCode %s + bridge установлены и повторно проверены"+lineEnding, openCodeBundleVersion)

	if !sameDir {
		if got := binaryVersion(dstCLI); got != version {
			fmt.Printf("Сверка     : установленный отвечает версией %q, ожидалась %q — установка не подтверждена"+lineEnding, got, version)
			return 2
		}
		hs, e1 := sha256File(srcCLI)
		hd, e2 := sha256File(dstCLI)
		if e1 != nil || e2 != nil || !strings.EqualFold(hs, hd) {
			fmt.Print("Сверка     : SHA-256 установленного не совпал с исходным — установка не подтверждена" + lineEnding)
			return 2
		}
		fmt.Print("Сверка     : версия по имени и SHA-256 совпали с исходным файлом" + lineEnding)
	}

	// АВТОЗАПУСК ПО УМОЛЧАНИЮ НЕ ОБЪЯВЛЯЕТСЯ, и это исправление моей ошибки.
	//
	// 14.09.2026 я добавила запись в ветвь автозапуска, не получив такого указания. ЛПР
	// сказал прямо: «я такую задачу не ставил, чтобы при входе в систему уже появился;
	// там должен быть ручной запуск либо по команде». Он просил УСТАНОВКУ без повышения
	// и значок, поднятый по его команде, — а не решение о том, когда продукт работает.
	//
	// Разница не формальная. «Куда положить файлы» — вопрос установки, и его закрывает
	// установщик. «Когда продукту запускаться» — вопрос владельца машины, и брать его на
	// себя нельзя: автозапуск переживает выход из сессии и меняет поведение машины
	// навсегда, а спросить об этом дешевле, чем отменить.
	if !*auto {
		fmt.Print("Автозапуск : НЕ объявлен — значок поднимается командой" + lineEnding)
		fmt.Print("             нужен при входе в систему — поставь с флагом -autostart" + lineEnding)
	} else if err := setRunValue("\"" + dstTray + "\""); err != nil {
		fmt.Printf("Автозапуск : НЕ объявлен — %v"+lineEnding, err)
		return 1
	} else {
		fmt.Print("Автозапуск : объявлен в ветви пользователя, повышение не потребовалось" + lineEnding)
		fmt.Print("             сработает ПРИ ВХОДЕ пользователя, не при загрузке машины:" + lineEnding)
		fmt.Print("             значок живёт на рабочем столе, а стола до входа не существует" + lineEnding)
	}

	// PATH — чтобы ЧУЖИЕ продукты могли сослаться на судью и двигатель по имени, а не
	// абсолютным путём в чей-то клон и не версионным адресом плагина. Это ответ на
	// вопрос AIR-ENV-002 от 14.09.2026: у продукта, который не везёт бинарник внутри,
	// стабильный адрес появляется установкой, а не псевдонимом, заведённым руками.
	if changed, err := addToUserPath(binDir); err != nil {
		fmt.Printf("PATH       : НЕ дописан — %v (продукт придётся звать полным путём)"+lineEnding, err)
	} else if changed {
		fmt.Print("PATH       : каталог дописан в PATH пользователя (повышение не потребовалось)" + lineEnding)
		fmt.Print("             новые процессы увидят его сразу, уже запущенные — нет: своё окружение они держат копией" + lineEnding)
	} else {
		fmt.Print("PATH       : каталог уже в PATH, второй копии не заведено" + lineEnding)
	}

	if *noStart {
		fmt.Print("Значок     : не запускался (по флагу -no-start)" + lineEnding)
		return 0
	}
	// Отказ владельца машины действует и здесь: установка ставит файлы, но не решает,
	// чему на машине работать.
	if trayDisabled() {
		fmt.Print("Значок     : не запускался — выключен переменной AIR_WORKER_NO_TRAY" + lineEnding)
		return 0
	}
	// ЗНАЧОК ОТЦЕПЛЯЕТСЯ ОТ РОДИТЕЛЯ, и это не украшение.
	//
	// Замер AIR-ENV-002 от 14.09.2026: `Start-Process ... install -Wait` не возвращался
	// НИКОГДА — висел десять минут, при том что вся работа была сделана за секунды.
	// PowerShell ждёт не процесс, а дерево процессов, а значок живёт вечно по своей
	// природе. Снаружи это неотличимо от зависшей установки — тот же класс, что
	// зависшие установщики winget: успех выглядит как отказ.
	//
	// DETACHED_PROCESS уводит значок из консоли родителя, CREATE_BREAKAWAY_FROM_JOB
	// выводит его из объекта задания, которым PowerShell держит дерево. Второй флаг
	// разрешён не всегда, и отказ по нему — не повод не запустить значок: пробуем с
	// ним, при отказе повторяем без. Молча ронять установку из-за флага нельзя.
	const (
		createNewProcessGroup  = 0x00000200
		detachedProcess        = 0x00000008
		createBreakawayFromJob = 0x01000000
	)
	start := func(flags uint32) error {
		c := exec.Command(dstTray)
		c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: flags}
		if err := c.Start(); err != nil {
			return err
		}
		_ = c.Process.Release()
		return nil
	}
	if err := start(createNewProcessGroup | detachedProcess | createBreakawayFromJob); err == nil {
		fmt.Print("Значок     : запущен (отцеплён от вызвавшего процесса)" + lineEnding)
		fmt.Print("Проверь замером: air-worker install -status" + lineEnding)
		return 0
	}
	cmd := exec.Command(dstTray)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNewProcessGroup | detachedProcess}
	if err := cmd.Start(); err != nil {
		fmt.Printf("Значок     : не запустился — %v"+lineEnding, err)
		return 1
	}
	_ = cmd.Process.Release()
	fmt.Print("Значок     : запущен" + lineEnding)
	fmt.Print("Проверь замером: air-worker install -status" + lineEnding)
	return 0
}
