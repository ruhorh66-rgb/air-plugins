r"""
Unattended nightly vault-fill job for air-vault-local. Runs Phase B (extraction)
-> Phase C (cross-vault check) -> Phase D (Graphify) over every project registered
in nightly_scan_projects.json, then commits each vault's changes. No Claude session
involved at runtime -- pure orchestration of already-built core scripts, driven by
Windows Task Scheduler.

Deliberately does NOT run Phase A (vault_init.ps1) -- this job only fills vaults
that already exist (registered by vault_init.ps1's own Phase 9 hook at creation
time), never creates new ones. Re-running vault_init.ps1's registry-CSV/template
copy on an existing vault nightly would silently overwrite manual edits.

Idempotent by construction: l2_extract_local.py checkpoints per source, so
re-scanning a project's whole raw_source_root every night costs almost nothing
for already-processed files (instant skip) and only does real work on files
added since the last run.

Usage:
    python nightly_vault_fill.py [--project-id ONLY_THIS_ONE]
"""
import argparse
import json
import os
import subprocess
import sys
import time
from datetime import date, datetime
from pathlib import Path

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

QUEUE_DIR = Path(__file__).resolve().parent
# State and site-specific configuration live in the data dir, not beside the code:
# the manifest names real vault paths and raw-source roots, which must not follow this
# script into a public repository. Same split as dispatcher.py and llm_client.py.
DATA_DIR = Path(os.environ.get("LLM_QUEUE_DATA_DIR", QUEUE_DIR))
MANIFEST_PATH = DATA_DIR / "nightly_scan_projects.json"
LOGS_DIR = DATA_DIR / "logs"

# Installation-specific paths come from a config file in the data dir, or from the
# environment. Neither is hard-coded here: a default naming real directories would leak
# the machine's layout into a public repository, and a default that silently points at
# the wrong place is worse than an error.
CONFIG_PATH = DATA_DIR / "nightly_config.json"


def _setting(key: str, env_var: str, required: bool = True) -> str:
    if CONFIG_PATH.exists():
        try:
            value = json.loads(CONFIG_PATH.read_text(encoding="utf-8")).get(key)
            if value:
                return value
        except (OSError, json.JSONDecodeError):
            pass
    value = os.environ.get(env_var)
    if value:
        return value
    if required:
        raise SystemExit(
            f"Не задано: {key}. Укажите его в {CONFIG_PATH} либо в переменной {env_var}."
        )
    return ""


AIR_VAULT_SKILL_SCRIPTS = Path(_setting("air_vault_skill_scripts",
                                        "AIR_VAULT_SKILL_SCRIPTS"))
PYTHON = _setting("python", "LLM_QUEUE_PYTHON")
LLAMA_HEALTH_URL = _setting("llama_health_url", "LLAMA_HEALTH_URL", required=False) \
    or "http://127.0.0.1:8080/health"
LLAMA_START_BAT = _setting("llama_start_bat", "LLAMA_START_BAT", required=False)
PER_FILE_TIMEOUT_S = 1800  # 30min -- generous since this runs unattended overnight, not blocking anyone


def log_lines(log_file, *lines: str) -> None:
    stamped = [f"[{datetime.now().isoformat(timespec='seconds')}] {line}" for line in lines]
    for line in stamped:
        print(line)
    with open(log_file, "a", encoding="utf-8") as f:
        f.write("\n".join(stamped) + "\n")


def ensure_llama_server(log_file) -> bool:
    import requests
    try:
        r = requests.get(LLAMA_HEALTH_URL, timeout=3)
        if r.status_code == 200:
            log_lines(log_file, "llama-server already running.")
            return True
    except requests.RequestException:
        pass

    log_lines(log_file, "llama-server not responding -- starting it...")
    subprocess.Popen(["cmd", "/c", "start", "/min", LLAMA_START_BAT], shell=False)
    for _ in range(24):  # up to 2 minutes
        time.sleep(5)
        try:
            r = requests.get(LLAMA_HEALTH_URL, timeout=3)
            if r.status_code == 200:
                log_lines(log_file, "llama-server is up.")
                return True
        except requests.RequestException:
            continue
    log_lines(log_file, "ERROR: llama-server did not come up within 2 minutes.")
    return False


def run_step(log_file, description: str, cmd: list[str], timeout: int) -> bool:
    log_lines(log_file, f"-- {description}", f"   cmd: {' '.join(cmd)}")
    try:
        result = subprocess.run(cmd, capture_output=True, text=True, encoding="utf-8",
                                 errors="replace", timeout=timeout)
        tail = "\n".join(result.stdout.strip().splitlines()[-15:]) if result.stdout.strip() else ""
        if tail:
            log_lines(log_file, "   output (tail):", *[f"     {l}" for l in tail.splitlines()])
        if result.returncode != 0:
            log_lines(log_file, f"   FAILED (exit {result.returncode}): {result.stderr.strip()[-500:]}")
            return False
        return True
    except subprocess.TimeoutExpired:
        log_lines(log_file, f"   TIMEOUT (>{timeout}s)")
        return False
    except Exception as e:
        log_lines(log_file, f"   FAILED (exception): {type(e).__name__}: {e}")
        return False


def git_commit_if_dirty(log_file, vault_path: Path, message: str) -> None:
    status = subprocess.run(["git", "status", "--short"], cwd=vault_path,
                             capture_output=True, text=True, encoding="utf-8", errors="replace")
    if not status.stdout.strip():
        log_lines(log_file, "   nothing to commit.")
        return
    subprocess.run(["git", "add", "-A"], cwd=vault_path, capture_output=True)
    commit = subprocess.run(["git", "commit", "-q", "-m", message], cwd=vault_path,
                             capture_output=True, text=True, encoding="utf-8", errors="replace")
    if commit.returncode == 0:
        rev = subprocess.run(["git", "rev-parse", "--short", "HEAD"], cwd=vault_path,
                              capture_output=True, text=True).stdout.strip()
        log_lines(log_file, f"   committed: {rev}")
    else:
        log_lines(log_file, f"   commit FAILED: {commit.stderr.strip()[-300:]}")


def process_project(log_file, project: dict) -> None:
    project_id = project["project_id"]
    vault_path = Path(project["vault_path"])
    raw_root = Path(project["raw_source_root"])
    log_lines(log_file, f"=== Project: {project_id} ===", f"   vault: {vault_path}", f"   raw:   {raw_root}")

    if not vault_path.exists():
        log_lines(log_file, f"   SKIP: vault path does not exist ({vault_path})")
        return
    if not raw_root.exists():
        log_lines(log_file, f"   SKIP: raw source root does not exist ({raw_root})")
        return

    ok = run_step(
        log_file, "Phase B (run_batch.py)",
        [PYTHON, str(AIR_VAULT_SKILL_SCRIPTS / "run_batch.py"),
         "--vault", str(vault_path), "--project-id", project_id,
         "--source-root", str(raw_root), "--timeout", str(PER_FILE_TIMEOUT_S)],
        timeout=PER_FILE_TIMEOUT_S * 20,  # generous overall ceiling for the whole batch
    )
    if not ok:
        log_lines(log_file, "   Phase B had failures -- continuing to Phase C/D on whatever succeeded.")

    run_step(
        log_file, "Phase C (cross_vault_check_local.py)",
        [PYTHON, str(AIR_VAULT_SKILL_SCRIPTS / "cross_vault_check_local.py"),
         "--vault", str(vault_path), "--vault-root", str(vault_path.parent)],
        # Now checkpointed per-batch (2026-07-12) so a timeout here only loses the
        # in-flight batch, not the whole scan -- generous ceiling is safe, this
        # runs unattended overnight with nothing waiting on it. First run against
        # an existing large vault does a one-time full catch-up scan (nothing is
        # marked checked yet); every run after that is incremental and fast.
        timeout=6 * 3600,
    )

    graphify_script = AIR_VAULT_SKILL_SCRIPTS / "graphify.ps1"
    run_step(
        log_file, "Phase D (graphify.ps1)",
        ["powershell.exe", "-NoProfile", "-Command",
         f"& '{graphify_script}' -VaultPath '{vault_path}' -Wiki"],
        # 300s wasn't enough once the vault reached ~2000 cards (confirmed timeout
        # 2026-07-12) -- graphify.ps1 isn't checkpointed (it's a full rebuild each
        # run by design, not incremental), so it just needs a bigger ceiling.
        timeout=1800,
    )

    log_lines(log_file, "-- git commit")
    git_commit_if_dirty(log_file, vault_path, f"nightly_vault_fill: {date.today().isoformat()} auto-run")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--project-id", default=None, help="Process only this project (default: all enabled)")
    args = parser.parse_args()

    LOGS_DIR.mkdir(parents=True, exist_ok=True)
    log_file = LOGS_DIR / f"nightly_{date.today().isoformat()}.log"
    log_lines(log_file, f"=== nightly_vault_fill.py starting ===")

    if not MANIFEST_PATH.exists():
        log_lines(log_file, f"No manifest at {MANIFEST_PATH} -- nothing to do.")
        return

    if not ensure_llama_server(log_file):
        log_lines(log_file, "Aborting run: llama-server unavailable.")
        return

    projects = json.loads(MANIFEST_PATH.read_text(encoding="utf-8"))
    for project in projects:
        if not project.get("enabled", True):
            continue
        if args.project_id and project["project_id"] != args.project_id:
            continue
        process_project(log_file, project)

    log_lines(log_file, "=== nightly_vault_fill.py done ===")


if __name__ == "__main__":
    main()
