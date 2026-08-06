<#
.SYNOPSIS
Проверка на утечку секретов после работы роя.

.DESCRIPTION
ЗАЧЕМ. Рой пишет файлы и коммиты автономно, и никто не смотрит, не утащил ли он
токен, ключ или пароль в рабочее дерево. Инцидент этого класса в контуре уже был —
ACL-экспозиция `openrouter.key` на SRVLM01, 01.08.2026.

ЧЕМ. `gitleaks` — отраслевой сканер, а не собственные регулярки. Свой набор
шаблонов означал бы, что мы гарантируем то, чего не проверяли: правило контура —
брать инструмент из официального источника, а не воспроизводить его по памяти.

ТРИ ИСХОДА, КАК ТРЕБУЕТ МОДУЛЬ КОНТРОЛЯ (AIR_CONTROL.md, правило 3): вердикт без
основания недействителен, поэтому возвращается ровно одно из —

    clean         проверено, находок нет   (рядом: чем и сколько просканировано)
    leaks         найдено, прогон грязный  (рядом: файл, строка, правило)
    unverifiable  ПРОВЕРИТЬ НЕ СМОГ        (рядом: почему)

`unverifiable` — это НЕ «чисто». Отсутствие сканера, отказ запуска или таймаут дают
именно его: молчаливое превращение непроверенного в чистое и есть тот дефект, ради
которого модуль контроля вводили.

ЗНАЧЕНИЕ СЕКРЕТА НЕ ПЕЧАТАЕТСЯ НИКОГДА. `--redact` включён жёстко: находка
называется местом и правилом, но не содержимым. Правило контура `secrets-never-in-chat`
действует и здесь — в отчёте, в логе, в выводе.

.PARAMETER Path
Что сканировать. По умолчанию — текущий каталог.

.PARAMETER ReportPath
Куда положить полный отчёт gitleaks в JSON. Без него отчёт не сохраняется.

.EXAMPLE
  .\scan_secrets.ps1 -Path E:\-8-\air-smeta
  .\scan_secrets.ps1 -Path . -ReportPath E:\-4-\ruflo-hive\leaks.json
#>
[CmdletBinding()]
param(
    [string]$Path = (Get-Location).Path,
    [string]$ReportPath,
    [int]$TimeoutSec = 180
)

$ErrorActionPreference = 'Stop'

function Out-Json($o) { $o | ConvertTo-Json -Depth 6 -Compress }

function Find-Gitleaks {
    $cmd = Get-Command gitleaks -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $roots = @("$env:LOCALAPPDATA\Microsoft\WinGet\Links",
               "$env:LOCALAPPDATA\Microsoft\WinGet\Packages")
    foreach ($r in $roots) {
        if (-not (Test-Path -LiteralPath $r)) { continue }
        $f = Get-ChildItem $r -Recurse -Filter 'gitleaks.exe' -ErrorAction SilentlyContinue |
             Select-Object -First 1
        if ($f) { return $f.FullName }
    }
    return $null
}

if (-not (Test-Path -LiteralPath $Path)) {
    Out-Json ([pscustomobject]@{ outcome='unverifiable'; reason="каталог не существует: $Path" })
    exit 3
}
$Path = (Resolve-Path -LiteralPath $Path).Path

$exe = Find-Gitleaks
if (-not $exe) {
    # Правило 3 модуля контроля: нет инструмента — значит НЕ ПРОВЕРЕНО, а не чисто.
    Out-Json ([pscustomobject]@{
        outcome='unverifiable'
        reason='gitleaks не найден — проверка не выполнялась'
        advice='winget install --id gitleaks.gitleaks --scope user'
        path=$Path
    })
    exit 3
}

$report = if ($ReportPath) { $ReportPath } else {
    Join-Path ([System.IO.Path]::GetTempPath()) "gitleaks-$PID.json"
}
$reportDir = Split-Path -Parent $report
if ($reportDir -and -not (Test-Path -LiteralPath $reportDir)) {
    New-Item -ItemType Directory -Path $reportDir -Force | Out-Null
}

# --redact: значение секрета не попадает ни в отчёт, ни в вывод.
# --exit-code 0: код возврата gitleaks не решает за нас — вердикт выносится
# по содержимому отчёта, чтобы «упал сканер» и «нашлась утечка» не сливались.
$args = @('dir', $Path, '--no-banner', '--redact', '--exit-code', '0',
          '--report-format', 'json', '--report-path', $report)

# Если в сканируемом каталоге лежит свой .gitleaks.toml — подать его явно.
# Без --config gitleaks не гарантированно подхватывает конфиг из чужого cwd
# (мы не меняем cwd под $Path). Найдено 06.08.2026: вендоренная документация
# сторонних API (пример-значения токенов в OpenAPI-спецификациях) даёт
# стабильные ложные срабатывания — конфиг с allowlist по пути решает это
# без ослабления проверки остального дерева.
$localConfig = Join-Path $Path '.gitleaks.toml'
if (Test-Path -LiteralPath $localConfig) {
    $args += @('--config', $localConfig)
}

$started = Get-Date
try {
    $proc = Start-Process -FilePath $exe -ArgumentList $args -NoNewWindow -PassThru `
                          -RedirectStandardError "$report.err"
    if (-not $proc.WaitForExit($TimeoutSec * 1000)) {
        try { $proc.Kill() } catch { }
        Out-Json ([pscustomobject]@{
            outcome='unverifiable'; reason="сканер не уложился в $TimeoutSec с"; path=$Path })
        exit 3
    }
} catch {
    Out-Json ([pscustomobject]@{
        outcome='unverifiable'; reason="сканер не запустился: $($_.Exception.Message)"; path=$Path })
    exit 3
}

if (-not (Test-Path -LiteralPath $report)) {
    Out-Json ([pscustomobject]@{
        outcome='unverifiable'; reason='сканер отработал, но отчёта не оставил'; path=$Path })
    exit 3
}

try { $findings = @(Get-Content -LiteralPath $report -Raw | ConvertFrom-Json) }
catch { $findings = @() }
$findings = @($findings | Where-Object { $_ })

$elapsed = [math]::Round(((Get-Date) - $started).TotalSeconds, 1)

if ($findings.Count -eq 0) {
    Out-Json ([pscustomobject]@{
        outcome='clean'; path=$Path; findings=0; seconds=$elapsed
        verifiedBy="gitleaks $((& $exe version) 2>$null)"; report=$report
    })
    exit 0
}

# Находки: место и правило, БЕЗ значения.
$brief = $findings | Select-Object -First 20 | ForEach-Object {
    [pscustomobject]@{ file=$_.File; line=$_.StartLine; rule=$_.RuleID; commit=$_.Commit }
}
Out-Json ([pscustomobject]@{
    outcome='leaks'; path=$Path; findings=$findings.Count; seconds=$elapsed
    shown=$brief; report=$report
    note='значения секретов не выводятся (--redact); смотреть по файлу и строке'
})
exit 1
