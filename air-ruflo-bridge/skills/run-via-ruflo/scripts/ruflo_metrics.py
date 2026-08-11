r"""Метрики прогонов роя: деньги, ходы, модели, реальность роя.

ЗАЧЕМ ОТДЕЛЬНАЯ КОМАНДА. Указание ЛПР 10.08.2026: «мы перестали анализировать, что
у нас с расходом токенов, какие модели, какие метрики — для меня это важно». До неё
метрика существовала как разовый скрипт в черновиках, то есть не существовала.

ПОЧЕМУ НЕ БЕРЁМ ЦИФРЫ У ДВИЖКА. У ruflo есть команда `providers usage`, и она
печатает красивую таблицу: 12 847 запросов, $12.60, «savings from local embeddings
$890.12». Эти числа ЗАШИТЫ В КОД (`dist/src/commands/providers.js`) и к нашим
прогонам отношения не имеют — проверено 10.08.2026 поиском по исходнику после того,
как они разошлись с нашим замером на порядок. Считаем сами, по логам прогонов.

ЧТО СЧИТАЕТСЯ ИСТОЧНИКОМ. `run_task.ps1` сохраняет весь stream-json прогона в отчёт.
Там есть `usage` в записях assistant (расход той модели, что стоит в `model`),
`total_cost_usd` в записи `result` (итог самого движка) и вызовы
`mcp__claude-flow__*` — единственное доказательство, что рой был роем, а не одной
дорогой сессией (`ERR-2026-000192`).

Использование:
    python ruflo_metrics.py            # сводка по всем прогонам
    python ruflo_metrics.py --models   # плюс разбивка по моделям на каждый прогон
"""
from __future__ import annotations

import collections
import glob
import json
import os
import re
import sys

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

REPORTS = os.environ.get("RUFLO_REPORTS") or r"E:\-4-\ruflo-hive"
MIN_SIZE = 5_000  # мельче — это не прогон, а обрывок

MCP_CALL = re.compile(r'"name":"(mcp__claude-flow__[A-Za-z_]+)"')


def scan(path: str) -> dict:
    """Один отчёт → метрики. Битые строки пропускаются, а не роняют подсчёт."""
    per_model: dict[str, collections.Counter] = collections.defaultdict(collections.Counter)
    result: dict = {}
    answers = 0
    with open(path, encoding="utf-8", errors="replace") as fh:
        text = fh.read()
    for line in text.splitlines():
        line = line.strip()
        if not line.startswith("{"):
            continue
        try:
            obj = json.loads(line)
        except ValueError:
            continue
        if obj.get("type") == "result":
            result = obj
        elif obj.get("type") == "assistant":
            answers += 1
            message = obj.get("message") or {}
            usage = message.get("usage") or {}
            counter = per_model[message.get("model") or "?"]
            counter["ответов"] += 1
            for key in ("input_tokens", "output_tokens",
                        "cache_creation_input_tokens", "cache_read_input_tokens"):
                counter[key] += int(usage.get(key) or 0)
    return {
        "файл": os.path.basename(path),
        "модели": per_model,
        "стоимость": float(result.get("total_cost_usd") or 0),
        "ходов": int(result.get("num_turns") or answers),
        "исход": (result.get("subtype") or "нет записи result")
                 + (" (is_error)" if result.get("is_error") else ""),
        "mcp": collections.Counter(MCP_CALL.findall(text)),
    }


def main(argv: list[str]) -> int:
    runs = [scan(p) for p in sorted(glob.glob(os.path.join(REPORTS, "*.md")))
            if os.path.getsize(p) >= MIN_SIZE]
    if not runs:
        print(f"прогонов не найдено в {REPORTS}")
        return 3

    print(f"{'прогон':44}{'$':>8}{'ходов':>7}{'вызовов роя':>13}  исход")
    print("-" * 96)
    for run in runs:
        print(f"{run['файл'][:43]:44}{run['стоимость']:8.2f}{run['ходов']:7}"
              f"{sum(run['mcp'].values()):13}  {run['исход']}")
    total = sum(r["стоимость"] for r in runs)
    print("-" * 96)
    print(f"{'ИТОГО за ' + str(len(runs)) + ' прогонов':44}{total:8.2f}")

    grand: dict[str, collections.Counter] = collections.defaultdict(collections.Counter)
    for run in runs:
        for model, counts in run["модели"].items():
            grand[model].update(counts)
    print("\nМОДЕЛИ (все прогоны):")
    print(f"{'модель':44}{'ответов':>9}{'выход':>10}{'чтение кэша':>15}")
    for model, c in sorted(grand.items(), key=lambda kv: -kv[1]["ответов"]):
        print(f"{model[:43]:44}{c['ответов']:9}{c['output_tokens']:10,}"
              f"{c['cache_read_input_tokens']:15,}")
    # Отношение «выход / перечитывание» — то, из-за чего рой дорогой. Оно же
    # объясняет, почему цена падает от КОРОТКИХ заданий, а не от дешёвых моделей.
    out = sum(c["output_tokens"] for c in grand.values())
    cr = sum(c["cache_read_input_tokens"] for c in grand.values())
    if out:
        print(f"\nНа каждый токен ВЫХОДА приходится {cr // out:,} токенов перечитывания "
              f"кэша.\nЦена линейна по числу ходов: каждый ход перечитывает всё "
              f"накопленное.")

    if "--models" in argv:
        for run in runs:
            if not run["модели"]:
                continue
            print(f"\n=== {run['файл']}   ${run['стоимость']:.2f}")
            for model, c in sorted(run["модели"].items(), key=lambda kv: -kv[1]["ответов"]):
                print(f"  {model[:34]:36}ответов {c['ответов']:4}  "
                      f"выход {c['output_tokens']:7,}  "
                      f"чтение кэша {c['cache_read_input_tokens']:12,}")
    return 0


def _selftest() -> int:
    """Проверяется разбор, а не цифры: цифры зависят от прогонов и меняются."""
    import tempfile
    sample = [
        '{"type":"assistant","message":{"model":"claude-opus-5","usage":'
        '{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":1000}}}',
        'не json — обязан быть пропущен, а не уронить подсчёт',
        '{"broken": ',
        '{"type":"user","message":{"content":"mcp__claude-flow__agent_spawn упомянут '
        'в тексте — и посчитан, потому что отличить упоминание от вызова в этом '
        'формате нельзя; число трактуется как ВЕРХНЯЯ оценка"}}',
        '{"name":"mcp__claude-flow__agent_spawn"}',
        '{"type":"result","subtype":"success","total_cost_usd":1.25,"num_turns":7}',
    ]
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "проба.md")
        with open(path, "w", encoding="utf-8") as fh:
            fh.write("\n".join(sample))
        got = scan(path)
    assert got["стоимость"] == 1.25, got
    assert got["ходов"] == 7, got
    assert got["исход"] == "success", got
    assert got["модели"]["claude-opus-5"]["output_tokens"] == 5, got
    assert got["модели"]["claude-opus-5"]["cache_read_input_tokens"] == 1000, got
    assert sum(got["mcp"].values()) >= 1, got
    print("selftest ok: битые строки пропущены, счётчики сошлись")
    return 0


if __name__ == "__main__":
    sys.exit(_selftest() if "--selftest" in sys.argv else main(sys.argv[1:]))
