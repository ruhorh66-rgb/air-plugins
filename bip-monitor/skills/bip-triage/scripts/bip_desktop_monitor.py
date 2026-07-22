"""BiP Desktop → локальная LLM: триаж входящих через CDP-attach.

Механизм (подтверждён на живом клиенте 2026-07-21, SRVLM01):
BiP Desktop = Electron 22 поверх web.bip.com. Прицепляемся к залогиненному окну по CDP.

ДВА РЕЖИМА, они различаются последствиями — выбирать осознанно:

  list (по умолчанию) — опрос СПИСКА чатов, ничего не открывается. Список несёт полный
    текст последнего сообщения каждого чата (CSS обрезает лишь визуально), поэтому режим
    строго read-only: собеседник ничего не видит, счётчики непрочитанных не трогаются.
    Цена: видно только ПОСЛЕДНЕЕ сообщение чата.

  chat (--chat "часть имени") — открывает чат и читает всю подгруженную ленту (десятки
    сообщений), триаж по каждому входящему. ВНИМАНИЕ: открытие чата помечает сообщения
    прочитанными — собеседник видит отметку о прочтении, счётчик непрочитанных обнуляется.
    Это НЕ read-only. На собственном аккаунте применять по явному решению ЛПР; штатное
    место режима — аккаунт ИИ-ассистента, где отметки ожидаемы.

Локальная база клиента пуста (проверено 2026-07-21): IndexedDB держит только список чатов
и аватары, истории переписки на диске нет — она приходит с сервера при открытии чата.
Поэтому «прочитать прошлое, ничего не открывая» из файлов невозможно.

Границы (DEC-023, RISK-018/019): без автоотправки; сырьё и триаж — только на E:\\-4-,
не в git и не в RAG; в stdout не выводится ни содержимое, ни номера; Bitrix24 write не включён.

Селекторы (сняты bip_discover.py):
  строка чата     [itemtype="ChatListRow"]  + data-jid  (стабильны, не CSS-хеши)
  имя чата        [class*='header__name']
  время           [class*='header__time']
  превью текста   [class*='message__text']
  своё исходящее  [class*='message__icon'] svg   (галочка «доставлено/прочитано»)
  непрочитанные   [class*='extra__unread']
  сообщение       [itemtype="TextBubble"|"ImageBubble"|"RichLinkBubble"|"ContactBubble"]
  текст сообщения .readonly-editor
  разделитель дня [itemtype="date-bubble"]
  своё в ленте    [class*='message_status__icon']

Запуск:
  1) Get-Process BiP* | Stop-Process -Force
  2) & 'C:\\Program Files\\BiP\\BiP.exe' --remote-debugging-port=9222 --remote-allow-origins=*
  3) irm http://localhost:9222/json/version   → должен вернуть Browser/webSocketDebuggerUrl
  4) python bip_desktop_monitor.py [--minutes N] [--once] | [--chat "фамилия"] [--limit N]
"""
import argparse
import hashlib
import json
import pathlib
import sys
import time
import urllib.request

from bip_cdp import BipPage

LLM_URL = "http://localhost:8080/v1/chat/completions"
OUT_JSONL = r"E:\-4-\bip\triage.jsonl"      # ПДн: только на E:\-4-, не в git/RAG
POLL_SEC = 8

SYSTEM = ("Ты — ассистент триажа рабочих чатов. По сообщению ответь ОДНОЙ строкой JSON: "
          '{"тема": "...", "срочность": "низкая|средняя|высокая", "нужен_ответ": true|false}. '
          "Без пояснений.")

# Читаем только строки, где последнее сообщение ВХОДЯЩЕЕ (нет галочки статуса).
JS_ROWS = r"""
(() => [...document.querySelectorAll('[itemtype="ChatListRow"]')].map(r => {
  const q = s => r.querySelector(s);
  const txt = q("[class*='message__text']");
  const unread = q("[class*='extra__unread']");
  return {
    jid: r.getAttribute('data-jid') || '',
    chat: (q("[class*='header__name']")?.innerText || '').trim(),
    time: (q("[class*='header__time']")?.innerText || '').trim(),
    text: (txt?.innerText || '').trim(),
    outgoing: !!q("[class*='message__icon'] svg"),
    unread: unread ? parseInt(unread.innerText.trim(), 10) || 0 : 0,
  };
}))()
"""


# Лента открытого чата. Сортировка по положению на экране, а НЕ по порядку в DOM:
# подгрузка истории дописывает старые узлы в конец, из-за чего DOM-порядок врёт.
JS_CHAT = r"""
(() => {
  const KINDS = ['TextBubble','ImageBubble','RichLinkBubble','ContactBubble'];
  const items = [...document.querySelectorAll('[itemtype]')]
    .filter(e => KINDS.includes(e.getAttribute('itemtype'))
              || e.getAttribute('itemtype') === 'date-bubble')
    .map(e => ({el: e, top: e.getBoundingClientRect().top}))
    .sort((a, b) => a.top - b.top);
  let day = '';
  const out = [];
  for (const {el} of items) {
    if (el.getAttribute('itemtype') === 'date-bubble') { day = el.innerText.trim(); continue; }
    const body = el.querySelector('.readonly-editor');
    const time = el.querySelector("[class*='message_status__time']");
    out.push({
      day,
      kind: el.getAttribute('itemtype'),
      outgoing: !!el.querySelector("[class*='message_status__icon']"),
      time: (time?.innerText || '').trim(),
      text: (body?.innerText || el.innerText || '').trim(),
    });
  }
  return out;
})()
"""

# Открыть чат по части имени. Возвращает jid и имя — или null, если не найден.
JS_OPEN = """
(() => {
  const needle = %s;
  const row = [...document.querySelectorAll('[itemtype="ChatListRow"]')]
    .find(r => (r.querySelector("[class*='header__name']")?.innerText || '')
                 .toLowerCase().includes(needle));
  if (!row) return null;
  (row.querySelector("[class*='_contact_']") || row).click();
  return {jid: row.getAttribute('data-jid') || '',
          chat: (row.querySelector("[class*='header__name']")?.innerText || '').trim()};
})()
"""


def redact(jid: str) -> str:
    """Номер телефона в jid — ПДн. В консоль и в отчёты идёт только короткий хеш."""
    return hashlib.sha256(jid.encode("utf-8")).hexdigest()[:8]


def dedup_key(row: dict) -> str:
    """Стабильный ключ записи.

    sha256, а не встроенный hash(): hash() солится PYTHONHASHSEED и между запусками
    даёт разные значения, то есть при рестарте всё перечитывалось бы заново.

    Форма ключа зависит от режима, чтобы не обесценить уже накопленный журнал:
      list — (jid, текст): в списке у чата одно последнее сообщение;
      chat — (jid, день, время, текст): в ленте много сообщений одного чата.

    ponytail: стабильного message_id в DOM нет (у пузыря только itemtype и классы), а
    «день» ненадёжен — BiP не подставляет даты и рисует плейсхолдеры локали (`ДД.ММ.ВЕК`,
    `ддд`), корректно только «Вчера»/«Сегодня». Следствие: два сообщения с одинаковым
    текстом и одинаковым временем в РАЗНЫЕ старые дни схлопнутся в одну запись. Считаем
    приемлемым (нужно совпадение с точностью до минуты); чинится появлением message_id
    или взятием даты не из DOM, а из сетевого ответа сервера.
    """
    if row.get("source") == "chat":
        parts = (row["jid"], row.get("day", ""), row.get("time", ""), row["text"])
    else:
        parts = (row["jid"], row["text"])
    return hashlib.sha256("\x00".join(parts).encode("utf-8")).hexdigest()


def triage(text: str) -> str:
    body = json.dumps({"model": "local", "temperature": 0, "max_tokens": 200,
                       "messages": [{"role": "system", "content": SYSTEM},
                                    {"role": "user", "content": text}]}).encode("utf-8")
    req = urllib.request.Request(LLM_URL, body, {"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=120) as r:
        return json.load(r)["choices"][0]["message"]["content"].strip()


def triage_with_learning(item: dict) -> tuple:
    """Триаж локальной LLM + наложение выученных правил ЛПР.

    Правила применяются ПОСЛЕ модели и переопределяют только заданные ими поля: правка
    ЛПР должна побеждать модель детерминированно, а не «влиять на промпт».
    Если store недоступен (скилл ещё не инициализирован) — работаем без правил, а не падаем.
    """
    raw = triage(item["text"])
    try:
        import learning_store
        parsed = json.loads(raw)
    except ImportError:
        return raw, []
    except json.JSONDecodeError:
        return raw, []          # модель ответила не JSON — правила накладывать не на что
    try:
        result, fired = learning_store.apply_patterns(item, parsed)
    except Exception as e:      # store не должен ронять мониторинг
        print(f"warn: правила не применены: {e}", file=sys.stderr)
        return raw, []
    return json.dumps(result, ensure_ascii=False), fired


def incoming_rows(page: BipPage) -> list:
    return [r for r in page.evaluate(JS_ROWS) if r["text"] and not r["outgoing"]]


def load_seen(out: pathlib.Path) -> set:
    """Ключи из уже записанного triage.jsonl — иначе каждый рестарт дублирует весь список."""
    if not out.exists():
        return set()
    seen = set()
    with out.open(encoding="utf-8") as f:
        for line in f:
            try:
                seen.add(dedup_key(json.loads(line)))
            except (json.JSONDecodeError, KeyError):
                continue          # битая строка не должна ронять монитор
    return seen


def run_chat(needle: str, limit: int) -> int:
    """Открыть чат и оттриажить его входящие. ВНИМАНИЕ: помечает сообщения прочитанными."""
    out = pathlib.Path(OUT_JSONL)
    out.parent.mkdir(parents=True, exist_ok=True)
    seen, written = load_seen(out), 0
    with BipPage() as page:
        target = page.evaluate(JS_OPEN % json.dumps(needle.lower()))
        if not target:
            print(f"чат по подстроке {needle!r} не найден", file=sys.stderr)
            return 0
        print(f"открыт чат {redact(target['jid'])} — сообщения помечены прочитанными")

        # ждём, пока история подтянется с сервера: считаем пузыри, пока их число растёт
        prev, stable = -1, 0
        for _ in range(20):
            time.sleep(1)
            n = len(page.evaluate(JS_CHAT))
            stable = stable + 1 if n == prev else 0
            prev = n
            if stable >= 2 and n:
                break
        feed = page.evaluate(JS_CHAT)

    # ponytail: берём только уже подгруженное; более старое требует прокрутки ленты вверх —
    # добавить, когда понадобится глубина больше одной подгрузки.
    tail = [m for m in feed if m["text"]][-limit:]
    incoming = [m for m in tail if not m["outgoing"]]
    print(f"в ленте {len(feed)}, взято {len(tail)}, из них входящих {len(incoming)}")

    for m in incoming:
        rec = {"ts": time.time(), "source": "chat", "jid": target["jid"],
               "chat": target["chat"], "day": m["day"], "time": m["time"],
               "kind": m["kind"], "text": m["text"]}
        key = dedup_key(rec)
        if key in seen:
            continue
        seen.add(key)
        rec["triage"], fired = triage_with_learning(rec)
        if fired:
            rec["patterns_fired"] = fired
        with out.open("a", encoding="utf-8") as f:
            f.write(json.dumps(rec, ensure_ascii=False) + "\n")
        written += 1
        print(f"• [{m['day']} {m['time']}] {m['kind']} len={len(m['text'])} "
              f"→ {rec['triage'][:80]}" + (f"  [правила: {', '.join(fired)}]" if fired else ""))
    print(f"Готово: новых записей {written}.")
    return written


def run(minutes: float, once: bool) -> int:
    out = pathlib.Path(OUT_JSONL)
    out.parent.mkdir(parents=True, exist_ok=True)
    seen, written = load_seen(out), 0
    deadline = time.time() + minutes * 60
    with BipPage() as page:
        print(f"Прицепился к {page.url} — read-only мониторинг списка чатов "
              f"(уже в журнале: {len(seen)}).")
        while True:
            try:
                for row in incoming_rows(page):
                    key = dedup_key(row)
                    if key in seen:
                        continue
                    seen.add(key)
                    rec = {"ts": time.time(), "jid": row["jid"], "chat": row["chat"],
                           "time": row["time"], "unread": row["unread"],
                           "text": row["text"]}
                    rec["triage"], fired = triage_with_learning(rec)
                    if fired:
                        rec["patterns_fired"] = fired
                    with out.open("a", encoding="utf-8") as f:
                        f.write(json.dumps(rec, ensure_ascii=False) + "\n")
                    written += 1
                    # в консоль — без содержимого и имён (ПДн остаются в файле на E:\-4-)
                    print(f"• {redact(row['jid'])} unread={row['unread']} "
                          f"len={len(row['text'])} → {rec['triage'][:80]}"
                          + (f"  [правила: {', '.join(fired)}]" if fired else ""))
            except Exception as e:                     # клиент мог перезапуститься
                print("warn:", e, file=sys.stderr)
            if once or time.time() >= deadline:
                break
            time.sleep(POLL_SEC)
    print(f"Готово: обработано чатов {len(seen)}, записей в triage.jsonl {written}.")
    return written


def selftest():
    """Проверка чистой логики без BiP: дедуп стабилен, ПДн не утекают в вывод."""
    a = {"jid": "79990000000@tims.turkcell.com.tr", "text": "привет"}
    b = {"jid": "79990000000@tims.turkcell.com.tr", "text": "привет"}
    c = {"jid": "79990000001@tims.turkcell.com.tr", "text": "привет"}
    assert dedup_key(a) == dedup_key(b), "одинаковые чат+текст должны схлопываться"
    assert dedup_key(a) != dedup_key(c), "разные чаты не должны схлопываться"
    assert dedup_key(a) == hashlib.sha256(
        f"{a['jid']}\x00{a['text']}".encode("utf-8")).hexdigest(), "ключ должен быть стабилен между запусками"
    r = redact(a["jid"])
    assert len(r) == 8 and "7999" not in r, "номер не должен просматриваться в хеше"
    rows = [{"text": "in", "outgoing": False}, {"text": "out", "outgoing": True}, {"text": "", "outgoing": False}]
    kept = [x for x in rows if x["text"] and not x["outgoing"]]
    assert kept == [rows[0]], "берём только непустые входящие"

    # режим chat: одинаковый текст в разное время — разные записи, повтор — одна
    c1 = {"source": "chat", "jid": "79990000000@x", "day": "21 июля", "time": "19:08", "text": "ок"}
    c2 = {"source": "chat", "jid": "79990000000@x", "day": "21 июля", "time": "19:09", "text": "ок"}
    c3 = dict(c1)
    assert dedup_key(c1) != dedup_key(c2), "одинаковый текст в разное время — разные сообщения"
    assert dedup_key(c1) == dedup_key(c3), "то же сообщение — тот же ключ"
    assert dedup_key(c1) != dedup_key({"jid": c1["jid"], "text": c1["text"]}), \
        "ключи режимов не должны пересекаться"
    assert dedup_key({"jid": a["jid"], "text": a["text"]}) == dedup_key(a), \
        "записи без поля source читаются как list — старый журнал остаётся валидным"

    import tempfile
    with tempfile.TemporaryDirectory() as d:
        p = pathlib.Path(d) / "t.jsonl"
        p.write_text(json.dumps(a, ensure_ascii=False) + "\n{битая строка}\n", encoding="utf-8")
        assert load_seen(p) == {dedup_key(a)}, "рестарт должен видеть уже записанное, битую строку — пропускать"
        assert load_seen(pathlib.Path(d) / "нет.jsonl") == set(), "отсутствующий журнал = пустой seen"
    print("selftest OK")


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser()
    ap.add_argument("--minutes", type=float, default=15)
    ap.add_argument("--once", action="store_true", help="один проход и выход")
    ap.add_argument("--chat", metavar="ИМЯ",
                    help="открыть чат по части имени и оттриажить ленту "
                         "(ПОМЕЧАЕТ СООБЩЕНИЯ ПРОЧИТАННЫМИ)")
    ap.add_argument("--limit", type=int, default=50,
                    help="сколько последних сообщений ленты брать (по умолчанию 50)")
    ap.add_argument("--selftest", action="store_true", help="проверить логику без BiP")
    args = ap.parse_args()
    if args.selftest:
        selftest()
    elif args.chat:
        run_chat(args.chat, args.limit)
    else:
        run(args.minutes, args.once)
