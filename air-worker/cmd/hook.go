package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ВХОД БИНАРНИКА ДЛЯ ХУКОВ CLAUDE CODE. План air-worker, шаг 38.
//
// Архитектура утверждена ЛПР: «Манифест подключает. Хук доставляет. Бинарник решает. Скил
// объясняет.» Хук (обёртка под hooks.json) — только адаптер: получил событие харнесса,
// передал сюда, вернул код и stderr обратно харнессу. Правил, текстов отказа и состояния в
// хуке нет и не будет — они все здесь.
//
// ПРОТОКОЛ ХУКОВ CLAUDE CODE. На stdin приходит JSON: session_id, transcript_path, cwd,
// hook_event_name — всегда; tool_name/tool_input — у PreToolUse; stop_hook_active — у Stop
// и SubagentStop. Код 0 значит «пропустить», код 2 — «отказать» с причиной в stderr (у
// PreToolUse это запрет вызова инструмента, у Stop — запрет закончить ход), любой другой
// ненулевой код — неблокирующая ошибка, харнесс идёт дальше.
//
// КЛАССЫ СОБЫТИЙ — ОДНОЙ ТАБЛИЦЕЙ, hookEventClasses ниже, и это ЕДИНСТВЕННОЕ место, где
// класс события решается (шаг 66 подключается СЮДА же — classifyBypass ниже вызывается
// обработчиком PreToolUse, второй классификации не заводится):
//
//	control    PreToolUse — событие ДО действия. Решение не получено (ошибка обработчика,
//	           паника, нечитаемое для обработчика событие активной сессии) -> ОТКАЗ кодом 2
//	           с названной причиной: fail-closed.
//	lifecycle  Stop, SubagentStart, SubagentStop, SessionStart, PostToolUse,
//	           UserPromptSubmit. Решение обработчика действует, когда оно получено; когда
//	           не получено -> ПРОПУСК кодом 0: fail-open. Fail-closed на Stop не дал бы
//	           сессии закончить ход ни разу — это НЕ то же самое, что «безопасно».
//	unknown    событие механизму не названо — пропуск и запись в след.
//
// АКТИВНОСТЬ ПРОВЕРЯЕТСЯ ДО КЛАССА И ДЕРЖИТ СТАРШИНСТВО НАД НИМ. До явного включения Air
// Worker для сессии — no-op: пропуск ВСЕХ событий БЕЗ единого решения, unknown в том числе.
// Активность — та же identity и то же состояние, что пишет `air-worker session declare`
// (session.go, шаг 39): principal "claude" (адаптер, который зовёт хуки Claude Code) плюс
// session-key = session_id события. Второй модели активности здесь не заводится — иначе
// `session declare/off/on` и хук расходились бы в вопросе «включён ли Air Worker».
//
// Если JSON события не разобрался, если в нём нет session_id, или если session_id не
// проходит правило identity (session.go, sanitizeIdentityPart) — проверить активность
// нечем, и сессия считается неактивной: пропуск и след (протокол хуков нарушен, и это стоит
// увидеть — в отличие от рутинного «Air Worker не включён для этой сессии», которое не
// трассируется вовсе: иначе след стал бы бесполезен на первой же машине, где сессий без
// Вуди больше, чем с ним).
//
// ПАНИКА. Паника Go завершает процесс кодом 2 — а в протоколе хуков код 2 значит «блок».
// Необработанная паника в control была бы, возможно, случайным fail-closed, а на Stop —
// сорванным ходом. cmdHook перехватывает панику ЕДИНОЙ точкой (defer/recover) и отвечает по
// классу события той же функцией, что и обычную ошибку обработчика, — см. finishHook.
type hookClass int

const (
	classControl hookClass = iota
	classLifecycle
	classUnknown
)

// String — те же слова, что в плане (control/lifecycle), плюс unknown для симметрии. Уходит
// в JSONL следа как технический токен, а не как объяснение для человека.
func (c hookClass) String() string {
	switch c {
	case classControl:
		return "control"
	case classLifecycle:
		return "lifecycle"
	default:
		return "unknown"
	}
}

// hookEventClasses — ЕДИНСТВЕННАЯ таблица «событие -> класс» во всём бинарнике. Событие,
// которого здесь нет, — classUnknown, а не отказ сборки: харнесс со временем заведёт новые
// события хуков, и незнание одного из них не должно останавливать работу остальных.
var hookEventClasses = map[string]hookClass{
	"PreToolUse":       classControl,
	"Stop":             classLifecycle,
	"SubagentStart":    classLifecycle,
	"SubagentStop":     classLifecycle,
	"SessionStart":     classLifecycle,
	"PostToolUse":      classLifecycle,
	"UserPromptSubmit": classLifecycle,
}

func classifyHookEvent(event string) hookClass {
	if c, ok := hookEventClasses[event]; ok {
		return c
	}
	return classUnknown
}

// hookInput — общее подмножество протокола хуков, которое нужно диспетчеру. Обработчики
// читают его же: заводить второй разбор JSON под каждый обработчик значило бы разойтись с
// протоколом по частям, а не сразу.
type hookInput struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name,omitempty"`
	ToolInput      json.RawMessage `json:"tool_input,omitempty"`
	StopHookActive bool            `json:"stop_hook_active,omitempty"`
}

// hookResult — решение обработчика. Reason годится и для stderr харнесса, и для следа —
// один текст на оба места, а не два формулирования одной причины.
type hookResult struct {
	Block  bool
	Reason string
}

// hookHandler — обработчик одного события. Ошибка ИЛИ паника здесь — это «решение получить
// не удалось»: дальше её разбирает finishHook по классу события, самому обработчику это
// решать незачем.
type hookHandler func(in hookInput) (hookResult, error)

func defaultHookHandler(hookInput) (hookResult, error) { return hookResult{}, nil }

// hookHandlers — реестр «событие -> обработчик». PreToolUse уже занят стражем обхода
// (classifyBypass, шаг 66, подключён этим же шагом по решению ЛПР о переносе); остальные
// решения (учёт субагентов, стражи кодировки и режима) встанут регистрацией в этой же карте
// на следующих шагах, без изменения диспетчера.
var hookHandlers = map[string]hookHandler{
	"PreToolUse": handlePreToolUseBypassGuard,
}

func handlerFor(event string) hookHandler {
	if h, ok := hookHandlers[event]; ok {
		return h
	}
	return defaultHookHandler
}

// handlePreToolUseBypassGuard — страж обхода штатного пути установки (шаг 66), подключённый
// к событию хоста этим шагом. classifyBypass сама по себе чистая функция над текстом
// команды; здесь только извлечение команды из tool_input и вызов уже готового решения —
// второй классификации не заводится.
func handlePreToolUseBypassGuard(in hookInput) (hookResult, error) {
	if !strings.EqualFold(in.ToolName, "Bash") {
		return hookResult{}, nil
	}
	var params struct {
		Command string `json:"command"`
	}
	if len(in.ToolInput) > 0 {
		if err := json.Unmarshal(in.ToolInput, &params); err != nil {
			return hookResult{}, fmt.Errorf("не разобран tool_input PreToolUse: %v", err)
		}
	}
	if blocked, reason := classifyBypass(params.Command, []string{hookInstallHome()}); blocked {
		return hookResult{Block: true, Reason: reason}, nil
	}
	return hookResult{}, nil
}

// hookStateDirEnv — переменная окружения для тестов, перекрывающая каталог состояния сессии
// (sessionStateDir, session_state_windows.go/session_state_other.go). Настоящие файлы в
// %ProgramData%/XDG_STATE_HOME тестами не трогаются.
const hookStateDirEnv = "AIR_WORKER_HOOK_STATE_DIR"

func hookStateDir() string {
	if v := strings.TrimSpace(os.Getenv(hookStateDirEnv)); v != "" {
		_ = os.MkdirAll(v, 0o755)
		return v
	}
	return sessionStateDir()
}

// hookSessionIdentity — identity хук-события: principal "claude" (адаптер Claude Code,
// единственный, что зовёт `air-worker hook` на этом шаге), session-key = session_id
// события. sanitizeIdentityPart (session.go) решает, годится ли строка session_id как
// session-key: не годится — активность проверить нечем, событие считается неактивной
// сессией (см. комментарий у cmdHook).
func hookSessionIdentity(sessionID string) (sessionIdentity, bool) {
	id, err := parseIdentity("claude", sessionID)
	if err != nil {
		return sessionIdentity{}, false
	}
	return id, true
}

// sessionActive — включён ли Air Worker для этой сессии. Та же identity и то же состояние,
// что читает/пишет `air-worker session declare|off|on` (session.go, К44): второй модели
// активности здесь нет. Файла режима нет, режим выключен, либо сессия выключена словом
// ЛПР (`session off`) — во всех трёх случаях неактивна.
func sessionActive(sessionID string) bool {
	id, ok := hookSessionIdentity(sessionID)
	if !ok {
		return false
	}
	dir := hookStateDir()
	if _, err := readSessionOff(dir, id); err == nil {
		return false
	}
	var mode sessionModeState
	if readJSON(sessionModePath(dir, id), &mode) != nil {
		return false
	}
	return mode.Enabled
}

// hookTraceRecord — одна строка следа: время, событие, сессия, класс, что случилось, какое
// решение отдано. Тот же каталог и построчный JSONL, что у остального состояния сессии:
// человек смотрит в одно место, а не в четыре разных.
type hookTraceRecord struct {
	TS       string `json:"ts"`
	Role     string `json:"role"`
	Session  string `json:"session"`
	Event    string `json:"event"`
	Class    string `json:"class"`
	Decision string `json:"decision"`
	Detail   string `json:"detail"`
}

// hookUnknownSessionKey — заглушка в имени файла следа, когда session_id недоступен вовсе
// (JSON не разобрался или поля не было). Настоящий session_id харнесса на это значение не
// похож никогда, так что общий файл не путается с чьей-то настоящей сессией.
const hookUnknownSessionKey = "unknown"

func hookTracePath(sessionID string) string {
	key := strings.TrimSpace(sessionID)
	if key == "" {
		key = hookUnknownSessionKey
	}
	return filepath.Join(hookStateDir(), "woody-trace-"+key+".jsonl")
}

// writeHookTrace — запись следа. ЛЮБАЯ ОШИБКА ЗДЕСЬ ГЛОТАЕТСЯ: решение хука след менять не
// должен, диагностика не важнее работы харнесса.
func writeHookTrace(sessionID, event string, class hookClass, decision, detail string) {
	defer func() { _ = recover() }()
	dir := hookStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	rec := hookTraceRecord{
		TS:       time.Now().Format("2006-01-02T15:04:05"),
		Role:     "хук",
		Session:  sessionID,
		Event:    event,
		Class:    class.String(),
		Decision: decision,
		Detail:   detail,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	f, err := os.OpenFile(hookTracePath(sessionID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(b, '\r', '\n'))
}

// finishHook — код возврата и след, когда решение обработчика получить НЕ удалось (ошибка
// или паника). control -> отказ кодом 2 с причиной в stderr: fail-closed. lifecycle/unknown
// -> пропуск кодом 0: fail-open, иначе Stop не дал бы сессии закончить ход ни разу.
func finishHook(class hookClass, event, sessionID, reason string) int {
	if class == classControl {
		fmt.Fprint(os.Stderr, "air-worker hook "+event+": "+reason+lineEnding)
		writeHookTrace(sessionID, event, class, "отклонил", reason)
		return 2
	}
	writeHookTrace(sessionID, event, class, "пропустил", reason)
	return 0
}

// cmdHook — подкоманда `air-worker hook <событие>`. Событие хоста читается из stdin (JSON),
// решение принимает бинарник целиком; сам хук (обёртка под hooks.json) — только адаптер.
func cmdHook(argv []string) (code int) {
	if len(argv) < 1 || strings.TrimSpace(argv[0]) == "" {
		fmt.Fprint(os.Stderr, "укажи событие: air-worker hook <PreToolUse|Stop|...>"+lineEnding)
		return 2
	}
	event := argv[0]
	class := classifyHookEvent(event)
	var sessionID string

	// ПАНИКА — ОДНОЙ ТОЧКОЙ. recover здесь ловит панику где угодно ниже по функции: и в
	// чтении/разборе stdin, и — для протокола хуков это главное — в самом обработчике.
	// Именованный возврат code позволяет defer переписать его после recover.
	defer func() {
		if r := recover(); r != nil {
			code = finishHook(class, event, sessionID, fmt.Sprintf("паника: %v", r))
		}
	}()

	raw, readErr := io.ReadAll(os.Stdin)
	var in hookInput
	var parseErr error
	if readErr != nil {
		parseErr = readErr
	} else {
		parseErr = json.Unmarshal(bytes.TrimPrefix(raw, utf8BOM), &in)
	}
	sessionID = in.SessionID

	// НЕЧИТАЕМЫЙ ВХОД ИЛИ ПУСТОЙ session_id — сессия считается НЕАКТИВНОЙ, каким бы ни был
	// класс события. Fail-closed у control защищает АКТИВНУЮ сессию с работающим Air
	// Worker, а не любые байты на stdin: без session_id проверить активность нечем, и
	// отказывать здесь означало бы держать под блоком инструменты каждой сессии Claude
	// Code на машине, где Вуди никто не включал.
	if parseErr != nil || strings.TrimSpace(sessionID) == "" {
		detail := "в событии нет session_id"
		if parseErr != nil {
			detail = "не разобрался JSON на stdin: " + parseErr.Error()
		}
		writeHookTrace(sessionID, event, class, "пропустил", detail)
		return 0
	}

	if !sessionActive(sessionID) {
		// Рутинный путь для подавляющего большинства вызовов: Air Worker для этой сессии
		// не включён. НИКАКИХ решений, включая след, — ровно так и названо в шаге: «no-op:
		// пропуск всех событий без решений», unknown в том числе.
		return 0
	}

	if class == classUnknown {
		writeHookTrace(sessionID, event, class, "пропустил", "неизвестное событие: "+event)
		return 0
	}

	res, err := handlerFor(event)(in)
	if err != nil {
		return finishHook(class, event, sessionID, err.Error())
	}
	if res.Block {
		fmt.Fprint(os.Stderr, "air-worker hook "+event+": "+res.Reason+lineEnding)
		writeHookTrace(sessionID, event, class, "отклонил", res.Reason)
		return 2
	}
	return 0
}
