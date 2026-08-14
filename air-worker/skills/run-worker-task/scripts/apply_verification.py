#!/usr/bin/env python3
"""apply_verification.py — TASK-OBS-0054-бис, замыкание цикла (Ф1/Ф4/Ф6/Ф7).

Не переизобретает ступень 0 и лестницу: `level0_verify.iter_claims()` /
`verify_claim_level0()` и `ladder.run_claim()` уже написаны и прошли свои
`--selftest` — этот файл только соединяет их с ЗАПИСЬЮ в реальные реестры и с
`worker.record_metric()` (§ границы 6 постановки — волт правится только в
полях исхода).

Режимы (ПО УМОЛЧАНИЮ — dry-run, ничего не пишет):
    python apply_verification.py                       # = --dry-run
    python apply_verification.py --dry-run              # предпросмотр, JSON, без записи
    python apply_verification.py --apply-level0-only     # пишет закрытые/переоценённые уровнем 0
    python apply_verification.py --escalate N            # + реальная эскалация, лестница до 5,
                                                          #   paid_budget=N (НЕ вызывать без решения ЛПР)
    python apply_verification.py --selftest              # offline, tempfile CSV, без сети/реестра

Не трогает: ladder.py, level0_check.py, level0_verify.py, selftest.py,
executors.py, worker.py.
"""
from __future__ import annotations

import argparse
import csv
import json
import os
import shutil
import sys
import tempfile
import time
from pathlib import Path

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import ladder  # noqa: E402 — переиспользуем run_claim/reset_paid_counter, не копируем
import level0_check  # noqa: E402 — REGISTRY_DIR/REGISTRY_GLOB/ORIGINALS_DIR/load_originals
import level0_verify  # noqa: E402 — iter_claims/verify_claim_level0, уже прошли selftest
import worker  # noqa: E402 — record_metric пишет в ОБЩИЙ run_metrics.csv, свой журнал не заводим

# ponytail: 480 символов — не научная константа, просто «влезает в CSV-ячейку
# и Excel её не обрежет молча»; апгрейд — вынести в параметр, если понадобится
# другая длина под конкретный реестр.
MAX_REASON_LEN = 480


def _truncate_reason(reason: str) -> str:
    reason = str(reason)
    if len(reason) <= MAX_REASON_LEN:
        return reason
    return reason[: MAX_REASON_LEN - 1] + "…"


# --- 1. оценка заявок: закрыто / кандидаты на эскалацию / терминально -----------

def evaluate(rows, file_index):
    """Разложить заявки по трём корзинам контрактом verify_claim_level0 (шаги 3-5
    постановки). Не фильтрует по прежнему verification_status — переоценивает
    ВСЕ строки, включая уже помеченные «проверить не смог» (2в)."""
    closed: list[tuple[dict, dict]] = []
    escalatable: list[tuple[dict, dict]] = []
    terminal: list[tuple[dict, dict]] = []
    for row in rows:
        result = level0_verify.verify_claim_level0(row, file_index)
        outcome = result["outcome"]
        if outcome in ("подтверждено", "опровергнуто"):
            closed.append((row, result))
        elif result["candidates"]:
            escalatable.append((row, result))
        else:
            terminal.append((row, result))
    return closed, escalatable, terminal


# --- 2. планы записи: только verification_status/next_action --------------------

def _build_level0_writeback(closed, terminal) -> dict[str, dict[str, dict]]:
    """registry filename -> {verification_id: {"verification_status"?: str, "next_action": str}}.

    closed (шаг 3): status меняется на исход, next_action — причина level0.
    terminal без кандидатов (шаг 5): status НЕ включён в апдейт — колонка
    остаётся той, что была прочитана (2в: «даже если текст статуса не
    меняется»), next_action фиксирует, что заявку переоценили заново."""
    plans: dict[str, dict[str, dict]] = {}
    for row, result in closed:
        plans.setdefault(row["registry"], {})[row["verification_id"]] = {
            "verification_status": result["outcome"],
            "next_action": _truncate_reason(result["reason"]),
        }
    for row, result in terminal:
        plans.setdefault(row["registry"], {})[row["verification_id"]] = {
            "next_action": _truncate_reason(
                f"уровень 0 переоценён (apply_verification): {result['reason']}"
            ),
        }
    return plans


def _run_escalation(row: dict, result: dict, paid_budget: int) -> dict:
    """Шаг 4: эскалация с уровня 1, лестница считает paid_calls_used против
    paid_budget сама (ladder.run_claim). next_action называет путь подъёма и
    достигнутый уровень — не молчаливая замена статуса (2в)."""
    claim = {
        "card_type": row.get("card_type", ""),
        "claim_to_verify": row.get("claim_to_verify", ""),
        "candidates": result["candidates"],
    }
    out = ladder.run_claim(claim, start_level=1, paid_budget=paid_budget)
    next_action = (
        f"эскалация со ступени 1 до {out['max_level_reached']} "
        f"(paid_calls_used={out['paid_calls_used']}, paid_budget={paid_budget}): "
        f"{_truncate_reason(out['reason'])}"
    )
    return {"verification_status": out["outcome"], "next_action": next_action}


# --- 3. запись в CSV: DictReader/DictWriter, .bak, только 2 колонки -------------

def _write_back(csv_path: Path, updates: dict[str, dict]) -> int:
    """Читает csv_path целиком, правит только verification_status/next_action
    для verification_id из updates (точное совпадение), пишет назад — порядок
    колонок и все прочие поля не трогает. .bak — один раз, если ещё нет."""
    if not updates:
        return 0
    bak = Path(str(csv_path) + ".bak")
    if not bak.exists():
        shutil.copy2(csv_path, bak)
    with csv_path.open(encoding="utf-8-sig", newline="") as fh:
        reader = csv.DictReader(fh)
        fieldnames = reader.fieldnames
        rows = list(reader)
    changed = 0
    for row in rows:
        upd = updates.get(row.get("verification_id"))
        if not upd:
            continue
        if "verification_status" in upd:
            row["verification_status"] = upd["verification_status"]
        row["next_action"] = upd["next_action"]
        changed += 1
    with csv_path.open("w", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows)
    return changed


def _apply_plans(plans: dict[str, dict[str, dict]], registry_dir: Path) -> int:
    total = 0
    for registry_name, updates in plans.items():
        total += _write_back(registry_dir / registry_name, updates)
    return total


# --- 4. режимы CLI ---------------------------------------------------------------

def _dry_run() -> dict:
    file_index = level0_check.load_originals(level0_check.ORIGINALS_DIR)
    rows = list(level0_verify.iter_claims())
    closed, escalatable, terminal = evaluate(rows, file_index)
    summary = {
        "total_claims": len(rows),
        "would_close_at_level0": len(closed),
        "would_close_confirmed": sum(1 for _, r in closed if r["outcome"] == "подтверждено"),
        "would_close_refuted": sum(1 for _, r in closed if r["outcome"] == "опровергнуто"),
        "would_escalate_with_candidates": len(escalatable),
        "terminal_no_candidates": len(terminal),
        "files_indexed_in_05_ORIGINALS": len(file_index),
    }
    print(json.dumps(summary, ensure_ascii=False, indent=2))
    return summary


def _apply_level0_only() -> dict:
    started = time.time()
    file_index = level0_check.load_originals(level0_check.ORIGINALS_DIR)
    rows = list(level0_verify.iter_claims())
    closed, escalatable, terminal = evaluate(rows, file_index)
    plans = _build_level0_writeback(closed, terminal)
    rows_written = _apply_plans(plans, level0_check.REGISTRY_DIR)
    elapsed = round(time.time() - started, 3)

    note = f"TASK-OBS-0054-bis level0 writeback: {len(closed)} closed, {len(rows)} reprocessed"
    worker.record_metric("air-worker apply_verification level0", model="", tokens_in="",
                         tokens_out="", wall_time_s=elapsed, outcome="accepted", note=note)

    result = {
        "closed": len(closed),
        "reprocessed_total": len(rows),
        "terminal_no_candidates_updated": len(terminal),
        "escalatable_left_for_escalate_flag": len(escalatable),
        "rows_written": rows_written,
        "wall_time_s": elapsed,
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return result


def _run_with_escalation(paid_budget: int) -> dict:
    """--escalate N: реальный проход по лестнице (ladder.run_claim) для заявок
    с кандидатами. НЕ вызывается этой сессией — потолок платных ступеней
    требует явного решения ЛПР перед первым реальным прогоном (§6)."""
    started = time.time()
    file_index = level0_check.load_originals(level0_check.ORIGINALS_DIR)
    rows = list(level0_verify.iter_claims())
    closed, escalatable, terminal = evaluate(rows, file_index)
    plans = _build_level0_writeback(closed, terminal)

    ladder.reset_paid_counter()
    for row, result in escalatable:
        plans.setdefault(row["registry"], {})[row["verification_id"]] = _run_escalation(
            row, result, paid_budget)

    rows_written = _apply_plans(plans, level0_check.REGISTRY_DIR)
    elapsed = round(time.time() - started, 3)

    note = (f"TASK-OBS-0054-bis full run: {len(closed)} closed level0, "
           f"{len(escalatable)} escalated (paid_budget={paid_budget}, "
           f"paid_calls_used={ladder.paid_calls_used()}), {len(rows)} reprocessed")
    worker.record_metric("air-worker apply_verification escalate", model="", tokens_in="",
                         tokens_out="", wall_time_s=elapsed, outcome="accepted", note=note)

    result = {
        "closed": len(closed),
        "escalated": len(escalatable),
        "paid_budget": paid_budget,
        "paid_calls_used": ladder.paid_calls_used(),
        "reprocessed_total": len(rows),
        "rows_written": rows_written,
        "wall_time_s": elapsed,
    }
    print(json.dumps(result, ensure_ascii=False, indent=2))
    return result


# --- 5. самопроверка: offline, tempfile CSV, без сети/реестра/метрик -----------

_FIELDNAMES = ["verification_id", "priority", "card_id", "card_type", "claim_to_verify",
              "required_primary_sources", "verification_status", "next_action"]


def _write_temp_csv(rows: list[dict]) -> Path:
    d = Path(tempfile.mkdtemp(prefix="apply_verification_selftest_"))
    p = d / "source_verification_queue_test.csv"
    with p.open("w", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=_FIELDNAMES)
        writer.writeheader()
        writer.writerows(rows)
    return p


def _selftest() -> None:
    # --- 1. level0-close write-back: меняет только status/next_action,
    #    остальные колонки той же строки и соседняя строка не тронуты ---------
    csv_path = _write_temp_csv([
        {"verification_id": "SV-T-001", "priority": "P0", "card_id": "C1",
         "card_type": "decision", "claim_to_verify": "утверждение 1",
         "required_primary_sources": "источник 1", "verification_status": "pending",
         "next_action": ""},
        {"verification_id": "SV-T-002", "priority": "P1", "card_id": "C2",
         "card_type": "risk", "claim_to_verify": "утверждение 2",
         "required_primary_sources": "источник 2", "verification_status": "pending",
         "next_action": ""},
    ])
    row1 = {"verification_id": "SV-T-001", "card_type": "decision",
           "claim_to_verify": "утверждение 1", "registry": csv_path.name}
    result1 = {"outcome": "подтверждено", "reason": "в источнике X.pdf найдено: номер(а) ['123']",
              "candidates": ["X.pdf"]}
    closed = [(row1, result1)]
    plans = _build_level0_writeback(closed, terminal=[])
    changed = _write_back(csv_path, plans[csv_path.name])
    assert changed == 1

    with csv_path.open(encoding="utf-8", newline="") as fh:
        out_rows = {r["verification_id"]: r for r in csv.DictReader(fh)}
    assert out_rows["SV-T-001"]["verification_status"] == "подтверждено"
    assert "X.pdf" in out_rows["SV-T-001"]["next_action"]
    # неизменённые колонки той же строки
    assert out_rows["SV-T-001"]["priority"] == "P0"
    assert out_rows["SV-T-001"]["card_id"] == "C1"
    assert out_rows["SV-T-001"]["required_primary_sources"] == "источник 1"
    # соседняя строка не тронута вообще
    assert out_rows["SV-T-002"]["verification_status"] == "pending"
    assert out_rows["SV-T-002"]["next_action"] == ""
    # .bak создан один раз
    bak = Path(str(csv_path) + ".bak")
    assert bak.is_file()
    bak_mtime = bak.stat().st_mtime
    _write_back(csv_path, {})  # пустой апдейт — .bak не трогается, ничего не пишется
    assert bak.stat().st_mtime == bak_mtime

    # --- 2. эскалация: next_action называет путь подъёма и достигнутый уровень
    captured = {}

    def fake_run_claim(claim, *, start_level, paid_budget):
        captured["claim"] = claim
        captured["start_level"] = start_level
        captured["paid_budget"] = paid_budget
        return {"outcome": "опровергнуто", "max_level_reached": 4,
               "paid_calls_used": 1, "reason": "haiku: не следует из источника", "log": []}

    orig_run_claim = ladder.run_claim
    ladder.run_claim = fake_run_claim
    try:
        row2 = {"verification_id": "SV-T-002", "card_type": "risk",
               "claim_to_verify": "утверждение 2", "registry": csv_path.name}
        result2 = {"outcome": "не смог", "reason": "первоисточник не опознан",
                  "candidates": ["cand1.pdf", "cand2.pdf"]}
        esc = _run_escalation(row2, result2, paid_budget=5)
    finally:
        ladder.run_claim = orig_run_claim

    assert esc["verification_status"] == "опровергнуто"
    assert "1" in esc["next_action"] and "4" in esc["next_action"]  # путь 1 → 4
    assert "paid_budget=5" in esc["next_action"]
    assert "haiku" in esc["next_action"]
    assert captured["start_level"] == 1
    assert captured["paid_budget"] == 5  # 4. paid_budget действительно передан и назван
    assert captured["claim"]["candidates"] == ["cand1.pdf", "cand2.pdf"]
    assert captured["claim"]["card_type"] == "risk"

    # --- 3. терминальные "не смог" без кандидатов: status НЕ меняется в плане,
    #    next_action фиксирует переоценку --------------------------------------
    row3 = {"verification_id": "SV-T-003", "card_type": "task",
           "claim_to_verify": "x", "registry": "r.csv"}
    result3 = {"outcome": "не смог", "reason": "первоисточник не найден", "candidates": []}
    plans3 = _build_level0_writeback(closed=[], terminal=[(row3, result3)])
    upd3 = plans3["r.csv"]["SV-T-003"]
    assert "verification_status" not in upd3
    assert "переоценён" in upd3["next_action"] and "первоисточник не найден" in upd3["next_action"]

    # --- 4. ранее закрытые 16-строчным исходом "проверить не смог" всё равно
    #    переоцениваются заново — evaluate() не фильтрует по старому статусу ---
    row_old_closed = {"verification_id": "SV-OLD-016", "card_type": "decision",
                      "claim_to_verify": "x", "registry": "r.csv",
                      "verification_status": "проверить не смог",
                      "next_action": "проверить не смог — первоисточник не найден в 05_ORIGINALS на уровне 0"}
    calls = []

    def fake_verify(row, file_index):
        calls.append(row["verification_id"])
        return {"outcome": "подтверждено", "reason": "источник Y.pdf: номер(а) ['77']",
               "candidates": ["Y.pdf"]}

    orig_verify = level0_verify.verify_claim_level0
    level0_verify.verify_claim_level0 = fake_verify
    try:
        closed4, esc4, term4 = evaluate([row_old_closed], file_index=[])
    finally:
        level0_verify.verify_claim_level0 = orig_verify
    assert calls == ["SV-OLD-016"]  # старый терминальный статус не пропустил переоценку
    assert len(closed4) == 1 and closed4[0][1]["outcome"] == "подтверждено"

    # --- 5. дефолтный режим (без флагов = --dry-run) не пишет ни в CSV, ни в
    #    метрики: подменяем iter_claims/load_originals фейками и ловим любой
    #    вызов _write_back/record_metric как провал ---------------------------
    def fail_write_back(*a, **k):
        raise AssertionError("dry-run не должен писать в CSV")

    def fail_record_metric(*a, **k):
        raise AssertionError("dry-run не должен писать в метрики")

    def fake_iter_claims():
        yield {"verification_id": "SV-T-001", "card_type": "decision",
              "claim_to_verify": "x", "registry": "r.csv"}
        yield {"verification_id": "SV-T-002", "card_type": "risk",
              "claim_to_verify": "y", "registry": "r.csv"}

    def fake_load_originals(root):
        return []

    orig_write_back = globals()["_write_back"]
    orig_record_metric = worker.record_metric
    orig_iter_claims = level0_verify.iter_claims
    orig_load_originals = level0_check.load_originals
    orig_verify2 = level0_verify.verify_claim_level0
    globals()["_write_back"] = fail_write_back
    worker.record_metric = fail_record_metric
    level0_verify.iter_claims = fake_iter_claims
    level0_check.load_originals = fake_load_originals
    level0_verify.verify_claim_level0 = fake_verify
    try:
        main(["--dry-run"])
        main([])  # без флагов — тот же дефолт
    finally:
        globals()["_write_back"] = orig_write_back
        worker.record_metric = orig_record_metric
        level0_verify.iter_claims = orig_iter_claims
        level0_check.load_originals = orig_load_originals
        level0_verify.verify_claim_level0 = orig_verify2

    shutil.rmtree(csv_path.parent, ignore_errors=True)
    print("apply_verification.py selftest ok")


# --- 6. CLI ------------------------------------------------------------------

def main(argv: list[str] | None = None) -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--dry-run", action="store_true", help="предпросмотр, без записи (по умолчанию)")
    mode.add_argument("--apply-level0-only", action="store_true",
                      help="писать в реальные CSV закрытые/переоценённые уровнем 0")
    mode.add_argument("--escalate", type=int, metavar="PAID_BUDGET", default=None,
                      help="+ реальная эскалация до ступеней 4-5, потолок платных вызовов PAID_BUDGET")
    mode.add_argument("--selftest", action="store_true", help="offline самопроверка")
    args = parser.parse_args(argv)

    if args.selftest:
        _selftest()
    elif args.apply_level0_only:
        _apply_level0_only()
    elif args.escalate is not None:
        _run_with_escalation(args.escalate)
    else:
        _dry_run()


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    main()
