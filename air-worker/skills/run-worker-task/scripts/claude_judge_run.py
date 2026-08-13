#!/usr/bin/env python3
r"""claude_judge_run.py — обёртка для уровней 4-5 ladder.py (Ф3).

`dispatcher.run_external` зовёт `params["cmd"]` как argv-список без shell —
там нет `< promptfile`. Промпт может быть длинным, а через `--cmd` его в
командную строку надёжно не запихнёшь (экранирование). Поэтому: промпт
пишется во временный файл заранее (это делает ladder.py), а сюда передаётся
только путь к нему — короткий и безопасный аргумент.

Использование:
    claude_judge_run.py <model> <prompt_file>

Печатает в stdout СЫРОЙ JSON-ответ claude.exe (--output-format json) —
разбирает его уже `_parse_claude_json()` в ladder.py.
"""
from __future__ import annotations

import os
import subprocess
import sys

CLAUDE_EXE = os.environ.get(
    "CLAUDE_JUDGE_EXE",
    r"C:\Users\admin_loc\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\bin\claude.exe",
)


def main() -> int:
    if len(sys.argv) != 3:
        print("использование: claude_judge_run.py <model> <prompt_file>", file=sys.stderr)
        return 2
    model, prompt_path = sys.argv[1], sys.argv[2]
    with open(prompt_path, encoding="utf-8") as fh:
        prompt = fh.read()
    proc = subprocess.run(
        [CLAUDE_EXE, "-p", "--model", model, "--output-format", "json", prompt],
        capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=300)
    sys.stdout.write(proc.stdout)
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr)
    return proc.returncode


if __name__ == "__main__":
    sys.exit(main())
