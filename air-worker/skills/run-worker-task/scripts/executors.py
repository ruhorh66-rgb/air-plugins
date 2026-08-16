r"""Типы задач air-worker — реестр исполнителей. Ядро о них ничего не знает.

ЗАЧЕМ РЕЕСТР, А НЕ IF В СЛУШАТЕЛЕ. Слушатель обязан запускать ОДИН шаблон,
зафиксированный в момент СОЗДАНИЯ заявки, и не собирать команду из чего-либо
пришедшего позже. Поэтому тип задачи (`task_type`) — подписанное поле заявки, а
код, который по нему запускается, лежит здесь и выбирается словарём. Добавить
второй тип = добавить строку в `REGISTRY`; ядро, подпись, лок и слушатель при
этом не трогаются вовсе. Это и есть разъём (Ф7 задания TASK-OBS-0046).

ГРАНИЦА. Исполнитель получает ПУТЬ к материалу и подписанные параметры. Он не
получает ничего из Telegram: канал умеет ровно два действия — «запустить заявку
с этим id» и «отклонить». Расширение канала до «выполнить присланное» ломает
условие, при котором он вообще допустим (см. `approve_via_telegram.py` в
air-ruflo-bridge, раздел «ГРАНИЦЫ БЕЗОПАСНОСТИ»).

ЧТО ВОЗВРАЩАЕТ ИСПОЛНИТЕЛЬ. Только скаляры: пути, хэши, счётчики, коды. Текст
ответа модели идёт в файл рядом с рантаймом, а не в возвращаемый словарь — иначе
он попадёт в протокол и в сообщение Telegram.

ИСПОЛНЕНИЕ — ЧЕРЕЗ `llm-queue` (TASK-OBS-0054, раздел 2в). Ни один исполнитель
здесь не бьёт по сети/CPU сам: air-worker остаётся гейтом и диспетчером, а
фактический запуск (слот, ретрай, переживание перезапуска) — у `dispatcher.py`
(`E:\-8-\llm-queue\llm-queue\`). Оба типа задачи ставят задание через его CLI
(`enqueue`/`enqueue-exec` + `run` + `show`) — subprocess-хелпер для этого не
пишется заново, переиспользуется `ladder._run_dispatcher`/`ladder._parse_show`
(тот же модуль, что и у ступеней 1–3 лестницы). Два независимых запускателя на
одном llama-server уже конфликтовали — ради этого очередь и написана.
"""
from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import ladder  # noqa: E402 — только dispatcher-хелпер, свой subprocess не заводим

# Локальный роутер: один OpenAI-совместимый адрес перед llama/Codex/Anthropic/
# OpenRouter (`E:\-4-\codex-shim\air_llm_router.py`). Своего ключа air-worker не
# держит и наружу сам не ходит — весь платный трафик остаётся за роутером с его
# free-only guard. Вызывает этот URL не сам air-worker, а подпроцесс, который
# поднимает llm-queue (см. `_openrouter_job_main` ниже).
ROUTER_URL = os.environ.get("AIR_WORKER_ROUTER_URL",
                            "http://127.0.0.1:8090/v1/chat/completions")
MAX_INPUT_CHARS = 40_000
MAX_TIMEOUT_SECONDS = 900


def free_only() -> bool:
    """Return whether unattended OpenRouter work is restricted to :free models."""
    return os.environ.get("AIR_WORKER_FREE_ONLY", "1").strip().lower() not in (
        "0", "false", "no", "off")


def _await_job(job_out: str, timeout: int) -> dict:
    """Run, wait for, and read exactly the enqueue result's own queue job.

    The legacy dispatcher has only global ``run --limit``; using it would
    process somebody else's job under concurrency.  Direct execution therefore
    fails closed until the queue advertises the targeted capability interface.
    """
    _require_targeted_queue()
    job_id = ladder._parse_job_id(job_out)
    if job_id is None:
        raise SystemExit(f"не удалось разобрать id задания llm-queue: {job_out!r}")
    try:
        ladder._run_dispatcher("run", "--job", job_id, timeout=timeout)
    except SystemExit:
        # A targeted job can fail/requeue and therefore return nonzero.  Its
        # JSON receipt, not CLI prose, remains the source of the final state.
        pass
    except subprocess.TimeoutExpired as exc:
        # The approved minimal queue API has no cancellation claim.  Do not
        # pretend this subprocess timeout killed an external job.
        raise SystemExit("targeted queue run timed out; reconciliation required") from exc

    deadline = time.monotonic() + timeout
    while True:
        receipt = _show_target_job(job_id)
        status = receipt.get("status")
        if status in ("done", "failed"):
            return receipt
        if status == "queued":
            raise SystemExit("targeted queue job requeued; retry policy belongs to llm-queue")
        if status == "unknown":
            raise SystemExit("targeted queue job disappeared")
        if time.monotonic() >= deadline:
            raise SystemExit("targeted queue wait timed out; reconciliation required")
        time.sleep(min(1.0, max(0.05, deadline - time.monotonic())))


_REQUIRED_QUEUE_CAPABILITIES = {"run-job", "show-job-json"}


def _require_targeted_queue() -> None:
    """Require the documented queue capability contract before direct dispatch."""
    try:
        raw = ladder._run_dispatcher("capabilities", "--format", "json", timeout=15)
        payload = json.loads(raw)
        capabilities = set(payload.get("capabilities", [])) if isinstance(payload, dict) else set()
    except Exception as exc:  # current dispatcher deliberately lands here
        raise SystemExit("llm-queue lacks targeted run/show capability; "
                         "direct execution is blocked to protect unrelated jobs") from exc
    missing = _REQUIRED_QUEUE_CAPABILITIES - capabilities
    if missing:
        raise SystemExit("llm-queue missing required targeted capability: " + ", ".join(sorted(missing)))


def _show_target_job(job_id: str) -> dict:
    try:
        raw = ladder._run_dispatcher("show", "--job", job_id, "--json", timeout=15)
    except SystemExit as exc:
        raise SystemExit("targeted queue job disappeared") from exc
    try:
        receipt = json.loads(raw)
    except ValueError as exc:
        raise SystemExit("llm-queue returned malformed targeted JSON receipt") from exc
    if not isinstance(receipt, dict) or str(receipt.get("job_id")) != str(job_id):
        raise SystemExit("llm-queue returned mismatched targeted JSON receipt")
    return receipt


def _read_input(path: str, limit: int) -> str:
    with open(path, encoding="utf-8", errors="replace") as fh:
        return fh.read(limit)


def _post(url: str, body: dict, timeout: int) -> dict:
    req = urllib.request.Request(
        url, data=json.dumps(body).encode("utf-8"),
        headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=timeout) as resp:
        return json.loads(resp.read().decode("utf-8"))


# --- Тип 1: LLM-обработка файла через OpenRouter поверх локального роутера ----

def validate_openrouter(params: dict) -> None:
    model = str(params.get("model", ""))
    if "/" not in model:
        # Роутер маршрутизирует по имени: имя БЕЗ слэша — не OpenRouter, и
        # уходит в llama молча. Молчаливая подмена бэкенда хуже отказа.
        raise ValueError("model: нужен id вида vendor/model[:free] — иначе роутер "
                         "отправит запрос не туда")
    if free_only() and not model.endswith(":free"):
        raise ValueError("model: в free-only режиме разрешены только модели с суффиксом :free")
    if not str(params.get("instruction", "")).strip():
        raise ValueError("instruction: пустая инструкция — задача не определена")
    limit = int(params.get("max_input_chars", MAX_INPUT_CHARS))
    if not (1 <= limit <= MAX_INPUT_CHARS):
        raise ValueError(f"max_input_chars: 1..{MAX_INPUT_CHARS}")
    timeout = int(params.get("timeout", 600))
    if not (1 <= timeout <= MAX_TIMEOUT_SECONDS):
        raise ValueError(f"timeout: 1..{MAX_TIMEOUT_SECONDS}")


def run_openrouter(request: dict, params: dict, out_path: str) -> dict:
    """Инструкция и модель — из ПОДПИСАННЫХ параметров заявки, материал — из файла.

    Сеть здесь НЕ трогается (2в). Задание уходит в llm-queue через
    `enqueue-exec` (cpu-пул) — тем же путём, каким level2/level3 лестницы
    зовут OpenRouter/Codex: `enqueue` (LLM-пул) зашит на локальную qwen
    (`llm_client.MODEL_NAME`) и не даёт выбрать бэкенд, поэтому произвольный
    HTTP-вызов идёт как внешний процесс. Сам вызов роутера делает подпроцесс
    (`_openrouter_job_main` ниже, тот же `executors.py`, но другой процесс) —
    материал и параметры передаются ему временным JSON-файлом, а не аргументами
    командной строки, чтобы длинная инструкция/кириллица не ломала кавычки."""
    job = {
        "input_path": request["input_path"],
        "max_input_chars": int(params.get("max_input_chars", MAX_INPUT_CHARS)),
        "model": params["model"], "instruction": params["instruction"],
        "temperature": float(params.get("temperature", 0.2)),
        "timeout": int(params.get("timeout", 600)),
        "out_path": out_path,
    }
    fd, job_path = tempfile.mkstemp(prefix="aw-openrouter-", suffix=".json")
    with os.fdopen(fd, "w", encoding="utf-8") as fh:
        json.dump(job, fh, ensure_ascii=False)
    result_path = job_path + ".result.json"
    try:
        cmd = (f'"{sys.executable}" "{os.path.abspath(__file__)}" '
              f'--openrouter-job "{job_path}"')
        out = ladder._run_dispatcher("enqueue-exec", "--kind", "openrouter-llm", "--cmd", cmd)
        fields = _await_job(out, job["timeout"] + 60)
        if fields.get("status") != "done":
            raise SystemExit(f"llm-queue задание не завершилось: "
                             f"{fields.get('status')} {fields.get('error_class') or ''}")
        if not os.path.isfile(result_path):
            raise SystemExit("llm-queue отчиталось done, но результата нет "
                             f"({result_path})")
        with open(result_path, encoding="utf-8") as fh:
            result = json.load(fh)
        if not isinstance(result, dict) or result.get("ok") is not True:
            raise SystemExit("llm-queue вернула результат без подтверждённого успеха")
        if not os.path.isfile(str(result.get("output_path") or "")):
            raise SystemExit("llm-queue вернула успех без выходного файла")
        return result
    finally:
        for p in (job_path, result_path):
            try:
                os.remove(p)
            except OSError:
                pass


def _openrouter_job_main(job_path: str) -> int:
    """Тело подпроцесса, который llm-queue поднимает по `--cmd` выше. Здесь и
    только здесь материал уходит по сети — процесс запущен и ограничен
    dispatcher.py (cpu-слот, ниже приоритет), не самим air-worker."""
    with open(job_path, encoding="utf-8") as fh:
        job = json.load(fh)
    material = _read_input(job["input_path"], job["max_input_chars"])
    body = {
        "model": job["model"],
        "messages": [
            {"role": "system", "content": job["instruction"]},
            {"role": "user", "content": material},
        ],
        "temperature": job["temperature"],
    }
    try:
        payload = _post(ROUTER_URL, body, job["timeout"])
    except urllib.error.HTTPError as exc:
        print(f"роутер отказал ({exc.code}): "
              f"{exc.read().decode('utf-8', 'replace')[:200]}", file=sys.stderr)
        return 1
    except OSError as exc:
        print(f"роутер недоступен на {ROUTER_URL}: {exc}", file=sys.stderr)
        return 1
    if "error" in payload:
        print(f"роутер вернул ошибку: {str(payload['error'])[:200]}", file=sys.stderr)
        return 1
    try:
        text = payload["choices"][0]["message"]["content"]
    except (KeyError, IndexError, TypeError) as exc:
        print(f"роутер вернул некорректный ответ ({type(exc).__name__})", file=sys.stderr)
        return 1
    if not isinstance(text, str):
        print("роутер вернул некорректный текст ответа", file=sys.stderr)
        return 1
    with open(job["out_path"], "w", encoding="utf-8") as fh:
        fh.write(text)
    usage = payload.get("usage") or {}
    result = {
        "ok": True, "model": job["model"], "chars_in": len(material),
        "chars_out": len(text),
        "tokens_in": int(usage.get("prompt_tokens", 0) or 0),
        "tokens_out": int(usage.get("completion_tokens", 0) or 0),
        "output_path": job["out_path"],
    }
    with open(job_path + ".result.json", "w", encoding="utf-8") as fh:
        json.dump(result, fh, ensure_ascii=False)
    return 0


# --- Тип 2: локальная Qwen (llama-server), уровень 1 — материал не покидает машину

def validate_qwen_local(params: dict) -> None:
    if not str(params.get("instruction", "")).strip():
        raise ValueError("instruction: пустая инструкция — задача не определена")
    limit = int(params.get("max_input_chars", MAX_INPUT_CHARS))
    if not (1 <= limit <= MAX_INPUT_CHARS):
        raise ValueError(f"max_input_chars: 1..{MAX_INPUT_CHARS}")
    timeout = int(params.get("timeout", 300))
    if not (1 <= timeout <= MAX_TIMEOUT_SECONDS):
        raise ValueError(f"timeout: 1..{MAX_TIMEOUT_SECONDS}")


def run_qwen_local(request: dict, params: dict, out_path: str) -> dict:
    """Реальный вызов, не заглушка. Идёт через llm-queue (`enqueue`, LLM-пул —
    тот же слот-семафор на llama-server, порт 8080, что и у уровня 1 лестницы
    в `ladder.level1_qwen_local`), не напрямую отсюда: два независимых
    запускателя на одном llama-server уже конфликтовали (2в). `--input` +
    `--prompt` идут как обычные argv-элементы subprocess.run — без shell,
    поэтому длинный материал и кириллица в инструкции не нуждаются в
    экранировании (в отличие от `--cmd` у openrouter выше)."""
    limit = int(params.get("max_input_chars", MAX_INPUT_CHARS))
    material = _read_input(request["input_path"], limit)
    timeout = int(params.get("timeout", 300))
    out = ladder._run_dispatcher(
        "enqueue", "--kind", "qwen-local-task",
        "--input", request["input_path"],
        "--prompt", params["instruction"],
        "--params", json.dumps({
            "input_char_limit": limit,
            "max_tokens": int(params.get("max_tokens", 1200)),
            "temperature": float(params.get("temperature", 0.1)),
            "timeout": timeout,
        }, ensure_ascii=False))
    fields = _await_job(out, timeout + 60)
    if fields.get("status") != "done":
        raise SystemExit(f"llm-queue задание не завершилось: "
                         f"{fields.get('status')} {fields.get('error_class') or ''}")
    result_path = fields.get("result_path")
    if not result_path or not os.path.isfile(result_path):
        raise SystemExit(f"llm-queue отчиталось done, но результата нет: {fields}")
    with open(result_path, encoding="utf-8") as fh:
        text = fh.read()
    with open(out_path, "w", encoding="utf-8") as fh:
        fh.write(text)
    return {
        "ok": True, "model": "qwen-local (llama-server через llm-queue)",
        "chars_in": len(material), "chars_out": len(text),
        "tokens_in": "", "tokens_out": "",  # call_llm не отдаёт usage — не оценка, пусто
        "output_path": out_path,
    }


REGISTRY = {
    "openrouter-llm": {
        "enabled": True,
        "title": "LLM-обработка файла через OpenRouter (через llm-queue)",
        "privacy": "материал уходит внешней модели — только уровень 0",
        "validate": validate_openrouter,
        "run": run_openrouter,
        "external": "openrouter",
    },
    "qwen-local": {
        "enabled": True,
        "title": "LLM-обработка файла локальной Qwen через llm-queue",
        "privacy": "материал не покидает машину — уровень 1",
        "validate": validate_qwen_local,
        "run": run_qwen_local,
        "external": "llama-local",
    },
}


def get(task_type: str) -> dict:
    spec = REGISTRY.get(task_type)
    if spec is None:
        raise SystemExit(f"неизвестный тип задачи: {task_type}. "
                         f"Известные: {', '.join(sorted(REGISTRY))}")
    if not spec["enabled"]:
        raise SystemExit(f"тип задачи {task_type} зарегистрирован, но выключен: "
                         f"{spec['title']}")
    return spec


if __name__ == "__main__":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    # Подпроцесс, который llm-queue поднимает по `--cmd` из run_openrouter() —
    # не режим для человека, вызывается только dispatcher.py enqueue-exec.
    if len(sys.argv) > 2 and sys.argv[1] == "--openrouter-job":
        sys.exit(_openrouter_job_main(sys.argv[2]))
    for name, spec in sorted(REGISTRY.items()):
        mark = "вкл" if spec["enabled"] else "разъём"
        print(f"{name:<16} [{mark:<6}] {spec['title']}\n{'':<26}{spec['privacy']}")
