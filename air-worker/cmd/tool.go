package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ПОИСК ИСПОЛНИТЕЛЯ — ОДНОЙ ФУНКЦИЕЙ, как и в PowerShell-части продукта.
//
// Это третий раз, когда один и тот же дефект чинится в этом продукте, и второй, когда
// я вношу его сама. Хроника короткая и поучительная:
//
//  1. 12.09.2026 — верх лестницы звал claude по прибитому адресу npm-установки, которой
//     на машине нет: ступени 4-5 не могли исполниться вовсе, а метрика «100% закрытых на
//     нулевом уровне» читалась как дешевизна.
//  2. 13.09.2026 — check-ladder-reachable брал интерпретатор по имени без пробы: на второй
//     машине это давало алиас-заглушку магазина и рушило три утверждения сразу.
//  3. 13.09.2026, тот же день, уже после обоих разборов — перенося петлю в бинарник, я
//     написала exec.Command("claude") без единого запасного пути, хотя в планировщике
//     рядом запасной путь есть. На машине AIR-ENV-002 claude лежит в AppData и на PATH
//     его нет: петля объявила бы «исполнителя нет на машине» при наличном исполнителе.
//
// Отсюда правило, которое дороже любой из трёх починок: правило, живущее уроком, чинится
// в месте находки и уцелевает в следующем месте. Поэтому — общая функция, и обе точки
// вызова обязаны ходить через неё.
//
// Порядок: явное перекрытие переменной -> PATH -> известные расположения. Не нашли —
// отказ с НАЗВАННОЙ причиной и перечнем испробованного. «Нечем исполнить» и «модель не
// справилась» — разные исходы, и путать их дорого: на втором петля поднимает ступень и
// платит за ту же ошибку дороже.
func resolveRunnerTool(tool string) (string, error) {
	p, _, err := resolveRunnerToolWhy(tool)
	return p, err
}

// resolveRunnerToolWhy — то же, но называет ПРАВИЛО, которым найдено.
//
// Заведено по разбору AIR-ENV-002 13.09.2026: единственным способом узнать, каким
// исполнителем пойдёт петля, был запуск петли — то есть узнать цену можно было, только
// заплатив её. А для второй машины, которой у автора нет, это был вообще единственный
// способ подтвердить починку резолвера.
func resolveRunnerToolWhy(tool string) (string, string, error) {
	var tried []string

	// Явное перекрытие. Имя переменной историческое — её объявляют на машинах, где
	// клиент лежит не на PATH; менять его значило бы сломать уже объявленное.
	if tool == "claude" {
		if v := strings.TrimSpace(os.Getenv("CLAUDE_JUDGE_EXE")); v != "" {
			if _, err := os.Stat(v); err == nil {
				return v, "перекрытие CLAUDE_JUDGE_EXE", nil
			}
			tried = append(tried, "CLAUDE_JUDGE_EXE="+v+" — файла нет")
		}
	}
	if v := strings.TrimSpace(os.Getenv("AIR_" + strings.ToUpper(tool) + "_EXE")); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v, "перекрытие AIR_" + strings.ToUpper(tool) + "_EXE", nil
		}
		tried = append(tried, "AIR_"+strings.ToUpper(tool)+"_EXE="+v+" — файла нет")
	}

	if p, err := exec.LookPath(tool); err == nil {
		// РЕЗОЛВ НЕ ДОКАЗЫВАЕТ НАЛИЧИЯ, ДОКАЗЫВАЕТ ТОЛЬКО ОТВЕТ — но пробовать запуском
		// здесь нельзя: у claude и codex нет дешёвого безобидного вызова, а платный
		// «пробный» противоречил бы смыслу продукта. Поэтому ловим известную обманку по
		// признаку, а не по ответу: алиасы магазина в WindowsApps имеют нулевой размер.
		if !isStoreStub(p) {
			return p, "PATH", nil
		}
		tried = append(tried, p+" — алиас-заглушка магазина")
	} else {
		tried = append(tried, tool+" — не на PATH")
	}

	for _, cand := range knownToolLocations(tool) {
		if _, err := os.Stat(cand); err == nil {
			return cand, "известное расположение", nil
		}
		tried = append(tried, cand+" — нет")
	}

	return "", "", fmt.Errorf("'%s' не найден: %s", tool, strings.Join(tried, "; "))
}

func isStoreStub(p string) bool {
	if !strings.Contains(strings.ToLower(p), `\windowsapps\`) {
		return false
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return false
	}
	return fi.Size() == 0
}

// knownToolLocations — места, куда клиенты ставятся по умолчанию и откуда на PATH они
// попадают не всегда. Список растёт по фактам, а не по догадкам: каждая строка здесь
// появилась после живого отказа на конкретной машине.
func knownToolLocations(tool string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	switch tool {
	case "claude":
		var out []string
		out = append(out, filepath.Join(home, ".local", "bin", "claude"+ext))
		// AIR-ENV-002, 13.09.2026: клиент лежит в профиле по версии, на PATH его нет.
		// Каталог версии заранее неизвестен, поэтому берётся последний найденный.
		base := filepath.Join(home, "AppData", "Roaming", "Claude", "claude-code")
		if entries, err := os.ReadDir(base); err == nil {
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].IsDir() {
					out = append(out, filepath.Join(base, entries[i].Name(), "claude"+ext))
				}
			}
		}
		return out
	case "codex":
		return []string{filepath.Join(home, ".local", "bin", "codex"+ext)}
	}
	return nil
}

// cmdTool — сухой вывод выбора исполнителя. НИЧЕГО НЕ ЗАПУСКАЕТ И НЕ СТОИТ НИ КОПЕЙКИ.
//
// Заведено по разбору AIR-ENV-002 13.09.2026, и её формулировка — основание: «единственный
// способ узнать, каким исполнителем пойдёт петля, это запустить петлю; то есть узнать цену
// можно, только заплатив её».
//
// Второе основание тяжелее первого. Починку резолвера, сделанную под её машину, на её
// машине нечем было подтвердить: -whatif останавливается раньше выбора исполнителя,
// -dry-run тоже, а всё остальное зовёт модель. Подтверждение оставалось чтением кода — то
// есть ровно той валютой, которую мы весь день отказывались принимать.
func cmdTool(argv []string) int {
	fs := flag.NewFlagSet("tool", flag.ContinueOnError)
	which := fs.String("which", "", "какой исполнитель: claude, codex")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	names := []string{*which}
	if *which == "" {
		names = []string{"claude", "codex"}
	}
	worst := 0
	for _, n := range names {
		path, why, err := resolveRunnerToolWhy(n)
		if err != nil {
			// «Нечем исполнить» — это код 2, а не 1: работой оно не лечится, нужен человек.
			fmt.Printf("%-7s НЕ НАЙДЕН%s  %v"+lineEnding, n, lineEnding, err)
			worst = 2
			continue
		}
		fmt.Printf("%-7s %s"+lineEnding, n, path)
		fmt.Printf("        найден правилом: %s"+lineEnding, why)
	}
	return worst
}
