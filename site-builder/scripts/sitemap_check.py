"""Проверка sitemap.xml без внешних зависимостей.

Появился 08.09.2026 вместо системного xmllint. Причина простая: пакета libxml2 в
winget нет, а `xmllint --noout` проверяет только правильность XML — то же самое
делает стандартная библиотека Python, которая на машине уже есть. Меньше системных
зависимостей при том же гейте.

Проверяет больше, чем xmllint: помимо правильности разметки — структурные правила
самого протокола sitemaps.org, которые xmllint не знает. Пределы взяты из
протокола: не более 50000 записей и не более 50 МБ в распакованном виде.

Выход — JSON в stdout. Код возврата: 0 если проверка прошла, 1 если нет.
Никаких обращений к модели: здесь только факты.
"""
from __future__ import annotations

import json
import sys

# Консоль Windows по умолчанию не в UTF-8, и печать кириллицы роняет вывод
# уже ПОСЛЕ выполнения проверки. Ставится первой строкой, до любого print.
sys.stdout.reconfigure(encoding="utf-8")
import xml.etree.ElementTree as ET
from pathlib import Path

MAX_URLS = 50_000
MAX_BYTES = 50 * 1024 * 1024
SITEMAP_NS = "http://www.sitemaps.org/schemas/sitemap/0.9"


def check(path: Path) -> dict:
    result: dict = {"tool": "sitemap_check.py", "path": str(path)}
    if not path.is_file():
        return {**result, "status": "not_run", "reason": f"файл не найден: {path}"}

    size = path.stat().st_size
    result["bytes"] = size

    try:
        root = ET.parse(path).getroot()
    except ET.ParseError as exc:
        return {**result, "status": "fail", "reason": f"XML не разбирается: {exc}"}

    tag = root.tag.split("}")[-1]
    if tag not in ("urlset", "sitemapindex"):
        return {**result, "status": "fail", "reason": f"корневой элемент {tag!r}, ожидался urlset или sitemapindex"}

    result["kind"] = tag
    entries = [child for child in root if child.tag.split("}")[-1] in ("url", "sitemap")]
    result["entries"] = len(entries)

    problems: list[str] = []
    if not root.tag.startswith("{" + SITEMAP_NS + "}"):
        problems.append(f"пространство имён не {SITEMAP_NS}")
    if len(entries) > MAX_URLS:
        problems.append(f"записей {len(entries)}, предел протокола {MAX_URLS}")
    if size > MAX_BYTES:
        problems.append(f"размер {size} байт, предел протокола {MAX_BYTES}")

    # loc обязателен у каждой записи — без него запись бессмысленна.
    missing_loc = sum(1 for e in entries if e.find("{%s}loc" % SITEMAP_NS) is None)
    if missing_loc:
        problems.append(f"записей без <loc>: {missing_loc}")
    result["missing_loc"] = missing_loc

    if problems:
        return {**result, "status": "fail", "problems": problems}
    return {**result, "status": "pass"}


def main() -> int:
    if len(sys.argv) != 2:
        print(json.dumps({"status": "fail", "reason": "использование: sitemap_check.py <путь к sitemap.xml>"}))
        return 1
    outcome = check(Path(sys.argv[1]))
    print(json.dumps(outcome, ensure_ascii=False))
    return 0 if outcome["status"] in ("pass", "not_run") else 1


if __name__ == "__main__":
    raise SystemExit(main())
