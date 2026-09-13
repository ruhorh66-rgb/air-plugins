<#
.SYNOPSIS
  Прогонная проверка правки 12.09.2026: второй рубеж, лестница в строке субагента,
  судья фоном, обязательная петля, запрет частичного выхода, точный резолв ступени,
  и новая логика учёта в mode.ps1 (-Track, -Forget, -Reconcile, -ReconcileNone).

.DESCRIPTION
  По образцу tools/test-mode-guard.ps1: тесты не читают код, а ПРОГОНЯЮТ его в
  подставном дереве и с подставными session_id, сверяя код возврата и содержимое
  файлов состояния. Каждый session_id — свежий guid: тесты не имеют права коснуться
  состояния живых сессий на машине.

  Возврат 0 — все проверки прошли, 1 — есть провал, 2 — охват пуст (проверять нечем).
#>
[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'
$skillRoot = Split-Path -Parent $PSScriptRoot
$stateDir = Join-Path $env:ProgramData 'AIR OS\State'

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

# Каждый выданный session_id сразу получает ВСЕ свои возможные файлы состояния в список
# уборки -- независимо от того, какой из хуков их на деле создаст (session-probe и
# verdict пишутся побочным эффектом самих хуков, а не нашими помощниками, и их легко
# забыть зарегистрировать по месту). Удаление несуществующего файла в конце -- no-op.
function New-TestSessionId {
    $id = 'woody-test-' + [guid]::NewGuid().ToString('N').Substring(0, 12)
    foreach ($pattern in @('woody-mode-{0}.json', 'woody-agents-{0}.json', 'woody-session-probe-{0}.json',
                            'woody-prompt-probe-{0}.json', 'woody-verdict-{0}.json', 'woody-mode-off-approved-{0}.json')) {
        Register-Cleanup (Join-Path $stateDir ($pattern -f $id))
    }
    return $id
}

# --- помощники состояния -------------------------------------------------------------
function Set-ModeFile([string]$sessionKey, [bool]$enabled, [int]$subagents) {
    $path = Join-Path $stateDir "woody-mode-$sessionKey.json"
    @{ enabled = $enabled; subagents = $subagents; session_id = $sessionKey
       updated = (Get-Date).ToString('s'); enabled_at = (Get-Date).ToString('s') } |
        ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $path -Encoding UTF8
    Register-Cleanup $path
    return $path
}

function Set-AgentsFile([string]$sessionKey, [object[]]$running) {
    $path = Join-Path $stateDir "woody-agents-$sessionKey.json"
    @{ session_id = $sessionKey; updated = (Get-Date).ToString('s'); running = @($running) } |
        ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $path -Encoding UTF8
    Register-Cleanup $path
    return $path
}

# Хуки читают stdin ЯВНО как UTF-8 (см. turn-guard.ps1) -- отправитель обязан писать
# тем же кодированием, иначе кириллица теста разойдётся с кириллицей хука так же, как
# расходилась в живом прогоне 12.09.2026. Пайп PowerShell 5.1 в родной exe идёт через
# консольную кодовую страницу и здесь не годится -- поток пишется явно.
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

function New-EventJson([hashtable]$fields) {
    return ($fields | ConvertTo-Json -Depth 6 -Compress)
}

# Строка состояния, полная по всем семи пунктам плюс задаче 2 -- используется как
# основа, которую отдельные тесты портят ровно одним недостающим элементом.
$goodMsg = @(
    'Фаза 1 · Тест · ступень sonnet · ведущая · раздаю'
    'Фаза 1 · субагент 1/1 · делает дело · ступень sonnet · прогон идёт'
    ''
    'Постановка: жёстко не прописана — выполняю сам, по своему пониманию цели'
    'Замер: итерация 1 · $0.01 · ходов 1 · всего $0.01 из бюджета $20'
    'Дальше: работаю дальше'
    'От тебя жду: подтверди план'
) -join "`n"

Write-Output 'Прогонная проверка второго рубежа, лестницы, судьи, петли и учёта'
Write-Output ''

try {

# ======================================================================================
# ЗАДАЧА 6 -- Resolve-LadderTier: точное совпадение, имя модели, отказ. Быстрый прогон
# без спавна процесса -- сама функция чистая и детерминированная.
# ======================================================================================
. (Join-Path $skillRoot 'lib\ladder.ps1')
$ladder6 = @('script', 'sonnet:medium', 'sonnet:max', 'opus:medium')

Assert-That 'Resolve-LadderTier: точное совпадение находит верный индекс' {
    (Resolve-LadderTier -Ladder $ladder6 -Tier 'sonnet:max').Index -eq 2
}
Assert-That 'Resolve-LadderTier: "sonnet" без усилия резолвится по имени модели, а не в индекс 0' {
    $r = Resolve-LadderTier -Ladder $ladder6 -Tier 'sonnet'
    $r.Found -and $r.Index -eq 1 -and $r.MatchKind -eq 'model-prefix'
}
Assert-That 'Resolve-LadderTier: ступени, которой нет ни точно, ни по имени, -- явный отказ, не индекс 0' {
    $r = Resolve-LadderTier -Ladder $ladder6 -Tier 'luna'
    (-not $r.Found) -and ($r.Index -eq -1)
}

# Сквозной прогон woody.ps1: план называет ступень, которой в лестнице нет ни точно,
# ни по имени модели -- петля обязана отказать НАЗВАННОЙ причиной, а не тихо взять script.
$prod6 = Join-Path ([System.IO.Path]::GetTempPath()) ('woody-test-prod6-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $prod6 | Out-Null
try {
    Set-Content -LiteralPath (Join-Path $prod6 'PLAN.md') -Value "- [ ] luna  Шаг без такой ступени в лестнице`n" -Encoding UTF8
    # Судья ДО первой итерации обязан вернуть код 1 (не 0), иначе woody.ps1 решит, что
    # цель уже достигнута, и выйдет раньше, чем дойдёт до резолва ступени этого шага.
    @{ goal = 'x'; updated = '2026-09-12'; items = @(@{ id = 'f01'; status = 'pending'; fact = 'x' }) } |
        ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $prod6 'checklist.json') -Encoding UTF8
    @{ judge = @{ checks = @(); checklist = 'checklist.json'; min_facts = 1 }; plan = 'PLAN.md'
       budget = @{ iterations = 1; usd = 1; turns_per_iteration = 1 }
       ladder = @('script', 'sonnet:medium', 'sonnet:max') } |
        ConvertTo-Json -Depth 4 | Set-Content -LiteralPath (Join-Path $prod6 'run-config.json') -Encoding UTF8
    $out6 = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File (Join-Path $skillRoot 'scripts\woody.ps1') `
                -ProductRoot $prod6 2>&1 | Out-String
    $code6 = $LASTEXITCODE
    # Совпадение проверяется по ЛАТИНСКОЙ подстроке ('luna'), не по кириллице: PowerShell
    # 5.1 при захвате stdout ВЛОЖЕННОГО процесса через конвейер (а не реальную консоль)
    # перекодирует нелатинские символы кодовой страницей хоста и буквально теряет их в
    # строке (не просто искажает отображение) -- тот же класс кодировочных потерь, ради
    # которого хуки читают/пишут явными байтовыми потоками. 'luna' -- имя ступени шага,
    # проходит через сообщение только если резолв реально дошёл до отказа по ЭТОМУ шагу,
    # а не свалился в script по умолчанию (тогда бы 'luna' в выводе не было вовсе, а
    # петля продолжила бы работу, а не вышла кодом 1).
    Assert-That 'woody.ps1: ступень плана мимо лестницы -- отказ кодом 1, причина названа (не тихий script)' {
        ($code6 -eq 1) -and ($out6 -match 'luna') -and ($out6 -match 'sonnet:medium')
    }
} finally {
    Remove-Item -LiteralPath $prod6 -Recurse -Force -ErrorAction SilentlyContinue
}

# ======================================================================================
# ЗАДАЧА 2 -- строка субагента обязана называть ступень, названная ступень обязана
# существовать в лестнице продукта, когда лестница известна.
# ======================================================================================
. (Join-Path $skillRoot 'lib\turn-format.ps1')

Assert-That 'Test-TurnFormat: полная строка (задача 1-6 baseline) не даёт недостач' {
    $running = @([pscustomobject]@{ id = 'a1'; type = 'general-purpose' })
    (Test-TurnFormat -Msg $goodMsg -Running $running -DeclaredSubagents 1 -Ladder @('script','sonnet')).Count -eq 0
}

Assert-That 'Test-TurnFormat: строка субагента без "ступень" -- недостача названа' {
    $bad = $goodMsg -replace '· ступень sonnet · прогон идёт', '· прогон идёт'
    $running = @([pscustomobject]@{ id = 'a1'; type = 'general-purpose' })
    $missing = @(Test-TurnFormat -Msg $bad -Running $running -DeclaredSubagents 1 -Ladder @('script','sonnet'))
    @($missing | Where-Object { $_ -match 'без названной ступени' }).Count -gt 0
}

Assert-That 'Test-TurnFormat: ступень субагента вне лестницы продукта -- недостача названа' {
    $bad = $goodMsg -replace '· ступень sonnet · прогон идёт', '· ступень luna · прогон идёт'
    $running = @([pscustomobject]@{ id = 'a1'; type = 'general-purpose' })
    $missing = @(Test-TurnFormat -Msg $bad -Running $running -DeclaredSubagents 1 -Ladder @('script','sonnet'))
    @($missing | Where-Object { $_ -match "которой нет в лестнице продукта" }).Count -gt 0
}

Assert-That 'Test-TurnFormat: без конфигурации (Ladder = $null) ступень вне общего списка не считается недостачей' {
    $running = @([pscustomobject]@{ id = 'a1'; type = 'general-purpose' })
    $missing = @(Test-TurnFormat -Msg $goodMsg -Running $running -DeclaredSubagents 1 -Ladder $null)
    @($missing | Where-Object { $_ -match "которой нет в лестнице продукта" }).Count -eq 0
}

# ======================================================================================
# СКВОЗНЫЕ ПРОГОНЫ turn-guard.ps1 (Stop) -- фиктивные session_id, реальный хук.
# ======================================================================================
$turnGuardPath = Join-Path $skillRoot 'hooks\turn-guard.ps1'

$sid1 = New-TestSessionId
Set-ModeFile $sid1 $true 0 | Out-Null
$r1 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
    session_id = $sid1; hook_event_name = 'Stop'; last_assistant_message = $goodMsg
})
Assert-That 'turn-guard.ps1: полное сообщение без продукта в рабочем каталоге -- код 0' {
    $r1.Code -eq 0
}

$sid2 = New-TestSessionId
Set-ModeFile $sid2 $true 0 | Out-Null
$badMsg2 = $goodMsg -replace '· ступень sonnet · прогон идёт', '· прогон идёт'
$r2 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
    session_id = $sid2; hook_event_name = 'Stop'; last_assistant_message = $badMsg2
})
Assert-That 'turn-guard.ps1: строка субагента без ступени -- код 2, причина названа' {
    ($r2.Code -eq 2) -and ($r2.Stderr -match 'без названной ступени')
}

# --- Задача 4: петля обязательна -----------------------------------------------------
$sid4 = New-TestSessionId
$prod4 = Join-Path ([System.IO.Path]::GetTempPath()) ('woody-test-prod4-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $prod4 | Out-Null
Set-ModeFile $sid4 $true 0 | Out-Null
try {
    Set-Content -LiteralPath (Join-Path $prod4 'run-config.json') -Value '{"judge":{"checks":[]}}' -Encoding UTF8
    Set-Content -LiteralPath (Join-Path $prod4 'PLAN.md') -Value "- [ ] script  Шаг`n" -Encoding UTF8
    $r4 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid4; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod4
    })
    Assert-That 'turn-guard.ps1: конфигурация и PLAN.md есть, steps.jsonl нет -- ход не заканчивается, команда названа' {
        ($r4.Code -eq 2) -and ($r4.Stderr -match 'steps\.jsonl') -and ($r4.Stderr -match 'woody\.ps1')
    }

    # Обязательный выход: PLAN.md объявляет "Вуди неприменим" -- блокировки больше нет.
    Add-Content -LiteralPath (Join-Path $prod4 'PLAN.md') -Value "`nВуди неприменим: работа не имеет машинного судьи`n" -Encoding UTF8
    $r4b = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid4; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod4
    })
    Assert-That 'turn-guard.ps1: PLAN.md объявил "Вуди неприменим" -- петля больше не требуется' {
        -not ($r4b.Stderr -match 'steps\.jsonl')
    }

    # Со steps.jsonl требование снимается и без объявления неприменимости.
    Remove-Item -LiteralPath (Join-Path $prod4 'PLAN.md') -Force
    Set-Content -LiteralPath (Join-Path $prod4 'PLAN.md') -Value "- [ ] script  Шаг`n" -Encoding UTF8
    Set-Content -LiteralPath (Join-Path $prod4 'steps.jsonl') -Value '{}' -Encoding UTF8
    $r4c = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid4; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod4
    })
    Assert-That 'turn-guard.ps1: steps.jsonl появился -- требование снято' {
        -not ($r4c.Stderr -match 'steps\.jsonl')
    }
} finally {
    Remove-Item -LiteralPath $prod4 -Recurse -Force -ErrorAction SilentlyContinue
}

# --- Задача 5: частичный выход из режима без согласования запрещён -------------------
$sid5 = New-TestSessionId
$prod5 = Join-Path ([System.IO.Path]::GetTempPath()) ('woody-test-prod5-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $prod5 | Out-Null
Set-ModeFile $sid5 $true 0 | Out-Null
try {
    # Конфигурация есть, ключа ladder в ней нет -- лестницу сняли.
    Set-Content -LiteralPath (Join-Path $prod5 'run-config.json') -Value '{"judge":{"checks":[]}}' -Encoding UTF8
    $r5 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid5; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod5
    })
    Assert-That 'turn-guard.ps1: ladder снят из конфигурации без согласования -- ход не заканчивается' {
        ($r5.Code -eq 2) -and ($r5.Stderr -match 'сокращение режима равносильно выходу')
    }

    # Конфигурации нет вовсе -- пропускаем (сессия работает вне продукта).
    Remove-Item -LiteralPath (Join-Path $prod5 'run-config.json') -Force
    $r5b = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid5; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod5
    })
    Assert-That 'turn-guard.ps1: конфигурации нет вовсе -- частичный выход не проверяется' {
        $r5b.Code -eq 0
    }

    # Конфигурация вернулась без ladder, но теперь есть согласование ЛПР -- выход
    # проходит и согласование РАСХОДУЕТСЯ.
    Set-Content -LiteralPath (Join-Path $prod5 'run-config.json') -Value '{"judge":{"checks":[]}}' -Encoding UTF8
    $approval5 = Join-Path $stateDir "woody-mode-off-approved-$sid5.json"
    @{ session_id = $sid5 } | ConvertTo-Json | Set-Content -LiteralPath $approval5 -Encoding UTF8
    $r5c = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid5; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod5
    })
    Assert-That 'turn-guard.ps1: согласование ЛПР есть -- частичный выход больше не в недостачах' {
        -not ($r5c.Stderr -match 'сокращение режима равносильно выходу')
    }
    Assert-That 'turn-guard.ps1: согласование РАСХОДУЕТСЯ -- файл снят после использования' {
        -not (Test-Path -LiteralPath $approval5)
    }
    Assert-That 'turn-guard.ps1: после расхода согласования режим сессии выключен' {
        $body = Get-Content -LiteralPath (Join-Path $stateDir "woody-mode-$sid5.json") -Raw -Encoding UTF8 | ConvertFrom-Json
        -not $body.enabled
    }
} finally {
    Remove-Item -LiteralPath $prod5 -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-mode-off-approved-$sid5.json") -Force -ErrorAction SilentlyContinue
}

# --- Задача 3: судья зовётся отдельным процессом, вердикт NULL при таймауте/отсутствии -
$sid3 = New-TestSessionId
$prod3 = Join-Path ([System.IO.Path]::GetTempPath()) ('woody-test-prod3-' + [guid]::NewGuid().ToString('N').Substring(0, 8))
New-Item -ItemType Directory -Force -Path $prod3 | Out-Null
Set-ModeFile $sid3 $true 0 | Out-Null
$verdict3 = Join-Path $stateDir "woody-verdict-$sid3.json"
try {
    # Нет run-config.json -- судья не зовётся, файла вердикта не появляется.
    $rV0 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid3; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod3
    })
    Assert-That 'turn-guard.ps1: нет run-config.json -- судья не зовётся, вердикта нет' {
        (-not (Test-Path -LiteralPath $verdict3)) -and ($rV0.Code -eq 0)
    }

    # Конфигурация есть -- судья зовётся, вердикт пишется файлом с полями code/time.
    # ladder указан явно (и включает 'sonnet' -- $goodMsg называет эту ступень в строке
    # субагента), чтобы не зацепить ни задачу 5 (частичный выход), ни задачу 2 (ступень
    # вне лестницы) -- обе проверяются отдельными сценариями и здесь были бы посторонним
    # источником недостачи.
    Set-Content -LiteralPath (Join-Path $prod3 'run-config.json') -Value '{"judge":{"checks":[]},"ladder":["script","sonnet"]}' -Encoding UTF8
    $rV1 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid3; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod3
    })
    Assert-That 'turn-guard.ps1: конфигурация есть -- вердикт судьи записан файлом' {
        Test-Path -LiteralPath $verdict3
    }
    $body3 = $null
    if (Test-Path -LiteralPath $verdict3) { $body3 = Get-Content -LiteralPath $verdict3 -Raw -Encoding UTF8 | ConvertFrom-Json }
    Assert-That 'turn-guard.ps1: вердикт несёт время и корень продукта' {
        ($body3) -and ($body3.time) -and ($body3.product_root -eq $prod3)
    }
    Assert-That 'turn-guard.ps1: вердикт НЕ БЛОКИРУЕТ ход -- код возврата не зависит от судьи' {
        $rV1.Code -eq 0
    }

    # Повторный прогон РАНЬШЕ 60 секунд -- время в файле не должно поменяться.
    Start-Sleep -Milliseconds 200
    $timeBefore = $body3.time
    $rV2 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
        session_id = $sid3; hook_event_name = 'Stop'; last_assistant_message = $goodMsg; cwd = $prod3
    })
    $bodyAfter = Get-Content -LiteralPath $verdict3 -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-That 'turn-guard.ps1: судья не чаще раза в 60 секунд -- повторный прогон не тронул файл' {
        $bodyAfter.time -eq $timeBefore
    }
} finally {
    Remove-Item -LiteralPath $prod3 -Recurse -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $verdict3 -Force -ErrorAction SilentlyContinue
}

# ======================================================================================
# ЗАДАЧА 1 -- prompt-guard.ps1 (UserPromptSubmit): читает журнал сессии, не
# last_assistant_message, и НЕ БЛОКИРУЕТ сообщение -- несёт замечание additionalContext.
# ======================================================================================
$promptGuardPath = Join-Path $skillRoot 'hooks\prompt-guard.ps1'

function New-FakeTranscript([string]$path, [string]$assistantText) {
    $lines = @(
        (@{ type = 'user'; message = @{ role = 'user'; content = 'привет' }; cwd = 'C:\fake'; sessionId = 'x' } | ConvertTo-Json -Depth 5 -Compress),
        (@{ type = 'assistant'; message = @{ role = 'assistant'; content = @(@{ type = 'text'; text = $assistantText }) }; cwd = 'C:\fake'; sessionId = 'x' } | ConvertTo-Json -Depth 5 -Compress)
    )
    Set-Content -LiteralPath $path -Value ($lines -join "`n") -Encoding UTF8
}

$sidP1 = New-TestSessionId
Set-ModeFile $sidP1 $true 0 | Out-Null
$transcriptGood = [System.IO.Path]::GetTempFileName()
try {
    New-FakeTranscript $transcriptGood $goodMsg
    $rP1 = Invoke-HookStdin $promptGuardPath (New-EventJson @{
        session_id = $sidP1; hook_event_name = 'UserPromptSubmit'; transcript_path = $transcriptGood
    })
    Assert-That 'prompt-guard.ps1: последнее сообщение в журнале полное -- код 0, без замечания' {
        ($rP1.Code -eq 0) -and ($rP1.Stdout -notmatch 'additionalContext')
    }
} finally { Remove-Item -LiteralPath $transcriptGood -Force -ErrorAction SilentlyContinue }

$sidP2 = New-TestSessionId
Set-ModeFile $sidP2 $true 0 | Out-Null
$transcriptBad = [System.IO.Path]::GetTempFileName()
try {
    $interrupted = 'Фаза 1 · Тест · ступень sonnet · ведущая · раздаю'   # ход прерван на первой строке
    New-FakeTranscript $transcriptBad $interrupted
    $rP2 = Invoke-HookStdin $promptGuardPath (New-EventJson @{
        session_id = $sidP2; hook_event_name = 'UserPromptSubmit'; transcript_path = $transcriptBad
    })
    Assert-That 'prompt-guard.ps1: прерванный ход в журнале -- замечание additionalContext, но код 0 (не блокирует ЛПР)' {
        ($rP2.Code -eq 0) -and ($rP2.Stdout -match 'additionalContext') -and ($rP2.Stdout -match 'ПРЕРВАН')
    }
} finally { Remove-Item -LiteralPath $transcriptBad -Force -ErrorAction SilentlyContinue }

Assert-That 'prompt-guard.ps1: пишет снимок stdin в woody-prompt-probe-<session_id>.json' {
    $probe = Join-Path $stateDir "woody-prompt-probe-$sidP2.json"
    Register-Cleanup $probe
    Test-Path -LiteralPath $probe
}

# ======================================================================================
# УЧЁТ mode.ps1: -Track, снятие мёртвой отцепленной ветви, -Reconcile, -ReconcileNone.
# ======================================================================================
$modeScript = Join-Path $skillRoot 'hooks\mode.ps1'

function Invoke-Mode([string]$sessionKey, [string[]]$modeArgs) {
    $env:CLAUDE_CODE_SESSION_ID = $sessionKey
    $out = & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $modeScript @modeArgs 2>&1 | Out-String
    return [pscustomobject]@{ Code = $LASTEXITCODE; Text = $out }
}

$sidM = New-TestSessionId
try {
    $onResult = Invoke-Mode $sidM @('-On', '-Subagents', '0')
    Register-Cleanup (Join-Path $stateDir "woody-mode-$sidM.json")
    Assert-That 'mode.ps1 -On: включение проходит на реальном скиле (команды хуков существуют)' {
        $onResult.Code -eq 0
    }

    $trackResult = Invoke-Mode $sidM @('-Track', '999999', '-TrackTitle', 'тест', '-TrackTier', 'sonnet')
    $agentsPath = Join-Path $stateDir "woody-agents-$sidM.json"
    Register-Cleanup $agentsPath
    Assert-That '-Track: отцепленная ветвь взята в учёт по названному id' {
        ($trackResult.Code -eq 0) -and (Test-Path -LiteralPath $agentsPath) -and
        ((Get-Content -LiteralPath $agentsPath -Raw -Encoding UTF8 | ConvertFrom-Json).running.id -contains '999999')
    }

    # PID 999999 почти наверняка не существует -- -Status обязан снять её как мёртвую.
    $statusResult = Invoke-Mode $sidM @('-Status')
    $afterStatus = Get-Content -LiteralPath $agentsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-That '-Status: мёртвая отцепленная ветвь (несуществующий PID) снята из учёта' {
        (@($afterStatus.running) | Where-Object { [string]$_.id -eq '999999' }).Count -eq 0
    }

    # -Reconcile с живым списком, где реального агента нет вовсе -- всё снимается.
    $null = Invoke-Mode $sidM @('-Track', 'agent-a', '-TrackTitle', 'A', '-TrackTier', 'sonnet')
    $null = Invoke-Mode $sidM @('-Track', 'agent-b', '-TrackTitle', 'B', '-TrackTier', 'sonnet')
    $reconcileResult = Invoke-Mode $sidM @('-Reconcile', 'agent-a')
    $afterReconcile = Get-Content -LiteralPath $agentsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-That '-Reconcile <живые>: снимает всё, чего нет в переданном списке' {
        (@($afterReconcile.running).Count -eq 1) -and ($afterReconcile.running[0].id -eq 'agent-a')
    }

    # -ReconcileNone снимает ВСЁ, включая agent-a, оставшегося после прошлой сверки.
    $reconcileNoneResult = Invoke-Mode $sidM @('-ReconcileNone')
    $afterNone = Get-Content -LiteralPath $agentsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-That '-ReconcileNone: снимает всех живых' {
        @($afterNone.running).Count -eq 0
    }

    # Отличие -ReconcileNone от НЕПЕРЕДАННОГО -Reconcile: без ключа сверка (-Reconcile/
    # -ReconcileNone) не запускается вовсе. Трекается PID ЭТОГО тестового процесса, а не
    # символический id: -Status отдельно от сверки снимает 'detached' записи по РЕАЛЬНОЙ
    # живости процесса (см. hooks/mode.ps1) -- символический id там всегда читался бы как
    # мёртвый (TryParse на нечисловой строке проваливается) и смешал бы два разных
    # механизма в одной проверке.
    $ownPid = [string]$PID
    $null = Invoke-Mode $sidM @('-Track', $ownPid, '-TrackTitle', 'C', '-TrackTier', 'sonnet')
    $beforeNoop = Get-Content -LiteralPath $agentsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    $noopResult = Invoke-Mode $sidM @('-Status')
    $afterNoop = Get-Content -LiteralPath $agentsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-That '-Reconcile непереданный (просто -Status) не снимает живых -- в отличие от -ReconcileNone' {
        @($afterNoop.running | Where-Object { [string]$_.id -eq $ownPid }).Count -eq 1
    }

    $forgetResult = Invoke-Mode $sidM @('-Forget', $ownPid)
    $afterForget = Get-Content -LiteralPath $agentsPath -Raw -Encoding UTF8 | ConvertFrom-Json
    Assert-That '-Forget: снимает названную запись поимённо' {
        ($forgetResult.Code -eq 0) -and (@($afterForget.running).Count -eq 0)
    }
} finally {
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-mode-$sidM.json") -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-agents-$sidM.json") -Force -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath (Join-Path $stateDir "woody-session-probe-$sidM.json") -Force -ErrorAction SilentlyContinue
}

} finally {
    # Зачистка: тестовые файлы состояния, ни один — файл живой сессии на машине.
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
