"""Выкладка статической сборки на виртуальный хостинг по FTP.

Обобщённый вариант скрипта, которым 16.09.2026 выкладывался tech-77.ru. Адреса и
порядок — из docs/HOSTING_NIC_RU.md: FTP `ftp.<услуга>.nichost.ru`, пользователь
`<услуга>_ftp`, корень сайта `/<домен>/docs`, пароль в диспетчере учётных данных
(keyring, служба по умолчанию `nic-ru-ftp`). Пароль читает сам скрипт и никуда не
печатает — ни в вывод, ни в журнал, ни в аргументы команды.

    python deploy_ftp.py --service koo7873294 --domain tech-77.ru --dir out --check
    python deploy_ftp.py --service koo7873294 --domain tech-77.ru --dir out --page / --page /contacts/

Работает и из сессии агента, и из обычного терминала. В сессии прямой сокет
наружу не открывается: всё идёт через прокси песочницы, и `socket.connect`
получает `PermissionError [WinError 10013]` на любом порту, включая 443. Тот же
прокси открывает туннель методом CONNECT, поэтому при заданном в окружении
прокси скрипт работает через туннель — и управляющее соединение, и каждое
пассивное соединение данных.

Ничего на сервере не удаляется: файлы прошлых сборок остаются лежать.
"""

from __future__ import annotations

import argparse
import base64
import ftplib
import hashlib
import ipaddress
import json
import os
import pathlib
import re
import socket
import sys
import urllib.error
import urllib.parse
import urllib.request


def proxy_settings() -> tuple[str, int, str | None] | None:
    """Адрес прокси из окружения и, если он там задан, заголовок авторизации."""
    raw = (
        os.environ.get("HTTPS_PROXY")
        or os.environ.get("https_proxy")
        or os.environ.get("HTTP_PROXY")
        or os.environ.get("http_proxy")
    )
    if not raw:
        return None
    parsed = urllib.parse.urlsplit(raw if "://" in raw else f"http://{raw}")
    if not parsed.hostname:
        return None
    header = None
    if parsed.username:
        token = f"{parsed.username}:{parsed.password or chr(0)}".replace(chr(0), "").encode()
        header = "Basic " + base64.b64encode(token).decode()
    return parsed.hostname, parsed.port or 8080, header


def open_tunnel(proxy: tuple[str, int, str | None], host: str, port: int, timeout: float) -> socket.socket:
    sock = socket.create_connection((proxy[0], proxy[1]), timeout)
    lines = [f"CONNECT {host}:{port} HTTP/1.1", f"Host: {host}:{port}"]
    if proxy[2]:
        lines.append(f"Proxy-Authorization: {proxy[2]}")
    sock.sendall(("\r\n".join(lines) + "\r\n\r\n").encode())
    answer = b""
    while b"\r\n\r\n" not in answer:
        chunk = sock.recv(4096)
        if not chunk:
            raise OSError("прокси закрыл соединение, не ответив")
        answer += chunk
    first = answer.split(b"\r\n", 1)[0].decode("latin-1")
    if " 200 " not in first:
        raise OSError(f"прокси не открыл туннель: {first}")
    return sock


class TunnelFTP(ftplib.FTP):
    """FTP поверх туннеля CONNECT.

    Пассивный режим сообщает адрес соединения данных отдельной командой, и
    туннель для него открывается свой. Штатная ftplib подставляет сюда адрес
    того, с кем соединена управляющая сессия, — через прокси это был бы сам
    прокси, поэтому адрес берётся из ответа сервера. Частный адрес снаружи
    бесполезен: тогда берётся исходное имя хоста.
    """

    trust_server_pasv_ipv4_address = True

    def __init__(self, host: str, proxy: tuple[str, int, str | None], timeout: float = 60) -> None:
        self.proxy = proxy
        self.real_host = host
        super().__init__(host=host, timeout=timeout)

    def connect(self, host: str = "", port: int = 0, timeout: float = -999, source_address=None):  # noqa: ARG002
        if host:
            self.host = host
        if port:
            self.port = port
        if timeout != -999:
            self.timeout = timeout
        self.sock = open_tunnel(self.proxy, self.host, self.port, self.timeout)
        self.af = self.sock.family
        self.file = self.sock.makefile("r", encoding=self.encoding)
        self.welcome = self.getresp()
        return self.welcome

    def ntransfercmd(self, cmd: str, rest=None):
        if not self.passiveserver:
            return super().ntransfercmd(cmd, rest)
        host, port = self.makepasv()
        try:
            if ipaddress.ip_address(host).is_private:
                host = self.real_host
        except ValueError:
            host = self.real_host
        conn = open_tunnel(self.proxy, host, port, self.timeout)
        try:
            if rest is not None:
                self.sendcmd(f"REST {rest}")
            resp = self.sendcmd(cmd)
            if resp[0] == "2":
                resp = self.getresp()
            if resp[0] != "1":
                raise ftplib.error_reply(resp)
        except Exception:
            conn.close()
            raise
        size = ftplib.parse150(resp) if resp[:3] == "150" else None
        return conn, size


def connect(host: str, user: str, service: str) -> tuple[ftplib.FTP, str]:
    import keyring

    password = keyring.get_password(service, user)
    if not password:
        sys.exit(f"нет пароля в хранилище: служба {service}, пользователь {user}")
    proxy = proxy_settings()
    try:
        if proxy:
            ftp: ftplib.FTP = TunnelFTP(host, proxy, timeout=60)
            mode = f"через туннель CONNECT на {proxy[0]}:{proxy[1]}"
        else:
            ftp = ftplib.FTP(host, timeout=60)
            mode = "напрямую"
        ftp.login(user, password)
    finally:
        password = None
    ftp.set_pasv(True)
    return ftp, mode


def ensure_dir(ftp: ftplib.FTP, path: str, known: set[str]) -> None:
    if path in known:
        return
    current = ""
    for part in [p for p in path.split("/") if p]:
        current += "/" + part
        if current in known:
            continue
        try:
            ftp.mkd(current)
        except ftplib.error_perm:
            pass  # каталог уже есть — сервера отвечают на это по-разному (550, 521)
        known.add(current)


def digest(path: pathlib.Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def state_path(local: pathlib.Path) -> pathlib.Path:
    return local.parent / ".deploy-state.json"


def load_state(local: pathlib.Path, remote_root: str) -> dict[str, str]:
    """Отпечатки того, что уже залито этой машиной.

    Без них каждая выкладка гонит по FTP всю разметку заново: сверять
    содержимое на сервере нечем — FTP умеет только размер, а размеру верить
    нельзя. Опись — подсказка, а не истина: если её нет или она врёт, худшее,
    что случится, — лишняя заливка, которую поймает сверка живого сайта.
    """
    try:
        data = json.loads(state_path(local).read_text(encoding="utf-8"))
    except Exception:  # noqa: BLE001 — нет файла, битый json, что угодно
        return {}
    return data.get(remote_root, {}) if isinstance(data, dict) else {}


def save_state(local: pathlib.Path, remote_root: str, files: dict[str, str]) -> None:
    text = json.dumps({remote_root: files}, ensure_ascii=False, indent=1)
    state_path(local).write_text(text + chr(10), encoding="utf-8")


def may_skip(rel: str) -> bool:
    """Можно ли доверять совпадению размера.

    Только для файлов из `_next/static`: там имя задаётся содержимым, и файл с
    тем же именем и размером — тот же файл. Для разметки это неверно и уже
    подвело 16.09.2026: в `index.html` поменялось только имя файла стилей, длина
    имени та же, размер совпал — файл не залился, и живая страница осталась
    ссылаться на стили прошлой сборки.
    """
    return rel.startswith("_next/static/")


def upload(ftp: ftplib.FTP, local: pathlib.Path, remote_root: str, reconnect) -> tuple[ftplib.FTP, int, int, int]:
    """Заливает всю сборку.

    Неизменяемый файл сборки того же размера не перезаливается: FTP медленный, а несколько сотен
    файлов не всегда укладываются в одну сессию. Обрыв не роняет выкладку — файл
    перекладывается заново после переподключения.
    """
    files = sorted(p for p in local.rglob("*") if p.is_file())
    known_dirs: set[str] = set()
    known_state = load_state(local, remote_root)
    fresh_state: dict[str, str] = {}
    sent = skipped = total = 0
    for index, path in enumerate(files, start=1):
        rel = path.relative_to(local).as_posix()
        remote = f"{remote_root}/{rel}"
        size = path.stat().st_size
        sha = digest(path)
        for attempt in (1, 2, 3):
            try:
                ensure_dir(ftp, remote.rsplit("/", 1)[0], known_dirs)
                try:
                    same = known_state.get(rel) == sha or (may_skip(rel) and known_state.get(rel) is None)
                    if same and ftp.size(remote) == size:
                        skipped += 1
                        fresh_state[rel] = sha
                        break
                except Exception:  # noqa: BLE001 — файла нет или сервер не умеет SIZE
                    pass
                with path.open("rb") as handle:
                    ftp.storbinary(f"STOR {remote}", handle)
                sent += 1
                total += size
                fresh_state[rel] = sha
                break
            except (ftplib.error_temp, ftplib.error_proto, OSError, EOFError) as exc:
                if attempt == 3:
                    raise
                print(f"  обрыв на {rel}: {exc} — переподключаюсь", flush=True)
                try:
                    ftp.close()
                except Exception:  # noqa: BLE001
                    pass
                ftp, _ = reconnect()
                known_dirs.clear()
        if index % 50 == 0:
            print(f"  обработано {index} из {len(files)}", flush=True)
    save_state(local, remote_root, fresh_state)
    return ftp, sent, skipped, total


ASSET_REF = re.compile(r'/_next/static/[^"\\\s]+?\.(?:js|css)')


def fetch(url: str) -> tuple[int, str]:
    request = urllib.request.Request(url, headers={"User-Agent": "site-builder-deploy"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.status, response.read().decode("utf-8", "replace")


def verify(site_url: str, local: pathlib.Path, pages: list[str]) -> int:
    """«Команда отработала без ошибки» и «сайт обновился» — разные события.

    Сверять по имени сборки нельзя: статический экспорт Next не пишет его в
    разметку. Зато страница перечисляет свои чанки, а имена им даёт содержимое:
    ссылка на файл, которого нет в нашей сборке, означает чужую сборку на
    сервере.
    """
    problems = 0
    for page in pages:
        try:
            status, body = fetch(site_url + page)
        except Exception as exc:  # noqa: BLE001
            print(f"  {page}: недоступно — {exc}")
            problems += 1
            continue
        refs = {ref.rstrip("\\") for ref in ASSET_REF.findall(body)}
        foreign = sorted(ref for ref in refs if not (local / ref.lstrip("/")).exists())
        robots = re.search(r'<meta name="robots" content="([^"]+)"', body)
        print(
            f"  {page}: HTTP {status}, чужих файлов сборки {len(foreign)}"
            f", robots={robots.group(1) if robots else 'нет'}"
        )
        if foreign:
            print(f"      например: {', '.join(foreign[:2])}")
        if status != 200 or foreign:
            problems += 1
    return problems


def main() -> int:
    parser = argparse.ArgumentParser(description="Выкладка статической сборки по FTP")
    parser.add_argument("--service", required=True, help="логин услуги хостинга, например koo7873294")
    parser.add_argument("--domain", required=True, help="домен сайта, например tech-77.ru")
    parser.add_argument("--dir", default="out", help="каталог сборки (по умолчанию out)")
    parser.add_argument("--service-name", default="nic-ru-ftp", help="служба в хранилище паролей")
    parser.add_argument("--check", action="store_true", help="только проверить доступ")
    parser.add_argument("--verify", action="store_true", help="только сверить живой сайт")
    parser.add_argument("--page", action="append", help="страница для сверки, можно несколько")
    args = parser.parse_args()
    sys.stdout.reconfigure(encoding="utf-8")  # консоль Windows по умолчанию cp1252

    local = pathlib.Path(args.dir).resolve()
    site_url = f"https://{args.domain}"
    pages = args.page or ["/"]

    if args.verify:
        return 1 if verify(site_url, local, pages) else 0
    if not local.exists():
        sys.exit(f"нет каталога сборки: {local}")

    host = f"ftp.{args.service}.nichost.ru"
    user = f"{args.service}_ftp"
    remote_root = f"/{args.domain}/docs"

    def reconnect():
        return connect(host, user, args.service_name)

    ftp, mode = reconnect()
    try:
        print(f"подключено: {host} {mode}, пользователь {user}")
        ftp.cwd(remote_root)
        print(f"каталог сайта {remote_root}: {len(ftp.nlst())} записей")
        if args.check:
            return 0
        count = sum(1 for p in local.rglob("*") if p.is_file())
        print(f"заливаю {count} файлов из {local}")
        ftp, sent, skipped, size = upload(ftp, local, remote_root, reconnect)
        print(f"залито {sent}, пропущено как совпавшие {skipped}, объём {size / 1024 / 1024:.1f} МБ")
    finally:
        try:
            ftp.quit()
        except Exception:  # noqa: BLE001
            ftp.close()

    print("сверка живого сайта:")
    return 1 if verify(site_url, local, pages) else 0


if __name__ == "__main__":
    sys.exit(main())
