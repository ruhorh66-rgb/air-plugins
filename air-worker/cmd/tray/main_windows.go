//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ТРЕЙ AIR-WORKER: механизм сообщает о себе без слов.
//
// Указание ЛПР 14.09.2026: «хочу, чтобы уже в трее висел наш air-worker, чтобы он уже
// работал, чтобы там какой-нибудь значок уже висел, да, что он работает».
//
// ЧТО ИМЕННО ПОКАЗЫВАЕТ ЗНАЧОК, и почему не «работает/не работает». Двоичное «жив»
// ничего не стоит: процесс, который жив и ничего не мерит, выглядит так же, как
// работающий. Значок показывает СОСТОЯНИЕ ОБЪЯВЛЕННЫХ ПРОДУКТОВ — то же расстояние до
// цели, которым меряет себя весь механизм:
//
//	серое кольцо  — продуктов не объявлено, мерить нечего (это НЕ «всё хорошо»);
//	жёлтый круг   — работа идёт, расстояние не ноль;
//	красный с вырезом — застой, эскалация или «ждёт ЛПР»: без человека не сдвинется;
//	зелёный круг  — расстояние ноль у всех объявленных продуктов.
//
// ОТКУДА БЕРУТСЯ ЧИСЛА. Трей НЕ ПОВТОРЯЕТ логику замера: он зовёт тот же
// `air-worker drift -product X -json`, что и страж хода. Второй реализации одного
// правила не заводится — две реализации расходятся молча, и это единственный урок,
// который за прошлые сутки подтвердился семь раз.
//
// ЧЕГО ТРЕЙ НЕ ДЕЛАЕТ. Он не зовёт судью: замер 13.09.2026 — судья ASW идёт 28 секунд,
// drift 0,08 с. Значок, раз в минуту занимающий полминуты процессора, снимут вместе с
// пользой. Поэтому расстояние он показывает по последнему записанному вердикту, и это
// названо в подсказке словами «вердикт от <время>».

const (
	appName   = "air-worker"
	className = "AirWorkerTrayWnd"
	trayID    = 1

	idRefresh           = 1000
	idState             = 1001
	idQuit              = 1002
	idCheckUpdate       = 1003
	idInstallUpdate     = 1004
	idUpdateLog         = 1005
	idChannelStable     = 1006
	idChannelPrerelease = 1007
	idProduct           = 2000

	refreshEvery = 60 * time.Second
)

var (
	hwnd     syscall.Handle
	icons    map[iconShape]syscall.Handle
	curIcon  syscall.Handle
	wmTaskba uint32

	stateMu      sync.Mutex
	products     []productState
	workerVer    string
	updateStatus trayUpdateState
	lastErr      string
)

type productState struct {
	Path           string
	Name           string
	Session        string
	Principal      string
	Distance       *int
	Verdict        string
	Stall          int
	JudgeAt        string
	Outcome        string
	CurrentStep    string
	NextAction     string
	StopReason     string
	ProgressClosed int
	ProgressTotal  int
	Worker         string
	Err            string
}

type trayUpdateState struct {
	CurrentVersion  string `json:"current_version"`
	Channel         string `json:"channel"`
	UpdateAvailable bool   `json:"update_available"`
	LatestVersion   string `json:"latest_version"`
	CheckedAt       string `json:"checked_at"`
	Phase           string `json:"phase"`
	Error           string `json:"error"`
}

func stateDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "AIR OS", "State")
}

// workerExe — CLI рядом с треем. Искать его в PATH было бы хуже: в PATH может
// оказаться другая версия, и значок показывал бы замер чужого бинарника.
func workerExe() string {
	self, err := os.Executable()
	if err != nil {
		return "air-worker.exe"
	}
	return filepath.Join(filepath.Dir(self), "air-worker.exe")
}

func runWorkerHidden(args ...string) ([]byte, error) {
	cmd := exec.Command(workerExe(), args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Output()
}

func readWorkerVersion() string {
	out, err := runWorkerHidden("version")
	if err != nil {
		return ""
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) == 0 {
		return ""
	}
	return fields[len(fields)-1]
}

func readTrayUpdateStatus(checkIfStale bool) trayUpdateState {
	args := []string{"update", "status", "-json"}
	if checkIfStale {
		args = []string{"update", "check", "-if-stale", "-json"}
	}
	out, err := runWorkerHidden(args...)
	if err != nil && len(out) == 0 {
		return trayUpdateState{Phase: "error", Error: "update status не ответил"}
	}
	var st trayUpdateState
	if json.Unmarshal(bytes.TrimPrefix(out, utf8BOM), &st) != nil {
		return trayUpdateState{Phase: "error", Error: "update status не разобран"}
	}
	return st
}

func updateDir() string {
	return filepath.Join(filepath.Dir(filepath.Dir(workerExe())), "update")
}

func updateStatusLine(st trayUpdateState) string {
	channel := strings.TrimSpace(st.Channel)
	if channel == "" {
		channel = "stable"
	}
	switch {
	case st.Error != "":
		return fmt.Sprintf("Обновления · %s · ошибка: %s", channel, st.Error)
	case st.UpdateAvailable && st.LatestVersion != "":
		return fmt.Sprintf("Обновления · %s · доступно %s", channel, st.LatestVersion)
	case st.Phase == "unchecked" || st.Phase == "":
		return fmt.Sprintf("Обновления · %s · ещё не проверялись", channel)
	default:
		return fmt.Sprintf("Обновления · %s · актуально", channel)
	}
}

func productMenuLine(p productState) string {
	session := strings.TrimSpace(p.Session)
	if len(session) > 22 {
		session = session[:8] + "…"
	}
	if session == "" {
		session = "сессия ?"
	}
	if principal := strings.TrimSpace(p.Principal); principal != "" {
		session = principal + "/" + session
	}
	step := ""
	if p.CurrentStep != "" {
		step = " · шаг " + p.CurrentStep
		if p.ProgressTotal > 0 {
			step += fmt.Sprintf(" (%d/%d)", p.ProgressClosed, p.ProgressTotal)
		}
	}
	state := ""
	switch {
	case p.Err != "":
		state = p.Err
	case p.Outcome == "running":
		state = "работает"
		if p.Worker != "" {
			state += ": " + p.Worker
		}
	case p.StopReason == "lpr_gate" || p.NextAction == "approve_lpr":
		state = "ждёт вашего слова"
	case p.Outcome == "completed" || p.Outcome == "done":
		state = "цель достигнута"
	case p.StopReason == "no_live_worker":
		state = "стоит: нет живого воркера"
	case p.StopReason == "limit":
		state = "стоит: лимит модели"
	case p.StopReason == "not_proven":
		state = "стоит: судья не подтвердил"
	case p.StopReason != "":
		state = "стоит: " + p.StopReason
	case p.Verdict == "THROTTLE":
		state = "сузьте шаг"
	case p.Verdict == "ESCALATE":
		state = "остановлено: нужен человек"
	case p.Verdict == "ЖДЁТ ЛПР":
		state = "ждёт вашего слова"
	case p.Distance == nil:
		state = "нечем измерить"
	default:
		state = fmt.Sprintf("расстояние %d", *p.Distance)
	}
	stamp := ""
	if strings.TrimSpace(p.JudgeAt) != "" {
		if t, err := time.Parse(time.RFC3339Nano, p.JudgeAt); err == nil {
			stamp = " · замер " + t.Local().Format("15:04")
		} else {
			stamp = " · замер " + p.JudgeAt
		}
	}
	return fmt.Sprintf("%s · %s%s · %s%s", session, p.Name, step, state, stamp)
}

// declaredProducts читает продукты, ОБЪЯВЛЕННЫЕ сессиями. Угадывания здесь нет
// намеренно: один раз угадывание по рабочему каталогу поймало каталог, мимо которого
// сессия проходила, и двигатель считал расстояние по чужому состоянию.
func declaredProducts() []productState {
	dir := stateDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	seen := map[string]productState{}
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, "woody-product-") || !strings.HasSuffix(n, ".json") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, n))
		if err != nil {
			continue
		}
		var body struct {
			Path       string `json:"path"`
			Principal  string `json:"principal"`
			SessionKey string `json:"session_key"`
		}
		// BOM ОТРЕЗАЕТСЯ ПЕРЕД РАЗБОРОМ. Правило продукта запрещает BOM у .json, и
		// mode.ps1 с 0.8.4 его не пишет — но файлы, записанные прежними версиями, уже
		// лежат на дисках обеих машин, и терпеть их обязан читатель. Без этого значок
		// печатал «продуктов не объявлено» на машине, где продукт объявлен и судья его
		// видит: отсутствие неотличимо от непрочитанного.
		if json.Unmarshal(bytes.TrimPrefix(raw, utf8BOM), &body) != nil || body.Path == "" {
			continue
		}
		if st, err := os.Stat(body.Path); err != nil || !st.IsDir() {
			continue
		}
		session := strings.TrimSpace(body.SessionKey)
		if session == "" {
			session = strings.TrimSuffix(strings.TrimPrefix(n, "woody-product-"), ".json")
		}
		key := strings.ToLower(body.Path)
		candidate := productState{
			Path: body.Path, Name: filepath.Base(body.Path),
			Session: session, Principal: strings.TrimSpace(body.Principal),
		}
		// Один продукт, объявленный двумя сессиями, — это ОДИН продукт. При миграции
		// рядом могут лежать legacy-файл (только path) и новый host-neutral declaration.
		// Предпочитаем запись с principal/session_key, иначе в меню снова останется
		// бессмысленный UUID вместо владельца.
		if old, ok := seen[key]; !ok || (old.Principal == "" && candidate.Principal != "") {
			seen[key] = candidate
		}
	}
	out := make([]productState, 0, len(seen))
	for _, v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func measure(p productState) productState {
	exe := workerExe()
	if _, err := os.Stat(exe); err != nil {
		p.Err = "рядом нет air-worker.exe — мерить нечем"
		return p
	}
	cmd := exec.Command(exe, "drift", "-product", p.Path, "-json")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		p.Err = "drift не ответил"
		return p
	}
	var d struct {
		Distance *int   `json:"distance"`
		Verdict  string `json:"verdict"`
		Stall    int    `json:"stall_moves"`
		At       string `json:"at"`
	}
	if json.Unmarshal(out, &d) != nil {
		p.Err = "ответ drift не разобран"
		return p
	}
	p.Distance, p.Verdict, p.Stall, p.JudgeAt = d.Distance, d.Verdict, d.Stall, d.At

	// Текущий шаг/воркер берутся из штатного adapter status. Значок не читает PLAN
	// и jobs сам: иначе рядом с ядром возник бы второй парсер состояния.
	statusCmd := exec.Command(exe, "adapter", "-action", "status", "-product", p.Path)
	statusCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	statusOut, _ := statusCmd.Output()
	if len(statusOut) != 0 {
		var a struct {
			Outcome     string `json:"outcome"`
			CurrentStep string `json:"current_step"`
			NextAction  string `json:"next_action"`
			StopReason  string `json:"stop_reason"`
			Progress    struct {
				Closed int `json:"closed"`
				Total  int `json:"total"`
			} `json:"progress"`
			Workers []struct {
				Runner   string `json:"runner"`
				Provider string `json:"provider"`
				Model    string `json:"model"`
				Role     string `json:"role"`
			} `json:"workers"`
		}
		if json.Unmarshal(statusOut, &a) == nil {
			p.Outcome, p.CurrentStep, p.NextAction, p.StopReason = a.Outcome, a.CurrentStep, a.NextAction, a.StopReason
			p.ProgressClosed, p.ProgressTotal = a.Progress.Closed, a.Progress.Total
			if len(a.Workers) != 0 {
				w := a.Workers[0]
				parts := []string{}
				if w.Role != "" {
					parts = append(parts, w.Role)
				}
				if w.Provider != "" {
					parts = append(parts, w.Provider)
				} else if w.Runner != "" {
					parts = append(parts, w.Runner)
				}
				if w.Model != "" {
					parts = append(parts, w.Model)
				}
				p.Worker = strings.Join(parts, " ")
			}
		}
	}
	return p
}

func runUpdateCommand(args ...string) {
	go func() {
		cmd := exec.Command(workerExe(), append([]string{"update"}, args...)...)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		_, _ = cmd.CombinedOutput()
		refreshAsync()
	}()
}

// shapeAndColor — единственное место, где состояние превращается в вид значка.
func shapeAndColor(ps []productState) (iconShape, rgba) {
	if len(ps) == 0 {
		return shapeRing, colorUnknown
	}
	attn, unknown, working := false, false, false
	for _, p := range ps {
		switch {
		case p.Err != "" || p.Distance == nil:
			// «Нечем измерить» — не ноль и не тревога. Сворачивать его в «всё хорошо»
			// значило бы повторить ровно ту ошибку, за которую судье завели третий код.
			unknown = true
		case p.Verdict == "THROTTLE" || p.Verdict == "ESCALATE" || p.Verdict == "ЖДЁТ ЛПР":
			attn = true
		case *p.Distance > 0:
			working = true
		}
	}
	switch {
	case attn:
		return shapeNotch, colorAttn
	case working:
		return shapeDisc, colorWork
	case unknown:
		return shapeRing, colorUnknown
	default:
		return shapeDisc, colorOK
	}
}

func tooltip(ps []productState, ver string) string {
	head := appName
	if strings.TrimSpace(ver) != "" {
		head += " " + strings.TrimSpace(ver)
	}
	if len(ps) == 0 {
		return head + " · продуктов не объявлено"
	}
	var parts []string
	for _, p := range ps {
		switch {
		case p.Err != "":
			parts = append(parts, fmt.Sprintf("%s: %s", p.Name, p.Err))
		case p.Distance == nil:
			parts = append(parts, fmt.Sprintf("%s: нечем измерить", p.Name))
		default:
			parts = append(parts, fmt.Sprintf("%s: %d · %s", p.Name, *p.Distance, p.Verdict))
		}
	}
	return head + " · " + strings.Join(parts, " · ")
}

func refreshAsync() {
	go func() {
		ps := declaredProducts()
		for i := range ps {
			ps[i] = measure(ps[i])
		}
		ver := readWorkerVersion()
		upd := readTrayUpdateStatus(true)
		stateMu.Lock()
		products = ps
		workerVer = ver
		updateStatus = upd
		stateMu.Unlock()
		// В окно возвращаемся сообщением, а не прямым вызовом: Shell_NotifyIcon и меню
		// обязаны жить в том потоке, который завёл окно.
		procPostMessageW.Call(uintptr(hwnd), uintptr(wmRefreshDone), 0, 0)
	}()
}

// utf8BOM — три байта, которыми Windows PowerShell 5.1 помечает UTF-8.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

const wmRefreshDone = 0x0400 + 2

func applyState() {
	stateMu.Lock()
	ps := append([]productState(nil), products...)
	ver := workerVer
	stateMu.Unlock()

	shape, color := shapeAndColor(ps)
	// Значок пересоздаётся каждый раз и старый уничтожается: дескрипторы значков —
	// ограниченный ресурс, и утечка по одному в минуту съедает стол за сутки.
	next := createIcon(color, shape)
	nd := notifyIconData{
		Size:  uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:   hwnd,
		ID:    trayID,
		Flags: nifIcon | nifTip,
		Icon:  next,
	}
	tip := tooltip(ps, ver)
	copyTip(&nd.Tip, tip)
	r, _, _ := procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nd)))
	writeProof(r != 0, tip)
	if curIcon != 0 && curIcon != next {
		procDestroyIcon.Call(uintptr(curIcon))
	}
	curIcon = next
}

func showMenu() {
	stateMu.Lock()
	ps := append([]productState(nil), products...)
	ver := workerVer
	upd := updateStatus
	stateMu.Unlock()

	m, _, _ := procCreatePopupMenu.Call()
	if m == 0 {
		return
	}
	defer procDestroyMenu.Call(m)

	versionLine := "AirWorker"
	if strings.TrimSpace(ver) != "" {
		versionLine += " " + strings.TrimSpace(ver)
	}
	procAppendMenuW.Call(m, mfString|mfGrayed, 0,
		uintptr(unsafe.Pointer(utf16(versionLine))))
	procAppendMenuW.Call(m, mfString|mfGrayed, 0,
		uintptr(unsafe.Pointer(utf16(updateStatusLine(upd)))))
	procAppendMenuW.Call(m, mfSeparator, 0, 0)

	if len(ps) == 0 {
		procAppendMenuW.Call(m, mfString|mfGrayed, 0,
			uintptr(unsafe.Pointer(utf16("продуктов не объявлено"))))
	}
	for i, p := range ps {
		procAppendMenuW.Call(m, mfString, uintptr(idProduct+i),
			uintptr(unsafe.Pointer(utf16(productMenuLine(p)))))
	}

	procAppendMenuW.Call(m, mfSeparator, 0, 0)
	procAppendMenuW.Call(m, mfString, idRefresh, uintptr(unsafe.Pointer(utf16("Обновить замер"))))
	procAppendMenuW.Call(m, mfString, idState, uintptr(unsafe.Pointer(utf16("Открыть каталог состояния"))))
	procAppendMenuW.Call(m, mfString, idUpdateLog, uintptr(unsafe.Pointer(utf16("Открыть журнал обновления"))))

	procAppendMenuW.Call(m, mfSeparator, 0, 0)
	procAppendMenuW.Call(m, mfString, idCheckUpdate, uintptr(unsafe.Pointer(utf16("Проверить обновление"))))
	installFlags := uintptr(mfString | mfGrayed)
	installText := "Установить обновление — нет доступного"
	if upd.UpdateAvailable {
		installFlags = mfString
		installText = "Установить обновление"
		if strings.TrimSpace(upd.LatestVersion) != "" {
			installText = "Установить " + strings.TrimSpace(upd.LatestVersion)
		}
	}
	procAppendMenuW.Call(m, installFlags, idInstallUpdate, uintptr(unsafe.Pointer(utf16(installText))))

	stableFlags := uintptr(mfString)
	preFlags := uintptr(mfString)
	if strings.EqualFold(strings.TrimSpace(upd.Channel), "prerelease") {
		preFlags |= mfChecked
	} else {
		stableFlags |= mfChecked
	}
	procAppendMenuW.Call(m, stableFlags, idChannelStable, uintptr(unsafe.Pointer(utf16("Канал обновлений: stable"))))
	procAppendMenuW.Call(m, preFlags, idChannelPrerelease, uintptr(unsafe.Pointer(utf16("Канал обновлений: prerelease"))))

	procAppendMenuW.Call(m, mfSeparator, 0, 0)
	procAppendMenuW.Call(m, mfString, idQuit, uintptr(unsafe.Pointer(utf16("Выход"))))

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Без SetForegroundWindow меню не закроется по щелчку мимо него — известная
	// особенность всплывающих меню у окон без фокуса.
	procSetForegroundWindow.Call(uintptr(hwnd))
	procTrackPopupMenu.Call(m, tpmLeftAlign|tpmRightButton,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(hwnd), 0)
	procPostMessageW.Call(uintptr(hwnd), wmClose+0x1000, 0, 0) // холостое: гасит меню
}

func openPath(p string) {
	procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(utf16("open"))),
		uintptr(unsafe.Pointer(utf16(p))),
		0, 0, 5 /* SW_SHOW */)
}

func messageBox(title, text string, flags uintptr) uintptr {
	r, _, _ := procMessageBoxW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(utf16(text))),
		uintptr(unsafe.Pointer(utf16(title))),
		flags,
	)
	return r
}

func confirmUpdateInstall(latest string) bool {
	if strings.TrimSpace(latest) == "" {
		latest = "доступную версию"
	}
	text := "Установить AirWorker " + latest + " из подписанного GitHub Release?\n\n" +
		"Перед заменой будут проверены подпись манифеста, SHA-256 и размер обоих файлов. " +
		"При неуспешной post-install проверке выполняется откат."
	return messageBox("AirWorker update", text, mbYesNo|mbIconQuestion) == idYes
}

func wndProc(h syscall.Handle, message uint32, wparam, lparam uintptr) uintptr {
	switch {
	case message == wmTaskba && wmTaskba != 0:
		// Проводник перезапустился и забыл все значки. Без этой ветви значок
		// исчезает навсегда, а процесс остаётся жив — худшее из состояний:
		// механизм работает, а сказать об этом нечем.
		addIcon()
		applyState()
		return 0
	case message == wmUserTrayMsg:
		switch uint32(lparam) {
		case wmRBUTTONUP, wmLBUTTONUP:
			showMenu()
		case wmLBUTTONDBL:
			openPath(stateDir())
		}
		return 0
	case message == wmRefreshDone:
		applyState()
		return 0
	case message == wmTimer:
		refreshAsync()
		return 0
	case message == wmCommand:
		id := uint32(wparam & 0xffff)
		switch {
		case id == idRefresh:
			refreshAsync()
		case id == idState:
			openPath(stateDir())
		case id == idUpdateLog:
			_ = os.MkdirAll(updateDir(), 0o755)
			openPath(updateDir())
		case id == idCheckUpdate:
			runUpdateCommand("check")
		case id == idInstallUpdate:
			stateMu.Lock()
			upd := updateStatus
			stateMu.Unlock()
			if upd.UpdateAvailable && confirmUpdateInstall(upd.LatestVersion) {
				runUpdateCommand("install")
			}
		case id == idChannelStable:
			runUpdateCommand("channel", "stable")
		case id == idChannelPrerelease:
			runUpdateCommand("channel", "prerelease")
		case id == idQuit:
			procPostMessageW.Call(uintptr(h), wmClose, 0, 0)
		case id >= idProduct:
			stateMu.Lock()
			ps := append([]productState(nil), products...)
			stateMu.Unlock()
			if i := int(id - idProduct); i >= 0 && i < len(ps) {
				openPath(ps[i].Path)
			}
		}
		return 0
	case message == wmClose:
		procDestroyWindow.Call(uintptr(h))
		return 0
	case message == wmDestroy:
		removeIcon()
		os.Remove(filepath.Join(stateDir(), "air-worker-tray.json"))
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(h), uintptr(message), wparam, lparam)
	return r
}

// ФАКТ ПРИЁМА ЗНАЧКА ЗАПИСЫВАЕТСЯ НА ДИСК, а не подразумевается.
//
// Причина конкретная: 14.09.2026 понадобилось доказать ЛПР, что значок действительно
// висит, а снимок экрана оказался недоступен. «Процесс жив» и «окно заведено» этого не
// доказывают: окно можно завести и не отдать значок оболочке, и снаружи это выглядит
// одинаково. Shell_NotifyIcon ВОЗВРАЩАЕТ ответ — он и записывается.
//
// Файл живёт рядом с остальным состоянием контура и переписывается атомарно: две копии
// значка одновременно невозможны (мьютекс), но перезапуск во время чтения — вполне.
type trayProof struct {
	PID      int    `json:"pid"`
	At       string `json:"at"`
	Accepted bool   `json:"shell_accepted"`
	Tooltip  string `json:"tooltip"`
	Version  string `json:"by"`
}

func writeProof(accepted bool, tip string) {
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	body, err := json.MarshalIndent(trayProof{
		PID:      os.Getpid(),
		At:       time.Now().Format("2006-01-02T15:04:05"),
		Accepted: accepted,
		Tooltip:  tip,
		Version:  appName + " tray",
	}, "", "  ")
	if err != nil {
		return
	}
	path := filepath.Join(dir, "air-worker-tray.json")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, body, 0o644) != nil {
		return
	}
	if os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
}

func addIcon() {
	tip := appName + " · замер идёт"
	nd := notifyIconData{
		Size:            uint32(unsafe.Sizeof(notifyIconData{})),
		Wnd:             hwnd,
		ID:              trayID,
		Flags:           nifMessage | nifIcon | nifTip,
		CallbackMessage: wmUserTrayMsg,
		Icon:            createIcon(colorUnknown, shapeRing),
	}
	copyTip(&nd.Tip, tip)
	curIcon = nd.Icon
	r, _, _ := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nd)))
	writeProof(r != 0, tip)
}

func removeIcon() {
	nd := notifyIconData{Size: uint32(unsafe.Sizeof(notifyIconData{})), Wnd: hwnd, ID: trayID}
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nd)))
}

func main() {
	// Цикл сообщений обязан жить в ОДНОМ потоке ОС от начала до конца: окно
	// принадлежит потоку, который его завёл, и переезд горутины на другой поток
	// оборвёт доставку сообщений молча.
	runtime.LockOSThread()

	// ВТОРОЙ ЗНАЧОК — ХУЖЕ ОТСУТСТВИЯ. Два значка одного продукта показывают два
	// замера, и человек читает их как разногласие механизма с самим собой.
	// КОД ОШИБКИ БЕРЁТСЯ ТРЕТЬИМ ЗНАЧЕНИЕМ САМОГО ВЫЗОВА, А НЕ ОТДЕЛЬНЫМ GetLastError.
	//
	// Прежде здесь стоял отдельный вызов GetLastError после CreateMutexW. Он НЕ ВИДИТ
	// кода ошибки: между двумя переходами в ядро рантайм Go делает свои вызовы ОС и
	// затирает последнюю ошибку потока, и LockOSThread выше от этого не спасает — поток
	// тот же, а код в нём уже чужой. Проверка «значок уже запущен» не срабатывала НИКОГДА.
	//
	// Пока значки поднимались по одному, дефект был невидим: запускающая команда
	// спрашивала мьютекс снаружи (OpenMutexW, это работает) и второй раз не запускала.
	// После перезагрузки 14.09.2026 три сессии подняли хук SessionStart за 38 мс,
	// все три спросили снаружи прежде, чем кто-то завёл мьютекс, — и в трее встало ТРИ
	// значка. Последней линией обороны должен был стать этот мьютекс, и он молчал.
	//
	// Хуже всего то, что ровно эту ловушку я нашла и починила в cmd/lock_windows.go
	// накануне, с комментарием, почему так нельзя, — и не перенесла урок в этот файл.
	// Правило, живущее уроком, чинится в месте находки и уцелевает по соседству.
	mu, _, callErr := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(utf16(`Local\air-worker-tray`))))
	if errno, ok := callErr.(syscall.Errno); mu != 0 && ok && uintptr(errno) == errAlreadyExists {
		return
	}

	inst, _, _ := procGetModuleHandleW.Call(0)
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  syscall.Handle(inst),
		ClassName: utf16(className),
	}
	if r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return
	}
	h, _, _ := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(utf16(className))),
		uintptr(unsafe.Pointer(utf16(appName))),
		0, 0, 0, 0, 0, 0, 0, inst, 0)
	if h == 0 {
		return
	}
	hwnd = syscall.Handle(h)

	t, _, _ := procRegisterWindowMsgW.Call(uintptr(unsafe.Pointer(utf16("TaskbarCreated"))))
	wmTaskba = uint32(t)

	addIcon()
	refreshAsync()
	procSetTimer.Call(uintptr(hwnd), 1, uintptr(refreshEvery/time.Millisecond), 0)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
