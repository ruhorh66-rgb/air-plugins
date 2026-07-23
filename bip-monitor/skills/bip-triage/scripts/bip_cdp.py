"""Тонкий CDP-клиент для BiP Desktop (Electron 22).

Playwright `connect_over_cdp` к этой сборке НЕ работает: Electron 22 не реализует
`Browser.setDownloadBehavior`, Playwright падает с "Browser context management is not
supported" (проверено 2026-07-21, playwright 1.61.0). Поэтому говорим с CDP напрямую —
нужен только `Runtime.evaluate`, ради него прослойка не нужна.

Зависимость: websocket-client.
"""
import json
import time
import urllib.request

import websocket

CDP_URL = "http://localhost:9222"


class BipPage:
    """Подключение к странице BiP по CDP.

    Базовый метод — evaluate() (чтение). send() открывает произвольные CDP-команды,
    wait_event() — приём событий страницы; вместе они нужны для отправки файла, где
    диалог выбора перехватывается, а не открывается пользователю.
    """

    def __init__(self, cdp_url: str = CDP_URL, timeout: int = 30):
        self.cdp_url = cdp_url
        self.timeout = timeout
        self._ws = None
        self._id = 0
        self._events = []
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
        присваивание значения, текст в него вводится только настоящими событиями ввода.

        События страницы, пришедшие в ожидании ответа, не выбрасываются, а копятся в
        `_events`: иначе перехват file chooser невозможен — событие приходит между
        отправкой клика и ответом на него."""
        self._id += 1
        req_id = self._id
        self._ws.send(json.dumps({"id": req_id, "method": method, "params": params or {}}))
        while True:
            msg = json.loads(self._ws.recv())
            if msg.get("id") != req_id:
                if "method" in msg:
                    self._events.append(msg)
                continue
            if "error" in msg:
                raise RuntimeError(f"CDP error ({method}): {msg['error']}")
            return msg.get("result", {})

    def wait_event(self, method: str, timeout: float = 15.0) -> dict:
        """Дождаться события CDP (например Page.fileChooserOpened).

        Сначала смотрит уже накопленное: событие часто приходит РАНЬШЕ, чем ответ на
        команду, которая его вызвала, и к моменту вызова уже лежит в буфере.
        """
        for i, ev in enumerate(self._events):
            if ev.get("method") == method:
                return self._events.pop(i).get("params", {})

        deadline = time.monotonic() + timeout
        old_timeout = self._ws.gettimeout()
        try:
            while time.monotonic() < deadline:
                self._ws.settimeout(max(0.1, deadline - time.monotonic()))
                try:
                    msg = json.loads(self._ws.recv())
                except websocket.WebSocketTimeoutException:
                    break
                if msg.get("method") == method:
                    return msg.get("params", {})
                if "method" in msg:
                    self._events.append(msg)
        finally:
            self._ws.settimeout(old_timeout)
        raise TimeoutError(f"событие {method} не пришло за {timeout} с")

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
