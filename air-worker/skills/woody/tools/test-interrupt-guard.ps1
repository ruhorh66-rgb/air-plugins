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
# СТРАЖ ХОДА: СВЕРКА ЧИСЛА С ЧИСЛОМ.
#
# На этом месте было 238 строк проверок надзора за формой сообщения: семь обязательных
# строк, ступень в строке субагента, частичный выход из режима, судья внутри стража и
# prompt-guard вторым рубежом. Всё это снято 13.09.2026 вместе с самим надзором — разбор
# показал, что 4 264 строки надзора за речью не улучшили за сутки ни одного продукта,
# а лечением каждой их блокировки была переписанная модель абзаца.
#
# Проверки ниже — те же четыре случая, которыми страж был проверен вручную на живом
# продукте, но записанные прогоном. Каждая сверяет ЧИСЛО, а не форму: подставное дерево
# и подставное расстояние обязаны быть пойманы, верный отчёт обязан пройти молча.
# ======================================================================================
$turnGuardPath = Join-Path $skillRoot 'hooks\turn-guard.ps1'

function New-GuardProduct {
    # Настоящий каталог с настоящим git: страж считает изменённые файлы через
    # `git status --porcelain`, и подставить их файлом-заглушкой нельзя — в этом и смысл.
    $dir = Join-Path ([System.IO.Path]::GetTempPath()) ("woody-guard-" + [guid]::NewGuid().ToString('N').Substring(0, 8))
    New-Item -ItemType Directory -Force -Path $dir | Out-Null
    & git -C $dir init -q 2>&1 | Out-Null
    Set-Content -LiteralPath (Join-Path $dir 'README.md') -Value 'проба' -Encoding UTF8
    return $dir
}

function Set-GuardProductBinding([string]$sessionKey, [string]$path) {
    $f = Join-Path $stateDir "woody-product-$sessionKey.json"
    @{ path = $path; declared_at = (Get-Date).ToString('s') } | ConvertTo-Json |
        Set-Content -LiteralPath $f -Encoding UTF8
    Register-Cleanup $f
    Register-Cleanup (Join-Path $stateDir "woody-turn-$sessionKey.json")
}

$guardProd = New-GuardProduct
try {
    $exe = Join-Path (Split-Path -Parent (Split-Path -Parent $skillRoot)) 'bin\air-worker.exe'
    if (-not (Test-Path -LiteralPath $exe)) {
        # Нет бинарника — нечем мерить. Это НЕ «проверка прошла»: код 2 у судьи означает
        # «проверять нечем», и здесь ровно тот же случай.
        Write-Host '[SKIP] bin\air-worker.exe не найден: страж хода не проверялся' -ForegroundColor Yellow
    }
    else {
        $driftJson = & $exe drift -product $guardProd -json 2>$null | Out-String
        $real = $null
        try { $real = $driftJson | ConvertFrom-Json } catch { }

        $treeN = @((& git -C $guardProd status --porcelain 2>$null) | Where-Object { $_.Trim() -ne '' }).Count
        $realDist = if ($null -eq $real.distance) { 'нечем измерить' } else { [string]$real.distance }

        # -- случай 1: отчёта в ходе нет вовсе -------------------------------------
        $sidG1 = New-TestSessionId
        Set-GuardProductBinding $sidG1 $guardProd
        $rG1 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
            session_id = $sidG1; hook_event_name = 'Stop'
            last_assistant_message = 'Сделал работу, всё хорошо.'
        })
        Assert-That 'turn-guard: ход без отчёта -- код 2, названа команда замера' {
            ($rG1.Code -eq 2) -and ($rG1.Stderr -match 'ХОД БЕЗ ЗАМЕРА') -and ($rG1.Stderr -match 'air-worker report')
        }

        # -- случай 2: отчёт совпадает с замером ------------------------------------
        $good = "Расстояние : $realDist · застой $($real.stall_moves) · вердикт $($real.verdict)`n" +
                "Дерево     : изменено файлов $treeN, из них новых 0"
        $sidG2 = New-TestSessionId
        Set-GuardProductBinding $sidG2 $guardProd
        $rG2 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
            session_id = $sidG2; hook_event_name = 'Stop'; last_assistant_message = $good
        })
        Assert-That 'turn-guard: числа сошлись -- код 0, страж молчит' {
            ($rG2.Code -eq 0) -and (-not ($rG2.Stderr -match 'НЕ СОВПАЛИ'))
        }

        # -- случай 3: расстояние подменено ----------------------------------------
        $fakeDist = if ($realDist -eq '1') { '2' } else { '1' }
        $badDist = "Расстояние : $fakeDist · застой $($real.stall_moves) · вердикт $($real.verdict)`n" +
                   "Дерево     : изменено файлов $treeN, из них новых 0"
        $sidG3 = New-TestSessionId
        Set-GuardProductBinding $sidG3 $guardProd
        $rG3 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
            session_id = $sidG3; hook_event_name = 'Stop'; last_assistant_message = $badDist
        })
        Assert-That 'turn-guard: подменённое расстояние -- код 2, названы ОБА числа' {
            ($rG3.Code -eq 2) -and ($rG3.Stderr -match 'расстояние') -and
            ($rG3.Stderr -match [regex]::Escape($fakeDist)) -and ($rG3.Stderr -match [regex]::Escape($realDist))
        }

        # -- случай 4: дерево подменено ---------------------------------------------
        $badTree = "Расстояние : $realDist · застой $($real.stall_moves) · вердикт $($real.verdict)`n" +
                   "Дерево     : изменено файлов 99, из них новых 0"
        $sidG4 = New-TestSessionId
        Set-GuardProductBinding $sidG4 $guardProd
        $rG4 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
            session_id = $sidG4; hook_event_name = 'Stop'; last_assistant_message = $badTree
        })
        Assert-That 'turn-guard: подменённое число изменённых файлов -- код 2' {
            ($rG4.Code -eq 2) -and ($rG4.Stderr -match 'изменённых файлов') -and ($rG4.Stderr -match '99')
        }

        # -- случай 5: продукт НЕ объявлен -- страж молчит ---------------------------
        # Правило безопасности, а не удобство: угадывание продукта по рабочему каталогу
        # один раз уже поймало каталог, мимо которого сессия проходила.
        $sidG5 = New-TestSessionId
        $rG5 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
            session_id = $sidG5; hook_event_name = 'Stop'; last_assistant_message = 'Без отчёта и без объявленного продукта.'
        })
        Assert-That 'turn-guard: продукт не объявлен -- код 0, страж не вмешивается' {
            $rG5.Code -eq 0
        }

        # -- случай 6: по продукту идёт петля -- страж ход не сверяет ----------------
        # Найдено 14.09.2026: петля в фоне правит дерево и переписывает вердикт, и отчёт,
        # снятый минуту назад, расходится с диском не потому, что ход врёт. Замок петли
        # берётся тем же именем, что у бинарника: sha1 нормализованного пути продукта.
        $norm = [IO.Path]::GetFullPath($guardProd).TrimEnd('\').ToLowerInvariant()
        $sha = [Security.Cryptography.SHA1]::Create().ComputeHash([Text.Encoding]::UTF8.GetBytes($norm))
        $loopMutexName = 'Local\air-worker-loop-' + (-join ($sha[0..7] | ForEach-Object { $_.ToString('x2') }))
        $loopMutex = [Threading.Mutex]::new($true, $loopMutexName)
        $savedExe = $env:AIR_WORKER_EXE
        try {
            $env:AIR_WORKER_EXE = $exe
            $loopSeen = $false
            try { $loopSeen = [bool]((& $exe drift -product $guardProd -json 2>$null | Out-String | ConvertFrom-Json).loop_running) } catch { }
            $sidG6 = New-TestSessionId
            Set-GuardProductBinding $sidG6 $guardProd
            $rG6 = Invoke-HookStdin $turnGuardPath (New-EventJson @{
                session_id = $sidG6; hook_event_name = 'Stop'; last_assistant_message = 'Петля идёт, отчёта в ходе нет.'
            })
            Assert-That 'turn-guard: по продукту идёт петля -- drift видит замок, ход без отчёта проходит кодом 0' {
                $loopSeen -and ($rG6.Code -eq 0)
            }
        }
        finally {
            $env:AIR_WORKER_EXE = $savedExe
            try { $loopMutex.ReleaseMutex() } catch { }
            $loopMutex.Dispose()
        }
    }
}
finally {
    Remove-Item -LiteralPath $guardProd -Recurse -Force -ErrorAction SilentlyContinue
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
