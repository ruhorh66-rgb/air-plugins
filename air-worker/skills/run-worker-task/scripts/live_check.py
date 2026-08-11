r"""Живой прогон одного исполнителя без Telegram — стадия СБОРКИ, не рабочий вызов.

Зачем отдельно от `selftest.py`: тот не ходит в сеть вообще и потому проходит на
машине без роутера. Здесь наоборот — доказывается, что тип задачи `openrouter-llm`
делает НАСТОЯЩИЙ вызов через `E:\-4-\codex-shim\air_llm_router.py`, а не отчитывается
о нём (Ф6 задания TASK-OBS-0046).

Гейт ЛПР этим не обходится: рабочая заявка идёт через `worker.py create` и кнопку.
Здесь исполнитель вызывается напрямую на файле, который положил сам проверяющий.

Запуск: python live_check.py [<файл>] [<модель>]
"""
from __future__ import annotations

import json
import os
import sys
import tempfile
import time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

import executors  # noqa: E402
import protocol  # noqa: E402
import worker  # noqa: E402

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

# Список бесплатных моделей OpenRouter меняется без предупреждения: 11.08.2026
# `meta-llama/llama-3.3-70b-instruct:free` перестала быть бесплатной прямо в этом
# прогоне, и роутер честно отказал вместо платного вызова (free-only guard). Список
# живых — `https://openrouter.ai/api/v1/models`, фильтр по суффиксу `:free`.
DEFAULT_MODEL = "google/gemma-4-26b-a4b-it:free"


def main(argv: list[str]) -> int:
    material = argv[0] if argv else None
    model = argv[1] if len(argv) > 1 else DEFAULT_MODEL
    tmp = None
    if not material:
        tmp = tempfile.mkdtemp(prefix="air-worker-live-")
        material = os.path.join(tmp, "образец.txt")
        with open(material, "w", encoding="utf-8") as fh:
            fh.write("Смета подрядчика на 2026 год. НДС 22% с 01.01.2026. "
                     "Договорная цена без увеличения. Срок — четыре недели.\n")
    request = {
        "id": "live" + time.strftime("%H%M%S"),
        "task_type": "openrouter-llm",
        "input_path": os.path.abspath(material),
        "input_sha256": worker.file_digest(os.path.abspath(material)),
        "params": {"model": model, "instruction": "Ответь одной строкой на русском: о чём этот текст."},
    }
    os.makedirs(worker.OUT_DIR, exist_ok=True)
    out_path = os.path.join(worker.OUT_DIR, request["id"] + ".txt")
    started = time.time()
    with protocol.stage("executor_run", external="openrouter",
                        task_type=request["task_type"]):
        result = executors.run_openrouter(request, request["params"], out_path)
    took = int(time.time() - started)
    worker.record_metric(f"air-worker live_check {request['id']}", model,
                         result.get("tokens_in", ""), result.get("tokens_out", ""),
                         took, "accepted", "живой прогон при сборке TASK-OBS-0046")
    print(json.dumps(result, ensure_ascii=False, indent=1))
    print(f"\nответ модели ({result['chars_out']} знаков) лежит в {out_path}")
    with open(out_path, encoding="utf-8") as fh:
        print("---\n" + fh.read()[:500])
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
