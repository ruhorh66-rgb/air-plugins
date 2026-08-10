#!/usr/bin/env python3
"""air-ruflo-bridge: запрет обхода обвязки — механизмом, а не текстом в SKILL.md.

ПОЧЕМУ ЭТО В ПЛАГИНЕ, А НЕ В ГЛОБАЛЬНОМ ХУКЕ. Указание ЛПР 08.08.2026: «не скилл,
а плагин — это должна быть жёсткая конструкция для разных платформ». Правило,
живущее в `SKILL.md`, читает только Claude Code и только если сессия открыла скилл;
правило, живущее в глобальном `settings.json` контура, не поедет на другую машину
вместе с плагином. Конструкция, которая едет с плагином, работает в обоих случаях.

Глобальная проверка `vibecoding_guard.check_hive_direct` при этом остаётся: она
страхует сессии, у которых плагин не установлен. Совпадение сознательное, а не
недосмотр — два предупреждения дешевле одного пропуска.

ЧТО ЗАПРЕЩАЕТСЯ. Прямой запуск роя мимо `run_task.ps1`: `hive-mind spawn`,
`hive-mind task`, `autopilot enable`, `swarm start`. Основание — `ERR-2026-000192`:
прямой `hive-mind spawn --claude` поднимает Queen-сессию, но НЕ заводит воркеров и
НЕ кладёт задачу в очередь роя. Снаружи похоже на рой, фактически — одна дорогая
сессия. Именно так рой не выполнил ни одной задачи за всё время своей работы:
18 воркеров, все `idle`, `Completed = 0` у каждого.

ЧТО НЕ ЗАПРЕЩАЕТСЯ. Диагностика (`status`, `task-status`, `memory`), любые чтения,
и сам `run_task.ps1` — он и есть законный путь. Запрет на исполнение, не на знание.

ТИП — ЗАПРЕТ, не сообщение (Модуль 5 § 10.2): цена пропуска здесь — прогон, который
выглядит роем и им не является, с оплаченными токенами и ложным отчётом. Цена
ложного срабатывания — одна команда, переписанная через обвязку.
"""
from __future__ import annotations

import json
import pathlib
import re
import subprocess
import sys

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

# Запуск роя. Диагностические подкоманды сюда намеренно не входят.
# `(?![-\w])` — иначе `hive-mind task-status` читается как запуск: граница слова стоит
# и перед дефисом. Хук блокировал ровно ту диагностику, которую его же текст объявляет
# разрешённой. Поймано на себе при разборе отказа прогона 08.08.2026.
LAUNCH = re.compile(r"hive-mind\s+(?:spawn|task)(?![-\w])|autopilot\s+enable\b|swarm\s+start\b",
                    re.I)
# Законный путь: команда идёт через обвязку.
WRAPPER = re.compile(r"run_task\.ps1", re.I)

# Оболочка исполняет команду не целиком, а кусками, разделёнными `;`, `&&`, `||`,
# `|` и переводом строки. Проверять надо КАЖДЫЙ кусок отдельно: образец, найденный
# в чужом куске, ничего не запускает.
SEGMENT_SPLIT = re.compile(r"[\n;|&]+")
# Чем движок вообще поднимается. Требование присутствия — единственное, что
# отличает запуск от упоминания: `grep "hive-mind task" файл` ИЩЕТ этот текст,
# `echo "hive-mind spawn"` его ПЕЧАТАЕТ, и ни одна из двух рой не заводит. Обе
# были отклонены 08.08.2026 и обе стоили круга на переписывание команды.
INVOKER = re.compile(r"(?:^|[\s\"'/\\])(?:node|npx|bunx|deno|claude-flow|ruflo)\b|cli\.js", re.I)
# Подкоманда движка первым словом куска — тоже запуск: зовут сам движок из PATH.
DIRECT = re.compile(r"^\s*(?:hive-mind|swarm|autopilot)\b", re.I)
# Справка ничего не поднимает. `hive-mind task --help` печатает список флагов —
# ровно та диагностика, которую текст запрета объявляет разрешённой, а проверка
# отклоняла, потому что читала подкоманду и не читала остаток куска.
HELP = re.compile(r"(?:^|\s)(?:--help|-h)(?:\s|$)")

DENY = (
    "air-ruflo-bridge: прямой запуск роя отклонён (ERR-2026-000192).\n"
    "`hive-mind spawn --claude` поднимает Queen-сессию, но НЕ заводит воркеров и НЕ "
    "кладёт задачу в очередь роя: снаружи похоже на рой, фактически одна дорогая "
    "сессия. Так рой не выполнил НИ ОДНОЙ задачи — 18 воркеров, все idle, Completed=0.\n"
    "Запуск состоит из четырёх шагов, и все четыре зашиты в обвязку:\n"
    "  hive-mind spawn -n <N>  ->  hive-mind task -d  ->  autopilot enable  ->  spawn --claude\n"
    "Законный путь (гейт ЛПР, скан секретов, проверка приёмки по вызовам "
    "mcp__claude-flow__ включены):\n"
    "  skills/run-via-ruflo/scripts/run_task.ps1\n"
    "Диагностика (status, task-status, memory) под запрет не попадает."
)


def read_payload() -> dict:
    """Вход хука. Любой мусор на stdin — не повод уронить чужую сессию."""
    try:
        data = json.loads(sys.stdin.read() or "{}")
    except (ValueError, OSError):
        return {}
    return data if isinstance(data, dict) else {}


# Установка версии плагина. Ловим и update, и install — оба доводят версию до сессий.
PLUGIN_INSTALL = re.compile(
    r"\bclaude\s+plugins?\s+(?:update|install|i)\s+([A-Za-z0-9._-]+)(?:@([A-Za-z0-9._-]+))?",
    re.I)
MARKETPLACES = pathlib.Path.home() / ".claude" / "plugins" / "known_marketplaces.json"


def _local_marketplaces(only: str | None = None) -> list[pathlib.Path]:
    """Каталоги маркетплейсов, лежащие на диске. Чужие github-плагины не наши.

    `only` — имя маркетплейса из команды (`plugin@market`). Сужать по нему
    обязательно: одно и то же имя плагина встречается в контуре дважды, и первая
    редакция нашла брошенную копию `legal-analysis` 1.0.0 в `E:\\-7-` вместо живой
    1.2.0 в `E:\\-8-`. Проверка судила бы о теге по чужому каталогу.
    """
    try:
        data = json.loads(MARKETPLACES.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return []
    roots = []
    for key, entry in (data.get("marketplaces") or data).items():
        if only and key != only:
            continue
        source = (entry or {}).get("source") or {}
        if source.get("source") == "directory" and source.get("path"):
            roots.append(pathlib.Path(source["path"]))
    return roots


def _plugin_source(name: str, market: str | None = None) -> tuple[pathlib.Path, str] | None:
    """(каталог плагина, версия) по имени — только для локальных источников.

    Маркетплейс в команде не назван и совпадений больше одного — НЕ судим: блокировать
    по догадке дороже, чем пропустить. Такое бывает при брошенных копиях плагина
    (`legal-analysis` лежит и в `E:\\-7-`, и в `E:\\-8-`).
    """
    if market is None:
        matches = []
        for root in _local_marketplaces():
            if not root.is_dir():
                continue
            # Корень тоже: `air-storage` — сам себе плагин, манифест лежит в корне
            # репозитория (`source: "./"`). Без этого проверка не видела его вовсе и
            # молча пропускала установку без тега — поймано на настоящем релизе.
            for manifest in [root / ".claude-plugin" / "plugin.json"] + \
                    list(root.glob("*/.claude-plugin/plugin.json")) + \
                    list(root.glob("*/*/.claude-plugin/plugin.json")):
                if not manifest.is_file():
                    continue
                try:
                    data = json.loads(manifest.read_text(encoding="utf-8"))
                except (OSError, ValueError):
                    continue
                if data.get("name") == name and data.get("version"):
                    matches.append((manifest.parent.parent, str(data["version"])))
        if len(matches) != 1:
            return None
        return matches[0]
    for root in _local_marketplaces(market):
        if not root.is_dir():
            continue
        # Корень тоже — см. пояснение выше: манифест плагина, который сам себе
        # репозиторий, лежит не в подпапке.
        for manifest in [root / ".claude-plugin" / "plugin.json"] + \
                list(root.glob("*/.claude-plugin/plugin.json")) + \
                list(root.glob("*/*/.claude-plugin/plugin.json")):
            if not manifest.is_file():
                continue
            try:
                data = json.loads(manifest.read_text(encoding="utf-8"))
            except (OSError, ValueError):
                continue
            if data.get("name") == name and data.get("version"):
                return manifest.parent.parent, str(data["version"])
    return None


def check_release_tag(command: str) -> str | None:
    """Версия не ставится сессиям, пока у неё нет тега релиза.

    ЗАЧЕМ. Правило «релиз — это тег, а стабильная точка называется явно» записано в
    AIR_VIBECODING § «Разработка не мешает эксплуатации». Оно существовало и не
    соблюдалось: на 08.08.2026 в контуре не было НИ ОДНОГО тега релиза ни у одного
    плагина — и когда `air-comms 1.1.0` остановил разбор почты, откатываться оказалось
    некуда. Сломали за минуту, чинили часами (ERR-2026-000206).

    Указание ЛПР: раз правило не соблюдали — сделать так, чтобы отклониться было
    нельзя. Проверка ровно об этом: тег обязан существовать ДО того, как версия
    доедет до сессий.

    Замена существует и штатная — `claude plugin tag`, она же сверяет манифест с
    записью маркетплейса. Это не изобретение обходного пути, а требование сделать
    один шаг, который и так предусмотрен инструментом.

    ЧУЖИЕ ПЛАГИНЫ НЕ ТРОГАЕМ: ponytail, codex, claude-video приходят из github, их
    релизами мы не управляем, и требовать от них наш тег бессмысленно.
    """
    match = PLUGIN_INSTALL.search(command)
    if not match:
        return None
    name, market = match.group(1), match.group(2)
    found = _plugin_source(name, market)
    if not found:
        return None  # источник не локальный либо маркетплейс чужой — не наш релиз
    path, version = found
    tag = f"{name}--v{version}"
    try:
        res = subprocess.run(["git", "-C", str(path), "tag", "-l", tag],
                             capture_output=True, text=True, timeout=15)
    except Exception:
        return None  # git недоступен — не выдумываем находку
    if res.returncode != 0 or res.stdout.strip():
        return None
    return (
        f"air-ruflo-bridge: у версии {version} нет тега релиза — установка отклонена.\n"
        f"Правило AIR_VIBECODING § «Разработка не мешает эксплуатации»: релиз — это ТЕГ, "
        f"а не поднятый номер. Без тега нет точки отката, и поломка чинится часами "
        f"вместо секунд (ERR-2026-000206: так встал разбор почты).\n"
        f"Сначала пометить релиз — команда штатная, она же сверит манифест с "
        f"маркетплейсом:\n"
        f'  claude plugin tag "{path}" -m "чем эта версия подтверждена" --push\n'
        f"В сообщении тега писать НЕ «что изменилось», а ЧЕМ ПОДТВЕРЖДЕНА: на каком "
        f"живом потоке проверено. «Собралось и самопроверка прошла» подтверждением не "
        f"является — сегодняшний хук проходил самопроверку при остановленном контуре."
    )


def strip_data(command: str) -> str:
    """Убрать из команды то, что является ДАННЫМИ, а не командой.

    Тело heredoc, текст сообщения (`-m "..."`, `-F -`) и программа, поданная
    строкой (`-c "..."`), исполнению ОБОЛОЧКОЙ не подлежат: это содержимое,
    которое передаётся программе на вход. Проверка без этого запретила
    собственный `git commit`, в сообщении которого описан тот самый порядок
    запуска, ради запрета которого хук и написан. Ложное срабатывание у
    блокирующей проверки стоит дороже пропуска — оно ломает работу, а не напоминает.

    Разбор `-c` добавлен по журналу 08.08.2026: отклонены были и `python -c` с
    таблицей тестовых команд внутри, и `python -c`, поднимающий сам этот хук для
    проверки. Обе строки — данные для питона, а не команды для оболочки. Это
    осознанно оставляет щель для `python -c "os.system('… hive-mind spawn …')"`:
    хук защищает от невнимательности, а не от обхода — текст хода и вызовы пишет
    один исполнитель, и обходить самого себя ему незачем.
    """
    # Открывающая строка heredoc продолжается после разделителя: реальный отказ
    # пришёл на `git commit -F - <<'MSG' && git push …`, где сразу за `MSG` стоял
    # не перевод строки, а `&&`, и тело не вырезалось вовсе.
    without = re.sub(r"<<\s*'?\"?(\w+)'?\"?[^\n]*\n.*?\n\1", " ", command, flags=re.DOTALL)
    without = re.sub(r"-m\s+(['\"])(?:\\.|(?!\1).)*\1", " ", without, flags=re.DOTALL)
    without = re.sub(r"-c\s+(['\"])(?:\\.|(?!\1).)*\1", " ", without, flags=re.DOTALL)
    return without


def launches(command: str) -> bool:
    """Есть ли в команде кусок, который ДЕЙСТВИТЕЛЬНО поднимает рой.

    Прежняя редакция искала образец по всей строке сразу. Из девяти отказов,
    вынутых из журнала сессии, восемь оказались ложными именно поэтому: образец
    лежал в тексте `echo`, в шаблоне `grep`, в теле сообщения коммита или в
    строке внутри `python -c`. Кусок команды признаётся запуском, только если в
    нём есть чем запускать (`node`, `npx`, `cli.js`, …) либо подкоманда движка
    стоит первым словом — и если это не запрос справки.
    """
    for segment in SEGMENT_SPLIT.split(command):
        if not LAUNCH.search(segment) or HELP.search(segment):
            continue
        if INVOKER.search(segment) or DIRECT.match(segment):
            return True
    return False


def check(command: str) -> str | None:
    if not command:
        return None
    payload = strip_data(command)
    note = check_release_tag(payload)
    if note:
        return note
    if not launches(payload) or WRAPPER.search(payload):
        return None
    return DENY


def main() -> int:
    payload = read_payload()
    tool_input = payload.get("tool_input")
    tool_input = tool_input if isinstance(tool_input, dict) else {}
    note = check(str(tool_input.get("command") or ""))
    if note:
        print(json.dumps({
            "hookSpecificOutput": {
                "hookEventName": "PreToolUse",
                "permissionDecision": "deny",
                "permissionDecisionReason": note,
            }
        }, ensure_ascii=False))
    return 0


def _selftest() -> None:
    assert check('node cli.js hive-mind spawn --claude -o "цель"')
    assert check("npx claude-flow autopilot enable")
    assert check("hive-mind task -d 'задача' -p high")
    # Законный путь и диагностика молчат.
    assert check('& "…/scripts/run_task.ps1" -Objective x -TargetPath y') is None
    assert check("node cli.js hive-mind status") is None
    assert check("git status") is None
    assert check("") is None
    # Данные, а не команда: описание запрета не есть его нарушение.
    assert check("git commit -m 'порядок: hive-mind spawn -n 5 затем autopilot enable'") is None, \
        "текст сообщения коммита исполнению не подлежит"
    assert check("git commit -F - <<MSG\nhive-mind spawn --claude в обход обвязки\nMSG") is None, \
        "тело heredoc — содержимое, а не команда"
    # …но настоящий запуск рядом с heredoc всё равно ловится.
    assert check("cat <<TXT\nпросто текст\nTXT\nnode cli.js hive-mind spawn --claude -o x"), \
        "команда после heredoc проверяется как обычно"

    # Классы ложных срабатываний, вынутые из журнала сессии (TASK-OBS-0044).
    # Общее у всех: образец найден там, где он СОДЕРЖИМОЕ, а не исполняемая команда.
    assert check("node cli.js hive-mind task --help") is None, \
        "справка печатает флаги и ничего не поднимает"
    assert check('grep -n "hive-mind task\\|Submit task" отчёт.md | head -30') is None, \
        "поисковый шаблон grep — данные"
    assert check('ls .; echo "=== документация по hive-mind task ==="; find . -name "*.md"') \
        is None, "текст echo — данные"
    assert check("python -c \"print('node cli.js hive-mind task -d x')\"") is None, \
        "строка внутри python -c — данные для питона, не команда для оболочки"
    assert check("git add -A && git commit -F - <<'MSG' && git push -q origin main\n"
                 "порядок: hive-mind spawn -n 5, затем autopilot enable\nMSG") is None, \
        "тело heredoc не вырезалось, когда за разделителем шёл не перевод строки, а &&"

    # Требование тега релиза — на настоящем состоянии контура, а не на выдумке.
    assert check_release_tag("git status") is None
    assert check_release_tag("claude plugin list") is None
    assert check_release_tag('claude plugin tag hr -m "проверено" --push') is None, \
        "тегирование не должно блокироваться — это и есть законный путь"
    assert check_release_tag("claude plugin update ponytail@ponytail") is None, \
        "чужой github-плагин не наш релиз"
    if _plugin_source("air-ruflo-bridge", "air-plugins"):
        # Свой же плагин: если тег на текущую версию есть — молчим, нет — запрещаем.
        _path, _version = _plugin_source("air-ruflo-bridge", "air-plugins")
        _tagged = subprocess.run(
            ["git", "-C", str(_path), "tag", "-l", f"air-ruflo-bridge--v{_version}"],
            capture_output=True, text=True).stdout.strip()
        _note = check_release_tag("claude plugin update air-ruflo-bridge@air-plugins")
        assert (_note is None) == bool(_tagged), \
            "решение проверки обязано совпадать с фактическим наличием тега"
    # Хук не должен падать ни на каком входе.
    assert main() is not None or True
    # Корпус из журнала — часть самопроверки, а не отдельный ритуал: иначе он
    # проживёт ровно до первой сессии, которая о нём не вспомнит.
    corpus = pathlib.Path(__file__).with_name("test_guard_corpus.py")
    if corpus.is_file():
        done = subprocess.run([sys.executable, str(corpus)], capture_output=True, text=True,
                              encoding="utf-8", errors="replace")
        assert done.returncode == 0, "корпус отказов не пройден:\n" + (done.stdout or "")
        print((done.stdout or "").strip().splitlines()[-2])
    print("selftest ok")


if __name__ == "__main__":
    if "--selftest" in sys.argv:
        _selftest()
    else:
        sys.exit(main())
