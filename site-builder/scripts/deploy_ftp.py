"""Выкладка статической сборки на виртуальный хостинг по FTP.

Обобщённый вариант скрипта, написанного 16.09.2026 при выкладке tech-77.ru.
Адреса и порядок — из docs/HOSTING_NIC_RU.md: FTP `ftp.<услуга>.nichost.ru`,
пользователь `<услуга>_ftp`, корень сайта `/<домен>/docs`, пароль лежит в
диспетчере учётных данных (keyring, служба по умолчанию `nic-ru-ftp`).

Пароль читает сам скрипт и никуда не печатает — ни в вывод, ни в журнал, ни в
аргументы команды.

    python deploy_ftp.py --service koo7873294 --domain tech-77.ru --dir out --check
    python deploy_ftp.py --service koo7873294 --domain tech-77.ru --dir out

ВАЖНО: запускать в обычном терминале человека. Из сессии агента Claude Code
команды идут через прокси песочницы, который пропускает только HTTP и HTTPS,
и любое прямое соединение падает с `PermissionError [WinError 10013]`.

Скрипт ничего не удаляет на сервере: файлы прошлых сборок остаются лежать.
"""

from __future__ import annotations

import argparse
import ftplib
import pathlib
import re
import sys
import urllib.request


def connect(host: str, user: str, service: str) -> tuple[ftplib.FTP, str]:
    import keyring

    password = keyring.get_password(service, user)
    if not password:
        sys.exit(f"нет пароля в хранилище: служба {service}, пользователь {user}")
    try:
        ftp: ftplib.FTP = ftplib.FTP_TLS(host, timeout=30)
        ftp.login(user, password)
        ftp.prot_p()  # type: ignore[attr-defined]
        mode = "FTPS"
    except Exception:
        ftp = ftplib.FTP(host, timeout=30)
        ftp.login(user, password)
        mode = "FTP без TLS"
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


def upload(ftp: ftplib.FTP, local: pathlib.Path, remote_root: str, reconnect) -> tuple[ftplib.FTP, int, int]:
    """Обрыв сессии на общем хостинге — обычное дело, поэтому файл
    перекладывается заново после переподключения, а не роняет всю выкладку."""
    files = sorted(p for p in local.rglob("*") if p.is_file())
    known_dirs: set[str] = set()
    sent = 0
    total = 0
    for index, path in enumerate(files, start=1):
        rel = path.relative_to(local).as_posix()
        remote = f"{remote_root}/{rel}"
        for attempt in (1, 2, 3):
            try:
                ensure_dir(ftp, remote.rsplit("/", 1)[0], known_dirs)
                with path.open("rb") as handle:
                    ftp.storbinary(f"STOR {remote}", handle)
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
        sent += 1
        total += path.stat().st_size
        if index % 50 == 0:
            print(f"  залито {index} из {len(files)}", flush=True)
    return ftp, sent, total


BUILD_ID = re.compile(r'"b":"([A-Za-z0-9_-]{15,30})"')


def fetch(url: str) -> tuple[int, str]:
    request = urllib.request.Request(url, headers={"User-Agent": "site-builder-deploy"})
    with urllib.request.urlopen(request, timeout=30) as response:
        return response.status, response.read().decode("utf-8", "replace")


def verify(site_url: str, local: pathlib.Path, pages: list[str]) -> int:
    """«Команда отработала без ошибки» и «сайт обновился» — разные события.
    Сверяем идентификатор сборки в живой выдаче с тем, что лежит в out/."""
    index = local / "index.html"
    match = BUILD_ID.search(index.read_text(encoding="utf-8")) if index.exists() else None
    build_id = match.group(1) if match else None
    print(f"сборка: buildId={build_id}")
    problems = 0
    for page in pages:
        try:
            status, body = fetch(site_url + page)
        except Exception as exc:  # noqa: BLE001
            print(f"  {page}: недоступно — {exc}")
            problems += 1
            continue
        live = BUILD_ID.search(body)
        same = bool(build_id) and bool(live) and live.group(1) == build_id
        robots = re.search(r'<meta name="robots" content="([^"]+)"', body)
        print(
            f"  {page}: HTTP {status}, buildId="
            f"{'совпал' if same else (live.group(1) if live else 'нет')}"
            f", robots={robots.group(1) if robots else 'нет'}"
        )
        if status != 200 or (build_id and not same):
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
    parser.add_argument("--page", action="append", default=None, help="страница для сверки, можно несколько")
    args = parser.parse_args()

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
        print(f"подключено: {host} ({mode}), пользователь {user}")
        ftp.cwd(remote_root)
        print(f"каталог сайта {remote_root}: {len(ftp.nlst())} записей")
        if args.check:
            return 0
        count = sum(1 for p in local.rglob("*") if p.is_file())
        print(f"заливаю {count} файлов из {local}")
        ftp, sent, size = upload(ftp, local, remote_root, reconnect)
        print(f"залито файлов: {sent}, объём: {size / 1024 / 1024:.1f} МБ")
    finally:
        try:
            ftp.quit()
        except Exception:  # noqa: BLE001
            ftp.close()

    print("сверка живого сайта:")
    return 1 if verify(site_url, local, pages) else 0


if __name__ == "__main__":
    sys.exit(main())
