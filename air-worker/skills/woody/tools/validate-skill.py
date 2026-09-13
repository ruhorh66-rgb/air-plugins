"""Проверка того, что скил вообще загрузится.

Повод конкретный. 11.09.2026 frontmatter скила был невалиден: в `description` стояло
двоеточие с пробелом, и YAML рвался на нём. Скил не загрузился бы ни разу — вместе со
всеми хуками, которые и есть механизм удержания режима. При этом НИЧЕГО не сообщалось:
ни ошибки, ни предупреждения. Механизм, который молча не работает, — тот самый класс
отказа, ради которого написан весь этот скил.

Проверяет три вещи:
  1. frontmatter разбирается как YAML;
  2. имя скила совпадает с именем каталога (иначе платформа его не найдёт);
  3. каждая команда каждого хука существует файлом.

Код возврата 0 — скил загрузится. Ненулевой — не загрузится, и причина названа.
"""

import io
import os
import sys

try:
    import yaml
except ImportError:
    print("ОТКАЗ: нет модуля yaml — проверить нечем, а не 'всё хорошо'")
    sys.exit(2)

HERE = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
PATH = os.path.join(HERE, "SKILL.md")

if not os.path.isfile(PATH):
    print("ОТКАЗ: нет %s" % PATH)
    sys.exit(1)

with io.open(PATH, "r", encoding="utf-8") as fh:
    text = fh.read()

if not text.startswith("---"):
    print("ОТКАЗ: файл не начинается с ---")
    sys.exit(1)

try:
    end = text.index("\n---", 3)
except ValueError:
    print("ОТКАЗ: frontmatter не закрыт")
    sys.exit(1)

try:
    data = yaml.safe_load(text[3:end])
except Exception as exc:
    print("ОТКАЗ: frontmatter не разобран как YAML")
    print("  %s" % exc)
    print("  чаще всего причина — двоеточие с пробелом в незакавыченном значении")
    sys.exit(1)

problems = []

name = data.get("name")
folder = os.path.basename(HERE)
if not name:
    problems.append("в frontmatter нет поля name")
elif name != folder:
    problems.append("name=%r не совпадает с именем каталога %r" % (name, folder))

hooks = data.get("hooks") or {}
declared = 0
for event, entries in hooks.items():
    for entry in entries or []:
        for handler in entry.get("hooks", []) or []:
            declared += 1
            cmd = handler.get("command")
            if not cmd:
                problems.append("%s: у обработчика нет command" % event)
            else:
                # command может быть обёрнут в литеральные кавычки ради оболочки
                # (см. SKILL.md, коммит 3df4195: путь с "\-N-\" иначе рассыпается в
                # cmd.exe) — на диске у файла кавычек в имени нет, проверять исходную
                # строку через isfile() значит всегда получать False.
                path = cmd[1:-1] if cmd.startswith('"') and cmd.endswith('"') else cmd
                if not os.path.isfile(path):
                    problems.append("%s: команды нет файлом — %s" % (event, cmd))

print("skill    : %s" % folder)
print("name     : %s" % name)
print("событий  : %d, обработчиков: %d" % (len(hooks), declared))

if problems:
    print("")
    print("СКИЛ НЕ ЗАГРУЗИТСЯ:")
    for p in problems:
        print("  - %s" % p)
    sys.exit(1)

print("")
print("скил загрузится: frontmatter валиден, имя совпадает, команды хуков на месте")
sys.exit(0)
