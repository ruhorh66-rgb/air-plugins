package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
)

// utf8BOM — единственная уступка прошлому. Файлы, написанные PowerShell 5.1, несут BOM;
// разбор JSON на нём спотыкается. Снимаем молча: наличие BOM — свойство того, кто писал,
// а не смысл содержимого. Сами пишем без BOM: в UTF-8 он не нужен и никогда не был.
var utf8BOM = utf8BOMBytes

func readJSON(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(bytes.TrimPrefix(raw, utf8BOM), v)
}

// exitCode — код возврата дочернего процесса.
//
// Нужен отдельной функцией, потому что ошибка запуска и ненулевой код — РАЗНЫЕ исходы, и
// путать их дорого: «нечем исполнить» и «не прошло» ведут к разным решениям. Процесс,
// который не стартовал вовсе, отдаёт здесь -1, и вызывающий обязан это различать.
func exitCode(cmd *exec.Cmd, err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee != nil && ee.ProcessState != nil {
		if code := ee.ExitCode(); code != 0 {
			return code
		}
	}
	if cmd != nil && cmd.ProcessState != nil {
		if code := cmd.ProcessState.ExitCode(); code != 0 {
			return code
		}
	}
	return -1
}

func intPtr(v int) *int { return &v }
