"""Какой движок считается действующим — ОДНО место на весь мост.

ЗАЧЕМ. 12.08.2026 контур обновился на 3.38.0, а прогон ушёл на 3.36.0: путь к движку
лежал константой в двух скриптах, и обновление кэша их не касалось. Снаружи это
выглядит как «обновились», а исполняется старое — тот же класс, что расхождение версии
плагина с установленной копией.

КАК УСТРОЕНО. Действующая версия объявляется файлом `engine.json` в корне роя:

    {"version": "3.38.0", "rollback": "3.36.0", "pinned_at": "2026-08-12", "why": "…"}

Путь по версии собирается сам: в каталоге пилота лежат кэши `.npm-cache-<версия>`, в
каждом — свой `cli.js`. Значит обновление движка это (1) поставить новый кэш,
(2) поменять ОДНО число в `engine.json`. Кода никто не правит, откат — вернуть прежнее
число, оно тут же и записано.

ФАЙЛА НЕТ — берётся самый свежий кэш, и это НАЗЫВАЕТСЯ вслух: молчаливый выбор версии
и есть то, из-за чего писался этот модуль.
"""
from __future__ import annotations

import glob
import json
import os
import re
import subprocess

PILOT = os.environ.get("RUFLO_PILOT") or r"E:\-4-\ruflo-pilot"
HIVE = os.environ.get("RUFLO_HIVE") or r"E:\-4-\ruflo-hive"
PIN = os.path.join(HIVE, "engine.json")

_CLI_TAIL = os.path.join("_npx", "*", "node_modules", "@claude-flow", "cli", "bin", "cli.js")


def _cli_for(version: str) -> str | None:
    """Путь к cli.js внутри кэша конкретной версии. Хэш-каталог `_npx` ищется маской:
    он меняется от версии к версии и запоминать его негде."""
    found = glob.glob(os.path.join(PILOT, f".npm-cache-{version}", _CLI_TAIL))
    return found[0] if found else None


def _all_versions() -> list[tuple[tuple[int, ...], str, str]]:
    """Все установленные версии: (ключ сортировки, версия, путь). Сортировка по числам,
    а не по строке: иначе 3.9 окажется старше 3.36."""
    out = []
    for path in glob.glob(os.path.join(PILOT, ".npm-cache-*", _CLI_TAIL)):
        match = re.search(r"\.npm-cache-([0-9][^\\/]*)", path)
        if not match:
            continue
        version = match.group(1)
        key = tuple(int(p) if p.isdigit() else 0 for p in version.split("."))
        out.append((key, version, path))
    return sorted(out, reverse=True)


def resolve() -> dict:
    """Действующий движок: путь, версия, откуда решение и чем откатываться."""
    if os.path.isfile(PIN):
        try:
            pin = json.load(open(PIN, encoding="utf-8"))
        except (OSError, ValueError) as exc:
            return {"cli": None, "version": None, "source": f"engine.json нечитаем: {exc}"}
        version = str(pin.get("version") or "")
        cli = _cli_for(version)
        if not cli:
            # Объявленной версии на диске нет — это отказ, а не повод взять другую:
            # «взял что нашлось» и есть тот самый молчаливый выбор.
            return {"cli": None, "version": version, "rollback": pin.get("rollback"),
                    "source": f"engine.json называет {version}, но кэша .npm-cache-{version} нет"}
        return {"cli": cli, "version": version, "rollback": pin.get("rollback"),
                "source": f"engine.json (закреплено {pin.get('pinned_at', 'дата не указана')})"}

    versions = _all_versions()
    if not versions:
        return {"cli": None, "version": None, "source": f"в {PILOT} нет ни одного кэша движка"}
    _, version, cli = versions[0]
    return {"cli": cli, "version": version, "rollback": None,
            "source": f"engine.json НЕТ — взята самая свежая из установленных ({version}); "
                      f"закрепить: записать {PIN}"}


def cli_path() -> str:
    """Путь к движку либо внятный отказ. Молча вернуть None нельзя: вызывающий соберёт
    команду с пустым путём и получит невнятную ошибку на три экрана."""
    got = resolve()
    if not got["cli"]:
        raise SystemExit(f"движок не определён: {got['source']}")
    return got["cli"]


def link_booster() -> str:
    """Дать движку УВИДЕТЬ agentic-flow — нулевой уровень маршрутизации, Agent Booster.

    Три недели он числился «не установлен». На самом деле установлен дважды: рабочая
    копия в корне роя и своя, битая, внутри npx-кэша движка. Движок импортирует
    `agentic-flow` из СВОЕГО каталога (ESM резолвит от файла, а не от cwd), натыкался
    на непостроенные нативные модули своей копии и падал в `ERR_DLOPEN_FAILED`, что в
    выводе выглядело безобидным «agentic-flow not available (using fallbacks)».

    Лечится связкой каталогов: копия движка заменяется junction'ом на рабочую. Делается
    здесь, а не руками, потому что npx-кэш у каждой версии свой — после обновления
    движка ссылки снова нет, и Booster тихо гаснет.
    """
    # …\_npx\<hash>\node_modules\@claude-flow\cli\bin\cli.js → …\<hash>\node_modules
    mods = cli_path()
    for _ in range(4):
        mods = os.path.dirname(mods)
    src = os.path.join(HIVE, "node_modules", "agentic-flow")
    dst = os.path.join(mods, "agentic-flow")
    if not os.path.isdir(src):
        return f"agentic-flow нет в {src} — Booster остаётся выключенным"
    if os.path.islink(dst) or (os.path.isdir(dst) and os.path.realpath(dst) == os.path.realpath(src)):
        return "связан"
    if os.path.isdir(dst):
        return (f"в кэше движка лежит СВОЯ копия {dst} — не трогаю автоматически: "
                f"переименовать её и повторить (junction ставится на {src})")
    # Junction, а не symlink: symlink на Windows требует привилегии, junction — нет.
    done = subprocess.run(["cmd", "/c", "mklink", "/J", dst, src],
                          capture_output=True, text=True)
    if done.returncode != 0:
        return f"связать не удалось: {(done.stderr or done.stdout).strip()[:200]}"
    return f"связан: {dst} -> {src}"


def sync_mcp() -> str:
    """Привести `.mcp.json` в корне роя к действующему движку.

    Второе место, где путь жил руками. 12.08.2026 там лежала 3.34.0, тогда как спавн
    шёл 3.36.0: рой КООРДИНИРОВАЛСЯ одной версией, а исполнялся другой. Обновление
    движка не должно требовать правки этого файла — иначе `engine.json` перестаёт быть
    единственным источником ровно так же, как раньше перестали быть им константы.

    Заодно чинится имя сервера: движок в своём промпте требует `mcp__ruflo__*`, а
    префикс задаётся именем регистрации (ERR-2026-000228).
    """
    path = os.path.join(HIVE, ".mcp.json")
    if not os.path.isfile(path):
        return f"{path} нет — регистрация MCP не выполнена"
    cfg = json.load(open(path, encoding="utf-8"))
    servers = cfg.setdefault("mcpServers", {})
    server = servers.pop("claude-flow", None) or servers.get("ruflo")
    if server is None:
        return f"{path}: сервера движка нет ни под именем ruflo, ни под claude-flow"
    servers["ruflo"] = server
    cli = cli_path()
    was = [a for a in server.get("args", []) if a.endswith("cli.js")]
    server["args"] = [cli if a.endswith("cli.js") else a for a in server.get("args", [])]
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(cfg, fh, ensure_ascii=False, indent=2)
        fh.write("\n")
    if was and was[0] == cli:
        return "совпадает"
    return f"обновлено: {was[0] if was else '—'} -> {cli}"


if __name__ == "__main__":
    import sys
    sys.stdout.reconfigure(encoding="utf-8")
    if "--sync-mcp" in sys.argv:
        print(sync_mcp())
        print("booster:", link_booster())
        raise SystemExit(0)
    state = resolve()
    print(f"версия : {state.get('version') or '—'}")
    print(f"путь   : {state.get('cli') or '—'}")
    print(f"откат  : {state.get('rollback') or '—'}")
    print(f"откуда : {state['source']}")
    print("\nустановлено на машине:")
    for _, version, path in _all_versions():
        print(f"  {version:10} {path}")
