#!/usr/bin/env python3
"""level1_qwen.py — TASK-OBS-0054-бис, ступень 1 лестницы (часть B).

Берёт заявки, которые ступень 0 (level0_verify.py) закрыла как «не смог», но
для которых НАШЛИСЬ файлы-кандидаты (level0 умеет опознать источник по
номеру/дате/стороне/типу документа, но не умеет читать СМЫСЛ — сопоставить
«заявка словами» с текстом источника). Ступень 1 отдаёт релевантный кусок
текста локальной qwen2.5-7b-instruct через уже существующую очередь
llm-queue и получает исход по существу.

Не трогает: ladder.py, selftest.py, level0_check.py, executors.py,
worker.py, claude_judge_run.py, llm-queue (dispatcher.py/llm_client.py) —
их правят другие воркеры. `wait_job`/`_parse_job_id` ИМПОРТИРУЮТСЯ из
ladder.py, не переписаны. В реестры волта ничего не пишет — только читает
через level0_verify.iter_claims() и level0_verify_report.json.

Осторожность важнее полноты (2а/приёмка TASK-OBS-0054): «подтверждено» от
7B-модели принимается ТОЛЬКО когда сверяемое значение (номер/дата) реально
есть в присланном куске текста — это проверяется МЕХАНИЧЕСКИ питоном ПОСЛЕ
ответа модели, не на слово модели. Ложное «подтверждено» — худший исход;
неразобранный ответ, таймаут очереди, пустой текст — всегда «не смог» с
причиной, никогда исключение и никогда подтверждение по умолчанию.

Запуск:
    python level1_qwen.py --smoke          # один реальный вызов через очередь
    python level1_qwen.py --run --limit N  # прогон N заявок ступени 0 "не смог"
    python level1_qwen.py --selftest       # офлайн assert'ы, очередь мокается
"""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
import time
from pathlib import Path

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import ladder  # noqa: E402 — DISPATCHER/DISPATCHER_PYTHON путь переиспользуем, не дублируем
from ladder import wait_job, _parse_job_id  # noqa: E402 — не пишем свой wait_job (ТЗ, часть B)

# level0_check даёт extract_numbers/extract_dates (регэксп номеров/дат) и
# ORIGINALS_DIR/load_originals — тот же индекс 05_ORIGINALS, что и ступень 0,
# не второй парсер. level0_verify даёт extract_text (с кэшем на диске),
# verify_claim_level0 (чтобы заново получить РАНЖИРОВАННЫХ кандидатов для
# конкретной заявки — отчёт ступени 0 хранит только их число, не пути,
# потому что 2в.4 этой задачи запрещает трогать что-либо в level0_verify.py
# кроме извлечения текста) и REPORT_PATH.
from level0_check import ORIGINALS_DIR, extract_dates, extract_numbers, load_originals  # noqa: E402
import level0_verify  # noqa: E402

MODEL_NAME = "qwen2.5-7b-instruct"
REPORT_PATH = level0_verify.REPORT_PATH  # E:\-4-\air-worker\level0_verify_report.json (вход, только читаем)
OUT_REPORT_PATH = Path(r"E:\-4-\air-worker\level1_verify_report.json")

# Потолок кандидатов на заявку — ТЗ прямо ограничивает "не больше 2" (лучший,
# при неудаче — второй). Третий и далее не пробуем: цена растёт линейно,
# а выигрыш после второго кандидата на практике падает почти до нуля —
# если статистика прогона это опровергнет, поднять здесь одну константу.
MAX_CANDIDATES_PER_CLAIM = 2

# n_ctx локальной qwen — 4096 ТОКЕНОВ на промпт+ответ (жёсткий потолок среды,
# см. постановку). Инструкция+утверждение+обвязка промпта занимают порядка
# 300-400 токенов, ответ модели — до ~150. Русский текст у этой модели
# в среднем 2-3 символа/токен; берём ХУДШИЙ случай (2 симв/токен), чтобы не
# обрезать середину предложения посреди окна, и оставляем запас: 3400 токенов
# на текст источника * 2 симв/токен = 6800; округляем вниз до круглого числа.
# ponytail: символьный бюджет — грубая оценка, не настоящий подсчёт токенов
# (нет токенизатора qwen под рукой офлайн); апгрейд — считать через
# tiktoken/tokenizers, если резка станет обрезать реальный номер/дату.
MAX_WINDOW_CHARS = 6000


# --- 1. релевантный кусок текста под n_ctx --------------------------------------

def pick_window(text: str, claim_text: str, max_chars: int) -> str:
    """Окно текста источника вокруг САМОГО РАННЕГО совпавшего номера/даты
    из утверждения; нет совпадений — начало документа (пункт 2 ТЗ части B)."""
    if not text:
        return ""
    if len(text) <= max_chars:
        return text

    positions: list[int] = []
    for num in extract_numbers(claim_text):
        idx = text.find(num)
        if idx != -1:
            positions.append(idx)
    for d in extract_dates(claim_text):
        y, mo, day = d.split("-")
        for form in (f"{day}.{mo}.{y}", f"{day}-{mo}-{y}", f"{y}-{mo}-{day}"):
            idx = text.find(form)
            if idx != -1:
                positions.append(idx)

    if not positions:
        return text[:max_chars]

    center = min(positions)
    half = max_chars // 2
    start = max(0, center - half)
    end = min(len(text), start + max_chars)
    start = max(0, end - max_chars)  # у конца документа сдвигаем окно назад, не обрезаем его короче
    return text[start:end]


# --- 2. промпт и разбор ответа модели --------------------------------------------

_PROMPT_TMPL = """Ты — механический сверщик документов. Не рассуждай сверх необходимого,\
не досочиняй факты, которых нет в тексте ниже.

Тебе дан ОДИН фрагмент текста первоисточника и ОДНО утверждение заявки.\
Сверь утверждение с текстом.

Правила:
- В тексте ЕСТЬ прямое подтверждение утверждения — ответь ПОДТВЕРЖДЕНО.
- В тексте есть данные, которые ПРЯМО противоречат утверждению — ответь ОПРОВЕРГНУТО.
- По тексту нельзя сказать точно (нужных данных нет, фрагмент не по теме,\
текст обрезан) — ответь НЕ_СМОГ.

Формат ответа СТРОГО две строки, без markdown, без вступлений и заключений:
СТРОКА 1 — ровно одно слово: ПОДТВЕРЖДЕНО или ОПРОВЕРГНУТО или НЕ_СМОГ
СТРОКА 2 — причина одним предложением

Тип карточки: {card_type}
Утверждение заявки: {claim_text}

Фрагмент текста источника:
\"\"\"
{window}
\"\"\"

Ответ:"""

_KEYWORD_RE = re.compile(r"(ПОДТВЕРЖДЕНО|ОПРОВЕРГНУТО|НЕ_СМОГ)")
_OUTCOME_MAP = {
    "ПОДТВЕРЖДЕНО": "подтверждено",
    "ОПРОВЕРГНУТО": "опровергнуто",
    "НЕ_СМОГ": "не смог",
}


def parse_model_response(raw: str) -> tuple[str | None, str]:
    """Устойчивый разбор: не разобрали — (None, "ответ модели не разобран"),
    НЕ исключение. Ищем ключевое слово в первых нескольких строках (модель
    иногда добавляет "Ответ:" или кавычки перед словом), причину берём с
    хвоста той же строки либо со следующей непустой строки."""
    lines = [ln.strip() for ln in raw.strip().splitlines() if ln.strip()]
    if not lines:
        return None, "пустой ответ модели"

    head = "\n".join(lines[:4])
    m = _KEYWORD_RE.search(head.upper())
    if not m:
        return None, "ответ модели не разобран"

    kw = m.group(1)
    outcome = _OUTCOME_MAP[kw]

    reason = ""
    for i, ln in enumerate(lines):
        pos = ln.upper().find(kw)
        if pos == -1:
            continue
        tail = ln[pos + len(kw):].strip(" :\u2014-.")
        if tail:
            reason = tail
        elif i + 1 < len(lines):
            reason = lines[i + 1]
        break
    if not reason:
        reason = "причина не указана моделью"
    return outcome, reason


# --- 3. механическая защита от ложного "подтверждено" ---------------------------

def _value_mechanically_present(card_type: str, claim_text: str, window: str) -> bool:
    """Модель сказала "подтверждено" — принимаем ТОЛЬКО если сверяемое
    значение реально есть в присланном окне, проверено питоном, а не на
    слово модели (2а/приёмка). Утверждение с числом/датой — сверяем
    число/дату; утверждение без них (цитата принципа/паттерна) — дословный
    поиск цитаты, реюз ladder.verify_principle_pattern, а не второй парсер."""
    claim_nums = extract_numbers(claim_text)
    claim_dates = extract_dates(claim_text)
    if claim_nums or claim_dates:
        win_nums = extract_numbers(window)
        win_dates = extract_dates(window)
        return bool((claim_nums & win_nums) or (claim_dates & win_dates))

    vr = ladder.verify_principle_pattern(
        card_type, {"claim_to_verify": claim_text}, {"source_excerpt": window}
    )
    return bool(vr.get("passed"))


# --- 4. очередь: enqueue + wait_job(), не переписываем llm-queue ----------------

def _enqueue(prompt: str, kind: str = "level1-verify-claim") -> str:
    """`dispatcher.py enqueue --kind K --prompt "..."` подпроцессом — путь и
    интерпретатор берём у ladder.py (DISPATCHER/DISPATCHER_PYTHON), чтобы не
    заводить второй захардкоженный путь к тому же файлу."""
    proc = subprocess.run(
        [ladder.DISPATCHER_PYTHON, ladder.DISPATCHER, "enqueue",
         "--kind", kind, "--prompt", prompt],
        capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=60,
    )
    return (proc.stdout or "") + (proc.stderr or "")


def _enqueue_and_wait(prompt: str, timeout_s: float, poll_s: float) -> tuple[str | None, str | None, str]:
    """Возвращает (сырой_ответ_или_None, job_id_или_None, заметка). Единая
    точка сетевого/очередного взаимодействия — судя по имени, единственное,
    что мокается в --selftest (сеть и очередь там не дёргаются)."""
    out = _enqueue(prompt)
    job_id = _parse_job_id(out)
    if job_id is None:
        return None, None, f"не удалось разобрать id задания очереди (вывод: {out[:200]!r})"

    wait = wait_job(job_id, timeout_s=timeout_s, poll_s=poll_s)
    if wait["status"] != "done":
        return None, job_id, f"задание очереди не завершилось (status={wait['status']}, timed_out={wait['timed_out']})"

    result_path = wait.get("result_path")
    if not result_path or not Path(result_path).is_file():
        return None, job_id, "задание завершилось, но файла результата нет"
    try:
        raw = Path(result_path).read_text(encoding="utf-8", errors="replace")
    except OSError:
        return None, job_id, "файл результата не читается"
    return raw, job_id, "ok"


# --- 5. один вызов ступени 1 ------------------------------------------------------

def judge_claim(
    claim_text: str,
    card_type: str,
    candidate_path: str | Path | None,
    candidate_text: str,
    wait_timeout_s: float = 180.0,
    poll_s: float = 3.0,
) -> dict:
    """Контракт: {"outcome": "подтверждено"|"опровергнуто"|"не смог",
    "reason": str, "evidence_path": str|None, "model": "qwen2.5-7b-instruct",
    "job_id": str|None}. Один вызов qwen через очередь llm-queue."""
    evidence = str(candidate_path) if candidate_path else None

    window = pick_window(candidate_text or "", claim_text, MAX_WINDOW_CHARS)
    if not window.strip():
        return {
            "outcome": "не смог",
            "reason": "источник-кандидат дал пустой текст — ступени 1 нечего сверять",
            "evidence_path": evidence, "model": MODEL_NAME, "job_id": None,
        }

    prompt = _PROMPT_TMPL.format(card_type=card_type or "?", claim_text=claim_text, window=window)
    raw, job_id, note = _enqueue_and_wait(prompt, wait_timeout_s, poll_s)
    if raw is None:
        return {"outcome": "не смог", "reason": note, "evidence_path": evidence,
                "model": MODEL_NAME, "job_id": job_id}

    outcome, reason = parse_model_response(raw)
    if outcome is None:
        return {"outcome": "не смог", "reason": reason, "evidence_path": evidence,
                "model": MODEL_NAME, "job_id": job_id}

    if outcome == "подтверждено" and not _value_mechanically_present(card_type, claim_text, window):
        return {
            "outcome": "не смог",
            "reason": "модель подтвердила, но значение не найдено в тексте механически",
            "evidence_path": evidence, "model": MODEL_NAME, "job_id": job_id,
        }

    return {"outcome": outcome, "reason": reason, "evidence_path": evidence,
            "model": MODEL_NAME, "job_id": job_id}


# --- 6. прогон над отчётом ступени 0 ----------------------------------------------

def run_level1_over_report(report_path: str | Path = REPORT_PATH, limit: int | None = None) -> dict:
    """Только ЧТЕНИЕ level0_verify_report.json и реестров волта (через
    level0_verify.iter_claims()) — пишет ТОЛЬКО level1_verify_report.json,
    ничего в реестры волта (это другой воркер, не эта задача)."""
    report_path = Path(report_path)
    data = json.loads(report_path.read_text(encoding="utf-8"))
    rows = data.get("rows", [])
    targets = [r for r in rows if r.get("outcome") == "не смог" and r.get("n_candidates", 0) > 0]
    if limit is not None:
        targets = targets[:limit]

    claims_by_id: dict[str, dict] = {}
    for row in level0_verify.iter_claims():
        vid = row.get("verification_id")
        if vid:
            claims_by_id[vid] = row

    file_index = load_originals(ORIGINALS_DIR)

    out_rows: list[dict] = []
    outcomes = {"подтверждено": 0, "опровергнуто": 0, "не смог": 0}
    t0_total = time.time()

    for t in targets:
        vid = t.get("verification_id")
        row_t0 = time.time()
        claim = claims_by_id.get(vid)
        if claim is None:
            out_rows.append({
                "verification_id": vid, "registry": t.get("registry"), "card_type": t.get("card_type"),
                "outcome": "не смог",
                "reason": "заявка не найдена повторным чтением реестров волта",
                "evidence_path": None, "model": MODEL_NAME, "job_id": None,
                "candidates_tried": [], "elapsed_s": round(time.time() - row_t0, 1),
            })
            outcomes["не смог"] += 1
            continue

        # Ступень 0 хранит в JSON-отчёте только n_candidates (число), не пути
        # (правка отчёта — вне разрешённого куска level0_verify.py, часть A
        # ТЗ). Пути получаем повторным детерминированным вызовом того же
        # verify_claim_level0 — тот же файл-индекс, тот же результат, что
        # ступень 0 уже посчитала при --dry-run.
        lvl0 = level0_verify.verify_claim_level0(claim, file_index)
        candidates = (lvl0.get("candidates") or [])[:MAX_CANDIDATES_PER_CLAIM]

        claim_text = str(claim.get("claim_to_verify", ""))
        card_type = str(claim.get("card_type", ""))

        result: dict | None = None
        tried: list[dict] = []
        for cand in candidates:
            cand_path = Path(cand)
            text = level0_verify.extract_text(cand_path)
            res = judge_claim(claim_text, card_type, cand_path, text)
            tried.append({"candidate": str(cand_path), "outcome": res["outcome"]})
            result = res
            if res["outcome"] != "не смог":
                break

        if result is None:
            result = {
                "outcome": "не смог",
                "reason": "у заявки не оказалось кандидатов при повторном ранжировании",
                "evidence_path": None, "model": MODEL_NAME, "job_id": None,
            }

        outcomes[result["outcome"]] += 1
        out_rows.append({
            "verification_id": vid, "registry": t.get("registry"), "card_type": card_type,
            "level0_reason": t.get("reason"),
            "outcome": result["outcome"], "reason": result["reason"],
            "evidence_path": result["evidence_path"], "model": result["model"], "job_id": result["job_id"],
            "candidates_tried": tried, "elapsed_s": round(time.time() - row_t0, 1),
        })

    total_elapsed = round(time.time() - t0_total, 1)
    summary = {
        "total_processed": len(out_rows),
        "outcomes": outcomes,
        "total_elapsed_s": total_elapsed,
        "avg_elapsed_s": round(total_elapsed / len(out_rows), 1) if out_rows else 0.0,
    }

    OUT_REPORT_PATH.parent.mkdir(parents=True, exist_ok=True)
    OUT_REPORT_PATH.write_text(
        json.dumps({"summary": summary, "rows": out_rows}, ensure_ascii=False, indent=2),
        encoding="utf-8",
    )
    return {"summary": summary, "rows": out_rows}


# --- 7. CLI -------------------------------------------------------------------------

def _smoke() -> None:
    t0 = time.time()
    res = judge_claim(
        claim_text="Договор №5239/47 упомянут в тексте источника",
        card_type="decision",
        candidate_path="smoke-test (синтетический текст, не файл)",
        candidate_text="Договор подряда №5239/47 от 01.02.2026 между сторонами ЦКБА и РСУ-8.",
        wait_timeout_s=180, poll_s=3,
    )
    elapsed = time.time() - t0
    print(f"smoke: outcome={res['outcome']!r}")
    print(f"       reason={res['reason']!r}")
    print(f"       job_id={res['job_id']!r} model={res['model']!r}")
    print(f"       elapsed={elapsed:.1f}s")


def _run(limit: int | None) -> None:
    out = run_level1_over_report(REPORT_PATH, limit=limit)
    print(json.dumps(out["summary"], ensure_ascii=False, indent=2))
    print(f"report written to: {OUT_REPORT_PATH}")


def _selftest() -> None:
    # 1-6: разбор ответа модели во всех формах
    assert parse_model_response("ПОДТВЕРЖДЕНО\nномер сходится") == ("подтверждено", "номер сходится")
    assert parse_model_response("подтверждено\nномер сходится")[0] == "подтверждено"
    assert parse_model_response("Ответ: ОПРОВЕРГНУТО\nдата не сходится")[0] == "опровергнуто"
    assert parse_model_response("НЕ_СМОГ\nданных недостаточно в тексте")[0] == "не смог"
    assert parse_model_response("бла бла бла без ключевого слова") == (None, "ответ модели не разобран")
    assert parse_model_response("") == (None, "пустой ответ модели")
    assert parse_model_response("   \n  \n") == (None, "пустой ответ модели")

    # 7-8: pick_window
    text = ("шум " * 500) + "номер 12345 важный контекст " + ("шум " * 500)
    win = pick_window(text, "документ содержит номер 12345", max_chars=200)
    assert "12345" in win, "окно должно было захватить совпавший номер"
    assert len(win) <= 200
    win2 = pick_window("A" * 10000, "утверждение без чисел и дат", max_chars=100)
    assert win2 == "A" * 100, "нет совпадений — берём начало документа"
    assert pick_window("короткий текст", "что угодно", max_chars=1000) == "короткий текст"

    # 9. пустой текст источника — "не смог", очередь вообще не дёргаем
    res_empty = judge_claim("утверждение", "task", "f.pdf", "   ")
    assert res_empty["outcome"] == "не смог"
    assert res_empty["job_id"] is None

    # 10-12: очередь МОКАЕТСЯ — подменяем модульный _enqueue_and_wait
    global _enqueue_and_wait
    orig = _enqueue_and_wait
    try:
        # 10. модель говорит "подтверждено", но значения в окне НЕТ —
        #     механическая проверка понижает исход до "не смог" (2а/приёмка)
        globals()["_enqueue_and_wait"] = lambda p, t, s: ("ПОДТВЕРЖДЕНО\nномер сходится", "111", "ok")
        res10 = judge_claim("документ содержит номер 424242", "task", "fake.pdf", "текст без этого номера вообще")
        assert res10["outcome"] == "не смог"
        assert "механически" in res10["reason"]

        # 11. модель говорит "подтверждено", значение РЕАЛЬНО есть в окне —
        #     исход остаётся "подтверждено"
        globals()["_enqueue_and_wait"] = lambda p, t, s: ("ПОДТВЕРЖДЕНО\nномер сходится", "112", "ok")
        res11 = judge_claim("документ содержит номер 424242", "task", "fake.pdf", "текст с номером 424242 внутри")
        assert res11["outcome"] == "подтверждено"

        # 12. неразобранный ответ модели — "не смог", не исключение
        globals()["_enqueue_and_wait"] = lambda p, t, s: ("невнятный ответ без ключевых слов", "113", "ok")
        res12 = judge_claim("x", "task", "fake.pdf", "текст с числом 555")
        assert res12["outcome"] == "не смог"
        assert res12["reason"] == "ответ модели не разобран"

        # 13. очередь не вернула результат (таймаут/failed) — "не смог", не исключение
        globals()["_enqueue_and_wait"] = lambda p, t, s: (None, "114", "задание очереди не завершилось (status=queued, timed_out=True)")
        res13 = judge_claim("x", "task", "fake.pdf", "текст с числом 555")
        assert res13["outcome"] == "не смог"
        assert res13["job_id"] == "114"

        # 14. "опровергнуто" от модели не проверяется механически на значение
        #     (защита только против ложного "подтверждено" — п.3 ТЗ части B)
        globals()["_enqueue_and_wait"] = lambda p, t, s: ("ОПРОВЕРГНУТО\nдата не сходится", "115", "ok")
        res14 = judge_claim("документ датирован 01.01.2099", "decision", "fake.pdf", "документ датирован 05.05.2026")
        assert res14["outcome"] == "опровергнуто"
    finally:
        globals()["_enqueue_and_wait"] = orig

    # 15. _value_mechanically_present: цитатный claim без чисел/дат идёт через
    #     ladder.verify_principle_pattern (реюз, не второй парсер)
    assert _value_mechanically_present("principle", "дословная цитата нормы", "текст содержит дословная цитата нормы и продолжение")
    assert not _value_mechanically_present("principle", "цитата, которой нет", "совсем другой текст источника")

    print("selftest ok")


if __name__ == "__main__":
    if "--selftest" in sys.argv:
        _selftest()
    elif "--smoke" in sys.argv:
        _smoke()
    elif "--run" in sys.argv:
        lim = None
        if "--limit" in sys.argv:
            lim = int(sys.argv[sys.argv.index("--limit") + 1])
        _run(lim)
    else:
        print(__doc__)
