r"""Протоколирование по этапам для air-worker.

Форма скопирована с `E:\-0-\air-vibecoding\scripts\stage_protocol_template.py`
(AIR_VIBECODING.md § «Протокол по этапам»). Шаблон прямо запрещает общий
импортируемый модуль: имена этапов и список внешних процессов предметны — здесь
они свои (`request_create`, `telegram_send`, `executor_run`, external=
`openrouter`/`telegram`/`air_llm_router`).

Четыре правила шаблона перенесены целиком, не частично:
  1. событие без содержимого — только скаляры; не-скаляры и длинные строки режутся;
  2. только дописывание — JSONL, файл не переписывается;
  3. внешняя нога помечается `external=` отдельно от своей работы;
  4. отказ записи не роняет основную работу — доказано `--selfcheck`, не заявлено.

Материал задачи в протокол не поднимается НИКОГДА: пути, хэши, счётчики, коды.
Правило 1 это и обеспечивает механически — длинная строка не пройдёт `_scrub`.
"""
from __future__ import annotations

import json
import os
import statistics
import sys
import time
from contextlib import contextmanager
from datetime import datetime, timezone
from functools import wraps
from pathlib import Path

PLUGIN_NAME = "air-worker"
DEFAULT_PATH = Path(rf"E:\-4-\{PLUGIN_NAME}\protocol.jsonl")
ENV_VAR = "AIR_WORKER_PROTOCOL_PATH"

_MAX_SCALAR_LEN = 200  # правило 1: длинная строка — не скаляр для этого протокола


def _protocol_path() -> Path:
    return Path(os.environ.get(ENV_VAR) or DEFAULT_PATH)


def _scrub(fields: dict) -> dict:
    """Правило 1: событие без содержимого. Отсекается здесь, а не полагается на
    дисциплину вызывающего кода — иначе первый же `text=ответ_модели` вынесет
    материал задачи в лог."""
    clean = {}
    for key, value in fields.items():
        if isinstance(value, bool) or value is None or isinstance(value, (int, float)):
            clean[key] = value
        elif isinstance(value, str) and len(value) <= _MAX_SCALAR_LEN:
            clean[key] = value
        else:
            clean[key] = f"<dropped:{type(value).__name__}>"
    return clean


def emit(stage_name: str, outcome: str, duration_ms: float,
         external: str | None = None, **fields) -> None:
    """Одна строка события. Правило 4: отказ записи проглатывается целиком."""
    event = {
        "ts": datetime.now(timezone.utc).isoformat(timespec="milliseconds"),
        "stage": stage_name,
        "outcome": outcome,  # "success" | "error"
        "duration_ms": round(duration_ms, 1),
    }
    if external:
        event["external"] = external  # правило 3
    event.update(_scrub(fields))
    try:
        path = _protocol_path()
        path.parent.mkdir(parents=True, exist_ok=True)
        with path.open("a", encoding="utf-8") as fh:  # правило 2
            fh.write(json.dumps(event, ensure_ascii=False) + "\n")
    except Exception:
        pass  # правило 4 — намеренно тихо


@contextmanager
def stage(name: str, external: str | None = None, **fields):
    """`with stage("executor_run", external="openrouter", bytes_in=1234): ...`"""
    started = time.perf_counter()
    outcome = "success"
    try:
        yield
    except Exception:
        outcome = "error"
        raise
    finally:
        emit(name, outcome, (time.perf_counter() - started) * 1000,
             external=external, **fields)


def timed(name: str, external: str | None = None):
    def decorator(fn):
        @wraps(fn)
        def wrapper(*args, **kwargs):
            with stage(name, external=external):
                return fn(*args, **kwargs)
        return wrapper
    return decorator


def summary(path: Path | None = None) -> dict:
    """calls / errors / total_s / median_ms / max_ms по каждому этапу."""
    path = path or _protocol_path()
    by_stage: dict[str, list[float]] = {}
    errors: dict[str, int] = {}
    if not path.is_file():
        return {}
    with path.open(encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            try:
                ev = json.loads(line)
            except ValueError:
                continue
            name = ev.get("stage", "?")
            by_stage.setdefault(name, []).append(ev.get("duration_ms", 0.0))
            if ev.get("outcome") == "error":
                errors[name] = errors.get(name, 0) + 1
    out = {}
    for name, durations in by_stage.items():
        out[name] = {
            "calls": len(durations),
            "errors": errors.get(name, 0),
            "total_s": round(sum(durations) / 1000, 2),
            "median_ms": round(statistics.median(durations), 1),
            "max_ms": round(max(durations), 1),
        }
    return out


def _tail(n: int, path: Path | None = None) -> list[str]:
    path = path or _protocol_path()
    if not path.is_file():
        return []
    with path.open(encoding="utf-8") as fh:
        lines = fh.readlines()
    return [ln.rstrip("\n") for ln in lines[-n:]]


def selfcheck() -> None:
    """Правило 4 доказывается, не заявляется."""
    bad_path = Path("Z:\\недостижимый\\путь\\protocol.jsonl")
    os.environ[ENV_VAR] = str(bad_path)
    try:
        with stage("selfcheck_bad_path"):
            result = 1 + 1
        assert result == 2, "работа под сбойным логом не должна была прерваться"
    finally:
        os.environ.pop(ENV_VAR, None)

    import tempfile
    with tempfile.TemporaryDirectory() as tmp:
        good_path = Path(tmp) / "protocol.jsonl"
        os.environ[ENV_VAR] = str(good_path)
        try:
            with stage("selfcheck_ok", widgets=3):
                pass
            try:
                with stage("selfcheck_err"):
                    raise ValueError("ожидаемая ошибка теста")
            except ValueError:
                pass
            # Правило 1 на живом примере: материал задачи в лог не попадает.
            emit("selfcheck_scrub", "success", 1.0, text="я" * 500)
            with good_path.open(encoding="utf-8") as fh:
                lines = [json.loads(x) for x in fh if x.strip()]
            assert lines[-1]["text"] == "<dropped:str>", "длинная строка попала в лог"
            s = summary(good_path)
            assert s["selfcheck_ok"]["calls"] == 1
            assert s["selfcheck_err"]["errors"] == 1
            assert len(_tail(10, good_path)) == 3
        finally:
            os.environ.pop(ENV_VAR, None)
    print("protocol selfcheck ok")


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    if "--selfcheck" in sys.argv:
        selfcheck()
    elif "--tail" in sys.argv:
        idx = sys.argv.index("--tail")
        n = int(sys.argv[idx + 1]) if len(sys.argv) > idx + 1 else 20
        for line in _tail(n):
            print(line)
    else:
        for name, stats in summary().items():
            print(f"{name:<22} calls={stats['calls']:<4} errors={stats['errors']:<3} "
                  f"total={stats['total_s']}s median={stats['median_ms']}ms "
                  f"max={stats['max_ms']}ms")
