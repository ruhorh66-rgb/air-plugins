r"""air-worker — ядро: заявка, подпись, лок, состояние канала.

ЗАЧЕМ ПЛАГИН. Код контура едет через рой ruflo. Тяжёлые НЕроевые задачи —
обработка файла сторонней моделью, OCR, длинная перегонка — рою не нужны и в
сессии оркестратора не помещаются. Схема та же, что у роя (AIR_VIBECODING.md
§ «Схема исполнения»): оркестратор → отцеплённый процесс → подтверждение ЛПР
кнопкой → сводка. Меняется только то, ЧТО запускается по нажатию.

ЧТО ЗДЕСЬ ЕСТЬ И ЧЕГО НЕТ:
  - заявка живёт СТРОКОЙ В SQLite, а не файлом на заявку: россыпь файлов не даёт
    посчитать «на что уходит время» без отдельного парсера по каталогу
    (NEW_PLUGIN_CHECKLIST.md § 3);
  - команды как строки в заявке НЕТ. Хранится тип задачи и параметры; код,
    который по типу запускается, лежит в `executors.py` и выбирается словарём;
  - секретов в коде нет: токен бота, chat_id и ключ подписи — Windows keyring,
    сервис `air-comms-telegram-bot`. Своего бота плагин НЕ заводит — это тот же
    бот, что у air-ruflo-bridge, и отсюда же берётся общий ресурс, который надо
    делить (см. `foreign_listener_state`).

ПОДПИСЬ — ПОРТИРОВАНА, НЕ НАПИСАНА ВТОРОЙ РАЗ.
Источник: `E:\-7-\air-ruflo-bridge\skills\run-via-ruflo\scripts\approve_via_telegram.py`
(функции `sign_request` / `verify_request` / `_canonical` / `file_digest`, редакция
10.08.2026). Импортом взять нельзя: тот файл — часть чужого плагина, живёт по своему
пути установки и правится другим прогоном (TASK-OBS-0045), а набор подписываемых полей
здесь другой. Перенесён АЛГОРИТМ: HMAC-SHA256 по канонической JSON-форме фиксированного
списка полей + отдельная сверка sha256 самого материала. Оба свойства оригинала
сохранены и проверяются `selftest.py`:
  1. ключ заводит только создающая сторона (`create_if_missing=True`), проверяющая —
     никогда: иначе удаление ключа стало бы способом обойти проверку;
  2. третьего исхода у проверки нет: «проверить не смог» = отказ.
"""
from __future__ import annotations

import csv
import hashlib
import hmac
import json
import os
import secrets
import sqlite3
import subprocess
import sys
import time
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path

try:
    sys.stdout.reconfigure(encoding="utf-8")
    sys.stderr.reconfigure(encoding="utf-8")
except Exception:
    pass

SERVICE = "air-comms-telegram-bot"   # тот же бот, что у air-ruflo-bridge
HMAC_ACCOUNT = "request-hmac-key"    # тот же ключ подписи — второй записи на тот
                                     # же секрет чек-лист заводить запрещает (§ 6)

def _default_runtime() -> Path:
    """Keep the installed Windows location, with a usable POSIX default.

    Hosts should normally set ``AIR_WORKER_RUNTIME``.  The fallback is only for
    a standalone local installation; it deliberately does not make a drive
    letter part of the task contract.
    """
    if os.name == "nt":
        return Path(r"E:\-4-\air-worker")  # existing AIR OS installation
    return Path.home() / ".air-worker"


def runtime_layout(root: Path) -> dict[str, Path]:
    """Build runtime paths without assuming a Windows drive or shell syntax."""
    return {
        "db": root / "worker.sqlite3",
        "lock": root / "listener.lock.json",
        "offset": root / "offset.txt",
        "out": root / "out",
    }


_RUNTIME_ROOT = Path(os.environ["AIR_WORKER_RUNTIME"]) if os.environ.get("AIR_WORKER_RUNTIME") else _default_runtime()
_RUNTIME_LAYOUT = runtime_layout(_RUNTIME_ROOT)
RUNTIME = str(_RUNTIME_ROOT)
DB_PATH = str(_RUNTIME_LAYOUT["db"])
LOCK_PATH = str(_RUNTIME_LAYOUT["lock"])
OFFSET_PATH = str(_RUNTIME_LAYOUT["offset"])
OUT_DIR = str(_RUNTIME_LAYOUT["out"])
METRICS_PATH = (os.environ.get("AIR_WORKER_METRICS_PATH")
                or r"E:\-5-\010_Task_Control_Platform\00_REGISTRY\run_metrics.csv")

TTL_SECONDS = 12 * 3600
MAX_INPUT_BYTES = 5 * 1024 * 1024
OMSK = timezone(timedelta(hours=6))  # контур живёт по Омску, машина — нет

SIGNED_FIELDS = ("id", "title", "task_type", "input_path", "input_sha256",
                 "params", "created_at", "ttl_seconds")


# --- секреты -----------------------------------------------------------------

def secret(account: str) -> str:
    import keyring
    value = keyring.get_password(SERVICE, account)
    if not value:
        raise SystemExit(f"нет учётной записи {SERVICE}/{account} в keyring")
    return value


def api(method: str, payload: dict | None = None, timeout: int = 30) -> dict:
    token = secret("botfather-token")
    url = f"https://api.telegram.org/bot{token}/{method}"
    data = json.dumps(payload).encode("utf-8") if payload else None
    req = urllib.request.Request(
        url, data=data,
        headers={"Content-Type": "application/json"} if data else {},
        method="POST" if data else "GET")
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read().decode("utf-8"))
    except Exception as exc:  # сеть моргнула — вызывающий решает, что делать
        return {"ok": False, "error": f"{type(exc).__name__}: {exc}"}


def notify(text: str) -> None:
    try:
        api("sendMessage", {"chat_id": int(secret("chat_id")), "text": text}, timeout=20)
    except Exception:
        pass


# --- подпись (портирована, источник в шапке модуля) --------------------------

def file_digest(path: str) -> str:
    """SHA-256 материала. Нечитаемый файл даёт пустую строку — и она тоже
    подписывается: заявка на недоступный материал обязана отличаться от заявки
    на живой."""
    try:
        with open(path, "rb") as fh:
            digest = hashlib.sha256()
            for chunk in iter(lambda: fh.read(1 << 20), b""):
                digest.update(chunk)
            return digest.hexdigest()
    except OSError:
        return ""


def hmac_key(create_if_missing: bool = False) -> bytes:
    # This makes the signed contract portable to Claude Code/Codex hosts where
    # Windows keyring is unavailable.  It is intentionally read-only: only the
    # legacy Windows creator may provision a missing key in keyring.
    configured = os.environ.get("AIR_WORKER_HMAC_KEY")
    if configured:
        return configured.encode("utf-8")
    import keyring
    value = keyring.get_password(SERVICE, HMAC_ACCOUNT)
    if not value and create_if_missing:
        value = secrets.token_hex(32)
        keyring.set_password(SERVICE, HMAC_ACCOUNT, value)
    if not value:
        raise SystemExit(f"нет ключа подписи {SERVICE}/{HMAC_ACCOUNT} в keyring")
    return value.encode("utf-8")


def canonical(request: dict) -> bytes:
    """Подписываемое представление. Порядок и формат фиксированы, иначе подпись
    развалится от перестановки ключей при перезаписи."""
    payload = {field: request.get(field) for field in SIGNED_FIELDS}
    return json.dumps(payload, ensure_ascii=False, sort_keys=True,
                      separators=(",", ":")).encode("utf-8")


def sign_request(request: dict) -> str:
    return hmac.new(hmac_key(create_if_missing=True), canonical(request),
                    hashlib.sha256).hexdigest()


def verify_request(request: dict) -> tuple[bool, str]:
    """(годна, причина). Нет подписи либо нет ключа — НЕ ГОДНА.

    Третьего исхода нет намеренно: молчаливое превращение непроверенного в
    разрешённое — тот самый дефект, ради которого проверка и вводится.
    """
    got = request.get("sig")
    if not got:
        return False, "заявка без подписи"
    try:
        key = hmac_key(create_if_missing=False)
    except SystemExit as exc:
        return False, f"ключ подписи недоступен: {exc}"
    expected = hmac.new(key, canonical(request), hashlib.sha256).hexdigest()
    if not hmac.compare_digest(str(got), expected):
        return False, "подпись не сходится — поля заявки изменены после создания"
    signed_digest = request.get("input_sha256")
    if signed_digest is None:
        return False, "заявка старого образца: материал не подписан по содержимому"
    if not hmac.compare_digest(str(signed_digest), file_digest(str(request.get("input_path") or ""))):
        return False, ("материал изменился после создания заявки — ЛПР подтверждал "
                       "другой файл")
    return True, "подпись сходится, материал не менялся"


# --- база --------------------------------------------------------------------

SCHEMA = """
CREATE TABLE IF NOT EXISTS requests (
    id            TEXT PRIMARY KEY,
    title         TEXT NOT NULL,
    task_type     TEXT NOT NULL,
    input_path    TEXT NOT NULL,
    input_sha256  TEXT NOT NULL,
    params        TEXT NOT NULL,   -- каноническая JSON-строка подписанных параметров
    created_at    REAL NOT NULL,
    ttl_seconds   REAL NOT NULL,
    sig           TEXT NOT NULL,
    status        TEXT NOT NULL,   -- pending|approved|rejected|expired|done|failed|invalid
    decided_at    REAL,
    message_id    INTEGER,
    started_at    REAL,
    finished_at   REAL,
    result        TEXT             -- только скаляры исполнителя, не материал
);
"""


def db() -> sqlite3.Connection:
    os.makedirs(RUNTIME, exist_ok=True)
    conn = sqlite3.connect(DB_PATH, timeout=30)
    conn.row_factory = sqlite3.Row
    conn.execute("PRAGMA journal_mode=WAL")
    conn.executescript(SCHEMA)
    return conn


def _row_to_request(row: sqlite3.Row) -> dict:
    """Строка БД → заявка в том виде, в каком она подписывалась.

    `params` в подпись идут РАЗОБРАННЫМ объектом, а не строкой: канонизация
    сортирует ключи сама, и хранить в БД можно любую валидную JSON-запись.
    """
    req = {k: row[k] for k in row.keys()}
    req["params"] = json.loads(row["params"])
    return req


def load(rid: str) -> dict | None:
    with db() as conn:
        row = conn.execute("SELECT * FROM requests WHERE id=?", (rid,)).fetchone()
    return _row_to_request(row) if row else None


def set_status(rid: str, status: str, **fields) -> None:
    cols = ", ".join(f"{k}=?" for k in ("status", *fields))
    with db() as conn:
        conn.execute(f"UPDATE requests SET {cols} WHERE id=?",
                     (status, *fields.values(), rid))


def claim_for_execution(rid: str) -> bool:
    """Atomically transition a queued request once; repeated delivery is safe."""
    with db() as conn:
        result = conn.execute(
            "UPDATE requests SET status='running', started_at=? WHERE id=? "
            "AND status IN ('queued','approved')", (time.time(), rid))
    return result.rowcount == 1


def is_expired(req: dict) -> bool:
    return time.time() - float(req["created_at"]) > float(req["ttl_seconds"])


# --- лок: три состояния, unknown НИКОГДА не равен free ------------------------
#
# Форма повторена с `hive.lock.json` (air-ruflo-bridge / hive_single.ps1): замок
# несёт PID и время старта держателя, живость проверяется парой PID+время (Windows
# переиспользует PID, одного номера мало). Чек-лист § 4.

def _cim(where: str) -> list[dict] | None:
    """Опрос процессов. None = ОПРОСИТЬ НЕ УДАЛОСЬ — это `unknown`, не «пусто»."""
    cmd = ("Get-CimInstance Win32_Process -Filter \"" + where + "\" | "
           "Select-Object ProcessId,CreationDate,CommandLine | "
           "ConvertTo-Json -Compress -Depth 3")
    try:
        res = subprocess.run(
            ["powershell.exe", "-NoProfile", "-NonInteractive", "-Command", cmd],
            capture_output=True, text=True, encoding="utf-8", errors="replace",
            timeout=60)
    except Exception:
        return None
    if res.returncode != 0:
        return None
    out = (res.stdout or "").strip()
    if not out:
        return []
    try:
        data = json.loads(out)
    except ValueError:
        return None
    return data if isinstance(data, list) else [data]


def lock_state(path: str = LOCK_PATH) -> tuple[str, str]:
    """`held` / `free` / `unknown` + причина. Нечитаемый замок — `unknown`."""
    try:
        with open(path, encoding="utf-8") as fh:
            raw = fh.read()
    except FileNotFoundError:
        return "free", "замка нет"
    except OSError as exc:
        return "unknown", f"файл замка не прочитан: {type(exc).__name__}"
    try:
        lock = json.loads(raw)
    except ValueError:
        return "unknown", "файл замка есть, но замка в нём нет"
    if not isinstance(lock, dict) or not lock.get("pid"):
        return "unknown", "в замке нет держателя"
    procs = _cim(f"ProcessId={int(lock['pid'])}")
    if procs is None:
        return "unknown", "опросить процессы не удалось"
    if not procs:
        return "free", f"держатель PID {lock['pid']} мёртв"
    if lock.get("pid_started") and str(procs[0].get("CreationDate")) != str(lock["pid_started"]):
        return "free", f"PID {lock['pid']} переиспользован другим процессом"
    return "held", f"держит PID {lock['pid']} с {lock.get('since', '?')}"


def _my_start_stamp() -> str:
    procs = _cim(f"ProcessId={os.getpid()}")
    return str(procs[0].get("CreationDate")) if procs else ""


def acquire(path: str = LOCK_PATH, note: str = "") -> tuple[bool, str]:
    """Взять замок. `unknown` НЕ даёт взять — непрочитанный замок не значит «свободно»."""
    state, why = lock_state(path)
    if state != "free":
        return False, f"{state}: {why}"
    os.makedirs(os.path.dirname(path), exist_ok=True)
    body = json.dumps({"pid": os.getpid(), "pid_started": _my_start_stamp(),
                       "since": datetime.now(OMSK).isoformat(timespec="seconds"),
                       "note": note}, ensure_ascii=False)
    try:  # CREATE_NEW: гонка двух стартов решается файловой системой, не проверкой выше
        fd = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY)
    except FileExistsError:
        # Замок на месте, но выше он оказался `free` — значит труп. Перебиваем и
        # ПЕРЕЧИТЫВАЕМ: между проверкой и записью тот же вывод мог сделать второй
        # процесс, и тогда замок достался ему, а не нам. Побеждает тот, чья запись
        # легла последней, — но узнаёт об этом каждый по факту, а не по намерению.
        tmp = f"{path}.{os.getpid()}.tmp"  # своё имя: общий tmp — вторая гонка
        with open(tmp, "w", encoding="utf-8") as fh:
            fh.write(body)
        os.replace(tmp, path)
        try:
            with open(path, encoding="utf-8") as fh:
                if json.load(fh).get("pid") != os.getpid():
                    return False, "замок перехвачен другим процессом"
        except (OSError, ValueError):
            return False, "замок перезаписан кем-то ещё, прочитать не удалось"
        return True, "замок взят вместо мёртвого"
    with os.fdopen(fd, "w", encoding="utf-8") as fh:
        fh.write(body)
    return True, "замок взят"


def release(path: str = LOCK_PATH) -> None:
    try:
        os.remove(path)
    except OSError:
        pass


# Общий с air-ruflo-bridge ресурс — ОДИН Telegram-бот. Long polling у Telegram
# эксклюзивен по факту: подтверждённый апдейт достаётся тому, кто спросил первым.
# Инцидент 10.08.2026: кнопка air-worker ушла живому слушателю ruflo, и тот ответил
# «Заявка не найдена». Поэтому канал проверяется ОТДЕЛЬНО от собственного замка.
FOREIGN_MARKERS = ("approve_listener.py",)


def foreign_listener_state() -> tuple[str, str]:
    """Занят ли бот ЧУЖИМ слушателем. `unknown` при неудачном опросе."""
    procs = _cim("Name like '%python%'")
    if procs is None:
        return "unknown", "опросить процессы не удалось — канал считаем занятым"
    me = os.getpid()
    for proc in procs:
        if proc.get("ProcessId") == me:
            continue
        cmd = str(proc.get("CommandLine") or "")
        for marker in FOREIGN_MARKERS:
            if marker in cmd:
                return "held", (f"бота слушает чужой процесс PID "
                                f"{proc.get('ProcessId')} ({marker})")
    return "free", "чужих слушателей бота не видно"


# --- журнал замеров ----------------------------------------------------------

METRICS_HEADER = ["date", "milestone", "task", "level", "model", "effort",
                  "tokens_in", "tokens_out", "wall_time_s", "outcome", "note"]


def record_metric(task: str, model: str, tokens_in, tokens_out,
                  wall_time_s: int, outcome: str, note: str = "",
                  milestone: str = "air-worker", level: str = "1",
                  effort: str = "") -> None:
    """Строка в ОБЩИЙ `run_metrics.csv` контура — своего журнала плагин не заводит.

    Где цифру взять нельзя — поле остаётся пустым, а не заполняется оценкой
    (AIR_VIBECODING.md § «Свои замеры»).
    """
    try:
        os.makedirs(os.path.dirname(METRICS_PATH), exist_ok=True)
        new = not os.path.isfile(METRICS_PATH)
        with open(METRICS_PATH, "a", encoding="utf-8", newline="") as fh:
            writer = csv.writer(fh, quoting=csv.QUOTE_ALL)
            if new:
                writer.writerow(METRICS_HEADER)
            writer.writerow([datetime.now(OMSK).strftime("%Y-%m-%d"), milestone,
                             task, level, model, effort, tokens_in, tokens_out,
                             wall_time_s, outcome, note])
    except OSError:
        pass  # журнал не имеет права уронить прогон


def _task_contract_ok(spec: dict, params: dict) -> tuple[bool, str]:
    """Validate the signed contract again immediately before execution."""
    try:
        spec["validate"](params)
    except (TypeError, ValueError) as exc:
        return False, f"контракт исполнителя недействителен: {exc}"
    privacy = str(params.get("privacy", "external"))
    if privacy not in ("external", "local"):
        return False, "контракт приватности должен быть external или local"
    if privacy == "local" and spec.get("external") == "openrouter":
        return False, "локальный материал нельзя отправлять во внешний OpenRouter"
    return True, "контракт годен"


def _input_ok(path: str) -> tuple[bool, str]:
    """Bound the material before queueing and again before recovering execution."""
    try:
        size = Path(path).stat().st_size
    except OSError:
        return False, "материал недоступен"
    if size > MAX_INPUT_BYTES:
        return False, f"размер материала превышает лимит {MAX_INPUT_BYTES} байт"
    return True, "материал в пределах лимита"


def execute_request(req: dict, *, notify_telegram: bool = False) -> dict:
    """Execute one persisted request through the registered llm-queue executor.

    This is host-neutral core shared by direct local operation and the optional
    Telegram listener.  A direct invocation never calls Telegram helpers.
    """
    import executors
    import protocol

    def fail(code: str, started: float | None = None) -> dict:
        took = int(time.time() - started) if started is not None else 0
        set_status(req["id"], "failed", finished_at=time.time(),
                   result=json.dumps({"error": code}, ensure_ascii=False))
        record_metric(f"air-worker {req.get('task_type', '?')} {req['id']}",
                      str(req.get("params", {}).get("model", "")), "", "", took,
                      "failed", code)
        if notify_telegram:
            notify(f"⚠ Заявка {req['id']} не отработала: {code}")
        return {"id": req["id"], "status": "failed", "error": code}

    ok, why = verify_request(req)
    if not ok:
        set_status(req["id"], "invalid", finished_at=time.time(),
                   result=json.dumps({"error": why}, ensure_ascii=False))
        if notify_telegram:
            notify(f"⛔ Заявка {req['id']} НЕ запущена — {why}")
        return {"id": req["id"], "status": "invalid", "error": why}
    try:
        spec = executors.get(req["task_type"])
    except SystemExit as exc:
        return fail("unknown_or_disabled_task_type")
    contract_ok, contract_why = _task_contract_ok(spec, req["params"])
    if not contract_ok:
        return fail("contract_rejected")
    input_ok, input_why = _input_ok(str(req["input_path"]))
    if not input_ok:
        return fail("input_rejected")

    if not claim_for_execution(req["id"]):
        current = load(req["id"])
        status = current["status"] if current else "unknown"
        # One request must not execute twice after an interrupted caller.  A
        # recovery is a new signed request, not an implicit replay.
        return {"id": req["id"], "status": status, "replayed": True}

    Path(OUT_DIR).mkdir(parents=True, exist_ok=True)
    out_path = str(Path(OUT_DIR) / f"{req['id']}.txt")
    started = time.time()
    try:
        with protocol.stage("executor_run", external=spec["external"],
                            task_type=req["task_type"],
                            input_sha256=req["input_sha256"][:16]):
            result = spec["run"](req, req["params"], out_path)
        if not isinstance(result, dict) or result.get("ok") is not True:
            return fail("executor_unconfirmed", started)
    except SystemExit as exc:
        return fail("executor_failed", started)
    except Exception as exc:  # executor failure must become an auditable status
        return fail("executor_exception", started)

    took = int(time.time() - started)
    set_status(req["id"], "done", finished_at=time.time(),
               result=json.dumps(result, ensure_ascii=False))
    record_metric(f"air-worker {req['task_type']} {req['id']}",
                  str(result.get("model", "")), result.get("tokens_in", ""),
                  result.get("tokens_out", ""), took, "accepted",
                  f"chars_in={result.get('chars_in')} chars_out={result.get('chars_out')}")
    if notify_telegram:
        notify(f"✅ Заявка {req['id']} отработала за {took} с\n"
               f"Тип: {req['task_type']}, модель: {result.get('model', '—')}\n"
               f"Результат: {result.get('output_path')}")
    return {"id": req["id"], "status": "done", "task_type": req["task_type"],
            "result": result}


# --- команды -----------------------------------------------------------------

def cmd_create(argv: list[str]) -> int:
    """Create a signed request and execute it directly by default.

    ЗАЯВКА ХРАНИТ ТИП И ПАРАМЕТРЫ, А НЕ КОМАНДУ. Ровно то же решение, что в
    air-ruflo-bridge после блокировки классификатором 08.08.2026: протащить в
    канал строку запуска нечем, потому что строки в нём не существует.
    """
    import argparse
    parser = argparse.ArgumentParser(prog="worker.py create", add_help=True)
    parser.add_argument("task_type")
    parser.add_argument("input_path")
    parser.add_argument("--title", default="")
    parser.add_argument("--param", action="append", default=[],
                        metavar="k=v", help="параметр исполнителя, можно несколько")
    parser.add_argument("--ttl-hours", type=float, default=TTL_SECONDS / 3600)
    parser.add_argument("--approval", choices=("direct", "telegram"), default="direct",
                        help="direct (default) or optional legacy Telegram approval")
    parser.add_argument("--privacy", choices=("external", "local"), default="external",
                        help="signed material classification; local blocks external executors")
    args = parser.parse_args(argv)

    import executors
    spec = executors.get(args.task_type)          # неизвестный/выключенный тип — отказ

    input_path = os.path.abspath(args.input_path)
    if not os.path.isfile(input_path):
        raise SystemExit(f"нет файла материала: {input_path}")
    input_ok, input_why = _input_ok(input_path)
    if not input_ok:
        raise SystemExit(input_why)

    params: dict = {}
    for item in args.param:
        if "=" not in item:
            raise SystemExit(f"--param ждёт k=v, получено: {item}")
        key, value = item.split("=", 1)
        params[key.strip()] = value
    if "privacy" in params:
        raise SystemExit("privacy задаётся только флагом --privacy и входит в подпись")
    params["privacy"] = args.privacy
    spec["validate"](params)                       # проверка ДО подписи и отправки
    contract_ok, contract_why = _task_contract_ok(spec, params)
    if not contract_ok:
        raise SystemExit(contract_why)

    # Канал общий с air-ruflo-bridge: пока бота слушает чужой процесс, кнопку слать
    # нельзя — её нажатие достанется ему. `unknown` тоже запрещает: непроверенный
    # канал не значит свободный.
    if args.approval == "telegram":
        channel, why = foreign_listener_state()
        if channel != "free":
            raise SystemExit(f"канал бота недоступен ({channel}): {why}. "
                             f"Заявка НЕ создана — иначе кнопка уйдёт не тому.")

    rid = secrets.token_hex(4)
    request = {
        "id": rid,
        "title": (args.title or f"{spec['title']}")[:120],
        "task_type": args.task_type,
        "input_path": input_path,
        "input_sha256": file_digest(input_path),
        "params": params,
        "created_at": time.time(),
        "ttl_seconds": args.ttl_hours * 3600,
    }
    request["sig"] = sign_request(request)  # подпись ДО первой записи на диск
    with db() as conn:
        conn.execute(
            "INSERT INTO requests (id,title,task_type,input_path,input_sha256,"
            "params,created_at,ttl_seconds,sig,status) VALUES (?,?,?,?,?,?,?,?,?,?)",
            (rid, request["title"], request["task_type"], request["input_path"],
             request["input_sha256"],
             json.dumps(params, ensure_ascii=False, sort_keys=True),
             request["created_at"], request["ttl_seconds"], request["sig"],
             "pending" if args.approval == "telegram" else "queued"))

    if args.approval == "direct":
        # Reload proves the exact signed material persisted to SQLite is what
        # reaches the executor.  No Telegram API/keyring bot call is reachable.
        persisted = load(rid)
        assert persisted is not None
        outcome = execute_request(persisted, notify_telegram=False)
        print(json.dumps(outcome, ensure_ascii=False))
        return 0 if outcome["status"] == "done" else 1

    size = os.path.getsize(input_path)
    text = (f"⚙ {request['title']}\n"
            f"────────────────\n"
            f"ТИП ЗАДАЧИ: {args.task_type} — {spec['title']}\n"
            f"Приватность: {spec['privacy']}\n\n"
            f"Материал: {os.path.basename(input_path)} ({size} байт)\n"
            f"sha256: {request['input_sha256'][:16]}…\n"
            f"Параметры: {', '.join(sorted(params)) or '—'}\n"
            f"────────────────\n"
            f"Заявка {rid}, кнопка действует {args.ttl_hours:.0f} ч.")
    resp = api("sendMessage", {
        "chat_id": int(secret("chat_id")),
        "text": text,
        "reply_markup": {"inline_keyboard": [[
            # Префикс СВОЙ: `run:`/`no:` — у air-ruflo-bridge. Одинаковые префиксы
            # на одном боте нечитаемы в разборе инцидента.
            {"text": "✅ Запустить", "callback_data": f"awrun:{rid}"},
            {"text": "✖ Отклонить", "callback_data": f"awno:{rid}"},
        ]]},
    })
    if not resp.get("ok"):
        set_status(rid, "failed", result=json.dumps(
            {"error": "telegram отклонил отправку"}, ensure_ascii=False))
        raise SystemExit(f"Telegram отклонил отправку заявки: {resp.get('error', resp)}")
    print(json.dumps({"request_id": rid, "task_type": args.task_type,
                      "input_sha256": request["input_sha256"], "db": DB_PATH},
                     ensure_ascii=False))
    return 0


def cmd_status(argv: list[str]) -> int:
    if not argv:
        print("usage: status <request_id>", file=sys.stderr)
        return 2
    req = load(argv[0])
    if not req:
        print(json.dumps({"status": "unknown"}))
        return 1
    status = req["status"]
    if status == "pending" and is_expired(req):
        status = "expired"
    print(json.dumps({"id": req["id"], "status": status, "task_type": req["task_type"],
                      "title": req["title"], "result": req["result"]},
                     ensure_ascii=False))
    return 0


def cmd_queue(argv: list[str]) -> int:
    with db() as conn:
        rows = conn.execute(
            "SELECT id,title,task_type,status,created_at,ttl_seconds FROM requests "
            "ORDER BY created_at DESC LIMIT 30").fetchall()
    now = time.time()
    for row in rows:
        status = row["status"]
        if status == "pending" and now - row["created_at"] > row["ttl_seconds"]:
            status = "expired"
        age = int((now - row["created_at"]) / 60)
        ago = f"{age} мин назад" if age < 90 else f"{age // 60} ч назад"
        print(f"{status:<9} {row['id']}  {row['task_type']:<16} {row['title'][:50]}  {ago}")
    if not rows:
        print("пусто")
    return 0


def cmd_lock(argv: list[str]) -> int:
    own, own_why = lock_state()
    foreign, foreign_why = foreign_listener_state()
    print(json.dumps({"own_listener": own, "own_why": own_why,
                      "bot_channel_foreign": foreign, "foreign_why": foreign_why,
                      "can_listen": own == "free" and foreign == "free"},
                     ensure_ascii=False, indent=1))
    return 0 if own == "free" and foreign == "free" else 4


def cmd_types(argv: list[str]) -> int:
    import executors
    for name, spec in sorted(executors.REGISTRY.items()):
        print(f"{name:<16} [{'вкл' if spec['enabled'] else 'разъём'}] {spec['title']}")
    return 0


COMMANDS = {"create": cmd_create, "status": cmd_status, "queue": cmd_queue,
            "lock": cmd_lock, "types": cmd_types}


def main(argv: list[str]) -> int:
    if not argv or argv[0] not in COMMANDS:
        print(__doc__)
        print("команды: " + ", ".join(COMMANDS), file=sys.stderr)
        return 2
    return COMMANDS[argv[0]](argv[1:])


if __name__ == "__main__":
    sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
    sys.exit(main(sys.argv[1:]))
