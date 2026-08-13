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
`dispatcher.py` (subprocess). Уровни 4-5 — суждение Claude: не локальный
процесс, а отдельный Task/Agent-вызов, который умеет поднять только
ОРКЕСТРАТОР (Claude Code сессия), не python-модуль. Поэтому level4/5-функции
здесь не исполняют ничего — они отдают оркestratору готовый конверт
(`pending_orchestrator: True`) с тем, что подать в Task.

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

import json
import os
import subprocess
import sys
import time
from datetime import datetime, timezone

# --- пути и настройки ---------------------------------------------------------

_HERE = os.path.dirname(os.path.abspath(__file__))
DISPATCHER = os.environ.get("LLM_QUEUE_DISPATCHER",
                            r"E:\-8-\llm-queue\llm-queue\dispatcher.py")
DISPATCHER_PYTHON = os.environ.get("LLM_QUEUE_PYTHON", sys.executable)
ROUTER_URL = os.environ.get("AIR_WORKER_ROUTER_URL",
                            "http://127.0.0.1:8090/v1/chat/completions")
PRIORS_PATH = os.environ.get("LADDER_PRIORS_PATH",
                             os.path.join(_HERE, "ladder_priors.json"))

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

    Синхронно: ставим задание и просим очередь обработать один шаг. ИЗВЕСТНЫЙ
    ГАП llm-queue (см. отчёт): `run --limit 1` берёт САМОЕ старое/приоритетное
    задание в очереди, а не обязательно только что поставленное — при
    параллельных заявках (2д) чужой job может обработаться раньше нашего.
    """
    out = _run_dispatcher("enqueue", "--kind", "verify-claim",
                          "--prompt", claim.get("claim_to_verify", ""))
    tokens = out.split()
    job_id = tokens[1] if len(tokens) > 1 else None
    if job_id is None:
        return {"ok": False, "reason": "не удалось разобрать id задания", "raw": out}
    _run_dispatcher("run", "--limit", "1")
    fields = _parse_show(_run_dispatcher("show", "--job", job_id))
    return {"ok": fields.get("status") == "done", "reason": "qwen-local через llm-queue",
            "job_id": job_id, "status": fields.get("status"),
            "result_path": fields.get("result_path")}


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
    видит и не трогает (граница §6)."""
    cmd = claim.get("openrouter_cmd")
    if not cmd:
        raise ValueError("level2: нужен claim['openrouter_cmd'] — команда вызова роутера")
    out = _run_dispatcher("enqueue-exec", "--kind", "verify-claim-openrouter", "--cmd", cmd)
    _run_dispatcher("run", "--limit", "1")
    return {"ok": None, "reason": "поставлено в llm-queue (cpu-пул) — см. dispatcher status/show",
            "enqueue_out": out.strip()}


def level3_codex(claim: dict) -> dict:
    """Codex, вне квоты: объёмные пакеты (сверка расчётов/таблиц/смет).
    `claim['codex_cmd']` строит вызывающий код (контракт вызова Codex CLI —
    `codex:codex-cli-runtime`, не этот модуль)."""
    cmd = claim.get("codex_cmd")
    if not cmd:
        raise ValueError("level3: нужен claim['codex_cmd'] — команда вызова Codex")
    out = _run_dispatcher("enqueue-exec", "--kind", "verify-claim-codex", "--cmd", cmd)
    _run_dispatcher("run", "--limit", "1")
    return {"ok": None, "reason": "поставлено в llm-queue (cpu-пул) — см. dispatcher status/show",
            "enqueue_out": out.strip()}


# --- уровни 4-5: прямой вызов = Claude Task/Agent, не эта функция --------------

def _claude_judgement_envelope(claim: dict, model_hint: str, instruction: str) -> dict:
    return {"ok": None, "pending_orchestrator": True, "level_kind": "claude-judgement",
            "model_hint": model_hint, "claim": claim, "instruction": instruction}


def level4_haiku_sonnet(claim: dict) -> dict:
    """Суждение: следует ли утверждение из источника. Не CPU-процесс и не
    llm-queue — ladder.py не умеет породить Claude-сессию сам (это
    python-модуль); отдаёт готовый конверт, оркестратор вызывает Task/Agent
    сам с effort haiku или sonnet."""
    return _claude_judgement_envelope(
        claim, "haiku|sonnet",
        f"следует ли утверждение {claim.get('claim_to_verify')!r} из источника")


def level5_opus(claim: dict) -> dict:
    """Как level4, но opus — только там, где спорна сама правовая конструкция."""
    return _claude_judgement_envelope(
        claim, "opus",
        f"спорна ли сама правовая конструкция утверждения {claim.get('claim_to_verify')!r}")


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

def demo() -> None:
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

    # 6. уровни 4-5 не исполняют суждение сами — отдают конверт оркестратору
    for lvl in (4, 5):
        out = escalate({"card_type": "risk", "claim_to_verify": "x"}, lvl, "нужно суждение модели")
        assert out["result"]["pending_orchestrator"] is True

    # 7. уровни 2-3 проверяют обязательное поле команды ДО обращения к очереди
    for lvl in (2, 3):
        try:
            LEVELS[lvl]["executor"]({})
            raise AssertionError(f"level {lvl} должен был потребовать *_cmd")
        except ValueError:
            pass

    # 8. уровень 0 — реальный файловый чек без сети и без очереди
    out = escalate({"originals_path": __file__}, 0, "старт снизу — заявка новая")
    assert out["result"]["ok"] is True

    print("ladder.py demo: все проверки прошли")


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    demo()
