package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// SESSION — host-neutral identity/state (этап 0.10, шаг 39, К12/К44).
//
// До этого шага режим/продукт/гейт ЛПР/выход сессии из работы держал `mode.ps1`, посессионно,
// ключом $env:CLAUDE_CODE_SESSION_ID. Ключ этот — понятие ИМЕННО Claude Code: у Codex и у
// прямого GPT-адаптера такой переменной нет вообще, и завязка на неё делает governance
// нерабочим нигде, кроме одного харнесса.
//
// Здесь — то же самое состояние, но identity объявляется явно командой бинарника:
//
//	air-worker session declare -product <корень> -principal <claude|codex|gpt|...> -session-key <ключ>
//
// `principal` называет АДАПТЕРА (кто зовёт), `session-key` — конкретную сессию этого
// адаптера. Ни то, ни другое НЕ читается из окружения самим бинарником: identity передаётся
// параметром, а откуда её берёт конкретный адаптер (переменная окружения харнесса, поле
// session_id из тела хук-события, что угодно ещё) — решение адаптера, не бинарника. Governance
// здесь не знает и не обязан знать про CLAUDE_CODE_SESSION_ID — эта строка НЕ встречается в
// этом файле нигде, кроме этого комментария, и тест К44 проверяет это фактом, а не словом.
var reIdentityPart = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// sessionIdentity — namespace, в который пишется и из которого читается всё состояние сессии.
// Составной ключ (принципал + ключ сессии) не даёт двум разным адаптерам столкнуться на одном
// ключе сессии, даже если оба почему-то выбрали одинаковую строку.
type sessionIdentity struct {
	Principal  string
	SessionKey string
}

func sanitizeIdentityPart(kind, v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("-%s пуст: identity объявляется явно, а не угадывается", kind)
	}
	if !reIdentityPart.MatchString(v) {
		return "", fmt.Errorf("-%s %q: только буквы, цифры, точка, дефис и подчёркивание", kind, v)
	}
	return v, nil
}

func parseIdentity(principal, sessionKey string) (sessionIdentity, error) {
	p, err := sanitizeIdentityPart("principal", principal)
	if err != nil {
		return sessionIdentity{}, err
	}
	k, err := sanitizeIdentityPart("session-key", sessionKey)
	if err != nil {
		return sessionIdentity{}, err
	}
	return sessionIdentity{Principal: p, SessionKey: k}, nil
}

// namespace — файловый ключ этой identity. Один составной токен: имя файла и есть привязка,
// как было заведено ещё в mode.ps1 (переделка 12.09.2026), только ключ теперь явный, а не
// взятый из переменной окружения одного конкретного харнесса.
func (id sessionIdentity) namespace() string {
	return id.Principal + "__" + id.SessionKey
}

func sessionModePath(dir string, id sessionIdentity) string {
	return filepath.Join(dir, "woody-mode-"+id.namespace()+".json")
}
func sessionProductPath(dir string, id sessionIdentity) string {
	return filepath.Join(dir, "woody-product-"+id.namespace()+".json")
}
func sessionOffPath(dir string, id sessionIdentity) string {
	return filepath.Join(dir, "woody-off-"+id.namespace()+".json")
}

type sessionProductState struct {
	Path       string `json:"path"`
	DeclaredAt string `json:"declared_at"`
	Principal  string `json:"principal"`
	SessionKey string `json:"session_key"`
}

type sessionModeState struct {
	Enabled    bool   `json:"enabled"`
	Principal  string `json:"principal"`
	SessionKey string `json:"session_key"`
	UpdatedAt  string `json:"updated_at"`
}

type sessionOffState struct {
	Words      string `json:"words"`
	At         string `json:"at"`
	Principal  string `json:"principal"`
	SessionKey string `json:"session_key"`
}

func writeStateJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// productReady — тот же критерий годности плана, что и `air-worker goals`: без него
// объявление продукта сессией стало бы дырой, которой можно обойти страж работы (mode.ps1
// решала это, зовя `air-worker goals` подпроцессом; здесь — та же проверка в один вызов,
// без второго процесса).
func productReady(root string) (bool, []string) {
	var cfg runConfig
	if readJSON(filepath.Join(root, "run-config.json"), &cfg) != nil {
		return false, []string{"нет run-config.json — это не продукт air-worker"}
	}
	planName := cfg.Plan
	if planName == "" {
		planName = "PLAN.md"
	}
	planPath := filepath.Join(root, planName)
	if raw, err := os.ReadFile(planPath); err == nil && strings.Contains(string(raw), "ЗАГОТОВКА-НЕ-ЗАПОЛНЕНА") {
		return false, []string{"PLAN.md — незаполненная заготовка"}
	}
	steps := readPlanSteps(planPath)
	if len(steps) == 0 {
		return false, []string{"план пуст или не найден: " + planPath}
	}
	g := readPlanGoals(planPath)
	problems := goalProblems(g, steps)
	if len(g.Criteria) > 0 {
		problems = append(problems, criteriaBinding(root, cfg, g)...)
	}
	return len(problems) == 0, problems
}

func cmdSession(argv []string) int {
	if len(argv) < 1 {
		fmt.Fprint(os.Stderr, "air-worker session declare|status|off|on|forget -principal <p> -session-key <k> ..."+lineEnding)
		return 2
	}
	switch argv[0] {
	case "declare":
		return cmdSessionDeclare(argv[1:])
	case "status":
		return cmdSessionStatus(argv[1:])
	case "off":
		return cmdSessionOff(argv[1:])
	case "on":
		return cmdSessionOn(argv[1:])
	case "forget":
		return cmdSessionForget(argv[1:])
	default:
		fmt.Fprintf(os.Stderr, "неизвестная подкоманда session: %s"+lineEnding, argv[0])
		return 2
	}
}

func sessionFlags(name string) (*flag.FlagSet, *string, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	principal := fs.String("principal", "", "кто объявляет (адаптер: claude, codex, gpt, ...)")
	sessionKey := fs.String("session-key", "", "ключ этой сессии адаптера")
	stateDir := fs.String("state-dir", "", "перекрыть каталог состояния (для тестов)")
	return fs, principal, sessionKey, stateDir
}

func resolveStateDir(override string) string {
	if override != "" {
		_ = os.MkdirAll(override, 0o755)
		return override
	}
	return sessionStateDir()
}

// cmdSessionDeclare — продукт и режим сессии объявляются одной командой, host-neutral.
// Коды: 0 объявлено; 1 отказ (план не годен / плохая identity); 2 разбор флагов.
func cmdSessionDeclare(argv []string) int {
	fs, principal, sessionKey, stateDir := sessionFlags("session declare")
	product := fs.String("product", "", "корень продукта")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	id, err := parseIdentity(*principal, *sessionKey)
	if err != nil {
		fmt.Print("ОТКАЗ: " + err.Error() + lineEnding)
		return 1
	}
	if strings.TrimSpace(*product) == "" {
		fmt.Print("ОТКАЗ: -product пуст" + lineEnding)
		return 1
	}
	root, err := filepath.Abs(*product)
	if err != nil {
		fmt.Print("ОТКАЗ: не разобран путь продукта: " + err.Error() + lineEnding)
		return 1
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		fmt.Print("ОТКАЗ: нет такого каталога — " + root + lineEnding)
		return 1
	}
	ok, problems := productReady(root)
	if !ok {
		fmt.Print("ОТКАЗ: план не годен к работе —" + lineEnding)
		for _, p := range problems {
			fmt.Print("  - " + p + lineEnding)
		}
		return 1
	}
	dir := resolveStateDir(*stateDir)
	now := time.Now().Format(time.RFC3339)
	state := sessionProductState{Path: root, DeclaredAt: now, Principal: id.Principal, SessionKey: id.SessionKey}
	if err := writeStateJSON(sessionProductPath(dir, id), state); err != nil {
		fmt.Print("ОТКАЗ: не записан файл продукта: " + err.Error() + lineEnding)
		return 1
	}
	mode := sessionModeState{Enabled: true, Principal: id.Principal, SessionKey: id.SessionKey, UpdatedAt: now}
	if err := writeStateJSON(sessionModePath(dir, id), mode); err != nil {
		fmt.Print("ОТКАЗ: не записан файл режима: " + err.Error() + lineEnding)
		return 1
	}
	_ = os.Remove(sessionOffPath(dir, id))
	fmt.Printf("продукт сессии %s: %s"+lineEnding, id.namespace(), root)
	return 0
}

// cmdSessionStatus — режим, продукт и (если сессия выключена) слова ЛПР дословно, как
// записаны, читаются здесь же — той же командой, что и пишет.
func cmdSessionStatus(argv []string) int {
	fs, principal, sessionKey, stateDir := sessionFlags("session status")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	id, err := parseIdentity(*principal, *sessionKey)
	if err != nil {
		fmt.Print("ОТКАЗ: " + err.Error() + lineEnding)
		return 1
	}
	dir := resolveStateDir(*stateDir)

	if off, err := readSessionOff(dir, id); err == nil {
		fmt.Printf("сессия %s выключена словом ЛПР: «%s»; с %s"+lineEnding, id.namespace(), off.Words, off.At)
		return 0
	}

	var mode sessionModeState
	if readJSON(sessionModePath(dir, id), &mode) != nil {
		fmt.Printf("режим выключен для сессии %s (файла состояния нет)"+lineEnding, id.namespace())
		return 0
	}
	state := "выключен"
	if mode.Enabled {
		state = "ВКЛЮЧЁН"
	}
	fmt.Printf("режим: %s; сессия %s; изменён %s"+lineEnding, state, id.namespace(), mode.UpdatedAt)

	var prod sessionProductState
	if readJSON(sessionProductPath(dir, id), &prod) == nil {
		fmt.Printf("продукт: %s; объявлен %s"+lineEnding, prod.Path, prod.DeclaredAt)
	} else {
		fmt.Print("продукт: НЕ ОБЪЯВЛЕН" + lineEnding)
	}
	return 0
}

func readSessionOff(dir string, id sessionIdentity) (sessionOffState, error) {
	var off sessionOffState
	err := readJSON(sessionOffPath(dir, id), &off)
	return off, err
}

// wordsFlags — гейт ЛПР пишется дословно: не пересказом, не решением сессии о себе.
// Минимум шесть знаков — то же правило, что держал mode.ps1 (-WorkerOff/-WorkerOn).
func sessionExitWords(fs *flag.FlagSet) *string {
	return fs.String("words", "", "слова ЛПР дословно (выход/возврат сессии из работы)")
}

// cmdSessionOff — выход сессии из работы словом ЛПР, дословно. Продукт сессии снимается:
// выключенная сессия не должна оставаться объявленной как чей-то рабочий продукт.
func cmdSessionOff(argv []string) int {
	fs, principal, sessionKey, stateDir := sessionFlags("session off")
	words := sessionExitWords(fs)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	id, err := parseIdentity(*principal, *sessionKey)
	if err != nil {
		fmt.Print("ОТКАЗ: " + err.Error() + lineEnding)
		return 1
	}
	w := strings.TrimSpace(*words)
	if utf8.RuneCountInString(w) < 6 {
		fmt.Print("ОТКАЗ: слов ЛПР нет или в них меньше шести знаков. Выход без слов ЛПР — не выход." + lineEnding)
		return 1
	}
	dir := resolveStateDir(*stateDir)
	off := sessionOffState{Words: w, At: time.Now().Format(time.RFC3339), Principal: id.Principal, SessionKey: id.SessionKey}
	if err := writeStateJSON(sessionOffPath(dir, id), off); err != nil {
		fmt.Print("ОТКАЗ: не записан файл выхода: " + err.Error() + lineEnding)
		return 1
	}
	_ = os.Remove(sessionProductPath(dir, id))
	fmt.Printf("air-worker для сессии %s выключен словом ЛПР: «%s». Продукт сессии снят."+lineEnding, id.namespace(), w)
	return 0
}

// cmdSessionOn — возврат сессии в работу, тем же правилом дословных слов ЛПР. Продукт
// сессией объявляется заново командой `session declare` — возврат его не восстанавливает
// молча, ровно как и mode.ps1 не восстанавливал.
func cmdSessionOn(argv []string) int {
	fs, principal, sessionKey, stateDir := sessionFlags("session on")
	words := sessionExitWords(fs)
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	id, err := parseIdentity(*principal, *sessionKey)
	if err != nil {
		fmt.Print("ОТКАЗ: " + err.Error() + lineEnding)
		return 1
	}
	w := strings.TrimSpace(*words)
	if utf8.RuneCountInString(w) < 6 {
		fmt.Print("ОТКАЗ: слов ЛПР нет или в них меньше шести знаков. Возврат без слов ЛПР — не возврат." + lineEnding)
		return 1
	}
	dir := resolveStateDir(*stateDir)
	_ = os.Remove(sessionOffPath(dir, id))
	fmt.Printf("air-worker для сессии %s снова включён словом ЛПР: «%s». Продукт объявляется заново: air-worker session declare -product <корень> -principal %s -session-key %s"+lineEnding,
		id.namespace(), w, id.Principal, id.SessionKey)
	return 0
}

func cmdSessionForget(argv []string) int {
	fs, principal, sessionKey, stateDir := sessionFlags("session forget")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	id, err := parseIdentity(*principal, *sessionKey)
	if err != nil {
		fmt.Print("ОТКАЗ: " + err.Error() + lineEnding)
		return 1
	}
	dir := resolveStateDir(*stateDir)
	if os.Remove(sessionProductPath(dir, id)) == nil {
		fmt.Print("продукт сессии снят" + lineEnding)
	} else {
		fmt.Print("продукт сессии не был объявлен" + lineEnding)
	}
	return 0
}
