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

import sys

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

REPORTS = os.environ.get("RUFLO_REPORTS") or r"E:\-4-\ruflo-hive"
MIN_SIZE = 5_000  # мельче — это не прогон, а обрывок

def _num(value, default: float = 0.0) -> float:
    """Число из повреждённого лога. Мусор даёт умолчание, а не падение.

    Прогон роя — единственный источник этих цифр, и он же пишется на живой машине:
    оборванная запись, строка вместо числа,наполовину обрезанный файл — обычное дело.
    Метрика, падающая на одной битой записи, не считает и всё остальное.
    """
    try:
        return float(value)
    except (TypeError, ValueError):
        return default


def scan(path: str) -> dict:
    """Один отчёт → метрики. Файл читается ПОТОКОМ, по строке.

    Целиком в память его брать нельзя: отчёты уже сейчас по 5 МБ, а верхней границы
    у них нет — она равна объёму работы роя (находка codex review 10.08.2026).
    """
    per_model: dict[str, collections.Counter] = collections.defaultdict(collections.Counter)
    result: dict = {}
    answers = 0
    mcp: collections.Counter = collections.Counter()
    with open(path, encoding="utf-8", errors="replace") as fh:
        for line in fh:
            line = line.strip()
            if not line.startswith("{"):
                continue
            try:
                obj = json.loads(line)
            except ValueError:
                continue
            kind = obj.get("type")
            if kind == "result":
                result = obj
            elif kind == "assistant":
                answers += 1
                message = obj.get("message") or {}
                usage = message.get("usage") or {}
                counter = per_model[message.get("model") or "?"]
                counter["ответов"] += 1
                for key in ("input_tokens", "output_tokens",
                            "cache_creation_input_tokens", "cache_read_input_tokens"):
                    counter[key] += int(_num(usage.get(key)))
                # Вызовы роевых инструментов считаются ТОЛЬКО по блокам tool_use в
                # ответе модели. Прежняя редакция искала `"name":"mcp__..."` по всему
                # тексту: считала упоминания в чужих строках и при этом теряла имена
                # с дефисом (`hive-mind_*`) — на прогоне 0040 выходило 22 против
                # настоящих 24. Обе ошибки сразу, в разные стороны.
                content = message.get("content")
                if isinstance(content, list):
                    for block in content:
                        if not isinstance(block, dict) or block.get("type") != "tool_use":
                            continue
                        name = str(block.get("name") or "")
                        if name.startswith("mcp__claude-flow__"):
                            mcp[name] += 1
    # `num_turns` берём, только если ключ ЕСТЬ: честный ноль подменялся числом
    # ответов, и прогон без ходов показывал единицу.
    turns = int(_num(result["num_turns"])) if "num_turns" in result else answers
    return {
        "файл": os.path.basename(path),
        "модели": per_model,
        "стоимость": _num(result.get("total_cost_usd")),
        "ходов": turns,
        "исход": (result.get("subtype") or "нет записи result")
                 + (" (is_error)" if result.get("is_error") else ""),
        "mcp": mcp,
    }


def main(argv: list[str]) -> int:
    # Путь(и) в аргументах — считать только их. Нужно для вызова из run_task.ps1 сразу
    # после прогона: там интересен ИМЕННО этот прогон, а не сводка по всем. Без такого
    # отбора команда печатала всю историю, и своя строка терялась в ней.
    only = [a for a in argv if not a.startswith("--")]
    paths = only or sorted(glob.glob(os.path.join(REPORTS, "*.md")))
    runs = [scan(p) for p in paths
            if os.path.isfile(p) and os.path.getsize(p) >= MIN_SIZE]
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
    def run(lines: list[str]) -> dict:
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "проба.md")
            with open(path, "w", encoding="utf-8") as fh:
                fh.write("\n".join(lines))
            return scan(path)

    got = run([
        '{"type":"assistant","message":{"model":"claude-opus-5","usage":'
        '{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":1000},'
        '"content":[{"type":"tool_use","name":"mcp__claude-flow__agent_spawn"},'
        '{"type":"tool_use","name":"mcp__claude-flow__hive-mind_status"},'
        '{"type":"text","text":"обычный текст"}]}}',
        'не json — обязан быть пропущен, а не уронить подсчёт',
        '{"broken": ',
        # Упоминание имени инструмента в ЧУЖОЙ записи вызовом не является.
        '{"type":"user","message":{"content":"тут написано '
        'mcp__claude-flow__agent_spawn, но это текст, а не вызов"}}',
        '{"type":"result","subtype":"success","total_cost_usd":1.25,"num_turns":7}',
    ])
    assert got["стоимость"] == 1.25, got
    assert got["ходов"] == 7, got
    assert got["исход"] == "success", got
    assert got["модели"]["claude-opus-5"]["output_tokens"] == 5, got
    assert got["модели"]["claude-opus-5"]["cache_read_input_tokens"] == 1000, got
    assert sum(got["mcp"].values()) == 2, f"вызовов должно быть ровно два: {got['mcp']}"
    assert "mcp__claude-flow__hive-mind_status" in got["mcp"], \
        "имя с дефисом обязано считаться — прежняя редакция их теряла"

    # Повреждённые числа не роняют подсчёт: строка вместо числа даёт ноль.
    broken = run(['{"type":"assistant","message":{"model":"m","usage":'
                  '{"output_tokens":"oops"}}}'])
    assert broken["модели"]["m"]["output_tokens"] == 0, broken

    # Честный ноль ходов остаётся нулём, а не подменяется числом ответов.
    zero = run(['{"type":"assistant","message":{"model":"m","usage":{}}}',
                '{"type":"result","subtype":"success","num_turns":0}'])
    assert zero["ходов"] == 0, zero
    # …а когда ключа нет вовсе — берём число ответов, иначе показывать нечего.
    noturns = run(['{"type":"assistant","message":{"model":"m","usage":{}}}'])
    assert noturns["ходов"] == 1, noturns

    print("selftest ok: битые строки и числа пропущены, вызовы считаются по tool_use")
    return 0


if __name__ == "__main__":
    sys.exit(_selftest() if "--selftest" in sys.argv else main(sys.argv[1:]))
