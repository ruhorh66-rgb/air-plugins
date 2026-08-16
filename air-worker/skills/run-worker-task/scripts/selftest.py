r"""Самопроверка air-worker. Запуск: python selftest.py

Прогон командой, не самоотчёт (NEW_PLUGIN_CHECKLIST.md § 7). Без фреймворка —
хватает `assert`. Сети и keyring не трогает: ключ подписи подменяется фиксированным,
Telegram не вызывается вовсе, база и журнал уводятся во временный каталог.

Что здесь доказывается, а не заявляется:
  1. подмена ЛЮБОГО подписанного поля после создания заявки ломает проверку;
  2. подмена самого материала ломает проверку, даже если поля целы;
  3. «проверить не смог» = отказ, а не разрешение;
  4. лок различает три состояния и НЕ берётся при `unknown`;
  5. заявка переживает круг через SQLite, не потеряв подпись;
  6. реестр исполнителей отказывает на неизвестном и на выключенном типе;
  7. протокол не роняет работу и не пропускает материал в лог;
  8. строка замера дописывается в общий журнал;
  9. лестница (ladder.py, TASK-OBS-0054 §2а/2б): подъём без причины падает,
     неизвестная ступень падает, проверяльщик есть на все 4 типа заявок,
     старт всегда 0 без приоров и поднимается только при статистике+объёме,
     реестр ступеней 0..5 полон, ступень 0 реально проверяет файл на диске,
     ступени 4-5 по умолчанию исполняют суждение САМИ (Ф3, платный счётчик
     растёт), `execute=False` — старый режим отладки, конверт без исполнения;
 10. level0_check.py не путает код проекта CHT-020 с номером документа
     (регресс уже описанного в его собственном --selftest бага);
 11. level0_check.REGISTRY_DIR/ORIGINALS_DIR смотрят на 020_CKBA_Wiki, а не на
     несуществующую 030_CKBA_Wiki (регресс этой сессии);
 12. ladder.router_alive() всегда возвращает bool и не падает — не зависит от
     того, поднят ли роутер на 127.0.0.1:8090 прямо сейчас;
 13. apply_verification.evaluate() раскладывает по трём корзинам ровно по
     контракту verify_claim_level0 (подтверждено / не смог+кандидаты /
     не смог без кандидатов), не дублируя его собственный --selftest;
 14. apply_verification._write_back() заводит .bak один раз и не трогает уже
     существующий;
 15. apply_verification._write_back() правит только целевые
     verification_status/next_action — порядок колонок, прочие поля и
     нетронутые строки остаются буквально теми же.
"""
from __future__ import annotations

import csv
import io
import json
import os
import sys
import tempfile
import time
import urllib.error
from pathlib import Path, PurePosixPath

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

TMP = tempfile.mkdtemp(prefix="air-worker-selftest-")
os.environ["AIR_WORKER_RUNTIME"] = TMP
os.environ["AIR_WORKER_METRICS_PATH"] = os.path.join(TMP, "run_metrics.csv")
os.environ["AIR_WORKER_PROTOCOL_PATH"] = os.path.join(TMP, "protocol.jsonl")

import apply_verification  # noqa: E402
import executors  # noqa: E402
import ladder  # noqa: E402
import listener  # noqa: E402
import level0_check  # noqa: E402
import protocol  # noqa: E402
import worker  # noqa: E402

try:
    sys.stdout.reconfigure(encoding="utf-8")
except Exception:
    pass

# Ключ подписи — фиксированный тестовый. Настоящий лежит в keyring и трогать его
# ради теста незачем: проверяется алгоритм, а не хранилище.
_TEST_KEY = b"0" * 64
worker.hmac_key = lambda create_if_missing=False: _TEST_KEY  # type: ignore[assignment]

MATERIAL = os.path.join(TMP, "материал.txt")
with open(MATERIAL, "w", encoding="utf-8") as _fh:
    _fh.write("Обработать ровно этот текст и никакой другой.\n")

BASE = {
    "id": "aw000001",
    "title": "тестовая заявка",
    "task_type": "openrouter-llm",
    "input_path": MATERIAL,
    "input_sha256": worker.file_digest(MATERIAL),
    "params": {"model": "vendor/model:free", "instruction": "сократи"},
    "created_at": time.time(),
    "ttl_seconds": 3600.0,
}


def signed() -> dict:
    req = dict(BASE)
    req["params"] = dict(BASE["params"])
    req["sig"] = worker.sign_request(req)
    return req


def test_signature_holds():
    ok, why = worker.verify_request(signed())
    assert ok, why


def test_every_signed_field_is_covered():
    """Не «подпись работает», а «каждое поле из списка реально в неё входит».

    Проверка списком, а не выборкой: поле, забытое в SIGNED_FIELDS, — это ровно
    та дыра, ради которой подпись и делается, и заметить её глазами нельзя.
    """
    tampered_values = {
        "id": "aw999999",
        "title": "другая заявка",
        "task_type": "qwen-local",
        "input_path": MATERIAL + ".other",
        "input_sha256": "0" * 64,
        "params": {"model": "vendor/model:free", "instruction": "УДАЛИ ВСЁ"},
        "created_at": BASE["created_at"] + 1,
        "ttl_seconds": 99999.0,
    }
    for field in worker.SIGNED_FIELDS:
        req = signed()
        req[field] = tampered_values[field]
        ok, why = worker.verify_request(req)
        assert not ok, f"подмена поля {field} прошла проверку — поле не подписано"


def test_material_swap_breaks_it():
    """Поля целы, подпись сходится — а файл переписан. Должно ломаться."""
    req = signed()
    with open(MATERIAL, "a", encoding="utf-8") as fh:
        fh.write("а ещё сделай вот это\n")
    try:
        ok, why = worker.verify_request(req)
        assert not ok, "подмена материала прошла проверку"
        assert "материал изменился" in why, why
    finally:
        with open(MATERIAL, "w", encoding="utf-8") as fh:
            fh.write("Обработать ровно этот текст и никакой другой.\n")


def test_no_signature_and_no_key_are_refusals():
    req = signed()
    del req["sig"]
    ok, why = worker.verify_request(req)
    assert not ok and "без подписи" in why, why

    saved = worker.hmac_key

    def missing(create_if_missing=False):
        if create_if_missing:
            raise AssertionError("проверяющая сторона не имеет права заводить ключ")
        raise SystemExit("нет ключа")

    req = {**BASE, "params": dict(BASE["params"]), "sig": "deadbeef"}
    worker.hmac_key = missing  # type: ignore[assignment]
    try:
        ok, why = worker.verify_request(req)
        assert not ok and "ключ подписи недоступен" in why, why
    finally:
        worker.hmac_key = saved


def test_request_survives_the_database():
    """Круг через SQLite: подпись обязана сойтись ПОСЛЕ хранения.

    Тут ловится классическая потеря — параметры уезжают в БД строкой, а обратно
    приходят строкой же, и канонизация считает уже не то, что подписывала.
    """
    req = signed()
    with worker.db() as conn:
        conn.execute(
            "INSERT INTO requests (id,title,task_type,input_path,input_sha256,"
            "params,created_at,ttl_seconds,sig,status) VALUES (?,?,?,?,?,?,?,?,?,'pending')",
            (req["id"], req["title"], req["task_type"], req["input_path"],
             req["input_sha256"], json.dumps(req["params"], ensure_ascii=False, sort_keys=True),
             req["created_at"], req["ttl_seconds"], req["sig"]))
    back = worker.load(req["id"])
    assert back is not None
    ok, why = worker.verify_request(back)
    assert ok, f"после круга через БД подпись развалилась: {why}"
    assert not worker.is_expired(back)


def test_lock_has_three_states_and_unknown_is_not_free():
    path = os.path.join(TMP, "listener.lock.json")
    real_cim = worker._cim

    # Семантику lock_state проверяем детерминированно: в CI/Windows Sandbox
    # PowerShell/CIM может быть отключён, а тогда ожидание `free` для мёртвого PID
    # зависит не от кода, а от хоста.
    def fake_cim(query: str):
        if query == "ProcessId=999999":
            return []
        if query == f"ProcessId={os.getpid()}":
            return [{"ProcessId": os.getpid(), "CreationDate": "selftest"}]
        return real_cim(query)

    worker._cim = fake_cim
    try:
        state, why = worker.lock_state(path)
        assert state == "free", (state, why)

        with open(path, "w", encoding="utf-8") as fh:
            fh.write("это не json")
        state, why = worker.lock_state(path)
        assert state == "unknown", (state, why)
        got, note = worker.acquire(path)
        assert not got, "замок взят при unknown — непрочитанный замок счёлся свободным"

        with open(path, "w", encoding="utf-8") as fh:
            json.dump({"pid": 999_999, "pid_started": "нет такого", "since": "?"}, fh)
        state, why = worker.lock_state(path)
        assert state == "free", (state, why)

        got, note = worker.acquire(path, note="selftest")
        assert got, note
        state, why = worker.lock_state(path)
        assert state == "held", (state, why)
        got, note = worker.acquire(path)
        assert not got, "замок взят поверх живого держателя"
        worker.release(path)
        assert worker.lock_state(path)[0] == "free"
    finally:
        worker._cim = real_cim


def test_foreign_channel_state_is_one_of_three():
    state, why = worker.foreign_listener_state()
    assert state in ("held", "free", "unknown"), state
    assert why


def test_executor_registry_refuses_unknown_and_disabled():
    try:
        executors.get("нет-такого-типа")
        raise AssertionError("неизвестный тип задачи не отвергнут")
    except SystemExit as exc:
        assert "неизвестный тип" in str(exc)

    # TASK-OBS-0054: qwen-local теперь реальный исполнитель (через llm-queue),
    # не разъём — оба типа задачи в REGISTRY включены.
    assert "qwen-local" in executors.REGISTRY
    assert executors.REGISTRY["qwen-local"]["enabled"], "qwen-local должен быть подключён"
    spec = executors.get("qwen-local")
    spec["validate"]({"instruction": "сократи"})
    try:
        spec["validate"]({"instruction": "  "})
        raise AssertionError("пустая инструкция qwen-local принята")
    except ValueError:
        pass

    # "выключенный тип" по-прежнему отвергается — на временной записи, чтобы не
    # держать в проде исполнитель нарочно выключенным ради одного теста.
    executors.REGISTRY["_selftest_disabled"] = {"enabled": False, "title": "тест"}
    try:
        executors.get("_selftest_disabled")
        raise AssertionError("выключенный тип задачи не отвергнут")
    except SystemExit as exc:
        assert "выключен" in str(exc)
    finally:
        del executors.REGISTRY["_selftest_disabled"]

    spec = executors.get("openrouter-llm")
    spec["validate"]({"model": "vendor/model:free", "instruction": "сократи"})
    for bad in ({"model": "qwen", "instruction": "x"},
                {"model": "vendor/model:free", "instruction": "  "},
                {"model": "vendor/model:free", "instruction": "x", "max_input_chars": 0}):
        try:
            spec["validate"](bad)
            raise AssertionError(f"негодные параметры приняты: {bad}")
        except ValueError:
            pass


def test_metrics_row_is_appended():
    worker.record_metric("selftest", "vendor/model:free", 10, 20, 3, "accepted", "прогон теста")
    with open(worker.METRICS_PATH, encoding="utf-8") as fh:
        lines = [ln for ln in fh.read().splitlines() if ln.strip()]
    assert lines[0].startswith('"date"'), lines[0]
    assert "selftest" in lines[-1] and "accepted" in lines[-1], lines[-1]


def _insert_request(req: dict, status: str = "pending") -> None:
    with worker.db() as conn:
        conn.execute(
            "INSERT INTO requests (id,title,task_type,input_path,input_sha256,"
            "params,created_at,ttl_seconds,sig,status) VALUES (?,?,?,?,?,?,?,?,?,?)",
            (req["id"], req["title"], req["task_type"], req["input_path"],
             req["input_sha256"], json.dumps(req["params"], ensure_ascii=False),
             req["created_at"], req["ttl_seconds"], req["sig"], status))


def _fake_spec(name: str = "_selftest_direct") -> dict:
    def validate(params: dict) -> None:
        if not str(params.get("instruction", "")).strip():
            raise ValueError("instruction required")

    def run(req: dict, params: dict, out_path: str) -> dict:
        with open(out_path, "w", encoding="utf-8") as fh:
            fh.write("offline result")
        return {"ok": True, "model": "offline-test", "chars_in": 1,
                "chars_out": 14, "tokens_in": 1, "tokens_out": 1,
                "output_path": out_path}

    return {"enabled": True, "title": name, "privacy": "test",
            "validate": validate, "run": run, "external": "offline"}


def test_direct_create_ignores_held_foreign_listener_and_never_calls_telegram():
    """Default direct mode must not consult or call the shared Telegram channel."""
    name = "_selftest_direct"
    executors.REGISTRY[name] = _fake_spec(name)
    original_foreign, original_api = worker.foreign_listener_state, worker.api
    api_calls: list[str] = []
    worker.foreign_listener_state = lambda: ("held", "mock foreign listener")

    def forbidden_api(method, *args, **kwargs):
        api_calls.append(method)
        raise AssertionError("direct execution called Telegram")

    worker.api = forbidden_api
    try:
        assert worker.cmd_create([name, MATERIAL, "--param", "instruction=run"]) == 0
        with worker.db() as conn:
            row = conn.execute("SELECT status,result FROM requests ORDER BY created_at DESC LIMIT 1").fetchone()
        assert row["status"] == "done", row["status"]
        assert json.loads(row["result"])["ok"] is True
        assert not api_calls, api_calls
        assert MATERIAL not in Path(worker.METRICS_PATH).read_text(encoding="utf-8")
        assert MATERIAL not in Path(os.environ["AIR_WORKER_PROTOCOL_PATH"]).read_text(encoding="utf-8")
    finally:
        worker.foreign_listener_state, worker.api = original_foreign, original_api
        del executors.REGISTRY[name]


def test_direct_core_rejects_tampered_signed_contract():
    req = signed()
    req["id"] = "aw-invalid-direct"
    req["sig"] = worker.sign_request(req)
    _insert_request(req, "queued")
    with worker.db() as conn:
        conn.execute("UPDATE requests SET title=? WHERE id=?", ("подмена", req["id"]))
    outcome = worker.execute_request(worker.load(req["id"]), notify_telegram=False)
    assert outcome["status"] == "invalid", outcome
    assert worker.load(req["id"])["status"] == "invalid"


def test_execution_claim_is_idempotent_after_done():
    name = "_selftest_idempotent"
    calls = 0
    spec = _fake_spec(name)
    original_run = spec["run"]

    def counted_run(*args):
        nonlocal calls
        calls += 1
        return original_run(*args)

    spec["run"] = counted_run
    executors.REGISTRY[name] = spec
    req = {"id": "aw-idempotent", "title": "idempotent", "task_type": name,
           "input_path": MATERIAL, "input_sha256": worker.file_digest(MATERIAL),
           "params": {"instruction": "run", "privacy": "external"},
           "created_at": time.time(), "ttl_seconds": 3600.0}
    req["sig"] = worker.sign_request(req)
    _insert_request(req, "queued")
    try:
        assert worker.execute_request(worker.load(req["id"]))["status"] == "done"
        replay = worker.execute_request(worker.load(req["id"]))
        assert replay == {"id": req["id"], "status": "done", "replayed": True}
        assert calls == 1
    finally:
        del executors.REGISTRY[name]


def test_openrouter_free_only_and_privacy_contract_are_enforced():
    old = os.environ.pop("AIR_WORKER_FREE_ONLY", None)
    try:
        try:
            executors.validate_openrouter({"model": "vendor/paid", "instruction": "x"})
            raise AssertionError("paid model accepted in default free-only mode")
        except ValueError as exc:
            assert ":free" in str(exc)
    finally:
        if old is not None:
            os.environ["AIR_WORKER_FREE_ONLY"] = old
    spec = executors.get("openrouter-llm")
    ok, why = worker._task_contract_ok(
        spec, {"model": "vendor/model:free", "instruction": "x", "privacy": "local"})
    assert not ok and "локальный материал" in why, why


def test_legacy_listener_uses_shared_core_offline():
    name = "_selftest_legacy"
    executors.REGISTRY[name] = _fake_spec(name)
    req = {
        "id": "aw-legacy", "title": "legacy", "task_type": name,
        "input_path": MATERIAL, "input_sha256": worker.file_digest(MATERIAL),
        "params": {"instruction": "run", "privacy": "external"},
        "created_at": time.time(), "ttl_seconds": 3600.0,
    }
    req["sig"] = worker.sign_request(req)
    _insert_request(req)
    original_api, original_secret = worker.api, worker.secret
    calls: list[str] = []

    def fake_api(method, payload=None, timeout=30):
        calls.append(method)
        if method == "getUpdates":
            return {"ok": True, "result": [{"update_id": 991,
                "callback_query": {"id": "cb", "from": {"id": "77"},
                "data": "awrun:aw-legacy", "message": {"message_id": 1}}}]}
        return {"ok": True, "result": {}}

    worker.api = fake_api
    worker.secret = lambda account: "77"  # mocked legacy bot identity, no keyring
    try:
        assert listener.poll_once("77") == 1
        final = worker.load(req["id"])
        assert final["status"] == "done", final
        assert json.loads(final["result"])["ok"] is True
        assert "answerCallbackQuery" in calls and "sendMessage" in calls, calls
    finally:
        worker.api, worker.secret = original_api, original_secret
        del executors.REGISTRY[name]


def test_openrouter_errors_and_malformed_responses_do_not_report_done():
    tmp = tempfile.mkdtemp(prefix="air-worker-openrouter-")
    material = os.path.join(tmp, "input.txt")
    output = os.path.join(tmp, "output.txt")
    job_path = os.path.join(tmp, "job.json")
    Path(material).write_text("x", encoding="utf-8")
    Path(job_path).write_text(json.dumps({"input_path": material, "max_input_chars": 10,
        "model": "vendor/model:free", "instruction": "x", "temperature": 0,
        "timeout": 1, "out_path": output}), encoding="utf-8")
    original_post = executors._post
    try:
        executors._post = lambda *a, **k: {}
        assert executors._openrouter_job_main(job_path) == 1
        assert not os.path.exists(job_path + ".result.json")
        executors._post = lambda *a, **k: (_ for _ in ()).throw(
            urllib.error.HTTPError("http://router", 502, "bad gateway", {}, io.BytesIO(b"bad")))
        assert executors._openrouter_job_main(job_path) == 1
        assert not os.path.exists(job_path + ".result.json")
    finally:
        executors._post = original_post


def test_runtime_layout_accepts_synthetic_posix_paths():
    root = PurePosixPath("/var/tmp/air-worker")
    layout = worker.runtime_layout(root)
    assert layout["db"] == PurePosixPath("/var/tmp/air-worker/worker.sqlite3")
    assert layout["out"] == PurePosixPath("/var/tmp/air-worker/out")


def test_queue_targeting_requires_capability_and_never_uses_global_run():
    calls: list[tuple[str, ...]] = []

    def targeted_queue(*args: str, timeout: int = 600) -> str:
        calls.append(args)
        if args[:2] == ("capabilities", "--format"):
            return json.dumps({"capabilities": ["run-job", "wait-job", "cancel-job"]})
        if args[0] == "wait":
            return "  status         done\n  result_path    safe-result.txt\n"
        return ""

    with ladder._patch_global("_run_dispatcher", targeted_queue):
        fields = executors._await_job("задание 42 поставлено: test", 5)
    assert fields["status"] == "done"
    assert ("run", "--job", "42") in calls
    assert any(c[0] == "wait" and c[2] == "42" for c in calls)
    assert not any("--limit" in c for c in calls), calls

    with ladder._patch_global("_run_dispatcher", lambda *a, **k: "{}"):
        try:
            executors._require_targeted_queue()
            raise AssertionError("queue without capabilities accepted")
        except SystemExit as exc:
            assert "missing required targeted capability" in str(exc)


def test_ladder_escalate_without_reason_fails():
    """2а: подъём — только по названной причине, молчаливая эскалация запрещена."""
    for bad_reason in ("", "   ", None):
        try:
            ladder.escalate({"card_type": "risk"}, 1, bad_reason)
            raise AssertionError(f"escalate() с reason={bad_reason!r} должна была упасть")
        except ValueError:
            pass


def test_ladder_escalate_unknown_level_fails():
    try:
        ladder.escalate({"card_type": "risk"}, 99, "нет такой ступени")
        raise AssertionError("escalate() на несуществующую ступень должна была упасть")
    except ValueError:
        pass


def test_ladder_verify_exists_for_four_types():
    """2б: проверяльщик есть на все 4 типа заявок (плюс их синонимы card_type)."""
    for card_type in ("chronology", "decision", "task", "principle", "pattern",
                      "risk", "fork", "неизвестный-тип"):
        out = ladder.verify(card_type, {"claim_to_verify": "x"}, {})
        assert set(out) == {"passed", "reason"}, out
        assert isinstance(out["passed"], bool) and isinstance(out["reason"], str)


def test_ladder_verify_risk_fork_missing_components_match_fails():
    """Регресс: components_match отсутствующий в evidence — провал, не тихое 'подтверждено'."""
    evidence = {"claimed_amount": 100, "computed_amount": 100}  # без components_match
    out = ladder.verify("risk", {}, evidence)
    assert out["passed"] is False, out


def test_ladder_choose_start_level_defaults_to_zero():
    """Старт всегда 0 без приоров — независимо от типа заявки и объёма партии."""
    assert ladder.choose_start_level("chronology", volume=50, priors=[]) == 0
    assert ladder.choose_start_level("decision", volume=1, priors=[]) == 0


def test_ladder_choose_start_level_needs_volume_and_stats():
    """Приор поднимает старт только при статистике И объёме партии сразу."""
    hot = [{"card_type": "chronology", "start_level": 2, "n_outcomes": 10, "success_rate": 0.05}]
    assert ladder.choose_start_level("chronology", volume=10, priors=hot) == 2
    assert ladder.choose_start_level("chronology", volume=1, priors=hot) == 0


def test_ladder_levels_registry_complete():
    assert list(ladder.LEVELS) == [0, 1, 2, 3, 4, 5]
    for spec in ladder.LEVELS.values():
        assert "name" in spec and "cost_class" in spec and callable(spec["executor"])


def test_ladder_level0_script_checks_real_file():
    ok = ladder.escalate({"originals_path": __file__}, 0, "selftest — файл существует")
    assert ok["result"]["ok"] is True
    missing = ladder.escalate({"originals_path": os.path.join(TMP, "нет-такого-файла.pdf")},
                              0, "selftest — файла нет")
    assert missing["result"]["ok"] is False


def test_ladder_levels_4_5_hand_off_to_orchestrator():
    """`execute=False` — старый режим отладки (ladder.py docstring): конверт без исполнения."""
    for lvl in (4, 5):
        out = ladder.escalate({"card_type": "risk", "claim_to_verify": "x", "execute": False}, lvl,
                              "нужно суждение модели")
        assert out["result"]["pending_orchestrator"] is True


def test_ladder_levels_4_5_execute_by_default():
    """Ф3: `execute` по умолчанию True — ступени 4-5 исполняют суждение САМИ (тот же
    фейк очереди, что и ladder.py demo()), а не отдают конверт; платный счётчик растёт."""
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

    try:
        ladder.reset_paid_counter()
        with ladder._patch_global("_run_dispatcher", fake_claude_queue):
            for lvl in (4, 5):
                out = ladder.escalate({"card_type": "risk", "claim_to_verify": "x"}, lvl,
                                      "нужно суждение модели")
                assert "pending_orchestrator" not in out["result"]
                assert out["result"]["ok"] is True
                assert out["result"]["answer"] == "да, следует"
        assert ladder.paid_calls_used() == 2
    finally:
        os.unlink(fake_result.name)


def test_level0_check_cht020_regression():
    """Регресс CHT-020: код проекта/папки не должен всплывать как номер документа —
    без вырезания он совпадал бы с ЛЮБЫМ файлом внутри папки CHT-020."""
    assert level0_check.extract_numbers("управляющая стратегия CHT-020") == set()
    assert level0_check.extract_numbers("договор №4960/47, ДС №2") == {"4960"}


def test_level0_check_own_selftest_passes():
    level0_check._selftest()


def test_level0_check_paths_point_at_020_not_030():
    """Регресс этой сессии: реестр/оригиналы были заведены на несуществующей
    030_CKBA_Wiki — реальная папка 020_CKBA_Wiki."""
    for path in (level0_check.REGISTRY_DIR, level0_check.ORIGINALS_DIR):
        s = str(path)
        assert "020_CKBA_Wiki" in s, s
        assert "030_CKBA_Wiki" not in s, s


def test_ladder_router_alive_returns_bool_never_raises():
    """Роутер сейчас поднят на 127.0.0.1:8090, но тест не должен зависеть от
    этого факта — падать он не смеет ни при живом, ни при мёртвом роутере."""
    assert isinstance(ladder.router_alive(timeout=1.0), bool)


def test_ladder_run_claim_skips_missing_paid_cmds_not_raises():
    """TASK-OBS-0067: заявка ровно такого вида, какой строит
    `apply_verification._run_escalation` (card_type/claim_to_verify/
    candidates/openrouter_cmd/codex_cmd), но с ПУСТЫМИ openrouter_cmd/
    codex_cmd, гоняется через РЕАЛЬНЫЙ `ladder.run_claim` (не подмену
    целиком) — заявка должна дойти до ступеней 2 и 3 БЕЗ падения
    исключением и закрыться скипом обеих платных ступеней.

    Роутер второй ступени мокается ЖИВЫМ (`ladder._patch_global`), иначе
    ступень 2 скипнется по мёртвому порту РАНЬШЕ проверки самого поля
    `openrouter_cmd` — и тест не проверит то, что должен (см. ladder.demo(),
    пункт 10, и комментарий там же).

    НЕГАТИВНЫЙ КОНТРОЛЬ (обязателен по TASK-OBS-0067, проверено вручную, а
    не предположено): если откатить правку ladder.level2_openrouter_free/
    level3_codex (пустой cmd снова бросает ValueError вместо скипа), этот
    тест ПАДАЕТ — исключение всплывает через escalate()/run_claim() и
    main() ловит его как FAIL. Дословный вывод обеих попыток — в отчёте по
    TASK-OBS-0067 (до отката правки 1 = провал этого теста, после
    возврата — снова ok)."""
    claim = {
        "card_type": "risk",
        "claim_to_verify": "утверждение под проверкой",
        "candidates": ["cand1.pdf"],
        "openrouter_cmd": "",
        "codex_cmd": "",
    }
    priors_path = os.path.join(TMP, "ladder_priors_negtest.json")
    with ladder._patch_global("router_alive", lambda *a, **k: True):
        result = ladder.run_claim(claim, start_level=2, top_level=3, priors_path=priors_path)

    assert result["outcome"] == "не смог", result
    assert result["max_level_reached"] == 3, result
    assert [step["level"] for step in result["log"]] == [2, 3], result["log"]

    level2_result = result["log"][0]["result"]
    level3_result = result["log"][1]["result"]
    assert level2_result.get("skipped") is True, level2_result
    assert "openrouter_cmd" in level2_result.get("reason", ""), level2_result
    assert level3_result.get("skipped") is True, level3_result
    assert "codex_cmd" in level3_result.get("reason", ""), level3_result
    assert "исчерпана" in result["reason"], result["reason"]


def test_apply_verification_evaluate_buckets_three_outcomes():
    """evaluate() раскладывает по корзинам контрактом verify_claim_level0:
    подтверждено -> closed; «не смог»+кандидаты -> escalatable;
    «не смог» без кандидатов -> terminal."""
    rows = [
        {"verification_id": "SV-B-001", "registry": "r.csv"},
        {"verification_id": "SV-B-002", "registry": "r.csv"},
        {"verification_id": "SV-B-003", "registry": "r.csv"},
    ]
    fakes = {
        "SV-B-001": {"outcome": "подтверждено", "reason": "найдено", "candidates": ["X.pdf"]},
        "SV-B-002": {"outcome": "не смог", "reason": "неоднозначно", "candidates": ["a.pdf", "b.pdf"]},
        "SV-B-003": {"outcome": "не смог", "reason": "не найдено", "candidates": []},
    }

    def fake_verify(row, file_index):
        return fakes[row["verification_id"]]

    orig = apply_verification.level0_verify.verify_claim_level0
    apply_verification.level0_verify.verify_claim_level0 = fake_verify
    try:
        closed, escalatable, terminal = apply_verification.evaluate(rows, file_index=[])
    finally:
        apply_verification.level0_verify.verify_claim_level0 = orig

    assert [r["verification_id"] for r, _ in closed] == ["SV-B-001"]
    assert [r["verification_id"] for r, _ in escalatable] == ["SV-B-002"]
    assert [r["verification_id"] for r, _ in terminal] == ["SV-B-003"]


def test_apply_verification_writeback_backup_created_once():
    """.bak заводится ровно при первой записи и не пересоздаётся, если уже есть."""
    tmp_dir = tempfile.mkdtemp(prefix="selftest-apply-verification-bak-")
    csv_path = Path(tmp_dir) / "queue_test.csv"
    fieldnames = ["verification_id", "verification_status", "next_action"]
    with csv_path.open("w", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerow({"verification_id": "SV-X-001", "verification_status": "pending",
                         "next_action": ""})
    bak = Path(str(csv_path) + ".bak")
    assert not bak.exists()

    apply_verification._write_back(
        csv_path, {"SV-X-001": {"verification_status": "подтверждено", "next_action": "ok"}})
    assert bak.is_file()

    # руками портим .bak — вторая запись НЕ должна его перезаписать
    with bak.open("w", encoding="utf-8") as fh:
        fh.write("испорчено нарочно\n")
    apply_verification._write_back(
        csv_path, {"SV-X-001": {"verification_status": "опровергнуто", "next_action": "ok2"}})
    with bak.open(encoding="utf-8") as fh:
        assert fh.read() == "испорчено нарочно\n", ".bak перезаписан при существующем файле"


def test_apply_verification_writeback_preserves_columns_and_order():
    """Запись правит только verification_status/next_action целевой строки —
    остальные колонки, их порядок и нетронутые строки выживают буквально."""
    tmp_dir = tempfile.mkdtemp(prefix="selftest-apply-verification-cols-")
    csv_path = Path(tmp_dir) / "queue_test.csv"
    fieldnames = ["priority", "verification_id", "card_id", "verification_status",
                 "extra_column", "next_action"]
    rows_in = [
        {"priority": "P0", "verification_id": "SV-C-001", "card_id": "C1",
         "verification_status": "pending", "extra_column": "не трогать", "next_action": ""},
        {"priority": "P2", "verification_id": "SV-C-002", "card_id": "C2",
         "verification_status": "pending", "extra_column": "тоже не трогать", "next_action": ""},
    ]
    with csv_path.open("w", encoding="utf-8", newline="") as fh:
        writer = csv.DictWriter(fh, fieldnames=fieldnames)
        writer.writeheader()
        writer.writerows(rows_in)

    changed = apply_verification._write_back(
        csv_path, {"SV-C-001": {"verification_status": "подтверждено", "next_action": "готово"}})
    assert changed == 1

    with csv_path.open(encoding="utf-8", newline="") as fh:
        reader = csv.DictReader(fh)
        assert reader.fieldnames == fieldnames  # порядок колонок не изменился
        out_rows = list(reader)
    assert out_rows[0]["verification_status"] == "подтверждено"
    assert out_rows[0]["next_action"] == "готово"
    assert out_rows[0]["priority"] == "P0" and out_rows[0]["card_id"] == "C1"
    assert out_rows[0]["extra_column"] == "не трогать"
    # вторая строка не тронута НИ В ОДНОЙ колонке
    assert out_rows[1] == rows_in[1]


def main() -> int:
    tests = [v for k, v in sorted(globals().items()) if k.startswith("test_")]
    failed = 0
    for test in tests:
        try:
            test()
            print(f"ok   {test.__name__}")
        except Exception as exc:  # noqa: BLE001
            failed += 1
            print(f"FAIL {test.__name__}: {type(exc).__name__}: {exc}")
    protocol.selfcheck()
    print(f"\n{len(tests)} проверок, провалов: {failed}")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
