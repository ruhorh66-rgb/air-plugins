#requires -Version 5.1
<#
    Проверка: ОБЩИЕ ФУНКЦИИ РАБОТАЮТ НА ТОЙ ОБОЛОЧКЕ, ГДЕ ОНИ ЖИВУТ.

    Правило сформулировала AIR-ENV-002 13.09.2026, и оно куплено в тот же день: резолвер
    инструмента шёл под pwsh 7 с первого раза и падал под Windows PowerShell 5.1 — дважды
    и по разным причинам. Незаданная переменная окружения давала пустую строку в
    [string[]] и рушила привязку параметра; двойные кавычки внутри аргумента калечились
    при передаче нативному процессу, и проба возвращала код 1 НА ИСПРАВНОМ интерпретаторе.

    Второй случай её же словами — самый чистый из сегодняшних: «проверка, написанная
    против обманок, сама стала обманкой». Она врала ровно тем способом, который должна
    была ловить.

    ПОЧЕМУ ИМЕННО 5.1. Задания планировщика контура запускают Windows PowerShell 5.1, и
    хуки зовут его же явно. pwsh 7 — оболочка удобства; проверка на ней доказывает
    удобную оболочку, а не ту, где код будет жить.

    Правило записано КОДОМ, а не уроком, по прямому разбору того же дня: правило, живущее
    уроком, чинится в месте находки и уцелевает в следующем месте. Именно так поиск
    инструмента пережил три починки.
#>
[CmdletBinding()]
param([string]$ProductRoot = '')
if (-not $ProductRoot) { $ProductRoot = Split-Path -Parent $PSScriptRoot }

$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [Text.UTF8Encoding]::new($false) } catch { }

$fail = @(); $unknown = @(); $ok = @()

$lib = Join-Path $ProductRoot 'tools\lib\resolve-tool.ps1'
if (-not (Test-Path -LiteralPath $lib -PathType Leaf)) {
    Write-Output "[FAIL] нечем проверить: нет $lib"
    exit 2
}

# Проба пишется во временный файл, а не передаётся через -Command: аргумент командной
# строки — ровно тот канал, на котором 5.1 калечит кавычки, и проверять совместимость
# каналом, который сам подозреваемый, было бы бессмысленно.
$probe = Join-Path ([IO.Path]::GetTempPath()) ("woody-shell-" + [guid]::NewGuid().ToString('N').Substring(0, 8) + '.ps1')
@"
. '$lib'
`$r = Resolve-ProductPython
Write-Output ('OK=' + `$r.Ok)
Write-Output ('PATH=' + `$r.Path)
Write-Output ('WHY=' + `$r.Reason)
"@ | Set-Content -LiteralPath $probe -Encoding UTF8

function Invoke-Shell([string]$exe, [string]$script) {
    $out = & $exe -NoProfile -ExecutionPolicy Bypass -File $script 2>&1 | Out-String
    return [pscustomobject]@{ Code = $LASTEXITCODE; Text = $out }
}

function Read-Field([string]$text, [string]$name) {
    $m = [regex]::Match($text, "(?m)^$name=(.*)$")
    if ($m.Success) { return $m.Groups[1].Value.Trim() }
    return ''
}

# --- BOM у общих .ps1: первая из двух причин, и она ловится СЧЁТОМ ------------------
# Windows PowerShell 5.1 читает файл без BOM как ANSI: кириллица превращается в мусор,
# разбор ломается, функции не определяются вовсе — и вызывающая проверка падает без
# внятной причины. Это не догадка: проверено изоляцией по одной переменной за раз.
$noBom = @()
foreach ($f in @(Get-ChildItem -LiteralPath (Join-Path $ProductRoot 'tools\lib') -Filter *.ps1 -File -ErrorAction SilentlyContinue)) {
    $b = [IO.File]::ReadAllBytes($f.FullName)
    if ($b.Length -lt 3 -or $b[0] -ne 239 -or $b[1] -ne 187 -or $b[2] -ne 191) { $noBom += $f.Name }
}
if ($noBom.Count) {
    $fail += ('общие библиотеки без BOM: ' + ($noBom -join ', ') + '. Под Windows PowerShell 5.1 их функции не определятся вовсе.')
} else {
    $ok += 'общие библиотеки хранятся UTF-8 с BOM'
}

try {
    # --- Windows PowerShell 5.1: оболочка, где код живёт --------------------------
    $ps51 = Get-Command 'powershell.exe' -ErrorAction SilentlyContinue
    if (-not $ps51) {
        $unknown += 'powershell.exe не резолвится — оболочку, где код живёт, проверить нечем'
    } else {
        $r51 = Invoke-Shell $ps51.Source $probe
        $ok51 = (Read-Field $r51.Text 'OK') -eq 'True'
        $path51 = Read-Field $r51.Text 'PATH'
        if ($r51.Code -ne 0) {
            $fail += "под Windows PowerShell 5.1 проба завершилась кодом $($r51.Code): $(($r51.Text -split "`r?`n" | Where-Object { $_.Trim() } | Select-Object -First 1))"
        } elseif (-not $ok51) {
            $fail += "под Windows PowerShell 5.1 резолвер НЕ нашёл интерпретатор: $(Read-Field $r51.Text 'WHY')"
        } else {
            $ok += "резолвер отвечает под Windows PowerShell 5.1: $path51"
        }
    }

    # --- pwsh 7, если он есть ------------------------------------------------------
    # Его отсутствие отказом НЕ является: 5.1 есть на каждой машине контура, pwsh не на
    # каждой. Но если он есть, ответы обязаны совпасть — расхождение означает, что
    # поведение зависит от оболочки, а это и есть проверяемый дефект.
    $pwsh = Get-Command 'pwsh' -ErrorAction SilentlyContinue
    if (-not $pwsh) {
        $ok += 'pwsh 7 на машине нет — сверка оболочек пропущена законно, 5.1 проверен'
    } else {
        $r7 = Invoke-Shell $pwsh.Source $probe
        $ok7 = (Read-Field $r7.Text 'OK') -eq 'True'
        $path7 = Read-Field $r7.Text 'PATH'
        if (-not $ok7) {
            $fail += "под pwsh 7 резолвер НЕ нашёл интерпретатор: $(Read-Field $r7.Text 'WHY')"
        } elseif ($ps51 -and $ok51 -and $path7 -ne $path51) {
            $fail += "оболочки дают РАЗНЫЙ ответ: 5.1 -> $path51, pwsh 7 -> $path7. Поведение зависит от оболочки."
        } else {
            $ok += 'обе оболочки дают один и тот же ответ'
        }
    }
}
finally {
    Remove-Item -LiteralPath $probe -Force -ErrorAction SilentlyContinue
}

if ($ok.Count -eq 0 -and $fail.Count -eq 0) { Write-Output '[FAIL] нечем проверить: ни одна оболочка не опрошена'; exit 2 }
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
Write-Output '[PASS] общие функции проверены на той оболочке, где живут, а не только на удобной'
exit 0
