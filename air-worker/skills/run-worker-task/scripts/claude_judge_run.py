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
import shutil
import subprocess
import sys

def _resolve_claude_exe() -> str:
    r"""Инструмент ищется ПО ОТВЕТУ, а не по прибитому пути.

    Прежняя редакция держала умолчанием путь npm-установки
    (AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\bin\claude.exe).
    Замер 12.09.2026: этого файла НЕ СУЩЕСТВУЕТ — клиент переехал на нативную
    установку в ~\.local\bin\claude.exe. То есть ступени 4-5 лестницы, где и живёт
    суждение, не могли исполниться вовсе: верх лестницы был оборван так же молча,
    как до того ступени 2-3.

    Порядок: явное перекрытие переменной → PATH → известные расположения. Ничего не
    найдено — отказ с НАЗВАННОЙ причиной, а не попытка запустить пустоту: «нечем
    исполнить» и «модель не справилась» — разные исходы, и путать их дорого.
    """
    explicit = os.environ.get("CLAUDE_JUDGE_EXE")
    if explicit:
        return explicit

    found = shutil.which("claude")
    if found:
        return found

    home = os.path.expanduser("~")
    for candidate in (
        os.path.join(home, ".local", "bin", "claude.exe"),
        os.path.join(home, ".local", "bin", "claude"),
        os.path.join(home, "AppData", "Roaming", "npm", "node_modules",
                     "@anthropic-ai", "claude-code", "bin", "claude.exe"),
    ):
        if os.path.isfile(candidate):
            return candidate

    raise FileNotFoundError(
        "claude не найден: ни в CLAUDE_JUDGE_EXE, ни в PATH, ни в известных "
        "расположениях. Это «нечем исполнить», а не отказ модели."
    )


def _environment_with_token() -> dict:
    r"""Окружение для подпроцесса, с токеном, который мог не доехать.

    Найдено живым прогоном 12.09.2026: claude.exe, запущенный ОЧЕРЕДЬЮ как внешний
    подпроцесс, отвечает «Not logged in», хотя файл учётных данных существует, а
    переменная CLAUDE_CODE_OAUTH_TOKEN задана на уровне пользователя. Причина не в
    секрете, а в ДОСТАВКЕ: процесс очереди был запущен ДО того, как переменную
    поставили, и своего окружения не перечитывает — новые переменные пользователя
    видят только процессы, стартовавшие после.

    Это тот же класс, что стоил трёх ходов в тот же день на отцепленных прогонах:
    токен был верен с первого раза, а до дочернего процесса не доходил. Поэтому
    здесь переменная берётся из реестра пользователя, если её нет в окружении, —
    и подставляется явно, а не в надежде на наследование.

    ЗНАЧЕНИЕ НЕ ПЕЧАТАЕТСЯ И НЕ ЛОЖИТСЯ В ЖУРНАЛ: протокол продукта по контракту
    исключает секреты, и диагностика здесь говорит только «взят/не найден».
    """
    env = dict(os.environ)
    if env.get("CLAUDE_CODE_OAUTH_TOKEN"):
        return env

    if sys.platform == "win32":
        try:
            import winreg
            with winreg.OpenKey(winreg.HKEY_CURRENT_USER, "Environment") as key:
                value, _ = winreg.QueryValueEx(key, "CLAUDE_CODE_OAUTH_TOKEN")
            if value:
                env["CLAUDE_CODE_OAUTH_TOKEN"] = value
                print("токен взят из окружения пользователя (в процессе его не было)",
                      file=sys.stderr)
        except OSError:
            # Переменной нет и там — это не наша беда: ниже claude сам скажет
            # «Not logged in», и отказ будет честным, а не замаскированным.
            pass
    return env


def main() -> int:
    if len(sys.argv) != 3:
        print("использование: claude_judge_run.py <model> <prompt_file>", file=sys.stderr)
        return 2
    model, prompt_path = sys.argv[1], sys.argv[2]
    with open(prompt_path, encoding="utf-8") as fh:
        prompt = fh.read()

    try:
        claude_exe = _resolve_claude_exe()
    except FileNotFoundError as exc:
        print(str(exc), file=sys.stderr)
        return 2

    env = _environment_with_token()

    # ПРОМПТ УХОДИТ ЧЕРЕЗ STDIN, А НЕ АРГУМЕНТОМ. Докстрока выше объясняет, почему его
    # нельзя класть в командную строку — «через --cmd надёжно не запихнёшь
    # (экранирование)», — и прежняя редакция клала его ровно туда, только уже в argv
    # самого claude. Длинное задание рвётся на пробелах и кавычках, и исполнитель
    # получает обрывок, не сообщая об этом: норма AUTO-080, требование 3, куплена
    # там дважды, а 12.09.2026 — в третий раз на отцепленных ветвях.
    proc = subprocess.run(
        [claude_exe, "-p", "--model", model, "--output-format", "json"],
        input=prompt, env=env,
        capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=300)
    sys.stdout.write(proc.stdout)
    if proc.returncode != 0:
        sys.stderr.write(proc.stderr)
    return proc.returncode


if __name__ == "__main__":
    sys.exit(main())
