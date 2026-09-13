<#
.SYNOPSIS
  Страж хода: не даёт закончить ход без утверждённого формата, без петли и без
  согласованного выхода из режима.

.DESCRIPTION
  Лечит конкретный отказ 11.09.2026. Сессия включила режим оркестрации, отработала в нём
  один ход и молча вышла: перестала печатать формат, написала «строю» без строки состояния
  и обогнала собственный запущенный субагент, подменив его результат своими заметками.
  ЛПР остановил работу словами «я не понимаю, какие агенты работают и над какой задачей ты
  работаешь». Режим держался на памяти, а память не механизм.

  Вешается на Stop. Получает на stdin JSON с last_assistant_message. Пока режим включён,
  проверяет формат ходом функции Test-TurnFormat (lib/turn-format.ps1 — общая с
  prompt-guard.ps1, второй рубежом на UserPromptSubmit, задача 1, 12.09.2026) и добавляет
  три собственные проверки состояния продукта: петля не заводилась (задача 4), частичный
  выход из режима без согласования (задача 5). Отдельно, без влияния на код возврата,
  зовёт судью продукта и пишет его вердикт файлом (задача 3).

  ДВА ПРАВИЛА БЕЗОПАСНОСТИ, оба обязательны:

  1. Страж молчит, пока режим не включён явно. Вуди грузится на каждом старте по контракту
     AIR_START, и глухой страж заблокировал бы каждый обычный ответ в каждой сессии.
  2. Страж ПРОПУСКАЕТ при любой своей ошибке. Сторож, который ломает работу, когда сам
     сломался, хуже отсутствующего: его снимут первым же движением и вместе с пользой.
#>
[CmdletBinding()]
param()

# Fail-open с первой строки: что бы ни случилось ниже, по умолчанию мы пропускаем.
$ErrorActionPreference = 'Continue'

. (Join-Path $PSScriptRoot '..\lib\turn-format.ps1')
. (Join-Path $PSScriptRoot '..\lib\transcript.ps1')

# СЛЕД РЕШЕНИЙ грузится здесь же, а не в момент записи: функции нужны и
# Invoke-WoodyVerdict (роль «судья»), и коду хода ниже (роль «страж»). Отсутствие
# файла не роняет хук — Write-WoodyTrace тогда просто не появится, и каждый вызов
# обёрнут СВОИМ try/catch (см. ниже), а не общим fail-open хука.
$__traceLib = Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\trace.ps1'
if (Test-Path -LiteralPath $__traceLib) { . $__traceLib }

# След роли «судья». ОБЁРНУТ СВОИМ try/catch, а не общим catch хука внизу файла:
# режим отладки 12.09.2026 нашёл живым прогоном, что ошибка в вызове следа (например,
# опечатка в имени переменной) улетала в общий catch и хук отдавал exit 0 вместо
# честного exit 2 — поломка ОДНОГО диагностического механизма маскировала отказ
# ДРУГОГО, реального. Здесь то же самое не должно повториться: эта функция может
# бросить исключение из-за отсутствующего Write-WoodyTrace (файла нет) или ошибки
# внутри неё самой, и ни то, ни другое не имеет права прервать Invoke-WoodyVerdict
# или уйти выше по стеку.
function Write-JudgeTrace {
    param([string]$SessionKey, [string]$Decision, [string]$Detail, [hashtable]$Extra)
    try {
        Write-WoodyTrace -SessionId $SessionKey -Role 'судья' -Event 'Stop' `
            -Decision $Decision -Detail $Detail -Extra $Extra
    } catch { }
}

# Судья зовётся ОТДЕЛЬНЫМ процессом (Start-Process ... -PassThru), не через '&' —
# иначе его exit код завершил бы сам хук, и остальные проверки этого хода не выполнились
# бы вовсе. Задача 3, 12.09.2026.
#
# Три условия по постановке:
#   - нет run-config.json в рабочем каталоге — судья не зовётся, функция молчит;
#   - таймаут хука 20с, судье отведён потолок 12с; не успел — code=NULL, а не 0
#     (0 означал бы «цель достигнута», NULL значит «не измерено»);
#   - не чаще раза в 60с — время последнего прогона берётся из самого файла вердикта,
#     а не из отдельного файла-замка: вердикт и есть свидетельство, что прогон был.
function Invoke-WoodyVerdict {
    param([string]$Cwd, [string]$SessionKey, [string]$StateDir)

    if (-not $Cwd) { Write-JudgeTrace $SessionKey 'пропущено-нечем' 'нет рабочего каталога (cwd) хода'; return }
    $cfgPath = Join-Path $Cwd 'run-config.json'
    if (-not (Test-Path -LiteralPath $cfgPath -PathType Leaf)) {
        Write-JudgeTrace $SessionKey 'пропущено-нечем' 'нет run-config.json в рабочем каталоге'
        return
    }

    $verdictPath = Join-Path $StateDir "woody-verdict-$SessionKey.json"
    if (Test-Path -LiteralPath $verdictPath) {
        try {
            $prev = Get-Content -LiteralPath $verdictPath -Raw -Encoding UTF8 | ConvertFrom-Json
            if ($prev.time) {
                $age = ((Get-Date) - [datetime]$prev.time).TotalSeconds
                if ($age -lt 60) {
                    Write-JudgeTrace $SessionKey 'пропущено-нечем' "вызывался $([int]$age)с назад, реже раза в 60с нельзя"
                    return
                }
            }
        } catch { }
    }

    $cfg = $null
    try { $cfg = Get-Content -LiteralPath $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch {
        Write-JudgeTrace $SessionKey 'пропущено-нечем' 'run-config.json не читается (ошибка разбора)'
        return
    }

    $judgePath = $null
    $judgeArgs = @()
    if ($cfg.judge -and $cfg.judge.path) {
        $judgePath = Join-Path $Cwd ([string]$cfg.judge.path)
        if ($cfg.judge.args) { $judgeArgs = @($cfg.judge.args | ForEach-Object { [string]$_ }) }
    } else {
        # Судья по умолчанию — универсальный, живёт в этом же скиле.
        $skillRoot = Split-Path -Parent $PSScriptRoot
        $judgePath = Join-Path $skillRoot 'scripts\judge.ps1'
        $judgeArgs = @('-ProductRoot', $Cwd, '-ConfigPath', $cfgPath)
    }
    if (-not (Test-Path -LiteralPath $judgePath -PathType Leaf)) {
        Write-JudgeTrace $SessionKey 'пропущено-нечем' "судья не найден по пути $judgePath"
        return
    }

    $psArgs = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', $judgePath) + $judgeArgs
    $outFile = [System.IO.Path]::GetTempFileName()
    $errFile = [System.IO.Path]::GetTempFileName()
    $code = $null
    $timedOut = $false
    try {
        $proc = Start-Process -FilePath 'powershell.exe' -ArgumentList $psArgs -PassThru -WindowStyle Hidden `
                    -RedirectStandardOutput $outFile -RedirectStandardError $errFile
        if ($proc.WaitForExit(12000)) {
            $code = $proc.ExitCode
        } else {
            $timedOut = $true
            try { $proc.Kill() } catch { }
        }
    } catch {
        Write-JudgeTrace $SessionKey 'пропущено-нечем' 'судья не запустился (Start-Process упал)'
        return
    }

    $firstLine = ''
    try {
        if (Test-Path -LiteralPath $outFile) {
            $firstLine = [string](Get-Content -LiteralPath $outFile -Encoding UTF8 | Select-Object -First 1)
        }
    } catch { }
    Remove-Item -LiteralPath $outFile, $errFile -Force -ErrorAction SilentlyContinue

    # NULL, а не ноль — ноль означал бы «цель достигнута».
    $codeOut = if ($timedOut) { $null } else { $code }
    $reasonOut = if ($timedOut) { 'судья не успел за отведённые 12 секунд' } else { $null }

    @{
        time         = (Get-Date).ToString('s')
        code         = $codeOut
        reason       = $reasonOut
        product_root = $Cwd
        first_line   = $firstLine
    } | ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $verdictPath -Encoding UTF8

    $judgeDetail = if ($timedOut) { 'таймаут 12с, code=null' } else { "код $codeOut" }
    Write-JudgeTrace $SessionKey 'записал' $judgeDetail -Extra @{ product_root = $Cwd }
}

try {
    # Stdin читается ЯВНО как UTF-8, а не через [Console]::In. Найдено прогоном
    # 12.09.2026: страж блокировал ход, в котором все четыре строки были на месте.
    # Зонд показал, почему — вместо «Фаза 1 · Чистка контура» он получал
    # «╨ñ╨░╨╖╨░ 1 ┬╖ ╨º╨╕╤ü╤é╨║╨░». Харнесс шлёт UTF-8, а [Console]::In в PowerShell 5.1
    # декодирует кодовой страницей консоли: кириллица превращается в мусор и «^Фаза»
    # не совпадает НИКОГДА. Зеркало той же беды, что была на выводе.
    $stdinStream = [Console]::OpenStandardInput()
    $stdinReader = New-Object System.IO.StreamReader($stdinStream, (New-Object System.Text.UTF8Encoding($false)))
    $raw = $stdinReader.ReadToEnd()
    if (-not $raw) { exit 0 }
    $ev = $raw | ConvertFrom-Json

    $stateDir = Join-Path $env:ProgramData 'AIR OS\State'
    if (-not (Test-Path -LiteralPath $stateDir)) { New-Item -ItemType Directory -Force -Path $stateDir | Out-Null }

    # СОСТОЯНИЕ ПОСЕССИОННОЕ. Ключ — session_id события, и ИМЯ ФАЙЛА И ЕСТЬ ПРИВЯЗКА.
    $sessionKey = [string]$ev.session_id
    if (-not $sessionKey) { exit 0 }

    # Зонд посессионный: диагностика последнего Stop этой сессии.
    try {
        $lam = [string]$ev.last_assistant_message
        @{
            session_id = $sessionKey
            hook_event_name = $ev.hook_event_name
            captured = (Get-Date).ToString('s')
            lam_length = $lam.Length
            lam_head_120 = $(if ($lam.Length -gt 120) { $lam.Substring(0,120) } else { $lam })
            lam_tail_120 = $(if ($lam.Length -gt 120) { $lam.Substring($lam.Length-120,120) } else { $lam })
        } | ConvertTo-Json | Set-Content -LiteralPath (Join-Path $stateDir "woody-session-probe-$sessionKey.json") -Encoding UTF8
    } catch { }

    $modeFile = Join-Path $stateDir "woody-mode-$sessionKey.json"
    if (-not (Test-Path -LiteralPath $modeFile)) { exit 0 }

    $mode = Get-Content -LiteralPath $modeFile -Raw -Encoding UTF8 | ConvertFrom-Json
    if (-not $mode.enabled) { exit 0 }

    $msg = [string]$ev.last_assistant_message
    if (-not $msg) { exit 0 }

    # Живые субагенты — из файла, который ведут SubagentStart/SubagentStop.
    $running = @()
    $runFile = Join-Path $stateDir ("woody-agents-" + $sessionKey + ".json")
    if (Test-Path -LiteralPath $runFile) {
        try {
            $st = Get-Content -LiteralPath $runFile -Raw -Encoding UTF8 | ConvertFrom-Json
            $running = @($st.running)
        } catch { }
    }

    # Рабочий каталог хода. Событие Stop не подтверждено прогоном как несущее cwd —
    # Get-EventCwd падает на журнал сессии, если поля события не хватает (lib/transcript.ps1).
    $cwd = Get-EventCwd $ev

    # Конфигурация продукта — читается один раз, дальше используется задачами 2, 3, 4, 5.
    # ПРОДУКТ РАЗРЕШАЕТСЯ ОДИН РАЗ, И ИМ ПОЛЬЗУЮТСЯ ВСЕ ПРОВЕРКИ.
    #
    # Здесь исправляются ДВЕ мои ошибки подряд, обе найдены claude-71 13.09.2026.
    #
    # Первая: страж брал продукт из рабочего каталога хода, а тот не удерживается между
    # вызовами инструментов. Вторая, моя починка первой и хуже неё: продукт привязывался
    # ПЕРВЫМ, который страж увидел, — и привязка поймала момент, когда сессия зашла в
    # каталог скила за обновлением. После этого двигатель цели считал расстояние по
    # чужому состоянию, эскалация сработала на нём, а страж потребовал закрывать шаги из
    # PLAN.example.md — из тестового образца. Механизм не отличает «решила работать
    # здесь» от «проходила мимо», и отличить не сможет: у прохода мимо нет своего
    # признака. Поэтому угадывание убрано целиком — продукт ОБЪЯВЛЯЕТСЯ.
    #
    # Третья ошибка была в том, что первую починку я провела ТОЛЬКО через двигатель, а
    # судья остался на сыром $cwd — и продолжал писать вердикт чужого продукта, пока
    # привязка была верной. Отсюда правило: точка разрешения одна, и ниже по коду $cwd
    # для состояния продукта не используется вовсе.
    $productRoot = $null
    $productDeclared = $false
    if ($sessionKey) {
        $prodFile = Join-Path $stateDir "woody-product-$sessionKey.json"
        if (Test-Path -LiteralPath $prodFile) {
            try {
                $pb = Get-Content -LiteralPath $prodFile -Raw -Encoding UTF8 | ConvertFrom-Json
                if ($pb.path -and $pb.declared_at -and (Test-Path -LiteralPath $pb.path -PathType Container)) {
                    $productRoot = [string]$pb.path
                    $productDeclared = $true
                }
            } catch { }
        }
    }

    $cfg = $null
    $ladder = $null
    $cfgPath = $null
    $driftWarn = $null
    $undeclaredNote = $null
    if (-not $productDeclared -and $cwd -and (Test-Path -LiteralPath (Join-Path $cwd 'run-config.json') -PathType Leaf)) {
        # Похоже на продукт, но это может быть и проход мимо. Не угадываем: называем.
        $undeclaredNote = "продукт сессии не объявлен, а в каталоге хода ($cwd) лежит run-config.json. " +
                          "Судья и двигатель цели по нему НЕ запускались: рабочий каталог не удерживается " +
                          "между вызовами инструментов, и один раз это уже стоило вердикта по чужому " +
                          "продукту. Объяви: mode.ps1 -Product `"$cwd`""
    }

    if ($productRoot) {
        $cfgPath = Join-Path $productRoot 'run-config.json'
        if (Test-Path -LiteralPath $cfgPath -PathType Leaf) {
            try { $cfg = Get-Content -LiteralPath $cfgPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch { $cfg = $null }
            if ($cfg -and $cfg.ladder) { $ladder = @($cfg.ladder) }
        }
    }

    # Проверки 1-7 (формат) и 2b (ступень субагента, задача 2) — общая функция с
    # prompt-guard.ps1. Одна логика, а не две расходящиеся копии.
    $missing = @(Test-TurnFormat -Msg $msg -Running $running -DeclaredSubagents ([int]$mode.subagents) -Ladder $ladder)

    # ЗАДАЧА 4, 12.09.2026: петлю игнорируют — страж не даёт закончить ход в обход неё.
    # Если в рабочем каталоге есть и конфигурация, и PLAN.md, а steps.jsonl нет вовсе —
    # петля не заводилась ни разу. Обязательный выход: PLAN.md строкой «Вуди неприменим:
    # <причина>» — молчание выходом не является.
    if ($productRoot -and $cfgPath -and (Test-Path -LiteralPath $cfgPath -PathType Leaf)) {
        $planPath4  = Join-Path $productRoot 'PLAN.md'
        $stepsPath4 = Join-Path $productRoot 'steps.jsonl'
        if ((Test-Path -LiteralPath $planPath4 -PathType Leaf) -and -not (Test-Path -LiteralPath $stepsPath4 -PathType Leaf)) {
            $planText = ''
            try { $planText = Get-Content -LiteralPath $planPath4 -Raw -Encoding UTF8 } catch { }
            if ($planText -notmatch '(?m)^\s*Вуди неприменим\s*:') {
                $skillRootTG = Split-Path -Parent $PSScriptRoot
                $woodyCmd = 'powershell -File "' + (Join-Path $skillRootTG 'scripts\woody.ps1') + '" -ProductRoot "' + $productRoot + '" -ConfigPath "' + $cfgPath + '"'
                $missing += "петля не заводилась ни разу (steps.jsonl нет). Запусти: $woodyCmd " +
                            "— либо, если судьи в принципе быть не может, объяви это в PLAN.md строкой " +
                            "«Вуди неприменим: <причина>»; молчание выходом не является."
            }
        }
    }

    # ДВИГАТЕЛЬ ЦЕЛИ. Восстановлен 13.09.2026 из Goal/Drift Loop ВЕРЫ по указанию ЛПР.
    # Здесь он заменяет собой ДОГАДКУ о намерении хода замером его ДЕЙСТВИЯ.
    #
    # Почему это сильнее соседних проверок. Проверка простоя и проверка ухода от плана,
    # написанные в этот же день выше и ниже, обе читают ТЕКСТ ХОДА: одна ищет «Дальше:
    # ничего», другая — номер фазы. Это самоотчёт, и ВЕРА такой счётчик у себя уже
    # ловила: он «молча переставал расти», когда менялась формулировка. Двигатель цели
    # не читает текст вовсе. Он берёт числа судьи и открытые шаги плана и отвечает на
    # один вопрос: расстояние до цели уменьшилось или нет.
    #
    # И уход в сторону, и сон дают ОДИН И ТОТ ЖЕ ответ — расстояние стоит. Поэтому одно
    # число закрывает обе беды, и закрывает их, не угадывая, чем сессия была занята.
    #
    # ЗАПИСЬ ЗАМЕРА ИДЁТ КАЖДЫЙ ХОД. Ход и есть единица движения; замерять реже значит
    # оставить часть ходов вне счёта, а именно они и уходят в сторону незамеченными.
    # ПРОДУКТ ПРИВЯЗЫВАЕТСЯ К СЕССИИ, А НЕ БЕРЁТСЯ ИЗ КАТАЛОГА ХОДА.
    #
    # Найдено claude-71 13.09.2026 и воспроизведено ею дважды: рабочий каталог хода не
    # удерживается между вызовами инструментов — он сбрасывается к корню почти всегда, а
    # однажды остался на каталоге самого скила, где лежат ЕГО PLAN.md и run-config.json.
    # Страж тогда предложил ей запустить петлю на чужом продукте.
    #
    # До двигателя цели это стоило бы одного неверного совета. С ним — дороже: -Record
    # пишет замер в историю ТОГО продукта, чей корень ему назвали, и расстояние чужого
    # продукта легло бы в наш счёт застоя. Молча, и с виду правдоподобно.
    #
    # Автопривязка УБРАНА 13.09.2026, через час после того, как я её написала. Она чинила
    # эту беду и делала хуже: ловила момент, когда сессия зашла в каталог скила за
    # обновлением, и закрепляла его как продукт — после чего расстояние считалось по
    # чужому состоянию, а страж требовал закрывать шаги из PLAN.example.md.
    #
    # Разницы между «решила работать здесь» и «проходила мимо» у механизма нет и быть не
    # может: у прохода мимо нет своего признака. Поэтому продукт объявляется командой
    # mode.ps1 -Product, и это решение, а не совпадение. Не объявлен — замер не пишется,
    # и причина идёт в след: лучше не мерить, чем мерить чужое.
    #
    # Второе уточнение claude-71, тем же днём: каталог, который видит страж, — вероятно,
    # каталог инструмента PowerShell, а не Bash; у первого он переживает вызовы, у второго
    # сбрасывается. Это объясняет, почему «прилипло» именно к скилу, куда она много раз
    # заходила запуском woody.ps1. На решение это не влияет — угадывать нельзя ни по
    # одному из них, — но записано, чтобы следующий не искал заново.
    $driftRoot = if ($productRoot -and $cfgPath -and (Test-Path -LiteralPath $cfgPath -PathType Leaf)) { $productRoot } else { $null }

    if ($driftRoot) {
        $driftPath = Join-Path (Split-Path -Parent $PSScriptRoot) 'scripts\goal-drift.ps1'
        if (Test-Path -LiteralPath $driftPath -PathType Leaf) {
            $noteTG = ''
            if ($msg -match '(?m)^\s*Фаза\s+([^\r\n·]+?)\s*·\s*([^\r\n·]+?)\s*·') {
                $noteTG = ($Matches[1].Trim() + ' · ' + $Matches[2].Trim())
            }
            $driftOut = Join-Path $StateDir "woody-drift-$SessionKey.out"
            $driftCode = $null
            try {
                $dp = Start-Process -FilePath 'powershell.exe' -WindowStyle Hidden -PassThru -Wait `
                      -ArgumentList @('-NoProfile','-ExecutionPolicy','Bypass','-File',$driftPath,
                                      '-ProductRoot',$driftRoot,'-Record','-Note',$noteTG) `
                      -RedirectStandardOutput $driftOut -RedirectStandardError ($driftOut + '.err')
                $driftCode = $dp.ExitCode
            } catch { $driftCode = $null }

            if ($null -ne $driftCode -and $driftCode -ne 0) {
                $why = ''
                try { $why = (Get-Content -LiteralPath $driftOut -Raw -Encoding UTF8).Trim() } catch { }
                # Код 2 — эскалация. У ВЕРЫ это было найденным дефектом: вердикт объявлялся,
                # а цикл шёл дальше и кончался PASS — «человеческий гейт не имел никакого
                # действия на плоскость управления». Поэтому эскалация ЗАКРЫВАЕТ ход, а
                # торможение — предупреждает, не блокируя: оно про сужение шага, а не про
                # остановку работы.
                if ($driftCode -eq 2) {
                    $missing += "ДВИГАТЕЛЬ ЦЕЛИ: ЭСКАЛАЦИЯ. Ход к цели не двигает, и это уже не " +
                                "случайность. $why`nЛибо назови шаг плана, который закроется " +
                                "СЛЕДУЮЩИМ ходом, либо объяви работу вне плана строкой «Фаза вне " +
                                "плана · <причина>», либо зови ЛПР: продолжать тем же способом " +
                                "значит платить за то же ещё раз."
                } elseif ($driftCode -eq 3) {
                    # «Ждёт ЛПР» ход не блокирует: работа кончилась не по вине сессии.
                    # Но и молчать нельзя — иначе исчерпанная работа выглядит как обычный
                    # ход, и остаток, который держит человек, никто ему не назовёт.
                    $driftWarn = "ДВИГАТЕЛЬ ЦЕЛИ: работой закрывать нечего, остаток у ЛПР. $why"
                } else {
                    $driftWarn = "ДВИГАТЕЛЬ ЦЕЛИ: торможение. $why"
                }
            }
        }
    }

    # ПРОСТОЙ ПРОВЕРЯЕТСЯ ПЛАНОМ, А НЕ ДОВЕРИЕМ. Найдено ЛПР 13.09.2026: ведущая закончила
    # ход словами «Дальше: ничего — единственный открытый шаг ведёт ветвь», и страж пропустил,
    # потому что причина была названа. По существу это неправда: в плане оставался открытый
    # шаг, от ветви не зависевший. «Нечего делать» подменилось на «нечего делать в этой ветке
    # мысли» — и снаружи сон неотличим от исчерпанной работы.
    #
    # Проверка механическая: страж и так читает план для задачи 4. Если в нём есть ОТКРЫТЫЙ
    # шаг, который НЕ гейт ЛПР, — заявленный простой ложен. Гейты не считаются: ожидание
    # человека это законная остановка, ради того они в плане и различаются.
    #
    # Разбираются оба формата плана, как и woody.ps1: таблица (гейт выразим только в ней) и
    # список с флажками. Не разобрали ничего — проверка молчит: пустой охват здесь дал бы
    # ложное обвинение, а не находку.
    if ($productRoot -and ($msg -match '(?m)^\s*Дальше\s*:\s*ничего\b')) {
        $planPathIdle = Join-Path $productRoot 'PLAN.md'
        if (Test-Path -LiteralPath $planPathIdle -PathType Leaf) {
            $planIdleText = ''
            try { $planIdleText = Get-Content -LiteralPath $planPathIdle -Raw -Encoding UTF8 } catch { }
            $openNonGate = @()
            foreach ($line in ($planIdleText -split "`r?`n")) {
                # Таблица: | № | Шаг | Ступень | Исполнитель | Состояние |
                if ($line -match '^\s*\|\s*\d+\s*\|') {
                    $cells = @($line -split '\|' | ForEach-Object { $_.Trim() })
                    if ($cells.Count -lt 6) { continue }
                    $tierCell = $cells[3]
                    $stateCell = $cells[5]
                    if ($tierCell -match '^\s*(—|-{1,2}|гейт)' -or $stateCell -match 'гейт') { continue }
                    if ($stateCell -match 'закрыт|сделан|готов') { continue }
                    $openNonGate += $cells[2]
                }
                # Список: - [ ] ступень Заголовок :: команда
                elseif ($line -match '^\s*-\s*\[\s\]\s*(\S+)\s+(.+)$') {
                    $openNonGate += $Matches[2]
                }
            }
            if ($openNonGate.Count -gt 0) {
                $names = ($openNonGate | Select-Object -First 3) -join '; '
                $missing += "ПРОСТОЙ ОБЪЯВЛЕН, А ПЛАН ГОВОРИТ ИНОЕ: открытых шагов, не являющихся " +
                            "гейтами ЛПР — $($openNonGate.Count). Ближайшие: $names. " +
                            "Работа не исчерпана: либо возьми шаг, либо закрой его в плане, либо " +
                            "переведи в гейт с названной причиной. «Ничего» при открытом шаге — это сон, " +
                            "а не исчерпанная работа."
            }
        }
    }

    # УХОД В СТОРОНУ ВИДЕН СЧЁТОМ, А НЕ ГЛАЗОМ ЛПР. Прямое указание 13.09.2026.
    # Поймано на живом случае: план продукта air-worker вёл к релизу шагами 5 и 6, а
    # ведущая правила инструмент (air-woody) и писала «Фаза 78 · Сон закрыт механизмом» —
    # такого шага в плане нет. Работа была настоящей, но плана она не двигала, и заметить
    # это мог только человек. Заметил.
    #
    # Проверка механическая: фаза обязана называть НОМЕР шага плана. Работа вне плана
    # законна — диагностика, ответ на вопрос, разбор отказа, — но объявляется ЯВНО:
    # «Фаза вне плана · <причина>». Молчаливый уход этим не запрещён, а сделан видимым:
    # через неделю счётом видно, сколько ходов шло мимо плана.
    if ($productRoot -and ($msg -match '(?m)^\s*Фаза\s+(\S+)')) {
        $phaseToken = $Matches[1]
        $planPathPh = Join-Path $productRoot 'PLAN.md'
        $offPlan = $msg -match '(?m)^\s*Фаза\s+вне\s+плана\s*·'
        if (-not $offPlan -and (Test-Path -LiteralPath $planPathPh -PathType Leaf)) {
            $planPhText = ''
            try { $planPhText = Get-Content -LiteralPath $planPathPh -Raw -Encoding UTF8 } catch { }
            $planSteps = @()
            # ЗАКРЫТЫЕ ШАГИ ТОЖЕ СЧИТАЮТСЯ ДОПУСТИМОЙ ФАЗОЙ. Прежний образец искал голое
            # число, а закрытый шаг записан зачёркнутым (`| ~~11~~ |`) и под него не
            # подходил. Следствие: ход, отчитывающийся о шаге, который в нём же и закрыли,
            # объявлялся уходом от плана — то есть страж требовал объявить работой «вне
            # плана» ровно ту работу, которой план и двигали. Поймано им же на мне
            # 13.09.2026, в ходе, закрывшем шаг 11.
            foreach ($line in ($planPhText -split "`r?`n")) {
                if ($line -match '^\s*\|\s*(?:~~)?\s*(\d+)\s*(?:~~)?\s*\|') { $planSteps += $Matches[1] }
            }
            # Пустой охват — молчим: план может быть списком без номеров, и ложное
            # обвинение здесь хуже пропущенного ухода.
            if ($planSteps.Count -gt 0 -and ($phaseToken -match '^\d+$') -and ($planSteps -notcontains $phaseToken)) {
                $missing += "УХОД ОТ ПЛАНА: фаза $phaseToken не соответствует ни одному шагу плана " +
                            "(в нём шаги: $($planSteps -join ', ')). Работа вне плана законна, но " +
                            "объявляется явно строкой «Фаза вне плана · <причина>» — иначе уход " +
                            "виден только человеку, а должен быть виден счётом."
            }
        }
    }

    # ЗАДАЧА 5, 12.09.2026, прямое указание ЛПР: частичный выход из режима запрещён так же,
    # как и полный. Различаем по следу, а не по намерению: конфигурации нет вовсе — сессия
    # работает вне продукта, пропускаем; конфигурация есть, а ключа ladder в ней нет —
    # лестницу сняли намеренно, это частичный выход, и он требует того же согласования, что
    # -Off. Согласование расходуется: одно открывает один выход.
    if ($cwd -and $cfgPath -and (Test-Path -LiteralPath $cfgPath -PathType Leaf) -and $cfg -and
        -not $cfg.PSObject.Properties['ladder']) {
        $approvalPath5 = Join-Path $stateDir "woody-mode-off-approved-$sessionKey.json"
        if (Test-Path -LiteralPath $approvalPath5) {
            try {
                @{ enabled = $false; subagents = $mode.subagents; session_id = $sessionKey; updated = (Get-Date).ToString('s') } |
                    ConvertTo-Json -Depth 3 | Set-Content -LiteralPath $modeFile -Encoding UTF8
                Remove-Item -LiteralPath $approvalPath5 -Force
            } catch { }
        } else {
            $missing += "сокращение режима равносильно выходу из него: $cfgPath существует, а ключа " +
                         "ladder в нём нет — лестницу сняли, не выключив режим. От тебя жду: согласование " +
                         "ЛПР файлом $approvalPath5, как при полном выходе — а не снятие молча."
        }
    }

    # ЗАДАЧА 3, 12.09.2026: судья зовётся отдельным процессом и НЕ ВЛИЯЕТ на код возврата
    # этого хука — красный судья такое же рабочее состояние, как и зелёный.
    try { Invoke-WoodyVerdict -Cwd $productRoot -SessionKey $sessionKey -StateDir $stateDir } catch { }

    # СЛЕД РЕШЕНИЯ. Заведён 12.09.2026 по запросу ЛПР: «я не вижу результата ни одного
    # стража». Пропуск и отказ пишутся ОБА — иначе в следе видно только плохое, а
    # молчание исправного стража снова неотличимо от его отсутствия.
    #
    # НАЙДЕНО ПРОБОЙ 12.09.2026: здесь стояло $sessionId вместо $sessionKey — переменной
    # с таким именем в скрипте нет, она разрешалась в $null, и Write-WoodyTrace (параметр
    # -SessionId Mandatory) бросала исключение ПРИ СВЯЗЫВАНИИ параметра — то есть ДО входа
    # в тело функции, где её собственный try/catch поймать это уже не мог. Исключение
    # улетало в общий catch хука (правило 2: «пропускает при любой своей ошибке») и хук
    # отдавал exit 0, даже когда $missing был не пуст — страж заведомо не мог отказать,
    # и след не писался вовсе. Прогон на stdin {"session_id":"...","hook_event_name":"Stop",
    # "last_assistant_message":"короткий ответ без формата"} воспроизвёл это: код 0 вместо 2.
    #
    # Почему блок обёрнут ОТДЕЛЬНЫМ try/catch, а не полагается на общий catch хука: общий
    # catch реализует правило 2 для ЛОГИКИ СТРАЖА (проверки формата, судья, петля, частичный
    # выход) — там поломка обязана пропускать ход, это и есть контракт. Но след — не логика
    # стража, а диагностика ЕЁ РАБОТЫ; если его поломка тоже уходит в тот же catch, она
    # достаётся туда же, куда и настоящий отказ формата, и превращает «страж отказал бы, но
    # не смог записать почему» в неотличимое «страж пропустил». Локальный try/catch не даёт
    # ошибке следа подняться выше этой точки: exit ниже по-прежнему решает $missing.Count,
    # а не судьба Write-WoodyTrace. Обнаружить будущую поломку самого следа (а не эту,
    # конкретную) можно тем же путём, каким её увидел ЛПР 12.09.2026 — mode.ps1 -Trace
    # печатает ТРЕВОГУ, если след пуст при включённом режиме (см. ниже по скрипту режима).
    try {
        Write-WoodyTrace -SessionId $sessionKey -Role 'страж' -Event 'Stop' `
            -Decision $(if ($missing.Count -eq 0) { 'пропустил' } else { 'отклонил' }) `
            -Detail $(if ($missing.Count -eq 0) { 'формат полон' } else { ($missing -join '; ') }) `
            -Extra @{ subagents = $running.Count; ladder = $(if ($ladder) { 'известна' } else { 'не объявлена' })
                      # Торможение двигателя цели ход НЕ блокирует — оно про сужение шага, а не
                      # про остановку. Но потеряться оно не имеет права: невидимый вердикт и есть
                      # тот дефект ВЕРЫ, ради которого механизм восстановлен. Поэтому торможение
                      # ложится в след, и три подряд сами дорастают до эскалации, которая блокирует.
                      goal_drift = $(if ($driftWarn) { $driftWarn } elseif ($undeclaredNote) { $undeclaredNote } else { 'без замечаний' })
                      product = $(if ($productRoot) { $productRoot } else { 'не объявлен' }) }
    } catch { }

    if ($missing.Count -eq 0) { exit 0 }

    $why = "РЕЖИМ ОРКЕСТРАЦИИ ДЯТЛА ВУДИ ВКЛЮЧЁН, а сообщение не держит утверждённый формат. " +
           "Не хватает: " + ($missing -join '; ') + ". " +
           "Формат согласован ЛПР (AIR_VIBECODING v1.49) и не меняется под предлогом экономии токенов " +
           "или удобства чтения — Ядро п. 1.2. Поле «кто исполняет» называет уровень лестницы и " +
           "исполнителя: скрипт, субагент N/M, ведущая, ЛПР. Слово «мы» запрещено — оно скрывает " +
           "исполнителя. «Строю» без строки состояния — это выход из режима, а не краткость. " +
           "«Не жду ничего» пишется прямо и только после сверки четырёх пунктов: невлитые ветки, " +
           "незакрытые развилки, гейты без ЛПР, решения, отложенные словом «потом». Пятый для Вуди: " +
           "остановка судьи с кодом 2 всегда идёт в «От тебя жду». " +
           "Выйти из режима, целиком или частично, можно только согласованием ЛПР, не молчанием."
    if ($running.Count -gt 0) {
        $why += " Незавершённых субагентов: $($running.Count). Их результат не замещается собственными заметками — дождись отчёта."
    }

    # Событие Stop: сообщение берётся из stderr при коде 2. Байты напрямую — PowerShell 5.1
    # иначе отдаёт кириллицу как «???????».
    $err = [Console]::OpenStandardError()
    $bytes = [System.Text.Encoding]::UTF8.GetBytes($why)
    $err.Write($bytes, 0, $bytes.Length)
    $err.Flush()
    exit 2
}
catch {
    # Правило 2: сломанный страж пропускает, а не держит.
    exit 0
}
