"""Discovery: снять РЕАЛЬНУЮ структуру DOM запущенного BiP Desktop (Electron) по CDP.

Печатает ТОЛЬКО структуру (классы, атрибуты, счётчики, ДЛИНЫ текста) — без содержимого
сообщений, имён и номеров, чтобы ПДн рабочих чатов не покидали приложение (RISK-019).

Предусловие: BiP запущен с --remote-debugging-port=9222 --remote-allow-origins=*
Запуск:  python bip_discover.py
"""
import sys

from bip_cdp import BipPage

JS = r"""
(() => {
  const rows = [...document.querySelectorAll('[itemtype="ChatListRow"]')];
  const freq = {};
  for (const el of document.querySelectorAll('div,li,span,a')) {
    const cls = (typeof el.className === 'string') ? el.className.trim() : '';
    if (cls && cls.length < 160) freq[cls] = (freq[cls] || 0) + 1;
  }
  return {
    title: document.title,
    rows: rows.length,
    itemtypes: [...new Set([...document.querySelectorAll('[itemtype]')]
                  .map(e => e.getAttribute('itemtype')))],
    classes: Object.entries(freq).sort((a, b) => b[1] - a[1]).slice(0, 30)
               .map(([c, n]) => n + '\t' + c),
    // по строке чата — только метрики, без текста
    sample: rows.map(r => ({
      jid_shape: (r.getAttribute('data-jid') || '').replace(/[0-9]/g, '#').slice(0, 40),
      name_len: (r.querySelector("[class*='header__name']") || {}).innerText?.trim().length || 0,
      preview_len: (r.querySelector("[class*='message__text']") || {}).innerText?.trim().length || 0,
      outgoing: !!r.querySelector("[class*='message__icon'] svg"),
      unread: (r.querySelector("[class*='extra__unread']") || {}).innerText?.trim() || null,
    })),
  };
})()
"""


def main():
    sys.stdout.reconfigure(encoding="utf-8")
    with BipPage() as page:
        print("URL:", page.url)
        d = page.evaluate(JS)
    print(f"title: {d['title']} | строк чатов: {d['rows']}")
    print("\n=== itemtype ===");  print(", ".join(d["itemtypes"]) or "(нет)")
    print("\n=== ЧАСТЫЕ КЛАССЫ (count<TAB>class) ===")
    print("\n".join(d["classes"]))
    print("\n=== СТРОКИ ЧАТОВ (только метрики) ===")
    for i, r in enumerate(d["sample"]):
        print(i, r)
    print("\n(Только структура и длины. Текст, имена и номера не выводятся — ПДн остаются в приложении.)")


if __name__ == "__main__":
    main()
