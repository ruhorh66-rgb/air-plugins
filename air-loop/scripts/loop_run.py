#!/usr/bin/env python3
"""Гейт цикла проверки: гоняет команду до чистого прохода, ведёт журнал попыток.

Зачем существует. Правило «каждый плагин несёт вживлённый цикл» жило в стандарте
текстом, и текст исполнялся по памяти: за одну работу с КС ушло семь ночных
прогонов на переформатирование вывода, потому что круг никто не считал и предел
никто не держал. Это класс «правило без механизма» — лечится не более подробным
текстом, а счётчиком.

Что делает: запускает ГЕЙТ (команду, которая сама решает, годен результат или нет),
считает круги, пишет каждую попытку в журнал и на пределе ОСТАНАВЛИВАЕТСЯ с
диагностикой. Ничего не чинит и результат не оценивает — оценивает гейт.

Чего НЕ делает и делать не должен:
  * не судит о качестве — вердикт даёт гейт своим кодом возврата;
  * не заменяет контроль другой природы (Модуль 4): здесь самопроверка той же
    модели, что делала работу;
  * не правит артефакт между кругами — правку делает исполнитель, в сборщике.

Коды возврата:
  0  гейт прошёл чисто
  1  предел кругов исчерпан, гейт так и не прошёл  (корректный отказ, не молчание)
  2  ошибка вызова самого loop_run

Использование:
    python loop_run.py --gate "python -m smeta.audit_ks2 out.xlsx" --limit 5 \\
                       --goal "КС-2 совпадает с подписанным актом" \\
                       --protocol E:\\-4-\\smeta\\protocol.jsonl
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import time
from datetime import datetime, timezone
from pathlib import Path


def log(protocol: Path | None, payload: dict) -> None:
    """Дописать строку журнала. Журнал append-only: круг не переписывается."""
    if protocol is None:
        return
    protocol.parent.mkdir(parents=True, exist_ok=True)
    with protocol.open("a", encoding="utf-8") as f:
        f.write(json.dumps(payload, ensure_ascii=False) + "\n")


def run_gate(cmd: str, timeout: int) -> tuple[int, str]:
    """Вернуть код возврата и хвост вывода. Таймаут — отказ, а не бесконечное ожидание."""
    try:
        r = subprocess.run(cmd, shell=True, capture_output=True, text=True,
                           encoding="utf-8", errors="replace", timeout=timeout)
    except subprocess.TimeoutExpired:
        return 124, f"гейт не завершился за {timeout} с"
    out = ((r.stdout or "") + (r.stderr or "")).strip()
    return r.returncode, out


def main() -> int:
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser(description="Цикл проверки: гейт до чистого прохода")
    ap.add_argument("--gate", required=True, help="команда гейта; код 0 = годен")
    ap.add_argument("--goal", default="", help="цель круга — против чего сверяется результат")
    ap.add_argument("--limit", type=int, default=5, help="предел кругов (по умолчанию 5)")
    ap.add_argument("--timeout", type=int, default=600, help="таймаут одного прогона гейта, с")
    ap.add_argument("--protocol", default="", help="путь к журналу .jsonl")
    ap.add_argument("--tail", type=int, default=1200, help="сколько знаков вывода гейта показывать")
    a = ap.parse_args()

    if a.limit < 1:
        print("предел кругов должен быть не меньше 1", file=sys.stderr)
        return 2

    protocol = Path(a.protocol) if a.protocol else None
    cycle_id = f"loop-{int(time.time())}"
    attempts: list[dict] = []

    for turn in range(1, a.limit + 1):
        started = time.monotonic()
        code, out = run_gate(a.gate, a.timeout)
        took = round((time.monotonic() - started) * 1000, 1)

        rec = {
            "event": "loop_turn", "cycle_id": cycle_id, "turn": turn,
            "ts": datetime.now(timezone.utc).isoformat(timespec="milliseconds"),
            "gate": a.gate, "goal": a.goal, "exit_code": code,
            "outcome": "clean" if code == 0 else "rejected",
            "duration_ms": took,
        }
        attempts.append(rec)
        log(protocol, rec)

        print(f"круг {turn}/{a.limit}: гейт -> код {code} ({took} мс)")
        if out:
            print("   " + out[-a.tail:].replace("\n", "\n   "))

        if code == 0:
            print()
            print(f"ЦИКЛ ЗАКРЫТ на круге {turn}: гейт прошёл чисто.")
            if a.goal:
                # Гейт — нижний порог. Совпадение с целью подтверждает исполнитель,
                # и оно не выводится из кода возврата (см. SKILL.md § Чистый гейт).
                print(f"ОСТАЁТСЯ СВЕРИТЬ ПО СУЩЕСТВУ: {a.goal}")
            log(protocol, {"event": "loop_done", "cycle_id": cycle_id,
                           "turns": turn, "outcome": "clean", "goal": a.goal})
            return 0

        if turn < a.limit:
            print("   -> починить ПРИЧИНУ в сборщике и пересобрать с нуля, затем следующий круг")

    print()
    print(f"ЦИКЛ ОСТАНОВЛЕН: предел {a.limit} кругов исчерпан, гейт не пройден.")
    print("Это корректный отказ, а не сбой: ЛПР получает то, что проверялось,")
    print("что именно не сходится и что уже испробовано.")
    print(f"   цель      : {a.goal or '(не задана)'}")
    print(f"   гейт      : {a.gate}")
    print(f"   кругов    : {len(attempts)}, все отклонены")
    log(protocol, {"event": "loop_done", "cycle_id": cycle_id,
                   "turns": len(attempts), "outcome": "limit_exhausted", "goal": a.goal})
    return 1


if __name__ == "__main__":
    raise SystemExit(main())
