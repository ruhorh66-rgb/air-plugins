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

JS_SEARCH_FOCUS = """
(() => {
  const i = [...document.querySelectorAll('input')]
    .find(e => (e.getAttribute('placeholder') || '').includes('Поиск'));
  if (!i) return false;
  i.focus();
  return true;
})()
"""

JS_SEARCH_PICK = """
(() => {
  const c = [...document.querySelectorAll('[class*="_contact_"]')]
    .find(e => (e.innerText || '').toLowerCase().includes(%s)
            && e.getBoundingClientRect().width > 0);
  if (!c) return null;
  c.click();
  return {chat: (c.innerText || '').trim().split('\\n')[0]};
})()
"""


def open_chat(page, needle: str):
    """Открыть чат по части имени. Возвращает {'chat': ...} либо None.

    Список чатов ВИРТУАЛИЗОВАН: в DOM лежит только видимая часть (порядка 18 строк),
    поэтому поиск по отрисованным ChatListRow находит не все чаты — отсутствие строки
    в выборке не означает отсутствия чата (`ERR-2026-000071`). Если прямого совпадения
    нет, добираем через штатный поиск BiP «Поиск контактов или групп»: результаты
    рендерятся классом `_contact_`, а не ChatListRow, и кликом по ним чат открывается.
    """
    needle = needle.lower()
    target = page.evaluate(JS_OPEN % json.dumps(needle, ensure_ascii=False))
    if target:
        return target

    if not page.evaluate(JS_SEARCH_FOCUS):
        return None
    page.send("Input.insertText", {"text": needle})
    time.sleep(2.5)
    return page.evaluate(JS_SEARCH_PICK % json.dumps(needle, ensure_ascii=False))


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
        target = open_chat(page, needle)
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

    Порядок (проверен 2026-07-23, работает):

      1) `.p-speeddial-button` — открыть меню вложений;
      2) `.p-speeddial-action` с aria-label «Документ» — ПЕРЕКЛЮЧАЕТ accept уже
         существующего `input[type=file]` с `image/*,video/*` на `document`.
         Отдельного input для документов нет, элемент переиспользуется;
      3) `DOM.setFileInputFiles` по nodeId — прикрепляет файл;
      4) открывается окно предпросмотра;
      5) отправка — клик по `a.send-media-button` внутри `div.send-media`
         (aria-label «Send Media», правый нижний угол).

    ⚠️ **Как НЕ проверять, что файл прикрепился** (ERR-2026-000068). Два индикатора
    выглядят очевидными и оба лгут:

      * `input.files.length` — остаётся 0. BiP переносит файл в собственное состояние
        и чистит input, поэтому ноль здесь не значит ничего;
      * поиск окна предпросмотра по `[role=dialog]`, `[class*=modal]`,
        `[class*=preview]` — BiP использует другие классы, и «не нашёл» читается как
        «не прикрепилось».

    На этих двух индикаторах функция была ошибочно объявлена неработающей, а
    возможность — закрытой. Проверять надо появление `div.send-media`: он существует
    только когда есть что отправлять.

    `Page.setInterceptFileChooserDialog` при этом действительно не шлёт события
    `fileChooserOpened` — но он и не нужен: клик по «Документ» диалога не открывает.
    """
    file_path = Path(path).resolve()
    if not file_path.is_file():
        print(f"СТОП: файла нет — {file_path}", file=sys.stderr)
        return False
    size = file_path.stat().st_size
    print(f"файл: {file_path.name} ({size:,} байт)")

    with BipPage() as page:
        target = open_chat(page, needle)
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
            time.sleep(3)

            # Проверка ДО точки невозврата. Смотрим на появление кнопки отправки медиа:
            # она существует только когда приложение приняло вложение. input.files и
            # поиск «модалки» по типовым классам здесь врут — см. docstring.
            ready = page.evaluate(
                '(() => !!document.querySelector("div.send-media a.send-media-button"))()')
            if not ready:
                print("СТОП: вложение не принято — кнопка отправки не появилась",
                      file=sys.stderr)
                return False
            print("файл прикреплён, предпросмотр открыт")
        finally:
            # вернуть штатное поведение, иначе следующий выбор файла в BiP руками зависнет
            page.send("Page.setInterceptFileChooserDialog", {"enabled": False})

        if caption:
            page.send("Input.insertText", {"text": caption})
            time.sleep(1)

        # точка невозврата: у вложения своя кнопка, Enter здесь не отправляет
        page.evaluate(
            '(() => { const b=document.querySelector("div.send-media a.send-media-button");'
            ' if(!b) return false; b.click(); return true; })()')
        time.sleep(6)

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


JS_FIND_BUBBLE = """
(() => {
  const b = [...document.querySelectorAll('[itemtype="DocumentBubble"],[itemtype="TextBubble"]')]
    .filter(e => (e.innerText || '').includes(%s) &&
                 e.querySelector("[class*='message_status__icon']"))   // только исходящие
    .pop();
  if (!b) return null;
  const r = b.getBoundingClientRect();
  return {txt: (b.innerText || '').trim().slice(0, 60),
          cx: Math.round(r.x + r.width / 2), cy: Math.round(r.y + r.height / 2),
          dotsX: Math.round(r.right - 14), dotsY: Math.round(r.top + 12)};
})()
"""

JS_MENU_ITEMS = """
(() => [...document.querySelectorAll('[class*="popperContainer__menu__item"],[role=menuitem]')]
  .filter(e => e.getBoundingClientRect().width > 0)
  .map(e => (e.innerText || '').trim()).filter(Boolean))()
"""


def delete_message(chat: str, needle: str, for_everyone: bool = True) -> bool:
    """Удалить СВОЁ отправленное сообщение: последний ИСХОДЯЩИЙ пузырь с `needle`.

    Необратимо, и у собеседника тоже.

    Путь, установленный 2026-07-23:
      1) навести мышь на пузырь — элементы управления появляются только при hover;
      2) кликнуть «три точки» в правом верхнем углу пузыря (координата, не селектор:
         поиск по классам даёт reactionEmoji, а не меню);
      3) в меню выбрать «Удалить» — рядом там же «Информация о сообщении»,
         «Переслать», «Ответить», «Скачать», «Pin»;
      4) в подтверждении выбрать «Удалить от всех» либо «Удалить у меня».
         Третья кнопка — «Сдавайся», это машинный перевод «Отмена», не нажимать.

    Диалог после удаления может остаться на экране — снимается Escape; факт удаления
    проверяется исчезновением пузыря, а не закрытием окна.
    """
    with BipPage() as page:
        opened = open_chat(page, chat)
        if not opened:
            print(f"СТОП: чат по подстроке {chat!r} не найден", file=sys.stderr)
            return False
        print(f"чат: {opened['chat']}")
        time.sleep(3)

        target = page.evaluate(JS_FIND_BUBBLE % json.dumps(needle, ensure_ascii=False))
        if not target:
            print(f"СТОП: исходящее сообщение с {needle!r} не найдено", file=sys.stderr)
            return False
        print(f"цель: {target['txt']}")

        page.send("Input.dispatchMouseEvent",
                  {"type": "mouseMoved", "x": target["cx"], "y": target["cy"]})
        time.sleep(1)
        page.send("Input.dispatchMouseEvent",
                  {"type": "mouseMoved", "x": target["dotsX"], "y": target["dotsY"]})
        time.sleep(0.5)
        for ev in ("mousePressed", "mouseReleased"):
            page.send("Input.dispatchMouseEvent",
                      {"type": ev, "x": target["dotsX"], "y": target["dotsY"],
                       "button": "left", "clickCount": 1})
        time.sleep(1.5)

        items = page.evaluate(JS_MENU_ITEMS)
        if not any("удал" in i.lower() for i in items):
            print(f"СТОП: меню не открылось (пункты: {items})", file=sys.stderr)
            return False

        page.evaluate("""(() => {
          const el = [...document.querySelectorAll('[class*="popperContainer__menu__item"],[role=menuitem]')]
            .find(e => (e.innerText || '').trim().toLowerCase().includes('удал'));
          if (el) el.click();
          return !!el;
        })()""")
        time.sleep(2)

        wanted = "от всех" if for_everyone else "у меня"
        clicked = page.evaluate("""(() => {
          const b = [...document.querySelectorAll('button,[role=button],a')]
            .filter(e => e.getBoundingClientRect().width > 0)
            .find(e => (e.innerText || '').trim().toLowerCase().includes(%s));
          if (b) b.click();
          return !!b;
        })()""" % json.dumps(wanted, ensure_ascii=False))
        if not clicked:
            print(f"СТОП: кнопка «{wanted}» не найдена", file=sys.stderr)
            return False
        time.sleep(4)

        left = page.evaluate(JS_FIND_BUBBLE % json.dumps(needle, ensure_ascii=False))
        for _ in range(2):                    # диалог иногда остаётся на экране
            for t in ("keyDown", "keyUp"):
                page.send("Input.dispatchKeyEvent", {"type": t, "key": "Escape",
                                                     "code": "Escape",
                                                     "windowsVirtualKeyCode": 27})
            time.sleep(0.5)

        print("УДАЛЕНО" if left is None else "НЕ ПОДТВЕРЖДЕНО: пузырь на месте")
        return left is None


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
    ap.add_argument("--delete", metavar="ФРАГМЕНТ",
                    help="удалить СВОЁ сообщение по фрагменту текста или имени файла")
    ap.add_argument("--only-me", action="store_true",
                    help="с --delete: удалить только у себя, не у собеседника")
    ap.add_argument("--yes", action="store_true", help="подтверждение: действие необратимо")
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()

    if args.selftest:
        selftest()
    elif args.text and args.file:
        ap.error("--text и --file вместе не отправляются: для файла есть --caption")
    elif not (args.chat and (args.text or args.file or args.delete)):
        ap.error("нужны --chat и (--text, --file либо --delete)")
    elif not args.yes:
        ap.error("необратимое действие: добавьте --yes")
    elif args.delete:
        sys.exit(0 if delete_message(args.chat, args.delete, not args.only_me) else 1)
    elif args.file:
        sys.exit(0 if send_file(args.chat, args.file, args.caption) else 1)
    else:
        sys.exit(0 if send(args.chat, args.text) else 1)
