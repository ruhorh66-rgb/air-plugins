<#
.SYNOPSIS
  Учёт живых субагентов: список ведётся файлом, а не памятью сессии.

.DESCRIPTION
  Вешается сразу на два события — SubagentStart и SubagentStop. Какое из них произошло,
  скрипт узнаёт из поля hook_event_name во входном JSON, а не из отдельного параметра:
  один файл вместо двух исключает расхождение между ними при правке.

  Зачем вообще. 11.09.2026 сессия запустила два субагента, дождалась одного и написала
  раздел за второго по собственным заметкам, пока тот ещё работал. Список живых прогонов,
  который держится в памяти, теряется ровно тогда, когда он нужен.

  Пропускает при любой своей ошибке: учёт, ломающий работу, снимут вместе с пользой.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

# СЛЕД РЕШЕНИЙ роли «учёт». Работает НЕЗАВИСИМО от режима оркестрации (учёт живых
# субагентов ведётся всегда, чтобы список был точен, если режим включат посреди
# сессии) — трейс пишется на тех же условиях. Отсутствие файла не роняет хук.
$__traceLib = Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\trace.ps1'
if (Test-Path -LiteralPath $__traceLib) { . $__traceLib }

try {
    # Stdin явно как UTF-8: [Console]::In в PowerShell 5.1 декодирует кодовой страницей
    # консоли. Здесь искажается описание субагента, которое потом печатает страж хода.
    $stdinStream = [Console]::OpenStandardInput()
    $stdinReader = New-Object System.IO.StreamReader($stdinStream, (New-Object System.Text.UTF8Encoding($false)))
    $raw = $stdinReader.ReadToEnd()
    if (-not $raw) { exit 0 }
    $ev = $raw | ConvertFrom-Json

    $stateDir = Join-Path $env:ProgramData 'AIR OS\State'
    if (-not (Test-Path -LiteralPath $stateDir)) {
        New-Item -ItemType Directory -Force -Path $stateDir | Out-Null
    }
    $runFile = Join-Path $stateDir ("woody-agents-" + $ev.session_id + ".json")

    $running = @()
    if (Test-Path -LiteralPath $runFile) {
        try {
            $st = Get-Content -LiteralPath $runFile -Raw -Encoding UTF8 | ConvertFrom-Json
            $running = @($st.running)
        } catch { }
    }

    $id = [string]$ev.agent_id
    $evName = [string]$ev.hook_event_name
    if (-not $id) {
        try {
            Write-WoodyTrace -SessionId ([string]$ev.session_id) -Role 'учёт' -Event $evName `
                -Decision 'пропущено-нечем' -Detail 'agent_id отсутствует в событии'
        } catch { }
        exit 0
    }

    $decision = 'записал'
    $detail = ''
    switch ($evName) {
        'SubagentStart' {
            if (-not ($running | Where-Object { $_.id -eq $id })) {
                $running += [pscustomobject]@{
                    id      = $id
                    type    = [string]$ev.agent_type
                    started = (Get-Date).ToString('s')
                }
                $detail = "старт $id ($([string]$ev.agent_type))"
            } else {
                $decision = 'пропустил'
                $detail = "$id уже в учёте"
            }
        }
        'SubagentStop' {
            $wasRunning = [bool]($running | Where-Object { $_.id -eq $id })
            $running = @($running | Where-Object { $_.id -ne $id })
            $detail = if ($wasRunning) { "стоп $id" } else { "$id не значился живым" }
        }
        default {
            try {
                Write-WoodyTrace -SessionId ([string]$ev.session_id) -Role 'учёт' -Event $evName `
                    -Decision 'пропущено-нечем' -Detail "событие $evName не обрабатывается"
            } catch { }
            exit 0
        }
    }

    @{ session_id = $ev.session_id; updated = (Get-Date).ToString('s'); running = @($running) } |
        ConvertTo-Json -Depth 5 | Set-Content -LiteralPath $runFile -Encoding UTF8

    # СЛЕД РЕШЕНИЯ. Пишется ПОСЛЕ записи файла учёта — детали отражают то, что реально
    # легло на диск, а не намерение до попытки.
    try {
        Write-WoodyTrace -SessionId ([string]$ev.session_id) -Role 'учёт' -Event $evName `
            -Decision $decision -Detail $detail -Extra @{ живых = $running.Count }
    } catch { }
    exit 0
}
catch {
    exit 0
}
