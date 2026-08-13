#!/usr/bin/env python3
"""level0_check.py — TASK-OBS-0054, ступень 0 лестницы ("скрипт, $0").

Для каждой строки verification_status=pending в четырёх реестрах
E:\\-5-\\030_CKBA_Wiki\\00_REGISTRY\\source_verification_queue*.csv проверяет
МЕХАНИЧЕСКИ, без чтения содержимого документов: есть ли в 05_ORIGINALS
файл, чьё имя правдоподобно соответствует хотя бы одному документу из
required_primary_sources — по совпадению номера (договора/письма/акта/
дела) или даты, извлечённых из ТЕКСТА претензии и из ПУТИ файла.

Что это НЕ делает (уровень 1+, не сюда):
  - не читает содержимое PDF/XLSX/DOCX;
  - не сверяет сумму, предмет, стороны ВНУТРИ документа;
  - не пишет ничего обратно в CSV реестров волта.

Запуск:
    python level0_check.py
Пишет отчёт в scratch-task-obs-0054-level0-report.json (путь ниже).

Самопроверка:
    python level0_check.py --selftest
"""
from __future__ import annotations

import csv
import json
import re
from pathlib import Path

REGISTRY_DIR = Path(r"E:\-5-\030_CKBA_Wiki\00_REGISTRY")
ORIGINALS_DIR = Path(r"E:\-5-\030_CKBA_Wiki\05_ORIGINALS")
REGISTRY_GLOB = "source_verification_queue*.csv"
OUT_PATH = Path(r"E:\-4-\ruflo-hive\scratch-task-obs-0054-level0-report.json")

# .md / .ocr.txt рядом с файлом — это извлечённый текстовый слой того же
# источника, не отдельный документ. Остальное (.pdf/.xlsx/.xls/.docx/.doc/
# .zip/.gsfx) — кандидаты в первоисточник.
SKIP_SUFFIXES = (".md", ".ocr.txt")

AMOUNT_RE = re.compile(r"\d[\d \u00a0]{2,}\d(?:,\d+)?\s*(?:руб|млн|коп)\.?")
DATE_DOT_RE = re.compile(r"\b(\d{2})\.(\d{2})\.(\d{4})\b")
DATE_ISO_RE = re.compile(r"\b(\d{4})-(\d{2})-(\d{2})\b")
DATE_DASH_RE = re.compile(r"\b(\d{2})-(\d{2})-(\d{4})\b")
YEAR_RE = re.compile(r"^(19|20)\d{2}$")
# "CHT-020" — код всего проекта/волта (и имя папки-хранилища переписки),
# не номер конкретного документа. Без вырезания "020" из него совпадёт с
# ЛЮБЫМ файлом внутри папки CHT-020 при любом упоминании "CHT-020"/
# "управляющая стратегия CHT-020" в тексте претензии — ложный сигнал.
PROJECT_CODE_RE = re.compile(r"\bCHT[-_]?020\b", re.IGNORECASE)


def extract_dates(text: str) -> set[str]:
    """Даты из текста/имени файла, нормализованные к YYYY-MM-DD."""
    out: set[str] = set()
    for d, mo, y in DATE_DOT_RE.findall(text):
        out.add(f"{y}-{mo}-{d}")
    for y, mo, d in DATE_ISO_RE.findall(text):
        out.add(f"{y}-{mo}-{d}")
    for d, mo, y in DATE_DASH_RE.findall(text):
        out.add(f"{y}-{mo}-{d}")
    return out


def extract_numbers(text: str) -> set[str]:
    """"Сильные" числовые токены (номер договора/письма/дела/акта и т.п.).

    Даты вырезаются первыми (иначе день/месяц/год дают ложные "номера"),
    затем денежные суммы (пробел-группировка тысяч + запятая-копейки —
    иначе "19 553 817,02" рассыпается на псевдо-номера "553"/"817").
    Голый 4-значный год без остального контекста тоже не считается
    номером — иначе он совпадёт почти с любым файлом с датой того же года.
    """
    t = PROJECT_CODE_RE.sub(" ", text)
    t = DATE_DOT_RE.sub(" ", t)
    t = DATE_ISO_RE.sub(" ", t)
    t = DATE_DASH_RE.sub(" ", t)
    t = AMOUNT_RE.sub(" ", t)
    nums = set()
    for tok in re.findall(r"\d+", t):
        if len(tok) < 3:
            continue
        if YEAR_RE.match(tok):
            continue
        nums.add(tok)
    return nums


def file_signals(path: Path) -> tuple[set[str], set[str]]:
    """Числа и даты, извлечённые из полного пути файла (папка+имя)."""
    text = str(path)
    return extract_numbers(text), extract_dates(text)


def load_originals(root: Path) -> list[tuple[Path, set[str], set[str]]]:
    index = []
    for p in root.rglob("*"):
        if not p.is_file():
            continue
        name_lower = p.name.lower()
        if any(name_lower.endswith(suf) for suf in SKIP_SUFFIXES):
            continue
        nums, dates = file_signals(p)
        index.append((p, nums, dates))
    return index


def split_sources(required_primary_sources: str) -> list[str]:
    return [s.strip() for s in required_primary_sources.split(";") if s.strip()]


def find_plausible_match(
    item_text: str, file_index: list[tuple[Path, set[str], set[str]]]
) -> Path | None:
    item_nums = extract_numbers(item_text)
    item_dates = extract_dates(item_text)
    if not item_nums and not item_dates:
        return None  # нет опознаваемого номера/даты — level 0 честно не находит
    for path, f_nums, f_dates in file_index:
        if item_nums & f_nums:
            return path
        if item_dates & f_dates:
            return path
    return None


def check_registry(
    csv_path: Path, file_index: list[tuple[Path, set[str], set[str]]]
) -> list[dict]:
    rows = []
    with csv_path.open(encoding="utf-8-sig", newline="") as fh:
        for row in csv.DictReader(fh):
            if row.get("verification_status") != "pending":
                continue
            items = split_sources(row.get("required_primary_sources", ""))
            matched_item = None
            matched_file = None
            for item in items:
                m = find_plausible_match(item, file_index)
                if m is not None:
                    matched_item, matched_file = item, m
                    break
            rows.append(
                {
                    "verification_id": row["verification_id"],
                    "card_type": row.get("card_type", ""),
                    "registry": csv_path.name,
                    "required_items": items,
                    "found": matched_item is not None,
                    "matched_item": matched_item,
                    "matched_file": str(matched_file) if matched_file else None,
                }
            )
    return rows


def main() -> None:
    file_index = load_originals(ORIGINALS_DIR)
    all_rows: list[dict] = []
    for csv_path in sorted(REGISTRY_DIR.glob(REGISTRY_GLOB)):
        all_rows.extend(check_registry(csv_path, file_index))

    total_pending = len(all_rows)
    found = [r for r in all_rows if r["found"]]
    not_found = [r for r in all_rows if not r["found"]]

    def bucket(rows: list[dict], key: str) -> dict:
        out: dict[str, dict[str, int]] = {}
        for r in rows:
            k = r[key]
            b = out.setdefault(k, {"pending": 0, "found": 0, "not_found": 0})
            b["pending"] += 1
            b["found" if r["found"] else "not_found"] += 1
        return out

    report = {
        "total_pending": total_pending,
        "found_plausible_source": len(found),
        "no_plausible_match": len(not_found),
        "by_registry": bucket(all_rows, "registry"),
        "by_card_type": bucket(all_rows, "card_type"),
        "examples_found": [r["verification_id"] for r in found[:5]],
        "examples_not_found": [r["verification_id"] for r in not_found[:5]],
    }

    OUT_PATH.parent.mkdir(parents=True, exist_ok=True)
    OUT_PATH.write_text(
        json.dumps(report, ensure_ascii=False, indent=2), encoding="utf-8"
    )

    print(json.dumps(report, ensure_ascii=False, indent=2))
    print(f"\nfiles indexed in 05_ORIGINALS: {len(file_index)}")
    print(f"report written to: {OUT_PATH}")


def _selftest() -> None:
    assert extract_dates("Ответ ЦКБА от 24.04.2026 задал срок") == {"2026-04-24"}
    assert extract_dates("CKBA-DS4-5239-47-2026-03-20.pdf") == {"2026-03-20"}
    assert extract_dates("18-06-2026.pdf") == {"2026-06-18"}

    # голый год не должен попадать в "номера"
    assert "2026" not in extract_numbers("документ 2026 года")
    # денежная сумма с пробел-группировкой не должна дать псевдо-номер
    assert extract_numbers("перенос аванса 19 553 817,02 руб.") == set()
    # короткие числа (< 3 цифр, включая "47" и "№2") отсекаются как шум
    assert extract_numbers("договор №4960/47, ДС №2") == {"4960"}
    # "CHT-020" — код проекта/папки, не номер документа; не должен всплыть
    assert extract_numbers("управляющая стратегия CHT-020") == set()

    idx = [
        (Path("PDF/SRC-020-CKBA-PISMO-47-6935_18-06-2026.pdf"), *file_signals(
            Path("PDF/SRC-020-CKBA-PISMO-47-6935_18-06-2026.pdf")
        )),
        (Path("SMETA-5239-47/КС-2 № 7 Вынос теплотрассы.xlsx"), *file_signals(
            Path("SMETA-5239-47/КС-2 № 7 Вынос теплотрассы.xlsx")
        )),
    ]
    assert find_plausible_match("письмо ЦКБА №47-6935", idx) is not None
    assert find_plausible_match("письмо ЦКБА №99-0000 от 01.01.2099", idx) is None
    assert find_plausible_match("переписка сторон", idx) is None  # нет номера/даты

    print("selftest ok")


if __name__ == "__main__":
    import sys

    if "--selftest" in sys.argv:
        _selftest()
    else:
        main()
