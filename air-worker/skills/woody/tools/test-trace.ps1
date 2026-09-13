<#
.SYNOPSIS
  Прогонная проверка режима отладки: каждая роль пишет в след своё решение, а
  поломка следа не превращается в тихое «всё в порядке».

.DESCRIPTION
  Заведено 12.09.2026 прямым запросом ЛПР: «чтобы на каждом ходе я видел, что
  происходит — постановщик записал то, исполнитель записал это». Причина глубже
  удобства — РЕЗУЛЬТАТ СТРАЖА ЭТО ОТСУТСТВИЕ СОБЫТИЯ, и отличить исправную тишину
  от мёртвого хука раньше можно было только замером зонда.

  ГЛАВНЫЙ РЕГРЕССИОННЫЙ СЦЕНАРИЙ (первая проверка ниже) воспроизводит ровно тот
  прогон, который в тот же день не прошёл живьём: turn-guard.ps1 на stdin
  {"session_id":"...","hook_event_name":"Stop","last_assistant_message":"короткий
  ответ без формата"} возвращал 0 вместо 2 и не писал в след ничего. Причина —
  опечатка $sessionId вместо $sessionKey в вызове Write-WoodyTrace: параметр
  -SessionId Mandatory, значение было $null, связывание параметра бросало
  исключение ДО входа в тело функции (её собственный try/catch поймать это не
  мог), исключение улетало в общий catch хука (правило 2: «пропускает при любой
  своей ошибке») — и хук отдавал exit 0, даже когда формат не держался. Починено
  исправлением имени переменной И разводкой: вызов следа обёрнут СВОИМ try/catch,
  отдельным от общего catch хука, — поломка следа отныне не может выдать себя за
  «формат в порядке».

  По образцу tools/test-mode-guard.ps1 и tools/test-interrupt-guard.ps1: тесты не
  читают код, а ПРОГОНЯЮТ хуки в подставном дереве с подставными session_id и
  сверяют код возврата, файлы состояния И содержимое woody-trace-<id>.jsonl.

  Возврат 0 — все проверки прошли, 1 — есть провал, 2 — охват пуст (проверять
  нечем, а не «норма»).
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$skillRoot = Split-Path -Parent $PSScriptRoot
$stateDir = Join-Path $env:ProgramData 'AIR OS\State'

. (Join-Path $skillRoot 'lib\trace.ps1')

$passed = 0
$failed = 0
$cleanupPaths = New-Object System.Collections.Generic.List[string]

function Assert-That([string]$title, [scriptblock]$check) {
    try {
        $verdict = & $check
        # Метка ASCII идёт ПЕРВОЙ намеренно: судья зовёт проверку через Start-Process с
        # перенаправлением, кириллица в перехваченном выводе превращается в мусор.
        if ($verdict) { Write-Output "  [PASS]  ПРОШЛА  $title"; $script:passed++ }
        else { Write-Output "  [FAIL]  ПРОВАЛ  $title"; $script:failed++ }
    } catch {
        Write-Output "  [FAIL]  ПРОВАЛ  $title -- $($_.Exception.Message)"
        $script:failed++
    }
}

function Register-Cleanup([string]$path) { $cleanupPaths.Add($path) }

function New-TestSessionId {
    $id = 'woody-test-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
    foreach ($pattern in @('woody-mode-{0}.json', 'woody-agents-{0}.json', 'woody-session-probe-{0}.json',
                            'woody-prompt-probe-{0}.json', 'woody-verdict-{0}.json', 'woody-mode-off-approved-{0}.json',
                            'woody-trace-{0}.jsonl')) {
        Register-Cleanup (Join-Path $stateDir ($pattern -f $id))
    }
    return $id
}

function Set-ModeFile([string]$sessionKey, [bool]$enabled, [int]$subagents) {
    $path = Join-Path $stateDir "woody-mode-$sessionKey.json"
    @{ enabled = $enabled; subagents = $subagents; session_id = $sessionKey
       updated = (Get-Date).ToString('s'); enabled_at = (Get-Date).ToString('s') } |
        ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $path -Encoding UTF8
    Register-Cleanup $path
    return $path
}

# Хуки читают stdin ЯВНО как UTF-8 -- тест обязан писать тем же кодированием, иначе
# кириллица разойдётся так же, как расходилась в живом прогоне 12.09.2026.
function Invoke-HookStdin([string]$scriptPath, [string]$json) {
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = 'powershell.exe'
    $psi.Arguments = '-NoProfile -ExecutionPolicy Bypass -File "' + $scriptPath + '"'
    $psi.RedirectStandardInput = $true
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.StandardOutputEncoding = [System.Text.Encoding]::UTF8
    $psi.StandardErrorEncoding = [System.Text.Encoding]::UTF8
    $proc = [System.Diagnostics.Process]::Start($psi)
    $writer = New-Object System.IO.StreamWriter($proc.StandardInput.BaseStream, (New-Object System.Text.UTF8Encoding($false)))
    $writer.Write($json)
    $writer.Flush()
    $writer.Close()
    $stdout = $proc.StandardOutput.ReadToEnd()
    $stderr = $proc.StandardError.ReadToEnd()
    $proc.WaitForExit()
    return [pscustomobject]@{ Code = $proc.ExitCode; Stdout = $stdout; Stderr = $stderr }
}

function New-EventJson([hashtable]$fields) { return ($fields | ConvertTo-Json -Depth 6 -Compress) }

function Get-Trace([string]$sessionKey) { return @(Get-WoodyTrace -SessionId $sessionKey -Last 50) }

function New-FakeTranscript([string]$path, [string]$assistantText) {
    $lines = @(
        (@{ type = 'user'; message = @{ role = 'user'; content = 'привет' }; cwd = 'C:\fake'; sessionId = 'x' } | ConvertTo-Json -Depth 5 -Compress),
        (@{ type = 'assistant'; message = @{ role = 'assistant'; content = @(@{ type = 'text'; text = $assistantText }) }; cwd = 'C:\fake'; sessionId = 'x' } | ConvertTo-Json -Depth 5 -Compress)
    )
    Set-Content -LiteralPath $path -Value ($lines -join "`n") -Encoding UTF8
}

$goodMsg = @(
    'Фаза 1 · Тест следа · ступень sonnet · ведущая · раздаю'
    ''
    'Постановка: жёстко не прописана — выполняю сам, по своему пониманию цели'
    'Замер: итерация 1 · $0.01 · ходов 1 · всего $0.01 из бюджета $20'
    'Дальше: работаю дальше'
    'От тебя жду: подтверди план'
) -join "`n"
$badMsg = 'короткий ответ без формата'

Write-Output 'Прогонная проверка режима отладки: след решений по ролям'
Write-Output ''

try {

$turnGuardPath = Join-Path $skillRoot 'hooks\turn-guard.ps1'
$promptGuardPath = Join-Path $skillRoot 'hooks\prompt-guard.ps1'
$subagentTrackPath = Join-Path $skillRoot 'hooks\subagent-track.ps1'
$modeScript = Join-Path $skillRoot 'hooks\mode.ps1'

# ======================================================================================
# РЕГРЕССИЯ: ТОЧНЫЙ сценарий пробы 12.09.2026, не прошедшей живьём.
# ======================================================================================
$sidR = New-TestSessionId
Set-ModeFile $sidR $true 0 | Out-Null
$rR = Invoke-HookStdin $turnGuardPath (New-EventJson @{
    session_id = $sidR; hook_event_name = 'Stop'; last_assistant_message = $badMsg
})
Assert-That 'РЕГРЕССИЯ: короткое сообщение без формата -- turn-guard.ps1 отдаёт код 2, не 0' {
    $rR.Code -eq 2
}
$traceR = Get-Trace $sidR
Assert-That 'РЕГРЕССИЯ: страж записал в след РОВНО ЭТО решение (роль страж, отклонил)' {
    @($traceR | Where-Object { $_.role -eq 'страж' -and $_.decision -eq 'отклонил' }).Count -gt 0
}
Assert-That 'РЕГРЕССИЯ: запись стража несёт названные недостачи в detail, а не пусто' {
    $rec = @($traceR | Where-Object { $_.role -eq 'страж' })[0]
    ($rec.detail) -and ($rec.detail.Length -gt 0)
}

# ======================================================================================
# СТРАЖ (Stop): пропуск тоже пишется, не только отказ.
# ======================================================================================
$sid1 = New-TestSessionId
Set-ModeFile $sid1 $true 0 | Out-Null
$r1 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
    session_id = $sid1; hook_event_name = 'Stop'; last_assistant_message = $goodMsg
})
Assert-That 'страж: полный формат -- код 0' { $r1.Code -eq 0 }
$trace1 = Get-Trace $sid1
Assert-That 'страж: полный формат -- след несёт "пропустил", а не только отказы' {
    @($trace1 | Where-Object { $_.role -eq 'страж' -and $_.decision -eq 'пропустил' }).Count -gt 0
}

# ======================================================================================
# СУДЬЯ: нет run-config.json -- "пропущено-нечем"; есть -- "записал".
# ======================================================================================
$sidJ0 = New-TestSessionId
$prodJ0 = Join-Path ([System.IO.Path]::GetTempPath()) ('woody-test-trace-j0-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $prodJ0 | Out-Null
Set-ModeFile $sidJ0 $true 0 | Out-Null
try {
    $null = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sidJ0; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prodJ0
    })
    $traceJ0 = Get-Trace $sidJ0
    Assert-That 'судья: нет run-config.json -- след несёт "пропущено-нечем"' {
        @($traceJ0 | Where-Object { $_.role -eq 'судья' -and $_.decision -eq 'пропущено-нечем' }).Count -gt 0
    }
} finally { Remove-Item -LiteralPath $prodJ0 -Recurse -Force -ErrorAction SilentlyContinue }

$sidJ1 = New-TestSessionId
$prodJ1 = Join-Path ([System.IO.Path]::GetTempPath()) ('woody-test-trace-j1-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $prodJ1 | Out-Null
Set-ModeFile $sidJ1 $true 0 | Out-Null
try {
    Set-Content -LiteralPath (Join-Path $prodJ1 'run-config.json') -Value '{"judge":{"checks":[]},"ladder":["script","sonnet"]}' -Encoding UTF8
    $null = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sidJ1; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prodJ1
    })
    $traceJ1 = Get-Trace $sidJ1
    Assert-That 'судья: run-config.json есть -- след несёт "записал" с кодом в detail' {
        @($traceJ1 | Where-Object { $_.role -eq 'судья' -and $_.decision -eq 'записал' -and $_.detail -match 'код|таймаут' }).Count -gt 0
    }
} finally {
    Remove-Item -LiteralPath $prodJ1 -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-verdict-$sidJ1.json") -Force -ErrorAction SilentlyContinue
}

# ======================================================================================
# ВТОРОЙ РУБЕЖ (UserPromptSubmit): пропуск и отказ оба пишутся.
# ======================================================================================
$sidP1 = New-TestSessionId
Set-ModeFile $sidP1 $true 0 | Out-Null
$transcriptGood = [System.IO.Path]::GetTempFileName()
try {
    New-FakeTranscript $transcriptGood $goodMsg
    $null = Invoke-HookStdin $promptGuardPath (New-EventJson @{
        session_id = $sidP1; hook_event_name = 'UserPromptSubmit'; transcript_path = $transcriptGood
    })
    $traceP1 = Get-Trace $sidP1
    Assert-That 'второй рубеж: предыдущий ход полон -- след несёт "пропустил"' {
        @($traceP1 | Where-Object { $_.role -eq 'второй рубеж' -and $_.decision -eq 'пропустил' }).Count -gt 0
    }
} finally { Remove-Item -LiteralPath $transcriptGood -Force -ErrorAction SilentlyContinue }

$sidP2 = New-TestSessionId
Set-ModeFile $sidP2 $true 0 | Out-Null
$transcriptBad = [System.IO.Path]::GetTempFileName()
try {
    New-FakeTranscript $transcriptBad 'Фаза 1 · Тест · ступень sonnet · ведущая · раздаю'
    $null = Invoke-HookStdin $promptGuardPath (New-EventJson @{
        session_id = $sidP2; hook_event_name = 'UserPromptSubmit'; transcript_path = $transcriptBad
    })
    $traceP2 = Get-Trace $sidP2
    Assert-That 'второй рубеж: предыдущий ход прерван -- след несёт "отклонил" (сообщение ЛПР при этом не блокируется)' {
        @($traceP2 | Where-Object { $_.role -eq 'второй рубеж' -and $_.decision -eq 'отклонил' }).Count -gt 0
    }
} finally { Remove-Item -LiteralPath $transcriptBad -Force -ErrorAction SilentlyContinue }

# ======================================================================================
# УЧЁТ (SubagentStart/SubagentStop): пишет независимо от режима оркестрации.
# ======================================================================================
$sidU = New-TestSessionId
try {
    $null = Invoke-HookStdin $subagentTrackPath (New-EventJson @{
        session_id = $sidU; hook_event_name = 'SubagentStart'; agent_id = 'a1'; agent_type = 'general-purpose'
    })
    $traceU1 = Get-Trace $sidU
    Assert-That 'учёт: SubagentStart -- след несёт "записал"' {
        @($traceU1 | Where-Object { $_.role -eq 'учёт' -and $_.event -eq 'SubagentStart' -and $_.decision -eq 'записал' }).Count -gt 0
    }

    $null = Invoke-HookStdin $subagentTrackPath (New-EventJson @{
        session_id = $sidU; hook_event_name = 'SubagentStop'; agent_id = 'a1'
    })
    $traceU2 = Get-Trace $sidU
    Assert-That 'учёт: SubagentStop -- след несёт "записал"' {
        @($traceU2 | Where-Object { $_.role -eq 'учёт' -and $_.event -eq 'SubagentStop' -and $_.decision -eq 'записал' }).Count -gt 0
    }

    $null = Invoke-HookStdin $subagentTrackPath (New-EventJson @{
        session_id = $sidU; hook_event_name = 'SubagentStart'
    })
    $traceU3 = Get-Trace $sidU
    Assert-That 'учёт: событие без agent_id -- след несёт "пропущено-нечем", а не молчание' {
        @($traceU3 | Where-Object { $_.role -eq 'учёт' -and $_.decision -eq 'пропущено-нечем' }).Count -gt 0
    }
} finally {
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-agents-$sidU.json") -Force -ErrorAction SilentlyContinue
}

# ======================================================================================
# РЕЖИМ (-On/-Off/-Reconcile/-Track в mode.ps1).
# ======================================================================================
function Invoke-Mode([string]$sessionKey, [string[]]$modeArgs) {
    $env:CLAUDE_CODE_SESSION_ID = $sessionKey
    $out = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $modeScript @modeArgs 2>&1 | Out-String
    return [pscustomobject]@{ Code = $LASTEXITCODE; Text = $out }
}

$sidM1 = New-TestSessionId
try {
    $null = Invoke-Mode $sidM1 @('-On', '-Subagents', '0')
    $traceM1 = Get-Trace $sidM1
    Assert-That 'режим: -On на реальном скиле -- след несёт "записал"' {
        @($traceM1 | Where-Object { $_.role -eq 'режим' -and $_.event -eq '-On' -and $_.decision -eq 'записал' }).Count -gt 0
    }

    $null = Invoke-Mode $sidM1 @('-Track', '999999', '-TrackTitle', 'т', '-TrackTier', 'sonnet')
    $traceM2 = Get-Trace $sidM1
    Assert-That 'режим: -Track -- след несёт "записал"' {
        @($traceM2 | Where-Object { $_.role -eq 'режим' -and $_.event -eq '-Track' -and $_.decision -eq 'записал' }).Count -gt 0
    }

    $null = Invoke-Mode $sidM1 @('-Reconcile', 'нет-такого')
    $traceM3 = Get-Trace $sidM1
    Assert-That 'режим: -Reconcile -- след несёт "записал"' {
        @($traceM3 | Where-Object { $_.role -eq 'режим' -and $_.event -eq '-Reconcile' -and $_.decision -eq 'записал' }).Count -gt 0
    }

    # -Off без согласования -- отказ, тоже должен попасть в след.
    $rOffDenied = Invoke-Mode $sidM1 @('-Off')
    $traceM4 = Get-Trace $sidM1
    Assert-That 'режим: -Off без согласования ЛПР -- след несёт "отклонил"' {
        ($rOffDenied.Code -ne 0) -and (@($traceM4 | Where-Object { $_.role -eq 'режим' -and $_.event -eq '-Off' -and $_.decision -eq 'отклонил' }).Count -gt 0)
    }

    # -Off с согласованием -- успех тоже пишется.
    $approvalM1 = Join-Path $stateDir "woody-mode-off-approved-$sidM1.json"
    @{ session_id = $sidM1 } | ConvertTo-Json | Set-Content -LiteralPath $approvalM1 -Encoding UTF8
    $rOffOk = Invoke-Mode $sidM1 @('-Off')
    $traceM5 = Get-Trace $sidM1
    Assert-That 'режим: -Off с согласованием -- след несёт "записал"' {
        ($rOffOk.Code -eq 0) -and (@($traceM5 | Where-Object { $_.role -eq 'режим' -and $_.event -eq '-Off' -and $_.decision -eq 'записал' }).Count -gt 0)
    }
} finally {
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-mode-$sidM1.json") -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-agents-$sidM1.json") -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-session-probe-$sidM1.json") -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-mode-off-approved-$sidM1.json") -Force -ErrorAction SilentlyContinue
}

# -On, отказ (команда хука объявлена, но её нет на диске) -- тоже пишется в след,
# и в подставном дереве, где lib\trace.ps1 скопирован рядом (иначе Write-WoodyTrace
# просто не существует, и это НЕ баг: страж пропускает отсутствие своей же зависимости).
$fakeBase = Join-Path ([System.IO.Path]::GetTempPath()) ('woody-test-trace-fake-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path (Join-Path $fakeBase 'hooks') | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $fakeBase 'lib') | Out-Null
Copy-Item -LiteralPath $modeScript -Destination (Join-Path $fakeBase 'hooks\mode.ps1')
Copy-Item -LiteralPath (Join-Path $skillRoot 'lib\trace.ps1') -Destination (Join-Path $fakeBase 'lib\trace.ps1')
$skillText = @"
---
name: air-woody-test
description: 'подставной скил для проверки следа при предполётном отказе'
hooks:
  Stop:
    - hooks:
        - type: command
          command: '"Z:\заведомо-нет-такого\turn-guard.cmd"'
          timeout: 20
---
# подставной
"@
Set-Content -LiteralPath (Join-Path $fakeBase 'SKILL.md') -Value $skillText -Encoding UTF8
$sidM2 = New-TestSessionId
try {
    $env:CLAUDE_CODE_SESSION_ID = $sidM2
    $null = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $fakeBase 'hooks\mode.ps1') -On 2>&1
    $traceM6 = Get-Trace $sidM2
    Assert-That 'режим: -On отказал (команда хука не существует) -- след несёт "отклонил"' {
        @($traceM6 | Where-Object { $_.role -eq 'режим' -and $_.event -eq '-On' -and $_.decision -eq 'отклонил' }).Count -gt 0
    }
} finally {
    Remove-Item -LiteralPath $fakeBase -Recurse -Force -ErrorAction SilentlyContinue
}

# ======================================================================================
# mode.ps1 -Trace: читаемый вывод и ТРЕВОГА при пустом следе, а не "всё спокойно".
#
# Текст, который mode.ps1 печатает через Write-Output, здесь НЕ сверяется совпадением
# кириллицы, захваченной через конвейер дочернего процесса: PowerShell 5.1 отдаёт
# Write-Output в кодовой странице консоли при перенаправлении, и кириллица разбивается
# на ЗАПИСИ -- та же беда, ради которой tools/test-interrupt-guard.ps1 сверяет код
# возврата и файл состояния, а не текст отказа mode.ps1 (см. его сценарий 2). Поэтому:
# формат строки и текст тревоги проверяются В ИСТОЧНИКЕ -- та же техника, что
# tools/test-mode-guard.ps1 применяет к «РЕГИСТРАЦИЯ НЕ ПОДТВЕРЖДЕНА» (читает .ps1 как
# UTF8, а не выполняет и не перехватывает поток); а фактическое ПОВЕДЕНИЕ данных --
# напрямую через Get-WoodyTrace в этом же процессе, минуя дочерний процесс целиком.
# ======================================================================================
$modeSource = Get-Content -LiteralPath $modeScript -Raw -Encoding UTF8

Assert-That 'mode.ps1 -Trace: формат строки несёт время, роль, событие, решение и подробность' {
    $modeSource.Contains('{1,-14} {2,-18} {3,-12} {4}{5}')
}
Assert-That 'mode.ps1 -Trace: пустой след объявлен явной тревогой в тексте, а не "всё спокойно" (сверено в исходнике)' {
    $modeSource.Contains('следа нет') -and $modeSource.Contains('НЕ «всё спокойно»')
}

$sidT1 = New-TestSessionId
Set-ModeFile $sidT1 $true 0 | Out-Null
$null = Invoke-HookStdin $turnGuardPath (New-EventJson @{
    session_id = $sidT1; hook_event_name = 'Stop'; last_assistant_message = $goodMsg
})
$recordsT1 = Get-Trace $sidT1
Assert-That 'mode.ps1 -Trace: данные под печатью (Get-WoodyTrace) несут все пять полей непусто' {
    ($recordsT1.Count -gt 0) -and (@($recordsT1 | Where-Object { -not ($_.ts -and $_.role -and $_.event -and $_.decision) }).Count -eq 0)
}

$sidT2 = New-TestSessionId
Set-ModeFile $sidT2 $true 0 | Out-Null
$traceOut2 = Invoke-Mode $sidT2 @('-Trace')
Assert-That 'mode.ps1 -Trace: пустой след при включённом режиме -- код 0 (предупреждает, не блокирует)' {
    ($traceOut2.Code -eq 0) -and (@(Get-Trace $sidT2).Count -eq 0)
}

} finally {
    foreach ($p in ($cleanupPaths | Select-Object -Unique)) {
        if ($p -match 'woody-test-') { Remove-Item -LiteralPath $p -Force -ErrorAction SilentlyContinue }
    }
}

Write-Output ''
Write-Output ("прошло {0}, провалено {1}" -f $passed, $failed)
if (($passed + $failed) -eq 0) {
    Write-Output 'НЕЧЕМ ПРОВЕРИТЬ: ни одна проверка не собралась -- пустой охват, а не норма.'
    exit 2
}
if ($failed -gt 0) { exit 1 }
exit 0
