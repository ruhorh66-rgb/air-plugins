<#
.SYNOPSIS
  Страж хода: сверяет числа в сообщении с замером на диске. Ничего не знает о словах.

.DESCRIPTION
  ЧТО ЗДЕСЬ БЫЛО И ПОЧЕМУ ЭТОГО БОЛЬШЕ НЕТ.

  До 13.09.2026 этот файл был 573 строки и проверял ФОРМУ сообщения: семь обязательных
  строк, строку состояния, строку на каждого субагента, ступень в каждой такой строке,
  запрет простоя, объявление ухода с плана. Разбор того же дня дал число: из 9 400 строк
  механизма 4 264 надзирали за формой сообщения, и за сутки эта половина не улучшила НИ
  ОДНОГО продукта. Она около десяти раз остановила ход, и лечением каждый раз была
  переписанная модель абзаца. Вторая половина — та, что мерит продукт, — за те же сутки
  нашла расхождение плана и вердикта, пласт необъявленной работы, три файла с неверной
  кодировкой и невидимый frontmatter.

  Вывод ЛПР, дословно: «который день пытаемся настроить инструкцию взаимодействия, и
  только всё усложняем». Причина найдена: НАДЗИРАТЬ ЗА РЕЧЬЮ НЕЛЬЗЯ. Речь подстроится под
  любой надзор — на то она и речь, — и надзор приходится удлинять. Это и был «который
  день».

  Теперь страж задаёт один вопрос вместо семи проверок:

      СОВПАДАЮТ ЛИ ЧИСЛА В СООБЩЕНИИ С ЗАМЕРОМ НА ДИСКЕ?

  Подстроиться под это нельзя. Чтобы отчёт сошёлся, работа должна быть сделана, а судья
  прогнан: числа берутся не из текста хода, а из `air-worker drift -json` и из
  `git status --porcelain`, которые страж запускает сам.

  ТРИ ПРАВИЛА, И ВСЕ ТРИ — ЗАМЕР, А НЕ ВКУС.

  1. Отчёта в ходе нет вовсе. Это не надзор за речью, а требование замера: ход без
     замера — ход без доказательства.
  2. Число в отчёте не совпало с замером. Показывается оба, и где именно разошлось.
  3. Дерево изменилось с прошлого хода, а вердикт судьи не переписан. Работа была,
     судья её не видел — и отчёт, каким бы верным он ни выглядел, говорит о прошлом.

  ПОЧЕМУ СТРАЖ НЕ ЗОВЁТ СУДЬЮ САМ. Замер: судья ASW идёт 28 секунд, drift — 0,08 с.
  Судья гоняется РАЗ В ХОД, и гоняет его модель командой `air-worker report`; страж
  проверяет дёшево то, что уже посчитано. Страж, добавляющий полминуты к каждому ходу,
  будет снят первым же движением — вместе с пользой.

  ЧЕГО ЭТОТ СТРАЖ НЕ ЛОВИТ, И ЭТО НАЗВАНО ЧЕСТНО. Он не отличает судью, прогнанного
  минуту назад, от прогнанного час назад, если за этот час ничего не менялось — и не
  должен: при неизменившемся дереве вчерашний вердикт и есть сегодняшний. Правило 3
  закрывает ровно тот случай, когда это перестаёт быть верным.

  ДВА ПРАВИЛА БЕЗОПАСНОСТИ, оба сохранены от прежнего стража:

  1. Страж молчит, пока продукт сессии не ОБЪЯВЛЕН. Объявление — явный акт человека
     (`mode.ps1 -Product`), и без него сверять нечего: угадывание продукта по рабочему
     каталогу один раз уже поймало каталог, мимо которого сессия проходила, и двигатель
     считал расстояние по чужому состоянию.
  2. Страж ПРОПУСКАЕТ при любой своей ошибке. Сторож, ломающий работу, когда сломался
     сам, хуже отсутствующего: его снимут вместе с пользой.
#>
[CmdletBinding()]
param()

# Fail-open с первой строки: что бы ни случилось ниже, по умолчанию мы пропускаем.
$ErrorActionPreference = 'Continue'

# СЛЕД РЕШЕНИЙ. Без него молчащий страж неотличим от пропустившего — а это ровно тот
# класс отказа, который стоил суток 11-12.09.2026: состояние показывало «ВКЛЮЧЁН», а
# хук не запускался вовсе, и никто этого не видел. Читается `mode.ps1 -Trace`.
#
# Отсутствие файла библиотеки хук не роняет, и КАЖДЫЙ вызов обёрнут своим try/catch:
# режим отладки 12.09.2026 нашёл живым прогоном, что ошибка в вызове следа улетала в
# общий catch и хук отдавал exit 0 вместо честного отказа — поломка ОДНОГО
# диагностического механизма маскировала отказ ДРУГОГО, реального.
$__traceLib = Join-Path (Split-Path -Parent $PSScriptRoot) 'lib\trace.ps1'
if (Test-Path -LiteralPath $__traceLib) { . $__traceLib }

function Write-GuardTrace([string]$key, [string]$decision, [string]$detail) {
    try {
        if (Get-Command Write-WoodyTrace -ErrorAction SilentlyContinue) {
            Write-WoodyTrace -SessionId $key -Role 'страж' -Event 'Stop' -Decision $decision -Detail $detail
        }
    } catch { }
}

function Get-WoodyExe {
    # Бинарник ищется по ответу, а не по прибитому пути: перекрытие, каталог плагина,
    # затем известное расположение. От hooks до корня продукта ТРИ уровня —
    # hooks -> woody -> skills -> корень; прежняя редакция соседнего хука поднималась
    # на два, молча не находила bin и не делала ничего, ничем об этом не сообщая.
    if ($env:AIR_WORKER_EXE -and (Test-Path -LiteralPath $env:AIR_WORKER_EXE)) { return $env:AIR_WORKER_EXE }
    if ($env:CLAUDE_PLUGIN_ROOT) {
        $p = Join-Path $env:CLAUDE_PLUGIN_ROOT 'bin\air-worker.exe'
        if (Test-Path -LiteralPath $p) { return $p }
    }
    $root = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $PSScriptRoot))
    $p = Join-Path $root 'bin\air-worker.exe'
    if (Test-Path -LiteralPath $p) { return $p }
    return $null
}

function Deny([string]$key, [string]$short, [string]$reason) {
    # Код 2 — отказ, который харнесс показывает модели. Текст называет, ЧТО СДЕЛАТЬ,
    # а не чем сообщение плохо: страж, объясняющий стиль, возвращает нас к надзору
    # за речью через другую дверь.
    Write-GuardTrace $key 'отклонил' $short
    [Console]::Error.WriteLine($reason)
    exit 2
}

try {
    # Stdin читается ЯВНО как UTF-8, а не через [Console]::In. Найдено прогоном
    # 12.09.2026: страж блокировал ход, в котором всё было на месте. Харнесс шлёт UTF-8,
    # а [Console]::In в Windows PowerShell 5.1 декодирует кодовой страницей консоли —
    # кириллица превращается в мусор, и совпадения не будет НИКОГДА.
    $stdinStream = [Console]::OpenStandardInput()
    $stdinReader = New-Object System.IO.StreamReader($stdinStream, (New-Object System.Text.UTF8Encoding($false)))
    $raw = $stdinReader.ReadToEnd()
    if (-not $raw) { exit 0 }
    $ev = $raw | ConvertFrom-Json

    $sessionKey = [string]$ev.session_id
    if (-not $sessionKey) { exit 0 }

    $stateDir = Join-Path $env:ProgramData 'AIR OS\State'
    if (-not (Test-Path -LiteralPath $stateDir)) { exit 0 }

    # Продукт ОБЪЯВЛЕН, а не угадан. Нет объявления — сверять нечего, страж молчит.
    $prodFile = Join-Path $stateDir "woody-product-$sessionKey.json"
    if (-not (Test-Path -LiteralPath $prodFile)) { exit 0 }
    $productRoot = $null
    try {
        $pb = Get-Content -LiteralPath $prodFile -Raw -Encoding UTF8 | ConvertFrom-Json
        if ($pb.path -and (Test-Path -LiteralPath $pb.path -PathType Container)) { $productRoot = [string]$pb.path }
    } catch { }
    if (-not $productRoot) { exit 0 }

    $exe = Get-WoodyExe
    if (-not $exe) { exit 0 }

    $msg = [string]$ev.last_assistant_message
    if (-not $msg) { exit 0 }

    # --- ЗАМЕР НА ДИСКЕ. Числа берутся отсюда, а не из текста хода. ---------------

    $drift = $null
    try { $drift = (& $exe drift -product $productRoot -json 2>$null | Out-String) | ConvertFrom-Json } catch { }
    if (-not $drift) {
        # «Нечем сверять» и «сошлось» — разные ответы, и в следе они обязаны различаться.
        Write-GuardTrace $sessionKey 'пропущено-нечем' 'drift не ответил: сверять нечем'
        exit 0
    }

    $treeSig = ''
    try { $treeSig = (& git -C $productRoot status --porcelain 2>$null | Out-String) } catch { }
    $treeChanged = @($treeSig -split "`r?`n" | Where-Object { $_.Trim() -ne '' }).Count

    # Состояние прошлого хода этой сессии. Нужно правилу 3 и больше ничему.
    $turnFile = Join-Path $stateDir "woody-turn-$sessionKey.json"
    $prev = $null
    if (Test-Path -LiteralPath $turnFile) {
        try { $prev = Get-Content -LiteralPath $turnFile -Raw -Encoding UTF8 | ConvertFrom-Json } catch { }
    }

    $verdictPath = Join-Path $productRoot '.goal-verdict.json'
    $verdictStamp = ''
    if (Test-Path -LiteralPath $verdictPath) {
        $verdictStamp = (Get-Item -LiteralPath $verdictPath).LastWriteTimeUtc.ToString('o')
    }

    # Запись состояния хода АТОМАРНА. В сессии, где зарегистрированы две копии стража,
    # обе пишут один файл одновременно, и вторая получает «Stream was not readable».
    # Прошлая починка закрыла файл вердикта и оставила соседний — класс чинится целиком,
    # иначе уцелевает рядом.
    $save = {
        $tmp = "$turnFile.$PID.tmp"
        try {
            @{ tree = $treeSig; verdict_stamp = $verdictStamp; at = (Get-Date).ToString('s') } |
                ConvertTo-Json | Set-Content -LiteralPath $tmp -Encoding UTF8
            Move-Item -LiteralPath $tmp -Destination $turnFile -Force -ErrorAction Stop
        } catch { Remove-Item -LiteralPath $tmp -Force -ErrorAction SilentlyContinue }
    }

    # --- ПРАВИЛО 1: отчёт в ходе есть? -------------------------------------------

    $mDist = [regex]::Match($msg, '(?m)^\s*Расстояние\s*:\s*(\d+|нечем измерить)\s*\S?\s*застой\s*(\d+)\s*\S?\s*вердикт\s*(\S+)')
    $mTree = [regex]::Match($msg, '(?m)^\s*Дерево\s*:\s*изменено файлов\s*(\d+)')
    if (-not $mDist.Success) {
        & $save
        Deny $sessionKey 'отчёта в ходе нет' ("ХОД БЕЗ ЗАМЕРА. В сообщении нет строки отчёта, а продукт объявлен: $productRoot`n" +
              "Прогони и вставь вывод дословно:`n" +
              "    air-worker report -product `"$productRoot`"`n" +
              "Это не требование к форме сообщения: без замера ход ничем не доказан. " +
              "Слова остаются твоими — числа печатает программа.")
    }

    # --- ПРАВИЛО 2: числа совпадают с замером? -----------------------------------

    $bad = @()

    $saidDist = $mDist.Groups[1].Value
    $realDist = if ($null -eq $drift.distance) { 'нечем измерить' } else { [string]$drift.distance }
    if ($saidDist -ne $realDist) { $bad += "расстояние: в ходе «$saidDist», на диске «$realDist»" }

    $saidStall = [int]$mDist.Groups[2].Value
    if ($saidStall -ne [int]$drift.stall_moves) { $bad += "застой: в ходе $saidStall, на диске $($drift.stall_moves)" }

    $saidVerdict = $mDist.Groups[3].Value.Trim()
    if ($saidVerdict -ne [string]$drift.verdict) { $bad += "вердикт двигателя: в ходе «$saidVerdict», на диске «$($drift.verdict)»" }

    if ($mTree.Success) {
        $saidTree = [int]$mTree.Groups[1].Value
        if ($saidTree -ne $treeChanged) { $bad += "изменённых файлов: в ходе $saidTree, на самом деле $treeChanged" }
    }

    if ($bad.Count -gt 0) {
        & $save
        Deny $sessionKey ("числа разошлись: " + ($bad -join '; ')) ("ЧИСЛА В ХОДЕ НЕ СОВПАЛИ С ЗАМЕРОМ на ${productRoot}:`n  - " + ($bad -join "`n  - ") + "`n" +
              "Отчёт устарел или вставлен не дословно. Прогони заново и вставь вывод как есть:`n" +
              "    air-worker report -product `"$productRoot`"")
    }

    # --- ПРАВИЛО 3: работа была, а судья её не видел? -----------------------------

    if ($prev -and $null -ne $prev.tree -and $prev.tree -ne $treeSig -and
        $verdictStamp -and $prev.verdict_stamp -eq $verdictStamp) {
        & $save
        Deny $sessionKey 'дерево изменилось, вердикт судьи прежний' ("ДЕРЕВО ИЗМЕНИЛОСЬ, А СУДЬЯ НЕ ПРОГНАН. С прошлого хода в $productRoot файлы " +
              "поменялись, но вердикт судьи остался прежним: отчёт говорит о состоянии ДО работы.`n" +
              "    air-worker report -product `"$productRoot`"`n" +
              "Вердикт из файла — след прошлого прогона, а не состояние продукта.")
    }

    & $save
    Write-GuardTrace $sessionKey 'пропустил' "числа сошлись: расстояние $realDist, изменённых файлов $treeChanged"
}
catch { }

exit 0
