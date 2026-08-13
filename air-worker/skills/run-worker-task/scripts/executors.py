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
import sys
import tempfile
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


def _await_job(job_out: str, timeout: int) -> dict:
    """Разобрать id из вывода `enqueue`/`enqueue-exec`, прогнать очередь на один
    шаг и вернуть поля `show`. Общий хвост обоих типов задачи ниже: air-worker
    только ставит задание и забирает результат, исполняет llm-queue.

    ИЗВЕСТНЫЙ ГАП llm-queue (см. SKILL.md § «Не хватает в интерфейсе»):
    `run --limit 1` берёт САМОЕ старое/приоритетное задание в очереди, не
    обязательно только что поставленное — при параллельных заявках чужой job
    может обработаться раньше нашего. Не правится здесь (раздел 6 задания)."""
    tokens = job_out.split()
    job_id = tokens[1] if len(tokens) > 1 else None
    if job_id is None:
        raise SystemExit(f"не удалось разобрать id задания llm-queue: {job_out!r}")
    ladder._run_dispatcher("run", "--limit", "1", timeout=timeout)
    return ladder._parse_show(ladder._run_dispatcher("show", "--job", job_id))


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
    if not str(params.get("instruction", "")).strip():
        raise ValueError("instruction: пустая инструкция — задача не определена")
    limit = int(params.get("max_input_chars", MAX_INPUT_CHARS))
    if not (1 <= limit <= MAX_INPUT_CHARS):
        raise ValueError(f"max_input_chars: 1..{MAX_INPUT_CHARS}")


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
                             f"{fields.get('status')} {fields.get('error', '')[:200]}")
        if not os.path.isfile(result_path):
            raise SystemExit("llm-queue отчиталось done, но результата нет "
                             f"({result_path})")
        with open(result_path, encoding="utf-8") as fh:
            return json.load(fh)
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
    text = payload["choices"][0]["message"]["content"]
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
                         f"{fields.get('status')} {fields.get('error', '')[:200]}")
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
