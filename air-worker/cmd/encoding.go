package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ПРАВИЛО О BOM — ОДНО, И ОНО ЗДЕСЬ, А НЕ В ГОЛОВАХ.
//
// Указание ЛПР 13.09.2026: «ты постоянно ссылаешься на BOM, что у нас ошибки — давай
// зашивать это в хуки». Основание прямое: за одни сутки эта беда стоила четырёх отказов,
// и каждый раз я записывала урок словами, а он не срабатывал в следующем месте.
//
// Правило РАЗНОЕ для разных типов файлов, и именно поэтому его нельзя помнить:
//
//	.ps1  — BOM ОБЯЗАТЕЛЕН. Без него Windows PowerShell 5.1 читает кириллицу как ANSI,
//	        разбор ломается, функции не определяются вовсе, а вызывающая проверка падает
//	        без внятной причины. Задания контура и хуки запускают именно 5.1.
//	.md   — BOM ЗАПРЕЩЁН. Харнесс не распознаёт открывающий «---» frontmatter и сообщает
//	        «No frontmatter block found»: имя и описание скила не доходят вовсе, а плагин
//	        при этом устанавливается и числится рабочим.
//	.json — BOM ЗАПРЕЩЁН. Разбор спотыкается на нём в большинстве читателей.
//	.go   — BOM ЗАПРЕЩЁН. Компилятор его не принимает.
//
// Одно правило на все типы было бы неверным в обе стороны — поэтому память тут особенно
// плоха: она помнит «про BOM», а не «про BOM у .md против .ps1».
//
// ЛОВУШКА ПОЧИНКИ, куплённая в тот же день: снять BOM декодированием и записать «без BOM»
// НЕДОСТАТОЧНО. UTF8.GetString оставляет U+FEFF первым СИМВОЛОМ строки, и запись без BOM
// пишет его снова, уже как текст. Байты отрезаются ДО декодирования — так и сделано ниже.

var utf8BOMBytes = []byte{0xEF, 0xBB, 0xBF}

// bomRequired — нужен ли файлу BOM. Второе значение: знаем ли мы правило для этого типа.
// Незнакомый тип НЕ ТРОГАЕТСЯ: молча «починить» чужой формат хуже, чем не тронуть.
func bomRequired(path string) (bool, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ps1", ".psm1", ".psd1":
		return true, true
	case ".md", ".json", ".go", ".yaml", ".yml", ".cmd", ".bat":
		return false, true
	}
	return false, false
}

type encodingVerdict struct {
	Path   string
	Known  bool
	Want   bool
	Has    bool
	Fixed  bool
	Reason string
}

func checkEncoding(path string, fix bool) (encodingVerdict, error) {
	v := encodingVerdict{Path: path}
	want, known := bomRequired(path)
	v.Known, v.Want = known, want
	if !known {
		return v, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return v, err
	}
	v.Has = bytes.HasPrefix(raw, utf8BOMBytes)
	if v.Has == want {
		return v, nil
	}
	if want {
		v.Reason = "нет BOM, а Windows PowerShell 5.1 без него прочитает кириллицу как ANSI и не определит функции"
		if fix {
			if err := os.WriteFile(path, append(append([]byte{}, utf8BOMBytes...), raw...), 0o644); err != nil {
				return v, err
			}
			v.Fixed = true
		}
		return v, nil
	}
	v.Reason = "есть BOM, а он здесь запрещён: frontmatter становится невидим, разбор спотыкается"
	if fix {
		// Байты отрезаются ДО декодирования — см. ловушку в шапке файла.
		if err := os.WriteFile(path, bytes.TrimPrefix(raw, utf8BOMBytes), 0o644); err != nil {
			return v, err
		}
		v.Fixed = true
	}
	return v, nil
}

func cmdEncoding(argv []string) int {
	fs := flag.NewFlagSet("encoding", flag.ContinueOnError)
	path := fs.String("path", "", "файл или каталог")
	fix := fs.Bool("fix", false, "исправить, а не только назвать")
	quiet := fs.Bool("quiet", false, "молча, только код возврата")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *path == "" {
		fmt.Fprint(os.Stderr, "укажи -path"+lineEnding)
		return 2
	}

	var targets []string
	st, err := os.Stat(*path)
	if err != nil {
		// Файла нет — это не нарушение правила. Хук зовётся и на записи, которой не было.
		return 0
	}
	if st.IsDir() {
		_ = filepath.Walk(*path, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				// Рантайм и чужие деревья не трогаем: правило наше, файлы там чужие.
				if info != nil && info.IsDir() {
					n := info.Name()
					if n == ".git" || n == ".woody" || n == "node_modules" || n == "third_party" {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if _, known := bomRequired(p); known {
				targets = append(targets, p)
			}
			return nil
		})
	} else {
		targets = []string{*path}
	}

	bad := 0
	for _, t := range targets {
		v, err := checkEncoding(t, *fix)
		if err != nil || !v.Known || v.Reason == "" {
			continue
		}
		bad++
		if *quiet {
			continue
		}
		if v.Fixed {
			fmt.Printf("исправлено: %s — %s"+lineEnding, v.Path, v.Reason)
		} else {
			fmt.Printf("[FAIL] %s — %s"+lineEnding, v.Path, v.Reason)
		}
	}
	if bad == 0 {
		if !*quiet {
			fmt.Printf("[PASS] кодировка верна у файлов: %d"+lineEnding, len(targets))
		}
		return 0
	}
	if *fix {
		return 0
	}
	return 1
}
