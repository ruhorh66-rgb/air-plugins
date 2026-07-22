"""Отправка ОДНОГО сообщения в чат BiP. Необратимое действие, только по команде ЛПР.

Отдельный файл, а не ключ монитора: чтение и отправка не должны жить в одном запуске,
чтобы опечатка в аргументах мониторинга не могла ничего отправить.

Границы (DEC-023 в редакции 21.07.2026): отправка разрешена ТОЛЬКО по явной команде ЛПР
на конкретный текст. Автоответы, рассылки, реакции на входящие — НЕ включены и требуют
отдельного решения. Скрипт отправляет ровно одно сообщение за запуск и требует --yes.

Порядок с проверками перед точкой невозврата:
  1) открыть чат и убедиться, что это он (имя печатается до отправки);
  2) вставить текст в Lexical-редактор через CDP Input.insertText
     (присваивание value Lexical игнорирует — нужен настоящий ввод);
  3) прочитать поле обратно и сверить с ожидаемым — ДО нажатия Enter;
  4) Enter;
  5) подтвердить фактом: поле очистилось и последний пузырь исходящий нужной длины.

Запуск:
  python bip_send.py --chat "фамилия" --text "текст сообщения" --yes
"""
import argparse
import json
import sys
import time

from bip_cdp import BipPage

JS_OPEN = """
(() => {
  const row = [...document.querySelectorAll('[itemtype="ChatListRow"]')]
    .find(r => (r.querySelector("[class*='header__name']")?.innerText || '')
                 .toLowerCase().includes(%s));
  if (!row) return null;
  (row.querySelector("[class*='_contact_']") || row).click();
  return {chat: (row.querySelector("[class*='header__name']")?.innerText || '').trim()};
})()
"""

JS_BOX = """
(() => {
  const box = [...document.querySelectorAll('[data-lexical-editor]')]
    .find(e => e.getAttribute('contenteditable') === 'true');
  if (!box) return null;
  box.focus();
  return {text: box.innerText.trim()};
})()
"""

JS_READBACK = """
(() => {
  const box = [...document.querySelectorAll('[data-lexical-editor]')]
    .find(e => e.getAttribute('contenteditable') === 'true');
  return box ? box.innerText.trim() : null;
})()
"""

JS_LAST = """
(() => {
  const KINDS = ['TextBubble','ImageBubble','RichLinkBubble','ContactBubble'];
  const items = [...document.querySelectorAll('[itemtype]')]
    .filter(e => KINDS.includes(e.getAttribute('itemtype')))
    .map(e => ({el: e, top: e.getBoundingClientRect().top}))
    .sort((a, b) => a.top - b.top);
  const last = items[items.length - 1]?.el;
  if (!last) return null;
  const t = (last.querySelector('.readonly-editor')?.innerText || '').trim();
  return {outgoing: !!last.querySelector("[class*='message_status__icon']"), len: t.length};
})()
"""


def send(needle: str, text: str) -> bool:
    with BipPage() as page:
        # ensure_ascii=False: кириллица идёт в JS как есть, кавычки при этом экранируются
        target = page.evaluate(JS_OPEN % json.dumps(needle.lower(), ensure_ascii=False))
        if not target:
            print(f"СТОП: чат по подстроке {needle!r} не найден", file=sys.stderr)
            return False
        print(f"чат: {target['chat']}")
        time.sleep(3)

        box = page.evaluate(JS_BOX)
        if box is None:
            print("СТОП: поле ввода не найдено", file=sys.stderr)
            return False
        if box["text"]:
            print(f"СТОП: в поле ввода уже есть черновик ({len(box['text'])} символов) — "
                  f"не затираю", file=sys.stderr)
            return False

        page.send("Input.insertText", {"text": text})
        time.sleep(1)

        back = (page.evaluate(JS_READBACK) or "").strip()
        if back != text:
            print(f"СТОП: в поле {len(back)} символов вместо {len(text)} — Enter не нажимаю",
                  file=sys.stderr)
            return False
        print(f"текст в поле сверен ({len(text)} символов), отправляю")

        # точка невозврата
        for t in ("keyDown", "keyUp"):
            page.send("Input.dispatchKeyEvent", {
                "type": t, "key": "Enter", "code": "Enter",
                "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13,
            })
        time.sleep(3)

        # подтверждаем фактом, а не предположением (Module_01 п. 6.28)
        left = (page.evaluate(JS_READBACK) or "").strip()
        last = page.evaluate(JS_LAST)
        ok = not left and last and last["outgoing"] and last["len"] == len(text)
        print(f"поле после Enter: {len(left)} символов; последний пузырь: "
              f"исходящий={last and last['outgoing']}, длина={last and last['len']}")
        print("ОТПРАВЛЕНО" if ok else "НЕ ПОДТВЕРЖДЕНО — проверить клиент вручную")
        return bool(ok)


def selftest():
    """Проверка без BiP: JS собирается корректно и подстрока экранируется."""
    js = JS_OPEN % json.dumps("о'брайен", ensure_ascii=False)
    assert "о'брайен" in js, "кириллица должна попадать в JS как есть, не \\uXXXX"
    js2 = JS_OPEN % json.dumps('кавычка"внутри', ensure_ascii=False)
    assert '\\"' in js2, "кавычка в имени должна экранироваться, иначе JS сломается"
    print("selftest OK")


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser(description="Отправить ОДНО сообщение в чат BiP")
    ap.add_argument("--chat", metavar="ИМЯ", help="часть имени чата")
    ap.add_argument("--text", help="точный текст сообщения")
    ap.add_argument("--yes", action="store_true", help="подтверждение: действие необратимо")
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()

    if args.selftest:
        selftest()
    elif not (args.chat and args.text):
        ap.error("нужны --chat и --text")
    elif not args.yes:
        ap.error("необратимое действие: добавьте --yes")
    else:
        sys.exit(0 if send(args.chat, args.text) else 1)
