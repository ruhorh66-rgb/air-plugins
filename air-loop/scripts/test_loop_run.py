#!/usr/bin/env python3
"""Самопроверка loop_run: круг считается, предел держится, журнал пишется.

Запуск: python test_loop_run.py
Фреймворков нет намеренно — assert и один файл, по правилу «одна runnable проверка».
"""
from __future__ import annotations

import json
import subprocess
import sys
import tempfile
from pathlib import Path

RUNNER = Path(__file__).with_name("loop_run.py")
PY = sys.executable


def run(args: list[str]) -> tuple[int, str]:
    r = subprocess.run([PY, str(RUNNER), *args], capture_output=True, text=True,
                       encoding="utf-8", errors="replace", timeout=120)
    return r.returncode, (r.stdout or "") + (r.stderr or "")


def test_clean_gate_closes_on_first_turn():
    code, out = run(["--gate", f'"{PY}" -c "raise SystemExit(0)"', "--limit", "5"])
    assert code == 0, f"чистый гейт обязан дать 0, дал {code}"
    assert "круг 1/5" in out
    assert "ЦИКЛ ЗАКРЫТ на круге 1" in out
    assert "круг 2/5" not in out, "после чистого гейта круги продолжаться не должны"
    print("OK  test_clean_gate_closes_on_first_turn")


def test_failing_gate_stops_at_limit_not_forever():
    code, out = run(["--gate", f'"{PY}" -c "raise SystemExit(1)"', "--limit", "3"])
    assert code == 1, f"исчерпанный предел обязан дать 1, дал {code}"
    assert "круг 3/3" in out
    assert "круг 4/3" not in out, "предел не удержан — цикл ушёл за лимит"
    assert "ЦИКЛ ОСТАНОВЛЕН" in out
    print("OK  test_failing_gate_stops_at_limit_not_forever")


def test_goal_is_named_after_clean_gate():
    """Чистый гейт — нижний порог: цель обязана быть названа отдельной строкой."""
    code, out = run(["--gate", f'"{PY}" -c "raise SystemExit(0)"',
                     "--goal", "совпадает с подписанным актом"])
    assert code == 0
    assert "ОСТАЁТСЯ СВЕРИТЬ ПО СУЩЕСТВУ: совпадает с подписанным актом" in out, \
        "зелёный гейт не должен читаться как достигнутая цель"
    print("OK  test_goal_is_named_after_clean_gate")


def test_protocol_is_append_only_and_parses():
    with tempfile.TemporaryDirectory() as d:
        p = Path(d) / "protocol.jsonl"
        run(["--gate", f'"{PY}" -c "raise SystemExit(1)"', "--limit", "2",
             "--protocol", str(p), "--goal", "цель"])
        rows = [json.loads(x) for x in p.read_text(encoding="utf-8").splitlines() if x.strip()]
        turns = [r for r in rows if r["event"] == "loop_turn"]
        done = [r for r in rows if r["event"] == "loop_done"]
        assert len(turns) == 2, f"кругов в журнале {len(turns)}, ожидалось 2"
        assert [r["turn"] for r in turns] == [1, 2]
        assert all(r["outcome"] == "rejected" for r in turns)
        assert len(done) == 1 and done[0]["outcome"] == "limit_exhausted"
        assert len({r["cycle_id"] for r in rows}) == 1, "cycle_id обязан быть один на прогон"
    print("OK  test_protocol_is_append_only_and_parses")


def test_bad_limit_is_a_call_error_not_a_verdict():
    code, _ = run(["--gate", "echo x", "--limit", "0"])
    assert code == 2, f"ошибка вызова обязана дать 2, дала {code}"
    print("OK  test_bad_limit_is_a_call_error_not_a_verdict")


def test_timeout_counts_as_rejected_turn():
    code, out = run(["--gate", f'"{PY}" -c "import time; time.sleep(30)"',
                     "--limit", "1", "--timeout", "2"])
    assert code == 1, "зависший гейт обязан считаться отклонённым кругом, а не успехом"
    assert "не завершился за 2" in out
    print("OK  test_timeout_counts_as_rejected_turn")


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    test_clean_gate_closes_on_first_turn()
    test_failing_gate_stops_at_limit_not_forever()
    test_goal_is_named_after_clean_gate()
    test_protocol_is_append_only_and_parses()
    test_bad_limit_is_a_call_error_not_a_verdict()
    test_timeout_counts_as_rejected_turn()
    print("all loop_run self-checks passed")
