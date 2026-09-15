package main

// СТРАЖ ОБХОДА — ЧИСТАЯ ФУНКЦИЯ. По тексту команды оболочки она решает, обход это
// штатного пути установки или нет. Ничего не запускает и не читает диск: решение
// принимается только по самой строке (и параметру путей установки), поэтому его
// проверяют набором команд.
//
// Обходом считается ровно три вещи (решение ЛПР 14.09.2026, шаг 66):
//   1. `claude plugin marketplace add` с ЛОКАЛЬНЫМ путём вместо GitHub owner/repo;
//   2. копирование или перемещение файла В КАТАЛОГ УСТАНОВКИ air-worker либо в кэш
//      плагина (plugins\cache);
//   3. запуск `air-worker.exe install` НЕ из кэша плагина.
//
// Правило 2 сузили: раньше обходом считалась ЛЮБАЯ команда копирования или перемещения,
// где в строке встречается «air-worker.exe» — и под это подпадали законные команды:
// копия бинарника из рабочего дерева в черновой каталог, сборка выпуска в bin/,
// резервная копия. Обход — только когда путь ЦЕЛИ (или вообще команды) лежит под
// каталогом установки или под кэшем плагина: только туда копия действительно подменяет
// штатно поставленный файл.
//
// Пути каталога установки — ПАРАМЕТР, не константа и не вызов installHome(): функция
// остаётся чистой, а не привязанной к ОС или моменту установки.
//
// Подключение этой функции к событию хоста — шаг 38 (диспетчер хуков); здесь только
// само решение. В hooks.json на этом шаге ничего не добавляется.

import (
	"path/filepath"
	"regexp"
	"strings"
)

var (
	reMarketplaceAdd = regexp.MustCompile(`(?i)\bplugin\s+marketplace\s+add\s+(\S+)`)
	reOwnerRepo      = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	reCopyVerb       = regexp.MustCompile(`(?i)(^|[\s|&;])(copy|xcopy|robocopy|move|copy-item|move-item|cp|mv)([\s]|$)`)
	reAWInstall      = regexp.MustCompile(`(?i)("[^"]*air-worker\.exe"|\S*air-worker\.exe|air-worker)\s+install\b`)
)

// classifyBypass возвращает (обход, причина). Причина непустая только при обходе.
// installDirs — путь(и) каталога установки air-worker (например, %LOCALAPPDATA%\air-worker
// в разрешённом виде); пустые элементы и пустой срез допустимы — тогда правило 2 ловит
// только копии в кэш плагина.
func classifyBypass(cmdText string, installDirs []string) (bool, string) {
	s := strings.TrimSpace(cmdText)
	if s == "" {
		return false, ""
	}

	// 1) подмена маркетплейса локальным путём.
	if m := reMarketplaceAdd.FindStringSubmatch(s); m != nil {
		arg := strings.Trim(m[1], `"'`)
		if isLocalPathArg(arg) {
			return true, "подмена маркетплейса локальным путём: " + arg + " — источник обязан быть GitHub ruhorh66-rgb/air-plugins"
		}
		return false, ""
	}

	// 2) копирование/перемещение в каталог установки air-worker или в кэш плагина.
	if reCopyVerb.MatchString(s) {
		lowSlash := strings.ToLower(filepath.ToSlash(s))
		for _, dir := range installDirs {
			dir = strings.TrimSpace(dir)
			if dir == "" {
				continue
			}
			if strings.Contains(lowSlash, strings.ToLower(filepath.ToSlash(dir))) {
				return true, "копирование или перемещение в каталог установки air-worker (" + dir + ") — установка идёт только из кэша плагина"
			}
		}
		if strings.Contains(lowSlash, "plugins/cache") {
			return true, "копирование или перемещение в кэш плагина — установка идёт только штатным путём"
		}
	}

	// 3) запуск air-worker.exe install не из кэша плагина.
	if p, isInstall := airWorkerInstallInvocation(s); isInstall {
		if p != "" && !underPluginCache(p) {
			return true, "запуск air-worker.exe install не из кэша плагина: " + p
		}
	}

	return false, ""
}

// isLocalPathArg — аргумент `marketplace add` есть локальный путь, а не GitHub owner/repo.
// GitHub-форма `owner/repo` содержит один слэш и обе части словарные; всё остальное с
// разделителем пути, буквой диска, ведущими ./ ../ ~ / или обратным слэшем — локальный
// путь. URL http(s) не локальный (это удалённый источник, отдельный от каталога).
func isLocalPathArg(a string) bool {
	a = strings.Trim(a, `"'`)
	if a == "" {
		return false
	}
	if strings.Contains(a, `\`) {
		return true // путь Windows
	}
	if len(a) >= 2 && a[1] == ':' && isAlphaByte(a[0]) {
		return true // C:\... или C:/...
	}
	if strings.HasPrefix(a, "/") || strings.HasPrefix(a, "./") ||
		strings.HasPrefix(a, "../") || strings.HasPrefix(a, "~") || strings.HasPrefix(a, "file:") {
		return true
	}
	lo := strings.ToLower(a)
	if strings.HasPrefix(lo, "http://") || strings.HasPrefix(lo, "https://") {
		return false // удалённый URL — не локальный каталог
	}
	if reOwnerRepo.MatchString(a) {
		return false // GitHub owner/repo
	}
	// с разделителем, но не owner/repo и не URL — считаем путём
	return strings.Contains(a, "/")
}

func isAlphaByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

// airWorkerInstallInvocation — это ли запуск `... air-worker.exe install`, и путь к exe.
// Путь пустой означает «указано голым именем» (через PATH): откуда именно оно
// разрешится, из строки не видно, и такой случай обходом не объявляется.
func airWorkerInstallInvocation(s string) (string, bool) {
	m := reAWInstall.FindStringSubmatch(s)
	if m == nil {
		return "", false
	}
	p := strings.Trim(m[1], `"'`)
	if !strings.ContainsAny(p, `/\`) {
		return "", true // голое имя, путь неизвестен
	}
	return filepath.Clean(p), true
}
