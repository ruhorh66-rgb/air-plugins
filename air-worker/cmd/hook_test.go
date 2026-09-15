package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Шаг 38, К11/К15: вход бинарника для хуков Claude Code. Каждый тест подменяет каталог
// состояния переменной hookStateDirEnv — настоящие файлы в %ProgramData%/XDG_STATE_HOME не
// трогаются.

// withStdin — временно подменяет os.Stdin буфером с content. Тот же приём, что
// captureStdout (planrequire_test.go), только для входа, а не для выхода.
func withStdin(t *testing.T, content string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString(content); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
}

// captureStderr — как captureStdout (planrequire_test.go), но для os.Stderr: причина
// отказа control-события печатается именно туда, а не в os.Stdout.
func captureStderr(t *testing.T, f func() int) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = w
	code := f()
	_ = w.Close()
	os.Stderr = orig
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	return buf.String(), code
}

// hookTestState — временный каталог состояния плюс сессия с включённым режимом: ровно то,
// что читает sessionActive через ту же identity, что и `air-worker session declare`.
func hookTestState(t *testing.T, sessionID string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(hookStateDirEnv, dir)
	id, ok := hookSessionIdentity(sessionID)
	if !ok {
		t.Fatalf("session_id %q не проходит identity", sessionID)
	}
	mode := sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}
	if err := writeStateJSON(sessionModePath(dir, id), mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

// stubHookHandler — регистрирует обработчик события на время теста и снимает его после:
// реестр общий на пакет, и «протечь» в следующий тест он не должен.
func stubHookHandler(t *testing.T, event string, h hookHandler) {
	t.Helper()
	old, had := hookHandlers[event]
	hookHandlers[event] = h
	t.Cleanup(func() {
		if had {
			hookHandlers[event] = old
		} else {
			delete(hookHandlers, event)
		}
	})
}

func assertTraceContains(t *testing.T, dir, session, substr string) {
	t.Helper()
	p := filepath.Join(dir, "woody-trace-"+session+".jsonl")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ожидался файл следа %s: %v", p, err)
	}
	if !strings.Contains(string(raw), substr) {
		t.Fatalf("след не содержит %q: %s", substr, raw)
	}
}

// --- control ------------------------------------------------------------------------

func TestHookControlErrorBlocksWithReason(t *testing.T) {
	session := "s-control-err"
	hookTestState(t, session)
	stubHookHandler(t, "PreToolUse", func(hookInput) (hookResult, error) {
		return hookResult{}, errors.New("судья недоступен")
	})
	withStdin(t, `{"session_id":"`+session+`","hook_event_name":"PreToolUse","tool_name":"Bash"}`)

	out, code := captureStderr(t, func() int { return cmdHook([]string{"PreToolUse"}) })
	if code != 2 {
		t.Fatalf("control при ошибке обработчика обязан отдавать код 2, получено %d", code)
	}
	if !strings.Contains(out, "судья недоступен") {
		t.Fatalf("причина обязана быть названа в stderr, получено: %q", out)
	}
}

func TestHookControlPanicBlocksWithReason(t *testing.T) {
	session := "s-control-panic"
	hookTestState(t, session)
	stubHookHandler(t, "PreToolUse", func(hookInput) (hookResult, error) {
		panic("обвал обработчика")
	})
	withStdin(t, `{"session_id":"`+session+`","hook_event_name":"PreToolUse"}`)

	out, code := captureStderr(t, func() int { return cmdHook([]string{"PreToolUse"}) })
	if code != 2 {
		t.Fatalf("control при панике обработчика обязан отдавать код 2, получено %d", code)
	}
	if !strings.Contains(out, "обвал обработчика") {
		t.Fatalf("причина паники обязана быть названа в stderr, получено: %q", out)
	}
}

// --- lifecycle ------------------------------------------------------------------------

func TestHookLifecycleErrorSkipsAndTraces(t *testing.T) {
	session := "s-life-err"
	dir := hookTestState(t, session)
	stubHookHandler(t, "SessionStart", func(hookInput) (hookResult, error) {
		return hookResult{}, errors.New("не прочитан реестр")
	})
	withStdin(t, `{"session_id":"`+session+`","hook_event_name":"SessionStart"}`)

	if code := cmdHook([]string{"SessionStart"}); code != 0 {
		t.Fatalf("lifecycle при ошибке обработчика обязан отдавать код 0 (fail-open), получено %d", code)
	}
	assertTraceContains(t, dir, session, "не прочитан реестр")
}

func TestHookLifecyclePanicSkipsAndTraces(t *testing.T) {
	session := "s-life-panic"
	dir := hookTestState(t, session)
	stubHookHandler(t, "SubagentStop", func(hookInput) (hookResult, error) {
		panic("обвал обработчика")
	})
	withStdin(t, `{"session_id":"`+session+`","hook_event_name":"SubagentStop"}`)

	if code := cmdHook([]string{"SubagentStop"}); code != 0 {
		t.Fatalf("lifecycle при панике обработчика обязан отдавать код 0 (fail-open), получено %d", code)
	}
	assertTraceContains(t, dir, session, "обвал обработчика")
}

// TestHookStopPanicNeverBlocks — Stop проверен отдельно и дословно: паника Go сама по себе
// завершает процесс кодом 2, а Stop с кодом 2 не даёт сессии закончить ход НИ РАЗУ.
func TestHookStopPanicNeverBlocks(t *testing.T) {
	session := "s-stop-panic"
	hookTestState(t, session)
	stubHookHandler(t, "Stop", func(hookInput) (hookResult, error) {
		panic("обвал стража хода")
	})
	withStdin(t, `{"session_id":"`+session+`","hook_event_name":"Stop","stop_hook_active":false}`)

	code := cmdHook([]string{"Stop"})
	if code == 2 {
		t.Fatal("Stop с паникой обязан НЕ давать код 2 — иначе сессия не смогла бы закончить ход")
	}
	if code != 0 {
		t.Fatalf("Stop с паникой обязан давать код 0, получено %d", code)
	}
}

// --- unknown ----------------------------------------------------------------------------

func TestHookUnknownEventSkipsAndTraces(t *testing.T) {
	session := "s-unknown"
	dir := hookTestState(t, session)
	withStdin(t, `{"session_id":"`+session+`","hook_event_name":"НечтоНовое"}`)

	if code := cmdHook([]string{"НечтоНовое"}); code != 0 {
		t.Fatalf("неизвестное событие обязано пропускать кодом 0, получено %d", code)
	}
	assertTraceContains(t, dir, session, "неизвестное")
}

// --- активность ---------------------------------------------------------------------

func TestHookInactiveSessionIsNoOpForEveryClass(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(hookStateDirEnv, dir) // режим сессии НЕ включён — файла woody-mode-* нет
	session := "s-inactive"
	for _, event := range []string{"PreToolUse", "Stop", "НечтоНовое"} {
		event := event
		stubHookHandler(t, event, func(hookInput) (hookResult, error) {
			t.Fatalf("обработчик %s не должен вызываться для неактивной сессии", event)
			return hookResult{}, nil
		})
		withStdin(t, `{"session_id":"`+session+`","hook_event_name":"`+event+`"}`)
		if code := cmdHook([]string{event}); code != 0 {
			t.Fatalf("%s: неактивная сессия обязана давать код 0, получено %d", event, code)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("неактивная сессия не должна оставлять следов в каталоге состояния, найдено: %v", names)
	}
}

func TestHookSessionOffIsNoOp(t *testing.T) {
	session := "s-off"
	dir := hookTestState(t, session)
	id, ok := hookSessionIdentity(session)
	if !ok {
		t.Fatal("identity не разобралась")
	}
	off := sessionOffState{Words: "выхожу из сессии сейчас", Principal: id.Principal, SessionKey: id.SessionKey}
	if err := writeStateJSON(sessionOffPath(dir, id), off); err != nil {
		t.Fatal(err)
	}
	stubHookHandler(t, "PreToolUse", func(hookInput) (hookResult, error) {
		t.Fatal("обработчик не должен вызываться для сессии, выключенной словом ЛПР")
		return hookResult{}, nil
	})
	withStdin(t, `{"session_id":"`+session+`","hook_event_name":"PreToolUse"}`)
	if code := cmdHook([]string{"PreToolUse"}); code != 0 {
		t.Fatalf("сессия, выключенная session off, обязана давать код 0, получено %d", code)
	}
}

// --- нечитаемый вход ------------------------------------------------------------------

func TestHookUnreadableJSONSkipsAndTraces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(hookStateDirEnv, dir)
	withStdin(t, `{не json`)

	if code := cmdHook([]string{"PreToolUse"}); code != 0 {
		t.Fatalf("нечитаемый JSON обязан пропускать кодом 0 (сессия неактивна по определению), получено %d", code)
	}
	p := hookTracePath("")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("нечитаемый вход обязан оставить строку следа (%s): %v", p, err)
	}
}

func TestHookMissingSessionIDSkipsAndTraces(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(hookStateDirEnv, dir)
	withStdin(t, `{"hook_event_name":"PreToolUse"}`) // валидный JSON, но без session_id

	if code := cmdHook([]string{"PreToolUse"}); code != 0 {
		t.Fatalf("событие без session_id обязано пропускать кодом 0, получено %d", code)
	}
	p := hookTracePath("")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("ожидался след по заглушке session_id (%s): %v", p, err)
	}
}

// --- usage --------------------------------------------------------------------------

func TestHookWithoutEventIsUsageError(t *testing.T) {
	if code := cmdHook(nil); code != 2 {
		t.Fatalf("hook без события обязан давать код 2, получено %d", code)
	}
}

// --- страж обхода (шаг 66, подключён этим шагом) ---------------------------------------

func TestHookPreToolUseBlocksInstallBypass(t *testing.T) {
	session := "s-bypass"
	hookTestState(t, session)
	t.Setenv("AIR_WORKER_HOME", `C:\Users\u\AppData\Local\air-worker`)
	body, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: `copy air-worker.exe C:\Users\u\AppData\Local\air-worker\air-worker.exe`})
	if err != nil {
		t.Fatal(err)
	}
	toolInput, err := json.Marshal(struct {
		SessionID     string          `json:"session_id"`
		HookEventName string          `json:"hook_event_name"`
		ToolName      string          `json:"tool_name"`
		ToolInput     json.RawMessage `json:"tool_input"`
	}{SessionID: session, HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: body})
	if err != nil {
		t.Fatal(err)
	}
	withStdin(t, string(toolInput))

	out, code := captureStderr(t, func() int { return cmdHook([]string{"PreToolUse"}) })
	if code != 2 {
		t.Fatalf("обход штатного пути установки обязан давать код 2, получено %d", code)
	}
	if !strings.Contains(out, "каталог установки") {
		t.Fatalf("причина обязана назвать обход, получено: %q", out)
	}
}

func TestHookPreToolUseAllowsOrdinaryCommand(t *testing.T) {
	session := "s-ordinary"
	hookTestState(t, session)
	body, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: "go build ./..."})
	if err != nil {
		t.Fatal(err)
	}
	toolInput, err := json.Marshal(struct {
		SessionID     string          `json:"session_id"`
		HookEventName string          `json:"hook_event_name"`
		ToolName      string          `json:"tool_name"`
		ToolInput     json.RawMessage `json:"tool_input"`
	}{SessionID: session, HookEventName: "PreToolUse", ToolName: "Bash", ToolInput: body})
	if err != nil {
		t.Fatal(err)
	}
	withStdin(t, string(toolInput))

	if code := cmdHook([]string{"PreToolUse"}); code != 0 {
		t.Fatalf("обычная команда обязана давать код 0, получено %d", code)
	}
}

// --- К11: каждый класс события — тест Go на решение бинарника ---------------------------
//
// каждый хук плагина — обёртка, зовущая `air-worker hook <событие>`; решение принимает
// бинарник, и решение каждого хука проверено тестом Go (PLAN.md, К11). Проверяется прогоном
// самого cmdHook (то же, что вызвала бы тонкая обёртка хука) для всех четырёх путей
// политики: no-op до активации, fail-open+след telemetry/lifecycle, fail-closed control при
// активной сессии, fail-open+след неизвестного события; отдельно — что паника в hook
// отвечает по классу события, а не голым кодом 2.
func TestCriterion11Pending(t *testing.T) {
	t.Run("no-op до активации", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv(hookStateDirEnv, dir)
		withStdin(t, `{"session_id":"s-k11-inactive","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"rm -rf /"}}`)
		if code := cmdHook([]string{"PreToolUse"}); code != 0 {
			t.Fatalf("до активации Air Worker обязан быть no-op (код 0), получено %d", code)
		}
	})

	t.Run("telemetry/lifecycle fail-open со следом", func(t *testing.T) {
		session := "s-k11-lifecycle"
		dir := hookTestState(t, session)
		stubHookHandler(t, "SessionStart", func(hookInput) (hookResult, error) {
			return hookResult{}, errors.New("обработчик недоступен")
		})
		withStdin(t, `{"session_id":"`+session+`","hook_event_name":"SessionStart"}`)
		if code := cmdHook([]string{"SessionStart"}); code != 0 {
			t.Fatalf("lifecycle обязан fail-open (код 0) даже при ошибке обработчика, получено %d", code)
		}
		assertTraceContains(t, dir, session, "обработчик недоступен")
	})

	t.Run("control fail-closed с причиной при активной сессии", func(t *testing.T) {
		session := "s-k11-control"
		hookTestState(t, session)
		stubHookHandler(t, "PreToolUse", func(hookInput) (hookResult, error) {
			return hookResult{Block: true, Reason: "запрещённая команда"}, nil
		})
		withStdin(t, `{"session_id":"`+session+`","hook_event_name":"PreToolUse","tool_name":"Bash"}`)
		out, code := captureStderr(t, func() int { return cmdHook([]string{"PreToolUse"}) })
		if code != 2 {
			t.Fatalf("control при активной сессии обязан fail-closed (код 2), получено %d", code)
		}
		if !strings.Contains(out, "запрещённая команда") {
			t.Fatalf("причина отказа обязана быть в stderr, получено: %q", out)
		}
	})

	t.Run("неизвестное событие fail-open со следом", func(t *testing.T) {
		session := "s-k11-unknown"
		dir := hookTestState(t, session)
		withStdin(t, `{"session_id":"`+session+`","hook_event_name":"БудущееСобытие"}`)
		if code := cmdHook([]string{"БудущееСобытие"}); code != 0 {
			t.Fatalf("неизвестное событие обязано fail-open (код 0), получено %d", code)
		}
		assertTraceContains(t, dir, session, "неизвестное")
	})

	t.Run("паника отвечает по классу события, а не кодом 2 голой паники", func(t *testing.T) {
		session := "s-k11-panic-lifecycle"
		hookTestState(t, session)
		stubHookHandler(t, "Stop", func(hookInput) (hookResult, error) {
			panic("обвал")
		})
		withStdin(t, `{"session_id":"`+session+`","hook_event_name":"Stop"}`)
		if code := cmdHook([]string{"Stop"}); code != 0 {
			t.Fatalf("паника на lifecycle-событии (Stop) обязана давать код 0, а не голый код паники, получено %d", code)
		}
	})
}

// --- К15: доставка события в binary через stdin/exit code, без skill-слоя ----------------
//
// adapters доставляют событие в binary без зависимости от skill; прямой EXE-вызов
// используется, если host поддерживает stdin/exit code (PLAN.md, К15). Проверяется двумя
// фактами: (а) решение зависит ТОЛЬКО от содержимого stdin, не от argv/файлов/окружения
// адаптера; (б) exit code реального ПРОЦЕССА (не только внутреннего int) доносит решение —
// прямой вызов `air-worker.exe hook <событие>` с событием на stdin и кодом возврата
// достаточен, второй слой (skill) не нужен.
func TestCriterion15Pending(t *testing.T) {
	t.Run("решение зависит только от stdin", func(t *testing.T) {
		session := "s-k15-stdin"
		hookTestState(t, session)
		withStdin(t, `{"session_id":"`+session+`","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"go test ./..."}}`)
		if code := cmdHook([]string{"PreToolUse"}); code != 0 {
			t.Fatalf("обычная команда с тем же событием обязана давать код 0, получено %d", code)
		}

		hookTestState(t, session)
		t.Setenv("AIR_WORKER_HOME", `C:\Users\u\AppData\Local\air-worker`)
		withStdin(t, `{"session_id":"`+session+`","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"copy air-worker.exe C:\\Users\\u\\AppData\\Local\\air-worker\\air-worker.exe"}}`)
		if code := cmdHook([]string{"PreToolUse"}); code != 2 {
			t.Fatalf("решение обязано меняться при смене только тела stdin (то же событие, тот же argv), получено %d", code)
		}
	})

	t.Run("exit code процесса доносит решение без второго слоя", func(t *testing.T) {
		dir := t.TempDir()
		session := "proc-k15"
		id, ok := hookSessionIdentity(session)
		if !ok {
			t.Fatal("identity не разобралась")
		}
		mode := sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}
		if err := writeStateJSON(sessionModePath(dir, id), mode); err != nil {
			t.Fatal(err)
		}

		cmd := exec.Command(os.Args[0], "-test.run=TestHookHelperProcess", "--", "PreToolUse")
		cmd.Env = append(os.Environ(),
			"AW_HOOK_HELPER=1",
			"AW_HOOK_HELPER_FAIL=1",
			hookStateDirEnv+"="+dir,
		)
		cmd.Stdin = strings.NewReader(`{"session_id":"` + session + `","hook_event_name":"PreToolUse"}`)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		runErr := cmd.Run()

		if code := exitCode(cmd, runErr); code != 2 {
			t.Fatalf("ошибка control-обработчика обязана дойти до процесса кодом 2, получено %d (stderr: %s)", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "подставная ошибка") {
			t.Fatalf("причина обязана дойти до stderr процесса: %s", stderr.String())
		}
	})
}

// --- код, доходящий до процесса ------------------------------------------------------
//
// Тот же приём, что TestInvokeCodexWritesProductRoot/TestCodexHelperProcess рядом в
// пакете: тестовый бинарник перезапускает сам себя подпроцессом, событие приходит
// НАСТОЯЩИМ stdin процесса (а не os.Pipe() внутри одного процесса), и код проверяется
// через ProcessState.ExitCode() (exitCode, util.go) — путь «int из cmdHook -> os.Exit ->
// код процесса», как это устроено в main().

func TestHookExitCodeReachesProcess(t *testing.T) {
	dir := t.TempDir()
	session := "proc-control"
	id, ok := hookSessionIdentity(session)
	if !ok {
		t.Fatal("identity не разобралась")
	}
	mode := sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}
	if err := writeStateJSON(sessionModePath(dir, id), mode); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHookHelperProcess", "--", "PreToolUse")
	cmd.Env = append(os.Environ(),
		"AW_HOOK_HELPER=1",
		"AW_HOOK_HELPER_FAIL=1",
		hookStateDirEnv+"="+dir,
	)
	cmd.Stdin = strings.NewReader(`{"session_id":"` + session + `","hook_event_name":"PreToolUse"}`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	if code := exitCode(cmd, runErr); code != 2 {
		t.Fatalf("ошибка control-обработчика обязана дойти до процесса кодом 2, получено %d (stderr: %s)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "подставная ошибка") {
		t.Fatalf("причина обязана дойти до stderr процесса: %s", stderr.String())
	}
}

func TestHookStopPanicExitCodeReachesProcess(t *testing.T) {
	dir := t.TempDir()
	session := "proc-stop-panic"
	id, ok := hookSessionIdentity(session)
	if !ok {
		t.Fatal("identity не разобралась")
	}
	mode := sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey}
	if err := writeStateJSON(sessionModePath(dir, id), mode); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestHookHelperProcess", "--", "Stop")
	cmd.Env = append(os.Environ(),
		"AW_HOOK_HELPER=1",
		"AW_HOOK_HELPER_PANIC_STOP=1",
		hookStateDirEnv+"="+dir,
	)
	cmd.Stdin = strings.NewReader(`{"session_id":"` + session + `","hook_event_name":"Stop"}`)
	runErr := cmd.Run()

	code := exitCode(cmd, runErr)
	if code == 2 {
		t.Fatal("Stop с паникой не должен доходить до процесса кодом 2")
	}
	if code != 0 {
		t.Fatalf("Stop с паникой обязан доходить до процесса кодом 0, получено %d", code)
	}
}

// TestHookHelperProcess — сама процесс-подставка. Молчит, пока её не позвали явно
// переменной AW_HOOK_HELPER: без неё это холостой прогон обычного `go test`, как и у
// TestCodexHelperProcess рядом в пакете.
func TestHookHelperProcess(t *testing.T) {
	if os.Getenv("AW_HOOK_HELPER") != "1" {
		return
	}
	if os.Getenv("AW_HOOK_HELPER_FAIL") == "1" {
		hookHandlers["PreToolUse"] = func(hookInput) (hookResult, error) {
			return hookResult{}, errors.New("подставная ошибка обработчика")
		}
	}
	if os.Getenv("AW_HOOK_HELPER_PANIC_STOP") == "1" {
		hookHandlers["Stop"] = func(hookInput) (hookResult, error) {
			panic("подставная паника стража хода")
		}
	}
	var event string
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			event = os.Args[i+1]
			break
		}
	}
	os.Exit(cmdHook([]string{event}))
}
