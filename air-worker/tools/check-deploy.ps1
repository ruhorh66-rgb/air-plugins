#requires -Version 5.1
<#
    Проверка развёртывания: ОТВЕЧАЕТ ЛИ УСТАНОВЛЕННОЕ, А НЕ ЛЕЖИТ ЛИ НА МЕСТЕ.

    Заведено по прямому запросу ЛПР 13.09.2026: нужна сборка, разворачиваемая на второй
    машине без повышения прав. Требование к самой проверке сформулировала AIR-ENV-002 и
    оно здесь главное: «не файлы на месте, а прогон, отвечающий кодом. Наличие каталога мы
    сегодня уже принимали за установленный пакет — восемь раз подряд».

    Поэтому каждая проверка ниже что-то ЗАПУСКАЕТ и читает код возврата либо ответ. Ни
    одна не довольствуется Test-Path. Единственное исключение — рантайм: там проверяется
    право ЗАПИСИ, и проверяется оно записью, а не предположением.

    Три кода, как у судьи: 0 развернулось, 1 развернулось не полностью, 2 проверять нечем.

    СЕКРЕТЫ НЕ ПЕЧАТАЮТСЯ. Про ключ подписи говорится только «есть» или «нет» — значение
    не выводится, не логируется и не сравнивается по содержимому. Это контракт продукта.
#>
[CmdletBinding()]
param([string]$ProductRoot = '')
if (-not $ProductRoot) { $ProductRoot = Split-Path -Parent $PSScriptRoot }

$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$fail = @(); $unknown = @(); $ok = @()

function Get-Python {
    foreach ($c in @($env:AIR_PYTHON, 'python', 'python3', 'py')) {
        if (-not $c) { continue }
        $r = Get-Command $c -ErrorAction SilentlyContinue
        if (-not $r) { continue }
        # Резолв не доказывает наличия, доказывает только ОТВЕТ: в WindowsApps лежат
        # алиасы-заглушки магазина, которые находятся и не исполняются.
        $v = & $c -c "import sys;print(sys.version.split()[0])" 2>$null
        if ($LASTEXITCODE -eq 0 -and $v) { return [pscustomobject]@{ Exe = $r.Source; Version = ([string]$v).Trim() } }
    }
    return $null
}

# --- 1. интерпретатор отвечает ----------------------------------------------
$py = Get-Python
if (-not $py) {
    $unknown += 'Python не отвечает: ни AIR_PYTHON, ни python/python3/py. Без него исполнитель не работает.'
} else {
    $ok += "Python отвечает: $($py.Version) ($($py.Exe))"
}

# --- 2. бинарник отвечает ----------------------------------------------------
$exe = Join-Path $ProductRoot 'bin\air-worker.exe'
if (-not (Test-Path -LiteralPath $exe)) {
    $fail += "нет $exe — механизм не развёрнут"
} else {
    $v = & $exe version 2>&1
    if ($LASTEXITCODE -ne 0) { $fail += "бинарник не отвечает на version (код $LASTEXITCODE)" }
    else { $ok += "бинарник отвечает: $v" }
}

# --- 3. судья ПРОГОНЯЕТСЯ ----------------------------------------------------
# Проверяется не вердикт, а то, что судья ВЫНОСИТ вердикт: 0, 1 и 2 здесь одинаково
# хороши, потому что все три означают «механизм работает». Плохо только отсутствие ответа.
if (Test-Path -LiteralPath $exe) {
    $out = (& $exe judge -product $ProductRoot 2>&1 | Out-String).Trim()
    $code = $LASTEXITCODE
    if ($code -notin @(0, 1, 2)) {
        $fail += "судья вернул неожиданный код $code — это не вердикт, а поломка"
    } else {
        $ok += "судья выносит вердикт (код $code): $out"
    }
}

# --- 4. самотест продукта ----------------------------------------------------
$selftest = Join-Path $ProductRoot 'skills\run-worker-task\scripts\selftest.py'
if (-not $py) {
    $unknown += 'самотест не запускался: нет интерпретатора'
} elseif (-not (Test-Path -LiteralPath $selftest)) {
    $unknown += "самотест не запускался: нет $selftest"
} else {
    $so = (& $py.Exe $selftest 2>&1 | Out-String).Trim()
    $sc = $LASTEXITCODE
    if ($sc -eq 0) {
        $ok += 'самотест продукта прошёл'
    } else {
        $tail = @($so -split "`r?`n" | Where-Object { $_.Trim() }) | Select-Object -Last 1
        $fail += "самотест не прошёл (код $sc)$(if ($tail) { " — $tail" })"
    }
}

# --- 5. рантайм: разрешается И ПИШЕТСЯ ---------------------------------------
# Право записи проверяется записью. Каталог, существующий и недоступный на запись,
# выглядит так же, как рабочий, — и это тот самый класс, который мы весь день ловим.
if ($py) {
    $resolver = Join-Path $ProductRoot 'skills\run-worker-task\scripts'
    $root = (& $py.Exe -c "import sys;sys.path.insert(0,r'$resolver');import runtime_paths;print(runtime_paths.resolve_runtime_root())" 2>&1 | Out-String).Trim()
    if ($LASTEXITCODE -ne 0 -or -not $root) {
        $unknown += 'рантайм не разрешается: runtime_paths не ответил'
    } else {
        $probe = Join-Path $root ('.deploy-probe-' + [guid]::NewGuid().ToString('N').Substring(0, 6))
        try {
            New-Item -ItemType Directory -Path $root -Force -ErrorAction Stop | Out-Null
            Set-Content -LiteralPath $probe -Value 'x' -ErrorAction Stop
            Remove-Item -LiteralPath $probe -Force -ErrorAction SilentlyContinue
            $ok += "рантайм пишется: $root"
            # Профиль пользователя в периметр бэкапа контура не входит. Это не отказ —
            # решение о томе принимает человек, — но молчать об этом нельзя.
            if ($env:LOCALAPPDATA -and $root.StartsWith($env:LOCALAPPDATA, 'OrdinalIgnoreCase')) {
                $ok += 'ВНИМАНИЕ: рантайм лежит в профиле пользователя. Если профиль вне периметра бэкапа, объяви AIR_WORKER_RUNTIME на защищённом томе.'
            }
        } catch {
            $fail += "рантайм не пишется ($root): $($_.Exception.Message)"
        }
    }
}

# --- 6. ключ подписи: есть или нет, БЕЗ ЗНАЧЕНИЯ -----------------------------
$keyFound = $false
$keyWhere = ''
if ($env:AIR_WORKER_HMAC_KEY) { $keyFound = $true; $keyWhere = 'переменная окружения' }
elseif ($py) {
    # ИМЕНА УЧЁТНОЙ ЗАПИСИ СПРАШИВАЮТСЯ У КОДА ПРОДУКТА, А НЕ ДУБЛИРУЮТСЯ ЗДЕСЬ.
    # Первая редакция этой проверки угадывала их ('air-worker'/'hmac') и дала ЛОЖНЫЙ
    # ОТКАЗ: на самом деле SERVICE='air-comms-telegram-bot', HMAC_ACCOUNT='request-hmac-key'.
    # Проверка, дублирующая константу, расходится с ней молча — тот же класс, что два
    # источника состояния, только в миниатюре.
    $srcDir = Join-Path $ProductRoot 'skills\run-worker-task\scripts'
    $probe = & $py.Exe -c "import sys;sys.path.insert(0,r'$srcDir');import worker,keyring;print('yes' if keyring.get_password(worker.SERVICE, worker.HMAC_ACCOUNT) else 'no')" 2>$null
    if ($LASTEXITCODE -eq 0 -and ([string]$probe).Trim() -eq 'yes') { $keyFound = $true; $keyWhere = 'keyring' }
}
if ($keyFound) {
    $ok += "ключ подписи заведён ($keyWhere) — значение не читается и не печатается"
} else {
    $fail += 'ключа подписи нет ни в keyring, ни в переменной окружения: заявки подписывать нечем. Ключ заводится НА ЭТОЙ машине, а не переносится с другой.'
}

# --- вердикт -----------------------------------------------------------------
if ($ok.Count -eq 0 -and $fail.Count -eq 0) { Write-Output '[FAIL] нечем проверить: ни одна проверка не собралась'; exit 2 }
if ($unknown.Count) {
    foreach ($u in $unknown) { Write-Output "[FAIL] нечем проверить: $u" }
    foreach ($f in $fail) { Write-Output "[FAIL] $f" }
    exit 2
}
if ($fail.Count) {
    foreach ($o in $ok) { Write-Output "[PASS] $o" }
    foreach ($f in $fail) { Write-Output "[FAIL] $f" }
    exit 1
}
foreach ($o in $ok) { Write-Output "[PASS] $o" }
Write-Output '[PASS] развёрнуто: всё проверенное ОТВЕТИЛО, а не просто лежит на месте'
exit 0
