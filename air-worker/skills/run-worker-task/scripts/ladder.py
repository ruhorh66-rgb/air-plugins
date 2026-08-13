r"""ladder.py — 6-ступенчатая лестница исполнителей (TASK-OBS-0054, раздел 2а).

    0  script          $0        есть ли документ в 05_ORIGINALS; совпали номер/дата/сумма
    1  qwen-local       $0        локальная модель через llm-queue
    2  openrouter-free  ~0        бесплатные модели OpenRouter, второе мнение
    3  codex            вне квоты объёмные пакеты (расчёты, таблицы, сметы)
    4  haiku/sonnet     ×0,04/×0,2 суждение: следует ли утверждение из источника
    5  opus             ×1        только там, где спорна сама правовая конструкция

ПОДЪЁМ ТОЛЬКО ПО НАЗВАННОЙ ПРИЧИНЕ (2а). `escalate()` без `reason` — исключение,
не молчаливый переход. Молчаливая эскалация — ровно тот дефект, из-за которого у
роя было 88 % Opus.

ИСПОЛНЕНИЕ (2в). Уровни 1-3 — CPU/сетевые нагрузки одной машины, для них и
написана `llm-queue`; ladder.py их не запускает сам, а ставит в очередь через
`dispatcher.py` (subprocess) и дожидается СВОЕГО задания `wait_job()` (Ф5) —
не «очередь что-то обработала», а именно наш job_id закрылся. Уровни 4-5 —
суждение Claude: тоже claude.exe (подпиской, без API-ключа), тоже через
`enqueue-exec` (cpu-пул) + `wait_job()` — исполняются САМИ (Ф3), конверт
(`level_kind: "claude-judgement"`) остаётся ФОРМАТОМ задания, не сигналом
«передай в Task». Старый режим отладки без исполнения — `claim["execute"] =
False`.

ГРАНИЦЫ (раздел 6 постановки), соблюдены КОДОМ, не только текстом:
  - ключ OpenRouter этот модуль не читает и не хранит — level2 зовёт уже
    существующий локальный роутер (air_llm_router.py), у которого свой
    free-only guard;
  - реальные CSV-реестры волта ЦКБА этот модуль не открывает и не пишет —
    `claim`/`evidence` здесь абстрактные словари, интеграция с
    `source_verification_queue*.csv` — отдельная задача (не эта);
  - `llm-queue` не переписан: всё исполнение уровней 1-3 идёт ЕГО
    документированным CLI (`enqueue` / `enqueue-exec` / `run` / `show`),
    внутрь `dispatcher.py`/`llm_client.py` этот файл не лезет.
"""
from __future__ import annotations

import contextlib
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone

# --- пути и настройки ---------------------------------------------------------

_HERE = os.path.dirname(os.path.abspath(__file__))
DISPATCHER = os.environ.get("LLM_QUEUE_DISPATCHER",
                            r"E:\-8-\llm-queue\llm-queue\dispatcher.py")
DISPATCHER_PYTHON = os.environ.get("LLM_QUEUE_PYTHON", sys.executable)
ROUTER_URL = os.environ.get("AIR_WORKER_ROUTER_URL",
                            "http://127.0.0.1:8090/v1/chat/completions")
ROUTER_MODELS_URL = os.environ.get("AIR_WORKER_ROUTER_MODELS_URL",
                                   "http://127.0.0.1:8090/v1/models")
PRIORS_PATH = os.environ.get("LADDER_PRIORS_PATH",
                             os.path.join(_HERE, "ladder_priors.json"))

# Ф3: исполнитель уровней 4-5 — тот же claude.exe, что и оркестратор, только
# вызванный подпроцессом через enqueue-exec (cpu-пул), не Task/Agent. Промпт
# идёт файлом (см. `_write_prompt_file`) — рантайм-каталог, НЕ git и НЕ волт.
CLAUDE_JUDGE_PYTHON = os.environ.get("CLAUDE_JUDGE_PYTHON", sys.executable)
CLAUDE_JUDGE_WRAPPER = os.path.join(_HERE, "claude_judge_run.py")
CLAUDE_JUDGE_PROMPT_DIR = os.environ.get("CLAUDE_JUDGE_PROMPT_DIR",
                                         r"E:\-4-\air-worker\ladder_prompts")

TOP_LEVEL = 5


# --- llm-queue: один subprocess-хелпер, ничего внутрь dispatcher.py не лезет --

def _run_dispatcher(*args: str, timeout: int = 600) -> str:
    proc = subprocess.run([DISPATCHER_PYTHON, DISPATCHER, *args],
                          capture_output=True, text=True, encoding="utf-8",
                          errors="replace", timeout=timeout)
    if proc.returncode != 0:
        raise SystemExit(f"llm-queue dispatcher отказал ({proc.returncode}): "
                         f"{(proc.stderr or proc.stdout)[:300]}")
    return proc.stdout


def _parse_show(text: str) -> dict:
    """`dispatcher.py show` печатает `  ключ<pad>значение` построчно."""
    out: dict[str, str] = {}
    for line in text.splitlines():
        line = line.strip()
        if not line or " " not in line:
            continue
        key, _, rest = line.partition(" ")
        out[key] = rest.strip()
    return out


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


# `enqueue`/`enqueue-exec` печатают "задание N поставлено: ..."; дедуп в
# enqueue-exec (одна и та же команда уже стоит/выполнена) печатает другой
# текст — "...( задание N)" без слова "поставлено" — но это тоже валидный
# job_id, на который можно дождаться, поэтому вторая, более широкая заявка.
_JOB_ID_RE = re.compile(r"задание (\d+) поставлено")
_JOB_ID_FALLBACK_RE = re.compile(r"\(задание (\d+)\)")


def _parse_job_id(out: str) -> str | None:
    m = _JOB_ID_RE.search(out) or _JOB_ID_FALLBACK_RE.search(out)
    return m.group(1) if m else None


@contextlib.contextmanager
def _patch_global(name: str, value):
    """Только для demo(): подменяет модульный global (`_run_dispatcher`,
    `router_alive`, …) на время блока — офлайновая проверка wait_job/
    router_alive/run_claim без реального subprocess/сети."""
    orig = globals()[name]
    globals()[name] = value
    try:
        yield
    finally:
        globals()[name] = orig


# --- Ф5: дождаться СВОЕГО задания, а не «очередь что-то обработала» -----------

def wait_job(job_id: str | int, timeout_s: float = 120.0, poll_s: float = 3.0) -> dict:
    """Цикл `show --job job_id`, пока status не станет done|failed либо не
    выйдет таймаут; между проверками — `run --limit 1`, чтобы очередь вообще
    двигалась.

    ИЗВЕСТНЫЙ ГАП llm-queue (см. постановку): у `run` нет `--job` — он берёт
    ORDER BY priority ASC, id ASC, то есть каждый вызов `run --limit 1` может
    забрать ЧУЖОЕ задание, а не наше. Это не патчится здесь (llm-queue
    править запрещено, граница §6) — вместо этого мы не считаем «очередь
    что-то обработала» за «наше задание готово»: ждём именно наш `show`.
    """
    deadline = time.time() + timeout_s
    fields: dict[str, str] = {}
    while True:
        fields = _parse_show(_run_dispatcher("show", "--job", str(job_id)))
        status = fields.get("status")
        if status in ("done", "failed"):
            break
        if time.time() >= deadline:
            break
        _run_dispatcher("run", "--limit", "1")
        if poll_s:
            time.sleep(poll_s)
    status = fields.get("status")
    return {
        "job_id": str(job_id),
        "status": status,
        "timed_out": status not in ("done", "failed"),
        "result_path": fields.get("result_path"),
        "error": fields.get("error"),
        "fields": fields,
    }


# --- Ф2: живость роутера 2-й ступени — говорим, а не падаем -------------------

def router_alive(url: str = ROUTER_MODELS_URL, timeout: float = 2.0) -> bool:
    """GET .../v1/models — стандартной библиотекой (urllib), без requests:
    ключ OpenRouter этот модуль не хранит и не видит (граница §6), а живость
    роутера — не про ключ, просто «слушает ли порт»."""
    try:
        with urllib.request.urlopen(url, timeout=timeout) as resp:
            return 200 <= resp.status < 300
    except (urllib.error.URLError, OSError, ValueError):
        return False


# --- уровень 0: скрипт, $0, без очереди ----------------------------------------

def level0_script(claim: dict) -> dict:
    """Есть ли названный документ в 05_ORIGINALS; дальнейшая сверка
    номер/дата/сумма — на вызывающем коде (реальные реестры ЦКБА эта функция
    не читает, только путь, уже названный в `claim['originals_path']`)."""
    path = claim.get("originals_path")
    if not path or not os.path.isfile(path):
        return {"ok": False, "reason": f"первоисточник не найден: {path!r}"}
    return {"ok": True, "reason": "документ существует", "matched_path": path}


# --- уровень 1: локальная qwen через llm-queue (LLM-пул, `enqueue`) -----------

def level1_qwen_local(claim: dict) -> dict:
    """`enqueue` бьёт в локальный llama-server (llm_client.MODEL_NAME =
    qwen2.5-7b-instruct) — это и есть уровень 1 без параметров бэкенда.

    Ставим задание и дожидаемся ИМЕННО его через `wait_job()` (Ф5) — не
    «очередь что-то обработала» (см. известный гап в её докстроке)."""
    out = _run_dispatcher("enqueue", "--kind", "verify-claim",
                          "--prompt", claim.get("claim_to_verify", ""))
    job_id = _parse_job_id(out)
    if job_id is None:
        return {"ok": False, "reason": "не удалось разобрать id задания", "raw": out.strip()}
    wait = wait_job(job_id, timeout_s=claim.get("wait_timeout_s", 120),
                    poll_s=claim.get("wait_poll_s", 3))
    return {"ok": wait["status"] == "done", "reason": "qwen-local через llm-queue",
            "job_id": job_id, "status": wait["status"],
            "result_path": wait["result_path"], "timed_out": wait["timed_out"]}


# --- уровни 2-3: enqueue-exec (cpu-пул) -----------------------------------------
# `enqueue` (LLM-пул) зашит на локальную qwen (llm_client.MODEL_NAME) и не даёт
# выбрать бэкенд — уровни 2 и 3 поэтому идут через `enqueue-exec`, тот же путь,
# что и любой другой тяжёлый внешний процесс. Готовую команду (какая модель,
# какой промпт для ЭТОЙ заявки) строит вызывающий код — не ladder.py: здесь
# только маршрутизация в очередь и контракт обязательного поля.

def level2_openrouter_free(claim: dict) -> dict:
    """Второе мнение бесплатной моделью OpenRouter — только когда уровень 1 не
    уверен. `claim['openrouter_cmd']` обязан вызывать локальный роутер
    (air_llm_router.py) с его free-only guard; ключ OpenRouter этот модуль не
    видит и не трогает (граница §6).

    Ф2: роутер сейчас может быть не поднят — тогда СКИП с явной причиной, а
    не исключение и не пустая заявка в очереди."""
    if not router_alive():
        return {"ok": False, "skipped": True,
                "reason": "роутер второй ступени не поднят (127.0.0.1:8090) — ступень пропущена"}
    cmd = claim.get("openrouter_cmd")
    if not cmd:
        raise ValueError("level2: нужен claim['openrouter_cmd'] — команда вызова роутера")
    out = _run_dispatcher("enqueue-exec", "--kind", "verify-claim-openrouter", "--cmd", cmd)
    job_id = _parse_job_id(out)
    if job_id is None:
        return {"ok": False, "reason": "не удалось разобрать id задания", "raw": out.strip()}
    wait = wait_job(job_id, timeout_s=claim.get("wait_timeout_s", 180),
                    poll_s=claim.get("wait_poll_s", 3))
    return {"ok": wait["status"] == "done", "reason": "openrouter-free через llm-queue (cpu-пул)",
            "job_id": job_id, "status": wait["status"],
            "result_path": wait["result_path"], "timed_out": wait["timed_out"]}


def level3_codex(claim: dict) -> dict:
    """Codex, вне квоты: объёмные пакеты (сверка расчётов/таблиц/смет).
    `claim['codex_cmd']` строит вызывающий код (контракт вызова Codex CLI —
    `codex:codex-cli-runtime`, не этот модуль)."""
    cmd = claim.get("codex_cmd")
    if not cmd:
        raise ValueError("level3: нужен claim['codex_cmd'] — команда вызова Codex")
    out = _run_dispatcher("enqueue-exec", "--kind", "verify-claim-codex", "--cmd", cmd)
    job_id = _parse_job_id(out)
    if job_id is None:
        return {"ok": False, "reason": "не удалось разобрать id задания", "raw": out.strip()}
    wait = wait_job(job_id, timeout_s=claim.get("wait_timeout_s", 600),
                    poll_s=claim.get("wait_poll_s", 5))
    return {"ok": wait["status"] == "done", "reason": "codex через llm-queue (cpu-пул)",
            "job_id": job_id, "status": wait["status"],
            "result_path": wait["result_path"], "timed_out": wait["timed_out"]}


# --- уровни 4-5: claude.exe подпроцессом через llm-queue (Ф3) ------------------
# Конверт (`level_kind: "claude-judgement"`) остаётся ФОРМАТОМ задания — что
# спросить, какой моделью — но по умолчанию сам себя исполняет: enqueue-exec
# зовёт `claude_judge_run.py <model> <prompt_file>` (обёртка рядом, файл
# промпта — потому что `enqueue-exec` берёт argv-список без shell, stdin из
# файла ему передать нечем), результат ждём `wait_job()`. `claim["execute"] =
# False` — старый режим отладки: вернуть конверт без исполнения.

_PAID_CALLS = 0  # Ф1: модульный счётчик обращений к платным ступеням 4-5 за прогон


def reset_paid_counter() -> None:
    """Сбросить счётчик платных обращений — раз в прогон (`run_claim` считает
    по нему `paid_budget`)."""
    global _PAID_CALLS
    _PAID_CALLS = 0


def paid_calls_used() -> int:
    return _PAID_CALLS


def _claude_judgement_envelope(claim: dict, model_hint: str, instruction: str) -> dict:
    return {"ok": None, "pending_orchestrator": True, "level_kind": "claude-judgement",
            "model_hint": model_hint, "claim": claim, "instruction": instruction}


def _write_prompt_file(instruction: str) -> str:
    # ponytail: файлы промптов не удаляются и не ротируются — копятся в
    # CLAUDE_JUDGE_PROMPT_DIR. Апгрейд — os.unlink в конце _run_claude_judgement
    # либо периодическая чистка по возрасту, если счёт пойдёт на сотни заявок.
    os.makedirs(CLAUDE_JUDGE_PROMPT_DIR, exist_ok=True)
    fd, path = tempfile.mkstemp(prefix="ladder_prompt_", suffix=".txt",
                                dir=CLAUDE_JUDGE_PROMPT_DIR)
    with os.fdopen(fd, "w", encoding="utf-8") as fh:
        fh.write(instruction)
    return path


def _extract_result_payload(text: str) -> str:
    """`run_external` пишет результат как `"$ <команда>\\n\\n<stdout>"` —
    снимаем эту шапку, дальше идёт сырой stdout нашей обёртки (JSON claude.exe)."""
    _, sep, rest = (text or "").partition("\n\n")
    return rest if sep else (text or "")


def _parse_claude_json(raw: str) -> dict:
    """Разбор `claude.exe --output-format json`. Пустой ответ / не-JSON —
    `ok: False` с причиной, а не исключение (платная ступень не должна падать
    на плохом ответе)."""
    raw = (raw or "").strip()
    if not raw:
        return {"ok": False, "reason": "пустой ответ claude.exe"}
    try:
        data = json.loads(raw)
    except ValueError:
        return {"ok": False, "reason": "ответ claude.exe не JSON", "raw": raw[:500]}
    if not isinstance(data, dict) or data.get("is_error"):
        return {"ok": False, "reason": f"claude.exe вернул ошибку: {data!r}"[:500],
                "raw_json": data if isinstance(data, dict) else None}
    return {"ok": True, "reason": "суждение получено", "answer": data.get("result"),
            "raw_json": data}


def _run_claude_judgement(claim: dict, model: str, instruction: str) -> dict:
    global _PAID_CALLS
    prompt_path = _write_prompt_file(instruction)
    cmd = f'"{CLAUDE_JUDGE_PYTHON}" "{CLAUDE_JUDGE_WRAPPER}" "{model}" "{prompt_path}"'
    out = _run_dispatcher("enqueue-exec", "--kind", f"claude-judgement-{model}", "--cmd", cmd)
    job_id = _parse_job_id(out)
    if job_id is None:
        return {"ok": False, "reason": "не удалось разобрать id задания", "raw": out.strip()}
    _PAID_CALLS += 1
    wait = wait_job(job_id, timeout_s=claim.get("wait_timeout_s", 180),
                    poll_s=claim.get("wait_poll_s", 3))
    if wait["status"] != "done":
        return {"ok": False, "reason": f"claude-judgement не завершилась: status={wait['status']}",
                "job_id": job_id, "timed_out": wait["timed_out"], "error": wait.get("error")}
    try:
        with open(wait["result_path"], encoding="utf-8") as fh:
            raw_result = fh.read()
    except OSError as exc:
        return {"ok": False, "reason": f"результат не прочитан: {exc}", "job_id": job_id}
    parsed = _parse_claude_json(_extract_result_payload(raw_result))
    parsed.update({"job_id": job_id, "model": model, "instruction": instruction})
    return parsed


def level4_haiku_sonnet(claim: dict) -> dict:
    """Суждение: следует ли утверждение из источника. Модель — haiku по
    умолчанию, sonnet параметром (`claim['model'] = 'sonnet'`).
    `claim['execute'] = False` — вернуть конверт без исполнения (отладка)."""
    model = claim.get("model") or "haiku"
    instruction = f"следует ли утверждение {claim.get('claim_to_verify')!r} из источника — ответь коротко"
    if claim.get("execute", True) is False:
        return _claude_judgement_envelope(claim, model, instruction)
    return _run_claude_judgement(claim, model, instruction)


def level5_opus(claim: dict) -> dict:
    """Как level4, но opus — только там, где спорна сама правовая конструкция."""
    model = "opus"
    instruction = (f"спорна ли сама правовая конструкция утверждения "
                  f"{claim.get('claim_to_verify')!r} — ответь коротко")
    if claim.get("execute", True) is False:
        return _claude_judgement_envelope(claim, model, instruction)
    return _run_claude_judgement(claim, model, instruction)


# --- реестр ступеней -----------------------------------------------------------

LEVELS: dict[int, dict] = {
    0: {"name": "script", "cost_class": "$0", "executor": level0_script},
    1: {"name": "qwen-local", "cost_class": "$0", "executor": level1_qwen_local},
    2: {"name": "openrouter-free", "cost_class": "~0", "executor": level2_openrouter_free},
    3: {"name": "codex", "cost_class": "вне квоты", "executor": level3_codex},
    4: {"name": "haiku-sonnet", "cost_class": "×0,04/×0,2", "executor": level4_haiku_sonnet},
    5: {"name": "opus", "cost_class": "×1", "executor": level5_opus},
}


def escalate(claim: dict, level: int, reason: str) -> dict:
    """Поднять/зафиксировать заявку на ступени `level`. `reason` ОБЯЗАТЕЛЕН —
    без него функция падает: раздел 2а запрещает молчаливый подъём. Возврат —
    запись для журнала (Ф4/Ф5): уровень, причина, результат исполнителя,
    время."""
    if not reason or not str(reason).strip():
        raise ValueError("escalate() требует непустой reason — молчаливый подъём запрещён (2а)")
    if level not in LEVELS:
        raise ValueError(f"нет такой ступени: {level}")
    lv = LEVELS[level]
    started = time.time()
    result = lv["executor"](claim)
    return {
        "level": level,
        "level_name": lv["name"],
        "cost_class": lv["cost_class"],
        "reason": str(reason),
        "result": result,
        "wall_time_s": round(time.time() - started, 3),
        "at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
    }


# --- приоры: старт всегда снизу, статистика может СМЕСТИТЬ его вверх ----------
# (2г: приор обновляет ЗАПИСЬ ИСХОДА, а не запрос совета)

MIN_OUTCOMES_FOR_SHIFT = 5     # меньше — статистике не доверяем
LOW_SUCCESS_THRESHOLD = 0.15   # уровень 0 почти никогда не проходит проверку
MIN_VOLUME_FOR_SHIFT = 3       # разовую заявку не оптимизируем — не окупится


def load_priors(path: str = PRIORS_PATH) -> list[dict]:
    try:
        with open(path, encoding="utf-8") as fh:
            data = json.load(fh)
    except (OSError, ValueError):
        return []
    return data if isinstance(data, list) else []


def choose_start_level(card_type: str, volume: int = 1, *,
                       priors: list[dict] | None = None) -> int:
    """Стартовый уровень для новой заявки — по типу и по объёму партии.

    По умолчанию (пустой файл приоров, малая статистика, разовая заявка) —
    ВСЕГДА 0: раздел 2а требует старта снизу, и дефолтный `ladder_priors.json`
    для этого пуст. Приор может сместить старт ВВЕРХ, но только когда риск
    пропустить заведомо провальный уровень 0 уже статистически подтверждён
    (≥ MIN_OUTCOMES_FOR_SHIFT исходов, success_rate < LOW_SUCCESS_THRESHOLD) И
    объём текущей партии оправдывает точную настройку — иначе разовую заявку
    дешевле просто пропустить через 0, не считая.
    """
    if volume < MIN_VOLUME_FOR_SHIFT:
        return 0
    for row in (priors if priors is not None else load_priors()):
        if row.get("card_type") != card_type:
            continue
        if (row.get("n_outcomes", 0) >= MIN_OUTCOMES_FOR_SHIFT
                and row.get("success_rate", 1.0) < LOW_SUCCESS_THRESHOLD):
            return max(0, min(int(row.get("start_level", 0)), TOP_LEVEL))
        return 0
    return 0


def update_priors(card_type: str, level: int, passed: bool, path: str = PRIORS_PATH) -> None:
    """После заявки (2г): уровень, исход → в приоры. `success_rate` —доля
    заявок ЭТОГО типа, закрытых уже на уровне 0 (что и должно расти по Ф6).

    ponytail: без файловой блокировки — при параллельных заявках (2д, слоты
    очереди) возможна гонка read-modify-write и потеря одного обновления.
    Апгрейд — file lock или перенос приоров в SQLite, если параллелизм
    вырастет выше пилотных ~100 заявок за прогон.
    """
    priors = load_priors(path)
    row = next((r for r in priors if r.get("card_type") == card_type), None)
    if row is None:
        row = {"card_type": card_type, "start_level": 0, "n_outcomes": 0, "success_rate": 0.0}
        priors.append(row)
    n = int(row["n_outcomes"])
    success = 1.0 if (level == 0 and passed) else 0.0
    row["success_rate"] = round((row["success_rate"] * n + success) / (n + 1), 4)
    row["n_outcomes"] = n + 1
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(priors, fh, ensure_ascii=False, indent=1)


# --- Ф1: драйвер — эскалация по ИСЧЕРПАНИЮ, а не по неудаче поиска ------------

def _close_claim(log: list[dict], outcome: str, level: int, reason: str, card_type: str,
                 priors_path: str = PRIORS_PATH) -> dict:
    passed = outcome in ("подтверждено", "опровергнуто")
    update_priors(card_type, level, passed, path=priors_path)  # 2г: Ф6
    return {"log": log, "outcome": outcome, "max_level_reached": level,
            "reason": reason, "paid_calls_used": _PAID_CALLS}


def run_claim(claim: dict, *, start_level: int | None = None, top_level: int = TOP_LEVEL,
              paid_budget: int | None = None, level0_result: dict | None = None,
              level0_fn=None, priors_path: str = PRIORS_PATH) -> dict:
    """Провести заявку по лестнице снизу вверх. Подъём — ТОЛЬКО по исчерпанию
    текущей ступени (исход «не смог»), каждый подъём — с записанной причиной
    (её и так требует `escalate()`). Терминальный «не смог» наступает только
    после верхней ДОСТУПНОЙ ступени — не после первой неудачи.

    Ступень 0 — чужая предметная проверка (level0_verify.py), этот модуль её
    не импортирует: исход приходит параметром `level0_result` (уже готовый
    словарь по контракту `verify_claim_level0`) либо коллбэком `level0_fn`
    (claim -> тот же контракт). Без обоих — дефолт: свой урезанный
    `level0_script` (ok/reason), нормализованный в тот же словарь исходов.

    ОГРАНИЧИТЕЛЬ (2б, обязателен): ступени ВЫШЕ ПЕРВОЙ получают заявку только
    если `claim['candidates']` непуст либо `claim['sources_named_in_words']`
    — иначе подъём выше 1 не делается (причина — в журнале), и это САМЫЙ
    частый терминальный исход (без него 83 заявки уедут на платные модели).

    `paid_budget` — потолок обращений к ступеням 4-5 ЗА ПРОГОН (модульный
    счётчик `_PAID_CALLS`, сбрасывается `reset_paid_counter()`); исчерпан —
    ступень пропускается с причиной, а не молча.

    Возврат — журнал прохода: `log` (по шагу на уровень: уровень, причина
    подъёма, исход исполнителя, время), итоговый `outcome`,
    `max_level_reached`, `paid_calls_used`.
    """
    card_type = claim.get("card_type", "")
    log: list[dict] = []
    gate_open = bool(claim.get("candidates")) or bool(claim.get("sources_named_in_words"))
    level = start_level if start_level is not None else choose_start_level(
        card_type, claim.get("volume", 1))
    max_level = -1  # -1: ни одна ступень ещё не исполнена

    if level == 0:
        if level0_result is not None:
            zero = dict(level0_result)
        elif level0_fn is not None:
            zero = level0_fn(claim)
        else:
            raw = level0_script(claim)
            zero = {"outcome": "подтверждено" if raw.get("ok") else "не смог",
                    "reason": raw.get("reason", "")}
        gate_open = gate_open or bool(zero.get("candidates"))
        log.append({"level": 0, "level_name": LEVELS[0]["name"], "reason": "старт снизу (2а)",
                   "result": zero, "at": _now()})
        max_level = 0
        outcome0 = zero.get("outcome", "не смог")
        if outcome0 in ("подтверждено", "опровергнуто"):
            return _close_claim(log, outcome0, 0, zero.get("reason", ""), card_type, priors_path)
        reason = f"ступень 0 не смогла определить: {zero.get('reason', '')}"
        level = 1
    else:
        reason = f"старт сразу с уровня {level} — приор по типу {card_type!r}"

    while level <= top_level:
        if level > 1 and not gate_open:
            log.append({"level": level, "level_name": LEVELS[level]["name"], "reason": reason,
                       "result": {"skipped": True}, "at": _now()})
            return _close_claim(
                log, "не смог", max_level,
                "подъём выше 1 не сделан (2б-ограничитель): нет claim['candidates'] и "
                "claim['sources_named_in_words'] — что именно не сошлось на "
                f"ступени {max_level}: {reason}", card_type, priors_path)
        if level in (4, 5) and paid_budget is not None and _PAID_CALLS >= paid_budget:
            log.append({"level": level, "level_name": LEVELS[level]["name"], "reason": reason,
                       "result": {"skipped": True}, "at": _now()})
            return _close_claim(log, "не смог", max_level,
                                "потолок платных ступеней исчерпан (paid_budget) — "
                                f"что не сошлось до этого: {reason}", card_type, priors_path)
        entry = escalate(claim, level, reason)
        log.append(entry)
        max_level = level
        if entry["result"].get("ok") is True:
            return _close_claim(log, "подтверждено", level,
                                entry["result"].get("reason", ""), card_type, priors_path)
        reason = f"ступень {level} не смогла: {entry['result'].get('reason', '')}"
        level += 1

    return _close_claim(log, "не смог", max_level,
                        f"верхняя доступная ступень ({top_level}) исчерпана: {reason}",
                        card_type, priors_path)


# --- проверяльщики: 2б, по одному на 4 типа заявок -----------------------------
# Проверка не прошла — заявка поднимается на ступень, а не закрывается. Вопрос
# «хватило ли нижнего уровня» решает ЭТА функция, не исполнитель — исполнитель
# всегда скажет «да».

def verify_chronology(card_type: str, claim: dict, evidence: dict) -> dict:
    """chronology (40): дата, номер, предмет, стороны — сверка строк с документом."""
    fields = ("date", "number", "subject", "parties")
    missing = [f for f in fields if not evidence.get(f)]
    if missing:
        return {"passed": False, "reason": f"в источнике не найдены поля: {', '.join(missing)}"}
    mismatched = [f for f in fields
                 if str(claim.get(f, "")).strip().lower() != str(evidence.get(f, "")).strip().lower()]
    if mismatched:
        return {"passed": False, "reason": f"не сходится с документом: {', '.join(mismatched)}"}
    return {"passed": True, "reason": "дата/номер/предмет/стороны совпали с документом"}


def verify_decision_task(card_type: str, claim: dict, evidence: dict) -> dict:
    """decision/task (27): назван ли документ, существует ли он, есть ли в нём предмет решения."""
    if not evidence.get("document_named"):
        return {"passed": False, "reason": "документ не назван в заявке"}
    if not evidence.get("document_exists"):
        return {"passed": False, "reason": "названный документ не найден на диске"}
    if not evidence.get("decision_subject_present"):
        return {"passed": False, "reason": "в документе не найден предмет решения"}
    return {"passed": True, "reason": "документ назван, существует, предмет решения в нём есть"}


def verify_principle_pattern(card_type: str, claim: dict, evidence: dict) -> dict:
    """principle/pattern (22): цитируемая норма/пункт договора найдены в источнике дословно."""
    quote = str(claim.get("claim_to_verify", "")).strip()
    source = str(evidence.get("source_excerpt", ""))
    if not quote:
        return {"passed": False, "reason": "нет цитируемой нормы для сверки"}
    norm = lambda s: " ".join(s.split())  # схлопнуть пробелы/переносы, не смысл
    if norm(quote) not in norm(source):
        return {"passed": False, "reason": "цитата не найдена дословно в первоисточнике"}
    return {"passed": True, "reason": "цитата найдена дословно в первоисточнике"}


def verify_risk_fork_other(card_type: str, claim: dict, evidence: dict) -> dict:
    """risk/fork/прочее (19): сумма и её состав сходятся с расчётом."""
    claimed, computed = evidence.get("claimed_amount"), evidence.get("computed_amount")
    if claimed is None or computed is None:
        return {"passed": False, "reason": "сумма или расчёт не переданы проверке"}
    try:
        close_enough = abs(float(claimed) - float(computed)) <= max(1.0, float(claimed) * 0.005)
    except (TypeError, ValueError):
        return {"passed": False, "reason": "сумма/расчёт не числовые значения"}
    if not close_enough:
        return {"passed": False, "reason": f"сумма не сходится: заявлено {claimed}, расчёт {computed}"}
    if not evidence.get("components_match", False):
        return {"passed": False, "reason": "состав суммы не сходится с расчётом либо не проверен"}
    return {"passed": True, "reason": "сумма и состав сходятся с расчётом"}


VERIFIERS = {
    "chronology": verify_chronology,
    "decision": verify_decision_task,
    "task": verify_decision_task,
    "principle": verify_principle_pattern,
    "pattern": verify_principle_pattern,
    "risk": verify_risk_fork_other,
    "fork": verify_risk_fork_other,
}


def verify(card_type: str, claim: dict, evidence: dict) -> dict:
    """Контракт проверяльщика (2б): `{"passed": bool, "reason": str}`.
    Неизвестный `card_type` идёт в тот же проверяльщик, что и `risk/fork` —
    2б называет эту группу «и прочее»."""
    fn = VERIFIERS.get(card_type, verify_risk_fork_other)
    result = fn(card_type, claim, evidence)
    return {"passed": bool(result["passed"]), "reason": str(result["reason"])}


# --- самопроверка --------------------------------------------------------------
# Все проверки офлайновые: `_run_dispatcher`/`router_alive` подменяются
# фейками через `_patch_global` — ни llm-queue, ни claude здесь не зовутся.

def _fake_queue(show_sequence: dict[str, list[str]], enqueue_reply: str = "verify-claim"):
    """Мини-фейк dispatcher.py для wait_job/run_claim: `show_sequence[job_id]`
    — очередь статусов, отдаваемых по очереди при каждом `show --job job_id`
    (последний повторяется). `enqueue`/`enqueue-exec` всегда отвечают job_id=42."""
    calls = {"n": 0}

    def fake(*args: str, timeout: int = 600) -> str:
        if args[0] in ("enqueue", "enqueue-exec"):
            return f"задание 42 поставлено: {enqueue_reply} (приоритет 5)\n"
        if args[0] == "show":
            job_id = args[2]
            seq = show_sequence.get(job_id, ["done"])
            idx = min(calls.setdefault(job_id, 0), len(seq) - 1)
            calls[job_id] = idx + 1
            status = seq[idx]
            return (f"  id             {job_id}\n  status         {status}\n"
                   f"  result_path    fake_result.txt\n  error          None\n")
        if args[0] == "run":
            return "обработано: 1 успешно, 0 с ошибкой\n"
        return ""
    return fake


def demo() -> None:
    demo_priors = os.path.join(tempfile.gettempdir(), "ladder_demo_priors.json")

    # 1. эскалация без reason — падает
    try:
        escalate({"card_type": "risk"}, 1, "")
        raise AssertionError("escalate() без reason должна была упасть")
    except ValueError:
        pass

    # 2. старт всегда 0 без приоров, независимо от типа/объёма
    assert choose_start_level("chronology", volume=50) == 0
    assert choose_start_level("decision", volume=1) == 0

    # 3. приор может поднять старт — но только при статистике И объёме сразу
    hot = [{"card_type": "chronology", "start_level": 2, "n_outcomes": 10, "success_rate": 0.05}]
    assert choose_start_level("chronology", volume=10, priors=hot) == 2
    assert choose_start_level("chronology", volume=1, priors=hot) == 0  # объём мал — не окупится

    # 4. реестр ступеней полон, по порядку 0..5, у каждой есть исполнитель
    assert list(LEVELS) == [0, 1, 2, 3, 4, 5]
    for spec in LEVELS.values():
        assert "name" in spec and "cost_class" in spec and callable(spec["executor"])

    # 5. все 4 проверяльщика существуют и отвечают по контракту {"passed","reason"}
    for card_type in ("chronology", "decision", "task", "principle", "pattern",
                      "risk", "fork", "неизвестный-тип"):
        out = verify(card_type, {"claim_to_verify": "x"}, {})
        assert set(out) == {"passed", "reason"}
        assert isinstance(out["passed"], bool) and isinstance(out["reason"], str)

    # 6. execute=False на уровнях 4-5 — старый режим отладки: конверт без исполнения
    for lvl in (4, 5):
        out = escalate({"card_type": "risk", "claim_to_verify": "x", "execute": False}, lvl,
                       "нужно суждение модели")
        assert out["result"]["pending_orchestrator"] is True

    # 7. уровень 3 проверяет обязательное поле команды ДО обращения к очереди
    try:
        LEVELS[3]["executor"]({})
        raise AssertionError("level 3 должен был потребовать codex_cmd")
    except ValueError:
        pass

    # 8. уровень 0 — реальный файловый чек без сети и без очереди
    out = escalate({"originals_path": __file__}, 0, "старт снизу — заявка новая")
    assert out["result"]["ok"] is True

    # 9. Ф2: router_alive — мёртвый порт даёт False быстро, без исключения
    assert router_alive("http://127.0.0.1:1/v1/models", timeout=0.5) is False

    # 10. Ф2: level2 при мёртвом роутере — skipped, а не исключение (даже без
    # openrouter_cmd — роутер проверяется РАНЬШЕ обязательного поля)
    with _patch_global("router_alive", lambda *a, **k: False):
        out = level2_openrouter_free({})
        assert out == {"ok": False, "skipped": True,
                       "reason": "роутер второй ступени не поднят (127.0.0.1:8090) — ступень пропущена"}
    # ...а живой роутер по-прежнему требует openrouter_cmd
    with _patch_global("router_alive", lambda *a, **k: True):
        try:
            level2_openrouter_free({})
            raise AssertionError("level2 должен был потребовать openrouter_cmd при живом роутере")
        except ValueError:
            pass

    # 11. Ф5: wait_job дожидается ИМЕННО своего done, не первого "run" подряд
    with _patch_global("_run_dispatcher", _fake_queue({"42": ["queued", "queued", "done"]})):
        out = wait_job("42", timeout_s=30, poll_s=0)
        assert out == {"job_id": "42", "status": "done", "timed_out": False,
                       "result_path": "fake_result.txt", "error": "None",
                       "fields": out["fields"]}

    # 12. Ф5: wait_job — таймаут, если задание не закрывается
    with _patch_global("_run_dispatcher", _fake_queue({"42": ["queued"]})):
        out = wait_job("42", timeout_s=0.05, poll_s=0)
        assert out["timed_out"] is True and out["status"] == "queued"

    # 13. Ф5: level1/2/3 дожидаются СВОЕГО задания через wait_job, не разово run
    with _patch_global("_run_dispatcher", _fake_queue({"42": ["queued", "done"]})):
        out = level1_qwen_local({"claim_to_verify": "x", "wait_poll_s": 0})
        assert out["ok"] is True and out["job_id"] == "42"
    with _patch_global("router_alive", lambda *a, **k: True), \
        _patch_global("_run_dispatcher", _fake_queue({"42": ["done"]})):
        out = level3_codex({"codex_cmd": "echo x"})
        assert out["ok"] is True

    # 14. Ф3: level4/5 исполняются САМИ (execute по умолчанию True) — мокаем
    # очередь и результат: заголовок "$ cmd\n\n" + JSON claude.exe
    fake_result = tempfile.NamedTemporaryFile(
        mode="w", suffix=".txt", delete=False, encoding="utf-8")
    fake_result.write('$ claude_judge_run.py haiku prompt.txt\n\n'
                      '{"is_error": false, "result": "да, следует"}\n')
    fake_result.close()

    def fake_claude_queue(*args: str, timeout: int = 600) -> str:
        if args[0] == "enqueue-exec":
            return "задание 99 поставлено: claude-judgement-haiku (приоритет 5)\n"
        if args[0] == "show":
            return (f"  status         done\n  result_path    {fake_result.name}\n"
                   f"  error          None\n")
        return ""

    reset_paid_counter()
    with _patch_global("_run_dispatcher", fake_claude_queue):
        out = level4_haiku_sonnet({"claim_to_verify": "x"})
        assert out["ok"] is True and out["answer"] == "да, следует"
    assert paid_calls_used() == 1
    os.unlink(fake_result.name)

    # 15. Ф3: пустой/не-JSON ответ — ok:False с причиной, не исключение
    assert _parse_claude_json("")["ok"] is False
    assert _parse_claude_json("не json совсем")["ok"] is False
    assert _extract_result_payload("$ cmd\n\n{\"a\": 1}\n") == '{"a": 1}\n'

    # 16. Ф1: run_claim — журнал с подъёмом 0→1, закрыто на уровне 1
    reset_paid_counter()
    with _patch_global("_run_dispatcher", _fake_queue({"42": ["done"]})):
        result = run_claim({"card_type": "chronology", "claim_to_verify": "x"},
                           priors_path=demo_priors)
    assert [step["level"] for step in result["log"]] == [0, 1]
    assert result["outcome"] == "подтверждено" and result["max_level_reached"] == 1

    # 17. Ф1-ограничитель: без candidates/sources_named_in_words подъём выше 1
    # не делается — терминальный "не смог" на уровне 1, а не на 5
    reset_paid_counter()
    with _patch_global("_run_dispatcher", _fake_queue({"42": ["failed"]})):
        result = run_claim({"card_type": "risk", "claim_to_verify": "x"},
                           level0_result={"outcome": "не смог", "reason": "нет кандидатов"},
                           priors_path=demo_priors)
    assert result["outcome"] == "не смог" and result["max_level_reached"] == 1
    assert "2б-ограничитель" in result["reason"]
    assert result["log"][-1]["level"] == 2 and result["log"][-1]["result"] == {"skipped": True}

    # 18. Ф1-бюджет: потолок платных ступеней исчерпан — 4/5 пропускаются
    # с явной причиной, executor вообще не зовётся (без мока сети)
    reset_paid_counter()
    result = run_claim({"card_type": "risk", "candidates": ["x"]},
                       start_level=4, paid_budget=0, priors_path=demo_priors)
    assert result["outcome"] == "не смог"
    assert "потолок платных ступеней исчерпан" in result["reason"]
    assert paid_calls_used() == 0

    os.remove(demo_priors) if os.path.exists(demo_priors) else None

    print("ladder.py demo: все проверки прошли")


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    demo()
