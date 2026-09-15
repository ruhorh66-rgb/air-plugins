package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Продукт, годный к работе: тот же контракт, что goals_test.go/planWithGoals, но с фактом в
// реестре, потому что productReady (cmd/session.go) — та же проверка, что `air-worker goals`.
func setupSessionProduct(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	plan := "**Ц1.** механизм не работает без целей\n\n" +
		"| Критерий | Цель | Признак достижения | Чем меряется |\n|---|---|---|---|\n" +
		"| К1 | Ц1 | план без целей не исполняется | факт `f01` |\n\n" +
		"| № | Шаг | Ступень | Судья |\n|---|---|---|---|\n" +
		"| 1 | работа | `sonnet` | К1: тест |\n"
	if err := os.WriteFile(filepath.Join(root, "PLAN.md"), []byte(plan), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "checklist.json"),
		[]byte(`{"items":[{"id":"f01","status":"completed"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "run-config.json"),
		[]byte(`{"judge":{"checklist":"checklist.json"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// К12: режим, продукт, гейт ЛПР дословно и выход сессии из работы пишутся и читаются
// командами бинарника; `mode.ps1` снят из этого пути — здесь нет ни одного вызова
// PowerShell, всё состояние идёт через cmdSession*.
func TestCriterion12Pending(t *testing.T) {
	root := setupSessionProduct(t)
	stateDir := t.TempDir()

	if code := cmdSessionDeclare([]string{"-product", root, "-principal", "claude", "-session-key", "sess-1", "-state-dir", stateDir}); code != 0 {
		t.Fatalf("declare: код %d, ожидался 0", code)
	}

	out, code := captureStdout(t, func() int {
		return cmdSessionStatus([]string{"-principal", "claude", "-session-key", "sess-1", "-state-dir", stateDir})
	})
	if code != 0 || !strings.Contains(out, "ВКЛЮЧЁН") || !strings.Contains(out, root) {
		t.Fatalf("status после declare: код %d, вывод %q", code, out)
	}

	// Гейт ЛПР — дословно, не пересказ.
	words := "ухожу на встречу, вернусь позже"
	if code := cmdSessionOff([]string{"-principal", "claude", "-session-key", "sess-1", "-words", words, "-state-dir", stateDir}); code != 0 {
		t.Fatalf("off: код %d, ожидался 0", code)
	}
	if _, err := os.Stat(sessionProductPath(stateDir, sessionIdentity{"claude", "sess-1"})); !os.IsNotExist(err) {
		t.Fatalf("off обязан снять продукт сессии, файл всё ещё есть: %v", err)
	}
	out, code = captureStdout(t, func() int {
		return cmdSessionStatus([]string{"-principal", "claude", "-session-key", "sess-1", "-state-dir", stateDir})
	})
	if code != 0 || !strings.Contains(out, words) {
		t.Fatalf("status после off не вернул слова ЛПР дословно: код %d, вывод %q", code, out)
	}

	// Слова короче шести знаков — не выход.
	if code := cmdSessionOff([]string{"-principal", "claude", "-session-key", "sess-1", "-words", "нет", "-state-dir", stateDir}); code == 0 {
		t.Fatal("off с короткими словами обязан отказать")
	}

	// Возврат тем же правилом; продукт объявляется заново той же командой.
	if code := cmdSessionOn([]string{"-principal", "claude", "-session-key", "sess-1", "-words", "возвращаюсь к работе", "-state-dir", stateDir}); code != 0 {
		t.Fatalf("on: код %d, ожидался 0", code)
	}
	if _, err := readSessionOff(stateDir, sessionIdentity{"claude", "sess-1"}); err == nil {
		t.Fatal("on обязан снять файл выхода")
	}
	if code := cmdSessionDeclare([]string{"-product", root, "-principal", "claude", "-session-key", "sess-1", "-state-dir", stateDir}); code != 0 {
		t.Fatalf("declare после on: код %d, ожидался 0", code)
	}
}

// К44: сессия и продукт объявляются host-neutral командой binary с явными principal и
// session key; governance не зависит от CLAUDE_CODE_SESSION_ID, adapters только передают
// проверяемую identity.
func TestCriterion44Pending(t *testing.T) {
	// Переменная харнесса Claude Code явно пуста — declare/status обязаны работать
	// неизменно: если бы код читал её, пустое значение сломало бы этот сценарий.
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")

	root := setupSessionProduct(t)
	stateDir := t.TempDir()

	// Явная identity обязательна: без неё — отказ, а не угаданное значение.
	if code := cmdSessionDeclare([]string{"-product", root, "-session-key", "sess-x", "-state-dir", stateDir}); code == 0 {
		t.Fatal("declare без -principal обязан отказать")
	}
	if code := cmdSessionDeclare([]string{"-product", root, "-principal", "claude", "-state-dir", stateDir}); code == 0 {
		t.Fatal("declare без -session-key обязан отказать")
	}

	// Два разных адаптера с разными identity на один и тот же продукт не сталкиваются:
	// у каждого свой namespace, свой файл, независимое состояние.
	if code := cmdSessionDeclare([]string{"-product", root, "-principal", "claude", "-session-key", "sess-a", "-state-dir", stateDir}); code != 0 {
		t.Fatalf("declare claude/sess-a: код %d", code)
	}
	if code := cmdSessionDeclare([]string{"-product", root, "-principal", "codex", "-session-key", "sess-b", "-state-dir", stateDir}); code != 0 {
		t.Fatalf("declare codex/sess-b: код %d", code)
	}

	idA := sessionIdentity{"claude", "sess-a"}
	idB := sessionIdentity{"codex", "sess-b"}
	if sessionProductPath(stateDir, idA) == sessionProductPath(stateDir, idB) {
		t.Fatal("разные identity обязаны получать разные файлы состояния")
	}
	var prodA, prodB sessionProductState
	if err := readJSON(sessionProductPath(stateDir, idA), &prodA); err != nil || prodA.Principal != "claude" || prodA.SessionKey != "sess-a" {
		t.Fatalf("файл claude/sess-a: %+v, %v", prodA, err)
	}
	if err := readJSON(sessionProductPath(stateDir, idB), &prodB); err != nil || prodB.Principal != "codex" || prodB.SessionKey != "sess-b" {
		t.Fatalf("файл codex/sess-b: %+v, %v", prodB, err)
	}

	// Выключение одной identity не задевает вторую — namespace держит их независимыми.
	if code := cmdSessionOff([]string{"-principal", "claude", "-session-key", "sess-a", "-words", "выхожу из сессии сейчас", "-state-dir", stateDir}); code != 0 {
		t.Fatalf("off claude/sess-a: код %d", code)
	}
	if _, err := os.Stat(sessionProductPath(stateDir, idB)); err != nil {
		t.Fatalf("off одной сессии снял продукт другой: %v", err)
	}

	// Governance не зовёт CLAUDE_CODE_SESSION_ID нигде в реализации identity/состояния —
	// проверено фактом исходного текста, а не только поведением.
	src, err := os.ReadFile("session.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), `os.Getenv("CLAUDE_CODE_SESSION_ID")`) {
		t.Fatal("session.go обязан не читать CLAUDE_CODE_SESSION_ID: identity передаётся адаптером, не угадывается")
	}
}
