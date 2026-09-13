<#
.SYNOPSIS
  Второй рубеж стража: ловит ход, прерванный до того, как успел сработать Stop.

.DESCRIPTION
  ЗАДАЧА 1, 12.09.2026. turn-guard.ps1 висит на Stop, а при прерывании хода ЛПР Stop не
  наступает — страж не запускается вовсе. Замерено разбором 12.09.2026: зонд
  woody-session-probe-<id>.json держал запись 10:35, после чего шесть прерванных ходов
  подряд не оставили ни одной. Отдельно подтверждено разбором дня (docs — «делегирование»):
  ЛПР прерывает ход постоянно и по делу, он работает с телефона и не может дописать
  вводную в идущий поток. Это НОРМАЛЬНЫЙ режим работы, а не сбой, и механизм не должен с
  ним воевать.

  UserPromptSubmit наступает СО СЛЕДУЮЩИМ сообщением ЛПР — то есть уже после прерванного
  хода. У этого события нет last_assistant_message: единственный источник текста —
  журнал сессии по transcript_path (см. lib/transcript.ps1 — там же зафиксировано, ЧЕМ
  подтверждён формат журнала). Хук достаёт оттуда последнее сообщение ассистента и
  проверяет ЕГО ЖЕ функцией Test-TurnFormat (lib/turn-format.ps1), которой пользуется
  turn-guard.ps1 — общая логика, а не вторая копия, которая разойдётся с первой.

  ПОЧЕМУ ХУК НЕ БЛОКИРУЕТ НОВОЕ СООБЩЕНИЕ (exit 2), А ДОБАВЛЯЕТ КОНТЕКСТ (exit 0 +
  additionalContext) — решение, принятое здесь при реализации, а не продиктованное
  формулировкой задачи буквально:

  Задачи 3, 4 и 5 явно говорят «хук НЕ БЛОКИРУЕТ ход» / «ход НЕ ЗАКАНЧИВАЕТСЯ» — то есть
  автор постановки различал два действия словами и здесь слова «блокирует» НЕТ. Смысл
  тоже расходится: exit 2 у UserPromptSubmit по документации хуков блокирует именно ЭТО
  сообщение ЛПР — то самое, которым он «дописывает вводную в идущий поток» после
  прерывания. Блокировать ровно то действие, которое найдено НОРМАЛЬНЫМ режимом работы, —
  значит воевать с находкой дня, а не лечить дырку. Поэтому хук пропускает сообщение
  (exit 0) и несёт замечание вместе с ним через hookSpecificOutput.additionalContext:
  ассистент увидит, что предыдущий ход не закрылся форматом, и сможет отчитаться в начале
  нового хода — не рискуя обрывом того самого сообщения, ради которого ЛПР прервал работу.

  ПРОБА СТФАКТА ПРО STDIN. Поля события выяснялись и документацией (code.claude.com/docs/
  en/hooks.md называет session_id, transcript_path, cwd, hook_event_name), и чтением
  реального журнала этой же сессии — но НЕ прогоном живого UserPromptSubmit на этой
  машине: он наступит только со следующим сообщением ЛПР. Поэтому хук пишет необработанный
  снимок stdin в woody-prompt-probe-<session_id>.json ПРИ КАЖДОМ запуске, а не один раз —
  это тот же факт, но проверяемый снова, а не сгоревшее одноразовое наблюдение. Отсутствие
  ожидаемого поля не роняет хук: каждое обращение к $ev защищено и при недостаче тихо
  пропускает (правило 2 стража).

  ДВА ПРАВИЛА БЕЗОПАСНОСТИ СТРАЖА — те же, что у turn-guard.ps1: молчит, пока режим не
  включён явно; пропускает при любой своей ошибке.
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Continue'

. (Join-Path $PSScriptRoot '..\lib\turn-format.ps1')
. (Join-Path $PSScriptRoot '..\lib\transcript.ps1')

# СЛЕД РЕШЕНИЙ роли «второй рубеж». Отсутствие файла не роняет хук — Write-WoodyTrace
# тогда не появится, и вызов ниже обёрнут СВОИМ try/catch, а не общим catch хука: та же
# причина разводки, что в turn-guard.ps1 — поломка следа не имеет права выглядеть как
# «предыдущий ход был в порядке», если на деле его никто не проверил.
$__traceLib = Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\trace.ps1'
if (Test-Path -LiteralPath $__traceLib) { . $__traceLib }

try {
    $stdinStream = [Console]::OpenStandardInput()
    $stdinReader = New-Object System.IO.StreamReader($stdinStream, (New-Object System.Text.UTF8Encoding($false)))
    $raw = $stdinReader.ReadToEnd()
    if (-not $raw) { exit 0 }
    $ev = $raw | ConvertFrom-Json

    $stateDir = Join-Path $env:ProgramData 'AIR OS\State'
    if (-not (Test-Path -LiteralPath $stateDir)) { New-Item -ItemType Directory -Force -Path $stateDir | Out-Null }

    $sessionKey = [string]$ev.session_id
    if (-not $sessionKey) { exit 0 }

    # Снимок stdin — ФАКТ про реальную форму события, обновляется на каждом запуске.
    # Обрезан: цель — знать, какие поля пришли, а не хранить журнал целиком повторно.
    try {
        $keys = @()
        if ($ev.PSObject) { $keys = @($ev.PSObject.Properties.Name) }
        $rawHead = if ($raw.Length -gt 2000) { $raw.Substring(0, 2000) } else { $raw }
        @{
            session_id      = $sessionKey
            hook_event_name = [string]$ev.hook_event_name
            captured        = (Get-Date).ToString('s')
            fields_present  = $keys
            has_cwd              = [bool]$ev.cwd
            has_transcript_path  = [bool]$ev.transcript_path
            raw_head_2000   = $rawHead
        } | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath (Join-Path $stateDir "woody-prompt-probe-$sessionKey.json") -Encoding UTF8
    } catch { }

    $modeFile = Join-Path $stateDir "woody-mode-$sessionKey.json"
    if (-not (Test-Path -LiteralPath $modeFile)) { exit 0 }
    $mode = Get-Content -LiteralPath $modeFile -Raw -Encoding UTF8 | ConvertFrom-Json
    if (-not $mode.enabled) { exit 0 }

    $transcriptPath = [string]$ev.transcript_path
    $msg = Get-LastAssistantText $transcriptPath
    if (-not $msg) { exit 0 }   # нечем судить: либо первый ход сессии, либо журнал недостижим

    $running = @()
    $runFile = Join-Path $stateDir ("woody-agents-" + $sessionKey + ".json")
    if (Test-Path -LiteralPath $runFile) {
        try {
            $st = Get-Content -LiteralPath $runFile -Raw -Encoding UTF8 | ConvertFrom-Json
            $running = @($st.running)
        } catch { }
    }

    $cwd = Get-EventCwd $ev
    $ladder = $null
    if ($cwd) {
        $cfgPath = Join-Path $cwd 'run-config.json'
        if (Test-Path -LiteralPath $cfgPath -PathType Leaf) {
            try {
                $cfg = Get-Content -LiteralPath $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json
                if ($cfg.ladder) { $ladder = @($cfg.ladder) }
            } catch { }
        }
    }

    $missing = @(Test-TurnFormat -Msg $msg -Running $running -DeclaredSubagents ([int]$mode.subagents) -Ladder $ladder)

    # СЛЕД РЕШЕНИЯ. Этот рубеж никогда не блокирует (exit 2 здесь не бывает), но
    # «пропустил» и «отклонил» — про то, держал ли ПРЕДЫДУЩИЙ ход формат, а не про то,
    # заблокировано ли ЭТО сообщение ЛПР. Пишется ОБА исхода — тем же правилом, что и
    # у стража: молчание про хороший ход неотличимо от невызванной проверки.
    try {
        Write-WoodyTrace -SessionId $sessionKey -Role 'второй рубеж' -Event 'UserPromptSubmit' `
            -Decision $(if ($missing.Count -eq 0) { 'пропустил' } else { 'отклонил' }) `
            -Detail $(if ($missing.Count -eq 0) { 'предыдущий ход держал формат' } else { ($missing -join '; ') })
    } catch { }

    if ($missing.Count -eq 0) { exit 0 }

    $note = "ПРЕДЫДУЩИЙ ХОД НЕ ЗАКРЫЛСЯ УТВЕРЖДЁННЫМ ФОРМАТОМ (второй рубеж — UserPromptSubmit, " +
            "прочитано из журнала сессии, а не из last_assistant_message: скорее всего ход был " +
            "прерван до Stop). Не хватало: " + ($missing -join '; ') + ". " +
            "Это сообщение ЛПР пропущено без блокировки: прерывание — нормальный режим работы, " +
            "воевать с ним нельзя. Но в начале ЭТОГО хода отчитайся коротко, чем кончился прерванный: " +
            "если оркестрация шла, не подменяй результат незавершённых субагентов собственными " +
            "заметками — дождись отчёта."

    $json = @{
        hookSpecificOutput = @{
            hookEventName    = 'UserPromptSubmit'
            additionalContext = $note
        }
    } | ConvertTo-Json -Depth 4 -Compress

    # Байты напрямую: PowerShell 5.1 отдаёт stdout в кодовой странице консоли, и
    # кириллица приходит как «???????».
    $out = [Console]::OpenStandardOutput()
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($json)
    $out.Write($bytes, 0, $bytes.Length)
    $out.Flush()
    exit 0
}
catch {
    exit 0
}
