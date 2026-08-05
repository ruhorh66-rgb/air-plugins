"""Слить наработки всех роёв ruflo в одну базу — до того, как каталоги убирают.

ЗАЧЕМ. Состояние роя ruflo привязано к рабочему каталогу: команда из другого cwd
заводит там свой `.hive-mind` и `.claude-flow`. За неделю в контуре набралось девять
таких каталогов при нуле живых процессов. Свести их к одному рою правильно — но
просто удалить нельзя: внутри лежит ЕДИНСТВЕННЫЙ след того, что и зачем запускалось.

Ценного там четыре вида, и это не метрики:

  * `.hive-mind/sessions/hive-mind-prompt-*.txt` — ОБЪЕКТИВ роя, постановка задачи
    целиком. Нигде больше её нет;
  * `.claude-flow/hive-mind/state.json` — топология, состав рабочих, консенсус;
  * `.claude-flow/agents.json` — агенты, роли, время создания, счётчик задач;
  * `.claude-flow/metrics/*.json` и `logs/daemon.log` — единственные замеры того,
    как ruflo вёл себя на самом деле.

ЧТО ДЕЛАЕТ. Читает каждый каталог, раскладывает найденное по таблицам и кладёт
рядом СЫРЬЁ целиком. Ключ — SHA-256 содержимого, поэтому повторный запуск не
задваивает: слить можно сколько угодно раз, в том числе после новых прогонов.

ЧЕГО НЕ ДЕЛАЕТ. Ничего не удаляет и не переносит. Уборка — отдельный шаг
(`hive_single.ps1 -Action sweep`), и делать её осмысленно только ПОСЛЕ слияния.

    python consolidate_hives.py                    # слить всё найденное
    python consolidate_hives.py --report           # что уже лежит в базе
    python consolidate_hives.py --db <путь>
"""
from __future__ import annotations

import hashlib
import json
import re
import sqlite3
import sys
from datetime import datetime, timezone
from pathlib import Path

DB = Path(r"E:\-4-\ruflo-hive\hives.sqlite")
# r"E:\" не годится: raw-строка не может кончаться обратной косой
SEARCH_ROOTS = [Path(r"E:\-3-\Projects"), Path(r"E:\-8-"), Path("E:/")]
MAX_DEPTH = 3
MAX_RAW = 2_000_000          # сырьё крупнее не кладём — только отметку о нём

SCHEMA = """
CREATE TABLE IF NOT EXISTS swarm (
    swarm_id     TEXT PRIMARY KEY,
    name         TEXT,
    objective    TEXT,          -- постановка задачи; главная ценность
    topology     TEXT,
    workers      INTEGER,
    source_dir   TEXT NOT NULL,
    started_at   TEXT,
    merged_at    TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS agent (
    agent_id     TEXT PRIMARY KEY,
    swarm_dir    TEXT NOT NULL,
    agent_type   TEXT,
    role         TEXT,
    status       TEXT,
    task_count   INTEGER,
    created_at   TEXT
);
CREATE TABLE IF NOT EXISTS artifact (
    sha256       TEXT PRIMARY KEY,   -- ключ: повторный запуск не задваивает
    source_path  TEXT NOT NULL,
    project      TEXT NOT NULL,
    kind         TEXT NOT NULL,      -- objective | state | agents | metric | log | other
    bytes        INTEGER NOT NULL,
    modified_at  TEXT,
    content      TEXT,               -- сырьё целиком, если не громоздко
    merged_at    TEXT NOT NULL
);
"""

KIND = {
    "hive-mind-prompt": "objective",
    "state.json": "state",
    "agents.json": "agents",
    "daemon.log": "log",
    "daemon-state.json": "state",
}


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def _kind(p: Path) -> str:
    if p.name.startswith("hive-mind-prompt"):
        return "objective"
    if p.parent.name == "metrics":
        return "metric"
    return KIND.get(p.name, "other")


def find_hives() -> list[Path]:
    """Каталоги роёв. Корни поиска пересекаются — схлопываем по пути."""
    seen: dict[Path, None] = {}
    for root in SEARCH_ROOTS:
        if not root.exists():
            continue
        for name in (".hive-mind", ".claude-flow"):
            for d in root.glob("/".join(["*"] * 0) + name):
                seen.setdefault(d.resolve(), None)
            for depth in range(1, MAX_DEPTH + 1):
                for d in root.glob("/".join(["*"] * depth) + "/" + name):
                    if d.is_dir():
                        seen.setdefault(d.resolve(), None)
    return sorted(seen)


def merge(db: Path = DB) -> dict:
    db.parent.mkdir(parents=True, exist_ok=True)
    con = sqlite3.connect(db)
    con.executescript(SCHEMA)
    stats = {"каталогов": 0, "файлов": 0, "новых": 0, "роёв": 0, "агентов": 0}

    for hive in find_hives():
        project = str(hive.parent)
        stats["каталогов"] += 1
        for f in hive.rglob("*"):
            if not f.is_file():
                continue
            stats["файлов"] += 1
            try:
                blob = f.read_bytes()
            except Exception:
                continue
            sha = hashlib.sha256(blob).hexdigest()
            if con.execute("SELECT 1 FROM artifact WHERE sha256=?", (sha,)).fetchone():
                continue                       # уже слито — не задваиваем
            text = None
            if len(blob) <= MAX_RAW:
                try:
                    text = blob.decode("utf-8")
                except UnicodeDecodeError:
                    text = None
            con.execute(
                """INSERT INTO artifact (sha256, source_path, project, kind, bytes,
                                         modified_at, content, merged_at)
                   VALUES (?,?,?,?,?,?,?,?)""",
                (sha, str(f), project, _kind(f), len(blob),
                 datetime.fromtimestamp(f.stat().st_mtime, timezone.utc)
                 .isoformat(timespec="seconds"), text, _now()))
            stats["новых"] += 1

            if f.name.startswith("hive-mind-prompt") and text:
                sid = re.search(r"Swarm ID:\s*(\S+)", text)
                nm = re.search(r"Swarm Name:\s*(.+)", text)
                obj = re.search(r"Objective:\s*(.+?)(?:\n[A-ZА-Я📌🎯]|\n\n)", text, re.S)
                if sid:
                    con.execute(
                        """INSERT OR REPLACE INTO swarm
                           (swarm_id, name, objective, topology, workers, source_dir,
                            started_at, merged_at)
                           VALUES (?,?,?,?,?,?,?,?)""",
                        (sid.group(1), nm.group(1).strip() if nm else None,
                         obj.group(1).strip() if obj else None, None, None,
                         project, None, _now()))
                    stats["роёв"] += 1

            if f.name == "agents.json" and text:
                try:
                    for aid, a in (json.loads(text).get("agents") or {}).items():
                        con.execute(
                            """INSERT OR REPLACE INTO agent
                               (agent_id, swarm_dir, agent_type, role, status,
                                task_count, created_at) VALUES (?,?,?,?,?,?,?)""",
                            (aid, project, a.get("agentType"),
                             (a.get("config") or {}).get("role"), a.get("status"),
                             a.get("taskCount"), a.get("createdAt")))
                        stats["агентов"] += 1
                except Exception:
                    pass
    # ВТОРЫМ ПРОХОДОМ. Топология лежит в .claude-flow/hive-mind/state.json, а
    # объектив — в .hive-mind/sessions/. Каталоги обходятся по алфавиту, поэтому
    # state.json встречается РАНЬШЕ, чем заводится строка роя, и обогащение
    # «на лету» промахивалось молча. Данные к этому моменту уже в базе — берём
    # их оттуда, а не из файловой системы.
    for project, text in con.execute(
            "SELECT project, content FROM artifact "
            "WHERE kind='state' AND source_path LIKE '%hive-mind%state.json' "
            "AND content IS NOT NULL"):
        try:
            st = json.loads(text)
        except Exception:
            continue
        con.execute("UPDATE swarm SET topology=?, workers=? WHERE source_dir=?",
                    (st.get("topology"), len(st.get("workers") or []), project))

    stats["роёв"] = con.execute("SELECT count(*) FROM swarm").fetchone()[0]
    stats["агентов"] = con.execute("SELECT count(*) FROM agent").fetchone()[0]
    con.commit()
    con.close()
    return stats


def report(db: Path = DB) -> None:
    if not db.exists():
        print(f"база пуста: {db}")
        return
    con = sqlite3.connect(db)
    n_art = con.execute("SELECT count(*), sum(bytes) FROM artifact").fetchone()
    print(f"база: {db}")
    print(f"  сырья: {n_art[0]} файлов, {(n_art[1] or 0)/1024:.0f} КБ")
    print("  по видам:", dict(con.execute(
        "SELECT kind, count(*) FROM artifact GROUP BY kind ORDER BY 2 DESC")))
    print("  по проектам:", dict(con.execute(
        "SELECT project, count(*) FROM artifact GROUP BY project ORDER BY 2 DESC")))
    print(f"\n  роёв: {con.execute('SELECT count(*) FROM swarm').fetchone()[0]}"
          f" · агентов: {con.execute('SELECT count(*) FROM agent').fetchone()[0]}")
    for r in con.execute("""SELECT swarm_id, name, topology, workers, source_dir, objective
                            FROM swarm ORDER BY swarm_id"""):
        print(f"\n  {r[0]}  «{r[1]}»  {r[2] or '—'}, рабочих {r[3] or '—'}")
        print(f"     из {r[4]}")
        if r[5]:
            print(f"     задача: {' '.join(r[5].split())[:220]}")
    con.close()


def main(argv=None) -> int:
    sys.stdout.reconfigure(encoding="utf-8")
    args = list(sys.argv[1:] if argv is None else argv)
    db = Path(args[args.index("--db") + 1]) if "--db" in args else DB
    if "--report" not in args:
        st = merge(db)
        print("слито:", ", ".join(f"{k} {v}" for k, v in st.items()))
        print()
    report(db)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
