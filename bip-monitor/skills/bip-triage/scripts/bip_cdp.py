"""Тонкий CDP-клиент для BiP Desktop (Electron 22).

Playwright `connect_over_cdp` к этой сборке НЕ работает: Electron 22 не реализует
`Browser.setDownloadBehavior`, Playwright падает с "Browser context management is not
supported" (проверено 2026-07-21, playwright 1.61.0). Поэтому говорим с CDP напрямую —
нужен только `Runtime.evaluate`, ради него прослойка не нужна.

Зависимость: websocket-client.
"""
import json
import urllib.request

import websocket

CDP_URL = "http://localhost:9222"


class BipPage:
    """Подключение к странице BiP по CDP. Только чтение: единственный метод — evaluate()."""

    def __init__(self, cdp_url: str = CDP_URL, timeout: int = 30):
        self.cdp_url = cdp_url
        self.timeout = timeout
        self._ws = None
        self._id = 0
        self.url = None

    def connect(self) -> "BipPage":
        with urllib.request.urlopen(f"{self.cdp_url}/json", timeout=self.timeout) as r:
            targets = json.load(r)
        pages = [t for t in targets if t.get("type") == "page" and "devtools" not in t.get("url", "")]
        if not pages:
            raise RuntimeError(f"Нет page-таргетов на {self.cdp_url} — BiP запущен с --remote-debugging-port?")
        page = pages[0]
        self.url = page["url"]
        # suppress_origin: Electron отвергает Origin-заголовок, если приложение
        # запущено без --remote-allow-origins=* (CDP 403).
        self._ws = websocket.create_connection(
            page["webSocketDebuggerUrl"], timeout=self.timeout,
            suppress_origin=True, max_size=20 * 1024 * 1024)
        return self

    def send(self, method: str, params: dict = None):
        """Произвольная CDP-команда. Нужна для Input.* — Lexical-редактор не принимает
        присваивание значения, текст в него вводится только настоящими событиями ввода."""
        self._id += 1
        req_id = self._id
        self._ws.send(json.dumps({"id": req_id, "method": method, "params": params or {}}))
        while True:
            msg = json.loads(self._ws.recv())
            if msg.get("id") != req_id:
                continue  # события страницы нам не нужны
            if "error" in msg:
                raise RuntimeError(f"CDP error ({method}): {msg['error']}")
            return msg.get("result", {})

    def evaluate(self, expression: str):
        """Выполнить JS в странице и вернуть значение (returnByValue)."""
        result = self.send("Runtime.evaluate",
                           {"expression": expression, "returnByValue": True, "awaitPromise": True})
        if "exceptionDetails" in result:
            raise RuntimeError(f"JS error: {result['exceptionDetails'].get('text')}")
        return result["result"].get("value")

    def close(self):
        if self._ws:
            try:
                self._ws.close()
            finally:
                self._ws = None

    def __enter__(self):
        return self.connect()

    def __exit__(self, *exc):
        self.close()
