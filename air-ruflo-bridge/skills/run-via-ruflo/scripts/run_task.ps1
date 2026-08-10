# run_task.ps1 — одно окно, один рой.
#
# ПЕРЕПИСАН 05.08.2026 по решению ЛПР: контур не пишет свою оркестрацию поверх
# ruflo (swarm_init/task_create вручную) — используется КАНОНИЧЕСКАЯ команда
# самого движка, `hive-mind spawn --claude -o "<цель>"`. Наша обвязка — это
# только: (1) один и тот же рой для контура, (2) гейт человека перед реальным
# запуском, (3) сканер секретов после.
#
# ОДНО ОКНО. Рой всегда живёт и запускается из ОДНОГО канонического каталога
# ($ProjectRoot). У `hive-mind spawn` нет флага «выполнить в другом каталоге» —
# состояние роя жёстко привязано к cwd вызова. Поэтому реальная работа (другой
# путь, другой проект) не переносит рой физически — целевой путь дописывается
# ТЕКСТОМ в цель, и запущенная Claude Code сессия сама переходит туда первым
# действием. Один рой — один живой процесс — одна очередь целей, как и просил ЛПР.
[CmdletBinding()]
param(
    # Цель можно передать текстом ($Objective) либо ФАЙЛОМ ($ObjectiveFile).
    # Файловый вариант заведён 08.08.2026 ради вызова из слушателя Telegram:
    # раньше тот собирал `-Objective (Get-Content "<путь>" -Raw)` строкой для
    # `powershell -Command`, то есть склеивал команду из значений заявки — и это
    # был реальный вектор инъекции (codex review, blocker). С -ObjectiveFile
    # вызывающий передаёт ПУТЬ отдельным аргументом через -File, склейки нет.
    [Parameter()][string]$Objective,
    [Parameter()][string]$ObjectiveFile,
    # Абсолютный путь, где реально лежит код/данные задачи. Рой стоит в
    # $ProjectRoot, но работает здесь — путь вписывается в текст цели.
    [Parameter(Mandatory)][string]$TargetPath,
    [Parameter()][string]$ProjectRoot = 'E:\-4-\ruflo-hive',
    [Parameter()][switch]$AllowForeignHive,
    [Parameter(Mandatory)][string]$CliPath,
    [Parameter(Mandatory)][string]$ReportPath,
    [Parameter()][ValidateSet('I_APPROVE_RUFLO_PLAN')][string]$Approval,
    # Сколько воркеров завести под задачу. ДО 08.08.2026 не передавалось вовсе —
    # движок брал свой дефолт 1 (`-n, --count ... [default: 1]`), поэтому каждый
    # прогон контура шёл ОДНИМ воркером, а 18 накопленных сидели idle с нулём
    # выполненных задач. Значение 5 — из официального примера самого движка
    # (`claude-flow hive-mind spawn -n 5`), не выдумано нами.
    [Parameter()][int]$Workers = 5,
    [Parameter()][ValidateSet('normal','high','critical')][string]$Priority = 'high',
    # Идентификатор строки очереди, под которую идёт прогон. Необязателен: ручной
    # прогон очереди не касается. Уходит В ЗАМОК — по нему тот, кто замок взял,
    # чинит пережившие отметки ЧУЖИХ строк, не трогая свою (`ruflo_queue reconcile`).
    [Parameter()][string]$TaskId = '',
    # autopilot держит агентов в работе, пока не закрыты ВСЕ задачи
    # ("Persistent swarm completion"). Выключается осознанно, не по умолчанию.
    [Parameter()][switch]$NoAutopilot
)

$ErrorActionPreference = 'Stop'

if ($ObjectiveFile) {
    if ($Objective) { throw "Указывать одновременно -Objective и -ObjectiveFile нельзя" }
    if (-not (Test-Path -LiteralPath $ObjectiveFile)) { throw "ObjectiveFile not found: $ObjectiveFile" }
    $Objective = Get-Content -LiteralPath $ObjectiveFile -Raw -Encoding UTF8
}
if (-not $Objective) { throw "Нужен -Objective или -ObjectiveFile" }

# Движок проверяет доступность Claude Code вызовом `execSync('which claude')`
# (dist/src/commands/hive-mind.js) — ЮНИКСОВЫЙ `which`, которого в Windows нет
# по умолчанию. Проверено 06.08.2026 дважды: ни prepend каталога с claude.exe,
# ни что-либо ещё не помогает, пока самого `which` нет в PATH — движок падает
# в тихую деградацию ("Falling back to displaying instructions"), exit 0,
# реальная сессия не запускается, а отчёт без отдельной проверки врёт "EXECUTED".
# Рабочий рецепт — из проверенного `codex-shim`-шаблона: Git for Windows везёt
# свой which.exe в usr\bin, плюс каталог с настоящим claude.exe для самого
# исполнения (не только для проверки).
$GitBin      = 'C:\Program Files\Git\usr\bin'
$ClaudeExeDir = 'C:\Users\admin_loc\AppData\Roaming\npm\node_modules\@anthropic-ai\claude-code\bin'
if ((Test-Path -LiteralPath (Join-Path $GitBin 'which.exe')) -and (Test-Path -LiteralPath (Join-Path $ClaudeExeDir 'claude.exe'))) {
    $env:Path = "$GitBin;$ClaudeExeDir;$env:Path"
} else {
    Write-Warning "which.exe ($GitBin) или claude.exe ($ClaudeExeDir) не найдены — реальный запуск, скорее всего, откажет так же, как раньше"
}

if (-not (Test-Path -LiteralPath $CliPath)) { throw "CLI not found: $CliPath" }
if (-not (Test-Path -LiteralPath $TargetPath)) { throw "TargetPath not found: $TargetPath" }
$CanonicalHive = 'E:\-4-\ruflo-hive'
if (-not (Test-Path -LiteralPath $ProjectRoot)) { New-Item -ItemType Directory -Path $ProjectRoot -Force | Out-Null }
$ProjectRoot = (Resolve-Path -LiteralPath $ProjectRoot).Path
$TargetPath  = (Resolve-Path -LiteralPath $TargetPath).Path

# ЖЁСТКИЙ ЗАПРЕТ — не отменён этой правкой, один рой остаётся один.
if ($ProjectRoot -ne $CanonicalHive -and -not $AllowForeignHive) {
    [pscustomobject]@{
        outcome='refused'; reason='рой заводится только в каноническом корне'
        requested=$ProjectRoot; canonical=$CanonicalHive
        advice='убрать -ProjectRoot, либо осознанно передать -AllowForeignHive'
    } | ConvertTo-Json -Compress
    exit 5
}

$reportDirectory = Split-Path -Parent $ReportPath
if ($reportDirectory -and -not (Test-Path -LiteralPath $reportDirectory)) { New-Item -ItemType Directory -Path $reportDirectory -Force | Out-Null }
$transcript = [System.Collections.Generic.List[string]]::new()

# --- Замок: ОДИН на контур, и он же единственный источник факта «рой занят» ----
#
# КОРЕНЬ ЗАМКА — НЕ $ProjectRoot. Раньше замок брался в -Root $ProjectRoot, а
# `ruflo_queue.py` спрашивал его в своём REPORTS (E:\-4-\ruflo-hive). Пока
# $ProjectRoot совпадал с каноническим, разницы не было видно; с
# -AllowForeignHive это ДВА РАЗНЫХ ФАЙЛА, то есть два независимых замка, и
# «единый источник» перестаёт существовать ровно в том сценарии, ради которого
# флаг и заведён. Берём один корень с очередью и подменяем его той же
# переменной окружения — иначе полигон снова разъедет обе стороны.
$LockRoot = if ($env:RUFLO_REPORTS) { $env:RUFLO_REPORTS } else { $CanonicalHive }
$hiveSingle = Join-Path $PSScriptRoot 'hive_single.ps1'
if (Test-Path -LiteralPath $hiveSingle) {
    $claim = & $hiveSingle -Action claim -Root $LockRoot -Owner "run_task-$PID" -TaskId $TaskId 2>$null
    # Живой держатель (4) и нечитаемый замок (5) — оба отказ, и оба возвращаются
    # вызывающему как есть: он должен видеть, ЧТО именно случилось, а не общий «1».
    if ($LASTEXITCODE -ne 0) { $claim; exit $LASTEXITCODE }
    $transcript.Add("## Замок роя`n~~~json`n$claim`n~~~")

    # ПОЧИНКА СТРОК — ПРАВО ТОГО, КТО ВЗЯЛ ЗАМОК, и только его. Отметка `approved`,
    # пережившая свой прогон (падение между взятием замка и записью строки),
    # чинится здесь: замок только что взят нами, значит рой этими строками не занят.
    # Раньше это делал любой, кто заглянул в очередь, — и снимал отметку с прогона,
    # который в этот момент шёл.
    $queueScript = Join-Path $PSScriptRoot 'ruflo_queue.py'
    $python = (Get-Command python -ErrorAction SilentlyContinue).Source
    if ($python -and (Test-Path -LiteralPath $queueScript)) {
        try {
            $rec = & $python $queueScript reconcile $PID 2>&1 | Out-String
            $transcript.Add("## Очередь: reconcile`n~~~text`n$rec`n~~~")
        } catch {
            # Очередь недоступна — прогон из-за этого не отменяем: замок уже наш,
            # и занятость роя от строки таблицы не зависит.
            $transcript.Add("## Очередь: reconcile НЕ ВЫПОЛНЕН`n$($_.Exception.Message)")
        }
    }
}

# --- Дефект отчёта, найден и закрыт 08.08.2026 ------------------------------
# Симптом: реальный прогон упал на четвёртом шаге, а отчёт оборвался на третьем —
# причину (протухший OAuth в Claude Code) пришлось искать руками, повторяя вызов
# в консоли. Корень двойной:
#   1) $ErrorActionPreference = 'Stop': когда node пишет в stderr, PowerShell
#      бросает исключение ДО присваивания переменной, и вывод шага теряется целиком;
#   2) транскрипт писался в файл лишь на некоторых ветках, а не на всех.
# Лечится здесь: вызовы движка идут через Invoke-Ruflo (вывод и код возвращаются
# всегда, исключение не бросается), а запись отчёта вынесена в Save-Report и
# вызывается в finally безусловно.
function Invoke-Ruflo {
    param([Parameter(Mandatory)][string[]]$Arguments, [Parameter(Mandatory)][string]$Stage)
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try {
        $out = & node $CliPath @Arguments 2>&1 | Out-String
        $code = $LASTEXITCODE
    } finally {
        $ErrorActionPreference = $prev
    }
    $transcript.Add("## $Stage`n~~~text`n$out`n~~~`nexit: $code")
    [pscustomobject]@{ Output = $out; ExitCode = $code }
}

$script:reportSaved = $false
function Save-Report {
    try {
        $transcript | Set-Content -LiteralPath $ReportPath -Encoding utf8
        $script:reportSaved = $true
    } catch {
        Write-Warning "не удалось записать отчёт $ReportPath : $($_.Exception.Message)"
    }
}

Push-Location $ProjectRoot
try {
    # hive-mind init один раз на канонический рой — идемпотентно по факту наличия .hive-mind
    if (-not (Test-Path -LiteralPath (Join-Path $ProjectRoot '.hive-mind'))) {
        $initOut = & node $CliPath hive-mind init -t hierarchical-mesh 2>&1 | Out-String
        $transcript.Add("## hive-mind init`n~~~text`n$initOut`n~~~")
        if ($LASTEXITCODE -ne 0) { throw "hive-mind init failed ($LASTEXITCODE)" }
    }

    # Проверка регистрации MCP — ОДИН РАЗ на канонический каталог (не на TargetPath:
    # рой всегда запускается из $ProjectRoot, поэтому регистрация нужна только здесь).
    # Без неё `--claude` тихо запускает обычную дорогую сессию БЕЗ mcp__claude-flow__*
    # инструментов — задокументированный отказ (005_Ruflo_Wiki, Пилот 04.08.2026).
    $mcpConfigured = $false
    $mcpJson = Join-Path $ProjectRoot '.mcp.json'
    if (Test-Path -LiteralPath $mcpJson) {
        try {
            $cfg = Get-Content -LiteralPath $mcpJson -Raw | ConvertFrom-Json
            $mcpConfigured = [bool]($cfg.mcpServers.'claude-flow')
        } catch {}
    }
    if (-not $mcpConfigured) {
        $transcript.Add("## MCP registration`nНЕ НАЙДЕНА в $mcpJson. Без неё --claude запустит сессию без инструментов роя.")
    }

    # Цель роя = абсолютный путь работы + ТРЕБОВАНИЕ ДЕЛЕГИРОВАТЬ + сама задача.
    # Рой стоит в $ProjectRoot, спавненная сессия обязана сама перейти в $TargetPath.
    #
    # Требование делегирования дописывается ОБВЯЗКОЙ, а не автором цели
    # (ERR-2026-000194). Прогон 08.08.2026 прошёл все четыре шага, дал результат —
    # и при этом Queen не сделала НИ ОДНОГО вызова mcp__claude-flow__*, хотя все
    # 334 инструмента роя были ей доступны: сделала всё сама (Edit 45, Bash 31).
    # Причина: цель перечисляла ЧТО сделать, но не требовала РАСПРЕДЕЛИТЬ. Пока
    # это требование живёт в голове у автора цели, оно будет забываться — здесь
    # оно добавляется к каждой цели механически.
    $delegation = "КАК РАБОТАТЬ (обязательно, это рой, а не одиночная сессия): " +
        "тебе доступны инструменты mcp__claude-flow__* — используй их. Разбей задачу " +
        "на подзадачи и раздай воркерам через mcp__claude-flow__agent_spawn и " +
        "mcp__claude-flow__task_assign, координируй через mcp__claude-flow__ " +
        "(memory_store/search для общего контекста, swarm_status для контроля). " +
        "Делать всю работу самой обычными Edit/Bash/Write вместо распределения — " +
        "прямое нарушение постановки: приёмка прогона включает проверку лога на " +
        "фактические вызовы mcp__claude-flow__, и прогон без них считается " +
        "несостоявшимся независимо от того, получен ли предметный результат. " +
        "Спавни воркеров под РОЛИ задачи (coder, tester, reviewer, architect, " +
        "researcher), а не безымянных worker."
    $fullObjective = "РАБОЧИЙ КАТАЛОГ ЗАДАЧИ: $TargetPath — перейди туда (cd) или используй " +
        "только абсолютные пути на каждую файловую/shell операцию. Эта координирующая сессия " +
        "запущена из $ProjectRoot (канонический рой контура) — файлы этого каталога НЕ ТРОГАТЬ, " +
        "он не относится к задаче. $delegation ЗАДАЧА: $Objective"

    $dry = Invoke-Ruflo -Arguments @('hive-mind','spawn','--claude','-o',$fullObjective,'--dry-run') -Stage 'Dry run'
    $planned = "ЧТО БУДЕТ ЗАПУЩЕНО ПРИ -Approval (четыре шага, не один):`n" +
        "  1. hive-mind spawn -n $Workers        — завести $Workers воркеров под задачу`n" +
        "  2. hive-mind task -d <цель> -p $Priority   — положить задачу в очередь роя`n" +
        "  3. autopilot enable                   — $(if ($NoAutopilot) {'ОТКЛЮЧЕНО флагом -NoAutopilot'} else {'держать агентов до закрытия ВСЕХ задач'})`n" +
        "  4. hive-mind spawn --claude -o <цель> — Queen-сессия, раздаёт работу через MCP`n"
    $transcript.Insert(0, "# Ruflo proposal (canonical hive-mind spawn)`n`nTargetPath: $TargetPath`nProjectRoot: $ProjectRoot`nMCP registered: $mcpConfigured`n`n$planned")
    if ($dry.ExitCode -ne 0) { throw "hive-mind spawn --dry-run failed ($($dry.ExitCode))" }

    if (-not $Approval) {
        $transcript.Add("## Human gate`nSTOPPED: proposal above must be shown to a human. Re-run only with -Approval I_APPROVE_RUFLO_PLAN. Claude Code was NOT launched.")
        Save-Report
        Write-Output "STOPPED FOR HUMAN APPROVAL. Report: $ReportPath"
        exit 10
    }

    if (-not $mcpConfigured) {
        $transcript.Add("## Refused`nMCP не зарегистрирован для $ProjectRoot — реальный запуск дал бы одну дорогую сессию без роя, не запускаю. Разово: claude mcp add -s project claude-flow -- node `"$CliPath`" mcp start (из $ProjectRoot), затем одобрить в ~/.claude.json.")
        Save-Report
        Write-Output "REFUSED: MCP not registered for $ProjectRoot. Report: $ReportPath"
        exit 6
    }

    # --- Реальный запуск: ТРИ шага, а не один -------------------------------
    # Найдено 08.08.2026 разбором справки самого движка после указания ЛПР
    # «не придумывать, смотреть первоисточник» (ERR-2026-000192). До этой правки
    # вызывался ТОЛЬКО третий шаг, поэтому: воркеров всегда 1 (дефолт движка),
    # очередь задач роя пустая, координатору некого координировать и нечего
    # раздавать. Статистика подтвердила: 18 воркеров, все idle, Completed=0.
    #
    #   1) spawn -n N     — завести воркеров под задачу
    #   2) task -d "цель" — положить задачу В ОЧЕРЕДЬ РОЯ (`Submit tasks to the hive`)
    #   3) spawn --claude — поднять Queen-сессию, которая раздаёт работу через MCP

    $r1 = Invoke-Ruflo -Arguments @('hive-mind','spawn','-n',"$Workers") -Stage "1. Spawn workers (-n $Workers)"
    if ($r1.ExitCode -ne 0) { throw "hive-mind spawn -n $Workers failed ($($r1.ExitCode))" }

    # ОЧЕРЕДИ РОЯ — КОРОТКАЯ строка, Queen-сессии — полная. Это разные адресаты.
    #
    # Установлено разбором 08.08.2026 (ERR-2026-000214, вторая, верная итерация).
    # `task_create` в движке валидирует вход ПЕРВЫМ делом:
    #     validateText(input.description, 'description')   // maxLen = 10 000
    # и при отказе возвращает { success:false, error } — объект БЕЗ `assignedTo`.
    # Печать результата затем читает `result.assignedTo.length` и падает. То есть
    # «TypeError: Cannot read properties of undefined» означает не дефект движка,
    # а НАШУ слишком длинную строку: 9 129 знаков задания плюс преамбула обвязки.
    #
    # Прошлый удачный прогон (TASK-OBS-0040, 6 753 знака) укладывался — потому и
    # выглядело, будто дело в приоритете. Это была ложная связь.
    #
    # Очереди достаточно опознавательной строки: полное задание всё равно получает
    # Queen-сессия шагом 4, и оно же лежит файлом, который она читает.
    $queueLine = "$($TargetPath): " + ($Objective -replace '\s+', ' ')
    if ($queueLine.Length -gt 9000) { $queueLine = $queueLine.Substring(0, 9000) + ' […полное задание — шаг 4 и файл цели]' }
    $r2 = Invoke-Ruflo -Arguments @('hive-mind','task','-d',$queueLine,'-p',$Priority) -Stage "2. Submit task to hive queue (-p $Priority)"
    if ($r2.ExitCode -ne 0) { throw "hive-mind task failed ($($r2.ExitCode))" }

    if (-not $NoAutopilot) {
        $r3 = Invoke-Ruflo -Arguments @('autopilot','enable') -Stage "3. Autopilot (persistent completion)"
        if ($r3.ExitCode -ne 0) { $transcript.Add("ВНИМАНИЕ: autopilot enable вернул $($r3.ExitCode) — рой не будет держать задачи до полного закрытия.") }
    }

    # Queen-сессия. Наш внешний гейт (dry-run -> явное решение человека) уже
    # сыграл роль подтверждения — поэтому дефолт движка (--dangerously-skip-permissions)
    # НЕ отключается: отключить его здесь значило бы, что headless-сессия молча
    # виснет на первом же внутреннем запросе разрешения, которого некому одобрить.
    # --non-interactive обязателен: запуск идёт из скрипта, TTY нет, и без флага
    # сессия может встать на внутреннем интерактивном промпте, которого некому
    # ответить. Проверено 08.08.2026 — с флагом exit 0 и результат возвращается.
    $run = Invoke-Ruflo -Arguments @('hive-mind','spawn','--claude','-o',$fullObjective,'--non-interactive') -Stage '4. Execution (Queen session)'
    $runOut = $run.Output
    $transcript.Add("Проверять реальность роя — по логу (`"name`":`"mcp__claude-flow__`") и по росту Completed в hive-mind status, не по самоотчёту сессии.")

    # НЕ верить exit-коду и факту "команда отработала" — движок сам может тихо
    # не запустить Claude Code и просто напечатать инструкции, при этом exit
    # остаётся 0. Проверено 06.08.2026: отчёт писал "EXECUTED" при реально
    # несостоявшемся запуске (см. runOut ниже). Ищем маркер деградации явно.
    if ($runOut -match 'Claude Code CLI not found' -or $runOut -match 'Falling back to displaying instructions') {
        $transcript.Add("## Execution FAILED (silent degradation)`nДвижок не нашёл claude.exe и не запустил сессию — рой НЕ РАБОТАЛ, несмотря на то что процесс сам завершился без ошибки. См. вывод выше.")
        Save-Report
        Write-Output "EXECUTION FAILED (Claude Code was not actually launched). Report: $ReportPath"
        exit 7
    }
    # Отдельно — отказ авторизации. Проверено 08.08.2026: истёкшая OAuth-сессия
    # Claude Code роняет ЧЕТВЁРТЫЙ шаг с кодом 1, при этом первые три проходят
    # штатно, и без явного разбора это выглядит как «рой не поехал непонятно почему».
    if ($runOut -match 'OAuth session expired' -or $runOut -match 'authentication_failed' -or $runOut -match 'Failed to authenticate') {
        $transcript.Add("## Execution FAILED (authentication)`nУ Claude Code истекла авторизация — Queen-сессия не поднялась. Шаги 1–3 (воркеры, задача в очереди, autopilot) при этом ОТРАБОТАЛИ, состояние роя сохранено. Лечится в обычном PowerShell: claude auth login --claudeai, затем повторить запуск.")
        Save-Report
        Write-Output "EXECUTION FAILED (Claude Code auth expired — run: claude auth login --claudeai). Report: $ReportPath"
        exit 8
    }
    if ($run.ExitCode -ne 0) {
        $transcript.Add("## Execution FAILED`nQueen-сессия завершилась с кодом $($run.ExitCode). Полный вывод шага — выше, причина ищется там, а не повторным прогоном руками.")
        Save-Report
        Write-Output "EXECUTION FAILED (exit $($run.ExitCode)). Report: $ReportPath"
        exit 9
    }

    # --- Приёмка: был ли это рой или одна сессия (ERR-2026-000194, -000195) ---
    # Единственный годный критерий — ФАКТИЧЕСКИЕ вызовы в логе. Счётчики CLI
    # (agent metrics, swarm status, hive-mind status) многоагентность через MCP
    # НЕ отражают: 08.08.2026 по ним был сделан ложный вывод «продукт не умеет
    # исполнять задачи агентами», опровергнутый ЛПР по собственному опыту.
    # Отличать ВЫЗОВ от перечня доступных инструментов: в логе вызов выглядит
    # как "name":"mcp__claude-flow__<tool>", а простое упоминание имени — нет.
    $calls = [regex]::Matches($runOut, '"name"\s*:\s*"(mcp__claude-flow__[A-Za-z_-]+)"')
    $distinct = @($calls | ForEach-Object { $_.Groups[1].Value } | Sort-Object -Unique)
    if ($calls.Count -eq 0) {
        $transcript.Add("## ПРИЁМКА: РОЯ НЕ БЫЛО`nВ логе НИ ОДНОГО вызова mcp__claude-flow__* — " +
            "Queen выполнила работу сама, распределения по воркерам не произошло. " +
            "Предметный результат мог быть получен, но как ПРОГОН РОЯ это несостоявшийся " +
            "запуск (ERR-2026-000194). Смотреть, что мешало делегированию: доступны ли были " +
            "инструменты роя сессии (список в начале лога) и не сузила ли цель задачу до соло-работы.")
        Save-Report
        Write-Output "EXECUTED, НО РОЯ НЕ БЫЛО: 0 вызовов mcp__claude-flow__. Report: $ReportPath"
        exit 11
    }
    $transcript.Add("## ПРИЁМКА: рой работал`nВызовов mcp__claude-flow__: $($calls.Count), " +
        "различных инструментов: $($distinct.Count).`n" + ($distinct -join ', '))
    Save-Report
    Write-Output "EXECUTED. Рой работал: $($calls.Count) вызовов роя, $($distinct.Count) инструментов. Report: $ReportPath"
} catch {
    # Раньше исключение уносило с собой весь транскрипт: отчёт обрывался на
    # последнем успешном шаге, и причину падения приходилось искать вручную.
    $transcript.Add("## ПРЕРВАНО ИСКЛЮЧЕНИЕМ`n~~~text`n$($_.Exception.Message)`n~~~`nСтрока: $($_.InvocationInfo.ScriptLineNumber). Всё, что успело выполниться, — выше.")
    Save-Report
    Write-Output "FAILED: $($_.Exception.Message). Report: $ReportPath"
    exit 1
} finally {
    Pop-Location

    # Сканер секретов — по TargetPath (где реально менялись файлы), не по ProjectRoot.
    $scanner = Join-Path $PSScriptRoot 'scan_secrets.ps1'
    if ((Test-Path -LiteralPath $scanner) -and $Approval) {
        try {
            $scanJson = & $scanner -Path $TargetPath -ReportPath (Join-Path $ProjectRoot "leaks-$PID.json") 2>&1 | Out-String
            $scan = $scanJson | ConvertFrom-Json
            $verdict = switch ($scan.outcome) {
                'clean'  { "СЕКРЕТОВ НЕ НАЙДЕНО ($($scan.seconds) c, $($scan.verifiedBy))" }
                'leaks'  { "НАЙДЕНА УТЕЧКА: находок $($scan.findings). Смотреть по файлу и строке в $($scan.report)" }
                default  { "ПРОВЕРИТЬ НЕ СМОГ: $($scan.reason). Это НЕ «чисто»." }
            }
            $transcript.Add("## Secret scan`n$verdict`n~~~json`n$scanJson~~~")
            Save-Report
            Write-Output "SECRET SCAN: $verdict"
        } catch {
            $transcript.Add("## Secret scan`nПРОВЕРИТЬ НЕ СМОГ: $($_.Exception.Message). Это НЕ «чисто».")
            Save-Report
            Write-Output "SECRET SCAN: ПРОВЕРИТЬ НЕ СМОГ — $($_.Exception.Message)"
        }
    }

    # Снятие замка БЕЗ -Force. С ним правило «снимает только держатель» было
    # недостижимо: любой прогон сносил чужой живой замок, и замок переставал что-либо
    # значить. Здесь это и не нужно — claim выше сделан ИЗ ЭТОГО ЖЕ процесса
    # (`& $hiveSingle` выполняется в текущем процессе, $PID тот же), поэтому свой
    # замок снимается штатной проверкой pid + время старта.
    if (Test-Path -LiteralPath $hiveSingle) {
        & $hiveSingle -Action release -Root $LockRoot 2>$null | Out-Null
    }

    # Последняя страховка: если ни одна ветка выше отчёт не записала (падение до
    # try, отказ ещё на проверках, что угодно) — пишем здесь. Пустой отчёт хуже
    # неудобного: сегодня из-за потерянного транскрипта причину искали руками.
    if (-not $script:reportSaved -and $transcript.Count -gt 0) {
        $transcript.Add("## Примечание`nОтчёт записан страховочной веткой в finally — основной путь записи не отработал.")
        Save-Report
    }
}
