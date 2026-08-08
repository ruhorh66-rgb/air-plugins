"""Проверки того, что нельзя проверить глазами: обрезка сводки и фильтр путей.

Запуск: python test_approve_queue.py

Сводка обязана оставаться в лимите Telegram и при этом НИКОГДА не терять
нерешённые заявки молча — ради них она и существует. Фильтр путей в слушателе
должен отбивать всё, чем можно склеить команду. Оба места легко сломать
безобидной правкой, поэтому проверка живёт рядом с кодом.
"""
from __future__ import annotations

import json
import os
import sys
import tempfile
import time

sys.stdout.reconfigure(encoding="utf-8")

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)


def _with_queue(records, fn):
    """Подсунуть модулю временный каталог очереди и выполнить fn()."""
    import approve_via_telegram as m
    with tempfile.TemporaryDirectory() as tmp:
        for i, rec in enumerate(records):
            rec.setdefault("id", f"id{i:03d}")
            rec.setdefault("created_at", time.time() - rec.pop("age_min", 0) * 60)
            with open(os.path.join(tmp, f"{rec['id']}.json"), "w", encoding="utf-8") as fh:
                json.dump(rec, fh, ensure_ascii=False)
        old, m.QUEUE = m.QUEUE, tmp
        try:
            return fn(m)
        finally:
            m.QUEUE = old


def _summary(records) -> str:
    import io
    import contextlib
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        _with_queue(records, lambda m: m.queue([]))
    return buf.getvalue()


def test_pending_never_silently_dropped():
    """Сотня нерешённых не влезает в лимит — но потеря должна быть НАЗВАНА."""
    recs = [{"status": "pending", "title": "Заявка " + "д" * 70, "workers": 6, "age_min": 5}
            for _ in range(100)]
    out = _summary(recs)
    assert len(out) < 4096, f"сводка вышла за лимит Telegram: {len(out)}"
    assert "нерешённых не поместилось" in out, "потерянные pending не названы"
    assert "Ждёт решения: 100" in out, "счётчик ждущих должен считать ВСЕ, а не показанные"


def test_done_trimmed_before_pending():
    """Завершённые уходят под нож первыми, нерешённые остаются целиком."""
    recs = [{"status": "pending", "title": f"ждёт-{i}", "age_min": 1} for i in range(3)]
    recs += [{"status": "approved", "title": "с" * 200, "age_min": 30} for _ in range(5)]
    out = _summary(recs)
    for i in range(3):
        assert f"ждёт-{i}" in out, f"нерешённая ждёт-{i} пропала из сводки"


def test_old_done_hidden_with_count():
    """Решённое старше суток не показывается, но считается в хвосте."""
    recs = [{"status": "approved", "title": "старая", "age_min": 60 * 40}]
    out = _summary(recs)
    assert "завершённых ранее" in out and "старая" not in out


def test_broken_json_does_not_break_summary():
    """Битый файл в каталоге пропускается, а не роняет сводку."""
    import io
    import contextlib
    import approve_via_telegram as m
    with tempfile.TemporaryDirectory() as tmp:
        with open(os.path.join(tmp, "ok.json"), "w", encoding="utf-8") as fh:
            json.dump({"id": "ok", "status": "pending", "title": "живая",
                       "created_at": time.time()}, fh)
        with open(os.path.join(tmp, "broken.json"), "w", encoding="utf-8") as fh:
            fh.write('{"id": "brok')
        old, m.QUEUE = m.QUEUE, tmp
        buf = io.StringIO()
        try:
            with contextlib.redirect_stdout(buf):
                m.queue([])
        finally:
            m.QUEUE = old
    assert "живая" in buf.getvalue()


def test_path_filter_rejects_injection():
    """Фильтр путей слушателя обязан отбивать всё, чем склеивают команду."""
    import approve_listener as L
    assert L._safe_path(r"E:\-4-\ruflo-hive\report.md")
    for bad in [r'E:\x"; calc; "', r"E:\x;calc", r"E:\x`calc`", r"E:\x|calc",
                r"E:\x&calc", "E:\\x\nY", r"\\server\share\x", "report.md", "",
                r"E:\x$env:PATH", r"E:\x*"]:
        assert not L._safe_path(bad), f"пропущен опасный путь: {bad!r}"


if __name__ == "__main__":
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    for t in tests:
        t()
        print(f"ok  {t.__name__}")
    print(f"\n{len(tests)} проверок пройдено")
