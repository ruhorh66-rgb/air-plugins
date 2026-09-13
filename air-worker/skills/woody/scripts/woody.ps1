<#
.SYNOPSIS
  Дятел — петля разработки под судью цели.

.DESCRIPTION
  Долбит в одну точку дешёвыми ударами: берёт следующий незакрытый шаг плана, исполняет
  его на назначенной ступени, спрашивает судью и пишет замер. Поднимает ступень только
  когда судья не сдвинулся.

  Порядок рычагов обратному не подлежит (см. SKILL.md): не звать модель -> резать ходы ->
  и только потом поднимать ступень. Основание — замер 11.08.2026: цена определяется
  числом ходов, а не выбором модели.

  Совместимость: Windows PowerShell 5.1 и PowerShell 7. Задания планировщика в контуре
  запускаются 5.1, поэтому PS7-синтаксис здесь не используется намеренно.
#>
[CmdletBinding()]
param(
    [string]$ProductRoot = (Get-Location).Path,
    [string]$ConfigPath,
    [switch]$PlanOnly,
    [switch]$WhatIfRun,
    [switch]$Init
)

$ErrorActionPreference = 'Stop'

# Резолв ступени -- общая функция с turn-guard.ps1/prompt-guard.ps1 (задача 6, 12.09.2026):
# точное совпадение, при промахе -- по имени модели до двоеточия, при промахе обоих --
# отказ с названной причиной. См. lib/ladder.ps1 за тем, почему тихий индекс 0 был дефектом.
. (Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\ladder.ps1')

# КОДИРОВКА ВЫВОДА СТАВИТСЯ ЯВНО. Найдено третьей площадкой 12.09.2026: диагностика
# -Init превращалась в «????????» при запуске из чужой оболочки — ломался поток в
# консоль, а не запись в файл (созданные файлы были целы, проверено отдельно).
# Это ТРЕТЬЕ место одной болезни за сутки: запись в файл лечится BOM, перенаправленный
# вывод дочернего процесса — преамбулой UTF-8 в нём самом, а прямой вывод в консоль —
# вот этой строкой. Каждое место чинится своим способом, и починка одного не покрывает
# другие — на этом мы сегодня попались дважды.
#
# Цена молчаливая: сам прогон отрабатывает верно, но человек, доверившийся выводу как
# единственной проверке, видит мусор и не может сказать, сработало ли. Диагностика,
# которую нельзя прочесть, равна её отсутствию.
try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }

function Write-Line([string]$text) {
    $stamp = (Get-Date).ToString('HH:mm:ss')
    Write-Output "$stamp  $text"
}

# -Init: положить образцы в корень продукта. Стоит СРАЗУ ПОСЛЕ Write-Line намеренно —
# выше по файлу функции ещё не объявлены, и вызов упал бы на прогоне, а не на разборе.
#
# ПОЧЕМУ ЭТОТ КЛЮЧ ВООБЩЕ ПОЯВИЛСЯ. Разбор общего регресса 12.09.2026: на машине две
# сессии с загруженным скилом, и НИ ОДНА не завела ни одной итерации петли. Прогон
# показал, что петля исправна — судья вернул код 2 с названной причиной, петля отказалась
# и показала, чего не хватает. Не хватало ровно одного файла: run-config.json. Его
# создание было ручным шагом, не входящим ни в один поток работ, и обе сессии, упёршись,
# уходили делать работу руками. Порог входа в инструмент оказался выше, чем цена обойти
# инструмент.
#
# Отказ этого класса не кричит: инструмент честно говорит «нет конфигурации», человек
# честно делает работу без него, никто не виноват и никто не чинит.
if ($Init) {
    $skillRoot = Split-Path -Parent $PSScriptRoot
    # Реестр фактов кладётся ТОЖЕ. Прежде -Init клал конфигурацию, которая ссылается на
    # goal/checklist.json с min_facts, и самого реестра не клал: первый же прогон судьи
    # на свежем продукте давал «нечем проверить» по фактам. Третий код срабатывал верно,
    # но каркас был внутренне несогласован — обещал файл, которого не выкладывал.
    # Найдено прогоном на живой задаче 12.09.2026.
    $goalDir = Join-Path $ProductRoot 'goal'
    if (-not (Test-Path -LiteralPath $goalDir)) { New-Item -ItemType Directory -Force -Path $goalDir | Out-Null }
    $pairs = @(
        @{ From = Join-Path $skillRoot 'run-config.example.json';   To = Join-Path $ProductRoot 'run-config.json' },
        @{ From = Join-Path $skillRoot 'PLAN.example.md';           To = Join-Path $ProductRoot 'PLAN.md' },
        @{ From = Join-Path $skillRoot 'goal\checklist.example.json'; To = Join-Path $goalDir 'checklist.json' }
    )
    foreach ($pair in $pairs) {
        if (-not (Test-Path -LiteralPath $pair.From)) {
            Write-Line ("ОТКАЗ: образец не найден — {0}" -f $pair.From)
            exit 2
        }
        if (Test-Path -LiteralPath $pair.To) {
            # Существующий файл НЕ затирается. Конфигурация и план правятся руками, и
            # молча заменить их образцом значит стереть решения человека.
            Write-Line ("уже есть, не трогаю: {0}" -f $pair.To)
        } else {
            Copy-Item -LiteralPath $pair.From -Destination $pair.To
            Write-Line ("положено: {0}" -f $pair.To)
        }
    }
    Write-Line 'Дальше: поправь run-config.json под продукт (судья, лестница, бюджет) и PLAN.md под работу. Оба правятся руками, это не заготовка на выброс.'
    exit 0
}

# Нативные инструменты пишут предупреждения и ход работы в stderr, а при
# $ErrorActionPreference = 'Stop' это роняет вызов, хотя команда отработала. Поэтому
# предпочтение ослабляется НА ВРЕМЯ вызова, а судить надо по коду возврата — он
# единственное, что означает успех или отказ. Урок не новый: то же записано в
# E:\-4-\rclone\restic_weekly_backup.ps1, и я на него всё равно наступил 11.09.2026 —
# claude напечатал «Ignoring 17 permissions.allow entries…» и прогон оборвался.
function Invoke-Native([scriptblock]$cmd) {
    $prev = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try { $out = & $cmd 2>&1 | Out-String; $script:NativeExitCode = $LASTEXITCODE }
    finally { $ErrorActionPreference = $prev }
    return $out
}

# Формат согласован ЛПР (AIR_VIBECODING v1.49) и не меняется под предлогом экономии
# токенов или удобства чтения — Ядро п. 1.2.
function Write-Status([string]$phase, [string]$title, [string]$tier, [string]$who, [string]$state) {
    Write-Output ("Фаза {0} · {1} · ступень {2} · {3} · {4}" -f $phase, $title, $tier, $who, $state)
}

function Write-Measure([int]$iteration, $cost, $turns, $total, $budget) {
    # «Не измерено» и «ноль» показываются РАЗНЫМИ словами. Прежняя версия подставляла
    # ноль вместо отсутствующего значения, и строка замера утверждала, что работа была
    # и стоила нисколько. Поймано разбором соседней сессии 12.09.2026 на семи строках
    # сухого прогона, читавшихся как семь бесплатных итераций.
    $c = if ($null -eq $cost) { 'не измерено' } else { '$' + ('{0:N4}' -f [double]$cost) }
    $t = if ($null -eq $turns) { 'не измерено' } else { [string][int]$turns }
    Write-Output ("Замер: итерация {0} · {1} · ходов {2} · всего `${3:N2} из бюджета `${4}" -f `
        $iteration, $c, $t, [double]$total, [double]$budget)
}

# Каждое завершение заканчивается двумя строками. «Не жду ничего» пишется прямо:
# молчание в конце читается как незаданный вопрос. Но пишется после сверки, а не
# по ощущению — ERR-2026-000096.
function Close-Woody([string]$next, [string]$await, [int]$code) {
    Write-Output ''
    Write-Output ("Дальше: " + $next)
    Write-Output ("От тебя жду: " + $await)
    exit $code
}

# --- конфигурация -----------------------------------------------------------
if (-not $ConfigPath) { $ConfigPath = Join-Path $ProductRoot 'run-config.json' }
if (-not (Test-Path -LiteralPath $ConfigPath)) {
    Write-Line "ОТКАЗ: нет $ConfigPath. Образец — run-config.example.json в скиле."
    exit 2
}
$cfg = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json

# Умолчания заданы здесь, а не в примере конфигурации: пример можно не дочитать,
# а умолчание работает всегда.
# @() снаружи if — не украшение. Присваивание из if РАЗВОРАЧИВАЕТ массив из одного
# элемента в скаляр, и тогда $ladder[0] вернёт первую БУКВУ строки, а не ступень.
# Поймано боевым прогоном 11.09.2026: в журнале стояло «ступень h» вместо «haiku:low».
$ladder      = @(if ($cfg.ladder) { $cfg.ladder } else { 'script','haiku','sonnet','opus' })
$maxIter     = if ($cfg.budget -and $cfg.budget.iterations) { [int]$cfg.budget.iterations } else { 12 }
$maxUsd      = if ($cfg.budget -and $cfg.budget.usd) { [double]$cfg.budget.usd } else { 20 }
$maxTurns    = if ($cfg.budget -and $cfg.budget.turns_per_iteration) { [int]$cfg.budget.turns_per_iteration } else { 60 }
# Потолок застоя — четвёртый предохранитель, рядом с итерациями, деньгами и ходами.
# Требование 7 нормы AUTO-080: механизм, молча жгущий квоту на работе, которая не идёт,
# хуже остановленного — он выглядит работающим. Умолчание 3: один неподвижный прогон
# бывает и при исправной работе (шаг широк), два подряд уже подозрительны, три — застой.
$stallLimit  = if ($cfg.budget -and $cfg.budget.stall_runs) { [int]$cfg.budget.stall_runs } else { 3 }
$script:stalledRuns = 0

# Двигатель цели рядом с судьёй. Отсутствие файла — не отказ петли: механизм молодой,
# и продукт, собранный до него, обязан продолжать крутиться. Но и молча подставлять
# «всё хорошо» он не будет — при отсутствии просто не зовётся, и это видно в журнале.
$driftScript = Join-Path $PSScriptRoot 'goal-drift.ps1'
if (-not (Test-Path -LiteralPath $driftScript -PathType Leaf)) { $driftScript = $null }
$orchestrate = if ($cfg.orchestration -and $cfg.orchestration.enabled) { [bool]$cfg.orchestration.enabled } else { $false }
$subagents   = if ($cfg.orchestration -and $cfg.orchestration.subagents) { [int]$cfg.orchestration.subagents } else { 2 }
$planName    = if ($cfg.plan) { [string]$cfg.plan } else { 'PLAN.md' }
$planPath    = Join-Path $ProductRoot $planName
$stepsPath   = Join-Path $ProductRoot 'steps.jsonl'

# Судья по умолчанию — универсальный, из этого же скила: продукт объявляет ЧТО
# проверять, а не КАК. Свой judge.path берётся, только если продукт его назвал.
if (-not $cfg.judge) {
    Write-Line 'ОТКАЗ: в конфигурации нет раздела judge. Без судьи Вуди не работает.'
    exit 3
}
if ($cfg.judge.path) {
    $judgePath = Join-Path $ProductRoot $cfg.judge.path
    $judgeArgs = if ($cfg.judge.args) { @($cfg.judge.args) } else { @() }
} else {
    $judgePath = Join-Path $PSScriptRoot 'judge.ps1'
    $judgeArgs = @('-ProductRoot', $ProductRoot, '-ConfigPath', $ConfigPath)
}
if (-not (Test-Path -LiteralPath $judgePath)) {
    Write-Line "ОТКАЗ: судья не найден — $judgePath"
    exit 3
}

# --- план -------------------------------------------------------------------
# Формат строки, намеренно человеческий:
#   - [ ] script  Заголовок шага :: команда
#   - [x] sonnet  Закрытый шаг
# Закрывает шаг человек или петля, дописывая x. Машина не владеет разбивкой.
# Имена ступеней: свои и вендорские. Суффикс усилия НЕОБЯЗАТЕЛЕН, но принимается —
# решение ЛПР 11.09.2026 о двух ступеньках на каждую модель ('haiku:medium', 'haiku:max')
# было записано в лестницу конфигурации, а разборщик плана его не принимал. То есть
# согласованное решение нельзя было выразить в плане ВООБЩЕ. Найдено первым же живым
# прогоном петли 12.09.2026 — чтением не находилось, потому что оба файла по отдельности
# выглядели верными.
$script:TierPattern = 'script|haiku|sonnet|opus|luna|terra|sol'

function Read-Plan([string]$path) {
    if (-not (Test-Path -LiteralPath $path)) { return @() }
    $steps = New-Object System.Collections.Generic.List[object]
    $i = 0

    # ДВЕ ФОРМЫ ЗАПИСИ ШАГА, и это не украшение.
    #
    # Строка с галочкой — простая и короткая, для планов без приёмки на каждый шаг.
    # Строка таблицы — там, где у шага есть СВОЙ судья. Контракт требует «шаг без
    # машинной проверки в работу не берётся», а в строку с галочкой судью вписать
    # физически некуда: там только ступень, заголовок и команда.
    #
    # Поэтому таблица — не альтернативный стиль, а единственная форма, в которой
    # требуемое содержимое помещается. Второго ДОКУМЕНТА при этом не заводится: план
    # остаётся один, и человек правит ровно его.
    #
    # Найдено прогоном 12.09.2026: план ASW на 30 шагов был написан таблицами, петля
    # ответила «план пуст или не найден» — при живом, закоммиченном, вычитанном плане.
    $reLine  = '^\s*-\s*\[(?<done>[ xX])\]\s+(?<tier>' + $script:TierPattern + ')(?<effort>:[A-Za-z]+)?\s+(?<rest>.+)$'

    # Закрытый шаг таблицы помечается зачёркнутым номером: | ~~7~~ | ... |
    # Видно глазом, разбирается машиной, правится руками — как и весь план.
    $reTable = '^\s*\|\s*(?<done>~~)?\s*(?<num>[0-9]+[A-Za-zА-Яа-я]?)\s*(?:~~)?\s*\|' +
               '\s*(?<title>[^|]+?)\s*\|' +
               '\s*`?(?<tier>' + $script:TierPattern + ')(?<effort>:[A-Za-z]+)?`?\s*\|' +
               '\s*(?<judge>[^|]*?)\s*\|\s*$'

    foreach ($line in (Get-Content -LiteralPath $path -Encoding UTF8)) {
        $m = [regex]::Match($line, $reLine)
        if ($m.Success) {
            $i++
            $rest = $m.Groups['rest'].Value
            $cmd = $null
            $sep = $rest.IndexOf('::')
            if ($sep -ge 0) {
                $cmd = $rest.Substring($sep + 2).Trim()
                $rest = $rest.Substring(0, $sep).Trim()
            }
            $steps.Add([pscustomobject]@{
                Index  = $i
                Done   = ($m.Groups['done'].Value -ne ' ')
                Tier   = $m.Groups['tier'].Value + $m.Groups['effort'].Value
                Title  = $rest.Trim()
                Cmd    = $cmd
                Judge  = $null
                Gate   = $false
                Line   = $line
            })
            continue
        }

        # Шаг-ГЕЙТ: в колонке ступени не ступень, а прочерк или слово «гейт». Такой шаг
        # закрывается решением ЛПР, а не работой, и петля его не исполняет — но и НЕ
        # ГЛОТАЕТ. Первый прогон 12.09.2026 дал «шагов в плане: 30» при 31 строке в
        # плане: разница в один шаг («Релиз и тег») пропадала молча. Пропуск был верным,
        # молчание — нет: несходящийся счёт человек либо не заметит, либо потратит время
        # на поиск причины.
        $g = [regex]::Match($line, '^\s*\|\s*(?<done>~~)?\s*(?<num>[0-9]+[A-Za-zА-Яа-я]?)\s*(?:~~)?\s*\|\s*(?<title>[^|]+?)\s*\|\s*(?:—|-{1,2}|гейт[^|]*)\s*\|\s*(?<judge>[^|]*?)\s*\|\s*$')
        if ($g.Success) {
            $i++
            $steps.Add([pscustomobject]@{
                Index  = $i
                Done   = [bool]$g.Groups['done'].Success
                Tier   = 'gate'
                Title  = ($g.Groups['num'].Value + '. ' + $g.Groups['title'].Value.Trim())
                Cmd    = $null
                Judge  = $g.Groups['judge'].Value.Trim()
                Gate   = $true
                Line   = $line
            })
            continue
        }

        $t = [regex]::Match($line, $reTable)
        if (-not $t.Success) { continue }

        # Заголовок таблицы («| № | Шаг | Ступень | Судья |») сюда не попадает: слово
        # «Ступень» не входит в перечень имён ступеней, и строка просто не совпадает.
        $i++
        $title = $t.Groups['title'].Value.Trim()
        $judge = $t.Groups['judge'].Value.Trim()
        $cmd = $null
        $sep = $title.IndexOf('::')
        if ($sep -ge 0) {
            $cmd = $title.Substring($sep + 2).Trim()
            $title = $title.Substring(0, $sep).Trim()
        }
        $steps.Add([pscustomobject]@{
            Index  = $i
            Done   = [bool]$t.Groups['done'].Success
            Tier   = $t.Groups['tier'].Value + $t.Groups['effort'].Value
            Title  = ($t.Groups['num'].Value + '. ' + $title)
            Cmd    = $cmd
            Judge  = $(if ($judge) { $judge } else { $null })
            Gate   = $false
            Line   = $line
        })
    }
    return $steps
}

function Invoke-Judge {
    $out = Invoke-Native { & powershell.exe -NoProfile -ExecutionPolicy Bypass -File $judgePath @judgeArgs }
    return [pscustomobject]@{ Code = $script:NativeExitCode; Text = $out.Trim() }
}

function Add-Step([hashtable]$row) {
    $row['at'] = (Get-Date).ToString('s')
    # Сухой прогон помечается В КАЖДОЙ строке журнала. Без пометки итерации сухого
    # прогона неотличимы от настоящих: расход ноль, ходов ноль — и читатель journal'а
    # решит, что работа шла и не стоила ничего. Поймано первым же прогоном 12.09.2026:
    # -WhatIfRun оставил семь строк, в которых ничего не выдавало сухого хода.
    $row['whatif'] = [bool]$WhatIfRun
    ($row | ConvertTo-Json -Compress -Depth 4) | Add-Content -LiteralPath $stepsPath -Encoding UTF8
}

# --- исполнение шага --------------------------------------------------------
# Ступень script модель не зовёт вовсе. Это не оптимизация, а правило: шаг с проверяемым
# точным выходом, отданный модели, — оплаченное бесплатное.
function Invoke-ScriptStep($step) {
    if (-not $step.Cmd) {
        # Работы не было — значит и замера нет. $null, а не ноль.
        Write-Line "  шаг $($step.Index): ступень script, но команда не задана (нет '::'). Пропуск."
        return [pscustomobject]@{ Ok = $false; Cost = $null; Turns = $null; Session = $null }
    }
    Write-Line "  выполняю скриптом: $($step.Cmd)"
    if ($WhatIfRun) { return [pscustomobject]@{ Ok = $true; Cost = $null; Turns = $null; Session = 'whatif' } }
    $null = Invoke-Native { & powershell.exe -NoProfile -ExecutionPolicy Bypass -Command $step.Cmd }
    # Здесь ноль ЗАКОННЫЙ и отличается от $null по смыслу: скрипт отработал, и расхода у
    # него нет по определению. Пустое значение означало бы «не мерили», а мы мерили.
    return [pscustomobject]@{ Ok = ($script:NativeExitCode -eq 0); Cost = 0; Turns = 0; Session = $null }
}

# Ступень (цена) и исполнитель (вендор) — разные вещи. Ступень говорит, сколько мы
# готовы заплатить; исполнитель — чем именно исполняем. Одна лестница может смешивать
# вендоров: script -> haiku -> codex -> opus, если так решено для продукта.
function Resolve-Runner([string]$rung) {
    # Ступень — это тройка «вендор · модель · усилие», записанная одной строкой
    # вида «sonnet:high». Второй оси нет намеренно: лестница остаётся плоским
    # списком, петля не усложняется, а ЛПР видит порядок глазами и правит руками.
    $name = $rung
    $effort = $null
    $sep = $rung.IndexOf(':')
    if ($sep -gt 0) {
        $name = $rung.Substring(0, $sep)
        $effort = $rung.Substring($sep + 1).Trim()
    }

    $r = $null
    if ($cfg.runners -and $cfg.runners.PSObject.Properties[$name]) { $r = $cfg.runners.$name }
    if ($r) {
        $kind = if ($r.kind) { [string]$r.kind } else { 'claude' }
        $model = if ($r.model) { [string]$r.model } else { $name }
        if (-not $effort -and $r.effort) { $effort = [string]$r.effort }
        return [pscustomobject]@{ Kind = $kind; Model = $model; Effort = $effort; Name = $name }
    }
    # Умолчание: имя ступени и есть имя модели Claude. Так лестница работает без
    # раздела runners вовсе — он нужен только чтобы подмешать другого вендора.
    if ($name -eq 'script') { return [pscustomobject]@{ Kind='script'; Model=$null; Effort=$null; Name=$name } }
    return [pscustomobject]@{ Kind='claude'; Model=$name; Effort=$effort; Name=$name }
}

function Invoke-Claude([string]$prompt, [string]$model, [string]$effort, [double]$budgetLeft) {
    $a = @('-p', $prompt, '--model', $model, '--output-format', 'json', '--max-turns', $maxTurns)
    # Допустимые уровни: low, medium, high, xhigh, max. Неизвестное значение CLI не
    # отвергает, а МОЛЧА берёт умолчание — печатает предупреждение и идёт дальше.
    # Поэтому опечатка в лестнице обошлась бы дороже, чем ошибка: прогон состоялся бы
    # не на том усилии, и замер приписали бы не той ступени.
    if ($effort) { $a += @('--effort', $effort) }
    # Бюджет отдаётся самому CLI, а не считается постфактум: так он останавливается
    # ДО перерасхода, а не после. Работает только вместе с -p.
    if ($budgetLeft -gt 0) { $a += @('--max-budget-usd', ([math]::Round($budgetLeft, 2))) }
    $raw = Invoke-Native { & claude @a }

    # JSON вынимается ПОСТРОЧНО, а не разбором всего вывода. Причина замерена
    # 11.09.2026: claude печатает предупреждения в тот же поток ПЕРЕД результатом
    # («Ignoring N permissions.allow entries… workspace has not been trusted»), и
    # ConvertFrom-Json на всём выводе падает. Прогон при этом состоялся и деньги
    # потрачены — то есть разбор «не смог» выглядел бы как отказ модели.
    $res = $null
    foreach ($line in ($raw -split "`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        try { $o = $t | ConvertFrom-Json } catch { continue }
        if ($o.type -eq 'result' -or $o.PSObject.Properties['total_cost_usd']) { $res = $o }
    }
    if (-not $res) {
        Write-Line '  claude не вернул разбираемый результат; первые строки ответа:'
        ($raw -split "`n" | Select-Object -First 3) | ForEach-Object { Write-Line "    $_" }
        return [pscustomobject]@{ Ok=$false; Cost=0; Turns=0; Session=$null; Subtype='unparsed' }
    }
    return [pscustomobject]@{
        Ok = (-not $res.is_error); Cost = [double]$res.total_cost_usd; Turns = [int]$res.num_turns
        Session = $res.session_id; Subtype = $res.subtype; ApiMs = $res.duration_api_ms
    }
}

function Invoke-Codex([string]$prompt, [string]$model, [string]$effort) {
    # Поток у codex — JSONL событиями, а не одним объектом: thread.started,
    # turn.started, item.completed, turn.completed | turn.failed, error.
    #
    # stdin ОБЯЗАТЕЛЬНО закрывается. Без этого codex печатает «Reading additional
    # input from stdin...» и ждёт — в задании планировщика это вечное подвисание,
    # тот же класс, что интерактивный запрос пароля у psql и restic.
    $args = @('exec','--json','--skip-git-repo-check','-s','read-only','-C',$ProductRoot)
    if ($model) { $args += @('-m', $model) }
    # У codex усилие идёт конфигом вызова, а не флагом: -c <ключ=значение>.
    # Ставится ПОД ЗАДАЧУ в момент вызова, а не глобально в config.toml.
    if ($effort) { $args += @('-c', "model_reasoning_effort=$effort") }
    $args += @($prompt)
    $raw = Invoke-Native { $prompt | & codex @args }

    $turns = 0; $ok = $false; $subtype = 'unknown'; $cost = $null; $session = $null
    foreach ($line in ($raw -split "`n")) {
        $t = $line.Trim()
        if (-not $t.StartsWith('{')) { continue }
        $o = $null; try { $o = $t | ConvertFrom-Json } catch { continue }
        switch ([string]$o.type) {
            'thread.started'  { $session = $o.thread_id }
            'turn.started'    { $turns++ }
            'turn.completed'  { $ok = $true; $subtype = 'success'
                                if ($o.usage) { $cost = $o.usage.cost_usd } }
            'turn.failed'     { $ok = $false; $subtype = 'turn_failed' }
            'error'           { $subtype = 'error' }
        }
    }
    # Расход: если поток его не несёт, пишем null, а не ноль. Ноль означал бы
    # «бесплатно», и бюджет считался бы неверно в сторону «можно ещё».
    return [pscustomobject]@{ Ok=$ok; Cost=$cost; Turns=$turns; Session=$session; Subtype=$subtype; ApiMs=$null }
}

function Invoke-ModelStep($step, [string]$tier, $judgeText) {
    $lines = New-Object System.Collections.Generic.List[string]
    $lines.Add("Задача: $($step.Title)")
    $lines.Add('')
    $lines.Add('Цель проверяется судьёй, а не тобой. Переписывать судью запрещено.')
    $lines.Add("Судья: $($cfg.judge.path) $($judgeArgs -join ' ')")
    if ($judgeText) {
        $lines.Add('')
        $lines.Add('Последний вердикт судьи:')
        $lines.Add($judgeText)
    }
    $lines.Add('')
    $lines.Add("Потолок ходов на эту итерацию: $maxTurns. Упор в потолок означает, что шаг")
    $lines.Add('слишком широк: сузь его, а не проси модель подороже.')
    if ($orchestrate) {
        $lines.Add('')
        $lines.Add("Режим оркестрации: раздай работу $subagents субагентам и сведи результат.")
        $lines.Add('Оркестрация оправдана только на независимых шагах: на связанных она')
        $lines.Add('умножает ходы, а ходы и есть цена.')
    }
    # ЗАДАНИЕ ПЕРЕДАЁТСЯ ПУТЁМ К ФАЙЛУ, А НЕ ТЕКСТОМ. Требование 3 нормы AUTO-080,
    # купленное там дважды: «Длинный текст в командной строке до исполнителя не доезжает:
    # он отвечает "Понял, что нужно сделать?" и делает ноль работы». 12.09.2026 куплено
    # в третий раз — на отцепленных ветвях этой сессии задание, переданное аргументом,
    # порвалось на пробелах, и ветвь отработала вслепую, потратив доллар.
    #
    # Это же закрывает роль ЦЕЛЬ, которой у нас не было: по норме цель — «файл задания,
    # который исполнитель читает первым действием», а не текст в промпте. Прежде
    # исполнитель получал один заголовок шага из плана и догадывался об остальном.
    $taskDir = Join-Path $ProductRoot '.woody'
    if (-not (Test-Path -LiteralPath $taskDir)) { New-Item -ItemType Directory -Force -Path $taskDir | Out-Null }
    $safe = ($step.Title -replace '[^\p{L}\p{Nd}]+', '-').Trim('-')
    if ($safe.Length -gt 40) { $safe = $safe.Substring(0, 40) }
    $taskFile = Join-Path $taskDir ("TASK-{0}-{1}.md" -f $step.Index, $safe)
    [System.IO.File]::WriteAllText($taskFile, ($lines -join "`r`n"), (New-Object System.Text.UTF8Encoding($true)))

    # В командную строку уходит КОРОТКАЯ директива с путём. Она умещается в аргумент
    # целиком и не зависит от кавычек, пробелов и кодировки консоли.
    $prompt = "Прочитай ПЕРВЫМ ДЕЙСТВИЕМ файл с заданием: $taskFile`n" +
              "В нём задача, судья и ограничения итерации. Работай по нему.`n" +
              "Судью не переписывай: решение «готово» принимает его код возврата, а не твоё мнение."

    $runner = Resolve-Runner $tier
    $eff = if ($runner.Effort) { $runner.Effort } else { 'по умолчанию' }
    Write-Line "  исполнитель: $($runner.Kind) · модель $($runner.Model) · усилие $eff · потолок ходов $maxTurns"
    # Расход, которого поток НЕ ПРИНЁС, пишется как $null, а не как ноль. Норма скила,
    # которую сам скил и нарушал в четырёх местах — найдено разбором соседней сессии и
    # подтверждено чтением кода. Ноль означает «работа была и стоила нисколько», и бюджет
    # считается в сторону «можно ещё». Семь строк сухого прогона в asw читались как семь
    # бесплатных итераций.
    if ($WhatIfRun) { return [pscustomobject]@{ Ok = $true; Cost = $null; Turns = $null; Session = 'whatif'; Subtype = 'whatif' } }

    $tool = if ($runner.Kind -eq 'codex') { 'codex' } else { 'claude' }
    if (-not (Get-Command $tool -ErrorAction SilentlyContinue)) {
        # Инструмента нет — это «нечем исполнить», а не «модель не справилась».
        # Различать обязательно: иначе петля поднимет ступень и заплатит за то же.
        Write-Line "  ОТКАЗ: '$tool' не резолвится. Мерить надо составленным PATH: свой процесс держит копию окружения на момент запуска."
        # Инструмента нет — замера тоже нет. Ноль здесь был бы худшим из вариантов:
        # «попробовали и не потратили» вместо «не смогли попробовать».
        return [pscustomobject]@{ Ok = $false; Cost = $null; Turns = $null; Session = $null; Subtype = 'no_runner' }
    }

    if ($runner.Kind -eq 'codex') { return Invoke-Codex $prompt $runner.Model $runner.Effort }
    return Invoke-Claude $prompt $runner.Model $runner.Effort ($maxUsd - $spent)
}

# --- петля ------------------------------------------------------------------
# ЗАГОТОВКА — НЕ ПЛАН. Найдено прогоном на живой задаче 12.09.2026: сразу после -Init на
# пустом продукте петля печатала «шагов в плане: 7, из них закрыто 1» — это разобранный
# ОБРАЗЕЦ, семь примеров из шаблона, один помечен закрытым. Новый продукт показывал
# прогресс, которого нет. Тот же класс, что весь остальной день: цифра есть, смысла за
# ней нет. Маркер снимается человеком вместе с примерами — это и есть признак, что план
# заполняли, а не скопировали.
if (Test-Path -LiteralPath $planPath) {
    $planRaw = [System.IO.File]::ReadAllText($planPath, [System.Text.Encoding]::UTF8)
    if ($planRaw -match 'ЗАГОТОВКА-НЕ-ЗАПОЛНЕНА') {
        Write-Line "ОТКАЗ: план не заполнен — $planPath всё ещё содержит маркер заготовки."
        Write-Line 'Замени примеры своими шагами и удали строку с маркером. Пока она на месте,'
        Write-Line 'петля считала бы прогрессом разобранные примеры шаблона.'
        exit 2
    }
}

$plan = Read-Plan $planPath
if ($plan.Count -eq 0) {
    Write-Line "ОТКАЗ: план пуст или не найден — $planPath"
    Write-Line 'Без плана Дятел не начинает: разбивка на ступени и есть экономия.'
    exit 4
}

Write-Line "продукт     : $ProductRoot"
Write-Line "судья       : $judgePath"
# Счёт разложен так, чтобы он СХОДИЛСЯ с планом на глаз. Гейты названы отдельной
# величиной: петля их не исполняет, но и не прячет — иначе человек, сверяющий вывод с
# планом, находит расхождение и не знает, потеря это или замысел.
$gates    = @($plan | Where-Object { $_.Gate })
$workable = @($plan | Where-Object { -not $_.Gate })
Write-Line ("шагов в плане: {0}, из них закрыто {1}; исполняемых {2}, гейтов ЛПР {3}" -f `
    @($plan).Count, @($plan | Where-Object { $_.Done }).Count, $workable.Count, $gates.Count)
foreach ($g in $gates) {
    Write-Line ("  гейт ЛПР, петлёй не закрывается: {0}" -f $g.Title)
}
Write-Line "бюджет      : $maxIter итераций, `$$maxUsd, $maxTurns ходов на итерацию"
Write-Line "оркестрация : $(if ($orchestrate) { "включена, субагентов $subagents" } else { 'выключена' })"

$verdict = Invoke-Judge
Write-Line "судья до работы: код $($verdict.Code)"
Write-Line "  $($verdict.Text)"
if ($verdict.Code -eq 0) { Write-Line 'ЦЕЛЬ УЖЕ ДОСТИГНУТА, работать не над чем.'; exit 0 }

# Код 2 — «проверять нечем». Работа здесь не помогает: сколько ни долби, судья не
# сможет вынести вердикт. Это к человеку, а не к следующей итерации.
if ($verdict.Code -eq 2) {
    Write-Line 'СТОП: судья не может вынести вердикт. Чинить условия проверки, а не код.'
    exit 2
}
if ($PlanOnly) { Write-Line 'PlanOnly: дальше не иду.'; exit 0 }

$spent = 0.0
$iter = 0
$lastCode = $verdict.Code
# Подпись вердикта до начала работы: с ней сравнивается каждая последующая.
$lastSig = "$($verdict.Code)|" + (($verdict.Text -replace '\s+', ' ').Trim())

foreach ($step in ($plan | Where-Object { -not $_.Done -and -not $_.Gate })) {
    # Задача 6, 12.09.2026: точное совпадение, затем по имени модели до двоеточия, иначе
    # отказ с названной причиной -- НЕ молчаливый индекс 0. Тихая подмена объявленной
    # ступени самой низкой прежде заставляла петлю карабкаться с нуля незаметно для плана.
    $resolved = Resolve-LadderTier -Ladder $ladder -Tier $step.Tier
    if (-not $resolved.Found) {
        Close-Woody 'ничего: ступень шага не найдена в лестнице продукта' (
            "поправь либо PLAN.md -- шаг $($step.Index) «$($step.Title)» называет ступень " +
            "'$($step.Tier)', которой нет в лестнице (ни точно, ни по имени модели), либо ladder " +
            "в run-config.json. Доступные ступени: $($ladder -join ', ')"
        ) 1
    }
    $tierIndex = $resolved.Index

    while ($true) {
        if ($iter -ge $maxIter) {
            Close-Woody 'ничего: потолок итераций исчерпан' "решить, поднимать ли потолок ($maxIter) или переписать план" 1
        }
        if ($spent -ge $maxUsd) {
            Close-Woody 'ничего: бюджет исчерпан' ("решить, поднимать ли бюджет — потрачено `${0:N2} из `${1}" -f $spent, $maxUsd) 1
        }

        $iter++
        $tier = $ladder[$tierIndex]
        $runner = Resolve-Runner $tier
        $who = if ($runner.Kind -eq 'script') { 'скрипт' } elseif ($orchestrate) { "ведущая · субагентов $subagents" } else { $runner.Kind }
        Write-Status $step.Index $step.Title $tier $who 'прогон идёт'

        if ((Resolve-Runner $tier).Kind -eq 'script') { $r = Invoke-ScriptStep $step }
        else { $r = Invoke-ModelStep $step $tier $verdict.Text }

        $spent += $r.Cost
        $verdict = Invoke-Judge

        Add-Step @{
            step = $step.Index; title = $step.Title; tier = $tier
            iteration = $iter; code = $verdict.Code
            total_cost_usd = $r.Cost; num_turns = $r.Turns
            duration_api_ms = $r.ApiMs; session_id = $r.Session
            subtype = $r.Subtype; spent_usd = [math]::Round($spent, 4)
            orchestration = $orchestrate; subagents = $(if ($orchestrate) { $subagents } else { 0 })
        }

        $state = switch ($verdict.Code) { 0 { 'цель закрыта' } 2 { 'не проверено' } default { 'судья не пропустил' } }
        Write-Status $step.Index $step.Title $tier $who $state
        Write-Measure $iter $r.Cost $r.Turns $spent $maxUsd

        # ДВИГАТЕЛЬ ЦЕЛИ, восстановлен из Goal/Drift Loop ВЕРЫ 13.09.2026.
        # Замер идёт ПОСЛЕ итерации — у ВЕРЫ overlay считался до и после работы, и именно
        # пара «до/после» отвечала на вопрос, двинул ли ход расстояние.
        #
        # Чем он отличается от потолка застоя ниже, который уже есть. Тот сравнивает ПОДПИСЬ
        # ВЕРДИКТА: текст не изменился — значит стоим. Этого мало в двух случаях, и оба
        # случились живьём: текст вердикта может меняться, пока расстояние стоит («не пройдена
        # проверка А» сменилось на «не пройдена проверка Б»), и расстояние может стоять при
        # совершенно исправном вердикте, когда работа идёт по чужому продукту. Двигатель цели
        # считает ЧИСЛА — закрытые факты и открытые шаги плана, — и на подпись не смотрит.
        if ($driftScript) {
            $dOut = Join-Path $ProductRoot '.woody\drift-run.out'
            try {
                $dProc = Start-Process -FilePath 'powershell.exe' -WindowStyle Hidden -PassThru -Wait `
                         -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',$driftScript,
                                         '-ProductRoot',$ProductRoot,'-Record','-Note',"итерация $iter · шаг $($step.Index)") `
                         -RedirectStandardOutput $dOut -RedirectStandardError ($dOut + '.err')
                # Код 3 — «ждёт ЛПР»: работой закрывать нечего, но цель не достигнута.
                # Это НЕ эскалация и не дефект работы; петля закрывается по-хорошему, а
                # остаток уходит человеку. Различать обязательно: «сузь шаг» лечится
                # следующей итерацией, «работы не осталось» — только человеком, и склеить
                # их значило бы вернуть неразличимость, которую нашла AIR-ENV-002.
                if ($dProc.ExitCode -eq 3) {
                    $dWhy3 = ''
                    try { $dWhy3 = (Get-Content -LiteralPath $dOut -Raw -Encoding UTF8).Trim() } catch { }
                    Close-Woody 'ничего: работой закрывать нечего, расстояние до цели ноль' `
                                ("закрыть остаток — он держится гейтами ЛПР либо реестр не покрывает " +
                                 "того, что требует судья. $dWhy3") 0
                }
                if ($dProc.ExitCode -eq 2) {
                    $dWhy = ''
                    try { $dWhy = (Get-Content -LiteralPath $dOut -Raw -Encoding UTF8).Trim() } catch { }
                    # Эскалация ОСТАНАВЛИВАЕТ цикл. Это прямая копия найденного у ВЕРЫ дефекта:
                    # там вердикт сперва влиял только на отдельных исполнителей, цикл продолжал
                    # брать постороннюю работу и кончался PASS. Гейт без действия — не гейт.
                    Close-Woody 'ничего: двигатель цели объявил эскалацию' `
                                ("решить, что делать с работой, которая не уменьшает расстояние до цели. " +
                                 "$dWhy Подъём ступени этого не лечит: дорогая модель так же точно " +
                                 "будет двигаться мимо.") 1
                }
            } catch { }
        }

        if ($verdict.Code -eq 0) {
            Write-Line $verdict.Text
            Close-Woody 'закрыть работу вместе с замером: сколько стоило, сколько ходов, какими моделями' 'не жду ничего' 0
        }

        if ($verdict.Code -eq 2) {
            Write-Line $verdict.Text
            Close-Woody 'ничего: работой это не лечится' "починить условия проверки — судья не может вынести вердикт" 2
        }

        # Упор в потолок ходов подъёмом ступени не лечится: это признак слишком широкого
        # шага. Поднимать модель здесь значит платить дороже за ту же ошибку.
        if ($r.Subtype -eq 'error_max_turns') {
            Close-Woody 'ничего: подъём ступени эту ошибку не лечит, он оплачивает её дороже' `
                        "разбить шаг $($step.Index) в плане на более узкие — он упёрся в потолок ходов" 1
        }
        # CLI остановился своим же потолком бюджета. Это не отказ модели и не повод
        # поднимать ступень: следующая ступень дороже и упрётся в тот же потолок раньше.
        if ($r.Subtype -eq 'error_max_budget_usd') {
            Close-Woody 'ничего: прогон остановлен потолком бюджета внутри CLI' `
                        ("решить, поднимать ли бюджет — потрачено `${0:N4} из `${1}" -f $spent, $maxUsd) 1
        }
        if ($r.Subtype -eq 'no_runner') {
            Close-Woody 'ничего: исполнителя нет на машине' 'поставить или авторизовать исполнителя — это «нечем исполнить», а не отказ модели' 2
        }

        # ДВИЖЕНИЕ СУДЬИ ЛОВИТСЯ ПОДПИСЬЮ, А НЕ КОДОМ. Переделка 12.09.2026 по разбору
        # соседней сессии, проверенному в коде.
        #
        # Прежде здесь стояло сравнение кодов. Кодов три, а состояний работы — сколько
        # угодно: «закрыто 13 из 16» и «закрыто 14 из 16» оба дают код 1, значит прогресс
        # был НЕВИДИМ, и петля лезла вверх по лестнице независимо от него. Это прямо
        # обратно замыслу скила: множество дешёвых ударов в одну точку превращалось в
        # дорогие удары по неподвижной цели.
        #
        # Наблюдалось вживую на первом же прогоне: шаг прошёл всю лестницу от script до
        # opus:max за шесть подъёмов, ни один из которых не был вызван отсутствием
        # прогресса — прогресса просто не было видно.
        #
        # Подпись — это код ПЛЮС текст вердикта. Судья и так печатает «фактов закрыто N
        # из M»; смена N меняет подпись, и петля остаётся на дешёвой ступени, пока работа
        # сдвигается хоть на один факт.
        $verdictSig = "$($verdict.Code)|" + (($verdict.Text -replace '\s+', ' ').Trim())
        if ($verdictSig -ne $lastSig) {
            $lastSig = $verdictSig
            $lastCode = $verdict.Code
            $script:stalledRuns = 0
            Write-Line "  судья сдвинулся — остаюсь на той же ступени. Вердикт: $($verdict.Text)"
            continue
        }

        # ЗАСТОЙ ПО ЦЕЛИ, А НЕ ПО ШАГУ. Подъём ступени выше — правильный ответ на застой
        # ОДНОГО шага. Но если вердикт не двигается подряд, сколько бы шагов петля ни
        # перебрала и на какие бы ступени ни поднималась, — работа не идёт, и разница
        # между «механизм крутится» и «механизм буксует» снаружи неразличима.
        #
        # Требование 7 нормы AUTO-080 дословно: «Если несколько запусков подряд не
        # сдвигают вердикт судьи, цель снимается с вращения с записью в журнал.
        # Механизм, молча жгущий квоту на работе, которая не идёт, хуже остановленного:
        # он ВЫГЛЯДИТ работающим.»
        #
        # Счётчик общий и сбрасывается любым сдвигом вердикта — в том числе на другом
        # шаге: цель одна, и двигают её сообща.
        $script:stalledRuns++
        if ($script:stalledRuns -ge $stallLimit) {
            Close-Woody ("ничего: цель не сдвинулась $($script:stalledRuns) прогонов подряд") `
                        ("снять цель с вращения либо изменить постановку — вердикт судьи не меняется " +
                         "с $($script:stalledRuns) прогонов: «$($verdict.Text)». Подъём ступени это не лечит, " +
                         "он оплачивает ту же неподвижность дороже.") 1
        }

        if ($tierIndex -ge ($ladder.Count - 1)) {
            Close-Woody 'ничего: лестница пройдена до верха' `
                        "решение по шагу $($step.Index) «$($step.Title)» — судья не сдвинулся и на верхней ступени" 1
        }
        $tierIndex++
        Write-Line "  судья не сдвинулся — поднимаю ступень до $($ladder[$tierIndex])."
    }
}

# Найдено прогоном 12-13.09.2026: этот выход достигается, когда foreach ($step in ...)
# на строке 558 не находит ни одного открытого неgate-шага и потому ни разу не входит в
# тело цикла -- ни одного Add-Step. Журнал стоял пустым при настоящем, честном прогоне
# петли, и «петля не заводилась» (нет steps.jsonl) становилось неотличимо от «завелась и
# честно нашла, что делать нечего» -- тот же класс путаницы, ради которого весь журнал
# и заведён. Нулевая итерация тоже итерация: она должна быть в журнале, а не молчать.
Add-Step @{
    step = $null; title = 'план пройден до конца'; tier = $null
    iteration = 0; code = $lastCode
    total_cost_usd = 0; num_turns = 0
    duration_api_ms = 0; session_id = $null
    subtype = $null; spent_usd = [math]::Round($spent, 4)
    orchestration = $orchestrate; subagents = $(if ($orchestrate) { $subagents } else { 0 })
    reason = 'все исполняемые шаги плана уже закрыты (Done); открытых non-gate шагов нет, foreach не вошёл ни разу'
}
Close-Woody 'ничего: план пройден до конца' 'дополнить план — все шаги закрыты, а судья цель не подтвердил' 1
