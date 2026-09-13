<#
.SYNOPSIS
  Универсальный судья цели для «Дятла Вуди».

.DESCRIPTION
  Возвращает 0 только когда цель достигнута. Судьёй не может быть модель: она объявляет
  победу рано. Решение «готово» принимает код возврата этого скрипта.

  Три кода возврата, и третий — главное отличие от прежних судей продуктов:

    0  ЦЕЛЬ ДОСТИГНУТА    — всё проверено и пройдено;
    1  ЦЕЛЬ НЕ ДОСТИГНУТА — проверено и не пройдено; работать дальше есть над чем;
    2  НЕ ПРОВЕРЕНО       — проверять нечем: нет реестра фактов, нет команды проверки,
                            нет инструмента. Работать дальше бессмысленно, нужен человек.

  Отсутствие доказательства не является нулевой оценкой. Судья ASW считал пропавший
  реестр фактов за «закрыто 0 из 16» — и «не доказано» становилось неотличимо от
  «доказательство уничтожено». Ровно так 11.09.2026 пропажа реестра выглядела как
  недоделанная работа.

  Совместимость: Windows PowerShell 5.1 и PowerShell 7. Файл сохраняется UTF-8 с BOM —
  без него 5.1 читает кириллицу как ANSI.
#>
[CmdletBinding()]
param(
    [string]$ProductRoot = (Get-Location).Path,
    [string]$ConfigPath,
    [int]$MinFacts = -1
)

try { [Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false) } catch { }
$ErrorActionPreference = 'Continue'

if (-not $ConfigPath) { $ConfigPath = Join-Path $ProductRoot 'run-config.json' }
if (-not (Test-Path -LiteralPath $ConfigPath)) {
    Write-Output "НЕ ПРОВЕРЕНО: нет конфигурации $ConfigPath"
    exit 2
}
$cfg = Get-Content -LiteralPath $ConfigPath -Raw -Encoding UTF8 | ConvertFrom-Json
$jc = $cfg.judge
if (-not $jc) {
    Write-Output 'НЕ ПРОВЕРЕНО: в конфигурации нет раздела judge'
    exit 2
}

$failed = @()      # проверено и не пройдено
$unknown = @()     # проверять нечем
$passed = @()

# --- проверки ---------------------------------------------------------------
# Каждая идёт ОТДЕЛЬНЫМ процессом. Вызванная через `&`, она выполняется в этом же
# процессе, и её `exit 1` завершил бы самого судью: он отдал бы чужой код вместо
# собственного вердикта, а остальные условия остались бы непроверенными.
foreach ($chk in @($jc.checks)) {
    if (-not $chk) { continue }
    $name = if ($chk.name) { [string]$chk.name } else { 'проверка без имени' }
    $logPath = $null
    if ($chk.log) { $logPath = Join-Path $ProductRoot ([string]$chk.log) }

    if ($chk.script) {
        $scriptPath = Join-Path $ProductRoot ([string]$chk.script)
        if (-not (Test-Path -LiteralPath $scriptPath)) {
            $unknown += "$name — нечем: нет $scriptPath"
            continue
        }
        # $args БРАТЬ НЕЛЬЗЯ: это автоматическая переменная PowerShell, хранящая
        # несвязанные аргументы вызова. Работало, но поведение зависело от того, как
        # позвали сам судью. Тот же класс, что $pid в mode.ps1 — имя занято языком.
        # ЗОВЁМ ЧЕРЕЗ -Command, А НЕ -File, РАДИ ОДНОЙ СТРОКИ ПРЕАМБУЛЫ. Под -File
        # дочерний powershell пишет перенаправленный вывод в своей КОНСОЛЬНОЙ кодировке,
        # и если она кириллицу не представляет — подставляет вопросительные знаки ПРИ
        # ЗАПИСИ. Потеря происходит у источника, и никаким чтением её не вернуть:
        # проверено прогоном 12.09.2026, декодер по кодовой странице не помог, потому
        # что в файле лежали уже '?'. Преамбула ставит UTF-8 до первой строки вывода.
        $quoted = "'" + ($scriptPath -replace "'", "''") + "'"
        $tail = ''
        if ($chk.args) {
            $tail = ' ' + (@($chk.args | ForEach-Object { "'" + ([string]$_ -replace "'", "''") + "'" }) -join ' ')
        }
        $preamble = '[Console]::OutputEncoding=[System.Text.UTF8Encoding]::new($false); $OutputEncoding=[System.Text.UTF8Encoding]::new($false); '
        $psArgs = @('-NoProfile','-ExecutionPolicy','Bypass','-Command', ($preamble + '& ' + $quoted + $tail + '; exit $LASTEXITCODE'))

        # ВЕТВИ БЫЛИ НЕСИММЕТРИЧНЫ, и это порождало дефект в КАЖДОМ продукте. Ветвь
        # command ставила рабочий каталог (Push-Location ниже), а script не ставила
        # ничего: проверка запускалась отдельным процессом и должна была сама угадать,
        # где продукт. Очевидный способ угадать — $PSScriptRoot — под powershell.exe
        # -File пуст на момент разбора блока param, и проверка падала ДО первой своей
        # проверки. Судья видел код 1 и печатал «не прошла», хотя она не начиналась.
        # Найдено второй машиной 12.09.2026 ценой потерянного прогона.
        $outLog = if ($logPath) { $logPath } else { Join-Path ([IO.Path]::GetTempPath()) ("woody-check-" + [guid]::NewGuid().ToString('N').Substring(0,8) + '.log') }
        $errLog = "$outLog.err"
        $p = Start-Process -FilePath 'powershell.exe' -ArgumentList $psArgs -Wait -PassThru -WindowStyle Hidden `
             -WorkingDirectory $ProductRoot -RedirectStandardOutput $outLog -RedirectStandardError $errLog
        if ($p.ExitCode -eq 0) {
            $passed += $name
            if (-not $logPath) { Remove-Item -LiteralPath $outLog, $errLog -Force -ErrorAction SilentlyContinue }
            continue
        }

        # ВЫВОД ПРОВЕРКИ БОЛЬШЕ НЕ ПРОПАДАЕТ. Прежде Start-Process шёл без
        # перенаправления вовсе: весь вывод уходил в скрытое окно, и до человека
        # доживала строка «проверка (код 1)» без причины — ровно то, что судья и
        # должен устранять. Поле log при этом вычислялось и ветвью script не
        # использовалось: конфигурация принималась, поле игнорировалось молча.
        # ЧИТАТЬ ПЕРЕНАПРАВЛЕННЫЙ ВЫВОД КАК UTF-8 НЕЛЬЗЯ. Start-Process пишет его в
        # КОНСОЛЬНОЙ кодировке дочернего процесса, и кириллица превращается в мусор:
        # причина доживает до человека, но нечитаемой. Поймано прогоном 12.09.2026 сразу
        # после того, как перенаправление вообще появилось. Поэтому пробуем UTF-8 и при
        # символах замены перечитываем в кодировке консоли.
        function Read-CheckLog([string]$path) {
            if (-not (Test-Path -LiteralPath $path)) { return @() }
            $bytes = [System.IO.File]::ReadAllBytes($path)
            if ($bytes.Length -eq 0) { return @() }
            $text = [System.Text.Encoding]::UTF8.GetString($bytes)
            if ($text.Contains([char]0xFFFD)) {
                try { $text = [System.Text.Encoding]::GetEncoding([Console]::OutputEncoding.CodePage).GetString($bytes) }
                catch { $text = [System.Text.Encoding]::Default.GetString($bytes) }
            }
            return ($text -split "`r?`n")
        }

        $why = ''
        foreach ($lf in @($outLog, $errLog)) {
            $line = @(Read-CheckLog $lf | Where-Object { $_ -match '\[FAIL\]|ОТКАЗ|НЕЧЕМ|FAIL|Exception|ошибка' }) | Select-Object -First 1
            if ($line) { $why = ' — ' + $line.Trim(); break }
        }
        if (-not $why) {
            $tail = @(Read-CheckLog $outLog | Where-Object { $_.Trim() }) | Select-Object -Last 1
            if ($tail) { $why = ' — ' + $tail.Trim() }
        }
        $failed += "$name (код $($p.ExitCode))$why"
        if (-not $logPath) { Remove-Item -LiteralPath $outLog, $errLog -Force -ErrorAction SilentlyContinue }
        continue
    }

    if ($chk.command) {
        $exe = [string]$chk.command
        $exeArgs = @()
        if ($chk.args) { $exeArgs = @($chk.args | ForEach-Object { [string]$_ }) }
        $resolved = Get-Command $exe -ErrorAction SilentlyContinue
        if (-not $resolved) {
            # Инструмент не резолвится — это «нечем проверить», а не «не прошло».
            # Мерить надо составленным PATH: свой процесс держит копию окружения
            # на момент запуска и после переносов врёт.
            $unknown += "$name — нечем: команда '$exe' не резолвится"
            continue
        }

        # РЕЗОЛВ НЕ ДОКАЗЫВАЕТ НАЛИЧИЯ ИНСТРУМЕНТА, ДОКАЗЫВАЕТ ТОЛЬКО ОТВЕТ.
        # Найдено на второй машине 12.09.2026: в %LOCALAPPDATA%\Microsoft\WindowsApps
        # лежит алиас-заглушка магазина. Get-Command её находит, ветвь «не резолвится»
        # не срабатывает, вызов отвечает «Python was not found» с кодом 9009 — и судья
        # объявлял ЦЕЛЬ НЕ ДОСТИГНУТА. Отсутствующий инструмент превращался в претензию
        # к продукту, то есть третий код обходился любой заглушкой на PATH.
        $sourcePath = [string]$resolved.Source
        if ($sourcePath -and ($sourcePath -match '\\WindowsApps\\')) {
            try {
                $stub = Get-Item -LiteralPath $sourcePath -Force -ErrorAction Stop
                if ($stub.Length -eq 0 -or ($stub.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
                    $unknown += "$name — нечем: '$exe' ведёт на алиас-заглушку магазина ($sourcePath), а не на инструмент"
                    continue
                }
            } catch { }
        }
        Push-Location $ProductRoot
        if ($chk.env) {
            foreach ($kv in $chk.env.PSObject.Properties) {
                Set-Item -Path ("Env:\" + $kv.Name) -Value ([string]$kv.Value) -ErrorAction SilentlyContinue
            }
        }
        if ($logPath) { & $exe @exeArgs *> $logPath } else { & $exe @exeArgs | Out-Null }
        $code = $LASTEXITCODE
        Pop-Location
        if ($code -eq 0) { $passed += $name; continue }

        # 9009 — «команда не найдена» у интерпретатора команд: процесс НЕ исполнился.
        # Это «нечем проверить», а не «не прошло». Второй рубеж после проверки заглушки:
        # первый ловит известный путь WindowsApps, второй — любой случай, когда вызов
        # не состоялся вовсе, включая обманки, о которых мы ещё не знаем.
        if ($code -eq 9009) {
            $unknown += "$name — нечем: команда '$exe' не исполнилась (код 9009: не найдена, хотя и резолвилась)"
            continue
        }

        # Вывод проверки сохраняется, а не выбрасывается: вердикт «не проходит» без
        # причины заставляет строить гипотезы вместо работы.
        $why = ''
        if ($logPath -and (Test-Path -LiteralPath $logPath)) {
            $line = @(Get-Content -LiteralPath $logPath -Encoding UTF8 |
                      Where-Object { $_ -match '^(FAIL|---\s+FAIL|# |panic:|Error|ОШИБКА|.*:\d+:)' }) | Select-Object -First 1
            if ($line) { $why = ' — ' + $line.Trim() }
        }
        $failed += "$name (код $code)$why"
        continue
    }

    $unknown += "$name — нечем: в проверке не задан ни script, ни command"
}

# --- реестр фактов ----------------------------------------------------------
# Живёт С ПРОДУКТОМ и под git. Прежний судья держал его в E:\-4-\ruflo-hive\
# .claude-flow\data\ — в рантайме чужого продукта, вне git и вне периметра бэкапа;
# зачистка 11.09.2026 забрала его вместе с рантаймом.
$want = if ($MinFacts -ge 0) { $MinFacts } elseif ($jc.min_facts) { [int]$jc.min_facts } else { 0 }
$factsLine = $null
if ($want -gt 0) {
    if (-not $jc.checklist) {
        $unknown += "реестр фактов — нечем: judge.checklist не назван, а требуется $want фактов"
    } else {
        $clPath = Join-Path $ProductRoot ([string]$jc.checklist)
        if (-not (Test-Path -LiteralPath $clPath)) {
            # ГЛАВНОЕ отличие: пропажа реестра — это «не проверено», а не «закрыто 0».
            $unknown += "реестр фактов — нечем: нет $clPath"
        } else {
            try {
                $items = @((Get-Content -LiteralPath $clPath -Raw -Encoding UTF8 | ConvertFrom-Json).items)
                $closed = @($items | Where-Object { $_.status -eq 'completed' }).Count

                # ФАКТ, КОТОРЫЙ ЗАКРЫВАЕТ ЧЕЛОВЕК, А НЕ СЕССИЯ. Найдено AIR-ENV-002
                # 13.09.2026 на живом состоянии: три факта из семи требовали повышения
                # прав и перезагрузки, и расстояние до цели не уменьшилось бы НИ ОДНИМ
                # её ходом. Двигатель цели дал бы эскалацию — верное правило и неверный
                # вердикт: он измерял бы не работу ведущей, а границу её полномочий, и
                # честно заблокированный продукт стал бы неотличим от дрейфующего.
                #
                # У ШАГОВ ПЛАНА это уже решено — гейты ЛПР из расстояния исключены. У
                # фактов метки не было вовсе: статус знал только completed и open.
                #
                # МЕТКА ОБЯЗАНА СТОИТЬ. Её опасность назвала та же ENV002: «если метку
                # можно ставить самому себе без основания, она станет удобной». Поэтому
                # gated без поля awaits — это НЕ гейт, а «нечем проверить»: гейт обязан
                # называть, чего именно ждёт человек. И гейты остаются ВИДИМЫ в тексте
                # вердикта, чтобы метка не превратилась в способ спрятать работу.
                $gatedItems = @($items | Where-Object { $_.status -eq 'gated' })
                $gated = $gatedItems.Count
                $gatedNoReason = @($gatedItems | Where-Object { -not $_.awaits })
                if ($gatedNoReason.Count) {
                    $unknown += ("факт помечен гейтом без названной причины (" +
                                 (($gatedNoReason | ForEach-Object { [string]$_.id }) -join ', ') +
                                 "): поле awaits обязано называть, чего ждёт человек — иначе метка прячет работу")
                }
                $script:factsClosed = $closed
                $script:factsGated = $gated
                $factsLine = "фактов закрыто $closed из $want"
                if ($gated -gt 0) { $factsLine += ", из них $gated ждут ЛПР" }
                # Гейт цель НЕ закрывает: продукт с непройденным гейтом не готов. Но и
                # работой он не лечится, поэтому строка вердикта называет его отдельно,
                # а расстояние (ниже, в машинном вердикте) его не считает.
                if ($closed -lt $want) { $failed += $factsLine } else { $passed += $factsLine }
            } catch {
                $unknown += "реестр фактов — нечем: $clPath не разобран ($($_.Exception.Message))"
            }
        }
    }
}

# --- вердикт ----------------------------------------------------------------
# Кладётся ещё и в файл: вывод проходит через консольную трубу, а системная кодировка
# машины кириллицы не содержит — в журналах оставались «???? ?? ??????????». Потеря
# происходит при ЗАПИСИ в трубу, чтением её не исправить.
function Publish-Verdict([string]$text, [int]$code) {
    Write-Output $text
    try { Set-Content -Path (Join-Path $ProductRoot '.goal-verdict') -Value $text -Encoding UTF8 } catch { }

    # РЯДОМ С ТЕКСТОМ — ЧИСЛА. Восстановлено из Goal/Drift Loop ВЕРЫ 13.09.2026 по
    # прямому указанию ЛПР: тот механизм считал расстояние до цели, и у него был
    # найденный контролёром 05.09.2026 дефект-предупреждение, дословно — счётчик
    # брался «из поиска русской подстроки в тексте причины: текст причины меняется
    # свободно, и счётчик молча переставал расти». Здесь ровно тот же риск: «фактов
    # закрыто 8 из 9» читаемо человеком и хрупко для машины. Поэтому числа кладутся
    # отдельным файлом как ФАКТ, а текст остаётся человеку.
    #
    # distance — расстояние до цели: сколько ещё не сделано. Ноль достигается только
    # вместе с кодом 0. Непроверенное в расстояние НЕ СВОРАЧИВАЕТСЯ: у ВЕРЫ
    # неизмеримая метрика помечалась и выводилась из правил, потому что «ноль здесь
    # читался бы как всё в порядке». Поэтому при коде 2 distance равен null, а не сумме.
    # ГЕЙТЫ ИЗ РАССТОЯНИЯ ВЫЧИТАЮТСЯ. Расстояние измеряет то, что сессия МОЖЕТ закрыть
    # работой; факт, ждущий человека, работой не лечится, и держать его в расстоянии
    # значит обвинять сессию в дрейфе за чужое бездействие. В тексте вердикта выше он
    # при этом назван — спрятать работу меткой нельзя.
    $gatedCount = if ($null -ne $script:factsGated) { [int]$script:factsGated } else { 0 }
    $factsShort = if ($want -gt 0 -and $null -ne $script:factsClosed) { [Math]::Max(0, $want - $script:factsClosed - $gatedCount) } else { 0 }
    $checksFailed = @($failed | Where-Object { $_ -ne $factsLine }).Count
    $dist = if ($code -eq 2) { $null } else { $checksFailed + $factsShort }
    $machine = [ordered]@{
        at             = (Get-Date).ToString('s')
        code           = $code
        distance       = $dist
        checks_passed  = @($passed | Where-Object { $_ -ne $factsLine }).Count
        checks_failed  = $checksFailed
        checks_unknown = $unknown.Count
        facts_closed   = $(if ($null -ne $script:factsClosed) { $script:factsClosed } else { $null })
        facts_gated    = $gatedCount
        facts_required = $want
        verdict_text   = $text
        # ЧЕМ ПОСЧИТАНО. Поле завёл бинарник, и AIR-ENV-002 13.09.2026 верно заметила,
        # что оно полезное, но его отсутствие у скрипта делает набор полей несимметричным —
        # а несимметричность двух реализаций одного правила и есть то, что расходится
        # молча. Пока обе живут рядом, по этому полю видно, которая считала.
        by             = "judge.ps1 0.4.0"
    }
    try {
        $json = $machine | ConvertTo-Json -Depth 4
        [IO.File]::WriteAllText((Join-Path $ProductRoot '.goal-verdict.json'), $json, (New-Object Text.UTF8Encoding $false))
    } catch { }
}

if ($unknown.Count) {
    Publish-Verdict ("НЕ ПРОВЕРЕНО: " + ($unknown -join '; ') + $(if ($failed.Count) { "; отдельно не пройдено: " + ($failed -join '; ') } else { '' })) 2
    exit 2
}
if ($failed.Count) {
    Publish-Verdict ("ЦЕЛЬ НЕ ДОСТИГНУТА: " + ($failed -join '; ')) 1
    exit 1
}
$summary = if ($factsLine) { "пройдено проверок $($passed.Count), $factsLine" } else { "пройдено проверок $($passed.Count)" }
Publish-Verdict "ЦЕЛЬ ДОСТИГНУТА: $summary" 0
exit 0
