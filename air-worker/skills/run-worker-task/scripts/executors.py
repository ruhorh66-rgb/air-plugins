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
"""
from __future__ import annotations

import json
import os
import urllib.error
import urllib.request

# Локальный роутер: один OpenAI-совместимый адрес перед llama/Codex/Anthropic/
# OpenRouter (`E:\-4-\codex-shim\air_llm_router.py`). Своего ключа air-worker не
# держит и наружу сам не ходит — весь платный трафик остаётся за роутером с его
# free-only guard.
ROUTER_URL = os.environ.get("AIR_WORKER_ROUTER_URL",
                            "http://127.0.0.1:8090/v1/chat/completions")
MAX_INPUT_CHARS = 40_000


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
    """Инструкция и модель — из ПОДПИСАННЫХ параметров заявки, материал — из файла."""
    limit = int(params.get("max_input_chars", MAX_INPUT_CHARS))
    material = _read_input(request["input_path"], limit)
    body = {
        "model": params["model"],
        "messages": [
            {"role": "system", "content": params["instruction"]},
            {"role": "user", "content": material},
        ],
        "temperature": float(params.get("temperature", 0.2)),
    }
    try:
        payload = _post(ROUTER_URL, body, int(params.get("timeout", 600)))
    except urllib.error.HTTPError as exc:
        detail = exc.read().decode("utf-8", "replace")[:200]
        raise SystemExit(f"роутер отказал ({exc.code}): {detail}") from None
    except OSError as exc:
        raise SystemExit(f"роутер недоступен на {ROUTER_URL}: {exc}") from None
    if "error" in payload:
        raise SystemExit(f"роутер вернул ошибку: {str(payload['error'])[:200]}")
    text = payload["choices"][0]["message"]["content"]
    with open(out_path, "w", encoding="utf-8") as fh:
        fh.write(text)
    usage = payload.get("usage") or {}
    return {
        "ok": True,
        "model": params["model"],
        "chars_in": len(material),
        "chars_out": len(text),
        "tokens_in": int(usage.get("prompt_tokens", 0) or 0),
        "tokens_out": int(usage.get("completion_tokens", 0) or 0),
        "output_path": out_path,
    }


# --- Тип 2: РАЗЪЁМ. Локальная Qwen, уровень 1 — материал под границей приватности

def validate_qwen_local(params: dict) -> None:
    if not str(params.get("instruction", "")).strip():
        raise ValueError("instruction: пустая инструкция — задача не определена")


def run_qwen_local(request: dict, params: dict, out_path: str) -> dict:
    """Разъём, не реализация. Здесь и только здесь появится вызов llama-server
    (`AIR_ROUTER_LLAMA_URL`, порт 8080) — ядро, подпись, лок и слушатель при этом
    не меняются. Пока не поднят локальный сервер, честный отказ лучше тихого
    ухода материала уровня 1 во внешнюю модель."""
    raise SystemExit("qwen-local: разъём есть, исполнитель не реализован — "
                     "включать вместе с локальным llama-server")


REGISTRY = {
    "openrouter-llm": {
        "enabled": True,
        "title": "LLM-обработка файла через OpenRouter (локальный роутер)",
        "privacy": "материал уходит внешней модели — только уровень 0",
        "validate": validate_openrouter,
        "run": run_openrouter,
        "external": "openrouter",
    },
    "qwen-local": {
        "enabled": False,
        "title": "LLM-обработка файла локальной Qwen (разъём, не реализован)",
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
    import sys
    try:
        sys.stdout.reconfigure(encoding="utf-8")
    except Exception:
        pass
    for name, spec in sorted(REGISTRY.items()):
        mark = "вкл" if spec["enabled"] else "разъём"
        print(f"{name:<16} [{mark:<6}] {spec['title']}\n{'':<26}{spec['privacy']}")
