package main

import "testing"

// СТРАЖ ОБХОДА проверяется НАБОРОМ КОМАНД: решение принимается по тексту (и по путям
// установки, параметру), значит и проверяется текстом. Каждая строка ниже — либо живой
// обход, который обязан быть пойман, либо законная команда, которую нельзя принять за
// обход.

// testInstallDir — каталог установки air-worker для правила 2: копия ПОВЕРХ него — обход,
// копия куда-то ещё (черновой каталог, сборка в bin/ рабочего дерева) — нет.
const testInstallDir = `C:\Users\u\AppData\Local\air-worker`

func TestСтражОбхода(t *testing.T) {
	dirs := []string{testInstallDir}
	bypass := []string{
		// 1) подмена маркетплейса локальным путём вместо GitHub owner/repo
		`claude plugin marketplace add F:\-8-\air-plugins`,
		`claude plugin marketplace add C:\x\air-plugins`,
		`claude plugin marketplace add ./air-plugins`,
		`claude plugin marketplace add ../air-plugins`,
		`claude plugin marketplace add /home/u/air-plugins`,
		// 2) копирование/перемещение в каталог установки или в кэш плагина (правило
		// сужено: раньше ловило любую команду с "air-worker.exe" в строке)
		`copy build\air-worker.exe "C:\Users\u\AppData\Local\air-worker\bin\air-worker.exe"`,
		`Copy-Item .\air-worker.exe C:\Users\u\AppData\Local\air-worker\bin\air-worker.exe`,
		`move air-worker.exe C:\Users\u\AppData\Local\air-worker\bin\air-worker.exe`,
		`copy build\air-worker.exe "C:\Users\u\.claude\plugins\cache\air-plugins\air-worker\0.10.1\bin\air-worker.exe"`,
		// 3) запуск air-worker.exe install не из кэша плагина
		`F:\-7-\air-worker\bin\air-worker.exe install`,
		`"F:\-7-\air-worker\bin\air-worker.exe" install -autostart`,
	}
	for _, c := range bypass {
		if ok, why := classifyBypass(c, dirs); !ok || why == "" {
			t.Errorf("обход не пойман: %q (ok=%v, why=%q)", c, ok, why)
		}
	}

	allowed := []string{
		`claude plugin marketplace add ruhorh66-rgb/air-plugins`,
		`claude plugin marketplace add https://github.com/ruhorh66-rgb/air-plugins`,
		`E:\-4-\claude-home\plugins\cache\air-plugins\air-worker\0.10.1\bin\air-worker.exe install`,
		`air-worker install`, // голое имя: путь неизвестен, обходом не объявляем
		`claude plugin update air-worker@air-plugins`,
		`air-worker judge -product .`,
		// копия бинарника из рабочего дерева в черновой каталог — не обход: ни
		// каталога установки, ни кэша плагина в пути нет.
		`copy F:\-7-\air-worker\bin\air-worker.exe F:\-7-\_worktrees\aw-step66\scratch\air-worker.exe`,
		// сборка выпуска в bin/ — не команда копирования/перемещения вовсе.
		`go build -o bin/air-worker.exe`,
		`git status`,
		``,
	}
	for _, c := range allowed {
		if ok, why := classifyBypass(c, dirs); ok {
			t.Errorf("законная команда принята за обход: %q (why=%q)", c, why)
		}
	}
}

// TestСтражОбходаПустыеКаталоги — без installDirs (nil) правило 2 не разваливается и не
// паникует: копия в каталог установки такой вызов уже не ловит (путей нет), но копия в
// кэш плагина по-прежнему ловится, потому что это отдельное условие.
func TestСтражОбходаПустыеКаталоги(t *testing.T) {
	cache := `copy build\air-worker.exe "C:\Users\u\.claude\plugins\cache\air-plugins\air-worker\0.10.1\bin\air-worker.exe"`
	if ok, why := classifyBypass(cache, nil); !ok || why == "" {
		t.Errorf("копия в кэш плагина обязана ловиться даже без installDirs: ok=%v why=%q", ok, why)
	}
	toInstallDir := `copy build\air-worker.exe "` + testInstallDir + `\bin\air-worker.exe"`
	if ok, _ := classifyBypass(toInstallDir, nil); ok {
		t.Error("без installDirs копию в каталог установки ловить нечем — это ожидаемо, не паника")
	}
}
