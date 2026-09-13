<#
    ПОИСК ИНСТРУМЕНТА — ОДНОЙ ФУНКЦИЕЙ НА ВЕСЬ ПРОДУКТ.

    Вынесено 13.09.2026 по разбору AIR-ENV-002, и её довод здесь важнее самого дефекта.

    Правило «инструмент ищется ПО ОТВЕТУ, а не по имени» куплено в продукте трижды:
    прибитый python314 в конфигурации самотеста, оборванный верх лестницы с адресом
    несуществующей npm-установки, ложный красный у судьи от алиаса магазина. Каждый раз
    оно чинилось В ТОМ МЕСТЕ, ГДЕ НАЙДЕНО, — и потому уцелело в четвёртом: в
    check-ladder-reachable.ps1 интерпретатор брался по Get-Command без пробы.

    Цена уцелевшего экземпляра измерена ею же: на её машине Get-Command python отдавал
    алиас магазина (WindowsApps), который отвечает «Python was not found» с кодом 9009, —
    и рушились ТРИ утверждения сразу, причём первое звучало пустым отказом без причины.
    Полчаса ушло на поиск того, чего не хватает её машине. Не хватало не ей.

    Её формулировка и есть основание этого файла: две проверки одного продукта расходились
    в способе искать один и тот же инструмент — «две реализации одного правила расходятся
    молча», только про инструмент, а не про вердикт.

    Отсюда контракт функции:
      - кандидаты по порядку: явная переменная, потом обычные имена;
      - РЕЗОЛВ НЕ ДОКАЗЫВАЕТ НАЛИЧИЯ, ДОКАЗЫВАЕТ ТОЛЬКО ОТВЕТ: каждый кандидат пробуется
        запуском, и годным считается тот, кто ответил;
      - отказ НАЗЫВАЕТ ПРИЧИНУ и перечисляет, что пробовали. «Нечем исполнить» и «не
        справился» — разные исходы, и путать их дорого.
#>

function Resolve-ProductTool {
    <#
    .SYNOPSIS
        Найти инструмент по ответу. Возвращает {Ok, Path, Version, Reason, Tried}.

    .PARAMETER Names
        Кандидаты по порядку. Пустые пропускаются — это позволяет ставить первым
        $env:ПЕРЕМЕННУЮ, не заботясь о том, задана ли она.

    .PARAMETER ProbeArgs
        Чем спрашивать. Для интерпретаторов это обычно короткая программа, печатающая
        версию; для инструментов — их собственный флаг версии.

    .PARAMETER Expect
        Необязательный образец, которому обязан удовлетворить ответ. Нужен там, где
        ненулевой код возврата — не единственная форма обмана: заглушка может ответить
        нулём и напечатать не то.
    #>
    [CmdletBinding()]
    param(
        [Parameter(Mandatory = $true)][string[]]$Names,
        [string[]]$ProbeArgs = @('--version'),
        [string]$Expect = ''
    )

    $tried = New-Object System.Collections.Generic.List[string]
    foreach ($name in $Names) {
        if ([string]::IsNullOrWhiteSpace($name)) { continue }

        $cmd = Get-Command $name -ErrorAction SilentlyContinue
        if (-not $cmd) { $tried.Add("$name — не резолвится"); continue }
        $src = [string]$cmd.Source

        # Известная обманка называется отдельно: диагноз «ведёт на заглушку магазина»
        # экономит человеку полчаса против «не отвечает».
        if ($src -match '\\WindowsApps\\') {
            try {
                $item = Get-Item -LiteralPath $src -Force -ErrorAction Stop
                if ($item.Length -eq 0 -or ($item.Attributes -band [IO.FileAttributes]::ReparsePoint)) {
                    $tried.Add("$name — алиас-заглушка магазина ($src)")
                    continue
                }
            } catch { }
        }

        # Проба ЗАПУСКОМ. Нативные инструменты пишут предупреждения в поток ошибок, и при
        # ErrorActionPreference=Stop это роняет вызов, хотя команда отработала: судить
        # надо по коду возврата, а не по тому, что что-то попало в stderr.
        $prev = $ErrorActionPreference
        $ErrorActionPreference = 'Continue'
        $answer = ''
        $code = -1
        try {
            $answer = (& $src @ProbeArgs 2>&1 | Out-String).Trim()
            $code = $LASTEXITCODE
        } catch {
            $answer = $_.Exception.Message
        } finally {
            $ErrorActionPreference = $prev
        }

        if ($code -ne 0) {
            $why = if ($code -eq 9009) { 'не исполнился (код 9009: не найден, хотя и резолвился)' } else { "код $code" }
            $tried.Add("$name ($src) — $why")
            continue
        }
        if ($Expect -and $answer -notmatch $Expect) {
            $tried.Add("$name ($src) — ответил не тем: «$($answer -split "`n" | Select-Object -First 1)»")
            continue
        }
        return [pscustomobject]@{
            Ok = $true; Path = $src; Version = ($answer -split "`r?`n" | Select-Object -First 1)
            Reason = ''; Tried = $tried.ToArray()
        }
    }

    return [pscustomobject]@{
        Ok = $false; Path = $null; Version = $null
        Reason = 'ни один кандидат не ответил: ' + ($tried -join '; ')
        Tried = $tried.ToArray()
    }
}

function Resolve-ProductPython {
    <#
        Интерпретатор продукта. Порядок кандидатов: явное перекрытие, затем обычные имена.
        Проба спрашивает САМ интерпретатор о его версии — алиас магазина такого ответа не
        даёт, а `py` без аргументов открыл бы диалог установки.
    #>
    [CmdletBinding()]
    param()
    # Пустые кандидаты отсеиваются ДО вызова: PowerShell отвергает привязку [string[]] с
    # пустой строкой внутри, а незаданная переменная окружения даёт ровно её. Отказ при
    # этом звучит как ошибка вызова, а не как «переменная не задана», и уводит в сторону.
    $names = @($env:AIR_PYTHON, 'python', 'python3', 'py') | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    return Resolve-ProductTool -Names $names `
                                                              # ВНУТРИ ПРОБЫ НЕТ ДВОЙНЫХ КАВЫЧЕК. Windows PowerShell 5.1
                               # калечит их при передаче нативному процессу, и проба
                               # возвращала код 1 на исправном интерпретаторе — то есть
                               # проверка, написанная против обманок, сама стала обманкой.
                               # Поймано прогоном под 5.1 сразу после выноса функции.
                               -ProbeArgs @('-c', 'import sys;print(chr(80)+chr(121)+chr(116)+chr(104)+chr(111)+chr(110)+chr(32)+sys.version.split()[0])') `
                               -Expect '^Python \d'
}
