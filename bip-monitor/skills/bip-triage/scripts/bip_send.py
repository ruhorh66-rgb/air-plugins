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
from pathlib import Path

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


JS_FOCUS_END = """
(() => {
  const box = [...document.querySelectorAll('[data-lexical-editor]')]
    .find(e => e.getAttribute('contenteditable') === 'true');
  if (!box) return false;
  box.focus();
  const sel = window.getSelection();
  const range = document.createRange();
  range.selectNodeContents(box);
  range.collapse(false);           // каретку в конец
  sel.removeAllRanges();
  sel.addRange(range);
  return true;
})()
"""


def press_enter(page) -> None:
    """Enter, который BiP действительно понимает как «отправить».

    Перед нажатием ОБЯЗАТЕЛЬНО вернуть фокус и поставить каретку в конец поля. Без этого
    отправка срабатывает через раз: фокус теряется между Input.insertText и клавишей,
    Enter уходит «в никуда», текст остаётся в поле. Поймано 2026-07-23 на втором
    сообщении подряд — первое ушло, второе нет, при одинаковом коде.

    Найдено 2026-07-23. Без поля `text` Chromium НЕ генерирует событие keypress: браузер
    обрабатывает такой Enter как перенос строки в contenteditable, а приложение его вообще
    не видит. Внешне это неотличимо от отправки — поле выглядит так же, — но сообщение не
    уходит, а в черновик добавляется \\n (было видно по росту длины на 3 символа).

    Нужны все три события подряд, включая `type: "char"`. Ctrl+Enter не помогает: он
    убирает перенос, но отправку тоже не запускает.
    """
    page.evaluate(JS_FOCUS_END)
    time.sleep(0.5)
    page.send("Input.dispatchKeyEvent", {
        "type": "keyDown", "key": "Enter", "code": "Enter",
        "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13,
        "text": "\r", "unmodifiedText": "\r",
    })
    page.send("Input.dispatchKeyEvent", {
        "type": "char", "key": "Enter",
        "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13,
        "text": "\r", "unmodifiedText": "\r",
    })
    page.send("Input.dispatchKeyEvent", {
        "type": "keyUp", "key": "Enter", "code": "Enter",
        "windowsVirtualKeyCode": 13, "nativeVirtualKeyCode": 13,
    })


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
        press_enter(page)
        time.sleep(3)

        # подтверждаем фактом, а не предположением (Module_01 п. 6.28)
        left = (page.evaluate(JS_READBACK) or "").strip()
        last = page.evaluate(JS_LAST)
        ok = not left and last and last["outgoing"] and last["len"] == len(text)
        print(f"поле после Enter: {len(left)} символов; последний пузырь: "
              f"исходящий={last and last['outgoing']}, длина={last and last['len']}")
        print("ОТПРАВЛЕНО" if ok else "НЕ ПОДТВЕРЖДЕНО — проверить клиент вручную")
        return bool(ok)


JS_OPEN_DIAL = """
(() => {
  const b = document.querySelector('.p-speeddial-button');
  if (!b) return null;
  b.click();
  return true;
})()
"""

JS_PICK_DOC = """
(() => {
  const a = [...document.querySelectorAll('.p-speeddial-action')]
    .find(e => (e.getAttribute('aria-label') || '').trim() === 'Документ');
  if (!a) return null;
  a.click();
  return true;
})()
"""

JS_ATTACH_STATE = """
(() => {
  const box = [...document.querySelectorAll('[data-lexical-editor]')]
    .find(e => e.getAttribute('contenteditable') === 'true');
  const names = [...document.querySelectorAll('[class*="attach"],[class*="preview"],[class*="file"]')]
    .map(e => (e.innerText || '').trim())
    .filter(t => t && t.length < 120);
  return {draft: box ? box.innerText.trim() : null, labels: [...new Set(names)].slice(0, 6)};
})()
"""


def send_file(needle: str, path: str, caption: str = "") -> bool:
    """Отправить ОДИН файл в чат BiP.

    ⚠️ НЕ РАБОТАЕТ на текущей сборке (Electron 22 / Chrome 108, проверено 2026-07-23).
    Код доведён до конца и корректен, упирается в ограничение платформы.

    Что установлено проверкой:
      * меню вложений открывается: `.p-speeddial-button` → `.p-speeddial-action`
        с aria-label «Документ» (есть также «Фото и видео»);
      * клик по «Документ» ПЕРЕКЛЮЧАЕТ уже существующий `input[type=file]`:
        accept меняется с `image/*,video/*` на `document`. Отдельного input для
        документов в DOM нет, элемент переиспользуется;
      * `Page.setInterceptFileChooserDialog` команду ПРИНИМАЕТ, но событие
        `Page.fileChooserOpened` не приходит (ждали 15 с);
      * `DOM.setFileInputFiles` принимает и objectId, и nodeId, ошибки НЕ возвращает —
        и не делает ничего: `input.files.length` остаётся 0. Проверено на пути с
        кириллицей и на коротком ASCII-пути, разницы нет.

    То есть Electron реализует эти методы CDP формально. Тот же класс, что описан в
    bip_cdp.py про Browser.setDownloadBehavior и Playwright: часть протокола заявлена,
    но не действует.

    Рабочие пути отправки файла, если он понадобится:
      1) ЛПР отправляет вручную — одно действие, ничего дописывать не нужно;
      2) computer-use: клик мышью по скрепке и ввод пути в системном диалоге. Требует
         отдельного разрешения на приложение и устойчив к смене вёрстки хуже, чем CDP;
      3) отправить содержимое ТЕКСТОМ через send() — годится для короткой выжимки,
         не для документа на несколько страниц.

    Функция оставлена целиком намеренно: разведка DOM в ней верная, и если сборку BiP
    обновят до Electron с рабочим DOM.setFileInputFiles, она заработает без правок.
    """
    file_path = Path(path).resolve()
    if not file_path.is_file():
        print(f"СТОП: файла нет — {file_path}", file=sys.stderr)
        return False
    size = file_path.stat().st_size
    print(f"файл: {file_path.name} ({size:,} байт)")

    with BipPage() as page:
        target = page.evaluate(JS_OPEN % json.dumps(needle.lower(), ensure_ascii=False))
        if not target:
            print(f"СТОП: чат по подстроке {needle!r} не найден", file=sys.stderr)
            return False
        print(f"чат: {target['chat']}")
        time.sleep(3)

        page.send("Page.enable")
        page.send("DOM.enable")
        page.send("Page.setInterceptFileChooserDialog", {"enabled": True})

        try:
            if not page.evaluate(JS_OPEN_DIAL):
                print("СТОП: кнопка вложений не найдена", file=sys.stderr)
                return False
            time.sleep(1.5)

            if not page.evaluate(JS_PICK_DOC):
                print("СТОП: пункт «Документ» не найден", file=sys.stderr)
                return False

            # Клик по «Документ» переключает accept существующего input на 'document'.
            # Событие fileChooserOpened Electron не шлёт, поэтому не ждём его, а работаем
            # с input напрямую — и сразу проверяем, принял ли он файл.
            page.send("DOM.enable")
            root = page.send("DOM.getDocument", {"depth": -1})["root"]["nodeId"]
            node = page.send("DOM.querySelector",
                             {"nodeId": root, "selector": 'input[accept="document"]'}).get("nodeId")
            if not node:
                print("СТОП: input для документов не появился", file=sys.stderr)
                return False

            page.send("DOM.setFileInputFiles", {"files": [str(file_path)], "nodeId": node})
            time.sleep(2)

            # Проверка ДО точки невозврата: команда проходит без ошибки даже когда
            # ничего не делает, поэтому верим только состоянию элемента.
            accepted = page.evaluate(
                '(() => { const e=[...document.querySelectorAll("input[type=file]")]'
                '.find(x=>x.accept==="document"); return e ? e.files.length : -1; })()')
            if not accepted or accepted < 1:
                print("СТОП: файл не принят элементом (files=%s). На Electron 22 команда "
                      "DOM.setFileInputFiles не действует — см. docstring, отправлять "
                      "нечем." % accepted, file=sys.stderr)
                return False
        finally:
            # вернуть штатное поведение, иначе следующий выбор файла в BiP руками зависнет
            page.send("Page.setInterceptFileChooserDialog", {"enabled": False})

        time.sleep(4)
        state = page.evaluate(JS_ATTACH_STATE)
        print(f"состояние после подстановки: {state}")

        if caption:
            page.send("Input.insertText", {"text": caption})
            time.sleep(1)

        # точка невозврата
        press_enter(page)
        time.sleep(5)

        last = page.evaluate(JS_LAST_ANY)
        ok = bool(last and last.get("outgoing"))
        print(f"последний пузырь: {last}")
        print("ОТПРАВЛЕНО" if ok else "НЕ ПОДТВЕРЖДЕНО — проверить клиент вручную")
        return ok


JS_LAST_ANY = """
(() => {
  const items = [...document.querySelectorAll('[itemtype]')]
    .filter(e => /Bubble$/.test(e.getAttribute('itemtype') || ''))
    .map(e => ({el: e, top: e.getBoundingClientRect().top}))
    .sort((a, b) => a.top - b.top);
  const last = items[items.length - 1]?.el;
  if (!last) return null;
  return {
    kind: last.getAttribute('itemtype'),
    outgoing: !!last.querySelector("[class*='message_status__icon']"),
    text: (last.innerText || '').trim().slice(0, 80),
  };
})()
"""


def selftest():
    """Проверка без BiP: JS собирается корректно и подстрока экранируется."""
    js = JS_OPEN % json.dumps("о'брайен", ensure_ascii=False)
    assert "о'брайен" in js, "кириллица должна попадать в JS как есть, не \\uXXXX"
    js2 = JS_OPEN % json.dumps('кавычка"внутри', ensure_ascii=False)
    assert '\\"' in js2, "кавычка в имени должна экранироваться, иначе JS сломается"
    print("selftest OK")


if __name__ == "__main__":
    sys.stdout.reconfigure(encoding="utf-8")
    ap = argparse.ArgumentParser(description="Отправить ОДНО сообщение или файл в чат BiP")
    ap.add_argument("--chat", metavar="ИМЯ", help="часть имени чата")
    ap.add_argument("--text", help="точный текст сообщения")
    ap.add_argument("--file", metavar="ПУТЬ", help="файл-вложение (документ)")
    ap.add_argument("--caption", default="", help="подпись к файлу")
    ap.add_argument("--yes", action="store_true", help="подтверждение: действие необратимо")
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()

    if args.selftest:
        selftest()
    elif args.text and args.file:
        ap.error("--text и --file вместе не отправляются: для файла есть --caption")
    elif not (args.chat and (args.text or args.file)):
        ap.error("нужны --chat и (--text либо --file)")
    elif not args.yes:
        ap.error("необратимое действие: добавьте --yes")
    elif args.file:
        sys.exit(0 if send_file(args.chat, args.file, args.caption) else 1)
    else:
        sys.exit(0 if send(args.chat, args.text) else 1)
