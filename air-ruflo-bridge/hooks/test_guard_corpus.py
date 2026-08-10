#!/usr/bin/env python3
"""Корпус отказов guard.py: команда -> ожидаемое решение. Прогон одной командой.

    python hooks/test_guard_corpus.py

ЗАЧЕМ ОТДЕЛЬНЫМ ФАЙЛОМ, А НЕ СТРОКАМИ В `--selftest`. Самопроверка писалась вместе
с кодом и потому проверяет то, о чём автор кода уже подумал. Здесь входы взяты из
ЖУРНАЛА СЕССИИ, где хук отказал по-настоящему: `id` вида `journal:23412` — номер
строки в `31e875f4-….jsonl`, команда дословная. Такой корпус ловит и то, о чём
автор не подумал, и падает, если поведение поедет назад (TASK-OBS-0044).

Разметка `expect` — решение, которое проверка ОБЯЗАНА принять на этом входе:
`deny` — команда действительно запускала рой мимо обвязки либо ставила версию без
тега; `allow` — образец попал в команду как ДАННЫЕ (текст сообщения, шаблон grep,
строка внутри python -c, справка `--help`).
"""
from __future__ import annotations

import importlib.util
import json
import pathlib
import sys

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

HERE = pathlib.Path(__file__).resolve().parent
CORPUS = HERE / "guard_corpus.json"


def load_guard():
    spec = importlib.util.spec_from_file_location("guard", HERE / "guard.py")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def fixture_tag_missing(guard) -> bool:
    """Требование тега — на своём плагине, у которого тега заведомо НЕТ.

    Случай `journal:22746` из корпуса истинный, но воспроизвести его прогоном
    нельзя: тег с тех пор поставлен, и проверка теперь справедливо молчит. Чтобы
    истинность класса доказывалась ЗАПУСКОМ, а не утверждением, собираем
    одноразовый маркетплейс на диске: репозиторий без единого тега и манифест
    плагина в нём.
    """
    import subprocess
    import tempfile
    root = pathlib.Path(tempfile.mkdtemp())
    plugin = root / "fixture-plugin"
    (plugin / ".claude-plugin").mkdir(parents=True)
    (plugin / ".claude-plugin" / "plugin.json").write_text(
        json.dumps({"name": "fixture-plugin", "version": "9.9.9"}), encoding="utf-8")
    subprocess.run(["git", "-C", str(root), "init", "-q"], check=True)
    market = root / "known_marketplaces.json"
    market.write_text(json.dumps({"marketplaces": {
        "fixture-market": {"source": {"source": "directory", "path": str(root)}}}}),
        encoding="utf-8")
    keep, guard.MARKETPLACES = guard.MARKETPLACES, market
    try:
        note = guard.check_release_tag(
            "claude plugin update fixture-plugin@fixture-market")
        ok = bool(note) and "нет тега релиза" in note
        # …а с тегом — молчит: иначе проверка запрещала бы вообще всё.
        subprocess.run(["git", "-C", str(root), "-c", "user.email=t@t", "-c", "user.name=t",
                        "commit", "-q", "--allow-empty", "-m", "x"],
                       check=True)
        subprocess.run(["git", "-C", str(root), "tag", "fixture-plugin--v9.9.9"], check=True)
        ok = ok and guard.check_release_tag(
            "claude plugin update fixture-plugin@fixture-market") is None
    finally:
        guard.MARKETPLACES = keep
    return ok


def main() -> int:
    guard = load_guard()
    cases = json.loads(CORPUS.read_text(encoding="utf-8"))["cases"]
    failures = []
    for case in cases:
        if case.get("skip"):
            print(f"  --  {case['id']:<16} не прогоняется — {case['skip']}")
            continue
        got = "deny" if guard.check(case["command"]) else "allow"
        mark = "ok " if got == case["expect"] else "ПЛОХО"
        if got != case["expect"]:
            failures.append(case)
        print(f"  {mark} {case['id']:<16} ждали {case['expect']:<5} получили {got:<5} "
              f"— {case['why']}")

    if fixture_tag_missing(guard):
        print("  ok  fixture:tag-missing  ждали deny  получили deny  "
              "— установка версии без тега отклоняется, с тегом проходит")
    else:
        failures.append({"id": "fixture:tag-missing"})
        print("  ПЛОХО fixture:tag-missing — требование тега перестало работать")

    denies = [c for c in cases if c["source"].endswith(".jsonl")]
    false_denies = [c for c in denies if c["expect"] == "allow"]
    still_false = [c for c in false_denies
                   if guard.check(c["command"])]
    print()
    print(f"отказов из журнала: {len(denies)}; из них ложных по разметке: {len(false_denies)}; "
          f"ложных ОСТАЛОСЬ: {len(still_false)}")
    if failures:
        print(f"РАСХОЖДЕНИЙ: {len(failures)}")
        return 1
    print("корпус пройден")
    return 0


if __name__ == "__main__":
    sys.exit(main())
